package detect

import (
	"context"
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
}

const (
	pathBegin = "__KIKRI_PATH_BEGIN__"
	pathEnd   = "__KIKRI_PATH_END__"
)

// GUI apps on macOS start with launchd's minimal PATH (/usr/bin:/bin:...),
// which misses Homebrew, nvm, ~/.local/bin and friends (docs/spike/m0-step4 T1).
// Asking the user's login shell is the most faithful way to recover it.
func probeLoginShell(ctx context.Context) ([]string, error) {
	shell := userShell()
	script := `printf '` + pathBegin + `%s` + pathEnd + `' "$PATH"`
	res, err := proc.Run(ctx, proc.Spec{
		Path:    shell,
		Args:    []string{"-ilc", script},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if res.TimedOut {
		return nil, errTimeout(shell)
	}
	return parseShellPATH(string(res.Stdout))
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
		dirs, err := probeLoginShell(ctx)
		if err != nil {
			sp.LoginShellError = err.Error()
		}
		add(dirs, SourceLoginShell)
	}
	add(splitList(os.Getenv("PATH")), SourceProcessPATH)
	home, _ := os.UserHomeDir()
	add(fallbackDirs(home), SourceFallback)
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

// Env returns the current environment with PATH replaced by the search path.
func (sp SearchPath) Env() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			continue
		}
		out = append(out, kv)
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
