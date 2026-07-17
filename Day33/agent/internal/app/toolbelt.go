package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	mcpadapter "ai-adv-agent/internal/adapter/mcp"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
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

// MCPToolsFor returns the tools exposed by the servers in the allow list. Use
// []string{"*"} for every configured server. An empty allow list yields no tools,
// so a sub-agent role only sees MCP when its config lists servers.
func (tb *Toolbelt) MCPToolsFor(allow []string) []domain.MCPTool {
	if tb.MCPPool == nil || len(allow) == 0 {
		return nil
	}
	var out []domain.MCPTool
	for _, t := range tb.MCPTools {
		srv, ok := tb.ToolRouting[t.Name]
		if ok && serverAllowed(allow, srv.Name) {
			out = append(out, t)
		}
	}
	return out
}

// ToolExecutor returns a port.ToolExecutor that dispatches a tool call to the
// serving MCP session, restricted to servers in the allow list. Calls to tools
// outside the allow list are refused so a sub-agent cannot reach beyond its
// granted servers.
func (tb *Toolbelt) ToolExecutor(allow []string) port.ToolExecutor {
	return func(ctx context.Context, name, argsJSON string) (string, error) {
		srv, ok := tb.ToolRouting[name]
		if !ok {
			return "", fmt.Errorf("unknown tool %q", name)
		}
		if !serverAllowed(allow, srv.Name) {
			return "", fmt.Errorf("tool %q is not permitted for this agent", name)
		}
		args, err := parseToolArgs(argsJSON)
		if err != nil {
			return "", fmt.Errorf("bad tool arguments: %w", err)
		}
		return tb.MCPPool.CallTool(ctx, srv, name, args)
	}
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
