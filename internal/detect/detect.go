// Package detect finds the AI coding CLIs on this machine and checks their
// login state locally (PRD F1).
package detect

import (
	"context"
	"sync"
	"time"

	"github.com/ethan2047rr/kikri/internal/proc"
)

// Tool describes a CLI Kikri knows how to look for.
type Tool struct {
	ID          string
	Name        string
	Binary      string
	VersionArgs []string
	// AuthArgs and parseAuth are nil for tools whose login check is not
	// implemented yet; those only report path and version.
	AuthArgs  []string
	parseAuth func(proc.Result) (*AuthInfo, error)
}

// Tools is the built-in list. Claude Code and Codex are fully supported in M1;
// the others are detected so the UI can show them, with runners to follow.
var Tools = []Tool{
	{
		ID: "claude", Name: "Claude Code", Binary: "claude",
		VersionArgs: []string{"--version"},
		AuthArgs:    []string{"auth", "status", "--json"},
		parseAuth:   parseClaudeAuth,
	},
	{
		ID: "codex", Name: "Codex CLI", Binary: "codex",
		VersionArgs: []string{"--version"},
		AuthArgs:    []string{"login", "status"},
		parseAuth:   parseCodexAuth,
	},
	{ID: "opencode", Name: "OpenCode", Binary: "opencode", VersionArgs: []string{"--version"}},
	{ID: "copilot", Name: "GitHub Copilot CLI", Binary: "copilot", VersionArgs: []string{"--version"}},
	// `agy version` fails without a TTY; `--version` does not (docs/spike/m0-step3).
	{ID: "agy", Name: "Antigravity CLI", Binary: "agy", VersionArgs: []string{"--version"}},
}

// Status is the detection result for one tool.
type Status struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Found   bool      `json:"found"`
	Path    string    `json:"path,omitempty"`
	Source  Source    `json:"source,omitempty"`
	Version string    `json:"version,omitempty"`
	Auth    *AuthInfo `json:"auth,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// Report is the result of a full detection pass.
type Report struct {
	CheckedAt  time.Time  `json:"checkedAt"`
	SearchPath SearchPath `json:"searchPath"`
	Tools      []Status   `json:"tools"`
}

// Options tunes a detection pass.
type Options struct {
	// Overrides maps tool ID to a user-chosen executable path.
	Overrides map[string]string
	// Dir is the working directory for the CLI calls (Kikri's sandbox).
	Dir string
}

const (
	versionTimeout = 10 * time.Second
	authTimeout    = 15 * time.Second
)

// Run detects all built-in tools concurrently.
func Run(ctx context.Context, opts Options) Report {
	sp := BuildSearchPath(ctx)
	rep := Report{CheckedAt: time.Now(), SearchPath: sp, Tools: make([]Status, len(Tools))}
	env := sp.Env()

	var wg sync.WaitGroup
	for i, t := range Tools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep.Tools[i] = detectTool(ctx, t, sp, env, opts)
		}()
	}
	wg.Wait()
	return rep
}

func detectTool(ctx context.Context, t Tool, sp SearchPath, env []string, opts Options) Status {
	st := Status{ID: t.ID, Name: t.Name}

	if p := opts.Overrides[t.ID]; p != "" {
		if !isExecutable(p) {
			st.Error = "configured path is not an executable file: " + p
			return st
		}
		st.Path, st.Source = p, SourceOverride
	} else if p, src, ok := sp.Find(t.Binary); ok {
		st.Path, st.Source = p, src
	} else {
		return st
	}
	st.Found = true

	run := func(args []string, timeout time.Duration) (proc.Result, error) {
		return proc.Run(ctx, proc.Spec{Path: st.Path, Args: args, Dir: opts.Dir, Env: env, Timeout: timeout})
	}

	if res, err := run(t.VersionArgs, versionTimeout); err != nil {
		st.Error = err.Error()
		return st
	} else if res.TimedOut {
		st.Error = "`" + t.Binary + " --version` timed out"
		return st
	} else {
		st.Version = firstLine(res)
	}

	if t.parseAuth == nil {
		return st
	}
	res, err := run(t.AuthArgs, authTimeout)
	switch {
	case err != nil:
		st.Error = err.Error()
	case res.TimedOut:
		st.Error = "login check timed out"
	default:
		if st.Auth, err = t.parseAuth(res); err != nil {
			st.Error = err.Error()
		}
	}
	return st
}
