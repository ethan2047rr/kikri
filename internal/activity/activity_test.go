package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLatest(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	recent := time.Now().Add(-time.Hour).Truncate(time.Second)
	write := func(rel string, mt time.Time) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte("x"), 0o600)
		os.Chtimes(p, mt, mt)
	}
	write("2026/11/24/a.jsonl", old)
	write("2026/11/25/b.jsonl", recent)
	// Directories themselves do not count.
	os.Chtimes(filepath.Join(root, "2026"), time.Now(), time.Now())

	if got := Latest(root, filepath.Join(root, "missing")); !got.Equal(recent) {
		t.Fatalf("got %s, want %s", got, recent)
	}
	if got := Latest(filepath.Join(root, "missing")); !got.IsZero() {
		t.Fatalf("missing root: %s", got)
	}
}

func TestRootsHonourEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	t.Setenv("CODEX_HOME", "/x")
	if r := ClaudeRoots(); len(r) != 1 || r[0] != filepath.Join("/c", "projects") {
		t.Fatal(r)
	}
	if r := CodexRoots(); len(r) != 1 || r[0] != filepath.Join("/x", "sessions") {
		t.Fatal(r)
	}
}
