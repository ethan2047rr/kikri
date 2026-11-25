// Command kikri is a menu bar / system tray app that sends a tiny request to
// AI coding CLIs on a schedule, so their 5-hour quota windows start early.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ethan2047rr/kikri/internal/app"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	diagnose := flag.Bool("diagnose", false, "print environment detection as JSON and exit (no GUI)")
	showVersion := flag.Bool("version", false, "print version and exit")
	wakeTool := flag.String("wake", "", "send one real wake request for `plan` (claude|chatgpt, or the tool name codex) and print the result as JSON; this starts its 5-hour window")
	dryRun := flag.Bool("dry-run", false, "with -wake: print the command instead of running it")
	planSpec := flag.String("plan", "", "set a schedule: `PLAN=early-bird|three|HH:MM,HH:MM|off`, e.g. claude=early-bird")
	status := flag.Bool("status", false, "print schedules, next wakes and the recent log as JSON")
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("kikri", version)
	case *diagnose:
		svc := app.NewService(version, nil)
		svc.Prepare()
		printJSON(svc.Refresh(context.Background()))
		svc.Close()
	case *wakeTool != "":
		printOrFail(app.WakeOnce(context.Background(), *wakeTool, *dryRun))
	case *planSpec != "":
		printOrFail(app.SetPlanFromSpec(*planSpec))
	case *status:
		printOrFail(app.StatusReport())
	default:
		if err := app.Run(version); err != nil {
			fmt.Fprintln(os.Stderr, "kikri:", err)
			os.Exit(1)
		}
	}
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printOrFail(v any, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "kikri:", err)
		os.Exit(1)
	}
	printJSON(v)
}
