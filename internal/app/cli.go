package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/schedule"
	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// The functions here back the command-line flags (no GUI). They share the
// GUI's database, so a running Kikri sees their changes on its next pass.

// WakeOnce wakes one plan or tool (`kikri -wake claude|codex|chatgpt`) and
// logs it like the tray's "wake now". With dryRun it only returns the command
// line that would be executed.
func WakeOnce(ctx context.Context, name string, dryRun bool) (any, error) {
	planID, toolID, err := planAndTool(name)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return dryRunCommand(ctx, toolID)
	}
	svc, err := openService()
	if err != nil {
		return nil, err
	}
	defer svc.Close()
	return svc.WakeNow(ctx, planID)
}

// SetPlanFromSpec handles `kikri -plan claude=early-bird|three|06:30,11:35|off`.
// Custom times and presets run on workdays; days are editable in the UI (M4).
func SetPlanFromSpec(spec string) (any, error) {
	name, value, ok := strings.Cut(spec, "=")
	if !ok {
		return nil, fmt.Errorf("want PLAN=early-bird|three|HH:MM,HH:MM|off, got %q", spec)
	}
	planID, _, err := planAndTool(name)
	if err != nil {
		return nil, err
	}
	svc, err := openService()
	if err != nil {
		return nil, err
	}
	defer svc.Close()

	cur, err := svc.engine.Settings()
	if err != nil {
		return nil, err
	}
	p := cur.Plans[planID]
	switch value {
	case "off":
		p.Enabled = false
	case schedule.PresetEarlyBird, schedule.PresetThree:
		p.Enabled, p.Preset, p.Times = true, value, nil
	default:
		var times []schedule.Clock
		for _, part := range strings.Split(value, ",") {
			c, err := schedule.ParseClock(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			times = append(times, c)
		}
		p.Enabled, p.Preset, p.Times = true, schedule.PresetCustom, times
	}
	if p.Weekdays == (schedule.Weekdays{}) {
		p.Weekdays = schedule.Workdays
	}
	if err := svc.SetPlan(planID, p); err != nil {
		return nil, err
	}
	return svc.engine.Status()
}

// StatusReport handles `kikri -status`: schedules, next slots, usage (the
// read-only Codex query and Claude's cached windows) and the log.
func StatusReport() (any, error) {
	svc, err := openService()
	if err != nil {
		return nil, err
	}
	defer svc.Close()
	plans, err := svc.engine.Status()
	if err != nil {
		return nil, err
	}
	settings, err := svc.engine.Settings()
	if err != nil {
		return nil, err
	}
	log, err := svc.Log("", 20)
	if err != nil {
		return nil, err
	}
	svc.RefreshQuota(true)
	return map[string]any{"plans": plans, "cutoff": settings.Cutoff, "useActivity": settings.UseActivity,
		"pausedUntil": settings.PausedUntil, "usage": svc.Snapshot().Quota, "log": log}, nil
}

func openService() (*Service, error) {
	svc := NewService("cli", nil)
	svc.Prepare()
	snap := svc.Snapshot()
	if snap.AppDirError != "" {
		return nil, fmt.Errorf("data directory: %s", snap.AppDirError)
	}
	if snap.StoreError != "" {
		return nil, fmt.Errorf("database: %s", snap.StoreError)
	}
	return svc, nil
}

// planAndTool accepts a plan ID (claude, chatgpt) or a tool ID (codex).
func planAndTool(name string) (planID, toolID string, err error) {
	for _, p := range scheduler.Plans {
		if p.ID == name || p.Tool == name {
			return p.ID, p.Tool, nil
		}
	}
	return "", "", fmt.Errorf("unknown plan %q (available: claude, chatgpt or codex)", name)
}

func dryRunCommand(ctx context.Context, toolID string) (any, error) {
	r, ok := wake.Runners[toolID]
	if !ok {
		return nil, fmt.Errorf("no wake runner for %q", toolID)
	}
	layout, err := appdir.Ensure()
	if err != nil {
		return nil, err
	}
	bin, env, err := resolveTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	args, extra := r.Command(layout)
	parts := append(append([]string{}, extra...), bin)
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	out := map[string]any{
		"command": strings.Join(parts, " ") + " < /dev/null",
		"cwd":     layout.Sandbox,
		"proxy":   displayProxyFromEnv(env),
	}
	if toolID == "codex" {
		if p, n, ok := wake.CodexGlobalAgents(); ok {
			out["globalAgentsMd"] = map[string]any{"path": p, "bytes": n, "extraTokens": wake.EstimateTokens(n)}
		}
	}
	return out, nil
}

// displayProxyFromEnv reports whether a proxy is set, without its value
// (which may contain a password).
func displayProxyFromEnv(env []string) string {
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if l := strings.ToLower(k); l == "https_proxy" || l == "all_proxy" {
			return "set (" + k + ")"
		}
	}
	return ""
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_./:=@,+-]+$`)

func shellQuote(s string) string {
	if plainArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
