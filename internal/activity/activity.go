// Package activity finds when the user last used a CLI, from the modification
// times of its session files (PRD F3.4 basis B, D6/D16). It only stats files;
// it never opens or reads them.
package activity

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// maxFiles bounds a scan so a huge history cannot stall the scheduler.
const maxFiles = 200_000

var errEnough = errors.New("enough")

// Latest returns the newest modification time of the regular files under
// roots (a root may also be a single file). Missing roots are ignored; the
// zero time means no files were found. Symlinks are not followed.
func Latest(roots ...string) time.Time {
	var newest time.Time
	seen := 0
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entry or missing root: skip it
			}
			if !d.Type().IsRegular() {
				return nil
			}
			if seen++; seen > maxFiles {
				return errEnough
			}
			if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
			return nil
		})
	}
	return newest
}

// ClaudeRoots are Claude Code's session directories. Kikri's own wakes use
// --no-session-persistence and leave nothing here.
func ClaudeRoots() []string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return []string{filepath.Join(dir, "projects")}
	}
	return homeJoin(".claude", "projects")
}

// CodexRoots are Codex's session directories (also used by T3 Code, V14).
// Kikri's own wakes use --ephemeral and leave nothing here.
func CodexRoots() []string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return []string{filepath.Join(dir, "sessions")}
	}
	return homeJoin(".codex", "sessions")
}

func homeJoin(elem ...string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(append([]string{home}, elem...)...)}
}
