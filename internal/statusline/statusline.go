// Package statusline reads Claude's 5-hour window from Claude Code's own
// status line data (D24).
//
// Claude has no read-only quota query; the only official source of the reset
// time is the JSON Claude Code passes to the status line command, whose
// rate_limits field carries five_hour / seven_day {used_percentage,
// resets_at} for Pro/Max/Ultra subscribers (code.claude.com/docs/en/statusline).
//
// When the user opts in, Kikri puts itself in front of their status line:
// ~/.claude/settings.json's statusLine.command becomes `kikri -statusline`,
// which saves rate_limits (and nothing else from the session JSON) to Kikri's
// data directory and then runs the user's original command with the same
// input, so their status line looks the same. The original statusLine is
// backed up first and restored when the option is turned off.
//
// Only the statusLine key of settings.json is changed; the other keys keep
// their order and values. Kikri never reads credentials.
package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Flag is the command-line flag that runs Relay.
const Flag = "statusline"

// Window is one rate-limit window as Claude Code reports it.
type Window struct {
	UsedPercent *float64  `json:"usedPercent,omitempty"`
	ResetsAt    time.Time `json:"resetsAt"`
}

// Data is what Relay saves.
type Data struct {
	At       time.Time `json:"at"` // when Claude Code last reported rate limits
	FiveHour *Window   `json:"fiveHour,omitempty"`
	SevenDay *Window   `json:"sevenDay,omitempty"`
}

// Paths locates the files involved.
type Paths struct {
	Settings string // Claude Code's settings.json
	Backup   string // the user's original statusLine (Kikri data dir)
	Data     string // the saved rate limits (Kikri data dir)
}

// DefaultPaths uses CLAUDE_CONFIG_DIR (or ~/.claude) and Kikri's data dir.
func DefaultPaths(kikriRoot string) (Paths, error) {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		dir = filepath.Join(home, ".claude")
	}
	return Paths{
		Settings: filepath.Join(dir, "settings.json"),
		Backup:   filepath.Join(kikriRoot, "claude-statusline-backup.json"),
		Data:     filepath.Join(kikriRoot, "claude-rate-limits.json"),
	}, nil
}

// Command is the statusLine command that runs exe as the relay.
func Command(exe string) string {
	if runtime.GOOS == "windows" {
		return `"` + exe + `" -` + Flag
	}
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' -" + Flag
}

// isRelay reports whether a statusLine command is a Kikri relay (from any
// install location).
func isRelay(cmd string) bool {
	return strings.HasSuffix(cmd, " -"+Flag) && strings.Contains(strings.ToLower(cmd), "kikri")
}

type statusLine struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type backup struct {
	// Original is the user's statusLine value, or null when there was none.
	Original json.RawMessage `json:"original"`
	SavedAt  time.Time       `json:"savedAt"`
}

// State reports whether the relay is installed.
type State struct {
	Installed bool `json:"installed"`
	// Stale: installed, but pointing at another Kikri executable (moved app).
	Stale bool `json:"stale,omitempty"`
}

// Status checks settings.json.
func Status(p Paths, exe string) (State, error) {
	keys, err := readObject(p.Settings)
	if err != nil {
		return State{}, err
	}
	raw, ok := keys.get("statusLine")
	if !ok {
		return State{}, nil
	}
	var sl statusLine
	if json.Unmarshal(raw, &sl) != nil || !isRelay(sl.Command) {
		return State{}, nil
	}
	return State{Installed: true, Stale: sl.Command != Command(exe)}, nil
}

// Install puts the relay in front of the user's status line. Re-installing
// only updates the executable path and keeps the original backup.
func Install(p Paths, exe string) error {
	keys, err := readObject(p.Settings)
	if err != nil {
		return err
	}
	raw, had := keys.get("statusLine")
	var sl object
	if had {
		if sl, err = parseObject(raw); err != nil {
			return fmt.Errorf("statusLine in %s is not an object", p.Settings)
		}
	}
	var cur statusLine
	_ = json.Unmarshal(raw, &cur)
	if !isRelay(cur.Command) {
		orig := json.RawMessage("null")
		if had {
			orig = raw
		}
		b := marshal(backup{Original: orig, SavedAt: time.Now()})
		if err := writeAtomic(p.Backup, b, 0o600); err != nil {
			return fmt.Errorf("back up statusLine: %w", err)
		}
	}
	// Keep the user's other statusLine options (padding, refreshInterval...).
	sl.set("type", marshal("command"))
	sl.set("command", marshal(Command(exe)))
	val, _ := sl.MarshalJSON()
	keys.set("statusLine", val)
	return writeObject(p.Settings, keys)
}

// marshal encodes v without escaping &, < and > (json.Marshal would turn a
// user's "a && b" into "a \u0026\u0026 b").
func marshal(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n")
}

// Uninstall restores the original statusLine. If the user has replaced the
// relay with something else meanwhile, settings.json is left alone.
func Uninstall(p Paths) error {
	keys, err := readObject(p.Settings)
	if err != nil {
		return err
	}
	raw, ok := keys.get("statusLine")
	var cur statusLine
	if ok {
		_ = json.Unmarshal(raw, &cur)
	}
	if ok && isRelay(cur.Command) {
		orig, err := readBackup(p.Backup)
		if err != nil {
			return err
		}
		if orig == nil {
			keys.del("statusLine")
		} else {
			keys.set("statusLine", orig)
		}
		if err := writeObject(p.Settings, keys); err != nil {
			return err
		}
	}
	for _, f := range []string{p.Backup, p.Data} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// readBackup returns the original statusLine, nil when there was none.
func readBackup(path string) (json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // nothing to restore: remove the relay
	}
	if err != nil {
		return nil, err
	}
	var bk backup
	if err := json.Unmarshal(b, &bk); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(bk.Original) == 0 || string(bk.Original) == "null" {
		return nil, nil
	}
	return bk.Original, nil
}

// Read returns the saved rate limits, or nil if there are none.
func Read(path string) (*Data, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// input is the part of Claude Code's status line JSON Kikri looks at.
type input struct {
	RateLimits *struct {
		FiveHour *rawWindow `json:"five_hour"`
		SevenDay *rawWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type rawWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *float64 `json:"resets_at"` // Unix seconds
}

func (w *rawWindow) window() *Window {
	if w == nil || w.ResetsAt == nil || *w.ResetsAt <= 0 {
		return nil
	}
	return &Window{UsedPercent: w.UsedPercentage, ResetsAt: time.Unix(int64(*w.ResetsAt), 0)}
}

// Save records the rate limits in one status line input, if it has any.
// A window Claude Code no longer reports (it drops one once it resets) is
// kept from the previous save, so the last reset time stays known.
func Save(path string, in []byte, now time.Time) error {
	var v input
	if err := json.Unmarshal(in, &v); err != nil || v.RateLimits == nil {
		return err
	}
	d := Data{At: now, FiveHour: v.RateLimits.FiveHour.window(), SevenDay: v.RateLimits.SevenDay.window()}
	if prev, _ := Read(path); prev != nil {
		if d.FiveHour == nil {
			d.FiveHour = prev.FiveHour
		}
		if d.SevenDay == nil {
			d.SevenDay = prev.SevenDay
		}
	}
	return writeAtomic(path, marshal(d), 0o600)
}

// Relay is `kikri -statusline`: save the rate limits, then run the user's
// original status line command with the same input. Saving never gets in the
// way of the user's status line. It returns the exit code.
func Relay(p Paths, stdin io.Reader, stdout, stderr io.Writer) int {
	in, _ := io.ReadAll(io.LimitReader(stdin, 4<<20))
	now := time.Now()
	_ = Save(p.Data, in, now)

	// No status line of the user's own: an empty output would leave a blank
	// row, so show a short quota line instead.
	orig, _ := readBackup(p.Backup)
	var sl statusLine
	if orig == nil || json.Unmarshal(orig, &sl) != nil || sl.Command == "" || isRelay(sl.Command) {
		d, _ := Read(p.Data)
		fmt.Fprintln(stdout, Line(d, now))
		return 0
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", sl.Command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", sl.Command)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(in), stdout, stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(stderr, "kikri:", err)
		return 1
	}
	return 0
}

// Line is the status line Kikri shows for users without one of their own,
// e.g. "5h 23% · 18:30 刷新 · 本周 54%".
func Line(d *Data, now time.Time) string {
	var parts []string
	if d != nil && d.FiveHour != nil && d.FiveHour.ResetsAt.After(now) {
		w := d.FiveHour
		if w.UsedPercent != nil {
			parts = append(parts, fmt.Sprintf("5h %.0f%%", *w.UsedPercent))
		}
		parts = append(parts, w.ResetsAt.Local().Format("15:04")+" 刷新")
	}
	if d != nil && d.SevenDay != nil && d.SevenDay.ResetsAt.After(now) && d.SevenDay.UsedPercent != nil {
		parts = append(parts, fmt.Sprintf("本周 %.0f%%", *d.SevenDay.UsedPercent))
	}
	if len(parts) == 0 {
		return "额度：发第一条消息后显示"
	}
	return strings.Join(parts, " · ")
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
