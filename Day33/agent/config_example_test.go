package main

import (
	"testing"

	"ai-adv-agent/internal/config"
)

// TestExampleConfigIsValid guards the shipped agent.config.yaml: it must parse,
// keep all memory layers enabled, expose the three-role roster, and pass
// validation once credentials are present.
func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := config.Load("agent.config.yaml")
	if err != nil {
		t.Fatalf("example config failed to load: %v", err)
	}

	if !cfg.Memory.STM.Enabled || !cfg.Memory.WM.Enabled || !cfg.Memory.LTM.Enabled ||
		!cfg.Memory.Facts.Enabled || !cfg.Memory.Profile.Enabled || !cfg.Memory.TaskMemory.Enabled {
		t.Errorf("all memory layers must be enabled by default: %+v", cfg.Memory)
	}

	if len(cfg.Orchestrator.SubAgents) != 3 {
		t.Fatalf("expected 3 sub-agents, got %d", len(cfg.Orchestrator.SubAgents))
	}
	if cfg.Orchestrator.SubAgents[0].Name != "researcher" || !cfg.Orchestrator.SubAgents[0].RAG {
		t.Errorf("researcher role misconfigured: %+v", cfg.Orchestrator.SubAgents[0])
	}

	// Provider/key come from the environment; simulate them for validation.
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_API_KEY", "sk-test")
	config.ResolveEnv(&cfg)
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("example config failed validation: %v", err)
	}
}

// TestReviewConfigIsValid guards the CI review config (agent.review.yaml, used
// by .github/workflows/pr-agent-review.yml): it must parse, disable every
// persistent memory layer (CI runs are ephemeral), wire the github MCP server,
// expose the five-role review roster with correct tool gating, and validate.
func TestReviewConfigIsValid(t *testing.T) {
	cfg, err := config.Load("agent.review.yaml")
	if err != nil {
		t.Fatalf("review config failed to load: %v", err)
	}

	if cfg.Memory.STM.Enabled || cfg.Memory.WM.Enabled || cfg.Memory.LTM.Enabled ||
		cfg.Memory.Facts.Enabled || cfg.Memory.Profile.Enabled || cfg.Memory.TaskMemory.Enabled {
		t.Errorf("CI review config must disable all memory layers: %+v", cfg.Memory)
	}
	if cfg.RAG.Enabled || cfg.Consultant.Enabled {
		t.Error("RAG and consultant must be disabled in the CI review config")
	}

	if !cfg.MCP.Enabled || len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "github" {
		t.Fatalf("expected the single github MCP server, got %+v", cfg.MCP)
	}

	want := map[string][]string{
		"diff-reader":   {"github"},
		"bug-hunter":    {},
		"architect":     {},
		"code-reviewer": {},
		"publisher":     {"github"},
	}
	if len(cfg.Orchestrator.SubAgents) != len(want) {
		t.Fatalf("expected %d sub-agents, got %d", len(want), len(cfg.Orchestrator.SubAgents))
	}
	for _, sa := range cfg.Orchestrator.SubAgents {
		allow, ok := want[sa.Name]
		if !ok {
			t.Errorf("unexpected sub-agent %q", sa.Name)
			continue
		}
		if len(sa.MCP) != len(allow) {
			t.Errorf("%s: MCP allow-list = %v, want %v", sa.Name, sa.MCP, allow)
		}
		if sa.RAG {
			t.Errorf("%s: RAG must be off in the review roster", sa.Name)
		}
	}

	t.Setenv("LLM_PROVIDER", "openrouter")
	t.Setenv("LLM_API_KEY", "sk-test")
	t.Setenv("LLM_MODEL", "openai/gpt-4o")
	config.ResolveEnv(&cfg)
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("review config failed validation: %v", err)
	}
}
