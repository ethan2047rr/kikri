package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	// The real data directory is "Application Support": keep a space in the path.
	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "kikri.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestSettingsRoundTrip(t *testing.T) {
	s, _ := open(t)
	var v map[string]int
	if ok, err := s.GetSetting("x", &v); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := s.PutSetting("x", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting("x", map[string]int{"a": 2}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.GetSetting("x", &v); !ok || err != nil || v["a"] != 2 {
		t.Fatal(ok, err, v)
	}
}

func TestSlotsUpsertAndSurviveReopen(t *testing.T) {
	s, path := open(t)
	now := time.Unix(1795500000, 0)
	at := now.Add(-time.Hour)
	if err := s.PutSlot(Slot{Plan: "claude", At: at, State: SlotPending, Attempts: 1, NextTry: now}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSlot(Slot{Plan: "claude", At: at, State: SlotDone, Attempts: 2}, now); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.Slots("claude", at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sl, ok := got[at.Unix()]
	if len(got) != 1 || !ok || sl.State != SlotDone || sl.Attempts != 2 || !sl.NextTry.IsZero() {
		t.Fatalf("got %+v", got)
	}
	if other, _ := s2.Slots("chatgpt", time.Time{}); len(other) != 0 {
		t.Fatal("slots leaked across plans")
	}
}

func TestEntries(t *testing.T) {
	s, _ := open(t)
	base := time.Unix(1795500000, 0)
	slot := base.Add(-time.Minute)
	for i, o := range []string{"ok", "skipped", "network_error"} {
		if _, err := s.AddEntry(Entry{Plan: "claude", SlotAt: &slot, Trigger: "scheduled",
			StartedAt: base.Add(time.Duration(i) * time.Minute), Outcome: o, Result: json.RawMessage(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddEntry(Entry{Plan: "chatgpt", Trigger: "manual", StartedAt: base, Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Entries("", 10)
	if len(all) != 4 || all[0].Outcome != "network_error" {
		t.Fatalf("all = %+v", all)
	}
	last, err := s.LastEntry("claude", "ok", "skipped")
	if err != nil || last == nil || last.Outcome != "skipped" || last.SlotAt == nil || !last.SlotAt.Equal(slot) {
		t.Fatalf("last = %+v, %v", last, err)
	}
	if m, _ := s.LastEntry("chatgpt"); m == nil || m.SlotAt != nil || m.Result != nil {
		t.Fatalf("manual = %+v", m)
	}
	if none, _ := s.LastEntry("claude", "quota_exhausted"); none != nil {
		t.Fatal("unexpected entry")
	}
	if n, err := s.CountEntries("ok", base.Add(-time.Hour)); err != nil || n != 2 {
		t.Fatalf("count ok = %d, %v", n, err)
	}
	if n, _ := s.CountEntries("ok", base.Add(time.Second)); n != 0 {
		t.Fatalf("count since later = %d", n)
	}
}
