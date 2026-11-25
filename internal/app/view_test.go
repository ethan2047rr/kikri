package app

import (
	"strings"
	"testing"
	"time"

	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

var now = time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local)

func TestCodexQuotaDropsVirtualReset(t *testing.T) {
	// No window running: Codex answers with resetsAt = now + 5h (V18).
	idle := &quota.Codex{
		Primary:   &quota.Window{UsedPercent: 0, WindowMins: 300, ResetsAt: now.Add(5 * time.Hour)},
		Secondary: &quota.Window{UsedPercent: 0, WindowMins: 10080, ResetsAt: now.Add(7 * 24 * time.Hour)},
	}
	q := codexQuota(idle, now)
	if q.FiveHour.Active || q.FiveHour.ResetsAt != nil {
		t.Fatalf("idle five-hour window = %+v", q.FiveHour)
	}
	if q.Weekly.ResetsAt != nil {
		t.Fatal("unused weekly window has a virtual reset time")
	}

	running := &quota.Codex{
		Primary:          &quota.Window{UsedPercent: 23, WindowMins: 300, ResetsAt: now.Add(98 * time.Minute)},
		Secondary:        &quota.Window{UsedPercent: 11, WindowMins: 10080, ResetsAt: now.Add(50 * time.Hour)},
		LimitReachedType: "primary",
	}
	q = codexQuota(running, now)
	if !q.FiveHour.Active || q.FiveHour.ResetsAt == nil || *q.FiveHour.UsedPercent != 23 {
		t.Fatalf("running five-hour window = %+v", q.FiveHour)
	}
	if q.Weekly.ResetsAt == nil || !q.LimitReached || q.Source != "live" {
		t.Fatalf("quota = %+v", q)
	}
}

func TestCachedQuota(t *testing.T) {
	u := 0.12
	ws := []wake.Window{
		{Kind: "five_hour", Utilization: &u, ResetsAt: now.Add(2 * time.Hour), Status: "allowed"},
		{Kind: "seven_day", ResetsAt: now.Add(3 * 24 * time.Hour)},
	}
	q := cachedQuota(ws, now.Add(-3*time.Hour), now)
	if q.Source != "cached" || !q.FiveHour.Active || *q.FiveHour.UsedPercent < 11.99 || *q.FiveHour.UsedPercent > 12.01 {
		t.Fatalf("five-hour = %+v", q.FiveHour)
	}
	if q.Weekly == nil || q.Weekly.UsedPercent != nil {
		t.Fatalf("weekly = %+v", q.Weekly)
	}
	// After the window has reset, the old utilization no longer applies.
	later := cachedQuota(ws, now.Add(-3*time.Hour), now.Add(3*time.Hour))
	if later.FiveHour.Active || later.FiveHour.UsedPercent != nil {
		t.Fatalf("after reset = %+v", later.FiveHour)
	}
	if cachedQuota(nil, now, now) != nil || cachedQuota([]wake.Window{{Kind: "five_hour"}}, now, now) != nil {
		t.Fatal("no usable windows should give nil")
	}
}

func TestSlotNotice(t *testing.T) {
	claude := scheduler.Plans[0]
	at := time.Date(2026, 11, 25, 6, 30, 0, 0, time.Local)
	failed := store.Slot{Plan: claude.ID, At: at, State: store.SlotFailed, Attempts: 3}

	title, body, ok := slotNotice(claude, failed, wake.Result{Outcome: wake.OutcomeNotLoggedIn})
	if !ok || !strings.Contains(title, "未登录") || !strings.Contains(body, "claude auth login") || !strings.Contains(body, "06:30") {
		t.Fatalf("not logged in: %q %q %v", title, body, ok)
	}
	if _, body, ok := slotNotice(claude, failed, wake.Result{Outcome: wake.OutcomeNetworkError, Message: strings.Repeat("x", 300)}); !ok || !strings.Contains(body, "3 次") || len([]rune(body)) > 160 {
		t.Fatalf("network: %q %v", body, ok)
	}
	for _, c := range []struct {
		state store.SlotState
		out   wake.Outcome
	}{
		{store.SlotFailed, wake.OutcomeQuotaExhausted},
		{store.SlotPending, wake.OutcomeOffline},
		{store.SlotSkipped, wake.OutcomeSkipped},
		{store.SlotDone, wake.OutcomeOK},
	} {
		sl := failed
		sl.State = c.state
		if _, _, ok := slotNotice(claude, sl, wake.Result{Outcome: c.out}); ok {
			t.Errorf("%s/%s should stay quiet", c.state, c.out)
		}
	}
}
