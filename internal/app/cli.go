package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/detect"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// WakeOnce runs one wake for toolID outside the GUI (`kikri -wake`). With
// dryRun it only returns the command line that would be executed.
func WakeOnce(ctx context.Context, toolID string, dryRun bool) (any, error) {
	r, ok := wake.Runners[toolID]
	if !ok {
		return nil, fmt.Errorf("no wake runner for %q (available: claude, codex)", toolID)
	}
	layout, err := appdir.Ensure()
	if err != nil {
		return nil, err
	}
	sp := detect.BuildSearchPath(ctx)
	bin, _, found := sp.Find(binaryFor(toolID))
	if !found {
		return nil, fmt.Errorf("%s not found in PATH or the usual install locations", binaryFor(toolID))
	}
	env := sp.Env()

	if dryRun {
		args, extra := r.Command(layout)
		parts := append(append([]string{}, extra...), bin)
		for _, a := range args {
			parts = append(parts, shellQuote(a))
		}
		out := map[string]any{
			"command": strings.Join(parts, " ") + " < /dev/null",
			"cwd":     layout.Sandbox,
			"proxy":   sp.Proxy,
		}
		if toolID == "codex" {
			if p, n, ok := wake.CodexGlobalAgents(); ok {
				out["globalAgentsMd"] = map[string]any{"path": p, "bytes": n, "extraTokens": wake.EstimateTokens(n)}
			}
		}
		return out, nil
	}

	return wake.Wake(ctx, r, wake.Options{
		Layout: layout,
		Binary: bin,
		Env:    env,
		CheckLogin: func(ctx context.Context) (bool, error) {
			a, err := detect.CheckAuth(ctx, toolID, bin, env, layout.Sandbox)
			if err != nil {
				return false, err
			}
			return a.LoggedIn, nil
		},
	}), nil
}

func binaryFor(toolID string) string {
	for _, t := range detect.Tools {
		if t.ID == toolID {
			return t.Binary
		}
	}
	return toolID
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_./:=@,+-]+$`)

func shellQuote(s string) string {
	if plainArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
