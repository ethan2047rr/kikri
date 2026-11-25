// Package proc runs CLI child processes the way every Kikri call must:
// stdin from the null device, a hard timeout, and the whole process tree
// killed when the timeout fires (see PRD F2.7).
package proc

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Spec describes one child process.
type Spec struct {
	Path    string
	Args    []string
	Dir     string        // working directory; empty means inherit
	Env     []string      // full environment; nil means inherit
	Timeout time.Duration // must be > 0
}

// Result is what a finished (or killed) child produced.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int // -1 when the process did not exit normally
	Duration time.Duration
	TimedOut bool
}

// ErrNoTimeout guards against accidentally running a CLI without a deadline.
var ErrNoTimeout = errors.New("proc: Spec.Timeout must be > 0")

// Run starts the child, waits for it, and returns its output. A non-zero exit
// code is not an error: callers classify the result themselves. err is only
// set when the process could not be started or waited on for other reasons.
func Run(ctx context.Context, s Spec) (Result, error) {
	if s.Timeout <= 0 {
		return Result{ExitCode: -1}, ErrNoTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	cmd := exec.Command(s.Path, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = nil // os/exec connects a nil Stdin to the null device
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	setProcessGroup(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1}, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		killTree(cmd)
		waitErr = <-done
	}

	res := Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: -1,
		Duration: time.Since(start),
		TimedOut: timedOut,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !timedOut && ctx.Err() == nil {
		return res, waitErr
	}
	if ctx.Err() != nil && !timedOut {
		return res, ctx.Err()
	}
	return res, nil
}
