package detect

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ethan2047rr/kikri/internal/proc"
)

// AuthInfo is the result of a local, offline login check. Kikri never reads
// credential files; it only runs the CLI's own status command.
type AuthInfo struct {
	LoggedIn  bool   `json:"loggedIn"`
	Method    string `json:"method,omitempty"`    // e.g. "claude.ai", "ChatGPT"
	Plan      string `json:"plan,omitempty"`      // e.g. "pro", "max", "plus"
	ExpiresAt string `json:"expiresAt,omitempty"` // RFC 3339, when the CLI reports it
	Detail    string `json:"detail,omitempty"`    // one-line human-readable summary
}

// parseClaudeAuth reads `claude auth status --json`
// (docs/spike/m0-step4 T2/T5). Only the fields Kikri needs are kept; account
// identifiers such as e-mail are deliberately ignored.
func parseClaudeAuth(res proc.Result) (*AuthInfo, error) {
	out := res.Stdout
	if i := bytes.IndexByte(out, '{'); i >= 0 {
		out = out[i:]
	}
	var v struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
		PlanTier         string `json:"planTier"`
		LoginExpiresAt   string `json:"loginExpiresAt"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, errors.New("unexpected output from `claude auth status --json`: " + firstLine(res))
	}
	info := &AuthInfo{
		LoggedIn:  v.LoggedIn,
		Method:    v.AuthMethod,
		Plan:      v.SubscriptionType,
		ExpiresAt: v.LoginExpiresAt,
	}
	if info.Plan == "" {
		info.Plan = v.PlanTier
	}
	if info.Method == "none" {
		info.Method = ""
	}
	if info.LoggedIn {
		info.Detail = "Logged in"
		if info.Method != "" {
			info.Detail += " via " + info.Method
		}
	} else {
		info.Detail = "Not logged in"
	}
	return info, nil
}

// parseCodexAuth reads `codex login status`: exit code 0 means logged in,
// 1 means not logged in (docs/spike/m0-step4 T5).
func parseCodexAuth(res proc.Result) (*AuthInfo, error) {
	line := firstLine(res)
	switch res.ExitCode {
	case 0:
		info := &AuthInfo{LoggedIn: true, Detail: line}
		if m, ok := strings.CutPrefix(line, "Logged in using "); ok {
			info.Method = strings.TrimSpace(m)
		}
		return info, nil
	case 1:
		return &AuthInfo{LoggedIn: false, Detail: line}, nil
	default:
		return nil, errors.New("unexpected result from `codex login status`: " + line)
	}
}

// firstLine returns the first non-empty line of stdout, else of stderr.
func firstLine(res proc.Result) string {
	for _, b := range [][]byte{res.Stdout, res.Stderr} {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				return l
			}
		}
	}
	return ""
}
