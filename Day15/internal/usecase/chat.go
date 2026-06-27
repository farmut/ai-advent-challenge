package usecase

import (
	"context"
	"fmt"
	"os"
	"sort"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ContextStrategy determines how conversation history is managed between turns.
type ContextStrategy string

const (
	StrategyNone          ContextStrategy = ""
	StrategySlidingWindow ContextStrategy = "sliding-window"
	StrategyStickyFacts   ContextStrategy = "sticky-facts"
	StrategyBranching     ContextStrategy = "branching"
)

// ChatConfig holds all per-call parameters for the chat use case.
type ChatConfig struct {
	Model          string
	FullQuery      string // user query with format instruction already appended
	SystemMessage  string
	MaxTokens      int
	Stop           []string
	Temperature    float64 // negative = provider default
	HistoryLimit   int
	SummaryEnabled bool
	Strategy       ContextStrategy
	WindowSize     int
	MemoryUpdate   bool
	Debug          bool
	ShowTokens     bool
}

// ChatResult is the value returned by ChatUseCase.Execute.
type ChatResult struct {
	Content          string
	Usage            domain.Usage
	Stats            domain.SessionStats
	PrevPromptTokens int
}

// ChatUseCase orchestrates a single LLM exchange with full memory management.
type ChatUseCase struct {
	llm     port.LLMClient
	history port.HistoryRepository
	stats   port.StatsRepository
	summary port.SummaryRepository
	facts   port.FactsRepository
	wm      port.WorkingMemoryRepository
	ltm     port.LongTermMemoryRepository
	profile port.UserProfileRepository
}

// NewChatUseCase creates a ChatUseCase with all required dependencies injected.
func NewChatUseCase(
	llm port.LLMClient,
	history port.HistoryRepository,
	stats port.StatsRepository,
	summary port.SummaryRepository,
	facts port.FactsRepository,
	wm port.WorkingMemoryRepository,
	ltm port.LongTermMemoryRepository,
	profile port.UserProfileRepository,
) *ChatUseCase {
	return &ChatUseCase{
		llm: llm, history: history, stats: stats,
		summary: summary, facts: facts, wm: wm, ltm: ltm, profile: profile,
	}
}

// Execute runs one full chat turn: load state → build prompt → call LLM → update memory → persist.
func (uc *ChatUseCase) Execute(ctx context.Context, cfg ChatConfig) (ChatResult, error) {
	// ---- Load all state ----
	history, _ := uc.history.Load()
	stats, _ := uc.stats.Load()
	wmData, _ := uc.wm.Load()
	ltmData, _ := uc.ltm.Load()
	profileData, _ := uc.profile.Load()

	var factsData domain.FactsStore
	if cfg.Strategy == StrategyStickyFacts {
		factsData, _ = uc.facts.Load()
	}

	var existingSummary string
	if cfg.SummaryEnabled && (cfg.Strategy == StrategyNone || cfg.Strategy == StrategyBranching) {
		existingSummary, _ = uc.summary.Load()
	}

	// ---- Build system content: Profile → LTM → WM → [sticky-facts] → [user system] → [summary] ----
	systemContent := cfg.SystemMessage

	if cfg.Strategy == StrategyStickyFacts {
		if block := FactsSystemBlock(factsData); block != "" {
			systemContent = prepend(block, systemContent)
		}
	}
	if block := WMSystemBlock(wmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := LTMSystemBlock(ltmData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if block := ProfileSystemBlock(profileData); block != "" {
		systemContent = prepend(block, systemContent)
	}
	if existingSummary != "" {
		appendBlock := "Summary of earlier conversation:\n" + existingSummary
		if systemContent != "" {
			systemContent += "\n\n---\n" + appendBlock
		} else {
			systemContent = appendBlock
		}
	}

	// ---- Select history slice ----
	historyForPrompt := history
	if cfg.Strategy == StrategySlidingWindow || cfg.Strategy == StrategyStickyFacts {
		historyForPrompt = SlidingWindow(history, cfg.WindowSize)
	}

	// ---- Build messages ----
	var messages []domain.Message
	if systemContent != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: systemContent})
	}
	messages = append(messages, historyForPrompt...)
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: cfg.FullQuery})

	// ---- Pre-call context estimation ----
	if cfg.ShowTokens {
		if p, known := domain.PricingFor(cfg.Model); known && p.ContextWindow > 0 {
			est := domain.EstimateMessagesTokens(messages)
			pct := float64(est) / float64(p.ContextWindow) * 100
			remaining := p.ContextWindow - est
			status := domain.ContextStatus(pct)
			if remaining < 0 {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%) — EXCEEDS LIMIT by %d tokens  %s\n",
					est, p.ContextWindow, pct, -remaining, status)
			} else {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%, ~%d remaining)  %s\n",
					est, p.ContextWindow, pct, remaining, status)
			}
		}
	}

	// ---- Call LLM ----
	resp, err := uc.llm.Chat(ctx, port.LLMRequest{
		Model:       cfg.Model,
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		Stop:        cfg.Stop,
		Temperature: cfg.Temperature,
		Debug:       cfg.Debug,
	})
	if err != nil {
		return ChatResult{}, err
	}

	// ---- Update session stats ----
	prevPromptTokens := stats.LastCallPromptTokens
	price, known := domain.PricingFor(cfg.Model)
	var callCost float64
	if known {
		callCost = float64(resp.Usage.PromptTokens)/1_000_000*price.InputPer1M +
			float64(resp.Usage.CompletionTokens)/1_000_000*price.OutputPer1M
	}
	stats.Calls++
	stats.PromptTokens += resp.Usage.PromptTokens
	stats.CompletionTokens += resp.Usage.CompletionTokens
	stats.TotalTokens += resp.Usage.TotalTokens
	stats.EstimatedCostUSD += callCost
	stats.LastCallPromptTokens = resp.Usage.PromptTokens

	// ---- Update memory layers (Layer 2 + 3) ----
	if cfg.MemoryUpdate {
		fmt.Fprintf(os.Stderr, "[memory-wm] updating working memory...\n")
		if updatedWM, err := UpdateWM(ctx, uc.llm, cfg.Model, wmData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update working memory: %v\n", err)
		} else if err := uc.wm.Save(updatedWM); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save working memory: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[memory-wm] %d facts saved\n", len(updatedWM.Facts))
		}

		fmt.Fprintf(os.Stderr, "[memory-ltm] updating long-term memory...\n")
		if updatedLTM, err := UpdateLTM(ctx, uc.llm, cfg.Model, ltmData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update long-term memory: %v\n", err)
		} else if err := uc.ltm.Save(updatedLTM); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save long-term memory: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[memory-ltm] %d entries saved\n", len(updatedLTM.Entries))
		}
	}

	// ---- Persist history (Layer 1) by strategy ----
	newHistory := append(history,
		domain.Message{Role: domain.RoleUser, Content: cfg.FullQuery},
		domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
	)

	switch cfg.Strategy {
	case StrategySlidingWindow:
		if err := uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	case StrategyStickyFacts:
		fmt.Fprintf(os.Stderr, "[facts] updating facts store...\n")
		if updatedFacts, err := UpdateFacts(ctx, uc.llm, cfg.Model, factsData, cfg.FullQuery, resp.Content, cfg.Debug); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update facts: %v\n", err)
		} else if err := uc.facts.Save(updatedFacts); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save facts: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[facts] %d facts saved\n", len(updatedFacts.Facts))
			if cfg.Debug {
				keys := make([]string, 0, len(updatedFacts.Facts))
				for k := range updatedFacts.Facts {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(os.Stderr, "[facts]   %s: %s\n", k, updatedFacts.Facts[k])
				}
			}
		}
		if err := uc.history.Save(SlidingWindow(newHistory, cfg.WindowSize)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	default: // StrategyNone or StrategyBranching
		keep, excess := TrimHistory(newHistory, cfg.HistoryLimit)
		if cfg.SummaryEnabled && len(excess) > 0 {
			fmt.Fprintf(os.Stderr, "[summary] %d messages exceed limit — generating summary...\n", len(excess))
			if newSummary, err := BuildSummary(ctx, uc.llm, cfg.Model, excess, existingSummary, cfg.Debug); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to generate summary: %v\n", err)
			} else if err := uc.summary.Save(newSummary); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save summary: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[summary] saved\n")
			}
		}
		if err := uc.history.Save(keep); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}
	}

	// ---- Persist stats ----
	if err := uc.stats.Save(stats); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save stats: %v\n", err)
	}

	return ChatResult{
		Content:          resp.Content,
		Usage:            resp.Usage,
		Stats:            stats,
		PrevPromptTokens: prevPromptTokens,
	}, nil
}

func prepend(block, existing string) string {
	if existing != "" {
		return block + "\n\n" + existing
	}
	return block
}

// LLM returns the underlying LLMClient for use by sub-agents that need direct API access.
func (uc *ChatUseCase) LLM() port.LLMClient {
	return uc.llm
}
