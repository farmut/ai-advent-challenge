package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"ai-adv-agent/internal/adapter/llm"
	mcpadapter "ai-adv-agent/internal/adapter/mcp"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultOpenAIBaseURL     = "https://api.openai.com/v1"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenAIModel       = "gpt-4o"
	defaultGigaChatModel     = "GigaChat"
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

func (s *stringSlice) String() string         { return strings.Join(*s, ", ") }
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

// runProfileInit runs an interactive terminal dialog to create or overwrite the profile.
func runProfileInit(profilePath string) error {
	if profilePath == "" {
		return fmt.Errorf("profile path is empty — set --history or --profile")
	}

	reader := bufio.NewReader(os.Stdin)
	ask := func(prompt string) string {
		fmt.Print(prompt)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}

	fmt.Println("=== Profile Initialization ===")
	fmt.Println()

	name := ask("Your name: ")

	p := domain.UserProfile{
		Name:        name,
		Preferences: make(map[string]string),
	}

	type question struct{ key, prompt, def string }
	questions := []question{
		{"language", "Preferred response language", "english"},
		{"style", "Response style  (concise / detailed / technical)", "detailed"},
		{"format", "Response format (markdown / plain)", "markdown"},
		{"expertise", "Your primary expertise or domain", ""},
		{"constraints", "Constraints to keep in mind", ""},
	}

	fmt.Println()
	fmt.Println("Preferences (Enter to use default or skip):")
	fmt.Println()
	for _, q := range questions {
		var prompt string
		if q.def != "" {
			prompt = fmt.Sprintf("  %s [%s]: ", q.prompt, q.def)
		} else {
			prompt = fmt.Sprintf("  %s: ", q.prompt)
		}
		val := ask(prompt)
		if val == "" {
			val = q.def
		}
		if val != "" {
			p.Preferences[q.key] = val
		}
	}

	fmt.Println()
	fmt.Println("Custom preferences — key=value, empty line to finish:")
	for {
		val := ask("  > ")
		if val == "" {
			break
		}
		k, v, ok := strings.Cut(val, "=")
		if !ok || strings.TrimSpace(k) == "" {
			fmt.Fprintln(os.Stderr, "  Invalid format — use key=value")
			continue
		}
		p.Preferences[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	repo := storage.NewProfileFile(profilePath)
	if err := repo.Save(p); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Printf("\nProfile saved to: %s\n\n", profilePath)
	printProfile(p, profilePath)
	return nil
}

func printProfile(p domain.UserProfile, filePath string) {
	fmt.Printf("Profile file: %s\n", filePath)
	if p.Name != "" {
		fmt.Printf("Name: %s\n", p.Name)
	} else {
		fmt.Printf("Name: (not set)\n")
	}
	if len(p.Preferences) == 0 {
		fmt.Println("Preferences: (none)")
		return
	}
	fmt.Println("Preferences:")
	keys := make([]string, 0, len(p.Preferences))
	for k := range p.Preferences {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, p.Preferences[k])
	}
}

// printRAGAnswer renders the structured RAG result to stdout: the answer, then
// the sources it relies on (source + section/chunk_id) and the verbatim quotes.
// When the answer is not grounded (nothing cleared the threshold) only the
// honest "не знаю" reply is shown.
func printRAGAnswer(ans domain.RAGAnswer) {
	fmt.Println(ans.Answer)

	if !ans.Grounded {
		return
	}

	if len(ans.Sources) > 0 {
		fmt.Println("\n=== Источники ===")
		for i, s := range ans.Sources {
			label := s.Source
			if s.Section != "" {
				label += " — " + s.Section
			}
			fmt.Printf("  [%d] %s (chunk_id=%d, релевантность %.3f)\n", i+1, label, s.ChunkID, s.Score)
		}
	}

	if len(ans.Quotes) > 0 {
		fmt.Println("\n=== Цитаты ===")
		for i, q := range ans.Quotes {
			label := q.Source
			if q.Section != "" {
				label += " — " + q.Section
			}
			fmt.Printf("  [%d] %s (chunk_id=%d):\n      «%s»\n", i+1, label, q.ChunkID, q.Text)
		}
	}
}

// printRAGResult prints the retrieval/rerank funnel to stderr and the structured
// grounded answer (answer + sources + quotes) to stdout.
func printRAGResult(ans domain.RAGAnswer, res usecase.RAGResult, ragDB string, threshold float64) {
	if res.RerankErr != nil {
		fmt.Fprintf(os.Stderr, "[rag] rerank failed, falling back to similarity order: %v\n", res.RerankErr)
	}
	fmt.Fprintf(os.Stderr, "[rag] retrieved %d chunk(s) from %s (rerank=%t, threshold=%.2f) → kept %d:\n",
		len(res.Retrieved), ragDB, res.Reranked, threshold, len(res.Final))
	for i, c := range res.Final {
		label := c.File
		if c.Section != "" {
			label += " — " + c.Section
		}
		if res.Reranked {
			fmt.Fprintf(os.Stderr, "  %d. %s (similarity %.4f, rerank %.4f)\n", i+1, label, c.Similarity, c.RerankScore)
		} else {
			fmt.Fprintf(os.Stderr, "  %d. %s (similarity %.4f)\n", i+1, label, c.Similarity)
		}
	}
	if len(res.Final) == 0 {
		fmt.Fprintln(os.Stderr, "[rag] no chunks passed the threshold — answering \"не знаю\" and asking to clarify")
	}
	printRAGAnswer(ans)
}

// ragHistoryWindow bounds how many recent dialogue messages are replayed into
// each grounded prompt (4 user+assistant turns). The full history is still
// persisted; only the prompt context is windowed to keep token usage in check.
const ragHistoryWindow = 8

// printTaskMemory renders the dialogue task memory (goal / clarified points /
// fixed constraints) to stdout for the /memory command.
func printTaskMemory(tm domain.TaskMemory) {
	if tm.IsEmpty() {
		fmt.Println("=== Память задачи === (пусто)")
		return
	}
	fmt.Println("=== Память задачи ===")
	if g := strings.TrimSpace(tm.Goal); g != "" {
		fmt.Printf("Цель диалога: %s\n", g)
	}
	if len(tm.Clarified) > 0 {
		fmt.Println("Уже уточнено:")
		for _, c := range tm.Clarified {
			fmt.Printf("  - %s\n", c)
		}
	}
	if len(tm.Constraints) > 0 {
		fmt.Println("Зафиксированные ограничения/термины:")
		for _, c := range tm.Constraints {
			fmt.Printf("  - %s\n", c)
		}
	}
}

// runRAGInteractive runs an interactive RAG session: it reads a question per line
// from stdin and answers each with the grounded pipeline (retrieve → [rerank] →
// filter → answer with sources + quotes). Across turns it (1) persists the
// dialogue history, (2) re-retrieves fresh context for every new question, and
// (3) maintains a task memory of the dialogue goal, what the user has already
// clarified, and the fixed constraints/terms — injecting it into every answer.
// /exit or /quit (or EOF) ends the loop; /memory prints the task memory.
func runRAGInteractive(ragUC *usecase.RAGUseCase, llmClient port.LLMClient, model string, maxTokens int, ragDB string, cfg usecase.RAGConfig, histRepo port.HistoryRepository, taskMemRepo port.TaskMemoryRepository, debug bool) {
	fmt.Fprintln(os.Stderr, "[rag] Interactive session. Type a question and press Enter.")
	fmt.Fprintln(os.Stderr, "[rag] Commands: /exit or /quit to leave, /memory to show the task memory.")

	// Requirement 1: keep the dialogue history — load any prior turns so the
	// session resumes, and save after each exchange below.
	history, err := histRepo.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[rag] warning: could not load history: %v\n", err)
	}
	// Requirement 3: task memory — restore goal / clarified / constraints.
	taskMem, err := taskMemRepo.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[rag] warning: could not load task memory: %v\n", err)
	}
	if len(history) > 0 {
		fmt.Fprintf(os.Stderr, "[rag] resumed dialogue with %d prior message(s)\n", len(history))
	}
	if !taskMem.IsEmpty() {
		fmt.Fprintf(os.Stderr, "[rag] resumed task memory:\n%s\n", usecase.TaskMemorySystemBlock(taskMem))
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long questions
	for {
		fmt.Print("\nrag> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "\n[rag] input error: %v\n", err)
			} else {
				fmt.Fprintln(os.Stderr, "\n[rag] session ended")
			}
			return
		}
		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}
		// Slash commands are handled locally and never sent to the RAG pipeline.
		if strings.HasPrefix(question, "/") {
			switch strings.ToLower(strings.Fields(question)[0]) {
			case "/exit", "/quit", "/выход":
				fmt.Fprintln(os.Stderr, "[rag] bye")
				return
			case "/memory", "/память":
				printTaskMemory(taskMem)
			case "/help", "/помощь", "/?":
				fmt.Println("Команды: /help /помощь — эта справка; /memory /память — память задачи; /exit /quit /выход — выход.")
			default:
				fmt.Printf("Неизвестная команда %q. /help — список команд.\n", strings.Fields(question)[0])
			}
			continue
		}

		// Requirement 2: search fresh context via RAG for every new question,
		// grounded on the dialogue's task memory and recent history.
		actx, acancel := context.WithTimeout(context.Background(), 120*time.Second)
		conv := usecase.ConversationContext{
			History:    usecase.SlidingWindow(history, ragHistoryWindow),
			TaskMemory: taskMem,
		}
		ans, res, err := ragUC.AnswerWithContext(actx, llmClient, model, maxTokens, question, cfg, conv)
		acancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[rag] error: %v\n", err)
			continue
		}
		printRAGResult(ans, res, ragDB, cfg.Threshold)

		// Requirement 1: persist the dialogue turn.
		history = append(history,
			domain.Message{Role: domain.RoleUser, Content: question},
			domain.Message{Role: domain.RoleAssistant, Content: ans.Answer},
		)
		if err := histRepo.Save(history); err != nil {
			fmt.Fprintf(os.Stderr, "[rag] warning: could not save history: %v\n", err)
		}

		// Requirement 3: update the task memory (goal / clarified / constraints)
		// from this exchange and persist it. Give it the same budget as the answer
		// call — a reasoning model needs the headroom to think before emitting JSON.
		mctx, mcancel := context.WithTimeout(context.Background(), 120*time.Second)
		updated, err := usecase.UpdateTaskMemory(mctx, llmClient, model, maxTokens, taskMem, question, ans.Answer, debug)
		mcancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[rag] warning: task memory not updated: %v\n", err)
			continue
		}
		taskMem = updated
		if err := taskMemRepo.Save(taskMem); err != nil {
			fmt.Fprintf(os.Stderr, "[rag] warning: could not save task memory: %v\n", err)
		}
	}
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

func printMCPServers(servers []domain.MCPServerConfig, configFile string) {
	fmt.Printf("MCP config: %s\n", configFile)
	if len(servers) == 0 {
		fmt.Println("No MCP servers configured.")
		return
	}
	fmt.Printf("Configured MCP servers (%d):\n\n", len(servers))
	for _, s := range servers {
		fmt.Printf("  Name:    %s\n", s.Name)
		fmt.Printf("  Type:    %s\n", s.Type)
		switch s.Type {
		case domain.MCPStdio:
			fmt.Printf("  Command: %s\n", s.Command)
			if len(s.Args) > 0 {
				fmt.Printf("  Args:    %s\n", strings.Join(s.Args, " "))
			}
			if len(s.Env) > 0 {
				fmt.Printf("  Env:     %s\n", strings.Join(s.Env, " "))
			}
		case domain.MCPSSE:
			fmt.Printf("  URL:     %s\n", s.URL)
		}
		fmt.Println()
	}
}

func printMCPTools(results map[string][]domain.MCPTool) {
	if len(results) == 0 {
		fmt.Println("No tools found.")
		return
	}
	// Print in deterministic order
	names := make([]string, 0, len(results))
	for n := range results {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, serverName := range names {
		tools := results[serverName]
		fmt.Printf("=== Server: %s (%d tools) ===\n\n", serverName, len(tools))
		if len(tools) == 0 {
			fmt.Println("  (no tools exposed)")
			fmt.Println()
			continue
		}
		for i, t := range tools {
			fmt.Printf("  %d. %s\n", i+1, t.Name)
			if t.Description != "" {
				fmt.Printf("     %s\n", t.Description)
			}
			fmt.Println()
		}
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	configPath := flag.String("config", "", "Path to the agent YAML config. When set, runs the orchestrator app: the config drives memory layers, RAG and MCP; explicit flags override it. Env: AGENT_CONFIG")
	noTUI := flag.Bool("no-tui", false, "In orchestrator --interactive mode, use the plain line REPL instead of the full-screen TUI")
	query := flag.String("query", "", "The query string to send to the LLM (required in CLI mode)")
	interactive := flag.Bool("interactive", false, "Run in interactive mode with 4-phase task state machine")
	format := flag.String("format", "markdown", "Response format: markdown or json")
	maxTokens := flag.Int("max-tokens", 0, "Maximum tokens in response (0 = no limit)")
	caCert := flag.String("ca-cert", os.Getenv("LLM_CA_CERT"), "Path to a PEM cert to trust in addition to system roots (for a self-signed HTTPS endpoint, e.g. a private LiteLLM proxy). Env: LLM_CA_CERT")
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

	profileFile := flag.String("profile", "", "User profile file (.md). Default: derived from --history")
	profileInit := flag.Bool("profile-init", false, "[profile] Interactive profile initialization and exit")
	profileName := flag.String("profile-name", "", "[profile] Set user name and exit")
	profileList := flag.Bool("profile-list", false, "[profile] Print current profile and exit")
	profileDelete := flag.String("profile-delete", "", "[profile] Delete a preference by key and exit")

	invariantsFile := flag.String("invariants", "", "Invariants file (.md) — absolute constraints the agent must never violate. Default: derived from --history")

	// RAG flags (CLI mode): question → retrieve relevant chunks → combine with question → LLM
	ragEnabled := flag.Bool("rag", false, "Enable RAG: retrieve relevant chunks from the index and ground the query on them")
	ragDB := flag.String("rag-db", "rag.db", "SQLite index path (built by the `rag` component) used for retrieval")
	ragTopK := flag.Int("rag-top-k", 10, "Top-K chunks to retrieve BEFORE filtering (retrieval pool size)")
	ragRerank := flag.Bool("rag-rerank", false, "Enable the rerank stage: re-score retrieved chunks with a dedicated LLM before filtering")
	ragRerankModel := flag.String("rag-rerank-model", "", "Model used for the rerank stage (default: the main LLM_MODEL)")
	ragRerankMode := flag.String("rag-rerank-mode", "auto", "Rerank API shape: 'api' (dedicated /rerank endpoint, e.g. cohere/rerank-*), 'chat' (score via a chat model), or 'auto' (api when the model name contains 'rerank', else chat)")
	ragRerankProvider := flag.String("rag-rerank-provider", "", "Provider for the rerank model (openai|openrouter|gigachat; default: main LLM_PROVIDER)")
	ragRerankURL := flag.String("rag-rerank-url", "", "Base URL for the rerank model endpoint (default: main provider's URL)")
	ragRerankKey := flag.String("rag-rerank-key", "", "API key for the rerank model endpoint (default: the main LLM_API_KEY)")
	ragThreshold := flag.Float64("rag-threshold", 0, "Drop chunks scoring below this relevance threshold (0..1; 0 disables the cutoff)")
	ragTopKFinal := flag.Int("rag-top-k-final", 5, "Top-K chunks to keep AFTER reranking/filtering (0 = keep all above threshold)")
	ragEmbedURL := flag.String("rag-embed-url", "http://localhost:11434", "Embeddings API base URL for RAG retrieval")
	ragEmbedModel := flag.String("rag-embed-model", "nomic-embed-text", "Embeddings model for RAG (must match the index)")
	ragEmbedKey := flag.String("rag-embed-key", "", "Embeddings API key for RAG (empty for local runtimes)")

	var profileSetFlags stringSlice
	flag.Var(&profileSetFlags, "profile-set", "[profile] Set a preference key=value and exit (repeatable)")

	var stopSequences stringSlice
	flag.Var(&stopSequences, "stop", "Stop sequence (repeatable)")

	// MCP flags
	mcpConfigFile := flag.String("mcp-config", "", "MCP server config file (.yaml). Default: derived from --history")
	mcpAdd := flag.Bool("mcp-add", false, "[MCP] Add a new MCP server defined by --mcp-name/type/command/args/url/env")
	mcpRemove := flag.String("mcp-remove", "", "[MCP] Remove MCP server by name")
	mcpList := flag.Bool("mcp-list", false, "[MCP] List all configured MCP servers")
	mcpTools := flag.String("mcp-tools", "", "[MCP] List tools from the named MCP server (empty = all servers)")
	mcpToolsAll := flag.Bool("mcp-tools-all", false, "[MCP] List tools from all configured MCP servers")
	mcpName := flag.String("mcp-name", "", "[MCP] Server name (used with --mcp-add)")
	mcpType := flag.String("mcp-type", "stdio", "[MCP] Server type: stdio or sse (used with --mcp-add)")
	mcpCommand := flag.String("mcp-command", "", "[MCP] Executable for stdio server (used with --mcp-add)")
	mcpArgs := flag.String("mcp-args", "", "[MCP] Space-separated args for stdio server (used with --mcp-add)")
	mcpURL := flag.String("mcp-url", "", "[MCP] SSE endpoint URL (used with --mcp-add)")
	var mcpEnv stringSlice
	flag.Var(&mcpEnv, "mcp-env", "[MCP] Extra env var KEY=VALUE for stdio server (repeatable, used with --mcp-add)")

	flag.Parse()

	// ----------------------------------------------------------------
	// Orchestrator mode — driven by a YAML config (Day31). When --config
	// (or AGENT_CONFIG) is set, the config drives memory/RAG/MCP and the main
	// process runs as an orchestrator that spawns and routes sub-agents.
	// Explicit flags override the config. The legacy single-agent flag paths
	// below are used only when no config is given.
	// ----------------------------------------------------------------
	resolvedConfigPath := *configPath
	if resolvedConfigPath == "" {
		resolvedConfigPath = os.Getenv("AGENT_CONFIG")
	}
	if resolvedConfigPath != "" {
		cf := &cliFlags{
			query: query, interactive: interactive, noTUI: noTUI, debug: debug,
			maxTokens: maxTokens, temperature: temperature, caCert: caCert,
			historyFile: historyFile, historyLimit: historyLimit, summaryEnabled: summaryEnabled,
			strategyFlag: strategyFlag, windowSize: windowSize, memoryUpdate: memoryUpdate,
			profileFile: profileFile, invariantsFile: invariantsFile,
			ragEnabled: ragEnabled, ragDB: ragDB, ragTopK: ragTopK,
			ragRerank: ragRerank, ragRerankModel: ragRerankModel, ragRerankMode: ragRerankMode,
			ragRerankProvider: ragRerankProvider, ragRerankURL: ragRerankURL, ragRerankKey: ragRerankKey,
			ragThreshold: ragThreshold, ragTopKFinal: ragTopKFinal,
			ragEmbedURL: ragEmbedURL, ragEmbedModel: ragEmbedModel, ragEmbedKey: ragEmbedKey,
			mcpConfigFile: mcpConfigFile,
		}
		if err := runOrchestrator(resolvedConfigPath, cf); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

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
	// Resolve profile file path
	// ----------------------------------------------------------------
	resolvedProfileFile := *profileFile
	if resolvedProfileFile == "" && *historyFile != "" {
		resolvedProfileFile = storage.ProfilePath(*historyFile)
	}

	// ----------------------------------------------------------------
	// Profile initialization (interactive)
	// ----------------------------------------------------------------
	if *profileInit {
		if err := runProfileInit(resolvedProfileFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// ----------------------------------------------------------------
	// Profile management commands — no --query required
	// ----------------------------------------------------------------
	if *profileName != "" || len(profileSetFlags) > 0 || *profileDelete != "" || *profileList {
		if resolvedProfileFile == "" {
			fmt.Fprintln(os.Stderr, "Error: profile path is unknown — set --history or --profile to specify a file")
			os.Exit(1)
		}
		profileRepo := storage.NewProfileFile(resolvedProfileFile)
		profileUC := usecase.NewProfileUseCase(profileRepo)

		if *profileName != "" {
			if err := profileUC.SetName(*profileName); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting profile name: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[profile] name set to %q\n", *profileName)
		}

		for _, kv := range profileSetFlags {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				fmt.Fprintf(os.Stderr, "Error: --profile-set requires key=value format, got %q\n", kv)
				os.Exit(1)
			}
			if err := profileUC.Set(k, v); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting preference %q: %v\n", k, err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[profile] preference set: %s=%s\n", k, v)
		}

		if *profileDelete != "" {
			if err := profileUC.Delete(*profileDelete); err != nil {
				fmt.Fprintf(os.Stderr, "Error deleting preference %q: %v\n", *profileDelete, err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[profile] preference deleted: %s\n", *profileDelete)
		}

		if *profileList {
			p, err := profileUC.Profile()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading profile: %v\n", err)
				os.Exit(1)
			}
			printProfile(p, resolvedProfileFile)
		}

		return
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
	// MCP management commands — no --query / LLM_PROVIDER required
	// ----------------------------------------------------------------
	isMCPCommand := *mcpAdd || *mcpRemove != "" || *mcpList || *mcpTools != "" || *mcpToolsAll
	if isMCPCommand {
		// Resolve MCP config file path
		resolvedMCPFile := *mcpConfigFile
		if resolvedMCPFile == "" && *historyFile != "" {
			resolvedMCPFile = storage.MCPPath(*historyFile)
		}
		if resolvedMCPFile == "" {
			fmt.Fprintln(os.Stderr, "Error: MCP config path unknown — set --mcp-config or --history")
			os.Exit(1)
		}

		mcpRepo := storage.NewMCPConfigFile(resolvedMCPFile)
		mcpClient := mcpadapter.NewClient()
		mcpUC := usecase.NewMCPUseCase(mcpRepo, mcpClient)

		switch {
		case *mcpAdd:
			var args []string
			if *mcpArgs != "" {
				args = strings.Fields(*mcpArgs)
			}
			cfg := domain.MCPServerConfig{
				Name:    *mcpName,
				Type:    domain.MCPServerType(*mcpType),
				Command: *mcpCommand,
				Args:    args,
				URL:     *mcpURL,
				Env:     []string(mcpEnv),
			}
			if err := mcpUC.AddServer(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[mcp] server %q added to %s\n", cfg.Name, resolvedMCPFile)

		case *mcpRemove != "":
			if err := mcpUC.RemoveServer(*mcpRemove); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[mcp] server %q removed from %s\n", *mcpRemove, resolvedMCPFile)

		case *mcpList:
			servers, err := mcpUC.ListServers()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			printMCPServers(servers, resolvedMCPFile)

		case *mcpTools != "" || *mcpToolsAll:
			filter := *mcpTools
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			results, errs := mcpUC.ListTools(ctx, filter)
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "[mcp] warning: %v\n", e)
			}
			printMCPTools(results)
			if len(results) == 0 {
				os.Exit(1)
			}
		}
		return
	}

	// ----------------------------------------------------------------
	// Validate provider (shared by both CLI and interactive modes)
	// ----------------------------------------------------------------
	provider := os.Getenv("LLM_PROVIDER")
	if provider == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}
	if provider != llm.ProviderOpenAI && provider != llm.ProviderOpenRouter && provider != llm.ProviderGigaChat {
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER %q (supported: openai, openrouter, gigachat)\n", provider)
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
		switch provider {
		case llm.ProviderOpenRouter:
			baseURL = defaultOpenRouterBaseURL
		case llm.ProviderGigaChat:
			baseURL = llm.DefaultGigaChatBaseURL
		default:
			baseURL = defaultOpenAIBaseURL
		}
	}
	switch {
	case provider == llm.ProviderOpenAI && model == "":
		model = defaultOpenAIModel
	case provider == llm.ProviderGigaChat && model == "":
		model = defaultGigaChatModel
	}

	// GigaChat scope: GIGACHAT_API_PERS for individuals (default), can be overridden.
	gigaChatScope := os.Getenv("GIGACHAT_SCOPE")

	// ----------------------------------------------------------------
	// Wire shared adapters
	// ----------------------------------------------------------------
	llmClient := llm.NewClient(llm.Config{
		Provider:      provider,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		GigaChatScope: gigaChatScope,
		CACertFile:    *caCert,
	})

	// Resolve actual history file (branch-aware) — shared helper
	actualHistoryFileShared := *historyFile
	if strat == usecase.StrategyBranching && *historyFile != "" {
		branchRepo := storage.NewBranchFile(*historyFile)
		bs, _ := branchRepo.LoadState()
		actualHistoryFileShared = storage.CurrentBranchHistoryPath(*historyFile, bs)
		fmt.Fprintf(os.Stderr, "[branch] active branch: %q → %s\n", bs.Current, actualHistoryFileShared)
	}

	resolvedWMFileShared := *memoryWMFile
	if resolvedWMFileShared == "" && actualHistoryFileShared != "" {
		resolvedWMFileShared = storage.WMPath(actualHistoryFileShared)
	}
	resolvedLTMFileShared := *memoryLTMFile
	if resolvedLTMFileShared == "" && actualHistoryFileShared != "" {
		resolvedLTMFileShared = storage.LTMPath(actualHistoryFileShared)
	}

	// Resolve invariants file path (only relevant for --interactive mode).
	resolvedInvariantsFile := *invariantsFile
	if resolvedInvariantsFile == "" && actualHistoryFileShared != "" {
		resolvedInvariantsFile = storage.InvariantsPath(actualHistoryFileShared)
	}

	historyRepo := storage.NewHistoryFile(actualHistoryFileShared)
	statsRepo := storage.NewStatsFile(storage.StatsPath(actualHistoryFileShared))
	summaryRepo := storage.NewSummaryFile(storage.SummaryPath(actualHistoryFileShared))
	factsRepo := storage.NewFactsFile(storage.FactsPath(actualHistoryFileShared))
	wmRepo := storage.NewWorkingMemoryFile(resolvedWMFileShared)
	ltmRepo := storage.NewLongTermMemoryFile(resolvedLTMFileShared)
	profileRepo := storage.NewProfileFile(resolvedProfileFile)
	invariantsRepo := storage.NewInvariantsFile(resolvedInvariantsFile)

	chatUC := usecase.NewChatUseCase(llmClient, historyRepo, statsRepo, summaryRepo, factsRepo, wmRepo, ltmRepo, profileRepo)

	// ----------------------------------------------------------------
	// Interactive mode — 4-phase task state machine
	// ----------------------------------------------------------------
	// --rag --interactive is handled separately below (RAG REPL), so the FSM
	// agent only claims plain --interactive sessions.
	if *interactive && !*ragEnabled {
		taskRepo := storage.NewTaskFile(storage.TaskPath(actualHistoryFileShared))
		agentUC := usecase.NewAgentUseCase(chatUC, taskRepo, invariantsRepo)

		// Wire MCP if a config file is resolvable.
		resolvedMCPFileInteractive := *mcpConfigFile
		if resolvedMCPFileInteractive == "" && actualHistoryFileShared != "" {
			resolvedMCPFileInteractive = storage.MCPPath(actualHistoryFileShared)
		}
		if resolvedMCPFileInteractive != "" {
			mcpRepo := storage.NewMCPConfigFile(resolvedMCPFileInteractive)
			mcpClient := mcpadapter.NewClient()
			agentUC.WithMCP(usecase.NewMCPUseCase(mcpRepo, mcpClient)).
				WithMCPCaller(mcpClient).
				WithMCPPoolOpener(mcpClient)
			fmt.Fprintf(os.Stderr, "  MCP config       : %s\n", resolvedMCPFileInteractive)
		}

		// Print resolved file paths so the user knows where all state is stored.
		fmt.Fprintln(os.Stderr, "[session] Memory file paths:")
		fmt.Fprintf(os.Stderr, "  STM  (history)   : %s\n", actualHistoryFileShared)
		fmt.Fprintf(os.Stderr, "  WM   (working)   : %s\n", resolvedWMFileShared)
		fmt.Fprintf(os.Stderr, "  LTM  (long-term) : %s\n", resolvedLTMFileShared)
		fmt.Fprintf(os.Stderr, "  Profile          : %s\n", resolvedProfileFile)
		fmt.Fprintf(os.Stderr, "  Invariants       : %s\n", resolvedInvariantsFile)
		fmt.Fprintf(os.Stderr, "  Task state       : %s\n", storage.TaskPath(actualHistoryFileShared))

		agentCfg := usecase.ChatConfig{
			Model:          model,
			SystemMessage:  *system,
			MaxTokens:      *maxTokens,
			Temperature:    *temperature,
			HistoryLimit:   *historyLimit,
			SummaryEnabled: *summaryEnabled,
			Strategy:       strat,
			WindowSize:     *windowSize,
			MemoryUpdate:   *memoryUpdate,
			Debug:          *debug,
			ShowTokens:     *showTokens,
		}
		if err := agentUC.Run(agentCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// ----------------------------------------------------------------
	// From here --query is required (CLI mode) — except a RAG interactive
	// session (--rag --interactive), which reads questions from the prompt.
	// ----------------------------------------------------------------
	ragInteractive := *ragEnabled && *interactive
	if *query == "" && !ragInteractive {
		fmt.Fprintf(os.Stderr, "Error: --query flag is required (or use --interactive for interactive mode)\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// ----------------------------------------------------------------
	// CLI mode: resolve format instruction and run single query
	// ----------------------------------------------------------------
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ----------------------------------------------------------------
	// RAG pipeline: question → retrieve → [rerank] → filter → combine with question
	// The augmented text then flows into the normal LLM call below.
	// ----------------------------------------------------------------
	groundedQuery := *query
	if *ragEnabled {
		retriever, err := ragadapter.NewRetriever(ragadapter.Config{
			DBPath:     *ragDB,
			EmbedURL:   *ragEmbedURL,
			EmbedModel: *ragEmbedModel,
			EmbedKey:   *ragEmbedKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: RAG retrieval unavailable: %v\n", err)
			os.Exit(1)
		}
		defer retriever.Close()

		// The rerank stage runs on its own model — a dedicated cross-encoder /
		// scoring model kept separate from the answer-generating LLM. Endpoint,
		// provider and key each fall back to the main LLM config when not set,
		// so `--rag-rerank-model` alone is enough to point it at a different model
		// on the same provider.
		var reranker port.Reranker
		if *ragRerank {
			rerankProvider := *ragRerankProvider
			if rerankProvider == "" {
				rerankProvider = provider
			}
			if rerankProvider != llm.ProviderOpenAI && rerankProvider != llm.ProviderOpenRouter && rerankProvider != llm.ProviderGigaChat {
				fmt.Fprintf(os.Stderr, "Error: unsupported --rag-rerank-provider %q (supported: openai, openrouter, gigachat)\n", rerankProvider)
				os.Exit(1)
			}
			rerankKey := *ragRerankKey
			if rerankKey == "" {
				rerankKey = apiKey
			}
			rerankModel := *ragRerankModel
			if rerankModel == "" {
				rerankModel = model
			}
			rerankBaseURL := strings.TrimRight(*ragRerankURL, "/")
			if rerankBaseURL == "" {
				if rerankProvider == provider {
					rerankBaseURL = baseURL // reuse the already-resolved main URL
				} else {
					switch rerankProvider {
					case llm.ProviderOpenRouter:
						rerankBaseURL = defaultOpenRouterBaseURL
					case llm.ProviderGigaChat:
						rerankBaseURL = llm.DefaultGigaChatBaseURL
					default:
						rerankBaseURL = defaultOpenAIBaseURL
					}
				}
			}
			// Dedicated rerank models (cohere/rerank-*, rerank-v3.5, jina-reranker-*)
			// are cross-encoders served on a Cohere-style /rerank endpoint, not on
			// /chat/completions — driving them through a chat client 400s. Pick the
			// transport by mode: 'api' for those, 'chat' to score with a chat model,
			// 'auto' detects a rerank model by its name.
			useAPI := false
			switch strings.ToLower(*ragRerankMode) {
			case "api":
				useAPI = true
			case "chat":
				useAPI = false
			case "auto":
				useAPI = strings.Contains(strings.ToLower(rerankModel), "rerank")
			default:
				fmt.Fprintf(os.Stderr, "Error: invalid --rag-rerank-mode %q (want: api, chat, or auto)\n", *ragRerankMode)
				os.Exit(1)
			}

			if useAPI {
				reranker = ragadapter.NewAPIReranker(rerankBaseURL, rerankModel, rerankKey)
				fmt.Fprintf(os.Stderr, "[rag] rerank model: %s (provider %s, dedicated /rerank endpoint)\n", rerankModel, rerankProvider)
			} else {
				rerankClient := llm.NewClient(llm.Config{
					Provider:      rerankProvider,
					APIKey:        rerankKey,
					BaseURL:       rerankBaseURL,
					GigaChatScope: gigaChatScope,
				})
				reranker = ragadapter.NewLLMReranker(rerankClient, rerankModel)
				fmt.Fprintf(os.Stderr, "[rag] rerank model: %s (provider %s, chat scoring)\n", rerankModel, rerankProvider)
			}
		}

		ragUC := usecase.NewRAGUseCase(retriever, reranker)
		ragCfg := usecase.RAGConfig{
			TopKRetrieve: *ragTopK,
			Rerank:       *ragRerank,
			Threshold:    *ragThreshold,
			TopKFinal:    *ragTopKFinal,
		}

		// Interactive RAG session: read questions from the prompt and answer each
		// with the grounded pipeline (answer + sources + quotes), persisting the
		// dialogue history and the task memory (goal / clarified / constraints)
		// derived from the --history path.
		if ragInteractive {
			histRepo := storage.NewHistoryFile(*historyFile)
			taskMemRepo := storage.NewTaskMemoryFile(storage.TaskMemoryPath(*historyFile))
			runRAGInteractive(ragUC, llmClient, model, *maxTokens, *ragDB, ragCfg, histRepo, taskMemRepo, *debug)
			return
		}

		ans, res, err := ragUC.Answer(ctx, llmClient, model, *maxTokens, *query, ragCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: RAG answer failed: %v\n", err)
			os.Exit(1)
		}
		printRAGResult(ans, res, *ragDB, *ragThreshold)
		return
	}

	fullQuery := groundedQuery + "\n\n" + instruction

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
