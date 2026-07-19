package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	mcpadapter "ai-adv-agent/internal/adapter/mcp"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// Toolbelt is the shared bundle of capabilities available to the orchestrator
// and, identically, to every sub-agent it spawns: the LLM client, the RAG
// pipeline, the MCP tool pool, and the memory factory. Sub-agents receive the
// same Toolbelt, so they have the same reach as the orchestrator — gated only by
// each role's config (which tools it may use).
type Toolbelt struct {
	Cfg    config.Config
	LLM    port.LLMClient
	Memory *MemoryFactory

	// RAG (nil when disabled)
	RAG    *usecase.RAGUseCase
	RAGCfg usecase.RAGConfig

	// MCP (nil/empty when disabled)
	MCPPool    port.MCPPool
	MCPTools   []domain.MCPTool
	MCPServers map[string]domain.MCPServerConfig
	// ToolRouting maps a tool name to the server that serves it, so an executor
	// can dispatch a tool call to the right MCP session.
	ToolRouting map[string]domain.MCPServerConfig

	closers []func()
}

// Build wires a Toolbelt from a validated, env-resolved config. Call Close when
// done to release the RAG database handle and MCP subprocess/SSE sessions.
func Build(cfg config.Config) (*Toolbelt, error) {
	scope := os.Getenv("GIGACHAT_SCOPE")

	tb := &Toolbelt{
		Cfg:    cfg,
		LLM:    buildLLMClient(cfg.LLM, scope),
		Memory: NewMemoryFactory(cfg.Memory),
	}

	if cfg.RAG.Enabled {
		if err := tb.buildRAG(cfg, scope); err != nil {
			tb.Close()
			return nil, err
		}
	}

	if cfg.MCP.Enabled {
		if err := tb.buildMCP(cfg.MCP); err != nil {
			tb.Close()
			return nil, err
		}
	}

	return tb, nil
}

func (tb *Toolbelt) buildRAG(cfg config.Config, scope string) error {
	retriever, err := ragadapter.NewRetriever(ragadapter.Config{
		DBPath:     cfg.RAG.DB,
		EmbedURL:   cfg.RAG.EmbedURL,
		EmbedModel: cfg.RAG.EmbedModel,
		EmbedKey:   cfg.RAG.EmbedKey,
	})
	if err != nil {
		return fmt.Errorf("RAG retrieval unavailable: %w", err)
	}
	tb.closers = append(tb.closers, func() { _ = retriever.Close() })

	reranker, useAPI := buildReranker(cfg.RAG.Rerank, cfg.LLM, scope)
	if reranker != nil {
		mode := "chat scoring"
		if useAPI {
			mode = "dedicated /rerank endpoint"
		}
		model := cfg.RAG.Rerank.Model
		if model == "" {
			model = cfg.LLM.Model
		}
		fmt.Fprintf(os.Stderr, "[rag] rerank model: %s (%s)\n", model, mode)
	}

	tb.RAG = usecase.NewRAGUseCase(retriever, reranker)
	tb.RAGCfg = usecase.RAGConfig{
		TopKRetrieve: cfg.RAG.TopK,
		Rerank:       cfg.RAG.Rerank.Enabled,
		Threshold:    cfg.RAG.Threshold,
		TopKFinal:    cfg.RAG.TopKFinal,
	}
	return nil
}

func (tb *Toolbelt) buildMCP(mc config.MCPConfig) error {
	servers := append([]domain.MCPServerConfig(nil), mc.Servers...)

	// Merge servers from an external MCP YAML file, if configured.
	if mc.File != "" {
		fileCfg, err := storage.NewMCPConfigFile(mc.File).Load()
		if err != nil {
			return fmt.Errorf("load MCP file %q: %w", mc.File, err)
		}
		servers = append(servers, fileCfg.Servers...)
	}

	if len(servers) == 0 {
		fmt.Fprintln(os.Stderr, "[mcp] enabled but no servers configured")
		return nil
	}

	pool, errs := mcpadapter.NewClient().OpenPool(servers)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "[mcp] warning: %v\n", e)
	}
	tb.MCPPool = pool
	tb.closers = append(tb.closers, pool.Close)

	tb.MCPServers = make(map[string]domain.MCPServerConfig, len(servers))
	tb.ToolRouting = make(map[string]domain.MCPServerConfig)
	for _, s := range servers {
		tb.MCPServers[s.Name] = s
	}

	// List each server's tools once so sub-agents can advertise them and calls
	// can be routed back to the serving session.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, s := range servers {
		tools, err := pool.ListTools(ctx, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[mcp] warning: list tools for %q: %v\n", s.Name, err)
			continue
		}
		for _, t := range tools {
			tb.MCPTools = append(tb.MCPTools, t)
			tb.ToolRouting[t.Name] = s
		}
	}
	fmt.Fprintf(os.Stderr, "[mcp] %d server(s), %d tool(s) available\n", len(servers), len(tb.MCPTools))
	return nil
}

// serverAllowed reports whether a server name is in the allow list. A list
// containing "*" grants every server; an empty list grants none (a role must opt
// in to MCP explicitly).
func serverAllowed(allow []string, name string) bool {
	for _, a := range allow {
		if a == "*" || a == name {
			return true
		}
	}
	return false
}

// Grant is everything a role is allowed to reach: which MCP servers, which of
// their tools, and where it may write. The zero Grant grants nothing.
type Grant struct {
	// Servers is the MCP server allow list ("*" = all, empty = none).
	Servers []string
	// Tools narrows the servers' tools by name and carries the write root.
	Tools config.ToolPolicy
	// Role labels the guard's audit lines.
	Role string
}

// policyFor builds the file guard for one role. The roots are made absolute and
// symlink-resolved here, once per grant — never per tool call — so the executor
// closure captures a ready policy.
func (tb *Toolbelt) policyFor(g Grant) policy.FS {
	fg := tb.Cfg.MCP.FSGuard
	if !fg.Enabled {
		// Explicitly disabled: an empty policy has no specs and no file
		// servers, so every tool is neutral and passes through.
		return policy.FS{}
	}

	readRoot := fg.ReadRoot
	if g.Tools.ReadRoot != "" {
		readRoot = g.Tools.ReadRoot
	}
	deny := fg.Deny
	if len(deny) == 0 {
		deny = policy.DefaultDenyGlobs()
	}

	return policy.FS{
		ReadRoot:  absReal(readRoot),
		WriteRoot: absReal(g.Tools.WriteRoot), // empty stays empty: no writes
		Deny:      deny,
		Specs:     policy.DefaultSpecs(),
		FSServers: fg.Servers,
		Role:      g.Role,
	}
}

// absReal makes a path absolute and resolves symlinks. An empty path stays
// empty (it means "not granted"). Config.ResolvePaths normally did this at load
// time; repeating it here keeps policyFor correct for hand-built Toolbelts.
func absReal(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// toolNameAllowed applies the role's per-tool gating: deny always wins, and an
// empty allow list means every tool of the granted servers.
func toolNameAllowed(tp config.ToolPolicy, name string) bool {
	for _, d := range tp.Deny {
		if d == name {
			return false
		}
	}
	if len(tp.Allow) == 0 {
		return true
	}
	for _, a := range tp.Allow {
		if a == name {
			return true
		}
	}
	return false
}

// MCPToolsForGrant returns the tools a role may see: its granted servers, minus
// the names its policy denies, minus the ones the file guard would always
// refuse (a write tool with no write root, or an unknown tool from a file
// server). This is a UX filter — do not offer what is bound to fail. The
// executor below is the actual security boundary.
func (tb *Toolbelt) MCPToolsForGrant(g Grant) []domain.MCPTool {
	if tb.MCPPool == nil || len(g.Servers) == 0 {
		return nil
	}
	pol := tb.policyFor(g)
	var out []domain.MCPTool
	for _, t := range tb.MCPTools {
		srv, ok := tb.ToolRouting[t.Name]
		if !ok || !serverAllowed(g.Servers, srv.Name) {
			continue
		}
		if !toolNameAllowed(g.Tools, t.Name) {
			continue
		}
		if len(pol.FilterServer(srv.Name, []domain.MCPTool{t})) == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ToolExecutorForGrant returns a port.ToolExecutor that dispatches a call to the
// serving MCP session after the grant and the file guard both approve it.
//
// The order is: resolve the server → server allow list → tool name gate → parse
// arguments → policy check on the resolved paths → call. The name gate repeats
// what MCPToolsForGrant already filtered, and that duplication is deliberate: the
// filter only shapes what the model is offered, while this function is the
// boundary a model that invents a tool name has to get past.
func (tb *Toolbelt) ToolExecutorForGrant(g Grant) port.ToolExecutor {
	pol := tb.policyFor(g)
	return func(ctx context.Context, name, argsJSON string) (string, error) {
		srv, ok := tb.ToolRouting[name]
		if !ok {
			return "", fmt.Errorf("unknown tool %q", name)
		}
		if !serverAllowed(g.Servers, srv.Name) {
			return "", fmt.Errorf("tool %q is not permitted for this agent", name)
		}
		if !toolNameAllowed(g.Tools, name) {
			// The message reaches the model as the tool result, so it states
			// the code and what to do instead.
			return "", fmt.Errorf("%s: инструмент %q не выдан этой роли. Используй только перечисленные инструменты", policy.CodePermissionDenied, name)
		}
		args, err := parseToolArgs(argsJSON)
		if err != nil {
			return "", fmt.Errorf("bad tool arguments: %w", err)
		}
		if err := pol.Check(srv.Name, name, args); err != nil {
			return "", err
		}
		return tb.MCPPool.CallTool(ctx, srv, name, args)
	}
}

// MCPToolsFor returns the tools exposed by the servers in the allow list. Use
// []string{"*"} for every configured server. An empty allow list yields no tools,
// so a sub-agent role only sees MCP when its config lists servers.
//
// It is the compatibility form of MCPToolsForGrant: with no ToolPolicy the write
// root is empty, so the role is read-only.
func (tb *Toolbelt) MCPToolsFor(allow []string) []domain.MCPTool {
	return tb.MCPToolsForGrant(Grant{Servers: allow})
}

// ToolExecutor is the compatibility form of ToolExecutorForGrant: servers only,
// no per-tool gating, and no write access.
func (tb *Toolbelt) ToolExecutor(allow []string) port.ToolExecutor {
	return tb.ToolExecutorForGrant(Grant{Servers: allow})
}

// parseToolArgs decodes the LLM's JSON argument string into a map. An empty
// string means no arguments.
func parseToolArgs(argsJSON string) (map[string]interface{}, error) {
	if argsJSON == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Close releases all held resources (RAG DB, MCP sessions).
func (tb *Toolbelt) Close() {
	for i := len(tb.closers) - 1; i >= 0; i-- {
		tb.closers[i]()
	}
	tb.closers = nil
}
