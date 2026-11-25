package schedule

import (
	"encoding/json"
	"testing"
	"time"
)

var shanghai = time.FixedZone("UTC+8", 8*3600)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, shanghai)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseClock(t *testing.T) {
	for _, ok := range []string{"00:00", "06:30", "23:59"} {
		if c, err := ParseClock(ok); err != nil || c.String() != ok {
			t.Errorf("%s: %v %v", ok, c, err)
		}
	}
	for _, bad := range []string{"6:30", "24:00", "12:60", "ab:cd", "06:30:00", ""} {
		if _, err := ParseClock(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	var c Clock
	if err := json.Unmarshal([]byte(`"11:05"`), &c); err != nil || c != MustClock("11:05") {
		t.Fatal(c, err)
	}
	if b, _ := json.Marshal(c); string(b) != `"11:05"` {
		t.Fatal(string(b))
	}
}

func TestSlotsRespectWeekdaysPresetAndSince(t *testing.T) {
	// 2026-11-27 is a Friday.
	p := Plan{Enabled: true, Preset: PresetThree, Weekdays: Workdays}
	got := p.Slots(at("2026-11-27 00:00"), at("2026-11-30 23:59"), shanghai)
	want := []string{"2026-11-27 06:00", "2026-11-27 11:05", "2026-11-30 06:00", "2026-11-30 11:05"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if !got[i].Equal(at(want[i])) {
			t.Errorf("slot %d = %s, want %s", i, got[i], want[i])
		}
	}

	p.Since = at("2026-11-27 08:00")
	if got := p.Slots(at("2026-11-27 00:00"), at("2026-11-27 23:59"), shanghai); len(got) != 1 || !got[0].Equal(at("2026-11-27 11:05")) {
		t.Fatalf("Since not applied: %v", got)
	}

	p.Enabled = false
	if got := p.Slots(at("2026-11-27 00:00"), at("2026-11-30 23:59"), shanghai); got != nil {
		t.Fatal("disabled plan has slots")
	}
}

func TestCustomTimesSortedAndDeduped(t *testing.T) {
	p := Plan{Enabled: true, Preset: PresetCustom, Weekdays: Everyday,
		Times: []Clock{MustClock("11:35"), MustClock("06:30"), MustClock("11:35")}}
	if got := p.WakeTimes(); len(got) != 2 || got[0].String() != "06:30" || got[1].String() != "11:35" {
		t.Fatalf("got %v", got)
	}
}

func TestNext(t *testing.T) {
	p := Plan{Enabled: true, Preset: PresetEarlyBird, Weekdays: Workdays}
	// Friday after 06:30: next is Monday.
	if n, ok := p.Next(at("2026-11-27 07:00"), shanghai); !ok || !n.Equal(at("2026-11-30 06:30")) {
		t.Fatalf("got %s %v", n, ok)
	}
	// Exactly at a slot: that slot is not "next".
	if n, _ := p.Next(at("2026-11-26 06:30"), shanghai); !n.Equal(at("2026-11-27 06:30")) {
		t.Fatalf("got %s", n)
	}
}

func TestDeadline(t *testing.T) {
	cut := MustClock("09:00")
	if d := Deadline(at("2026-11-27 06:30"), cut, shanghai); !d.Equal(at("2026-11-27 09:00")) {
		t.Errorf("morning slot: %s", d)
	}
	if d := Deadline(at("2026-11-27 11:05"), cut, shanghai); !d.Equal(at("2026-11-27 11:35")) {
		t.Errorf("late slot: %s", d)
	}
	if d := Deadline(at("2026-11-27 09:00"), cut, shanghai); !d.Equal(at("2026-11-27 09:30")) {
		t.Errorf("slot at cut-off: %s", d)
	}
}

func TestSlotsAcrossDSTGap(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 2027-03-14 02:30 does not exist in New York; the slot still appears once.
	p := Plan{Enabled: true, Preset: PresetCustom, Times: []Clock{MustClock("02:30")}, Weekdays: Everyday}
	got := p.Slots(time.Date(2027, 3, 13, 0, 0, 0, 0, ny), time.Date(2027, 3, 15, 23, 0, 0, 0, ny), ny)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}
