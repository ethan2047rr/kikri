//go:build !windows

package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func processGone(t *testing.T, pid string) bool {
	n, err := strconv.Atoi(pid)
	if err != nil {
		t.Fatal(err)
	}
	return errors.Is(syscall.Kill(n, 0), syscall.ESRCH)
}

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	res, err := Run(context.Background(), Spec{
		Path:    "/bin/sh",
		Args:    []string{"-c", "echo out; echo err >&2; exit 3"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(res.Stdout)) != "out" || strings.TrimSpace(string(res.Stderr)) != "err" {
		t.Fatalf("unexpected output: %q / %q", res.Stdout, res.Stderr)
	}
	if res.ExitCode != 3 || res.TimedOut {
		t.Fatalf("exit=%d timedOut=%v", res.ExitCode, res.TimedOut)
	}
}

func TestRunStdinIsNull(t *testing.T) {
	// `cat` would block forever on an inherited terminal or pipe.
	res, err := Run(context.Background(), Spec{Path: "/bin/cat", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.TimedOut || res.ExitCode != 0 {
		t.Fatalf("cat did not see EOF: %+v", res)
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	start := time.Now()
	res, err := Run(context.Background(), Spec{
		Path: "/bin/sh",
		// The grandchild must die together with its parent's process group.
		Args:    []string{"-c", "sleep 30 & echo $! > " + pidFile + "; wait"},
		Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Run took %v after timeout", time.Since(start))
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(raw))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if processGone(t, pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %s still alive after timeout", pid)
}

func TestRunRequiresTimeout(t *testing.T) {
	if _, err := Run(context.Background(), Spec{Path: "/bin/true"}); err != ErrNoTimeout {
		t.Fatalf("want ErrNoTimeout, got %v", err)
	}
}
