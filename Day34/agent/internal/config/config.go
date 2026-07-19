// Package config defines the declarative configuration for the LLM agent and
// its orchestrator. A single YAML file turns capabilities on and off: every
// memory layer is enabled by default and can be disabled per-layer, while RAG
// and MCP are opt-in. The orchestrator section declares the roster of sub-agent
// roles the orchestrator LLM may spawn and route between.
//
// Loading merges three sources, lowest priority first:
//  1. Default() — memory on, RAG/MCP off, orchestrator on.
//  2. the YAML file — only the fields it mentions override the defaults, so an
//     omitted `enabled:` keeps a layer on.
//  3. explicit CLI flags (via Overrides) — only flags the user actually set.
package config

import "ai-adv-agent/internal/domain"

// Config is the top-level agent configuration document.
type Config struct {
	LLM          LLMConfig          `yaml:"llm"`
	Memory       MemoryConfig       `yaml:"memory"`
	RAG          RAGConfig          `yaml:"rag"`
	MCP          MCPConfig          `yaml:"mcp"`
	Invariants   InvariantsConfig   `yaml:"invariants"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	Consultant   ConsultantConfig   `yaml:"consultant"`
}

// LLMConfig holds the answer-generating model's connection and generation
// defaults. Empty Provider/Model/BaseURL/APIKey fall back to the LLM_* env vars
// at load time (see ResolveEnv), preserving the historical env-driven flow.
type LLMConfig struct {
	Provider      string   `yaml:"provider"`
	Model         string   `yaml:"model"`
	BaseURL       string   `yaml:"base_url"`
	APIKey        string   `yaml:"api_key"`        // prefer leaving empty → read from LLM_API_KEY
	Temperature   *float64 `yaml:"temperature"`    // nil = provider default
	MaxTokens     int      `yaml:"max_tokens"`     // 0 = no limit
	ContextWindow int      `yaml:"context_window"` // 0 = no client-side cap
	CACert        string   `yaml:"ca_cert"`        // PEM cert for a self-signed HTTPS endpoint
}

// MemoryConfig toggles the memory layers. Dir is the base path from which each
// layer's file path is derived (equivalent to the historical --history value).
type MemoryConfig struct {
	Dir        string     `yaml:"dir"`
	STM        STMConfig  `yaml:"stm"`
	WM         Toggle     `yaml:"wm"`
	LTM        Toggle     `yaml:"ltm"`
	Facts      Toggle     `yaml:"facts"`
	Profile    ProfileCfg `yaml:"profile"`
	TaskMemory Toggle     `yaml:"task_memory"`
	// AutoUpdate refreshes WM and LTM with an extra LLM call after each turn.
	AutoUpdate bool `yaml:"auto_update"`
}

// Toggle is a simple on/off switch for a capability.
type Toggle struct {
	Enabled bool `yaml:"enabled"`
}

// STMConfig configures Layer 1 short-term memory (dialogue history).
type STMConfig struct {
	Enabled    bool   `yaml:"enabled"`
	History    string `yaml:"history"`     // explicit path; empty = derived from Memory.Dir
	Limit      int    `yaml:"limit"`       // max messages kept (0 = unlimited)
	Summary    bool   `yaml:"summary"`     // summarize older turns past Limit
	Strategy   string `yaml:"strategy"`    // "" | sliding-window | sticky-facts | branching
	WindowSize int    `yaml:"window_size"` // recent messages in prompt for window strategies
}

// ProfileCfg configures the explicit user profile layer.
type ProfileCfg struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // empty = derived from Memory.Dir
}

// RAGConfig configures the retrieval-augmented pipeline (disabled by default).
type RAGConfig struct {
	Enabled    bool         `yaml:"enabled"`
	DB         string       `yaml:"db"`
	EmbedURL   string       `yaml:"embed_url"`
	EmbedModel string       `yaml:"embed_model"`
	EmbedKey   string       `yaml:"embed_key"`
	TopK       int          `yaml:"top_k"`       // retrieval pool size before filtering
	Threshold  float64      `yaml:"threshold"`   // drop chunks scoring below this (0 disables)
	TopKFinal  int          `yaml:"top_k_final"` // chunks kept after rerank/filter (0 = all)
	Rerank     RerankConfig `yaml:"rerank"`
}

// RerankConfig configures the optional rerank stage. Empty Model/Provider/URL/Key
// fall back to the main LLM at wiring time.
type RerankConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Model    string `yaml:"model"`
	Mode     string `yaml:"mode"` // api | chat | auto
	Provider string `yaml:"provider"`
	URL      string `yaml:"url"`
	Key      string `yaml:"key"`
}

// MCPConfig configures Model Context Protocol tool servers (disabled by default).
// Servers may be listed inline or loaded from an external YAML file via File.
type MCPConfig struct {
	Enabled bool                     `yaml:"enabled"`
	File    string                   `yaml:"file"` // optional path to an external MCP servers YAML
	Servers []domain.MCPServerConfig `yaml:"servers"`
	FSGuard FSGuardConfig            `yaml:"fs_guard"`
}

// FSGuardConfig configures the client-side file-access guard applied to the MCP
// servers named in Servers. It is enabled by default: forgetting to configure it
// must mean "forbidden", never "allowed".
type FSGuardConfig struct {
	Enabled bool `yaml:"enabled"`
	// ReadRoot is the root every relative path is addressed against. Empty
	// resolves to the config file's directory (see ResolvePaths).
	ReadRoot string `yaml:"read_root"`
	// Deny holds glob patterns; empty means policy.DefaultDenyGlobs().
	Deny []string `yaml:"deny"`
	// Servers lists the MCP server names whose tools the guard applies to.
	// A tool from one of these servers with no entry in the policy table is
	// refused (fail-closed). Empty means the guard governs no server.
	Servers []string `yaml:"servers"`
}

// ToolPolicy gates a role's tools by name and declares its write root. The zero
// value is read-only: no allow list restriction, no denials, and — because
// WriteRoot is empty — no write access at all (fail-closed).
type ToolPolicy struct {
	// Allow lists tool names; empty means every tool of the granted servers.
	Allow []string `yaml:"allow"`
	// Deny always beats Allow.
	Deny []string `yaml:"deny"`
	// WriteRoot is the only subtree the role may write into; empty forbids writes.
	WriteRoot string `yaml:"write_root"`
	// ReadRoot overrides mcp.fs_guard.read_root for this role; empty inherits it.
	ReadRoot string `yaml:"read_root"`
}

// InvariantsConfig loads absolute constraints the agent must never violate.
type InvariantsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // empty = derived from Memory.Dir
}

// OrchestratorConfig declares the orchestrator behaviour and its sub-agent roster.
type OrchestratorConfig struct {
	Enabled   bool             `yaml:"enabled"`
	MaxRounds int              `yaml:"max_rounds"` // safety cap on spawn/route cycles
	SubAgents []SubAgentConfig `yaml:"subagents"`
}

// SubAgentConfig defines one sub-agent role the orchestrator may spawn. Prompt is
// the role's system instruction. RAG/MCP grant the role access to those tools;
// MCP lists the server names visible to the role ("*" = all, empty = none).
// Tools narrows the granted servers' tools by name and sets the role's write
// root; an omitted `tools:` section leaves the role read-only.
type SubAgentConfig struct {
	Name   string         `yaml:"name"`
	Prompt string         `yaml:"prompt"`
	RAG    bool           `yaml:"rag"`
	MCP    []string       `yaml:"mcp"`
	Tools  ToolPolicy     `yaml:"tools"`
	Memory SubAgentMemory `yaml:"memory"`
}

// SubAgentMemory toggles a sub-agent's own ephemeral working memory. STM+WM are
// scoped to the sub-agent run; LTM/profile are shared read-only from the session.
type SubAgentMemory struct {
	WM bool `yaml:"wm"`
}

// ConsultantConfig configures the documentation-consultant mode, entered with
// /help in the interactive orchestrator and left with /end. The consultant
// answers questions about the project grounded on the documentation RAG index
// (an independent pipeline from the main `rag:` section — it points at docs.db)
// and may call MCP tools from its allow-list (e.g. the git server).
type ConsultantConfig struct {
	Enabled bool      `yaml:"enabled"`
	RAG     RAGConfig  `yaml:"rag"`    // docs index; its Enabled gates retrieval
	MCP     []string   `yaml:"mcp"`    // MCP server allow-list, e.g. ["git"]
	Tools   ToolPolicy `yaml:"tools"`  // per-tool gating; zero value = read-only
	Prompt  string     `yaml:"prompt"` // optional system-prompt override
}

// Default returns a Config with every memory layer enabled, RAG and MCP disabled,
// and the orchestrator enabled with an empty roster. YAML and flags layer on top.
func Default() Config {
	return Config{
		LLM: LLMConfig{
			// Provider/Model/BaseURL/APIKey resolved from env when left empty.
		},
		Memory: MemoryConfig{
			Dir:        "chat_history.json",
			STM:        STMConfig{Enabled: true, Limit: 10, WindowSize: 5},
			WM:         Toggle{Enabled: true},
			LTM:        Toggle{Enabled: true},
			Facts:      Toggle{Enabled: true},
			Profile:    ProfileCfg{Enabled: true},
			TaskMemory: Toggle{Enabled: true},
			AutoUpdate: false,
		},
		RAG: RAGConfig{
			Enabled:    false,
			DB:         "rag.db",
			EmbedURL:   "http://localhost:11434",
			EmbedModel: "nomic-embed-text",
			TopK:       10,
			TopKFinal:  5,
			Rerank:     RerankConfig{Mode: "auto"},
		},
		// The guard defaults to on: an unconfigured fs_guard must forbid, not permit.
		MCP:        MCPConfig{Enabled: false, FSGuard: FSGuardConfig{Enabled: true}},
		Invariants: InvariantsConfig{Enabled: false},
		Orchestrator: OrchestratorConfig{
			Enabled:   true,
			MaxRounds: 8,
		},
		Consultant: ConsultantConfig{
			Enabled: true,
			RAG: RAGConfig{
				Enabled:    true,
				DB:         "../rag/docs.db",
				EmbedURL:   "http://localhost:11434",
				EmbedModel: "nomic-embed-text",
				TopK:       12,
				Threshold:  0.35,
				TopKFinal:  6,
				Rerank:     RerankConfig{Mode: "auto"},
			},
			MCP: []string{"git"},
		},
	}
}
