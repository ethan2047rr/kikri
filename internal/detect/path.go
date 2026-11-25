package detect

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ethan2047rr/kikri/internal/proc"
)

// Source says where a search directory (and therefore a found CLI) came from.
type Source string

const (
	SourceOverride    Source = "override"     // path set by the user in settings
	SourceLoginShell  Source = "login-shell"  // PATH printed by the user's login shell
	SourceProcessPATH Source = "process-path" // PATH Kikri itself was started with
	SourceFallback    Source = "fallback"     // well-known install locations
)

// SearchDir is one directory to look in, tagged with its origin.
type SearchDir struct {
	Dir    string `json:"dir"`
	Source Source `json:"source"`
}

// SearchPath is the ordered, de-duplicated list of directories used to find
// CLIs. The same list becomes PATH for child processes, so npm-installed CLIs
// (`#!/usr/bin/env node`) can find node.
type SearchPath struct {
	Dirs []SearchDir `json:"dirs"`
	// LoginShellError is set when the login-shell probe failed (Unix only).
	LoginShellError string `json:"loginShellError,omitempty"`
	// ProxyEnv holds *_proxy variables exported by the login shell. A GUI app
	// started from Finder does not inherit them, yet the CLIs need them to
	// reach their APIs. They may carry credentials, so they are never
	// serialised; Proxy is the redacted form for display.
	ProxyEnv []string `json:"-"`
	Proxy    string   `json:"proxy,omitempty"`
}

const (
	pathBegin = "__KIKRI_PATH_BEGIN__"
	pathEnd   = "__KIKRI_PATH_END__"
	envBegin  = "__KIKRI_ENV_BEGIN__"
	envEnd    = "__KIKRI_ENV_END__"
)

// proxyVars are the only variables Kikri takes from the login shell besides
// PATH. Everything else (API keys included) is left alone.
var proxyVars = map[string]bool{
	"https_proxy": true, "http_proxy": true, "all_proxy": true, "no_proxy": true,
}

// GUI apps on macOS start with launchd's minimal PATH (/usr/bin:/bin:...),
// which misses Homebrew, nvm, ~/.local/bin and friends (docs/spike/m0-step4 T1).
// Asking the user's login shell is the most faithful way to recover it.
func probeLoginShell(ctx context.Context) (dirs, proxyEnv []string, err error) {
	shell := userShell()
	// grep keeps only proxy variables, so no other environment value (API
	// keys, tokens) ever reaches Kikri.
	script := `printf '` + pathBegin + `%s` + pathEnd + `' "$PATH"; ` +
		`printf '` + envBegin + `\n'; env | grep -iE '^(https?|all|no)_proxy='; printf '` + envEnd + `'`
	res, err := proc.Run(ctx, proc.Spec{
		Path:    shell,
		Args:    []string{"-ilc", script},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}
	if res.TimedOut {
		return nil, nil, errTimeout(shell)
	}
	out := string(res.Stdout)
	if dirs, err = parseShellPATH(out); err != nil {
		return nil, nil, err
	}
	return dirs, parseProxyEnv(out), nil
}

// parseProxyEnv extracts NAME=value lines for known proxy variables from
// between the env markers.
func parseProxyEnv(out string) []string {
	i := strings.LastIndex(out, envBegin)
	if i < 0 {
		return nil
	}
	rest := out[i+len(envBegin):]
	if j := strings.Index(rest, envEnd); j >= 0 {
		rest = rest[:j]
	}
	var env []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimRight(line, "\r")
		k, v, ok := strings.Cut(line, "=")
		if ok && v != "" && proxyVars[strings.ToLower(k)] {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// redactProxy turns a proxy URL into scheme://host:port, dropping any
// user:password. Unparseable values are reported as "(set)".
func redactProxy(v string) string {
	if !strings.Contains(v, "://") {
		v = "http://" + v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "(set)"
	}
	return u.Scheme + "://" + u.Host
}

// displayProxy picks the proxy the CLIs would use for HTTPS.
func displayProxy(env []string) string {
	vals := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vals[strings.ToLower(k)] = v
	}
	for _, k := range []string{"https_proxy", "all_proxy"} {
		if v := vals[k]; v != "" {
			return redactProxy(v)
		}
	}
	return ""
}

type shellError string

func (e shellError) Error() string { return string(e) }

func errTimeout(shell string) error {
	return shellError("login shell probe timed out: " + shell)
}

// parseShellPATH extracts PATH from between the markers; rc files may print
// banners, so everything outside the markers is ignored.
func parseShellPATH(out string) ([]string, error) {
	i := strings.LastIndex(out, pathBegin)
	if i < 0 {
		return nil, shellError("login shell printed no PATH marker")
	}
	rest := out[i+len(pathBegin):]
	j := strings.Index(rest, pathEnd)
	if j < 0 {
		return nil, shellError("login shell PATH marker not terminated")
	}
	return splitList(rest[:j]), nil
}

func splitList(s string) []string {
	var dirs []string
	for _, d := range filepath.SplitList(s) {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+os.Getenv("USER"), "UserShell").Output(); err == nil {
			if f := strings.Fields(string(out)); len(f) == 2 {
				return f[1]
			}
		}
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// fallbackDirs lists common install locations for the supported CLIs.
func fallbackDirs(home string) []string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")
		return compact([]string{
			filepath.Join(home, ".local", "bin"), // Claude Code native installer
			join(appData, "npm"),
			join(localAppData, "Microsoft", "WinGet", "Links"),
			filepath.Join(home, "scoop", "shims"),
			filepath.Join(home, ".bun", "bin"),
			join(localAppData, "Volta", "bin"),
			join(localAppData, "Programs", "opencode"),
		})
	}
	dirs := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".claude", "local"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".opencode", "bin"),
	}
	// Newest nvm-managed node first.
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
	sort.Sort(sort.Reverse(sort.StringSlice(nvm)))
	return append(dirs, nvm...)
}

func join(base string, elem ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, elem...)...)
}

func compact(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// BuildSearchPath merges login-shell PATH, Kikri's own PATH and fallback
// directories, in that order of preference.
func BuildSearchPath(ctx context.Context) SearchPath {
	var sp SearchPath
	seen := map[string]bool{}
	add := func(dirs []string, src Source) {
		for _, d := range dirs {
			key := filepath.Clean(d)
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			sp.Dirs = append(sp.Dirs, SearchDir{Dir: d, Source: src})
		}
	}
	if runtime.GOOS != "windows" {
		dirs, proxyEnv, err := probeLoginShell(ctx)
		if err != nil {
			sp.LoginShellError = err.Error()
		}
		add(dirs, SourceLoginShell)
		sp.ProxyEnv = proxyEnv
	}
	add(splitList(os.Getenv("PATH")), SourceProcessPATH)
	home, _ := os.UserHomeDir()
	add(fallbackDirs(home), SourceFallback)
	sp.Proxy = displayProxy(sp.Env())
	return sp
}

// PATH renders the search path as a PATH value for child processes.
func (sp SearchPath) PATH() string {
	dirs := make([]string, len(sp.Dirs))
	for i, d := range sp.Dirs {
		dirs[i] = d.Dir
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// Env returns the current environment with PATH replaced by the search path
// and the login shell's proxy variables added where Kikri's own environment
// does not already set them.
func (sp SearchPath) Env() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(sp.ProxyEnv)+1)
	have := map[string]bool{}
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(k, "PATH") {
			continue
		}
		have[k] = true
		out = append(out, kv)
	}
	for _, kv := range sp.ProxyEnv {
		if k, _, _ := strings.Cut(kv, "="); !have[k] {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+sp.PATH())
}

// Find looks for binary in the search directories and returns its path and
// the Source of the directory it was found in.
func (sp SearchPath) Find(binary string) (string, Source, bool) {
	for _, d := range sp.Dirs {
		for _, name := range candidateNames(binary) {
			p := filepath.Join(d.Dir, name)
			if isExecutable(p) {
				return p, d.Source, true
			}
		}
	}
	return "", "", false
}

func candidateNames(binary string) []string {
	if runtime.GOOS != "windows" {
		return []string{binary}
	}
	// Prefer native executables over npm's .cmd shims.
	return []string{binary + ".exe", binary + ".cmd", binary + ".bat"}
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0o111 != 0
}
