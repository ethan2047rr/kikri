// Package scheduler decides when each plan is woken: it fires due slots,
// makes up slots missed during sleep (until their deadline), retries network
// failures, applies Smart Skip and "pause today", and writes the wake log
// (PRD F3, F4.1, F4.3).
//
// All state lives in the store, keyed by (plan, slot time), and every pass
// re-derives what to do from it. That is what keeps wakes from being sent
// twice or missed when Kikri restarts, the computer sleeps, or the system
// clock moves in either direction.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/schedule"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// PlanDef is a built-in plan.
type PlanDef struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Tool   string        `json:"tool"` // runner and detect tool ID
	Window time.Duration `json:"-"`    // length of the quota window
}

// MarshalJSON reports the window in minutes for the UI.
func (p PlanDef) MarshalJSON() ([]byte, error) {
	type plain PlanDef
	return json.Marshal(struct {
		plain
		WindowMins int `json:"windowMins"`
	}{plain(p), int(p.Window / time.Minute)})
}

// Plans are the built-in plans, in display order.
var Plans = []PlanDef{
	{ID: "claude", Name: "Claude Pro / Max", Tool: "claude", Window: 5 * time.Hour},
	{ID: "chatgpt", Name: "ChatGPT Plus", Tool: "codex", Window: 5 * time.Hour},
}

// PlanByID finds a built-in plan.
func PlanByID(id string) (PlanDef, bool) {
	for _, p := range Plans {
		if p.ID == id {
			return p, true
		}
	}
	return PlanDef{}, false
}

// Outcomes the scheduler logs for slots that never reached the runner.
const (
	OutcomeExpired wake.Outcome = "expired" // missed, past its make-up deadline
	OutcomePaused  wake.Outcome = "paused"  // "pause today" was on
)

// Triggers recorded in the log.
const (
	TriggerScheduled = "scheduled"
	TriggerMakeup    = "makeup"
	TriggerRetry     = "retry"
	TriggerManual    = "manual"
)

const (
	// TickInterval is how often the engine re-checks (F4.1: every 30s, which
	// also catches wake-from-sleep and clock changes).
	TickInterval = 30 * time.Second
	// onTime is how late a slot may start and still count as "scheduled".
	onTime = 2 * time.Minute
	// lookBack bounds how far back slots are considered; deadlines are at
	// most a few hours after a slot, so a day is plenty.
	lookBack = 26 * time.Hour
	// maxSent is how many times a slot may actually send a request that ends
	// in a network error or timeout (F4.3 "limited retries").
	maxSent = 3
)

// Settings is everything the user configures for scheduling.
type Settings struct {
	Plans       map[string]schedule.Plan `json:"plans"`
	Cutoff      schedule.Clock           `json:"cutoff"`      // make-up deadline for morning slots (F4.1)
	UseActivity bool                     `json:"useActivity"` // Smart Skip basis B (D6, on by default)
	PausedUntil time.Time                `json:"pausedUntil,omitempty"`
}

const settingsKey = "scheduler"

// DefaultSettings: every plan off, early-bird on workdays when turned on.
func DefaultSettings() Settings {
	s := Settings{Plans: map[string]schedule.Plan{}, Cutoff: schedule.MustClock("09:00"), UseActivity: true}
	for _, p := range Plans {
		s.Plans[p.ID] = schedule.Plan{Preset: schedule.PresetEarlyBird, Weekdays: schedule.Workdays}
	}
	return s
}

// Deps connects the engine to the outside world. Store and Wake are required.
type Deps struct {
	Store *store.Store
	// Wake runs the full wake flow for a plan, passing skip through to
	// wake.Options.Skip (nil for manual wakes).
	Wake func(ctx context.Context, plan PlanDef, skip func(context.Context) (string, bool)) wake.Result
	// CodexQuota reads Codex rate limits (read-only). nil: unavailable.
	CodexQuota func(ctx context.Context) (*quota.Codex, error)
	// LastActivity returns when the user last used the plan's CLIs. nil: none.
	LastActivity func(plan PlanDef) time.Time
	// OnChange is called after anything the UI shows has changed.
	OnChange func()
	// OnSlot is called after a scheduled wake attempt with the slot's new
	// state, e.g. to notify the user when it ends up failed.
	OnSlot func(plan PlanDef, slot store.Slot, res wake.Result)

	Now func() time.Time
	Loc *time.Location
}

// Engine is the scheduler. Create it with New and start it with Run.
type Engine struct {
	d      Deps
	kick   chan struct{}
	mu     sync.Mutex // serialises settings writes (held briefly)
	tickMu sync.Mutex // serialises passes (held while wakes run)
}

// New creates an engine.
func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Loc == nil {
		d.Loc = time.Local
	}
	return &Engine{d: d, kick: make(chan struct{}, 1)}
}

// Run checks every TickInterval until ctx is done.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(TickInterval)
	defer t.Stop()
	for {
		e.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.kick:
		}
	}
}

// Kick asks Run for an immediate pass, e.g. after waking from sleep or a
// settings change.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Settings returns the stored settings, filling in defaults.
func (e *Engine) Settings() (Settings, error) {
	s := DefaultSettings()
	if _, err := e.d.Store.GetSetting(settingsKey, &s); err != nil {
		return DefaultSettings(), err
	}
	for _, p := range Plans { // plans added in a newer version
		if _, ok := s.Plans[p.ID]; !ok {
			s.Plans[p.ID] = DefaultSettings().Plans[p.ID]
		}
	}
	return s, nil
}

// SetPlan stores a plan's schedule. When it is turned on or its times or
// days change, Since moves to now so earlier slots are never made up.
func (e *Engine) SetPlan(id string, p schedule.Plan) error {
	if _, ok := PlanByID(id); !ok {
		return fmt.Errorf("unknown plan %q", id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.Settings()
	if err != nil {
		return err
	}
	old := s.Plans[id]
	if p.Enabled && (!old.Enabled || !sameSchedule(old, p)) {
		p.Since = e.d.Now()
	} else {
		p.Since = old.Since
	}
	s.Plans[id] = p
	if err := e.d.Store.PutSetting(settingsKey, s); err != nil {
		return err
	}
	e.changed()
	e.Kick()
	return nil
}

// UpdateSettings applies fn to the stored settings (cut-off, activity
// switch, ...). Use SetPlan to change a plan's schedule.
func (e *Engine) UpdateSettings(fn func(*Settings)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.Settings()
	if err != nil {
		return err
	}
	plans := s.Plans
	fn(&s)
	s.Plans = plans
	if err := e.d.Store.PutSetting(settingsKey, s); err != nil {
		return err
	}
	e.changed()
	return nil
}

// PauseToday skips every remaining slot of today (F3.5); resume undoes it.
func (e *Engine) PauseToday(pause bool) error {
	return e.UpdateSettings(func(s *Settings) {
		if pause {
			now := e.d.Now().In(e.d.Loc)
			s.PausedUntil = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, e.d.Loc)
		} else {
			s.PausedUntil = time.Time{}
		}
	})
}

func sameSchedule(a, b schedule.Plan) bool {
	at, bt := a.WakeTimes(), b.WakeTimes()
	if a.Weekdays != b.Weekdays || len(at) != len(bt) {
		return false
	}
	for i := range at {
		if at[i] != bt[i] {
			return false
		}
	}
	return true
}

// Tick runs one pass over all plans. Plans are handled in parallel; Tick
// returns when their wakes have finished.
func (e *Engine) Tick(ctx context.Context) {
	e.tickMu.Lock()
	defer e.tickMu.Unlock()
	s, err := e.Settings()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, def := range Plans {
		p := s.Plans[def.ID]
		if !p.Enabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.tickPlan(ctx, def, p, s)
		}()
	}
	wg.Wait()
}

func (e *Engine) tickPlan(ctx context.Context, def PlanDef, p schedule.Plan, s Settings) {
	now := e.d.Now()
	from := now.Add(-lookBack)
	slots := p.Slots(from, now, e.d.Loc)
	if len(slots) == 0 {
		return
	}
	recs, err := e.d.Store.Slots(def.ID, from)
	if err != nil {
		return
	}

	// Open slots: due and not finished. Expire the ones past their deadline.
	var live []time.Time
	for _, at := range slots {
		if r, ok := recs[at.Unix()]; ok && r.State.Final() {
			continue
		}
		deadline := schedule.Deadline(at, s.Cutoff, e.d.Loc)
		if now.After(deadline) {
			rec := recs[at.Unix()]
			why := "missed (computer asleep or Kikri not running)"
			if rec.State == store.SlotPending {
				why = "still failing (no network?)"
			}
			e.finish(def, at, rec, store.SlotExpired, now)
			e.log(def, &at, TriggerMakeup, now, wake.Result{Tool: def.Tool, Outcome: OutcomeExpired, StartedAt: now,
				Message: fmt.Sprintf("%s when the make-up deadline %s passed", why, deadline.In(e.d.Loc).Format("15:04"))})
			continue
		}
		live = append(live, at)
	}
	if len(live) == 0 {
		return
	}

	// Several open slots (e.g. 06:00 and 06:30 both slept through): one wake
	// covers them all, so only the latest runs.
	target := live[len(live)-1]
	for _, at := range live[:len(live)-1] {
		e.finish(def, at, recs[at.Unix()], store.SlotSuperseded, now)
	}

	rec := recs[target.Unix()]
	if now.Before(s.PausedUntil) {
		e.finish(def, target, rec, store.SlotPaused, now)
		e.log(def, &target, TriggerScheduled, now, wake.Result{Tool: def.Tool, Outcome: OutcomePaused, StartedAt: now, Message: "paused for today"})
		return
	}
	if rec.State == store.SlotPending && now.Before(rec.NextTry) {
		return
	}

	trigger := TriggerScheduled
	switch {
	case rec.State == store.SlotPending:
		trigger = TriggerRetry
	case now.Sub(target) > onTime:
		trigger = TriggerMakeup
	}

	res := e.d.Wake(ctx, def, e.smartSkip(def, s))

	next := store.Slot{Plan: def.ID, At: target, Attempts: rec.Attempts}
	switch res.Outcome {
	case wake.OutcomeOK:
		next.State = store.SlotDone
	case wake.OutcomeSkipped:
		next.State = store.SlotSkipped
	case wake.OutcomeBusy:
		next.State, next.NextTry = store.SlotPending, now.Add(TickInterval)
	case wake.OutcomeOffline:
		// Nothing was sent: keep probing every minute until the deadline.
		next.State, next.NextTry = store.SlotPending, now.Add(time.Minute)
	case wake.OutcomeNetworkError, wake.OutcomeTimeout:
		next.Attempts++
		if next.Attempts >= maxSent {
			next.State = store.SlotFailed
		} else {
			next.State, next.NextTry = store.SlotPending, now.Add(time.Duration(next.Attempts)*2*time.Minute)
		}
	default: // not logged in, quota exhausted, failed: retrying will not help
		next.State = store.SlotFailed
	}
	_ = e.d.Store.PutSlot(next, now)
	if e.d.OnSlot != nil {
		e.d.OnSlot(def, next, res)
	}

	// While offline the slot is re-probed every minute; log only the first.
	repeat := rec.State == store.SlotPending && (res.Outcome == wake.OutcomeOffline || res.Outcome == wake.OutcomeBusy)
	if !repeat {
		e.log(def, &target, trigger, res.StartedAt, res)
	}
	e.changed()
}

func (e *Engine) finish(def PlanDef, at time.Time, rec store.Slot, state store.SlotState, now time.Time) {
	_ = e.d.Store.PutSlot(store.Slot{Plan: def.ID, At: at, State: state, Attempts: rec.Attempts}, now)
}

func (e *Engine) log(def PlanDef, slot *time.Time, trigger string, started time.Time, res wake.Result) {
	if started.IsZero() {
		started = e.d.Now()
	}
	b, _ := json.Marshal(res)
	_, _ = e.d.Store.AddEntry(store.Entry{Plan: def.ID, SlotAt: slot, Trigger: trigger, StartedAt: started, Outcome: string(res.Outcome), Result: b})
	e.changed()
}

func (e *Engine) changed() {
	if e.d.OnChange != nil {
		e.d.OnChange()
	}
}

// WakeNow wakes a plan right away ("立即唤醒"), without Smart Skip, and logs it.
func (e *Engine) WakeNow(ctx context.Context, planID string) (wake.Result, error) {
	def, ok := PlanByID(planID)
	if !ok {
		return wake.Result{}, fmt.Errorf("unknown plan %q", planID)
	}
	res := e.d.Wake(ctx, def, nil)
	e.log(def, nil, TriggerManual, res.StartedAt, res)
	return res, nil
}

// smartSkip builds the F3.4 check for one wake. Official quota data wins
// when available; otherwise Kikri's own last wake (basis A) and the user's
// last session activity (basis B) are used.
func (e *Engine) smartSkip(def PlanDef, s Settings) func(context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		now := e.d.Now()
		hhmm := func(t time.Time) string { return t.In(e.d.Loc).Format("15:04") }
		last, _ := e.d.Store.LastEntry(def.ID, string(wake.OutcomeOK))

		official := false
		switch def.Tool {
		case "codex":
			if e.d.CodexQuota != nil {
				if q, err := e.d.CodexQuota(ctx); err == nil {
					if q.Primary == nil {
						return "this plan has no 5-hour window (" + q.PlanType + ")", true
					}
					if q.Primary.Active(now) {
						return "a window is already running; it resets at " + hhmm(q.Primary.ResetsAt), true
					}
					return "", false // authoritative: no window right now
				}
			}
		case "claude":
			// Claude has no read-only query; use the window reported by the
			// last successful wake (basis 0, cached).
			if w := fiveHour(last); w != nil {
				official = true
				if w.ResetsAt.After(now) {
					return "the window opened by the " + hhmm(last.StartedAt) + " wake runs until " + hhmm(w.ResetsAt), true
				}
			}
		}

		if !official && last != nil && now.Sub(last.StartedAt) < def.Window {
			return "Kikri already woke it at " + hhmm(last.StartedAt), true
		}
		if s.UseActivity && e.d.LastActivity != nil {
			if t := e.d.LastActivity(def); !t.IsZero() && now.Sub(t) < def.Window && !t.After(now) {
				return "you used it at " + hhmm(t) + ", so a window is already running", true
			}
		}
		return "", false
	}
}

// fiveHour extracts the cached 5-hour window from a logged wake result.
func fiveHour(e *store.Entry) *wake.Window {
	for _, w := range windowsOf(e) {
		if w.Kind == "five_hour" && !w.ResetsAt.IsZero() {
			return &w
		}
	}
	return nil
}

func windowsOf(e *store.Entry) []wake.Window {
	if e == nil || len(e.Result) == 0 {
		return nil
	}
	var r wake.Result
	if json.Unmarshal(e.Result, &r) != nil {
		return nil
	}
	return r.Windows
}

// CachedWindows returns the quota windows reported by the plan's last
// successful wake (Claude has no read-only query, so this is all there is),
// and when that wake happened.
func (e *Engine) CachedWindows(planID string) ([]wake.Window, time.Time) {
	last, err := e.d.Store.LastEntry(planID, string(wake.OutcomeOK))
	if err != nil || last == nil {
		return nil, time.Time{}
	}
	return windowsOf(last), last.StartedAt
}

// PlanStatus is what the UI shows per plan.
type PlanStatus struct {
	Plan     PlanDef          `json:"plan"`
	Schedule schedule.Plan    `json:"schedule"`
	Times    []schedule.Clock `json:"times"` // effective wake times (preset resolved)
	Next     *time.Time       `json:"next,omitempty"`
	Last     *store.Entry     `json:"last,omitempty"`
	Paused   bool             `json:"paused"`
}

// Status reports every plan's schedule, next slot and last log entry.
func (e *Engine) Status() ([]PlanStatus, error) {
	s, err := e.Settings()
	if err != nil {
		return nil, err
	}
	now := e.d.Now()
	out := make([]PlanStatus, 0, len(Plans))
	for _, def := range Plans {
		p := s.Plans[def.ID]
		st := PlanStatus{Plan: def, Schedule: p, Times: p.WakeTimes(), Paused: now.Before(s.PausedUntil)}
		if n, ok := p.Next(now, e.d.Loc); ok {
			st.Next = &n
		}
		st.Last, _ = e.d.Store.LastEntry(def.ID)
		out = append(out, st)
	}
	return out, nil
}
