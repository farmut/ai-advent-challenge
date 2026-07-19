//go:build livefsguard

// Temporary end-to-end validation: drives the REAL fs MCP server (npx) through
// the real Grant/guard path and checks that the permission model holds on live
// traffic, not only in unit tests. Run with:
//
//	go test -tags livefsguard -run TestLiveFSGuard -v ./internal/app/
package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
)

func TestLiveFSGuard(t *testing.T) {
	cfgPath, err := filepath.Abs("../../agent.config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := config.ResolvePaths(&cfg, cfgPath); err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	// Only the fs server is needed for this check.
	var servers []domain.MCPServerConfig
	for _, s := range cfg.MCP.Servers {
		if s.Name == "fs" {
			servers = append(servers, s)
		}
	}
	if len(servers) != 1 {
		t.Fatalf("expected exactly one fs server in the shipped config, got %d", len(servers))
	}
	cfg.MCP.Servers = servers

	tb := &Toolbelt{Cfg: cfg}
	if err := tb.buildMCP(cfg.MCP); err != nil {
		t.Fatalf("build mcp: %v", err)
	}
	defer tb.Close()
	if len(tb.MCPTools) == 0 {
		t.Fatal("no tools from the live fs server")
	}
	t.Logf("live fs server exposed %d tools", len(tb.MCPTools))

	// Regression: server-filesystem@2025.8.21 advertises the right 14 tool NAMES
	// but with an empty inputSchema — no properties, no required. Checking names
	// alone passed, while the model was being told these tools take no arguments;
	// it then called read_text_file with no path and could neither read nor write.
	// Names are not a contract — the schema is.
	for _, tool := range tb.MCPTools {
		if tb.ToolRouting[tool.Name].Name != "fs" {
			continue
		}
		spec, known := policy.DefaultSpecs()[tool.Name]
		if !known || len(spec.PathArgs) == 0 {
			continue // tools we do not gate on a path (list_allowed_directories)
		}
		schema, _ := tool.InputSchema.(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		if len(props) == 0 {
			t.Errorf("tool %q has an EMPTY inputSchema — the model cannot know it takes arguments; "+
				"check the pinned server-filesystem version", tool.Name)
			continue
		}
		for _, arg := range spec.PathArgs {
			if _, ok := props[arg]; !ok {
				t.Errorf("tool %q schema lacks the %q property the guard gates on; schema and policy table disagree",
					tool.Name, arg)
			}
		}
	}

	// Roles straight out of the shipped config.
	var coder, researcher config.SubAgentConfig
	for _, sa := range cfg.Orchestrator.SubAgents {
		switch sa.Name {
		case "coder":
			coder = sa
		case "researcher":
			researcher = sa
		}
	}
	if coder.Name == "" || researcher.Name == "" {
		t.Fatal("coder/researcher roles missing from the shipped config")
	}

	coderGrant := Grant{Servers: coder.MCP, Tools: coder.Tools, Role: coder.Name}
	researcherGrant := Grant{Servers: researcher.MCP, Tools: researcher.Tools, Role: researcher.Name}

	exec := tb.ToolExecutorForGrant(coderGrant)
	ctx := context.Background()

	// Requirement 4: create a file inside the write root.
	out, err := exec(ctx, "write_file", `{"path":"workspace/live_probe.md","content":"probe-v1\n"}`)
	if err != nil {
		t.Fatalf("step 9 FAILED: write inside workspace must be allowed, got: %v", err)
	}
	t.Logf("step 9 ok: %s", strings.TrimSpace(out))

	probe := filepath.Join(filepath.Dir(cfgPath), "..", "workspace", "live_probe.md")
	if b, rerr := os.ReadFile(probe); rerr != nil || !strings.Contains(string(b), "probe-v1") {
		t.Fatalf("step 9 FAILED: file not actually written (err=%v)", rerr)
	}
	defer os.Remove(probe)

	// Requirement 5: edit that file.
	if _, err := exec(ctx, "edit_file",
		`{"path":"workspace/live_probe.md","edits":[{"oldText":"probe-v1","newText":"probe-v2"}]}`); err != nil {
		t.Fatalf("step 10 FAILED: edit inside workspace must be allowed, got: %v", err)
	}
	if b, _ := os.ReadFile(probe); !strings.Contains(string(b), "probe-v2") {
		t.Fatal("step 10 FAILED: edit did not change the file")
	}
	t.Log("step 10 ok: edit applied")

	// Regression (agent-tui.log): the reviewer took file paths from the git tools,
	// which report them from the repository root, so they arrived as
	// "Day34/workspace/bot.py". Against a read root of Day34 that resolved to
	// Day34/Day34/... and was refused every single time, and the sub-agent spent
	// its whole round budget retrying. These must now succeed.
	gitStyle := []struct{ tool, args string }{
		{"read_text_file", `{"path":"Day34/workspace/live_probe.md"}`},
		{"get_file_info", `{"path":"Day34/workspace/live_probe.md"}`},
		{"list_directory", `{"path":"Day34/workspace"}`},
	}
	for _, c := range gitStyle {
		if _, err := exec(ctx, c.tool, c.args); err != nil {
			t.Errorf("git-style path FAILED: %s %s -> %v", c.tool, c.args, err)
			continue
		}
		t.Logf("git-style path ok: %s %s", c.tool, c.args)
	}

	// Requirement 1: read outside the write root but inside the read root.
	if _, err := exec(ctx, "read_text_file", `{"path":"agent/main.go"}`); err != nil {
		t.Fatalf("step 7 FAILED: read inside read root must be allowed, got: %v", err)
	}
	t.Log("step 7 ok: read of agent/main.go allowed")

	// Requirement 6: the denials.
	denials := []struct {
		step, tool, args, wantCode string
	}{
		{"11", "write_file", `{"path":"agent/main.go","content":"pwned"}`, "READ_ONLY_PATH"},
		{"11b", "edit_file", `{"path":"agent/internal/app/toolbelt.go","edits":[{"oldText":"a","newText":"b"}]}`, "READ_ONLY_PATH"},
		{"12", "read_text_file", `{"path":"agent/agent.config.yaml"}`, "DENIED_BY_POLICY"},
		{"12b", "read_text_file", `{"path":".git/config"}`, "DENIED_BY_POLICY"},
		// The root-prefix alias must not become an escape hatch: it re-runs the
		// same validation, so a prefixed path outside the write root stays denied.
		{"11c", "write_file", `{"path":"Day34/agent/main.go","content":"pwned"}`, "READ_ONLY_PATH"},
		{"12c", "read_text_file", `{"path":"Day34/agent/agent.config.yaml"}`, "DENIED_BY_POLICY"},
		{"14a", "write_file", `{"path":"../../../tmp/escape.txt","content":"x"}`, ""},
		{"14b", "write_file", `{"path":"/tmp/escape.txt","content":"x"}`, "BAD_ARGUMENT"},
		{"14c", "read_text_file", `{"path":"../../../etc/passwd"}`, ""},
	}
	for _, d := range denials {
		_, err := exec(ctx, d.tool, d.args)
		if err == nil {
			t.Errorf("step %s FAILED: %s %s was ALLOWED but must be denied", d.step, d.tool, d.args)
			continue
		}
		if d.wantCode != "" && !strings.Contains(err.Error(), d.wantCode) {
			t.Errorf("step %s: want code %s, got: %v", d.step, d.wantCode, err)
			continue
		}
		t.Logf("step %s ok: denied -> %v", d.step, err)
	}

	// Requirement 6: a read-only role must not even be offered write tools.
	tools := tb.MCPToolsForGrant(researcherGrant)
	for _, tool := range tools {
		switch tool.Name {
		case "write_file", "edit_file", "create_directory", "move_file":
			t.Errorf("step 13 FAILED: read-only role researcher is offered write tool %q", tool.Name)
		}
	}
	t.Logf("step 13 ok: researcher sees %d tools, none of them write tools", len(tools))

	// And the executor refuses it even if the model calls it anyway.
	rexec := tb.ToolExecutorForGrant(researcherGrant)
	if _, err := rexec(ctx, "write_file", `{"path":"workspace/sneak.md","content":"x"}`); err == nil {
		t.Error("step 13 FAILED: researcher executor allowed write_file")
	} else {
		t.Logf("step 13 ok: researcher executor refused write_file -> %v", err)
	}
}
