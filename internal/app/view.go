package app

import (
	"time"

	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/statusline"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// QuotaWindow is one usage window as the status card shows it.
type QuotaWindow struct {
	UsedPercent *float64 `json:"usedPercent,omitempty"`
	// ResetsAt is only set when it is real: for Codex with no window
	// running the query returns a virtual "now + 5h" (V18), which is dropped.
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
	Active   bool       `json:"active"`
}

// PlanQuota is what is known about a plan's windows.
type PlanQuota struct {
	// Source is "live" (official read-only query, Codex), "cached" (from
	// the last successful wake, Claude, which has no read-only query) or
	// "statusline" (Claude Code's status line, D24).
	Source       string       `json:"source,omitempty"`
	At           *time.Time   `json:"at,omitempty"` // when the data was read or reported
	FiveHour     *QuotaWindow `json:"fiveHour,omitempty"`
	Weekly       *QuotaWindow `json:"weekly,omitempty"`
	LimitReached bool         `json:"limitReached,omitempty"`
	PlanType     string       `json:"planType,omitempty"` // Codex: "plus", "pro", ...
	Error        string       `json:"error,omitempty"`
	// LastUsed is the newest session file's modification time (Smart Skip
	// basis B). Within the window length it means "active" even when the
	// reset time is unknown (F3.4: show 活跃中, not a guessed countdown).
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

// Stats is the footer line ("本月已提前唤醒 18 次").
type Stats struct {
	MonthWakes   int `json:"monthWakes"`
	MonthSkipped int `json:"monthSkipped"`
}

// AgentsMd warns about a large global ~/.codex/AGENTS.md (F5.1).
type AgentsMd struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Tokens int64  `json:"tokens"`
}

func pct(v float64) *float64 { return &v }

// codexQuota converts the official Codex answer.
func codexQuota(q *quota.Codex, at time.Time) *PlanQuota {
	pq := &PlanQuota{Source: "live", At: &at, LimitReached: q.LimitReachedType != "", PlanType: q.PlanType}
	if w := q.Primary; w != nil {
		qw := &QuotaWindow{UsedPercent: pct(w.UsedPercent), Active: w.Active(at)}
		if qw.Active {
			r := w.ResetsAt
			qw.ResetsAt = &r
		}
		pq.FiveHour = qw
	}
	if w := q.Secondary; w != nil {
		r := w.ResetsAt
		// A weekly window that nothing has used yet is just as virtual.
		qw := &QuotaWindow{UsedPercent: pct(w.UsedPercent), Active: w.Active(at)}
		if qw.Active || w.UsedPercent > 0 {
			qw.ResetsAt = &r
		}
		pq.Weekly = qw
	}
	return pq
}

// cachedQuota converts the windows reported by the last successful wake.
// Claude reports utilization as 0..1.
// statuslineQuota converts Claude Code's status line report (D24).
func statuslineQuota(d *statusline.Data, now time.Time) *PlanQuota {
	at := d.At
	pq := &PlanQuota{Source: "statusline", At: &at}
	conv := func(w *statusline.Window) *QuotaWindow {
		if w == nil {
			return nil
		}
		r := w.ResetsAt
		qw := &QuotaWindow{ResetsAt: &r, Active: r.After(now)}
		if w.UsedPercent != nil && qw.Active {
			qw.UsedPercent = pct(*w.UsedPercent)
		}
		return qw
	}
	pq.FiveHour, pq.Weekly = conv(d.FiveHour), conv(d.SevenDay)
	if pq.FiveHour == nil && pq.Weekly == nil {
		return nil
	}
	return pq
}

func cachedQuota(ws []wake.Window, at, now time.Time) *PlanQuota {
	if len(ws) == 0 {
		return nil
	}
	pq := &PlanQuota{Source: "cached", At: &at}
	for _, w := range ws {
		if w.ResetsAt.IsZero() {
			continue
		}
		r := w.ResetsAt
		qw := &QuotaWindow{ResetsAt: &r, Active: r.After(now)}
		if w.Utilization != nil && qw.Active {
			// Utilization is as of the wake; once the window has reset it
			// no longer applies.
			qw.UsedPercent = pct(*w.Utilization * 100)
		}
		switch w.Kind {
		case "five_hour":
			pq.FiveHour = qw
		case "seven_day":
			pq.Weekly = qw
		}
		if w.Status == "rejected" && qw.Active {
			pq.LimitReached = true
		}
	}
	if pq.FiveHour == nil && pq.Weekly == nil {
		return nil
	}
	return pq
}
