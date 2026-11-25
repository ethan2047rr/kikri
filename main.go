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
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("kikri", version)
	case *diagnose:
		svc := app.NewService(version, nil)
		svc.Prepare()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(svc.Refresh(context.Background())); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		if err := app.Run(version); err != nil {
			fmt.Fprintln(os.Stderr, "kikri:", err)
			os.Exit(1)
		}
	}
}
