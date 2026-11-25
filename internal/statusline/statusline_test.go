package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const userSettings = `{
  "model": "opus",
  "env": {"A": "1"},
  "statusLine": {"type": "command", "command": "echo \"$(cat | wc -c)\" && printf '<ok>'", "padding": 2},
  "permissions": {"allow": ["Bash(ls)"]}
}
`

func paths(t *testing.T, settings string) Paths {
	t.Helper()
	dir := t.TempDir()
	p := Paths{
		Settings: filepath.Join(dir, "claude", "settings.json"),
		Backup:   filepath.Join(dir, "kikri", "claude-statusline-backup.json"),
		Data:     filepath.Join(dir, "kikri", "claude-rate-limits.json"),
	}
	if settings != "" {
		if err := os.MkdirAll(filepath.Dir(p.Settings), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Settings, []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func keysOf(t *testing.T, s string) []string {
	t.Helper()
	o, err := parseObject([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	var ks []string
	for _, m := range o {
		ks = append(ks, m.Key)
	}
	return ks
}

func TestInstallAndUninstallRestoresOriginal(t *testing.T) {
	p := paths(t, userSettings)
	exe := "/Applications/Kikri.app/Contents/MacOS/kikri"
	if err := Install(p, exe); err != nil {
		t.Fatal(err)
	}
	got := read(t, p.Settings)
	if strings.Join(keysOf(t, got), ",") != "model,env,statusLine,permissions" {
		t.Fatalf("key order changed:\n%s", got)
	}
	if strings.Contains(got, `\u00`) {
		t.Fatalf("escaped characters:\n%s", got)
	}
	var s struct {
		StatusLine map[string]any `json:"statusLine"`
	}
	_ = json.Unmarshal([]byte(got), &s)
	if s.StatusLine["command"] != Command(exe, `echo "$(cat | wc -c)" && printf '<ok>'`) || s.StatusLine["padding"] != 2.0 {
		t.Fatalf("statusLine = %v", s.StatusLine)
	}
	if st, _ := Status(p, exe); !st.Installed || st.Stale {
		t.Fatalf("status = %+v", st)
	}
	if fi, _ := os.Stat(p.Settings); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v", fi.Mode())
	}

	// Installing again (e.g. the app moved) keeps the original backup.
	if err := Install(p, "/Other/Kikri.app/Contents/MacOS/kikri"); err != nil {
		t.Fatal(err)
	}
	if st, _ := Status(p, exe); !st.Installed || !st.Stale {
		t.Fatalf("status after move = %+v", st)
	}

	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	var before, after any
	_ = json.Unmarshal([]byte(userSettings), &before)
	_ = json.Unmarshal([]byte(read(t, p.Settings)), &after)
	if !jsonEqual(before, after) {
		t.Fatalf("not restored:\n%s", read(t, p.Settings))
	}
	if _, err := os.Stat(p.Backup); !os.IsNotExist(err) {
		t.Fatal("backup left behind")
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestInstallWithoutStatusLineOrFile(t *testing.T) {
	p := paths(t, "")
	if err := Install(p, "/k/kikri"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(read(t, p.Settings)); got != "{}" {
		t.Fatalf("settings = %s", got)
	}
}

func TestUninstallLeavesUserReplacement(t *testing.T) {
	p := paths(t, userSettings)
	if err := Install(p, "/k/kikri"); err != nil {
		t.Fatal(err)
	}
	replaced := `{"statusLine": {"type": "command", "command": "my-new-line"}}`
	if err := os.WriteFile(p.Settings, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if read(t, p.Settings) != replaced {
		t.Fatal("user's replacement was changed")
	}
}

func TestInvalidSettingsLeftAlone(t *testing.T) {
	p := paths(t, "{ // comment\n}")
	if err := Install(p, "/k/kikri"); err == nil {
		t.Fatal("installed into invalid JSON")
	}
	if read(t, p.Settings) != "{ // comment\n}" {
		t.Fatal("file changed")
	}
}

func TestSaveKeepsDroppedWindow(t *testing.T) {
	p := paths(t, "")
	now := time.Unix(1_800_000_000, 0)
	in := `{"cwd":"/secret","rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1800003600},"seven_day":{"used_percentage":41,"resets_at":1800300000}}}`
	if err := Save(p.Data, []byte(in), now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, p.Data), "secret") {
		t.Fatal("saved more than rate limits")
	}
	// After the reset Claude Code drops five_hour; the last one is kept.
	if err := Save(p.Data, []byte(`{"rate_limits":{"seven_day":{"used_percentage":42,"resets_at":1800300000}}}`), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// No rate_limits yet (before the first response): nothing changes.
	if err := Save(p.Data, []byte(`{"model":{}}`), now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err := Read(p.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !d.At.Equal(now.Add(2*time.Hour)) || d.FiveHour == nil || d.FiveHour.ResetsAt.Unix() != 1800003600 || *d.SevenDay.UsedPercent != 42 {
		t.Fatalf("data = %+v five=%+v", d, d.FiveHour)
	}
}

func TestRelayRunsOriginal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	p := paths(t, `{"statusLine":{"type":"command","command":"cat | wc -c | tr -d ' '; echo line2 >&2; exit 3"}}`)
	if err := Install(p, "/k/kikri"); err != nil {
		t.Fatal(err)
	}
	in := `{"rate_limits":{"five_hour":{"used_percentage":5,"resets_at":1800003600}}}`
	var out, errb bytes.Buffer
	code := Relay(p, strings.NewReader(in), &out, &errb)
	if code != 3 || strings.TrimSpace(out.String()) != strconv.Itoa(len(in)) || strings.TrimSpace(errb.String()) != "line2" {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	if d, _ := Read(p.Data); d == nil || d.FiveHour == nil {
		t.Fatal("rate limits not saved")
	}
}

func TestRelayWithoutOriginalPrintsQuotaLine(t *testing.T) {
	p := paths(t, "{}")
	if err := Install(p, "/k/kikri"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Relay(p, strings.NewReader("{}"), &out, &out); code != 0 || strings.TrimSpace(out.String()) != "额度：发第一条消息后显示" {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	in := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":23.4,"resets_at":%d},"seven_day":{"used_percentage":54,"resets_at":%d}}}`,
		reset.Unix(), reset.Add(72*time.Hour).Unix())
	out.Reset()
	Relay(p, strings.NewReader(in), &out, &out)
	want := "5h 23% · " + reset.Format("15:04") + " 刷新 · 本周 54%"
	if strings.TrimSpace(out.String()) != want {
		t.Fatalf("out=%q want %q", out.String(), want)
	}
}

func TestLineDropsExpiredWindow(t *testing.T) {
	now := time.Now()
	u := 80.0
	d := &Data{FiveHour: &Window{UsedPercent: &u, ResetsAt: now.Add(-time.Minute)}, SevenDay: &Window{UsedPercent: &u, ResetsAt: now.Add(time.Hour)}}
	if got := Line(d, now); got != "本周 80%" {
		t.Fatal(got)
	}
}

func TestCommandQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	if got := Command("/a b/it's/kikri", ""); got != `'/a b/it'\''s/kikri' -statusline` {
		t.Fatal(got)
	}
	if got := Command("/k/kikri", "my-line"); got != `[ -x '/k/kikri' ] && exec '/k/kikri' -statusline; my-line` {
		t.Fatal(got)
	}
	if !isRelay(Command("/Applications/Kikri.app/Contents/MacOS/kikri", "x")) || isRelay("echo hi") {
		t.Fatal("isRelay")
	}
}

// The app deleted without turning the option off: the command falls back to
// the user's own status line, with the same input.
func TestCommandFallsBackWhenAppIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p := paths(t, `{"statusLine":{"type":"command","command":"cat | wc -c | tr -d ' '"}}`)
	gone := filepath.Join(t.TempDir(), "Kikri.app", "kikri")
	if err := Install(p, gone); err != nil {
		t.Fatal(err)
	}
	var s struct {
		StatusLine statusLine `json:"statusLine"`
	}
	_ = json.Unmarshal([]byte(read(t, p.Settings)), &s)
	in := `{"model":{"id":"x"}}`
	cmd := exec.Command("/bin/sh", "-c", s.StatusLine.Command)
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != strconv.Itoa(len(in)) {
		t.Fatalf("out=%q err=%v (command %q)", out, err, s.StatusLine.Command)
	}
	// Re-installing from another path keeps the same fallback.
	if err := Install(p, "/k/kikri"); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(read(t, p.Settings)), &s)
	if !strings.HasSuffix(s.StatusLine.Command, "; cat | wc -c | tr -d ' '") {
		t.Fatal(s.StatusLine.Command)
	}
}
