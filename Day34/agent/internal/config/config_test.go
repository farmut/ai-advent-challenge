package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
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

// --- fs guard / tool policy ---

func TestDefault_FSGuardEnabled(t *testing.T) {
	c := Default()
	if !c.MCP.FSGuard.Enabled {
		t.Fatal("fs_guard must default to enabled: forgetting to configure it means forbidden, not allowed")
	}
	if c.MCP.FSGuard.ReadRoot != "" || len(c.MCP.FSGuard.Servers) != 0 {
		t.Fatalf("fs_guard must default to no roots and no guarded servers, got %+v", c.MCP.FSGuard)
	}
}

func TestLoad_ToolPolicyParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	yaml := `
mcp:
  enabled: true
  fs_guard:
    read_root: ".."
    servers: ["fs"]
    deny: [".env*"]
orchestrator:
  subagents:
    - name: coder
      mcp: ["fs"]
      tools:
        allow: ["read_text_file", "write_file"]
        deny: ["move_file"]
        write_root: "../workspace"
consultant:
  mcp: ["git"]
  tools:
    allow: ["read_text_file"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.MCP.FSGuard.ReadRoot != ".." || len(c.MCP.FSGuard.Servers) != 1 || c.MCP.FSGuard.Servers[0] != "fs" {
		t.Fatalf("fs_guard not parsed: %+v", c.MCP.FSGuard)
	}
	if len(c.MCP.FSGuard.Deny) != 1 || c.MCP.FSGuard.Deny[0] != ".env*" {
		t.Fatalf("fs_guard.deny not parsed: %+v", c.MCP.FSGuard.Deny)
	}
	sa := c.Orchestrator.SubAgents[0]
	if len(sa.Tools.Allow) != 2 || sa.Tools.Allow[1] != "write_file" {
		t.Fatalf("tools.allow not parsed: %+v", sa.Tools)
	}
	if len(sa.Tools.Deny) != 1 || sa.Tools.Deny[0] != "move_file" {
		t.Fatalf("tools.deny not parsed: %+v", sa.Tools)
	}
	if sa.Tools.WriteRoot != "../workspace" {
		t.Fatalf("tools.write_root not parsed: %q", sa.Tools.WriteRoot)
	}
	if len(c.Consultant.Tools.Allow) != 1 {
		t.Fatalf("consultant tools not parsed: %+v", c.Consultant.Tools)
	}
}

// A role that predates the tools section must keep working and stay read-only:
// an empty policy means "every tool of the granted servers, no write root".
func TestLoad_RoleWithoutToolsIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	yaml := `
orchestrator:
  subagents:
    - name: researcher
      mcp: ["git"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tp := c.Orchestrator.SubAgents[0].Tools
	if tp.WriteRoot != "" {
		t.Fatalf("a role without a tools section must have no write root, got %q", tp.WriteRoot)
	}
	if len(tp.Allow) != 0 || len(tp.Deny) != 0 {
		t.Fatalf("a role without a tools section must have empty allow/deny, got %+v", tp)
	}
}

func TestValidate_AllowDenyOverlap(t *testing.T) {
	base := func() Config {
		c := Default()
		c.LLM.Provider = ProviderOpenAI
		c.LLM.APIKey = "sk-test"
		return c
	}

	c := base()
	c.Orchestrator.SubAgents = []SubAgentConfig{{
		Name:  "coder",
		Tools: ToolPolicy{Allow: []string{"read_text_file", "edit_file"}, Deny: []string{"edit_file"}, WriteRoot: "/tmp/ws"},
	}}
	err := Validate(c)
	if err == nil {
		t.Fatal("expected an error when a tool is both allowed and denied")
	}
	if !strings.Contains(err.Error(), "edit_file") {
		t.Errorf("the error must name the offending tool, got: %v", err)
	}

	// A write tool granted without a write root is a permission the guard could
	// never honour — reject it at load time, not at the first tool call.
	c = base()
	c.Orchestrator.SubAgents = []SubAgentConfig{{
		Name:  "coder",
		Tools: ToolPolicy{Allow: []string{"write_file"}},
	}}
	if err := Validate(c); err == nil {
		t.Fatal("expected an error for a write tool with an empty write_root")
	}

	// The same rule applies to the consultant.
	c = base()
	c.Consultant.Tools = ToolPolicy{Allow: []string{"read_text_file"}, Deny: []string{"read_text_file"}}
	if err := Validate(c); err == nil {
		t.Fatal("expected the consultant's tool policy to be validated too")
	}

	// Read-only grants stay valid.
	c = base()
	c.Orchestrator.SubAgents = []SubAgentConfig{{
		Name:  "researcher",
		Tools: ToolPolicy{Allow: []string{"read_text_file", "grep"}},
	}}
	if err := Validate(c); err != nil {
		t.Errorf("a read-only grant must validate, got: %v", err)
	}
}

// ResolvePaths must resolve roots against the config file's directory, never
// against the process working directory — the agent may be launched anywhere.
func TestResolvePaths_RelativeToConfigDirNotCwd(t *testing.T) {
	tmp := t.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	projectDir := filepath.Join(tmp, "project")
	agentDir := filepath.Join(projectDir, "agent")
	workspace := filepath.Join(projectDir, "workspace")
	elsewhere := filepath.Join(tmp, "elsewhere")
	for _, d := range []string{agentDir, workspace, elsewhere} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfgPath := filepath.Join(agentDir, "agent.config.yaml")
	if err := os.WriteFile(cfgPath, []byte("mcp:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run from a directory that has nothing to do with the config.
	t.Chdir(elsewhere)

	cfg := Default()
	cfg.MCP.FSGuard.ReadRoot = ".."
	cfg.MCP.Servers = []domain.MCPServerConfig{{
		Name:    "fs",
		Type:    domain.MCPStdio,
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem@0.2.0", TokenReadRoot},
	}, {
		Name:    "search",
		Type:    domain.MCPStdio,
		Command: "fff-mcp",
		Args:    []string{TokenReadRoot, "--no-update-check"},
	}}
	cfg.Orchestrator.SubAgents = []SubAgentConfig{
		{Name: "coder", Tools: ToolPolicy{WriteRoot: "../workspace"}},
		{Name: "researcher"},
	}

	if err := ResolvePaths(&cfg, cfgPath); err != nil {
		t.Fatal(err)
	}

	if cfg.MCP.FSGuard.ReadRoot != projectDir {
		t.Errorf("read_root = %q, want %q (resolved from the config dir, not cwd)", cfg.MCP.FSGuard.ReadRoot, projectDir)
	}
	if got := cfg.Orchestrator.SubAgents[0].Tools.WriteRoot; got != workspace {
		t.Errorf("coder write_root = %q, want %q", got, workspace)
	}
	if got := cfg.Orchestrator.SubAgents[1].Tools.WriteRoot; got != "" {
		t.Errorf("a role without a write root must keep it empty, got %q", got)
	}
	if got := cfg.MCP.Servers[0].Args[2]; got != projectDir {
		t.Errorf("${READ_ROOT} not substituted: %q, want %q", got, projectDir)
	}
	if got := cfg.MCP.Servers[1].Args[0]; got != projectDir {
		t.Errorf("${READ_ROOT} not substituted in the search server args: %q", got)
	}
	for _, s := range cfg.MCP.Servers {
		for _, a := range s.Args {
			if strings.Contains(a, "${") {
				t.Errorf("server %q kept an unsubstituted token: %q", s.Name, a)
			}
		}
	}
}

// ${WRITE_ROOT} resolves to the role write root, and an empty read_root falls
// back to the config's own directory.
func TestResolvePaths_WriteRootTokenAndDefaults(t *testing.T) {
	tmp := t.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	ws := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmp, "agent.config.yaml")

	cfg := Default()
	cfg.MCP.Servers = []domain.MCPServerConfig{{
		Name: "fs",
		Args: []string{TokenWriteRoot},
	}}
	cfg.Orchestrator.SubAgents = []SubAgentConfig{{Name: "coder", Tools: ToolPolicy{WriteRoot: "workspace"}}}

	if err := ResolvePaths(&cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.FSGuard.ReadRoot != tmp {
		t.Errorf("an empty read_root must fall back to the config dir, got %q", cfg.MCP.FSGuard.ReadRoot)
	}
	if got := cfg.MCP.Servers[0].Args[0]; got != ws {
		t.Errorf("${WRITE_ROOT} = %q, want %q", got, ws)
	}
}

// A root that does not exist yet (workspace/ is created by the Makefile) must
// not break loading: it is kept absolute and refused later by the guard.
func TestResolvePaths_MissingDirectoryIsNotAnError(t *testing.T) {
	tmp := t.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	cfg := Default()
	cfg.Orchestrator.SubAgents = []SubAgentConfig{{Name: "coder", Tools: ToolPolicy{WriteRoot: "not-created-yet"}}}
	if err := ResolvePaths(&cfg, filepath.Join(tmp, "agent.config.yaml")); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Orchestrator.SubAgents[0].Tools.WriteRoot; got != filepath.Join(tmp, "not-created-yet") {
		t.Errorf("write_root = %q, want the absolute path of a missing dir", got)
	}
}
