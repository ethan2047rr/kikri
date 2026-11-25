//go:build !windows

package detect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethan2047rr/kikri/internal/proc"
)

func TestParseShellPATHIgnoresNoise(t *testing.T) {
	out := "Welcome banner\n" + pathBegin + "/opt/homebrew/bin:/usr/bin::/bin" + pathEnd + "\nbye\n"
	got, err := parseShellPATH(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "/opt/homebrew/bin,/usr/bin,/bin" {
		t.Fatalf("got %v", got)
	}
	if _, err := parseShellPATH("no marker"); err == nil {
		t.Fatal("expected error without marker")
	}
}

func TestProbeLoginShellReturnsPATH(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dirs, _, err := probeLoginShell(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("empty PATH from login shell")
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindPrefersEarlierDirsAndSkipsNonExecutables(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "claude"), []byte("x"), 0o644); err != nil { // not executable
		t.Fatal(err)
	}
	want := writeScript(t, b, "claude", "true")
	sp := SearchPath{Dirs: []SearchDir{{a, SourceLoginShell}, {b, SourceFallback}}}

	got, src, ok := sp.Find("claude")
	if !ok || got != want || src != SourceFallback {
		t.Fatalf("Find = %q %q %v", got, src, ok)
	}
	if _, _, ok := sp.Find("codex"); ok {
		t.Fatal("found a binary that does not exist")
	}
}

func TestParseProxyEnvKeepsOnlyProxyVars(t *testing.T) {
	out := "banner\n" + pathBegin + "/usr/bin" + pathEnd + envBegin + "\n" +
		"HTTPS_PROXY=http://u:secret@127.0.0.1:7890\nno_proxy=localhost\nANTHROPIC_API_KEY=sk-x\nall_proxy=\n" + envEnd
	got := parseProxyEnv(out)
	if strings.Join(got, "|") != "HTTPS_PROXY=http://u:secret@127.0.0.1:7890|no_proxy=localhost" {
		t.Fatalf("got %v", got)
	}
	if p := displayProxy(got); p != "http://127.0.0.1:7890" {
		t.Fatalf("displayProxy = %q", p)
	}
}

func TestEnvAddsShellProxyWithoutOverriding(t *testing.T) {
	t.Setenv("http_proxy", "http://own:1")
	sp := SearchPath{ProxyEnv: []string{"https_proxy=http://shell:2", "http_proxy=http://shell:3"}}
	env := strings.Join(sp.Env(), "\n")
	if !strings.Contains(env, "https_proxy=http://shell:2") {
		t.Fatal("shell proxy not added")
	}
	if strings.Contains(env, "http://shell:3") || !strings.Contains(env, "http_proxy=http://own:1") {
		t.Fatal("process proxy must win over the shell's")
	}
}

func TestEnvReplacesPATH(t *testing.T) {
	sp := SearchPath{Dirs: []SearchDir{{"/x", SourceFallback}, {"/y", SourceFallback}}}
	var paths []string
	for _, kv := range sp.Env() {
		if strings.HasPrefix(kv, "PATH=") {
			paths = append(paths, kv)
		}
	}
	if len(paths) != 1 || paths[0] != "PATH=/x:/y" {
		t.Fatalf("PATH entries = %v", paths)
	}
}

func TestParseClaudeAuth(t *testing.T) {
	in := proc.Result{Stdout: []byte(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty",` +
		`"email":"someone@example.com","subscriptionType":"pro","planTier":"pro","loginExpiresAt":"2026-12-17T21:23:29Z"}`)}
	got, err := parseClaudeAuth(in)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LoggedIn || got.Plan != "pro" || got.Method != "claude.ai" || got.ExpiresAt != "2026-12-17T21:23:29Z" {
		t.Fatalf("got %+v", got)
	}

	out, err := parseClaudeAuth(proc.Result{Stdout: []byte(`{"loggedIn":false,"authMethod":"none"}`)})
	if err != nil || out.LoggedIn || out.Method != "" || out.Detail != "Not logged in" {
		t.Fatalf("logged-out parse = %+v, %v", out, err)
	}

	if _, err := parseClaudeAuth(proc.Result{Stdout: []byte("error: unknown command")}); err == nil {
		t.Fatal("expected error for non-JSON output")
	}
}

func TestParseCodexAuth(t *testing.T) {
	in, err := parseCodexAuth(proc.Result{ExitCode: 0, Stdout: []byte("Logged in using ChatGPT\n")})
	if err != nil || !in.LoggedIn || in.Method != "ChatGPT" {
		t.Fatalf("logged-in parse = %+v, %v", in, err)
	}
	out, err := parseCodexAuth(proc.Result{ExitCode: 1, Stderr: []byte("Not logged in\n")})
	if err != nil || out.LoggedIn || out.Detail != "Not logged in" {
		t.Fatalf("logged-out parse = %+v, %v", out, err)
	}
	if _, err := parseCodexAuth(proc.Result{ExitCode: 2}); err == nil {
		t.Fatal("expected error for unknown exit code")
	}
}

// TestDetectToolWithFakeCLIs runs the full pipeline against fake CLIs.
func TestDetectToolWithFakeCLIs(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, bin, "claude", `case "$1" in
--version) echo "9.9.9 (Claude Code)";;
auth) echo '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}';;
esac`)
	writeScript(t, bin, "codex", `case "$1" in
--version) echo "codex-cli 1.2.3";;
login) echo "Not logged in"; exit 1;;
esac`)
	sp := SearchPath{Dirs: []SearchDir{{bin, SourceLoginShell}}}
	env := sp.Env()

	claude := detectTool(context.Background(), Tools[0], sp, env, Options{})
	if !claude.Found || claude.Version != "9.9.9 (Claude Code)" || claude.Auth == nil || claude.Auth.Plan != "max" {
		t.Fatalf("claude = %+v", claude)
	}
	codex := detectTool(context.Background(), Tools[1], sp, env, Options{})
	if !codex.Found || codex.Version != "codex-cli 1.2.3" || codex.Auth == nil || codex.Auth.LoggedIn {
		t.Fatalf("codex = %+v", codex)
	}
	missing := detectTool(context.Background(), Tools[2], sp, env, Options{})
	if missing.Found || missing.Error != "" {
		t.Fatalf("opencode = %+v", missing)
	}
	override := detectTool(context.Background(), Tools[2], sp, env, Options{Overrides: map[string]string{"opencode": "/nonexistent/opencode"}})
	if override.Found || override.Error == "" {
		t.Fatalf("bad override = %+v", override)
	}
}
