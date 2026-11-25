package wake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/proc"
)

// Runner knows how to wake one CLI. Detection and login checks live in
// package detect; a Runner only builds the wake command and reads its output.
type Runner interface {
	ID() string
	// Hosts are probed (TCP 443) before sending; reaching any one is enough.
	Hosts() []string
	// Command returns the CLI arguments and extra environment variables.
	Command(l appdir.Layout) (args, env []string)
	Parse(stdout []byte) parsed
}

// Runners lists the built-in runners by tool ID.
var Runners = map[string]Runner{
	"claude": Claude{},
	"codex":  Codex{},
}

// DefaultTimeout is the hard limit for one wake (Codex took at most 15s in M0).
const DefaultTimeout = 60 * time.Second

// Options configures one Wake call. Layout, Binary and Env are required.
type Options struct {
	Layout appdir.Layout
	Binary string   // CLI executable found by package detect
	Env    []string // full child environment, e.g. detect.SearchPath.Env()

	Timeout time.Duration // 0 means DefaultTimeout

	// CheckLogin runs the local login check (F2.6 step 1). nil skips it. An
	// error from the check itself does not block the wake.
	CheckLogin func(ctx context.Context) (loggedIn bool, err error)
	// Probe checks that the API is reachable (step 2). nil means ProbeNetwork.
	Probe func(ctx context.Context, env, hosts []string) error
	// Skip is Smart Skip (step 3): return a reason to skip. nil never skips.
	Skip func(ctx context.Context) (reason string, skip bool)

	Now func() time.Time // for tests; nil means time.Now
}

// Wake runs the F2.6 flow: lock, login check, network probe, Smart Skip,
// then the isolated request, and classifies the result. It never returns an
// error: every failure is a Result with an Outcome the scheduler can act on.
func Wake(ctx context.Context, r Runner, o Options) Result {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	res := Result{Tool: r.ID(), StartedAt: now()}
	finish := func(out Outcome, msg string) Result {
		res.Outcome, res.Message = out, msg
		res.DurationMs = now().Sub(res.StartedAt).Milliseconds()
		return res
	}

	// One wake per tool at a time, across processes too (F2.7).
	unlock, err := acquireLock(filepath.Join(o.Layout.Root, "locks", r.ID()+".lock"))
	if errors.Is(err, errLocked) {
		return finish(OutcomeBusy, "another wake for this tool is already running")
	} else if err != nil {
		return finish(OutcomeFailed, "lock: "+err.Error())
	}
	defer unlock()

	if err := appdir.CheckSandbox(o.Layout.Sandbox); err != nil {
		return finish(OutcomeFailed, err.Error())
	}

	if o.CheckLogin != nil {
		if ok, err := o.CheckLogin(ctx); err == nil && !ok {
			return finish(OutcomeNotLoggedIn, "not logged in (local check)")
		}
	}

	probe := o.Probe
	if probe == nil {
		probe = ProbeNetwork
	}
	if err := probe(ctx, o.Env, r.Hosts()); err != nil {
		return finish(OutcomeOffline, err.Error())
	}

	if o.Skip != nil {
		if reason, skip := o.Skip(ctx); skip {
			return finish(OutcomeSkipped, reason)
		}
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	args, extra := r.Command(o.Layout)
	env := append(append([]string(nil), o.Env...), extra...) // later entries win
	res.StartedAt = now()
	pr, err := proc.Run(ctx, proc.Spec{Path: o.Binary, Args: args, Dir: o.Layout.Sandbox, Env: env, Timeout: timeout})
	if err != nil && !pr.TimedOut && pr.ExitCode == -1 && len(pr.Stdout) == 0 {
		return finish(OutcomeFailed, "start "+filepath.Base(o.Binary)+": "+err.Error())
	}
	if pr.ExitCode >= 0 {
		code := pr.ExitCode
		res.ExitCode = &code
	}
	res.Stderr = tail(pr.Stderr, 2048)

	p := r.Parse(pr.Stdout)
	res.Usage, res.Windows, res.Reply = p.usage, p.windows, p.reply
	switch {
	case pr.TimedOut:
		return finish(OutcomeTimeout, fmt.Sprintf("no answer within %s; process tree killed", timeout))
	case p.terminal:
		return finish(p.outcome, p.message)
	case err != nil:
		return finish(OutcomeFailed, err.Error())
	}
	// No final event: fall back to the last error event, then stderr.
	msg := firstNonEmpty(p.lastErr, lastLine(res.Stderr))
	if msg == "" {
		return finish(OutcomeFailed, fmt.Sprintf("exited with code %d and no result", pr.ExitCode))
	}
	return finish(classifyMessage(msg), msg)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// ProbeNetwork opens a TCP connection to port 443 of any host (F2.6 step 2).
// When an HTTPS proxy is configured in env the CLI talks to the proxy, so
// the proxy is probed instead.
func ProbeNetwork(ctx context.Context, env, hosts []string) error {
	targets := make([]string, 0, len(hosts))
	if p := proxyAddr(env); p != "" {
		targets = append(targets, p)
	} else {
		for _, h := range hosts {
			targets = append(targets, net.JoinHostPort(h, "443"))
		}
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	var errs []string
	for _, t := range targets {
		conn, err := d.DialContext(ctx, "tcp", t)
		if err == nil {
			conn.Close()
			return nil
		}
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		return errors.New("no host to probe")
	}
	return errors.New("network unreachable: " + strings.Join(errs, "; "))
}

// proxyAddr returns host:port of the HTTPS proxy set in env, if any.
func proxyAddr(env []string) string {
	vals := map[string]string{}
	for _, kv := range env { // later duplicates win, as in os/exec
		if k, v, ok := strings.Cut(kv, "="); ok {
			vals[strings.ToLower(k)] = v
		}
	}
	raw := firstNonEmpty(vals["https_proxy"], vals["all_proxy"])
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "socks5", "socks5h", "socks":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

var errLocked = errors.New("locked")

// acquireLock takes an exclusive, non-blocking lock on path. The OS releases
// it if Kikri dies, so there are no stale lock files to clean up.
func acquireLock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
