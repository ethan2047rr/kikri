package wake

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethan2047rr/kikri/internal/appdir"
)

// Output shapes as observed in docs/spike/m0-step2 and m0-step4.
const (
	claudeOK = `{"type":"system","subtype":"init","cwd":"/x","tools":[],"mcp_servers":[],"model":"claude-haiku-5-5"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"ok"}]}}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","resetsAt":1795521000,"unifiedWindows":{"five_hour":{"utilization":0.38,"resetsAt":1795521000,"status":"allowed"},"seven_day":{"utilization":0.43,"resetsAt":1795852800,"status":"allowed"}}}}
{"type":"result","subtype":"success","is_error":false,"duration_ms":3600,"result":"ok","api_error_status":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":631,"output_tokens":4},"total_cost_usd":0.00013}
`
	claudeNotLoggedIn = `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login","terminal_reason":"api_error"}`
	claudeOffline     = `{"type":"result","subtype":"success","is_error":true,"result":"API Error: Connection refused. (ECONNREFUSED)"}`
	claudeQuota       = `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":1795521000}}
{"type":"result","subtype":"success","is_error":true,"result":"Claude usage limit reached"}`
	claude429 = `{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"result":"API Error"}`

	codexOK = `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":7906,"cached_input_tokens":6144,"output_tokens":5}}
`
	codex401 = `{"type":"thread.started","thread_id":"t1"}
{"type":"error","message":"Reconnecting... 1/5"}
{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized: Missing bearer"}}`
	codexOffline = `{"type":"turn.failed","error":{"message":"stream disconnected before completion: Connection failed: error sending request for url (https://chatgpt.com/backend-api/codex/responses)"}}`
	codexQuota   = `{"type":"turn.failed","error":{"message":"You've hit your usage limit. Try again at 3:05 PM."}}`
)

func TestClaudeParse(t *testing.T) {
	p := Claude{}.Parse([]byte(claudeOK))
	if !p.terminal || p.outcome != OutcomeOK || p.reply != "ok" {
		t.Fatalf("got %+v", p)
	}
	if p.usage == nil || p.usage.InputTokens != 633 || p.usage.OutputTokens != 4 || p.usage.CachedInputTokens != 631 {
		t.Fatalf("usage = %+v", p.usage)
	}
	if len(p.windows) != 2 || p.windows[0].Kind != "five_hour" || *p.windows[0].Utilization != 0.38 ||
		!p.windows[0].ResetsAt.Equal(time.Unix(1795521000, 0)) || p.windows[1].Kind != "seven_day" {
		t.Fatalf("windows = %+v", p.windows)
	}

	for name, tc := range map[string]struct {
		in   string
		want Outcome
	}{
		"not logged in": {claudeNotLoggedIn, OutcomeNotLoggedIn},
		"offline":       {claudeOffline, OutcomeNetworkError},
		"quota":         {claudeQuota, OutcomeQuotaExhausted},
		"http 429":      {claude429, OutcomeQuotaExhausted},
	} {
		if p := (Claude{}).Parse([]byte(tc.in)); !p.terminal || p.outcome != tc.want || p.message == "" {
			t.Errorf("%s: got %+v, want %s", name, p, tc.want)
		}
	}
}

func TestCodexParse(t *testing.T) {
	p := Codex{}.Parse([]byte(codexOK))
	if !p.terminal || p.outcome != OutcomeOK || p.reply != "ok" ||
		p.usage == nil || p.usage.InputTokens != 7906 || p.usage.CachedInputTokens != 6144 {
		t.Fatalf("got %+v usage %+v", p, p.usage)
	}
	for name, tc := range map[string]struct {
		in   string
		want Outcome
	}{
		"401":     {codex401, OutcomeNotLoggedIn},
		"offline": {codexOffline, OutcomeNetworkError},
		"quota":   {codexQuota, OutcomeQuotaExhausted},
	} {
		if p := (Codex{}).Parse([]byte(tc.in)); !p.terminal || p.outcome != tc.want {
			t.Errorf("%s: got %+v, want %s", name, p, tc.want)
		}
	}
	if p := (Codex{}).Parse([]byte(`{"type":"error","message":"boom"}`)); p.terminal || p.lastErr != "boom" {
		t.Fatalf("error event: %+v", p)
	}
}

func TestCodexCommand(t *testing.T) {
	l := appdir.Layout{Sandbox: "/data/sandbox", CodexInstructions: `C:\Users\a "b"\codex.md`}
	args, env := Codex{}.Command(l)
	if env != nil || args[0] != "exec" || args[len(args)-1] != "reply ok" {
		t.Fatalf("args = %v", args)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--ignore-user-config", "--ignore-rules", "--ephemeral", "--json", "-C /data/sandbox",
		`model_instructions_file="C:\\Users\\a \"b\"\\codex.md"`, "project_doc_max_bytes=0",
		"--disable unbounded_connection_retries", "--disable shell_tool",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	args, _ = Codex{Disable: []string{}, Config: []string{}}.Command(l)
	if slices.Contains(args, "--disable") {
		t.Fatal("empty Disable must add no --disable flags")
	}
}

func TestProxyAddr(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		want string
	}{
		{nil, ""},
		{[]string{"HTTPS_PROXY=http://u:p@127.0.0.1:7890"}, "127.0.0.1:7890"},
		{[]string{"all_proxy=socks5://proxy.local"}, "proxy.local:1080"},
		{[]string{"https_proxy=127.0.0.1:8080"}, "127.0.0.1:8080"},
		{[]string{"HTTPS_PROXY=http://a:1", "https_proxy=http://b:2"}, "b:2"},
	} {
		if got := proxyAddr(tc.env); got != tc.want {
			t.Errorf("proxyAddr(%v) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestProbeNetworkViaProxy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if err := ProbeNetwork(context.Background(), []string{"https_proxy=http://" + addr}, []string{"unused.invalid"}); err != nil {
		t.Fatalf("open proxy: %v", err)
	}
	ln.Close()
	if err := ProbeNetwork(context.Background(), []string{"https_proxy=http://" + addr}, nil); err == nil {
		t.Fatal("closed proxy should fail")
	}
}

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "x.lock")
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(path); !errors.Is(err, errLocked) {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	unlock2, err := acquireLock(path)
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	unlock2()
}

// --- full flow with a fake CLI ---

type fakeEnv struct {
	layout appdir.Layout
	out    string // where the fake CLI records what it saw
	bin    string
}

func newFake(t *testing.T, stdout, stderr string, exit int, sleep string) fakeEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLIs are shell scripts")
	}
	t.Setenv(appdir.EnvHome, t.TempDir())
	l, err := appdir.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	bin := filepath.Join(t.TempDir(), "fakecli")
	script := "#!/bin/sh\n" +
		"pwd -P > \"$KIKRI_TEST_OUT/cwd\"\n" +
		"printf '%s\\n' \"$@\" > \"$KIKRI_TEST_OUT/args\"\n" +
		"echo \"$CLAUDE_CODE_MAX_RETRIES $API_TIMEOUT_MS\" > \"$KIKRI_TEST_OUT/env\"\n"
	if sleep != "" {
		script += "sleep " + sleep + "\n"
	}
	script += "cat <<'KIKRI_EOF'\n" + stdout + "\nKIKRI_EOF\n"
	if stderr != "" {
		script += "echo '" + stderr + "' >&2\n"
	}
	script += "exit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return fakeEnv{layout: l, out: out, bin: bin}
}

func (f fakeEnv) opts() Options {
	return Options{
		Layout: f.layout,
		Binary: f.bin,
		Env:    append(os.Environ(), "KIKRI_TEST_OUT="+f.out),
		Probe:  func(context.Context, []string, []string) error { return nil },
	}
}

func (f fakeEnv) ran() bool {
	_, err := os.Stat(filepath.Join(f.out, "args"))
	return err == nil
}

func (f fakeEnv) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.out, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestWakeClaudeSuccessRunsIsolated(t *testing.T) {
	f := newFake(t, claudeOK, "", 0, "")
	res := Wake(context.Background(), Claude{}, f.opts())
	if res.Outcome != OutcomeOK || res.Reply != "ok" || res.Usage.InputTokens != 633 || len(res.Windows) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("exit code = %v", res.ExitCode)
	}
	sandbox, _ := filepath.EvalSymlinks(f.layout.Sandbox)
	if cwd := f.read(t, "cwd"); cwd != sandbox {
		t.Fatalf("cwd = %q, want sandbox %q", cwd, sandbox)
	}
	args := strings.Split(f.read(t, "args"), "\n")
	for _, want := range []string{"--safe-mode", "--strict-mcp-config", `{"mcpServers":{}}`, "--no-session-persistence"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "" {
		t.Errorf(`want --tools "" in %q`, args)
	}
	if slices.Contains(args, "--bare") {
		t.Error("--bare must never be used: it ignores the subscription login")
	}
	if env := f.read(t, "env"); env != "1 15000" {
		t.Errorf("retry env = %q", env)
	}
}

func TestWakeStopsBeforeSending(t *testing.T) {
	f := newFake(t, claudeOK, "", 0, "")

	o := f.opts()
	o.CheckLogin = func(context.Context) (bool, error) { return false, nil }
	if res := Wake(context.Background(), Claude{}, o); res.Outcome != OutcomeNotLoggedIn {
		t.Fatalf("logged out: %+v", res)
	}

	o = f.opts()
	o.Probe = func(context.Context, []string, []string) error { return errors.New("no route") }
	if res := Wake(context.Background(), Claude{}, o); res.Outcome != OutcomeOffline || res.Message != "no route" {
		t.Fatalf("offline: %+v", res)
	}

	o = f.opts()
	o.Skip = func(context.Context) (string, bool) { return "window running since 05:58", true }
	if res := Wake(context.Background(), Claude{}, o); res.Outcome != OutcomeSkipped {
		t.Fatalf("skip: %+v", res)
	}

	unlock, err := acquireLock(filepath.Join(f.layout.Root, "locks", "claude.lock"))
	if err != nil {
		t.Fatal(err)
	}
	res := Wake(context.Background(), Claude{}, f.opts())
	unlock()
	if res.Outcome != OutcomeBusy {
		t.Fatalf("busy: %+v", res)
	}

	if f.ran() {
		t.Fatal("the CLI must not run when a pre-check stops the wake")
	}
	for _, o := range []Outcome{OutcomeOffline, OutcomeSkipped, OutcomeBusy} {
		if o.Sent() {
			t.Errorf("%s.Sent() = true", o)
		}
	}
}

func TestWakeLoginCheckErrorDoesNotBlock(t *testing.T) {
	f := newFake(t, claudeOK, "", 0, "")
	o := f.opts()
	o.CheckLogin = func(context.Context) (bool, error) { return false, errors.New("check timed out") }
	if res := Wake(context.Background(), Claude{}, o); res.Outcome != OutcomeOK {
		t.Fatalf("got %+v", res)
	}
}

func TestWakeTimeout(t *testing.T) {
	f := newFake(t, claudeOK, "", 0, "30")
	o := f.opts()
	o.Timeout = 300 * time.Millisecond
	start := time.Now()
	res := Wake(context.Background(), Claude{}, o)
	if res.Outcome != OutcomeTimeout || !res.Outcome.Retryable() {
		t.Fatalf("got %+v", res)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %s; the process tree was not killed", d)
	}
}

func TestWakeFallsBackToStderr(t *testing.T) {
	f := newFake(t, "", "Error: network is unreachable", 1, "")
	res := Wake(context.Background(), Codex{}, f.opts())
	if res.Outcome != OutcomeNetworkError || !strings.Contains(res.Message, "unreachable") {
		t.Fatalf("got %+v", res)
	}
	if res.ExitCode == nil || *res.ExitCode != 1 {
		t.Fatalf("exit = %v", res.ExitCode)
	}
}

func TestWakeRefusesDirtySandbox(t *testing.T) {
	f := newFake(t, claudeOK, "", 0, "")
	if err := os.WriteFile(filepath.Join(f.layout.Sandbox, "CLAUDE.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := Wake(context.Background(), Claude{}, f.opts()); res.Outcome != OutcomeFailed || f.ran() {
		t.Fatalf("got %+v, ran=%v", res, f.ran())
	}
}

func TestCodexGlobalAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if _, _, ok := CodexGlobalAgents(); ok {
		t.Fatal("no file yet")
	}
	os.WriteFile(filepath.Join(home, "AGENTS.md"), make([]byte, 5000), 0o600)
	if p, n, ok := CodexGlobalAgents(); !ok || n != 5000 || filepath.Base(p) != "AGENTS.md" {
		t.Fatalf("got %q %d %v", p, n, ok)
	}
	os.WriteFile(filepath.Join(home, "AGENTS.override.md"), make([]byte, 10), 0o600)
	if p, _, _ := CodexGlobalAgents(); filepath.Base(p) != "AGENTS.override.md" {
		t.Fatalf("override not preferred: %q", p)
	}
}
