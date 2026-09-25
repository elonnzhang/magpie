package library

import (
	"os"
	"path/filepath"

	"github.com/yetone/magpie/internal/agent"
)

// Target is where one agent keeps each of the three: an empty path is
// something it has no user-wide place for.
type Target struct {
	Agent *agent.Agent
	// Instructions is the file the agent reads before every conversation;
	// Override, when it exists, is read instead of it (Codex's
	// AGENTS.override.md), which the page warns of.
	Instructions, Override string
	MCP                    *mcpFile
	Skills                 string // the folder the agent finds skills in
	// SkillsAlso are agents whose skills this one reads as well, as
	// OpenCode reads Claude Code's.
	SkillsAlso []string
	// MCPVia is the extension the agent reads its MCP servers through, for
	// one that has none of its own.
	MCPVia string
	// Note is what the page says of the agent's instructions file.
	Note string
}

func home() string { h, _ := os.UserHomeDir(); return h }

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".claude")
}

// claudeJSON is where Claude Code keeps its user-wide MCP servers: beside
// its folder, or in it when CLAUDE_CONFIG_DIR moves it.
func claudeJSON() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	return filepath.Join(home(), ".claude.json")
}

func codexDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".codex")
}

// targetOf is where a known agent keeps them, or nil for one magpie can't
// give any of them to.
func targetOf(a *agent.Agent) *Target {
	h := home()
	t := &Target{Agent: a}
	switch a.ID {
	case "claude":
		d := claudeDir()
		t.Instructions = filepath.Join(d, "CLAUDE.md")
		t.MCP = &mcpFile{Path: claudeJSON(), Format: fmtClaude}
		t.Skills = filepath.Join(d, "skills")
	case "codex":
		d := codexDir()
		t.Instructions = filepath.Join(d, "AGENTS.md")
		t.Override = filepath.Join(d, "AGENTS.override.md")
		t.MCP = &mcpFile{Path: filepath.Join(d, "config.toml"), Format: fmtCodex}
		t.Skills = filepath.Join(d, "skills")
	case "gemini":
		d := filepath.Join(h, ".gemini")
		t.Instructions = filepath.Join(d, "GEMINI.md")
		t.MCP = &mcpFile{Path: filepath.Join(d, "settings.json"), Format: fmtGemini}
		t.Skills = filepath.Join(d, "skills")
	case "opencode":
		d := filepath.Dir(a.Path)
		t.Instructions = filepath.Join(d, "AGENTS.md")
		t.Note = "opencode-claude"
		t.MCP = &mcpFile{Path: a.Path, Format: fmtOpenCode}
		t.Skills = filepath.Join(d, "skills")
		t.SkillsAlso = []string{"claude"}
	case "pi":
		d := os.Getenv("PI_CODING_AGENT_DIR")
		if d == "" {
			d = filepath.Join(h, ".pi", "agent")
		}
		t.Instructions = filepath.Join(d, "AGENTS.md")
		// Pi has no MCP of its own: its extensions for it (pi-mcp-adapter,
		// pi-mcp-extension) both read the agent folder's mcp.json
		t.MCP = &mcpFile{Path: filepath.Join(d, "mcp.json"), Format: fmtPi}
		t.MCPVia = "pi-mcp-adapter"
		t.Skills = filepath.Join(d, "skills")
	case "omp":
		d := filepath.Join(h, ".omp", "agent")
		t.Instructions = filepath.Join(d, "AGENTS.md")
		t.Skills = filepath.Join(d, "skills")
	case "goose":
		t.Instructions = filepath.Join(filepath.Dir(a.Path), ".goosehints")
		t.MCP = &mcpFile{Path: a.Path, Format: fmtGoose}
	case "cursor":
		d := filepath.Join(h, ".cursor")
		t.MCP = &mcpFile{Path: filepath.Join(d, "mcp.json"), Format: fmtCursor}
		t.Skills = filepath.Join(d, "skills")
	case "copilot":
		d := os.Getenv("COPILOT_HOME")
		if d == "" {
			d = filepath.Join(h, ".copilot")
		}
		t.Instructions = filepath.Join(d, "copilot-instructions.md")
		t.MCP = &mcpFile{Path: filepath.Join(d, "mcp-config.json"), Format: fmtCopilot}
		t.Skills = filepath.Join(d, "skills")
	case "crush":
		t.Instructions = filepath.Join(filepath.Dir(a.Path), "CRUSH.md")
		t.MCP = &mcpFile{Path: a.Path, Format: fmtCrush}
		t.Skills = filepath.Join(filepath.Dir(a.Path), "skills")
		t.SkillsAlso = []string{"claude"}
	default:
		return nil
	}
	return t
}

// Targets are the agents on this machine that magpie can give any of the
// three to, in the order the rest of magpie lists them.
func Targets() []*Target {
	var out []*Target
	for _, a := range agent.Detected() {
		if t := targetOf(a); t != nil {
			out = append(out, t)
		}
	}
	return out
}

func targetByID(id string) *Target {
	for _, t := range Targets() {
		if t.Agent.ID == id {
			return t
		}
	}
	return nil
}
