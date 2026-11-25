package wake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ethan2047rr/kikri/internal/appdir"
)

// Claude wakes Claude Code with every user customisation switched off
// (docs/spike/m0-step2, variant B: 633 input tokens, 3.6s).
type Claude struct{}

func (Claude) ID() string { return "claude" }

func (Claude) Hosts() []string { return []string{"api.anthropic.com"} }

func (Claude) Command(_ appdir.Layout) (args, env []string) {
	args = []string{
		"-p", "ok",
		// Disables CLAUDE.md, hooks, plugins, skills and MCP; OAuth still works.
		// Never --bare: it only accepts API keys, not the subscription login.
		"--safe-mode",
		"--tools", "",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--system-prompt", "Reply with: ok",
		"--model", "haiku", "--effort", "low",
		"--no-session-persistence",
		"--output-format", "stream-json", "--verbose",
	}
	// Without these Claude Code retries forever when offline (m0-step4 T4).
	env = []string{"CLAUDE_CODE_MAX_RETRIES=1", "API_TIMEOUT_MS=15000"}
	return args, env
}

type claudeEvent struct {
	Type           string          `json:"type"`
	IsError        *bool           `json:"is_error"`
	Result         string          `json:"result"`
	APIErrorStatus json.RawMessage `json:"api_error_status"`
	Usage          *struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
	RateLimitInfo *struct {
		Status         string   `json:"status"`
		RateLimitType  string   `json:"rateLimitType"`
		ResetsAt       int64    `json:"resetsAt"`
		Utilization    *float64 `json:"utilization"`
		UnifiedWindows map[string]struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    int64    `json:"resetsAt"`
			Status      string   `json:"status"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
	// "error" and "message" are strings on some events and objects on others
	// (assistant events carry the whole API message), so decode lazily.
	Error   json.RawMessage `json:"error"`
	Message json.RawMessage `json:"message"`
}

// rawString returns raw as a string if it is a JSON string, else "".
func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// Parse reads `--output-format stream-json` output. Success is judged by
// is_error only: on errors subtype is still "success" (m0-step4).
func (Claude) Parse(stdout []byte) parsed {
	var p parsed
	windows := map[string]Window{}
	rejected := false

	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev claudeEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "rate_limit_event":
			ri := ev.RateLimitInfo
			if ri == nil {
				continue
			}
			if ri.Status != "" && !strings.HasPrefix(ri.Status, "allowed") {
				rejected = true
			}
			for kind, w := range ri.UnifiedWindows {
				windows[kind] = Window{Kind: kind, Utilization: w.Utilization, ResetsAt: unix(w.ResetsAt), Status: w.Status}
				if w.Status != "" && !strings.HasPrefix(w.Status, "allowed") {
					rejected = true
				}
			}
			if ri.RateLimitType != "" && ri.ResetsAt > 0 {
				if _, ok := windows[ri.RateLimitType]; !ok {
					windows[ri.RateLimitType] = Window{Kind: ri.RateLimitType, Utilization: ri.Utilization, ResetsAt: unix(ri.ResetsAt), Status: ri.Status}
				}
			}
		case "result":
			p.terminal = true
			if u := ev.Usage; u != nil {
				p.usage = &Usage{
					InputTokens:       u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
					CachedInputTokens: u.CacheReadInputTokens,
					CacheWriteTokens:  u.CacheCreationInputTokens,
					OutputTokens:      u.OutputTokens,
				}
			}
			if ev.IsError != nil && !*ev.IsError {
				p.outcome, p.reply, p.message = OutcomeOK, strings.TrimSpace(ev.Result), ""
				continue
			}
			p.message = strings.TrimSpace(ev.Result)
			if p.message == "" {
				p.message = "Claude Code reported an error without a message"
			}
			p.outcome = classifyStatus(ev.APIErrorStatus)
			if p.outcome == "" {
				p.outcome = classifyMessage(p.message)
			}
		default:
			if msg := firstNonEmpty(rawString(ev.Error), rawString(ev.Message)); msg != "" && strings.Contains(ev.Type, "error") {
				p.lastErr = msg
			}
		}
	}
	if p.terminal && p.outcome != OutcomeOK && rejected && p.outcome != OutcomeNotLoggedIn {
		p.outcome = OutcomeQuotaExhausted
	}
	p.windows = sortedWindows(windows)
	return p
}

// classifyStatus maps api_error_status (an HTTP status, null when unset).
func classifyStatus(raw json.RawMessage) Outcome {
	var code int
	if len(raw) == 0 || json.Unmarshal(raw, &code) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil || s == "" {
			return ""
		}
		if _, err := fmt.Sscan(s, &code); err != nil {
			return ""
		}
	}
	switch {
	case code == 401 || code == 403:
		return OutcomeNotLoggedIn
	case code == 429:
		return OutcomeQuotaExhausted
	}
	return ""
}

func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func sortedWindows(m map[string]Window) []Window {
	if len(m) == 0 {
		return nil
	}
	out := make([]Window, 0, len(m))
	for _, w := range m {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResetsAt.Before(out[j].ResetsAt) })
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
