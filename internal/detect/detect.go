// Package detect finds the AI coding CLIs on this machine and checks their
// login state locally (PRD F1).
package detect

import (
	"context"
	"errors"
	"fmt"
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
	auth, err := checkAuth(ctx, t, st.Path, env, opts.Dir)
	if err != nil {
		st.Error = err.Error()
	}
	st.Auth = auth
	return st
}

// ErrNoAuthCheck is returned by CheckAuth for tools without a login check.
var ErrNoAuthCheck = errors.New("detect: no login check for this tool")

// CheckAuth runs one tool's local login check (no network request to the
// model API; PRD F2.6 step 1).
func CheckAuth(ctx context.Context, toolID, path string, env []string, dir string) (*AuthInfo, error) {
	for _, t := range Tools {
		if t.ID == toolID {
			if t.parseAuth == nil {
				return nil, ErrNoAuthCheck
			}
			return checkAuth(ctx, t, path, env, dir)
		}
	}
	return nil, fmt.Errorf("detect: unknown tool %q", toolID)
}

func checkAuth(ctx context.Context, t Tool, path string, env []string, dir string) (*AuthInfo, error) {
	res, err := proc.Run(ctx, proc.Spec{Path: path, Args: t.AuthArgs, Dir: dir, Env: env, Timeout: authTimeout})
	switch {
	case err != nil:
		return nil, err
	case res.TimedOut:
		return nil, errors.New("login check timed out")
	}
	return t.parseAuth(res)
}
