// Package wake sends the minimal request that starts a plan's quota window,
// through the user's own CLI, and classifies what happened (PRD F2, F4.3).
package wake

import (
	"strings"
	"time"
)

// Outcome is the classified result of one wake attempt.
type Outcome string

const (
	OutcomeOK             Outcome = "ok"
	OutcomeQuotaExhausted Outcome = "quota_exhausted" // do not retry
	OutcomeNotLoggedIn    Outcome = "not_logged_in"   // tray turns red, user must log in
	OutcomeNetworkError   Outcome = "network_error"   // request sent, connection failed
	OutcomeTimeout        Outcome = "timeout"         // killed after the hard timeout
	OutcomeOffline        Outcome = "offline"         // pre-check: endpoint unreachable, nothing sent
	OutcomeSkipped        Outcome = "skipped"         // Smart Skip: a window is already running
	OutcomeBusy           Outcome = "busy"            // another wake for this tool is in progress
	OutcomeFailed         Outcome = "failed"          // anything Kikri could not classify
)

// Sent reports whether the attempt reached the CLI's model request.
func (o Outcome) Sent() bool {
	switch o {
	case OutcomeOffline, OutcomeSkipped, OutcomeBusy:
		return false
	}
	return true
}

// Retryable reports whether the make-up logic (F4.1) may try again later.
func (o Outcome) Retryable() bool {
	switch o {
	case OutcomeNetworkError, OutcomeTimeout, OutcomeOffline:
		return true
	}
	return false
}

// Usage is the token accounting the CLI reported.
type Usage struct {
	InputTokens       int `json:"inputTokens"` // all input tokens, cached ones included
	CachedInputTokens int `json:"cachedInputTokens,omitempty"`
	CacheWriteTokens  int `json:"cacheWriteTokens,omitempty"`
	OutputTokens      int `json:"outputTokens"`
}

// Window is one quota window as reported by the CLI.
type Window struct {
	Kind        string    `json:"kind"`                  // "five_hour", "seven_day", ...
	Utilization *float64  `json:"utilization,omitempty"` // 0..1 when known
	ResetsAt    time.Time `json:"resetsAt"`
	Status      string    `json:"status,omitempty"` // CLI's own wording, e.g. "allowed"
}

// Result is the record of one wake attempt; it is what the log stores.
type Result struct {
	Tool       string    `json:"tool"`
	Outcome    Outcome   `json:"outcome"`
	Message    string    `json:"message,omitempty"` // error text from the CLI or from Kikri
	Reply      string    `json:"reply,omitempty"`   // model reply on success
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Usage      *Usage    `json:"usage,omitempty"`
	Windows    []Window  `json:"windows,omitempty"`
	ExitCode   *int      `json:"exitCode,omitempty"`
	Stderr     string    `json:"stderr,omitempty"` // tail, for diagnosing failures
}

// parsed is what a Runner extracts from the CLI's stdout.
type parsed struct {
	terminal bool // a final event (result / turn.completed / turn.failed) was seen
	outcome  Outcome
	message  string
	reply    string
	usage    *Usage
	windows  []Window
	lastErr  string // last non-final error message, used when nothing final arrived
}

// classifyMessage maps CLI error text to an Outcome, using the signatures
// observed in docs/spike/m0-step4. Login problems win over everything, then
// quota, then network.
func classifyMessage(msg string) Outcome {
	m := strings.ToLower(msg)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(m, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("not logged in", "please run /login", "401", "unauthorized", "invalid api key",
		"authentication", "token expired", "oauth token", "codex login"):
		return OutcomeNotLoggedIn
	case has("usage limit", "rate limit", "rate_limit", "quota", "limit reached", "hit your limit",
		"limit exceeded", "429", "too many requests", "out of credits", "insufficient credits"):
		return OutcomeQuotaExhausted
	case has("connection refused", "econnrefused", "connection failed", "error sending request",
		"connection reset", "econnreset", "enotfound", "etimedout", "could not resolve",
		"dns", "network", "unable to connect", "timed out", "timeout", "tls handshake", "socket hang up"):
		return OutcomeNetworkError
	}
	return OutcomeFailed
}

// tail returns at most n bytes from the end of b, as trimmed text.
func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.TrimSpace(string(b))
}
