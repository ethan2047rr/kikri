// Package schedule turns a plan's wake times into concrete slots and decides
// how long a missed slot may still be made up (PRD F3.2, F3.3, F4.1). It is
// pure: no clock, no I/O, so sleep and clock-change cases are easy to test.
package schedule

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Clock is a time of day in minutes after midnight, written "HH:MM".
type Clock int

// ParseClock parses "HH:MM" (24-hour).
func ParseClock(s string) (Clock, error) {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 || len(s) != 5 {
		return 0, fmt.Errorf("invalid time %q, want HH:MM", s)
	}
	return Clock(h*60 + m), nil
}

// MustClock is ParseClock for constants.
func MustClock(s string) Clock {
	c, err := ParseClock(s)
	if err != nil {
		panic(err)
	}
	return c
}

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60) }

func (c Clock) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

func (c *Clock) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseClock(s)
	*c = v
	return err
}

// On returns the instant of this clock time on the given day in loc. On a
// daylight-saving gap the result is normalised forward by time.Date.
func (c Clock) On(day time.Time, loc *time.Location) time.Time {
	y, m, d := day.In(loc).Date()
	return time.Date(y, m, d, int(c)/60, int(c)%60, 0, 0, loc)
}

// Weekdays says which days a schedule runs, indexed by time.Weekday
// (Sunday = 0).
type Weekdays [7]bool

// Workdays is Monday to Friday, the default (F3.3: weekends would only burn
// the weekly limit).
var Workdays = Weekdays{false, true, true, true, true, true, false}

// Everyday runs all week.
var Everyday = Weekdays{true, true, true, true, true, true, true}

// Preset names (F3.2).
const (
	PresetEarlyBird = "early-bird" // 06:30, refresh at 11:30
	PresetThree     = "three"      // 06:00 and 11:05; Smart Skip drops 11:05 if a window is running
	PresetCustom    = "custom"
)

// PresetTimes returns the wake times of a named preset.
func PresetTimes(name string) ([]Clock, bool) {
	switch name {
	case PresetEarlyBird:
		return []Clock{MustClock("06:30")}, true
	case PresetThree:
		return []Clock{MustClock("06:00"), MustClock("11:05")}, true
	}
	return nil, false
}

// Plan is the schedule of one plan.
type Plan struct {
	Enabled  bool     `json:"enabled"`
	Preset   string   `json:"preset"`
	Times    []Clock  `json:"times"` // used for PresetCustom; filled from the preset otherwise
	Weekdays Weekdays `json:"weekdays"`
	// Since is when the schedule was last enabled or changed. Slots before it
	// are never made up, so turning Kikri on at 08:00 does not fire 06:30.
	Since time.Time `json:"since"`
}

// WakeTimes returns the plan's times of day, sorted and de-duplicated.
func (p Plan) WakeTimes() []Clock {
	times := p.Times
	if t, ok := PresetTimes(p.Preset); ok {
		times = t
	}
	out := append([]Clock(nil), times...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	uniq := out[:0]
	for i, c := range out {
		if i == 0 || c != out[i-1] {
			uniq = append(uniq, c)
		}
	}
	return uniq
}

// Slots returns every slot of p in [from, to], oldest first.
func (p Plan) Slots(from, to time.Time, loc *time.Location) []time.Time {
	if !p.Enabled || to.Before(from) {
		return nil
	}
	if p.Since.After(from) {
		from = p.Since
	}
	times := p.WakeTimes()
	var out []time.Time
	day := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	for ; !day.After(to); day = day.AddDate(0, 0, 1) {
		if !p.Weekdays[day.Weekday()] {
			continue
		}
		for _, c := range times {
			if t := c.On(day, loc); !t.Before(from) && !t.After(to) {
				out = append(out, t)
			}
		}
	}
	return out
}

// Next returns the first slot strictly after now, looking up to 8 days ahead.
func (p Plan) Next(now time.Time, loc *time.Location) (time.Time, bool) {
	s := p.Slots(now.Add(time.Second), now.AddDate(0, 0, 8), loc)
	if len(s) == 0 {
		return time.Time{}, false
	}
	return s[0], true
}

// LateGrace is how long a slot at or after the cut-off may still run.
const LateGrace = 30 * time.Minute

// Deadline is the last moment a missed slot may still be made up (F4.1).
// A slot before the cut-off (default 09:00) may run until the cut-off: later
// than that, a wake would push the afternoon refresh back. A slot at or after
// the cut-off, such as 11:05, gets LateGrace.
func Deadline(slot time.Time, cutoff Clock, loc *time.Location) time.Time {
	if c := cutoff.On(slot, loc); slot.Before(c) {
		return c
	}
	return slot.Add(LateGrace)
}
