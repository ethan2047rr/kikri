// Package appdir owns Kikri's data directory and the empty sandbox directory
// every wake runs in (PRD F2.1).
package appdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// EnvHome overrides the data directory (tests, portable installs).
const EnvHome = "KIKRI_HOME"

// CodexInstructions replaces Codex's built-in base instructions during a wake
// (`-c model_instructions_file=...`), see docs/spike/m0-step2.
const CodexInstructions = "Reply with: ok\n"

// Layout lists the paths Kikri uses inside its data directory.
type Layout struct {
	Root              string `json:"root"`
	Sandbox           string `json:"sandbox"`
	CodexInstructions string `json:"codexInstructions"`
	Database          string `json:"database"`
}

// Resolve returns the layout without touching the filesystem.
//   - macOS:   ~/Library/Application Support/Kikri
//   - Windows: %AppData%\Kikri
//   - other:   $XDG_CONFIG_HOME/kikri (or ~/.config/kikri)
func Resolve() (Layout, error) {
	root := os.Getenv(EnvHome)
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Layout{}, fmt.Errorf("locate user config dir: %w", err)
		}
		name := "Kikri"
		if filepath.Base(base) == ".config" {
			name = "kikri"
		}
		root = filepath.Join(base, name)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, err
	}
	return Layout{
		Root:              root,
		Sandbox:           filepath.Join(root, "sandbox"),
		CodexInstructions: filepath.Join(root, "codex-instructions.md"),
		Database:          filepath.Join(root, "kikri.db"),
	}, nil
}

// ErrSandboxNotEmpty means something was put into the sandbox. A wake must not
// run there, because a CLAUDE.md / AGENTS.md / .git would leak into the request.
type ErrSandboxNotEmpty struct {
	Dir     string
	Entries []string
}

func (e *ErrSandboxNotEmpty) Error() string {
	return fmt.Sprintf("sandbox %s is not empty: %v", e.Dir, e.Entries)
}

// Ensure creates the directories and support files, then verifies that the
// sandbox is empty. It never deletes anything: a non-empty sandbox is reported
// so the user can see what got there.
func Ensure() (Layout, error) {
	l, err := Resolve()
	if err != nil {
		return l, err
	}
	if err := os.MkdirAll(l.Sandbox, 0o700); err != nil {
		return l, fmt.Errorf("create sandbox: %w", err)
	}
	if err := writeIfChanged(l.CodexInstructions, []byte(CodexInstructions)); err != nil {
		return l, fmt.Errorf("write codex instructions: %w", err)
	}
	return l, CheckSandbox(l.Sandbox)
}

// CheckSandbox returns *ErrSandboxNotEmpty if dir has any entry.
func CheckSandbox(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return &ErrSandboxNotEmpty{Dir: dir, Entries: names}
}

func writeIfChanged(path string, data []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && string(old) == string(data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
