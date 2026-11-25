// Package quota reads a plan's quota windows without spending any quota.
package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ethan2047rr/kikri/internal/proc"
)

// Window is one Codex rate-limit window.
type Window struct {
	UsedPercent float64   `json:"usedPercent"`
	WindowMins  int       `json:"windowMins"`
	ResetsAt    time.Time `json:"resetsAt"`
}

// Active reports whether a real window is running. With no active window
// Codex answers with a virtual resetsAt of "now + window" that moves with
// every query (V18), so anything within 2 minutes of a full window counts as
// "no window".
func (w Window) Active(now time.Time) bool {
	full := time.Duration(w.WindowMins) * time.Minute
	return w.ResetsAt.Sub(now) < full-2*time.Minute
}

// Codex is the answer of `account/rateLimits/read`.
type Codex struct {
	Primary          *Window `json:"primary,omitempty"`   // the 5-hour window; nil if the plan has none
	Secondary        *Window `json:"secondary,omitempty"` // the weekly window
	PlanType         string  `json:"planType,omitempty"`
	LimitReachedType string  `json:"limitReachedType,omitempty"` // non-empty when a limit is hit
}

type rpcWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

func (w *rpcWindow) toWindow() *Window {
	if w == nil {
		return nil
	}
	return &Window{UsedPercent: w.UsedPercent, WindowMins: w.WindowDurationMins, ResetsAt: time.Unix(w.ResetsAt, 0)}
}

type rpcMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ReadCodex asks `codex app-server` for the account's rate limits. The query
// is read-only: it neither spends quota nor opens a window (V18). Only the
// fields listed in Codex are kept; account identifiers are discarded.
func ReadCodex(ctx context.Context, bin string, env []string, dir string) (*Codex, error) {
	var out *Codex
	err := proc.Converse(ctx, proc.Spec{Path: bin, Args: []string{"app-server"}, Dir: dir, Env: env, Timeout: 20 * time.Second},
		func(stdin io.Writer, stdout io.Reader) error {
			enc := json.NewEncoder(stdin)
			sc := bufio.NewScanner(stdout)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			call := func(id int, method string, params any) (json.RawMessage, error) {
				if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
					return nil, err
				}
				for sc.Scan() {
					var m rpcMessage
					if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil || *m.ID != id {
						continue // notifications and unrelated lines
					}
					if m.Error != nil {
						return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
					}
					return m.Result, nil
				}
				if err := sc.Err(); err != nil {
					return nil, err
				}
				return nil, errors.New("codex app-server closed the connection")
			}

			if _, err := call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "kikri", "version": "1"}}); err != nil {
				return err
			}
			if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
				return err
			}
			raw, err := call(2, "account/rateLimits/read", map[string]any{})
			if err != nil {
				return err
			}
			out, err = parseCodex(raw)
			return err
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func parseCodex(raw json.RawMessage) (*Codex, error) {
	var r struct {
		RateLimits *struct {
			Primary              *rpcWindow `json:"primary"`
			Secondary            *rpcWindow `json:"secondary"`
			PlanType             string     `json:"planType"`
			RateLimitReachedType *string    `json:"rateLimitReachedType"`
		} `json:"rateLimits"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode rate limits: %w", err)
	}
	if r.RateLimits == nil {
		return nil, errors.New("rate limits missing from the answer (not logged in?)")
	}
	c := &Codex{
		Primary:   r.RateLimits.Primary.toWindow(),
		Secondary: r.RateLimits.Secondary.toWindow(),
		PlanType:  r.RateLimits.PlanType,
	}
	if t := r.RateLimits.RateLimitReachedType; t != nil {
		c.LimitReachedType = *t
	}
	return c, nil
}
