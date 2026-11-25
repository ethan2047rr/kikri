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
	wakeTool := flag.String("wake", "", "send one real wake request through `tool` (claude|codex) and print the result as JSON; this starts its 5-hour window")
	dryRun := flag.Bool("dry-run", false, "with -wake: print the command instead of running it")
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("kikri", version)
	case *diagnose:
		svc := app.NewService(version, nil)
		svc.Prepare()
		printJSON(svc.Refresh(context.Background()))
	case *wakeTool != "":
		out, err := app.WakeOnce(context.Background(), *wakeTool, *dryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, "kikri:", err)
			os.Exit(1)
		}
		printJSON(out)
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
