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

// fakePool is a port.MCPPool that records how many times a tool was actually
// dispatched. The counter is the point: a denial must never reach CallTool.
type fakePool struct {
	calls     int
	lastTool  string
	lastArgs  map[string]interface{}
	returnStr string
}

func (p *fakePool) ListTools(context.Context, domain.MCPServerConfig) ([]domain.MCPTool, error) {
	return nil, nil
}

func (p *fakePool) CallTool(_ context.Context, _ domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	p.calls++
	p.lastTool = name
	p.lastArgs = args
	return p.returnStr, nil
}

func (p *fakePool) Close() {}

// guardedToolbelt builds a Toolbelt wired to a fake pool with one file server
// ("fs") and one neutral server ("git"), plus a real directory layout:
//
//	root/                 read root
//	root/workspace/       write root
//	root/internal/app.go  a project file that must stay read-only
func guardedToolbelt(t *testing.T) (*Toolbelt, *fakePool, string) {
	t.Helper()

	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := domain.MCPServerConfig{Name: "fs", Type: domain.MCPStdio, Command: "fs-server"}
	git := domain.MCPServerConfig{Name: "git", Type: domain.MCPStdio, Command: "git-server"}

	tools := []domain.MCPTool{
		{Name: "read_text_file"},
		{Name: "write_file"},
		{Name: "edit_file"},
		{Name: "create_directory"},
		{Name: "list_directory"},
		{Name: "shady_tool"}, // from the fs server, absent from the policy table
		{Name: "git_diff"},
	}
	routing := map[string]domain.MCPServerConfig{
		"read_text_file":   fs,
		"write_file":       fs,
		"edit_file":        fs,
		"create_directory": fs,
		"list_directory":   fs,
		"shady_tool":       fs,
		"git_diff":         git,
	}

	cfg := config.Default()
	cfg.MCP.Enabled = true
	cfg.MCP.FSGuard = config.FSGuardConfig{
		Enabled:  true,
		ReadRoot: root,
		Servers:  []string{"fs"},
	}

	pool := &fakePool{returnStr: "ok"}
	tb := &Toolbelt{
		Cfg:         cfg,
		MCPPool:     pool,
		MCPTools:    tools,
		MCPServers:  map[string]domain.MCPServerConfig{"fs": fs, "git": git},
		ToolRouting: routing,
	}
	return tb, pool, root
}

func toolNames(tools []domain.MCPTool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestMCPToolsForGrant_FiltersWriteToolsWithoutWriteRoot(t *testing.T) {
	tb, _, root := guardedToolbelt(t)

	readOnly := toolNames(tb.MCPToolsForGrant(Grant{Servers: []string{"fs", "git"}, Role: "researcher"}))
	for _, w := range []string{"write_file", "edit_file", "create_directory"} {
		if contains(readOnly, w) {
			t.Errorf("write tool %q offered to a role with no write root: %v", w, readOnly)
		}
	}
	if !contains(readOnly, "read_text_file") {
		t.Errorf("read tool missing for a read-only role: %v", readOnly)
	}
	if !contains(readOnly, "git_diff") {
		t.Errorf("neutral tool from a non-file server must stay: %v", readOnly)
	}
	// Fail-closed: an unknown tool from a file server is not exposed at all.
	if contains(readOnly, "shady_tool") {
		t.Errorf("unknown file-server tool must not be offered: %v", readOnly)
	}

	writer := toolNames(tb.MCPToolsForGrant(Grant{
		Servers: []string{"fs"},
		Tools:   config.ToolPolicy{WriteRoot: filepath.Join(root, "workspace")},
		Role:    "coder",
	}))
	for _, w := range []string{"write_file", "edit_file", "create_directory"} {
		if !contains(writer, w) {
			t.Errorf("write tool %q missing for a role with a write root: %v", w, writer)
		}
	}
}

func TestMCPToolsForGrant_AllowDenyByToolName(t *testing.T) {
	tb, _, _ := guardedToolbelt(t)

	got := toolNames(tb.MCPToolsForGrant(Grant{
		Servers: []string{"fs", "git"},
		Tools: config.ToolPolicy{
			Allow: []string{"read_text_file", "list_directory", "git_diff"},
			Deny:  []string{"list_directory"}, // deny beats allow
		},
		Role: "researcher",
	}))

	if !contains(got, "read_text_file") || !contains(got, "git_diff") {
		t.Errorf("allowed tools missing: %v", got)
	}
	if contains(got, "list_directory") {
		t.Errorf("deny must beat allow, got: %v", got)
	}
	if contains(got, "get_file_info") {
		t.Errorf("tool outside the allow list leaked: %v", got)
	}
}

func TestToolExecutorForGrant_DeniesUnlistedTool(t *testing.T) {
	tb, pool, _ := guardedToolbelt(t)

	exec := tb.ToolExecutorForGrant(Grant{
		Servers: []string{"fs"},
		Tools:   config.ToolPolicy{Allow: []string{"read_text_file"}},
		Role:    "researcher",
	})

	// list_directory exists and its server is granted, but the role's allow list
	// omits it — the filter never offered it, and the executor still refuses.
	_, err := exec(context.Background(), "list_directory", `{"path":"internal"}`)
	if err == nil {
		t.Fatal("expected the executor to refuse a tool outside the allow list")
	}
	if !strings.Contains(err.Error(), policy.CodePermissionDenied) {
		t.Errorf("error should carry the %s code for the model, got: %v", policy.CodePermissionDenied, err)
	}
	if pool.calls != 0 {
		t.Errorf("CallTool must not be reached on a denial, got %d call(s)", pool.calls)
	}
}

func TestToolExecutorForGrant_GuardBlocksWriteOutsideWorkspace(t *testing.T) {
	tb, pool, root := guardedToolbelt(t)

	exec := tb.ToolExecutorForGrant(Grant{
		Servers: []string{"fs"},
		Tools: config.ToolPolicy{
			Allow:     []string{"write_file", "edit_file", "read_text_file"},
			WriteRoot: filepath.Join(root, "workspace"),
		},
		Role: "coder",
	})

	// Writing inside the workspace is fine.
	if _, err := exec(context.Background(), "write_file", `{"path":"workspace/report.md","content":"x"}`); err != nil {
		t.Fatalf("write inside the workspace must be allowed: %v", err)
	}
	if pool.calls != 1 {
		t.Fatalf("expected the allowed write to reach CallTool, got %d call(s)", pool.calls)
	}

	// Editing a project file outside the workspace is refused before dispatch.
	_, err := exec(context.Background(), "edit_file", `{"path":"internal/app.go","edits":[]}`)
	if err == nil {
		t.Fatal("expected a write outside the workspace to be refused")
	}
	if code := policy.CodeOf(err); code != policy.CodeReadOnlyPath {
		t.Errorf("expected %s, got %q (%v)", policy.CodeReadOnlyPath, code, err)
	}
	if pool.calls != 1 {
		t.Errorf("CallTool must not be reached on a denial, got %d call(s) total", pool.calls)
	}

	// Escaping the root entirely is refused too.
	_, err = exec(context.Background(), "write_file", `{"path":"../escape.txt","content":"x"}`)
	if err == nil {
		t.Fatal("expected a path escaping the root to be refused")
	}
	if pool.calls != 1 {
		t.Errorf("CallTool must not be reached on a denial, got %d call(s) total", pool.calls)
	}

	// Reading a project file still works: the asymmetry is the whole point.
	if _, err := exec(context.Background(), "read_text_file", `{"path":"internal/app.go"}`); err != nil {
		t.Fatalf("reads outside the write root must stay allowed: %v", err)
	}
	if pool.calls != 2 {
		t.Errorf("expected the read to reach CallTool, got %d call(s) total", pool.calls)
	}
}

func TestToolExecutor_LegacyAllowListStillWorks(t *testing.T) {
	tb, pool, _ := guardedToolbelt(t)

	// The legacy form: servers only, no tool policy.
	exec := tb.ToolExecutor([]string{"git"})
	if _, err := exec(context.Background(), "git_diff", `{"staged":true}`); err != nil {
		t.Fatalf("a neutral tool from a granted server must still run: %v", err)
	}
	if pool.calls != 1 || pool.lastTool != "git_diff" {
		t.Fatalf("expected git_diff to be dispatched, got %d call(s) to %q", pool.calls, pool.lastTool)
	}

	// A server outside the list is still refused, as before.
	if _, err := exec(context.Background(), "read_text_file", `{"path":"internal/app.go"}`); err == nil {
		t.Error("expected a tool from a non-granted server to be refused")
	}

	// And the legacy list still yields exactly the granted server's tools.
	if got := toolNames(tb.MCPToolsFor([]string{"git"})); len(got) != 1 || got[0] != "git_diff" {
		t.Errorf("legacy MCPToolsFor returned %v, want [git_diff]", got)
	}
	if got := tb.MCPToolsFor(nil); got != nil {
		t.Errorf("an empty allow list must yield no tools, got %v", toolNames(got))
	}
}

func TestPolicyFor_ZeroToolbeltDoesNotPanic(t *testing.T) {
	// orchestrator_test's testToolbelt builds a Toolbelt literal with no MCP at
	// all; the guard must degrade to "nothing granted", not panic.
	tb := &Toolbelt{}
	if got := tb.MCPToolsForGrant(Grant{Servers: []string{"*"}}); got != nil {
		t.Errorf("expected no tools from a Toolbelt without a pool, got %v", toolNames(got))
	}
	exec := tb.ToolExecutorForGrant(Grant{Servers: []string{"*"}})
	if _, err := exec(context.Background(), "write_file", `{"path":"x"}`); err == nil {
		t.Error("expected an error for an unknown tool")
	}
}

func TestPolicyFor_DisabledGuardIsNeutral(t *testing.T) {
	tb, _, _ := guardedToolbelt(t)
	tb.Cfg.MCP.FSGuard.Enabled = false

	pol := tb.policyFor(Grant{Servers: []string{"fs"}})
	if len(pol.Specs) != 0 || pol.ReadRoot != "" || pol.WriteRoot != "" {
		t.Fatalf("a disabled guard must be the zero policy, got %+v", pol)
	}
	// With the guard off, even an unknown file tool is passed through.
	if got := toolNames(tb.MCPToolsForGrant(Grant{Servers: []string{"fs"}})); !contains(got, "shady_tool") {
		t.Errorf("a disabled guard must not filter anything, got %v", got)
	}
}
