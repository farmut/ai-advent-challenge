package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_MemoryAllEnabled(t *testing.T) {
	c := Default()
	if !c.Memory.STM.Enabled || !c.Memory.WM.Enabled || !c.Memory.LTM.Enabled ||
		!c.Memory.Facts.Enabled || !c.Memory.Profile.Enabled || !c.Memory.TaskMemory.Enabled {
		t.Fatalf("all memory layers must default to enabled, got %+v", c.Memory)
	}
	if c.RAG.Enabled || c.MCP.Enabled {
		t.Fatalf("RAG and MCP must default to disabled")
	}
	if !c.Orchestrator.Enabled {
		t.Fatalf("orchestrator must default to enabled")
	}
}

func TestLoad_EmptyPathReturnsDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Memory.LTM.Enabled {
		t.Fatalf("empty path must yield defaults")
	}
}

// A YAML that only disables one layer and enables RAG must leave every other
// layer at its default (enabled) — the merge-into-defaults contract.
func TestLoad_PartialYAMLMergesOntoDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	yaml := `
memory:
  ltm:
    enabled: false
rag:
  enabled: true
  db: custom.db
orchestrator:
  subagents:
    - name: researcher
      prompt: "find facts"
      rag: true
    - name: coder
      prompt: "write code"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Memory.LTM.Enabled {
		t.Errorf("ltm should be disabled by YAML")
	}
	if !c.Memory.STM.Enabled || !c.Memory.WM.Enabled || !c.Memory.Profile.Enabled {
		t.Errorf("untouched layers must stay enabled, got %+v", c.Memory)
	}
	if !c.RAG.Enabled || c.RAG.DB != "custom.db" {
		t.Errorf("rag override not applied: %+v", c.RAG)
	}
	if c.RAG.TopK != 10 {
		t.Errorf("rag.top_k default must survive partial YAML, got %d", c.RAG.TopK)
	}
	if len(c.Orchestrator.SubAgents) != 2 || c.Orchestrator.SubAgents[0].Name != "researcher" {
		t.Errorf("subagents not parsed: %+v", c.Orchestrator.SubAgents)
	}
	if !c.Orchestrator.Enabled {
		t.Errorf("orchestrator.enabled default must survive")
	}
}

func TestResolveEnv_FillsFromEnv(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_API_KEY", "sk-test")
	t.Setenv("LLM_MODEL", "gpt-4o")
	t.Setenv("LLM_BASE_URL", "https://example.test/v1/")
	c := Default()
	ResolveEnv(&c)
	if c.LLM.Provider != "openai" || c.LLM.APIKey != "sk-test" || c.LLM.Model != "gpt-4o" {
		t.Fatalf("env not resolved: %+v", c.LLM)
	}
	if c.LLM.BaseURL != "https://example.test/v1" {
		t.Errorf("base url trailing slash not trimmed: %q", c.LLM.BaseURL)
	}
}

func TestResolveEnv_ConfigWinsOverEnv(t *testing.T) {
	t.Setenv("LLM_MODEL", "env-model")
	c := Default()
	c.LLM.Model = "config-model"
	ResolveEnv(&c)
	if c.LLM.Model != "config-model" {
		t.Fatalf("config value must win over env, got %q", c.LLM.Model)
	}
}

func TestValidate(t *testing.T) {
	base := func() Config {
		c := Default()
		c.LLM.Provider = "openai"
		c.LLM.APIKey = "sk"
		return c
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	c := base()
	c.LLM.Provider = ""
	if err := Validate(c); err == nil {
		t.Error("missing provider must fail")
	}

	c = base()
	c.LLM.APIKey = ""
	if err := Validate(c); err == nil {
		t.Error("missing api key must fail")
	}

	c = base()
	c.LLM.Provider = "anthropic"
	if err := Validate(c); err == nil {
		t.Error("unsupported provider must fail")
	}

	c = base()
	c.Memory.STM.Strategy = "bogus"
	if err := Validate(c); err == nil {
		t.Error("invalid strategy must fail")
	}

	c = base()
	c.Orchestrator.SubAgents = []SubAgentConfig{{Name: "a"}, {Name: "a"}}
	if err := Validate(c); err == nil {
		t.Error("duplicate subagent names must fail")
	}

	c = base()
	c.Orchestrator.SubAgents = []SubAgentConfig{{Name: ""}}
	if err := Validate(c); err == nil {
		t.Error("empty subagent name must fail")
	}
}

func TestOverrides_OnlyNonNilApplied(t *testing.T) {
	c := Default()
	origDB := c.RAG.DB
	model := "override-model"
	ragOn := true
	o := Overrides{
		Model:      &model,
		RAGEnabled: &ragOn,
		// RAGDB left nil → must not change
	}
	o.Apply(&c)
	if c.LLM.Model != "override-model" {
		t.Errorf("model override not applied")
	}
	if !c.RAG.Enabled {
		t.Errorf("rag enable override not applied")
	}
	if c.RAG.DB != origDB {
		t.Errorf("nil override must not change db, got %q", c.RAG.DB)
	}
}

func TestOverrides_DisableLayer(t *testing.T) {
	c := Default()
	off := false
	o := Overrides{LTMEnabled: &off}
	o.Apply(&c)
	if c.Memory.LTM.Enabled {
		t.Fatalf("override must be able to disable a layer")
	}
	if !c.Memory.WM.Enabled {
		t.Fatalf("other layers must be untouched")
	}
}
