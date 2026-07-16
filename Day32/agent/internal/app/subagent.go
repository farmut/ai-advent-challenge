package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/usecase"
)

// SubAgent is one worker the orchestrator spawns to carry out a delegated task.
// It reuses the same execution engine as the rest of the agent — ChatUseCase
// with the MCP tool-calling loop — but runs on its own ephemeral memory (in-memory
// STM+WM seeded with the orchestrator's context, shared read-only LTM/profile) and
// with tool access scoped to what its role config permits. It has the same reach
// as the orchestrator: RAG grounding and MCP tools, gated per role.
type SubAgent struct {
	role  config.SubAgentConfig
	tb    *Toolbelt
	debug bool
	out   io.Writer // progress sink; default os.Stderr, redirected by Orchestrator.SetOutput
}

// NewSubAgent binds a role definition to the shared toolbelt.
func NewSubAgent(role config.SubAgentConfig, tb *Toolbelt, debug bool) *SubAgent {
	return &SubAgent{role: role, tb: tb, debug: debug, out: os.Stderr}
}

// Run carries out task using the orchestrator-supplied context. It returns the
// sub-agent's final text answer, which the orchestrator routes onward.
func (s *SubAgent) Run(ctx context.Context, task, orchestratorContext string) (string, error) {
	chat := s.tb.Memory.EphemeralChat(s.tb.LLM, nil, s.tb.Memory.WMEnabledFor(s.role.Memory.WM))

	// The orchestrator context seeds the sub-agent's working input; the role
	// prompt is the system instruction.
	query := task
	if strings.TrimSpace(orchestratorContext) != "" {
		query = "Контекст от оркестратора:\n" + orchestratorContext + "\n\nЗадача:\n" + task
	}

	// RAG grounding: retrieve → [rerank] → filter, then prepend the context to the
	// query. Composes with MCP tools below (the grounded prompt is still the input
	// to the tool-calling loop).
	if s.role.RAG && s.tb.RAG != nil {
		res, err := s.tb.RAG.BuildPrompt(ctx, query, s.tb.RAGCfg)
		if err != nil {
			fmt.Fprintf(s.out, "[subagent %s] RAG retrieval failed: %v\n", s.role.Name, err)
		} else {
			if res.RerankErr != nil {
				fmt.Fprintf(s.out, "[subagent %s] rerank failed, using similarity order: %v\n", s.role.Name, res.RerankErr)
			}
			fmt.Fprintf(s.out, "[subagent %s] RAG grounded on %d chunk(s)\n", s.role.Name, len(res.Final))
			query = res.Prompt
		}
	}

	tools := s.tb.MCPToolsFor(s.role.MCP)
	executor := s.tb.ToolExecutor(s.role.MCP)

	cfg := usecase.ChatConfig{
		Model:         s.tb.Cfg.LLM.Model,
		FullQuery:     query,
		SystemMessage: s.role.Prompt,
		MaxTokens:     s.tb.Cfg.LLM.MaxTokens,
		Temperature:   temperatureOrDefault(s.tb.Cfg.LLM.Temperature),
		Strategy:      usecase.StrategyNone,
		Debug:         s.debug,
	}

	res, err := chat.ExecuteWithTools(ctx, cfg, tools, executor)
	if err != nil {
		return "", fmt.Errorf("subagent %q: %w", s.role.Name, err)
	}
	return res.Content, nil
}

// temperatureOrDefault maps a *float64 config temperature to the ChatConfig
// convention (negative = provider default).
func temperatureOrDefault(t *float64) float64 {
	if t == nil {
		return -1
	}
	return *t
}
