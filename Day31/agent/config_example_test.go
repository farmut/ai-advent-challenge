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
