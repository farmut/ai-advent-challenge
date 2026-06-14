package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL     = "https://api.openai.com/v1"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenAIModel       = "gpt-4o"
)

const (
	providerOpenAI     = "openai"
	providerOpenRouter = "openrouter"
)

type role string

const (
	roleUser      role = "user"
	roleSystem    role = "system"
	roleAssistant role = "assistant"
)

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type Message struct {
	Role    role   `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stop     []string  `json:"stop,omitempty"`

	MaxTokens           *int     `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
}

type chatChoice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   usage        `json:"usage"`
}

type errorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// sessionStats accumulates token usage across all calls in a session.
type sessionStats struct {
	PromptTokens         int     `json:"prompt_tokens"`
	CompletionTokens     int     `json:"completion_tokens"`
	TotalTokens          int     `json:"total_tokens"`
	EstimatedCostUSD     float64 `json:"estimated_cost_usd"`
	Calls                int     `json:"calls"`
	LastCallPromptTokens int     `json:"last_call_prompt_tokens"` // prompt tokens from the previous call, for growth tracking
}

// modelPrice holds per-1M-token prices (USD) and the context window size for a model.
type modelPrice struct {
	InputPer1M    float64
	OutputPer1M   float64
	ContextWindow int // max tokens the model accepts in a single request
}

// knownPrices covers widely-used models. Keys match both raw and OpenRouter-prefixed names.
var knownPrices = map[string]modelPrice{
	// OpenAI — direct names
	"gpt-4o":        {2.50, 10.00, 128_000},
	"gpt-4o-mini":   {0.15, 0.60, 128_000},
	"gpt-4-turbo":   {10.00, 30.00, 128_000},
	"gpt-3.5-turbo": {0.50, 1.50, 16_385},
	"o1":            {15.00, 60.00, 200_000},
	"o1-mini":       {3.00, 12.00, 128_000},
	// OpenAI — OpenRouter prefixes
	"openai/gpt-4o":        {2.50, 10.00, 128_000},
	"openai/gpt-4o-mini":   {0.15, 0.60, 128_000},
	"openai/gpt-4-turbo":   {10.00, 30.00, 128_000},
	"openai/gpt-3.5-turbo": {0.50, 1.50, 16_385},
	"openai/o1":             {15.00, 60.00, 200_000},
	"openai/o1-mini":        {3.00, 12.00, 128_000},
	// Anthropic — OpenRouter prefixes
	"anthropic/claude-opus-4":     {15.00, 75.00, 200_000},
	"anthropic/claude-sonnet-4":   {3.00, 15.00, 200_000},
	"anthropic/claude-3-5-sonnet": {3.00, 15.00, 200_000},
	"anthropic/claude-3-haiku":    {0.25, 1.25, 200_000},
	"anthropic/claude-3-opus":     {15.00, 75.00, 200_000},
	// Qwen — OpenRouter
	"qwen/qwen3.5-9b":        {0.06, 0.24, 32_768},
	"qwen/qwen3.5-122b-a10b": {0.14, 0.56, 32_768},
	"qwen/qwen3.5-397b-a17b": {0.40, 1.60, 32_768},
}

var formatInstructions = map[string]string{
	"markdown": "Format your response using Markdown.",
	"json":     "Respond in JSON format.",
}

// estimateTokens returns a rough token count (~4 chars per token).
func estimateTokens(text string) int {
	n := len([]rune(text)) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// estimateMessagesTokens estimates the total prompt tokens for a slice of messages.
// Uses ~4 chars/token plus ~4 tokens of per-message overhead (role, delimiters).
func estimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += estimateTokens(m.Content) + 4
	}
	return total
}

// statsPath derives the session-stats file path from the history file path.
func statsPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".stats" + ext
}

// summaryPath derives the summary file path from the history file path.
func summaryPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".summary.txt"
}

// modelPricing returns the pricing for model, and whether it was found.
func modelPricing(model string) (modelPrice, bool) {
	// Try exact match first, then strip ":free" / ":extended" suffixes used by OpenRouter.
	if p, ok := knownPrices[model]; ok {
		return p, true
	}
	if idx := strings.LastIndex(model, ":"); idx != -1 {
		if p, ok := knownPrices[model[:idx]]; ok {
			return p, true
		}
	}
	return modelPrice{}, false
}

func loadStats(path string) (sessionStats, error) {
	if path == "" {
		return sessionStats{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return sessionStats{}, nil
	}
	if err != nil {
		return sessionStats{}, err
	}
	var s sessionStats
	if err := json.Unmarshal(data, &s); err != nil {
		return sessionStats{}, fmt.Errorf("invalid stats file: %w", err)
	}
	return s, nil
}

func saveStats(path string, s sessionStats) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// contextStatus returns a short status label based on context fill percentage.
func contextStatus(pct float64) string {
	switch {
	case pct >= 95:
		return "[CRITICAL: context almost full!]"
	case pct >= 80:
		return "[WARN: context filling up]"
	case pct >= 50:
		return "[NOTE: context half full]"
	default:
		return "[OK]"
	}
}

func printTokenInfo(model, queryText string, u usage, stats sessionStats, prevPromptTokens int, showCost bool) {
	queryEst := estimateTokens(queryText)
	// history context = full prompt sent to API minus the estimated current message
	historyTokens := u.PromptTokens - queryEst
	if historyTokens < 0 {
		historyTokens = 0
	}

	fmt.Fprintf(os.Stderr,
		"[tokens]  query≈%-6d  history=%-6d  response=%-6d | session (%d calls): prompt=%d  completion=%d  total=%d\n",
		queryEst, historyTokens, u.CompletionTokens,
		stats.Calls, stats.PromptTokens, stats.CompletionTokens, stats.TotalTokens,
	)

	price, known := modelPricing(model)

	// Context window line (shown with --show-tokens regardless of --show-cost).
	if known && price.ContextWindow > 0 {
		used := u.PromptTokens
		window := price.ContextWindow
		pct := float64(used) / float64(window) * 100
		remaining := window - used
		status := contextStatus(pct)

		growth := used - prevPromptTokens // tokens added relative to previous call
		if stats.Calls > 1 && growth > 0 && remaining > 0 {
			callsUntilFull := remaining / growth
			fmt.Fprintf(os.Stderr,
				"[context] %d / %d (%.1f%% used, %d remaining, +%d/call, ~%d calls until full)  %s\n",
				used, window, pct, remaining, growth, callsUntilFull, status,
			)
		} else {
			fmt.Fprintf(os.Stderr,
				"[context] %d / %d (%.1f%% used, %d remaining)  %s\n",
				used, window, pct, remaining, status,
			)
		}
	} else {
		fmt.Fprintf(os.Stderr,
			"[context] unknown window for %q — add it to knownPrices for context tracking\n", model,
		)
	}

	if !showCost {
		return
	}

	if !known {
		fmt.Fprintf(os.Stderr, "[cost]    unknown model %q — add it to knownPrices for cost tracking\n", model)
		return
	}

	callCost := float64(u.PromptTokens)/1_000_000*price.InputPer1M +
		float64(u.CompletionTokens)/1_000_000*price.OutputPer1M

	fmt.Fprintf(os.Stderr,
		"[cost]    this call=$%.6f  session=$%.6f  (%s: $%.2f/$%.2f per 1M tokens in/out)\n",
		callCost, stats.EstimatedCostUSD, model, price.InputPer1M, price.OutputPer1M,
	)
}

func main() {
	query := flag.String("query", "", "The query string to send to the LLM (required)")
	format := flag.String("format", "markdown", "Response format: markdown or json (adds instruction to prompt)")
	maxTokens := flag.Int("max-tokens", 0, "Maximum number of tokens in response (0 = no limit)")
	debug := flag.Bool("debug", false, "Print request payload and token usage to stderr")
	formatHint := flag.String("format-hint", "", "Custom formatting instruction (overrides --format)")
	system := flag.String("system", "", "System message to set the assistant's behavior (optional)")
	temperature := flag.Float64("temperature", -1, "Sampling temperature, 0.0-2.0 (default: not set, provider default used)")
	historyFile := flag.String("history", "chat_history.json", "Path to chat history file (empty string disables history)")
	historyLimit := flag.Int("history-limit", 10, "Max messages to keep in history file; older messages are summarized (0 = unlimited)")
	showTokens := flag.Bool("show-tokens", false, "Print token breakdown (query / history / response / session) to stderr")
	showCost := flag.Bool("show-cost", false, "Print estimated session cost to stderr (implies --show-tokens)")
	var stopSequences stringSlice
	flag.Var(&stopSequences, "stop", "Stop sequence (can be specified multiple times)")
	flag.Parse()

	if *query == "" {
		fmt.Fprintf(os.Stderr, "Error: --query flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if *showCost {
		*showTokens = true
	}

	var instruction string
	if *formatHint != "" {
		if *format != "markdown" {
			fmt.Fprintf(os.Stderr, "Warning: --format-hint overrides --format, ignoring --format=%s\n", *format)
		}
		instruction = *formatHint
	} else {
		var formatOk bool
		instruction, formatOk = formatInstructions[*format]
		if !formatOk {
			fmt.Fprintf(os.Stderr, "Error: --format must be 'markdown' or 'json'\n")
			os.Exit(1)
		}
	}

	providerStr := os.Getenv("LLM_PROVIDER")
	if providerStr == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}

	if providerStr != providerOpenAI && providerStr != providerOpenRouter {
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER '%s'\nSupported: openai, openrouter\n", providerStr)
		os.Exit(1)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_API_KEY environment variable is required\n")
		os.Exit(1)
	}

	model := os.Getenv("LLM_MODEL")
	baseURL := os.Getenv("LLM_BASE_URL")

	if baseURL == "" {
		if providerStr == providerOpenRouter {
			baseURL = defaultOpenRouterBaseURL
		} else {
			baseURL = defaultOpenAIBaseURL
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if providerStr == providerOpenAI && model == "" {
		model = defaultOpenAIModel
	}

	fullQuery := *query + "\n\n" + instruction

	history, err := loadHistory(*historyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load history from %q: %v\n", *historyFile, err)
	}

	sPath := statsPath(*historyFile)
	sumFilePath := summaryPath(*historyFile)

	stats, err := loadStats(sPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load stats from %q: %v\n", sPath, err)
	}

	existingSummary, err := loadSummary(sumFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load summary from %q: %v\n", sumFilePath, err)
	}

	// Merge --system flag with existing summary (summary injected as part of system message).
	systemContent := *system
	if existingSummary != "" {
		if systemContent != "" {
			systemContent += "\n\n---\nSummary of earlier conversation:\n" + existingSummary
		} else {
			systemContent = "Summary of earlier conversation:\n" + existingSummary
		}
	}

	var messages []Message
	if systemContent != "" {
		messages = append(messages, Message{Role: roleSystem, Content: systemContent})
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: roleUser, Content: fullQuery})

	// Pre-call context estimation — shown before the request so it's visible even on API error.
	if *showTokens {
		if p, known := modelPricing(model); known && p.ContextWindow > 0 {
			est := estimateMessagesTokens(messages)
			pct := float64(est) / float64(p.ContextWindow) * 100
			remaining := p.ContextWindow - est
			status := contextStatus(pct)
			if remaining < 0 {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%) — EXCEEDS LIMIT by %d tokens  %s\n",
					est, p.ContextWindow, pct, -remaining, status,
				)
			} else {
				fmt.Fprintf(os.Stderr,
					"[pre-call] estimated context: ~%d / %d tokens (%.1f%%, ~%d remaining)  %s\n",
					est, p.ContextWindow, pct, remaining, status,
				)
			}
		}
	}

	chatResp, elapsed, err := sendChatRequest(providerStr, apiKey, baseURL, model, messages, *maxTokens, []string(stopSequences), *temperature, *debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying LLM: %v\n", err)
		os.Exit(1)
	}

	if *debug {
		finish := ""
		if len(chatResp.Choices) > 0 {
			finish = chatResp.Choices[0].FinishReason
		}
		fmt.Fprintf(os.Stderr, "\n[usage] prompt_tokens=%d, completion_tokens=%d, total_tokens=%d, finish_reason=%s, elapsed=%s\n\n",
			chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens, finish, elapsed.Round(time.Millisecond))
	}

	// Update session stats.
	prevPromptTokens := stats.LastCallPromptTokens

	price, known := modelPricing(model)
	var callCost float64
	if known {
		callCost = float64(chatResp.Usage.PromptTokens)/1_000_000*price.InputPer1M +
			float64(chatResp.Usage.CompletionTokens)/1_000_000*price.OutputPer1M
	}
	stats.Calls++
	stats.PromptTokens += chatResp.Usage.PromptTokens
	stats.CompletionTokens += chatResp.Usage.CompletionTokens
	stats.TotalTokens += chatResp.Usage.TotalTokens
	stats.EstimatedCostUSD += callCost
	stats.LastCallPromptTokens = chatResp.Usage.PromptTokens

	if *showTokens {
		printTokenInfo(model, fullQuery, chatResp.Usage, stats, prevPromptTokens, *showCost)
	}

	assistantContent := chatResp.Choices[0].Message.Content
	fmt.Println(assistantContent)

	// Append exchange to history, then trim if over limit.
	history = append(history, Message{Role: roleUser, Content: fullQuery})
	history = append(history, Message{Role: roleAssistant, Content: assistantContent})

	keep, excess := trimHistory(history, *historyLimit)
	if len(excess) > 0 {
		fmt.Fprintf(os.Stderr, "[summary] %d messages exceed limit — generating summary...\n", len(excess))
		newSummary, sumErr := buildSummary(excess, existingSummary, providerStr, apiKey, baseURL, model, *debug)
		if sumErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate history summary: %v\n", sumErr)
		} else {
			if err := saveSummary(sumFilePath, newSummary); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save summary to %q: %v\n", sumFilePath, err)
			} else {
				fmt.Fprintf(os.Stderr, "[summary] saved to %q\n", sumFilePath)
			}
		}
	}

	if err := saveHistory(*historyFile, keep); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save history to %q: %v\n", *historyFile, err)
	}
	if err := saveStats(sPath, stats); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save stats to %q: %v\n", sPath, err)
	}
}

func loadHistory(path string) ([]Message, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("invalid history file: %w", err)
	}
	return msgs, nil
}

func saveHistory(path string, messages []Message) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadSummary(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func saveSummary(path, content string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

// trimHistory splits history into messages to keep and messages to summarize.
// keep = last `limit` messages; excess = older messages beyond the limit.
// If limit <= 0 or len(history) <= limit, keep = history and excess is nil.
func trimHistory(history []Message, limit int) (keep, excess []Message) {
	if limit <= 0 || len(history) <= limit {
		return history, nil
	}
	splitAt := len(history) - limit
	excess = make([]Message, splitAt)
	copy(excess, history[:splitAt])
	keep = history[splitAt:]
	return keep, excess
}

// buildSummary calls the LLM to create or update a conversation summary.
// If existing is non-empty, it is incorporated into the new summary.
func buildSummary(trimmed []Message, existing, provider, apiKey, baseURL, model string, debug bool) (string, error) {
	var sb strings.Builder
	if existing != "" {
		sb.WriteString("Existing summary of earlier conversation:\n")
		sb.WriteString(existing)
		sb.WriteString("\n\nNew messages to incorporate into the summary:\n")
	} else {
		sb.WriteString("Messages to summarize:\n")
	}
	for _, m := range trimmed {
		fmt.Fprintf(&sb, "[%s]: %s\n\n", m.Role, m.Content)
	}

	systemMsg := "You are a helpful assistant that creates concise summaries of conversations. " +
		"Summarize the provided conversation messages into 1-3 paragraphs, focusing on key facts, " +
		"decisions, questions asked, and context useful for continuing the conversation. " +
		"If an existing summary is provided, incorporate the new messages to produce an updated summary. " +
		"Write in the third person as a neutral observer."

	msgs := []Message{
		{Role: roleSystem, Content: systemMsg},
		{Role: roleUser, Content: sb.String()},
	}

	resp, _, err := sendChatRequest(provider, apiKey, baseURL, model, msgs, 1000, nil, -1, debug)
	if err != nil {
		return "", fmt.Errorf("summary generation failed: %w", err)
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func sendChatRequest(provider, apiKey, baseURL, model string, messages []Message, maxTokens int, stop []string, temperature float64, debug bool) (*chatResponse, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	reqBody := chatRequest{
		Model:    model,
		Messages: messages,
	}

	if maxTokens > 0 {
		val := maxTokens
		reqBody.MaxCompletionTokens = &val
		reqBody.MaxTokens = &val
	}

	if len(stop) > 0 {
		reqBody.Stop = stop
	}

	if temperature >= 0 {
		t := temperature
		reqBody.Temperature = &t
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	if debug {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "", "  ") == nil {
			fmt.Fprintf(os.Stderr, "[debug] POST %s/chat/completions\n%s\n\n", baseURL, pretty.String())
		}
	}

	url := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	if provider == providerOpenRouter {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/farmut/ai-advent-challenge")
		httpReq.Header.Set("X-Title", "ai-adv-agent")
	}

	client := &http.Client{Timeout: 120 * time.Second}
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp errorPayload
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, 0, fmt.Errorf("%s API error (status %d): %s", provider, resp.StatusCode, errResp.Error.Message)
		}
		return nil, 0, fmt.Errorf("%s API error (status %d): %s", provider, resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if len(chatResp.Choices) == 0 {
		return nil, 0, fmt.Errorf("no response from %s", provider)
	}

	return &chatResp, elapsed, nil
}
