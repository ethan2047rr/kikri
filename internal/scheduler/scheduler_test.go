package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/schedule"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

var loc = time.FixedZone("UTC+8", 8*3600)

// 2026-11-26 is a Thursday.
func at(hhmm string) time.Time { return atDay("2026-11-26", hhmm) }

func atDay(day, hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", day+" "+hhmm, loc)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
	}
	if err != nil {
		panic(err)
	}
	return t
}

// harness is an engine with a settable clock and a scripted fake waker.
type harness struct {
	t        *testing.T
	st       *store.Store
	path     string
	now      time.Time
	results  []wake.Outcome // returned in order; then OK
	calls    []time.Time
	e        *Engine
	quota    *quota.Codex
	active   time.Time
	reported time.Time // Claude Code status line's five_hour resets_at
}

func newHarness(t *testing.T, plan schedule.Plan) *harness {
	t.Helper()
	h := &harness{t: t, path: filepath.Join(t.TempDir(), "kikri.db")}
	h.open()
	h.now = plan.Since
	if err := h.e.SetPlan("claude", plan); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) open() {
	st, err := store.Open(h.path)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { st.Close() })
	h.st = st
	h.e = New(Deps{
		Store: st,
		Now:   func() time.Time { return h.now },
		Loc:   loc,
		Wake: func(ctx context.Context, plan PlanDef, skip func(context.Context) (string, bool)) wake.Result {
			if skip != nil {
				if why, ok := skip(ctx); ok {
					return wake.Result{Tool: plan.Tool, Outcome: wake.OutcomeSkipped, Message: why, StartedAt: h.now}
				}
			}
			h.calls = append(h.calls, h.now)
			out := wake.OutcomeOK
			if len(h.results) > 0 {
				out, h.results = h.results[0], h.results[1:]
			}
			res := wake.Result{Tool: plan.Tool, Outcome: out, StartedAt: h.now}
			if out == wake.OutcomeOK && plan.Tool == "claude" {
				res.Windows = []wake.Window{{Kind: "five_hour", ResetsAt: h.now.Add(5 * time.Hour).Truncate(time.Minute)}}
			}
			return res
		},
		CodexQuota: func(context.Context) (*quota.Codex, error) {
			if h.quota == nil {
				return nil, context.DeadlineExceeded
			}
			return h.quota, nil
		},
		LastActivity:     func(PlanDef) time.Time { return h.active },
		ReportedFiveHour: func(PlanDef) (time.Time, time.Time) { return h.reported, h.now },
	})
}

func (h *harness) deferOn() {
	h.t.Helper()
	if err := h.e.UpdateSettings(func(s *Settings) { s.DeferSkipped = true }); err != nil {
		h.t.Fatal(err)
	}
}

// restart simulates quitting and relaunching Kikri: same database, new engine.
func (h *harness) restart() { h.st.Close(); h.open() }

func (h *harness) tickAt(t time.Time) { h.now = t; h.e.Tick(context.Background()) }

func (h *harness) wantCalls(n int) {
	h.t.Helper()
	if len(h.calls) != n {
		h.t.Fatalf("wake called %d times (%v), want %d", len(h.calls), h.calls, n)
	}
}

func (h *harness) slot(t time.Time) store.Slot {
	h.t.Helper()
	m, err := h.st.Slots("claude", t.Add(-time.Second))
	if err != nil {
		h.t.Fatal(err)
	}
	return m[t.Unix()]
}

func (h *harness) log() []store.Entry {
	es, err := h.st.Entries("claude", 100)
	if err != nil {
		h.t.Fatal(err)
	}
	return es
}

func early(since time.Time) schedule.Plan {
	return schedule.Plan{Enabled: true, Preset: schedule.PresetEarlyBird, Weekdays: schedule.Workdays, Since: since}
}

func TestOnTimeWakeRunsOnce(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.tickAt(at("06:29:50"))
	h.wantCalls(0)
	h.tickAt(at("06:30:10"))
	h.wantCalls(1)
	h.tickAt(at("06:30:40"))
	h.tickAt(at("07:00"))
	h.restart()
	h.tickAt(at("07:01"))
	h.wantCalls(1)
	if s := h.slot(at("06:30")); s.State != store.SlotDone {
		t.Fatalf("slot = %+v", s)
	}
	if l := h.log(); len(l) != 1 || l[0].Trigger != TriggerScheduled || l[0].Outcome != "ok" {
		t.Fatalf("log = %+v", l)
	}
}

func TestEnablingLateDoesNotFireEarlierSlot(t *testing.T) {
	h := newHarness(t, early(at("08:00")))
	h.tickAt(at("08:00:30"))
	h.wantCalls(0)
	if len(h.log()) != 0 {
		t.Fatal("slot before enabling must not be logged as expired either")
	}
}

func TestSleepThroughSlotsMakesUpOnce(t *testing.T) {
	plan := schedule.Plan{Enabled: true, Preset: schedule.PresetCustom, Weekdays: schedule.Workdays, Since: at("05:00"),
		Times: []schedule.Clock{schedule.MustClock("06:00"), schedule.MustClock("06:30")}}
	h := newHarness(t, plan)
	h.tickAt(at("05:59"))
	// Asleep from 05:59 to 07:40.
	h.tickAt(at("07:40"))
	h.wantCalls(1)
	if s := h.slot(at("06:00")); s.State != store.SlotSuperseded {
		t.Fatalf("06:00 = %+v", s)
	}
	if s := h.slot(at("06:30")); s.State != store.SlotDone {
		t.Fatalf("06:30 = %+v", s)
	}
	if l := h.log(); len(l) != 1 || l[0].Trigger != TriggerMakeup {
		t.Fatalf("log = %+v", l)
	}
}

func TestSleepPastCutoffSkips(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.tickAt(at("06:00"))
	h.tickAt(at("09:10")) // woke after the 09:00 cut-off
	h.tickAt(at("09:11"))
	h.wantCalls(0)
	l := h.log()
	if len(l) != 1 || l[0].Outcome != string(OutcomeExpired) {
		t.Fatalf("log = %+v", l)
	}
}

func TestClockMovedBackDoesNotRepeat(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.tickAt(at("06:30:05"))
	h.wantCalls(1)
	h.tickAt(at("05:10")) // user (or NTP) sets the clock back
	h.tickAt(at("06:30:05"))
	h.tickAt(at("06:31"))
	h.wantCalls(1)
	// The slot must not even reach Smart Skip a second time.
	if l := h.log(); len(l) != 1 {
		t.Fatalf("log = %+v", l)
	}
}

func TestClockMovedForwardPastDeadlineDoesNotFire(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.tickAt(at("05:10"))
	h.tickAt(at("10:00")) // clock jumps forward past the cut-off
	h.wantCalls(0)
	h.tickAt(at("05:20")) // and back again: the slot stays expired
	h.tickAt(at("06:30:05"))
	h.wantCalls(0)
}

func TestOfflineKeepsProbingAndLogsOnce(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.results = []wake.Outcome{wake.OutcomeOffline, wake.OutcomeOffline}
	h.tickAt(at("06:30:05"))
	h.tickAt(at("06:30:35")) // before NextTry: no attempt
	h.wantCalls(1)
	h.tickAt(at("06:31:10")) // offline again
	h.tickAt(at("06:32:20")) // back online
	h.wantCalls(3)
	if s := h.slot(at("06:30")); s.State != store.SlotDone || s.Attempts != 0 {
		t.Fatalf("slot = %+v", s)
	}
	l := h.log()
	if len(l) != 2 || l[0].Outcome != "ok" || l[0].Trigger != TriggerRetry || l[1].Outcome != string(wake.OutcomeOffline) {
		t.Fatalf("log = %+v", l)
	}
}

func TestOfflineUntilDeadlineExpires(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.results = []wake.Outcome{wake.OutcomeOffline}
	h.tickAt(at("06:30:05"))
	h.tickAt(at("09:00:30"))
	h.wantCalls(1)
	if s := h.slot(at("06:30")); s.State != store.SlotExpired {
		t.Fatalf("slot = %+v", s)
	}
}

func TestNetworkErrorRetriesThenGivesUp(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.results = []wake.Outcome{wake.OutcomeNetworkError, wake.OutcomeTimeout, wake.OutcomeNetworkError}
	h.tickAt(at("06:30:05"))
	h.tickAt(at("06:31:05")) // backoff 2 min: too early
	h.wantCalls(1)
	h.tickAt(at("06:32:10"))
	h.wantCalls(2)
	h.tickAt(at("06:35:00")) // backoff 4 min: too early
	h.tickAt(at("06:36:20"))
	h.wantCalls(3)
	h.tickAt(at("07:00"))
	h.wantCalls(3)
	if s := h.slot(at("06:30")); s.State != store.SlotFailed || s.Attempts != maxSent {
		t.Fatalf("slot = %+v", s)
	}
}

func TestFinalFailuresDoNotRetry(t *testing.T) {
	for _, out := range []wake.Outcome{wake.OutcomeNotLoggedIn, wake.OutcomeQuotaExhausted, wake.OutcomeFailed} {
		h := newHarness(t, early(at("05:00")))
		h.results = []wake.Outcome{out}
		h.tickAt(at("06:30:05"))
		h.tickAt(at("06:35"))
		h.tickAt(at("08:00"))
		h.wantCalls(1)
		if s := h.slot(at("06:30")); s.State != store.SlotFailed {
			t.Fatalf("%s: slot = %+v", out, s)
		}
	}
}

func TestPauseToday(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.now = at("06:00")
	if err := h.e.PauseToday(true); err != nil {
		t.Fatal(err)
	}
	h.tickAt(at("06:30:05"))
	h.wantCalls(0)
	if s := h.slot(at("06:30")); s.State != store.SlotPaused {
		t.Fatalf("slot = %+v", s)
	}
	// The pause ends at midnight.
	h.tickAt(atDay("2026-11-27", "06:30:05"))
	h.wantCalls(1)
}

func TestWeekendFiltered(t *testing.T) {
	h := newHarness(t, early(atDay("2026-11-28", "05:00"))) // Saturday
	h.tickAt(atDay("2026-11-28", "06:30:05"))
	h.tickAt(atDay("2026-11-29", "06:30:05"))
	h.wantCalls(0)
	h.tickAt(atDay("2026-11-30", "06:30:05")) // Monday
	h.wantCalls(1)
}

func TestSmartSkipClaudeUsesCachedWindow(t *testing.T) {
	// The three-round preset: 06:00 wake opens a window until 11:00, so the
	// 11:05 slot runs; a manual wake at 09:00 would make it skip.
	plan := schedule.Plan{Enabled: true, Preset: schedule.PresetThree, Weekdays: schedule.Workdays, Since: at("05:00")}
	h := newHarness(t, plan)
	h.tickAt(at("06:00:05"))
	h.tickAt(at("11:05:05"))
	h.wantCalls(2)

	h2 := newHarness(t, plan)
	h2.tickAt(at("06:00:05"))
	h2.now = at("09:00")
	if _, err := h2.e.WakeNow(context.Background(), "claude"); err != nil {
		t.Fatal(err)
	}
	h2.tickAt(at("11:05:05"))
	h2.wantCalls(2) // 06:00 and the manual one; 11:05 skipped
	if s := h2.slot(at("11:05")); s.State != store.SlotSkipped {
		t.Fatalf("11:05 = %+v", s)
	}
}

func TestSmartSkipActivity(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.active = at("05:58") // the user was coding before the alarm
	h.tickAt(at("06:30:05"))
	h.wantCalls(0)
	if s := h.slot(at("06:30")); s.State != store.SlotSkipped {
		t.Fatalf("slot = %+v", s)
	}

	h2 := newHarness(t, early(at("05:00")))
	h2.active = at("05:58")
	if err := h2.e.UpdateSettings(func(s *Settings) { s.UseActivity = false }); err != nil {
		t.Fatal(err)
	}
	h2.tickAt(at("06:30:05"))
	h2.wantCalls(1)
}

func TestSmartSkipCodexOfficialDataWins(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	var refresh time.Time
	skip := h.e.smartSkip(Plans[1], DefaultSettings(), &refresh)
	now := at("06:30")
	h.now = now
	h.active = at("05:58") // recent activity, but...

	// ...official data says no window (virtual now+5h): wake anyway.
	h.quota = &quota.Codex{PlanType: "plus", Primary: &quota.Window{WindowMins: 300, ResetsAt: now.Add(5*time.Hour + time.Second)}}
	if why, ok := skip(context.Background()); ok {
		t.Fatalf("skipped: %s", why)
	}
	// A real running window: skip.
	h.quota.Primary.ResetsAt = at("10:58")
	if _, ok := skip(context.Background()); !ok {
		t.Fatal("active window not skipped")
	}
	// No 5-hour window on this plan (e.g. Pro): skip.
	h.quota = &quota.Codex{PlanType: "pro"}
	if _, ok := skip(context.Background()); !ok {
		t.Fatal("plan without window not skipped")
	}
	// Query failed: fall back to activity.
	h.quota = nil
	if _, ok := skip(context.Background()); !ok {
		t.Fatal("fallback to activity failed")
	}
}

func TestSetPlanKeepsSinceUnlessScheduleChanges(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.now = at("12:00")
	p := early(time.Time{})
	if err := h.e.SetPlan("claude", p); err != nil {
		t.Fatal(err)
	}
	s, _ := h.e.Settings()
	if !s.Plans["claude"].Since.Equal(at("05:00")) {
		t.Fatalf("Since moved on an unchanged save: %s", s.Plans["claude"].Since)
	}
	p.Weekdays = schedule.Everyday
	h.e.SetPlan("claude", p)
	s, _ = h.e.Settings()
	if !s.Plans["claude"].Since.Equal(at("12:00")) {
		t.Fatalf("Since not moved after a change: %s", s.Plans["claude"].Since)
	}
}

func TestOnSlotAndCachedWindows(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	var got []store.SlotState
	h.e.d.OnSlot = func(_ PlanDef, sl store.Slot, _ wake.Result) { got = append(got, sl.State) }
	h.results = []wake.Outcome{wake.OutcomeNotLoggedIn}
	h.tickAt(at("06:30:05"))
	if len(got) != 1 || got[0] != store.SlotFailed {
		t.Fatalf("OnSlot states = %v", got)
	}
	if w, _ := h.e.CachedWindows("claude"); w != nil {
		t.Fatal("no successful wake yet")
	}
	h.tickAt(atDay("2026-11-27", "06:30:05"))
	w, when := h.e.CachedWindows("claude")
	if len(w) != 1 || w[0].Kind != "five_hour" || !when.Equal(atDay("2026-11-27", "06:30:05")) {
		t.Fatalf("cached = %+v at %s", w, when)
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, early(at("05:00")))
	h.tickAt(at("06:30:05"))
	st, err := h.e.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st[0].Plan.ID != "claude" || st[0].Next == nil || !st[0].Next.Equal(atDay("2026-11-27", "06:30")) || st[0].Last == nil {
		t.Fatalf("status = %+v", st[0])
	}
	if st[1].Schedule.Enabled || st[1].Next != nil {
		t.Fatalf("chatgpt should be off: %+v", st[1])
	}
}

func custom(since time.Time, times ...string) schedule.Plan {
	cs := make([]schedule.Clock, len(times))
	for i, t := range times {
		cs[i] = schedule.MustClock(t)
	}
	return schedule.Plan{Enabled: true, Preset: schedule.PresetCustom, Times: cs, Weekdays: schedule.Workdays, Since: since}
}

// codexHarness runs the Codex plan, whose Smart Skip uses official data.
// Deferral (off by default) is turned on.
func codexHarness(t *testing.T, plan schedule.Plan) *harness {
	t.Helper()
	h := newHarness(t, schedule.Plan{Since: plan.Since})
	h.deferOn()
	if err := h.e.SetPlan("chatgpt", plan); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) codexSlot(t time.Time) store.Slot {
	h.t.Helper()
	m, err := h.st.Slots("chatgpt", t.Add(-time.Second))
	if err != nil {
		h.t.Fatal(err)
	}
	return m[t.Unix()]
}

func running(resets time.Time) *quota.Codex {
	return &quota.Codex{PlanType: "plus", Primary: &quota.Window{WindowMins: 300, ResetsAt: resets}}
}

func TestDeferSkippedWakesAfterRefresh(t *testing.T) {
	h := codexHarness(t, custom(at("05:00"), "12:05"))
	h.quota = running(at("13:30:20"))
	h.tickAt(at("12:05:05"))
	h.wantCalls(0)
	s := h.codexSlot(at("12:05"))
	if s.State != store.SlotDeferred || !s.NextTry.Equal(at("13:35:20")) {
		t.Fatalf("12:05 = %+v", s)
	}
	st, _ := h.e.Status()
	if st[1].Deferred == nil || !st[1].Deferred.Equal(at("13:35:20")) {
		t.Fatalf("status deferred = %v", st[1].Deferred)
	}

	h.tickAt(at("13:00"))
	h.wantCalls(0)
	h.quota = running(at("13:35:30").Add(5 * time.Hour)) // virtual: no window
	h.tickAt(at("13:35:30"))
	h.wantCalls(1)
	if s := h.codexSlot(at("12:05")); s.State != store.SlotDone {
		t.Fatalf("after deferred wake = %+v", s)
	}
	es, _ := h.st.Entries("chatgpt", 10)
	if es[0].Trigger != TriggerDeferred {
		t.Fatalf("trigger = %q", es[0].Trigger)
	}
}

func TestDeferSkippedOnlyOnce(t *testing.T) {
	h := codexHarness(t, custom(at("05:00"), "12:05"))
	h.quota = running(at("13:30"))
	h.tickAt(at("12:05:05"))
	h.quota = running(at("15:00")) // the user started a new window meanwhile
	h.tickAt(at("13:35:05"))
	h.wantCalls(0)
	if s := h.codexSlot(at("12:05")); s.State != store.SlotSkipped {
		t.Fatalf("second skip = %+v", s)
	}
}

func TestDeferSkippedOffByDefault(t *testing.T) {
	h := newHarness(t, schedule.Plan{Since: at("05:00")})
	if err := h.e.SetPlan("chatgpt", custom(at("05:00"), "12:05")); err != nil {
		t.Fatal(err)
	}
	h.quota = running(at("13:30"))
	h.tickAt(at("12:05:05"))
	if s := h.codexSlot(at("12:05")); s.State != store.SlotSkipped {
		t.Fatalf("12:05 = %+v", s)
	}
}

func TestDeferSkippedNotPastNextSlot(t *testing.T) {
	// 06:30 is skipped while a window runs to 11:00; deferring to 11:05 would
	// collide with the 11:00 slot, so the next slot handles it.
	h := codexHarness(t, custom(at("05:00"), "06:30", "11:00"))
	h.quota = running(at("11:00"))
	h.tickAt(at("06:30:05"))
	if s := h.codexSlot(at("06:30")); s.State != store.SlotSkipped {
		t.Fatalf("06:30 = %+v", s)
	}
}

func TestDeferSkippedNotPastMidnight(t *testing.T) {
	h := codexHarness(t, custom(at("05:00"), "21:00"))
	h.quota = running(at("23:58"))
	h.tickAt(at("21:00:05"))
	if s := h.codexSlot(at("21:00")); s.State != store.SlotSkipped {
		t.Fatalf("21:00 = %+v", s)
	}
}

func TestDeferredMissedExpires(t *testing.T) {
	h := codexHarness(t, custom(at("05:00"), "12:05"))
	h.quota = running(at("13:30"))
	h.tickAt(at("12:05:05"))
	h.tickAt(at("16:00")) // asleep through 13:35 + grace
	h.wantCalls(0)
	if s := h.codexSlot(at("12:05")); s.State != store.SlotExpired {
		t.Fatalf("12:05 = %+v", s)
	}
}

func TestDeferSkippedClaudeCachedWindow(t *testing.T) {
	// Claude: the 06:00 wake opened a window to 11:00; a 09:00 slot is
	// deferred to 11:05.
	h := newHarness(t, custom(at("05:00"), "06:00", "09:00"))
	h.deferOn()
	h.tickAt(at("06:00:05"))
	h.tickAt(at("09:00:05"))
	h.wantCalls(1)
	if s := h.slot(at("09:00")); s.State != store.SlotDeferred || !s.NextTry.Equal(at("11:05")) {
		t.Fatalf("09:00 = %+v", s)
	}
	h.tickAt(at("11:05:10"))
	h.wantCalls(2)
}

func TestSmartSkipClaudeStatusLine(t *testing.T) {
	// The user opened a window at 05:40 by working; Claude Code's status line
	// reported it resets at 10:40, so 06:30 is skipped with that time known
	// (and deferral, when on, wakes at 10:45).
	h := newHarness(t, early(at("05:00")))
	h.deferOn()
	h.reported = at("10:40")
	h.tickAt(at("06:30:05"))
	h.wantCalls(0)
	if s := h.slot(at("06:30")); s.State != store.SlotDeferred || !s.NextTry.Equal(at("10:45")) {
		t.Fatalf("06:30 = %+v", s)
	}
	h.tickAt(at("10:45:10"))
	h.wantCalls(1)
}
