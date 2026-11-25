package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDarwin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exe := filepath.Join(home, "Kikri & Co.app", "kikri")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, nil, 0o755)
	executable = func() (string, error) { return exe, nil }
	defer func() { executable = os.Executable }()

	if on, err := Enabled(); err != nil || on {
		t.Fatalf("initially enabled=%v err=%v", on, err)
	}
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	if on, _ := Enabled(); !on {
		t.Fatal("not enabled after Set(true)")
	}
	b, _ := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", Label+".plist"))
	real, _ := filepath.EvalSymlinks(exe)
	if !strings.Contains(string(b), strings.ReplaceAll(real, "&", "&amp;")) || !strings.Contains(string(b), "<key>RunAtLoad</key>") {
		t.Fatalf("plist:\n%s", b)
	}
	if err := Set(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := Enabled(); on {
		t.Fatal("still enabled after Set(false)")
	}
	if err := Set(false); err != nil {
		t.Fatal("removing twice should be fine:", err)
	}
}
