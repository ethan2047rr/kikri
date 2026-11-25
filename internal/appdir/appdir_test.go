package appdir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesLayout(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kikri-home")
	t.Setenv(EnvHome, home)

	l, err := Ensure()
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != home || l.Sandbox != filepath.Join(home, "sandbox") {
		t.Fatalf("unexpected layout %+v", l)
	}
	got, err := os.ReadFile(l.CodexInstructions)
	if err != nil || string(got) != CodexInstructions {
		t.Fatalf("instructions file = %q, %v", got, err)
	}
	// Idempotent.
	if _, err := Ensure(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRejectsNonEmptySandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	l, err := Ensure()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", ".git"} {
		if err := os.WriteFile(filepath.Join(l.Sandbox, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, err = Ensure()
	var notEmpty *ErrSandboxNotEmpty
	if !errors.As(err, &notEmpty) {
		t.Fatalf("want ErrSandboxNotEmpty, got %v", err)
	}
	if len(notEmpty.Entries) != 2 || notEmpty.Entries[0] != ".git" {
		t.Fatalf("entries = %v", notEmpty.Entries)
	}
	// Nothing was deleted.
	if _, err := os.Stat(filepath.Join(l.Sandbox, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
}
