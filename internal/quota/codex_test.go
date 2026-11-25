package quota

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Shape of a real answer (2026-11-25, Plus account, no active 5h window);
// account identifiers removed.
const answer = `{"ordinaryUsageAllowed":true,"rateLimits":{"limitId":"codex","limitName":null,
"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1795589657},
"secondary":{"usedPercent":11,"windowDurationMins":10080,"resetsAt":1796033809},
"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"plus","rateLimitReachedType":null}}`

func TestParseCodex(t *testing.T) {
	c, err := parseCodex([]byte(answer))
	if err != nil {
		t.Fatal(err)
	}
	if c.PlanType != "plus" || c.Primary == nil || c.Primary.WindowMins != 300 ||
		!c.Primary.ResetsAt.Equal(time.Unix(1795589657, 0)) || c.Secondary.UsedPercent != 11 || c.LimitReachedType != "" {
		t.Fatalf("got %+v", c)
	}
	if _, err := parseCodex([]byte(`{}`)); err == nil {
		t.Fatal("missing rateLimits must be an error")
	}
}

func TestWindowActive(t *testing.T) {
	now := time.Unix(1795571657, 0) // 5h before the resetsAt above
	w := Window{WindowMins: 300, ResetsAt: now.Add(5*time.Hour + 2*time.Second)}
	if w.Active(now) {
		t.Fatal("virtual now+5h window must count as inactive (V18)")
	}
	w.ResetsAt = now.Add(4 * time.Hour)
	if !w.Active(now) {
		t.Fatal("window resetting in 4h is active")
	}
	w.ResetsAt = now.Add(5*time.Hour - 3*time.Minute)
	if !w.Active(now) {
		t.Fatal("window opened 3 minutes ago is active")
	}
}

func TestReadCodexWithFakeServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake server is a shell script")
	}
	bin := filepath.Join(t.TempDir(), "codex")
	// Answers by request order, with a notification in between, like the real server.
	script := `#!/bin/sh
[ "$1" = "app-server" ] || exit 2
read -r init
echo '{"id":1,"result":{"userAgent":"x"}}'
read -r initialized
read -r req
echo '{"method":"account/updated","params":{}}'
echo '{"id":2,"result":` + oneLine(answer) + `}'
sleep 30
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	c, err := ReadCodex(context.Background(), bin, os.Environ(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Primary == nil || c.PlanType != "plus" {
		t.Fatalf("got %+v", c)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("server not killed after the answer: %s", d)
	}
}

func oneLine(s string) string {
	out := []rune{}
	for _, r := range s {
		if r != '\n' {
			out = append(out, r)
		}
	}
	return string(out)
}
