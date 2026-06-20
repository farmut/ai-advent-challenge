package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultOpenAIBaseURL     = "https://api.openai.com/v1"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenAIModel       = "gpt-4o"
)

// formatInstructions maps --format values to prompt instructions.
var formatInstructions = map[string]string{
	"markdown": "Format your response using Markdown.",
	"json":     "Respond in JSON format.",
}

// ---------------------------------------------------------------------------
// CLI flag helpers
// ---------------------------------------------------------------------------

type stringSlice []string

func (s *stringSlice) String() string        { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(value string) error { *s = append(*s, value); return nil }

// ---------------------------------------------------------------------------
// Presentation helpers
// ---------------------------------------------------------------------------

func printTokenInfo(model, queryText string, u domain.Usage, stats domain.SessionStats, prevPromptTokens int, showCost bool) {
	queryEst := domain.EstimateTokens(queryText)
	historyTokens := u.PromptTokens - queryEst
	if historyTokens < 0 {
		historyTokens = 0
	}
	fmt.Fprintf(os.Stderr,
		"[tokens]  query≈%-6d  history=%-6d  response=%-6d | session (%d calls): prompt=%d  completion=%d  total=%d\n",
		queryEst, historyTokens, u.CompletionTokens,
		stats.Calls, stats.PromptTokens, stats.CompletionTokens, stats.TotalTokens,
	)

	price, known := domain.PricingFor(model)
	if known && price.ContextWindow > 0 {
		used := u.PromptTokens
		window := price.ContextWindow
		pct := float64(used) / float64(window) * 100
		remaining := window - used
		status := domain.ContextStatus(pct)
		growth := used - prevPromptTokens
		if stats.Calls > 1 && growth > 0 && remaining > 0 {
			fmt.Fprintf(os.Stderr,
				"[context] %d / %d (%.1f%% used, %d remaining, +%d/call, ~%d calls until full)  %s\n",
				used, window, pct, remaining, growth, remaining/growth, status,
			)
		} else {
			fmt.Fprintf(os.Stderr,
				"[context] %d / %d (%.1f%% used, %d remaining)  %s\n",
				used, window, pct, remaining, status,
			)
		}
	} else {
		fmt.Fprintf(os.Stderr,
			"[context] unknown window for %q — add it to KnownPrices for context tracking\n", model,
		)
	}

	if !showCost {
		return
	}
	if !known {
		fmt.Fprintf(os.Stderr, "[cost]    unknown model %q — add it to KnownPrices for cost tracking\n", model)
		return
	}
	callCost := float64(u.PromptTokens)/1_000_000*price.InputPer1M +
		float64(u.CompletionTokens)/1_000_000*price.OutputPer1M
	fmt.Fprintf(os.Stderr,
		"[cost]    this call=$%.6f  session=$%.6f  (%s: $%.2f/$%.2f per 1M tokens in/out)\n",
		callCost, stats.EstimatedCostUSD, model, price.InputPer1M, price.OutputPer1M,
	)
}

func printBranches(bs domain.BranchState) {
	fmt.Println("Branches:")
	for _, b := range bs.Branches {
		if b == bs.Current {
			fmt.Printf("  * %s (current)\n", b)
		} else {
			fmt.Printf("    %s\n", b)
		}
	}
	if len(bs.Checkpoints) > 0 {
		fmt.Println("\nCheckpoints:")
		names := make([]string, 0, len(bs.Checkpoints))
		for n := range bs.Checkpoints {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			cp := bs.Checkpoints[n]
			fmt.Printf("  %s  (%s, %d messages)\n", n, cp.CreatedAt, len(cp.Messages))
		}
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	query := flag.String("query", "", "The query string to send to the LLM (required for chat)")
	format := flag.String("format", "markdown", "Response format: markdown or json")
	maxTokens := flag.Int("max-tokens", 0, "Maximum tokens in response (0 = no limit)")
	debug := flag.Bool("debug", false, "Print request payload and token usage to stderr")
	formatHint := flag.String("format-hint", "", "Custom formatting instruction (overrides --format)")
	system := flag.String("system", "", "System message (optional)")
	temperature := flag.Float64("temperature", -1, "Sampling temperature 0.0–2.0 (default: provider default)")
	historyFile := flag.String("history", "chat_history.json", "Path to chat history file (Layer 1 — STM). Empty = disabled")
	historyLimit := flag.Int("history-limit", 10, "Max messages kept when --summary is enabled (0 = unlimited)")
	showTokens := flag.Bool("show-tokens", false, "Print token breakdown to stderr")
	showCost := flag.Bool("show-cost", false, "Print estimated cost to stderr (implies --show-tokens)")

	summaryEnabled := flag.Bool("summary", false, "Enable auto-summarization when history exceeds --history-limit")
	strategyFlag := flag.String("strategy", "", "Context strategy: sliding-window | sticky-facts | branching")
	windowSize := flag.Int("window-size", 5, "Recent messages to include in prompt (sliding-window / sticky-facts)")

	checkpointName := flag.String("checkpoint", "", "[branching] Save current history as a named checkpoint then exit")
	branchNew := flag.String("branch-new", "", "[branching] Create and switch to a new branch then exit")
	fromCheckpoint := flag.String("from-checkpoint", "", "[branching] Source checkpoint for --branch-new")
	branchSwitch := flag.String("branch-switch", "", "[branching] Switch to an existing branch then exit")
	branchList := flag.Bool("branch-list", false, "[branching] List all branches and checkpoints then exit")

	memoryWMFile := flag.String("memory-wm", "", "Working memory file (Layer 2: task facts). Default: derived from --history")
	memoryLTMFile := flag.String("memory-ltm", "", "Long-term memory file (Layer 3: profile/knowledge). Default: derived from --history")
	memoryUpdate := flag.Bool("memory-update", false, "Auto-update Layer 2 (WM) and Layer 3 (LTM) after each call")

	var stopSequences stringSlice
	flag.Var(&stopSequences, "stop", "Stop sequence (repeatable)")
	flag.Parse()

	// ----------------------------------------------------------------
	// Validate strategy
	// ----------------------------------------------------------------
	strat := usecase.ContextStrategy(*strategyFlag)
	switch strat {
	case usecase.StrategyNone, usecase.StrategySlidingWindow, usecase.StrategyStickyFacts, usecase.StrategyBranching:
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown --strategy %q. Valid: sliding-window, sticky-facts, branching\n", strat)
		os.Exit(1)
	}

	if *showCost {
		*showTokens = true
	}

	// ----------------------------------------------------------------
	// Branching management commands — no --query required
	// ----------------------------------------------------------------
	if strat == usecase.StrategyBranching && *historyFile != "" {
		branchRepo := storage.NewBranchFile(*historyFile)
		branchUC := usecase.NewBranchUseCase(branchRepo)

		if *branchList {
			bs, err := branchUC.ListBranches()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to load branch state: %v\n", err)
			}
			printBranches(bs)
			return
		}

		if *checkpointName != "" {
			n, err := branchUC.SaveCheckpoint(*checkpointName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error saving checkpoint: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[branch] checkpoint %q saved (%d messages)\n", *checkpointName, n)
			return
		}

		if *branchNew != "" {
			n, err := branchUC.CreateBranch(*branchNew, *fromCheckpoint)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating branch: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[branch] created and switched to branch %q (%d messages)\n", *branchNew, n)
			return
		}

		if *branchSwitch != "" {
			if err := branchUC.Switch(*branchSwitch); err != nil {
				fmt.Fprintf(os.Stderr, "Error switching branch: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[branch] switched to branch %q\n", *branchSwitch)
			return
		}
	}

	// ----------------------------------------------------------------
	// From here --query is required
	// ----------------------------------------------------------------
	if *query == "" {
		fmt.Fprintf(os.Stderr, "Error: --query flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Resolve format instruction
	var instruction string
	if *formatHint != "" {
		if *format != "markdown" {
			fmt.Fprintf(os.Stderr, "Warning: --format-hint overrides --format, ignoring --format=%s\n", *format)
		}
		instruction = *formatHint
	} else {
		var ok bool
		instruction, ok = formatInstructions[*format]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: --format must be 'markdown' or 'json'\n")
			os.Exit(1)
		}
	}

	// Validate provider
	provider := os.Getenv("LLM_PROVIDER")
	if provider == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}
	if provider != llm.ProviderOpenAI && provider != llm.ProviderOpenRouter {
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER %q (supported: openai, openrouter)\n", provider)
		os.Exit(1)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_API_KEY environment variable is required\n")
		os.Exit(1)
	}

	model := os.Getenv("LLM_MODEL")
	baseURL := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if baseURL == "" {
		if provider == llm.ProviderOpenRouter {
			baseURL = defaultOpenRouterBaseURL
		} else {
			baseURL = defaultOpenAIBaseURL
		}
	}
	if provider == llm.ProviderOpenAI && model == "" {
		model = defaultOpenAIModel
	}

	fullQuery := *query + "\n\n" + instruction

	// ----------------------------------------------------------------
	// Resolve actual history file (branch-aware)
	// ----------------------------------------------------------------
	actualHistoryFile := *historyFile
	if strat == usecase.StrategyBranching && *historyFile != "" {
		branchRepo := storage.NewBranchFile(*historyFile)
		bs, _ := branchRepo.LoadState()
		actualHistoryFile = storage.CurrentBranchHistoryPath(*historyFile, bs)
		fmt.Fprintf(os.Stderr, "[branch] active branch: %q → %s\n", bs.Current, actualHistoryFile)
	}

	// ----------------------------------------------------------------
	// Resolve memory layer paths (Layer 2 and Layer 3)
	// ----------------------------------------------------------------
	resolvedWMFile := *memoryWMFile
	if resolvedWMFile == "" && actualHistoryFile != "" {
		resolvedWMFile = storage.WMPath(actualHistoryFile)
	}
	resolvedLTMFile := *memoryLTMFile
	if resolvedLTMFile == "" && actualHistoryFile != "" {
		resolvedLTMFile = storage.LTMPath(actualHistoryFile)
	}

	// ----------------------------------------------------------------
	// Wire adapters
	// ----------------------------------------------------------------
	llmClient := llm.NewClient(llm.Config{
		Provider: provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	})

	historyRepo := storage.NewHistoryFile(actualHistoryFile)
	statsRepo := storage.NewStatsFile(storage.StatsPath(actualHistoryFile))
	summaryRepo := storage.NewSummaryFile(storage.SummaryPath(actualHistoryFile))
	factsRepo := storage.NewFactsFile(storage.FactsPath(actualHistoryFile))
	wmRepo := storage.NewWorkingMemoryFile(resolvedWMFile)
	ltmRepo := storage.NewLongTermMemoryFile(resolvedLTMFile)

	// ----------------------------------------------------------------
	// Execute chat use case
	// ----------------------------------------------------------------
	chatUC := usecase.NewChatUseCase(llmClient, historyRepo, statsRepo, summaryRepo, factsRepo, wmRepo, ltmRepo)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := chatUC.Execute(ctx, usecase.ChatConfig{
		Model:          model,
		FullQuery:      fullQuery,
		SystemMessage:  *system,
		MaxTokens:      *maxTokens,
		Stop:           []string(stopSequences),
		Temperature:    *temperature,
		HistoryLimit:   *historyLimit,
		SummaryEnabled: *summaryEnabled,
		Strategy:       strat,
		WindowSize:     *windowSize,
		MemoryUpdate:   *memoryUpdate,
		Debug:          *debug,
		ShowTokens:     *showTokens,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying LLM: %v\n", err)
		os.Exit(1)
	}

	if *debug {
		fmt.Fprintf(os.Stderr,
			"\n[usage] prompt_tokens=%d, completion_tokens=%d, total_tokens=%d\n\n",
			result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
	}

	fmt.Println(result.Content)

	if *showTokens {
		printTokenInfo(model, fullQuery, result.Usage, result.Stats, result.PrevPromptTokens, *showCost)
	}
}
