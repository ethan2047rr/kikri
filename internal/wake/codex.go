package wake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethan2047rr/kikri/internal/appdir"
)

// DefaultCodexDisable lists the Codex features switched off during a wake.
// Each removes tool definitions from the prompt (about 24% fewer tokens);
// unbounded_connection_retries must be off or Codex retries forever when
// offline. The list depends on the Codex version (tested on 0.171.0), so it
// is configurable and should be re-checked on each Codex release.
var DefaultCodexDisable = []string{
	"shell_tool", "unified_exec", "apps",
	"browser_use", "browser_use_external", "computer_use",
	"image_generation", "multi_agent", "plugins",
	"skill_search", "view_image", "hooks",
	"goals", "tool_suggest", "sleep_tool", "in_app_browser",
	"unbounded_connection_retries",
}

// DefaultCodexConfig holds extra `-c key=value` overrides. project_doc_max_bytes=0
// stops AGENTS.md in the working directory from loading (about 3.5k tokens);
// the include_* switches trim another ~260 tokens (7,906 -> 7,644).
var DefaultCodexConfig = []string{
	`model_reasoning_effort="low"`,
	"project_doc_max_bytes=0",
	"include_apps_instructions=false",
	"include_apps_usage_instructions=false",
	"include_collaboration_mode_instructions=false",
	"include_permissions_instructions=false",
	"include_plugin_usage_instructions=false",
	"include_skills_usage_instructions=false",
}

// Codex wakes Codex CLI without user config, rules or MCP servers
// (docs/spike/m0-step2, variant C: 7,906 input tokens, 11.4s).
type Codex struct {
	Disable []string // nil means DefaultCodexDisable
	Config  []string // nil means DefaultCodexConfig
}

func (Codex) ID() string { return "codex" }

// Hosts covers ChatGPT login (chatgpt.com) and API-key login (api.openai.com).
func (Codex) Hosts() []string { return []string{"chatgpt.com", "api.openai.com"} }

func (c Codex) Command(l appdir.Layout) (args, env []string) {
	args = []string{
		"exec",
		"--skip-git-repo-check", "--ephemeral",
		// Skips config.toml (and the MCP servers in it) and execpolicy rules.
		"--ignore-user-config", "--ignore-rules",
		"--sandbox", "read-only", "--json",
		"-C", l.Sandbox,
		// Replaces the built-in base instructions: the biggest token saving.
		"-c", "model_instructions_file=" + tomlString(l.CodexInstructions),
	}
	cfg := c.Config
	if cfg == nil {
		cfg = DefaultCodexConfig
	}
	for _, kv := range cfg {
		args = append(args, "-c", kv)
	}
	dis := c.Disable
	if dis == nil {
		dis = DefaultCodexDisable
	}
	for _, f := range dis {
		args = append(args, "--disable", f)
	}
	return append(args, "reply ok"), nil
}

// tomlString quotes s as a TOML basic string (Windows paths have backslashes).
func tomlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

type codexEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

// Parse reads `codex exec --json` JSONL: turn.completed means success,
// turn.failed carries the error message.
func (Codex) Parse(stdout []byte) parsed {
	var p parsed
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev codexEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				p.reply = strings.TrimSpace(ev.Item.Text)
			}
		case "turn.completed":
			p.terminal, p.outcome, p.message = true, OutcomeOK, ""
			if u := ev.Usage; u != nil {
				// Codex's input_tokens already includes the cached part.
				p.usage = &Usage{InputTokens: u.InputTokens, CachedInputTokens: u.CachedInputTokens, OutputTokens: u.OutputTokens}
			}
		case "turn.failed":
			p.terminal = true
			msg := ev.Message
			if ev.Error != nil {
				msg = firstNonEmpty(ev.Error.Message, msg)
			}
			p.message = firstNonEmpty(msg, "Codex reported turn.failed without a message")
			p.outcome = classifyMessage(p.message)
		case "error":
			msg := ev.Message
			if ev.Error != nil {
				msg = firstNonEmpty(ev.Error.Message, msg)
			}
			if msg != "" {
				p.lastErr = msg
			}
		}
	}
	if p.outcome != OutcomeOK {
		p.reply = ""
	}
	return p
}

// CodexGlobalAgents reports the global AGENTS.md that Codex loads on every
// run; no flag can skip it (PRD F2.4). It only stats the file and never reads
// its content. ok is false when there is none.
func CodexGlobalAgents() (path string, size int64, ok bool) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", 0, false
		}
		home = filepath.Join(h, ".codex")
	}
	// Codex prefers AGENTS.override.md when it exists.
	for _, name := range []string{"AGENTS.override.md", "AGENTS.md"} {
		p := filepath.Join(home, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return p, fi.Size(), true
		}
	}
	return "", 0, false
}

// AgentsWarnBytes is the size above which the UI warns about AGENTS.md cost.
const AgentsWarnBytes = 4 * 1024

// EstimateTokens is a rough bytes-to-tokens conversion for the UI hint.
func EstimateTokens(bytes int64) int64 { return (bytes + 3) / 4 }
