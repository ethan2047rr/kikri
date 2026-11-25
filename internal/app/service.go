package app

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/ethan2047rr/kikri/internal/activity"
	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/autostart"
	"github.com/ethan2047rr/kikri/internal/detect"
	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/schedule"
	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// EventSnapshot is emitted to the frontend whenever a new Snapshot is ready.
const EventSnapshot = "kikri:snapshot"

// Prefs are app settings outside the scheduler.
type Prefs struct {
	Onboarded bool `json:"onboarded"`
	Notify    bool `json:"notify"`
}

const prefsKey = "app"

func defaultPrefs() Prefs { return Prefs{Notify: true} }

// Snapshot is everything the popup shows.
type Snapshot struct {
	Version      string        `json:"version"`
	AppDir       appdir.Layout `json:"appDir"`
	AppDirError  string        `json:"appDirError,omitempty"`
	Detecting    bool          `json:"detecting"`
	Report       detect.Report `json:"report"`
	DetectedOnce bool          `json:"detectedOnce"`

	Plans      []scheduler.PlanStatus `json:"plans,omitempty"`
	Settings   *scheduler.Settings    `json:"settings,omitempty"`
	StoreError string                 `json:"storeError,omitempty"`

	Prefs             Prefs                 `json:"prefs"`
	Autostart         bool                  `json:"autostart"`
	NotifyUnavailable string                `json:"notifyUnavailable,omitempty"`
	Quota             map[string]*PlanQuota `json:"quota"`
	Stats             Stats                 `json:"stats"`
	AgentsMd          *AgentsMd             `json:"agentsMd,omitempty"`
	Waking            map[string]bool       `json:"waking"`
	// LogVersion changes whenever the log may have changed, so an open log
	// view knows to reload.
	LogVersion int64 `json:"logVersion"`
}

// Service is bound to the frontend; its exported methods are callable from
// TypeScript as "github.com/ethan2047rr/kikri/internal/app.Service.<Method>".
type Service struct {
	version string
	emit    func(Snapshot)
	quit    func()
	notes   notifier

	mu       sync.Mutex
	snap     Snapshot
	inflight chan struct{} // non-nil while a detection pass runs

	store  *store.Store
	engine *scheduler.Engine // nil when the data directory or database failed

	qmu       sync.Mutex
	quotaBusy bool
	quotaAt   time.Time
	codexLive *PlanQuota
	lastUsed  map[string]time.Time
}

// NewService creates the service. emit may be nil.
func NewService(version string, emit func(Snapshot)) *Service {
	return &Service{
		version: version, emit: emit,
		snap: Snapshot{Version: version, Prefs: defaultPrefs(), Quota: map[string]*PlanQuota{}, Waking: map[string]bool{}},
	}
}

// Prepare creates the data directory and sandbox and opens the database.
// It is called once at startup.
func (s *Service) Prepare() {
	layout, err := appdir.Ensure()
	s.mu.Lock()
	s.snap.AppDir = layout
	if err != nil {
		s.snap.AppDirError = err.Error()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	st, err := store.Open(layout.Database)
	if err != nil {
		s.mu.Lock()
		s.snap.StoreError = err.Error()
		s.mu.Unlock()
		return
	}
	s.store = st
	prefs := defaultPrefs()
	if _, err := st.GetSetting(prefsKey, &prefs); err != nil {
		slog.Warn("read prefs", "err", err)
	}
	on, _ := autostart.Enabled()
	s.mu.Lock()
	s.snap.Prefs, s.snap.Autostart = prefs, on
	s.mu.Unlock()
	s.engine = newEngine(layout, st, s.plansChanged, s.slotFinished)
	s.plansChanged()
}

// startNotifications sets up system notifications; call it once the GUI
// application is running.
func (s *Service) startNotifications() {
	s.notes.start()
	s.mu.Lock()
	s.snap.NotifyUnavailable = s.notes.unavailable()
	notify := s.snap.Prefs.Notify && s.snap.Prefs.Onboarded
	snap := s.snap
	s.mu.Unlock()
	if notify {
		s.notes.authorize()
	}
	s.publish(snap)
}

// Close releases the database.
func (s *Service) Close() {
	if s.store != nil {
		s.store.Close()
	}
}

// StartScheduler runs the scheduler until ctx is done.
func (s *Service) StartScheduler(ctx context.Context) {
	if s.engine != nil {
		go s.engine.Run(ctx)
	}
}

// Kick asks the scheduler for an immediate pass (after sleep, for example).
func (s *Service) Kick() {
	if s.engine != nil {
		s.engine.Kick()
	}
}

// slotFinished runs after every scheduled attempt.
func (s *Service) slotFinished(plan scheduler.PlanDef, sl store.Slot, res wake.Result) {
	if res.Outcome.Sent() || res.Outcome == wake.OutcomeSkipped {
		go s.RefreshQuota(true)
	}
	s.mu.Lock()
	notify := s.snap.Prefs.Notify
	s.mu.Unlock()
	if title, body, ok := slotNotice(plan, sl, res); ok && notify {
		s.notes.send(noticeID(plan, sl.At), title, body)
	}
}

func (s *Service) plansChanged() {
	if s.engine == nil {
		return
	}
	now := time.Now()
	plans, err := s.engine.Status()
	settings, serr := s.engine.Settings()
	var stats Stats
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	stats.MonthWakes, _ = s.store.CountEntries(string(wake.OutcomeOK), month)
	stats.MonthSkipped, _ = s.store.CountEntries(string(wake.OutcomeSkipped), month)
	q := s.quotaView(now)

	s.mu.Lock()
	if err == nil {
		s.snap.Plans = plans
	}
	if serr == nil {
		s.snap.Settings = &settings
	}
	s.snap.Stats = stats
	s.snap.Quota = q
	s.snap.LogVersion++
	snap := s.snap
	s.mu.Unlock()
	s.publish(snap)
}

// quotaView merges the live Codex answer, Claude's cached windows and
// session activity.
func (s *Service) quotaView(now time.Time) map[string]*PlanQuota {
	s.qmu.Lock()
	live, used := s.codexLive, s.lastUsed
	s.qmu.Unlock()
	out := map[string]*PlanQuota{}
	for _, def := range scheduler.Plans {
		var pq *PlanQuota
		if def.Tool == "codex" {
			if live != nil {
				c := *live
				pq = &c
			}
		} else if ws, at := s.engine.CachedWindows(def.ID); ws != nil {
			pq = cachedQuota(ws, at, now)
		}
		if t, ok := used[def.Tool]; ok && !t.IsZero() {
			if pq == nil {
				pq = &PlanQuota{}
			}
			t := t
			pq.LastUsed = &t
		}
		if pq != nil {
			out[def.ID] = pq
		}
	}
	return out
}

// quotaTTL limits how often opening the popup re-queries Codex.
const quotaTTL = time.Minute

// RefreshQuota re-reads Codex's official usage (read-only, it never opens a
// window) and the session activity. Without force it does nothing if the
// data is younger than a minute.
func (s *Service) RefreshQuota(force bool) {
	s.qmu.Lock()
	if s.quotaBusy || (!force && time.Since(s.quotaAt) < quotaTTL) || s.engine == nil {
		s.qmu.Unlock()
		return
	}
	s.quotaBusy = true
	s.qmu.Unlock()

	used := map[string]time.Time{
		"claude": activity.Latest(activity.ClaudeRoots()...),
		"codex":  activity.Latest(activity.CodexRoots()...),
	}
	var live *PlanQuota
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if bin, env, err := resolveTool(ctx, "codex"); err == nil {
		now := time.Now()
		if q, err := quota.ReadCodex(ctx, bin, env, s.Snapshot().AppDir.Sandbox); err == nil {
			live = codexQuota(q, now)
		} else {
			live = &PlanQuota{Source: "live", At: &now, Error: err.Error()}
		}
	}
	cancel()

	s.qmu.Lock()
	s.quotaBusy, s.quotaAt = false, time.Now()
	s.codexLive, s.lastUsed = live, used
	s.qmu.Unlock()
	s.plansChanged()
}

var errNoEngine = errors.New("scheduler unavailable: the data directory or database could not be opened")

func (s *Service) setWaking(planID string, on bool) {
	s.mu.Lock()
	w := make(map[string]bool, len(s.snap.Waking)+1)
	for k, v := range s.snap.Waking {
		w[k] = v
	}
	if on {
		w[planID] = true
	} else {
		delete(w, planID)
	}
	s.snap.Waking = w
	snap := s.snap
	s.mu.Unlock()
	s.publish(snap)
}

// WakeNow wakes a plan right away, without Smart Skip ("立即唤醒").
func (s *Service) WakeNow(ctx context.Context, planID string) (wake.Result, error) {
	if s.engine == nil {
		return wake.Result{}, errNoEngine
	}
	s.setWaking(planID, true)
	defer s.setWaking(planID, false)
	// A wake must finish even if the popup that asked for it closes.
	res, err := s.engine.WakeNow(context.WithoutCancel(ctx), planID)
	if err == nil && res.Outcome.Sent() {
		go s.RefreshQuota(true)
	}
	return res, err
}

// SetPlan stores one plan's schedule.
func (s *Service) SetPlan(planID string, plan schedule.Plan) error {
	if s.engine == nil {
		return errNoEngine
	}
	return s.engine.SetPlan(planID, plan)
}

// SetCutoff sets the make-up deadline for morning slots ("HH:MM").
func (s *Service) SetCutoff(hhmm string) error {
	if s.engine == nil {
		return errNoEngine
	}
	c, err := schedule.ParseClock(hhmm)
	if err != nil {
		return err
	}
	return s.engine.UpdateSettings(func(st *scheduler.Settings) { st.Cutoff = c })
}

// SetUseActivity turns Smart Skip basis B (session file mtimes) on or off.
func (s *Service) SetUseActivity(on bool) error {
	if s.engine == nil {
		return errNoEngine
	}
	return s.engine.UpdateSettings(func(st *scheduler.Settings) { st.UseActivity = on })
}

// PauseToday skips (or, with false, restores) the rest of today's wakes.
func (s *Service) PauseToday(pause bool) error {
	if s.engine == nil {
		return errNoEngine
	}
	return s.engine.PauseToday(pause)
}

func (s *Service) updatePrefs(fn func(*Prefs)) error {
	if s.store == nil {
		return errNoEngine
	}
	s.mu.Lock()
	p := s.snap.Prefs
	fn(&p)
	if err := s.store.PutSetting(prefsKey, p); err != nil {
		s.mu.Unlock()
		return err
	}
	s.snap.Prefs = p
	snap := s.snap
	s.mu.Unlock()
	s.publish(snap)
	return nil
}

// SetNotify turns failure notifications on or off.
func (s *Service) SetNotify(on bool) error {
	if err := s.updatePrefs(func(p *Prefs) { p.Notify = on }); err != nil {
		return err
	}
	if on {
		go s.notes.authorize()
	}
	return nil
}

// SetAutostart adds or removes the login item.
func (s *Service) SetAutostart(on bool) error {
	err := autostart.Set(on)
	cur, _ := autostart.Enabled()
	s.mu.Lock()
	s.snap.Autostart = cur
	snap := s.snap
	s.mu.Unlock()
	s.publish(snap)
	return err
}

// FinishOnboarding closes the welcome view.
func (s *Service) FinishOnboarding(startAtLogin bool) error {
	if err := s.updatePrefs(func(p *Prefs) { p.Onboarded = true }); err != nil {
		return err
	}
	s.mu.Lock()
	notify := s.snap.Prefs.Notify
	s.mu.Unlock()
	if notify {
		go s.notes.authorize()
	}
	if startAtLogin {
		return s.SetAutostart(true)
	}
	return nil
}

// Log returns the newest wake log entries (all plans when planID is empty).
func (s *Service) Log(planID string, limit int) ([]store.Entry, error) {
	if s.store == nil {
		return nil, errNoEngine
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.Entries(planID, limit)
}

// OpenDataFolder shows Kikri's data directory (database, sandbox) in the
// file manager.
func (s *Service) OpenDataFolder() error {
	dir := s.Snapshot().AppDir.Root
	if dir == "" {
		return errors.New("data directory unavailable")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

// Quit exits Kikri.
func (s *Service) Quit() {
	if s.quit != nil {
		s.quit()
	}
}

// Snapshot returns the latest state without running detection.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// Refresh runs a detection pass (joining one already in flight) and returns
// the resulting state.
func (s *Service) Refresh(ctx context.Context) Snapshot {
	s.mu.Lock()
	if ch := s.inflight; ch != nil {
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		return s.Snapshot()
	}
	ch := make(chan struct{})
	s.inflight = ch
	s.snap.Detecting = true
	dir := s.snap.AppDir.Sandbox
	if s.snap.AppDirError != "" {
		dir = "" // never run a CLI inside a sandbox that failed validation
	}
	pending := s.snap
	s.mu.Unlock()
	s.publish(pending)

	// Detection must not be cut short by the caller going away (e.g. the
	// popup closing); it has its own per-command timeouts.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	report := detect.Run(runCtx, detect.Options{Dir: dir})
	cancel()

	var agents *AgentsMd
	if p, n, ok := wake.CodexGlobalAgents(); ok && n > wake.AgentsWarnBytes {
		agents = &AgentsMd{Path: p, Bytes: n, Tokens: wake.EstimateTokens(n)}
	}

	s.mu.Lock()
	s.snap.Report = report
	s.snap.Detecting = false
	s.snap.DetectedOnce = true
	s.snap.AgentsMd = agents
	s.inflight = nil
	done := s.snap
	s.mu.Unlock()
	close(ch)
	s.publish(done)
	go s.RefreshQuota(true)
	return done
}

func (s *Service) publish(snap Snapshot) {
	if s.emit != nil {
		s.emit(snap)
	}
}
