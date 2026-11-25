package app

import (
	"context"
	"fmt"
	"time"

	"github.com/ethan2047rr/kikri/internal/activity"
	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/detect"
	"github.com/ethan2047rr/kikri/internal/quota"
	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// resolveTool finds a CLI and the environment to run it with. It runs at
// every wake rather than reusing the last detection, so an updated or
// reinstalled CLI is picked up.
func resolveTool(ctx context.Context, toolID string) (bin string, env []string, err error) {
	sp := detect.BuildSearchPath(ctx)
	bin, _, ok := sp.Find(binaryFor(toolID))
	if !ok {
		return "", nil, fmt.Errorf("%s not found in PATH or the usual install locations", binaryFor(toolID))
	}
	return bin, sp.Env(), nil
}

func binaryFor(toolID string) string {
	for _, t := range detect.Tools {
		if t.ID == toolID {
			return t.Binary
		}
	}
	return toolID
}

// runWake performs the full F2.6 flow for one tool.
func runWake(ctx context.Context, layout appdir.Layout, toolID string, skip func(context.Context) (string, bool)) wake.Result {
	r, ok := wake.Runners[toolID]
	if !ok {
		return wake.Result{Tool: toolID, Outcome: wake.OutcomeFailed, Message: "no wake runner for " + toolID, StartedAt: time.Now()}
	}
	bin, env, err := resolveTool(ctx, toolID)
	if err != nil {
		return wake.Result{Tool: toolID, Outcome: wake.OutcomeFailed, Message: err.Error(), StartedAt: time.Now()}
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
		Skip: skip,
	})
}

// newEngine connects the scheduler to the real CLIs, quota query and
// session activity.
func newEngine(layout appdir.Layout, st *store.Store, onChange func()) *scheduler.Engine {
	return scheduler.New(scheduler.Deps{
		Store: st,
		Wake: func(ctx context.Context, plan scheduler.PlanDef, skip func(context.Context) (string, bool)) wake.Result {
			return runWake(ctx, layout, plan.Tool, skip)
		},
		CodexQuota: func(ctx context.Context) (*quota.Codex, error) {
			bin, env, err := resolveTool(ctx, "codex")
			if err != nil {
				return nil, err
			}
			return quota.ReadCodex(ctx, bin, env, layout.Sandbox)
		},
		LastActivity: func(plan scheduler.PlanDef) time.Time {
			switch plan.Tool {
			case "claude":
				return activity.Latest(activity.ClaudeRoots()...)
			case "codex":
				return activity.Latest(activity.CodexRoots()...)
			}
			return time.Time{}
		},
		OnChange: onChange,
	})
}
