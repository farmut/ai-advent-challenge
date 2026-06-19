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
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Context strategy
// ---------------------------------------------------------------------------

type contextStrategy string

const (
	strategyNone          contextStrategy = ""
	strategySlidingWindow contextStrategy = "sliding-window"
	strategyStickyFacts   contextStrategy = "sticky-facts"
	strategyBranching     contextStrategy = "branching"
)

// ---------------------------------------------------------------------------
// HTTP / API types
// ---------------------------------------------------------------------------

type stringSlice []string

func (s *stringSlice) String() string        { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(value string) error { *s = append(*s, value); return nil }

type Message struct {
	Role    role   `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model               string    `json:"model"`
	Messages            []Message `json:"messages"`
	Stop                []string  `json:"stop,omitempty"`
	MaxTokens           *int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int      `json:"max_completion_tokens,omitempty"`
	Temperature         *float64  `json:"temperature,omitempty"`
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

// ---------------------------------------------------------------------------
// Session stats
// ---------------------------------------------------------------------------

type sessionStats struct {
	PromptTokens         int     `json:"prompt_tokens"`
	CompletionTokens     int     `json:"completion_tokens"`
	TotalTokens          int     `json:"total_tokens"`
	EstimatedCostUSD     float64 `json:"estimated_cost_usd"`
	Calls                int     `json:"calls"`
	LastCallPromptTokens int     `json:"last_call_prompt_tokens"`
}

// ---------------------------------------------------------------------------
// Model pricing
// ---------------------------------------------------------------------------

type modelPrice struct {
	InputPer1M    float64
	OutputPer1M   float64
	ContextWindow int
}

var knownPrices = map[string]modelPrice{
	"gpt-4o":               {2.50, 10.00, 128_000},
	"gpt-4o-mini":          {0.15, 0.60, 128_000},
	"gpt-4-turbo":          {10.00, 30.00, 128_000},
	"gpt-3.5-turbo":        {0.50, 1.50, 16_385},
	"o1":                   {15.00, 60.00, 200_000},
	"o1-mini":              {3.00, 12.00, 128_000},
	"openai/gpt-4o":        {2.50, 10.00, 128_000},
	"openai/gpt-4o-mini":   {0.15, 0.60, 128_000},
	"openai/gpt-4-turbo":   {10.00, 30.00, 128_000},
	"openai/gpt-3.5-turbo": {0.50, 1.50, 16_385},
	"openai/o1":            {15.00, 60.00, 200_000},
	"openai/o1-mini":       {3.00, 12.00, 128_000},

	"anthropic/claude-opus-4":     {15.00, 75.00, 200_000},
	"anthropic/claude-sonnet-4":   {3.00, 15.00, 200_000},
	"anthropic/claude-3-5-sonnet": {3.00, 15.00, 200_000},
	"anthropic/claude-3-haiku":    {0.25, 1.25, 200_000},
	"anthropic/claude-3-opus":     {15.00, 75.00, 200_000},

	"qwen/qwen3.5-9b":        {0.06, 0.24, 32_768},
	"qwen/qwen3.5-122b-a10b": {0.14, 0.56, 32_768},
	"qwen/qwen3.5-397b-a17b": {0.40, 1.60, 32_768},
}

var formatInstructions = map[string]string{
	"markdown": "Format your response using Markdown.",
	"json":     "Respond in JSON format.",
}

// ---------------------------------------------------------------------------
// Facts store (sticky-facts strategy)
// ---------------------------------------------------------------------------

type factsStore struct {
	Facts map[string]string `json:"facts"`
}

// ---------------------------------------------------------------------------
// Working memory — Layer 2 (task facts)
// ---------------------------------------------------------------------------

type workingMemory struct {
	Facts     map[string]string `json:"facts"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Long-term memory — Layer 3 (profile, decisions, knowledge)
// ---------------------------------------------------------------------------

type longTermMemory struct {
	Entries   map[string]string `json:"entries"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Branch state (branching strategy)
// ---------------------------------------------------------------------------

type branchCheckpoint struct {
	Messages  []Message `json:"messages"`
	CreatedAt string    `json:"created_at"`
}

type branchState struct {
	Current     string                      `json:"current"`
	Branches    []string                    `json:"branches"`
	Checkpoints map[string]branchCheckpoint `json:"checkpoints"`
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

func statsPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".stats" + ext
}

func summaryPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".summary.txt"
}

func factsFilePath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".facts.json"
}

func wmPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".wm" + ext
}

func ltmPath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".ltm" + ext
}

func branchStatePath(historyPath string) string {
	if historyPath == "" {
		return ""
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".branch-state.json"
}

// branchHistoryPath returns the history file for a named branch.
// "main" (or empty) resolves to the original history file.
func branchHistoryPath(historyPath, branchName string) string {
	if historyPath == "" {
		return ""
	}
	if branchName == "" || branchName == "main" {
		return historyPath
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".branch-" + branchName + ext
}

func currentBranchHistoryPath(historyPath string, bs branchState) string {
	return branchHistoryPath(historyPath, bs.Current)
}

// ---------------------------------------------------------------------------
// Token estimation
// ---------------------------------------------------------------------------

func estimateTokens(text string) int {
	n := len([]rune(text)) / 4
	if n < 1 {
		n = 1
	}
	return n
}

func estimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += estimateTokens(m.Content) + 4
	}
	return total
}

// ---------------------------------------------------------------------------
// Model pricing helpers
// ---------------------------------------------------------------------------

func modelPricing(model string) (modelPrice, bool) {
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

// ---------------------------------------------------------------------------
// Stats persistence
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Context status
// ---------------------------------------------------------------------------

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

	if known && price.ContextWindow > 0 {
		used := u.PromptTokens
		window := price.ContextWindow
		pct := float64(used) / float64(window) * 100
		remaining := window - used
		status := contextStatus(pct)

		growth := used - prevPromptTokens
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

// ---------------------------------------------------------------------------
// History persistence
// ---------------------------------------------------------------------------

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

// trimHistory splits history into keep (last `limit` messages) and excess (older messages).
// If limit <= 0 or len(history) <= limit, all messages are kept.
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

// slidingWindow returns the last n messages from history (no summary, just drop).
func slidingWindow(history []Message, n int) []Message {
	if n <= 0 || len(history) <= n {
		return history
	}
	return history[len(history)-n:]
}

// ---------------------------------------------------------------------------
// Summary persistence
// ---------------------------------------------------------------------------

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

	sysMsg := "You are a helpful assistant that creates concise summaries of conversations. " +
		"Summarize the provided conversation messages into 1-3 paragraphs, focusing on key facts, " +
		"decisions, questions asked, and context useful for continuing the conversation. " +
		"If an existing summary is provided, incorporate the new messages to produce an updated summary. " +
		"Write in the third person as a neutral observer."

	msgs := []Message{
		{Role: roleSystem, Content: sysMsg},
		{Role: roleUser, Content: sb.String()},
	}

	resp, _, err := sendChatRequest(provider, apiKey, baseURL, model, msgs, 1000, nil, -1, debug)
	if err != nil {
		return "", fmt.Errorf("summary generation failed: %w", err)
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// ---------------------------------------------------------------------------
// JSON helper
// ---------------------------------------------------------------------------

func stripJSONFences(s string) string {
	if after, found := strings.CutPrefix(s, "```json"); found {
		s = strings.TrimSpace(after)
	} else if after, found := strings.CutPrefix(s, "```"); found {
		s = strings.TrimSpace(after)
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// ---------------------------------------------------------------------------
// Facts store — sticky-facts strategy
// ---------------------------------------------------------------------------

func loadFacts(path string) (factsStore, error) {
	empty := factsStore{Facts: make(map[string]string)}
	if path == "" {
		return empty, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var fs factsStore
	if err := json.Unmarshal(data, &fs); err != nil {
		return empty, fmt.Errorf("invalid facts file: %w", err)
	}
	if fs.Facts == nil {
		fs.Facts = make(map[string]string)
	}
	return fs, nil
}

func saveFacts(path string, fs factsStore) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// factsSystemBlock formats the facts as a system-message section.
func factsSystemBlock(fs factsStore) string {
	if len(fs.Facts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fs.Facts))
	for k := range fs.Facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("[Sticky Facts]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, fs.Facts[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

// updateFacts calls the LLM to extract/update key-value facts from the latest exchange.
func updateFacts(existing factsStore, userMsg, assistantMsg, provider, apiKey, baseURL, model string, debug bool) (factsStore, error) {
	var sb strings.Builder
	if len(existing.Facts) > 0 {
		b, _ := json.Marshal(existing.Facts)
		sb.WriteString("Current facts (JSON):\n")
		sb.Write(b)
		sb.WriteString("\n\n")
	}
	sb.WriteString("New exchange:\nUser: ")
	sb.WriteString(userMsg)
	sb.WriteString("\n\nAssistant: ")
	sb.WriteString(assistantMsg)
	sb.WriteString("\n\nUpdate the facts JSON with any important new information (goals, constraints, preferences, decisions, agreements). " +
		"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store of important persistent facts from a conversation. " +
		"Return ONLY valid JSON with string keys and string values. Include only genuinely important facts."

	msgs := []Message{
		{Role: roleSystem, Content: sysMsg},
		{Role: roleUser, Content: sb.String()},
	}

	resp, _, err := sendChatRequest(provider, apiKey, baseURL, model, msgs, 500, nil, -1, debug)
	if err != nil {
		return existing, fmt.Errorf("facts extraction failed: %w", err)
	}

	content := stripJSONFences(strings.TrimSpace(resp.Choices[0].Message.Content))
	var updated map[string]string
	if err := json.Unmarshal([]byte(content), &updated); err != nil {
		return existing, fmt.Errorf("failed to parse facts JSON: %w (response: %s)", err, content)
	}
	return factsStore{Facts: updated}, nil
}

// ---------------------------------------------------------------------------
// Working memory — Layer 2 (task facts)
// ---------------------------------------------------------------------------

func loadWM(path string) (workingMemory, error) {
	empty := workingMemory{Facts: make(map[string]string)}
	if path == "" {
		return empty, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var wm workingMemory
	if err := json.Unmarshal(data, &wm); err != nil {
		return empty, fmt.Errorf("invalid working memory file: %w", err)
	}
	if wm.Facts == nil {
		wm.Facts = make(map[string]string)
	}
	return wm, nil
}

func saveWM(path string, wm workingMemory) error {
	if path == "" {
		return nil
	}
	wm.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(wm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func wmSystemBlock(wm workingMemory) string {
	if len(wm.Facts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(wm.Facts))
	for k := range wm.Facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("[Working Memory — Task Facts]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, wm.Facts[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

func updateWM(existing workingMemory, userMsg, assistantMsg, provider, apiKey, baseURL, model string, debug bool) (workingMemory, error) {
	var sb strings.Builder
	if len(existing.Facts) > 0 {
		b, _ := json.Marshal(existing.Facts)
		sb.WriteString("Current task facts (JSON):\n")
		sb.Write(b)
		sb.WriteString("\n\n")
	}
	sb.WriteString("New exchange:\nUser: ")
	sb.WriteString(userMsg)
	sb.WriteString("\n\nAssistant: ")
	sb.WriteString(assistantMsg)
	sb.WriteString("\n\nUpdate the task facts with information about the current task: " +
		"active goals, current requirements, constraints, intermediate results, short-term decisions. " +
		"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store for an AI agent's working memory (Layer 2: task facts). " +
		"Focus on task-specific data: active goals, current state, constraints, decisions made this session. " +
		"Return ONLY valid JSON with string keys and string values."

	msgs := []Message{
		{Role: roleSystem, Content: sysMsg},
		{Role: roleUser, Content: sb.String()},
	}

	resp, _, err := sendChatRequest(provider, apiKey, baseURL, model, msgs, 500, nil, -1, debug)
	if err != nil {
		return existing, fmt.Errorf("WM update failed: %w", err)
	}
	content := stripJSONFences(strings.TrimSpace(resp.Choices[0].Message.Content))
	var updated map[string]string
	if err := json.Unmarshal([]byte(content), &updated); err != nil {
		return existing, fmt.Errorf("failed to parse WM JSON: %w (response: %s)", err, content)
	}
	return workingMemory{Facts: updated}, nil
}

// ---------------------------------------------------------------------------
// Long-term memory — Layer 3 (profile, decisions, knowledge)
// ---------------------------------------------------------------------------

func loadLTM(path string) (longTermMemory, error) {
	empty := longTermMemory{Entries: make(map[string]string)}
	if path == "" {
		return empty, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var ltm longTermMemory
	if err := json.Unmarshal(data, &ltm); err != nil {
		return empty, fmt.Errorf("invalid long-term memory file: %w", err)
	}
	if ltm.Entries == nil {
		ltm.Entries = make(map[string]string)
	}
	return ltm, nil
}

func saveLTM(path string, ltm longTermMemory) error {
	if path == "" {
		return nil
	}
	ltm.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(ltm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ltmSystemBlock(ltm longTermMemory) string {
	if len(ltm.Entries) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ltm.Entries))
	for k := range ltm.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("[Long-term Memory — Profile & Knowledge]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, ltm.Entries[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

func updateLTM(existing longTermMemory, userMsg, assistantMsg, provider, apiKey, baseURL, model string, debug bool) (longTermMemory, error) {
	var sb strings.Builder
	if len(existing.Entries) > 0 {
		b, _ := json.Marshal(existing.Entries)
		sb.WriteString("Current long-term memory (JSON):\n")
		sb.Write(b)
		sb.WriteString("\n\n")
	}
	sb.WriteString("New exchange:\nUser: ")
	sb.WriteString(userMsg)
	sb.WriteString("\n\nAssistant: ")
	sb.WriteString(assistantMsg)
	sb.WriteString("\n\nUpdate the long-term memory with information relevant across sessions: " +
		"user profile, stable preferences, long-term goals, important strategic decisions with rationale, " +
		"accumulated domain knowledge. Ignore transient task state. " +
		"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store for an AI agent's long-term memory (Layer 3: profile/knowledge). " +
		"Focus on: user profile, stable preferences, long-term goals, strategic decisions, " +
		"accumulated domain knowledge. Ignore ephemeral task state. " +
		"Return ONLY valid JSON with string keys and string values."

	msgs := []Message{
		{Role: roleSystem, Content: sysMsg},
		{Role: roleUser, Content: sb.String()},
	}

	resp, _, err := sendChatRequest(provider, apiKey, baseURL, model, msgs, 500, nil, -1, debug)
	if err != nil {
		return existing, fmt.Errorf("LTM update failed: %w", err)
	}
	content := stripJSONFences(strings.TrimSpace(resp.Choices[0].Message.Content))
	var updated map[string]string
	if err := json.Unmarshal([]byte(content), &updated); err != nil {
		return existing, fmt.Errorf("failed to parse LTM JSON: %w (response: %s)", err, content)
	}
	return longTermMemory{Entries: updated}, nil
}

// ---------------------------------------------------------------------------
// Branch state — branching strategy
// ---------------------------------------------------------------------------

func defaultBranchState() branchState {
	return branchState{
		Current:     "main",
		Branches:    []string{"main"},
		Checkpoints: make(map[string]branchCheckpoint),
	}
}

func loadBranchState(path string) (branchState, error) {
	if path == "" {
		return defaultBranchState(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultBranchState(), nil
	}
	if err != nil {
		return defaultBranchState(), err
	}
	var bs branchState
	if err := json.Unmarshal(data, &bs); err != nil {
		return defaultBranchState(), fmt.Errorf("invalid branch state: %w", err)
	}
	if bs.Checkpoints == nil {
		bs.Checkpoints = make(map[string]branchCheckpoint)
	}
	if bs.Current == "" {
		bs.Current = "main"
	}
	return bs, nil
}

func saveBranchState(path string, bs branchState) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func printBranches(bs branchState) {
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
// HTTP client
// ---------------------------------------------------------------------------

func sendChatRequest(provider, apiKey, baseURL, model string, messages []Message, maxTokens int, stop []string, temperature float64, debug bool) (*chatResponse, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	reqBody := chatRequest{Model: model, Messages: messages}

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

	// Day10 flags
	summaryEnabled := flag.Bool("summary", false, "Enable auto-summarization when history exceeds --history-limit (default: disabled)")
	strategyFlag := flag.String("strategy", "", "Context strategy: sliding-window | sticky-facts | branching (default: none)")
	windowSize := flag.Int("window-size", 5, "Recent messages to include in prompt (sliding-window / sticky-facts)")

	// Branching management flags (require --strategy branching)
	checkpointName := flag.String("checkpoint", "", "[branching] Save current history as a named checkpoint then exit")
	branchNew := flag.String("branch-new", "", "[branching] Create and switch to a new branch then exit")
	fromCheckpoint := flag.String("from-checkpoint", "", "[branching] Source checkpoint for --branch-new")
	branchSwitch := flag.String("branch-switch", "", "[branching] Switch to an existing branch then exit")
	branchList := flag.Bool("branch-list", false, "[branching] List all branches and checkpoints then exit")

	// Day11: 3-layer memory flags
	memoryWMFile := flag.String("memory-wm", "", "Working memory file (Layer 2: task facts). Default: derived from --history")
	memoryLTMFile := flag.String("memory-ltm", "", "Long-term memory file (Layer 3: profile/knowledge). Default: derived from --history")
	memoryUpdate := flag.Bool("memory-update", false, "Auto-update Layer 2 (WM) and Layer 3 (LTM) after each call via LLM extraction")

	var stopSequences stringSlice
	flag.Var(&stopSequences, "stop", "Stop sequence (repeatable)")
	flag.Parse()

	// Validate strategy
	strat := contextStrategy(*strategyFlag)
	switch strat {
	case strategyNone, strategySlidingWindow, strategyStickyFacts, strategyBranching:
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
	if strat == strategyBranching {
		if *historyFile == "" {
			if *branchList || *checkpointName != "" || *branchNew != "" || *branchSwitch != "" {
				fmt.Fprintf(os.Stderr, "Error: branching requires --history to be non-empty\n")
				os.Exit(1)
			}
		} else {
			bsPath := branchStatePath(*historyFile)

			if *branchList {
				bs, err := loadBranchState(bsPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to load branch state: %v\n", err)
				}
				printBranches(bs)
				return
			}

			if *checkpointName != "" {
				bs, _ := loadBranchState(bsPath)
				histPath := currentBranchHistoryPath(*historyFile, bs)
				history, err := loadHistory(histPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error loading history for checkpoint: %v\n", err)
					os.Exit(1)
				}
				bs.Checkpoints[*checkpointName] = branchCheckpoint{
					Messages:  history,
					CreatedAt: time.Now().Format(time.RFC3339),
				}
				if err := saveBranchState(bsPath, bs); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving branch state: %v\n", err)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "[branch] checkpoint %q saved (%d messages)\n", *checkpointName, len(history))
				return
			}

			if *branchNew != "" {
				bs, _ := loadBranchState(bsPath)
				var srcMessages []Message
				if *fromCheckpoint != "" {
					cp, ok := bs.Checkpoints[*fromCheckpoint]
					if !ok {
						fmt.Fprintf(os.Stderr, "Error: checkpoint %q not found. Use --branch-list to see checkpoints.\n", *fromCheckpoint)
						os.Exit(1)
					}
					srcMessages = cp.Messages
				} else {
					srcMessages, _ = loadHistory(currentBranchHistoryPath(*historyFile, bs))
				}
				newHistPath := branchHistoryPath(*historyFile, *branchNew)
				if err := saveHistory(newHistPath, srcMessages); err != nil {
					fmt.Fprintf(os.Stderr, "Error creating branch history: %v\n", err)
					os.Exit(1)
				}
				exists := false
				for _, b := range bs.Branches {
					if b == *branchNew {
						exists = true
						break
					}
				}
				if !exists {
					bs.Branches = append(bs.Branches, *branchNew)
				}
				bs.Current = *branchNew
				if err := saveBranchState(bsPath, bs); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving branch state: %v\n", err)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "[branch] created and switched to branch %q (%d messages)\n", *branchNew, len(srcMessages))
				return
			}

			if *branchSwitch != "" {
				bs, _ := loadBranchState(bsPath)
				found := false
				for _, b := range bs.Branches {
					if b == *branchSwitch {
						found = true
						break
					}
				}
				if !found {
					fmt.Fprintf(os.Stderr, "Error: branch %q not found. Use --branch-list to see branches.\n", *branchSwitch)
					os.Exit(1)
				}
				bs.Current = *branchSwitch
				if err := saveBranchState(bsPath, bs); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving branch state: %v\n", err)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "[branch] switched to branch %q\n", *branchSwitch)
				return
			}
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
	providerStr := os.Getenv("LLM_PROVIDER")
	if providerStr == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}
	if providerStr != providerOpenAI && providerStr != providerOpenRouter {
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER %q (supported: openai, openrouter)\n", providerStr)
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

	// ----------------------------------------------------------------
	// Resolve actual history file (branch-aware)
	// ----------------------------------------------------------------
	actualHistoryFile := *historyFile
	if strat == strategyBranching && *historyFile != "" {
		bsPath := branchStatePath(*historyFile)
		bs, _ := loadBranchState(bsPath)
		actualHistoryFile = currentBranchHistoryPath(*historyFile, bs)
		fmt.Fprintf(os.Stderr, "[branch] active branch: %q → %s\n", bs.Current, actualHistoryFile)
	}

	// ----------------------------------------------------------------
	// Resolve memory layer paths (Layer 2 and Layer 3)
	// ----------------------------------------------------------------
	resolvedWMFile := *memoryWMFile
	if resolvedWMFile == "" && actualHistoryFile != "" {
		resolvedWMFile = wmPath(actualHistoryFile)
	}
	resolvedLTMFile := *memoryLTMFile
	if resolvedLTMFile == "" && actualHistoryFile != "" {
		resolvedLTMFile = ltmPath(actualHistoryFile)
	}

	// ----------------------------------------------------------------
	// Load history (Layer 1 — STM) and stats
	// ----------------------------------------------------------------
	history, err := loadHistory(actualHistoryFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load history from %q: %v\n", actualHistoryFile, err)
	}

	sPath := statsPath(actualHistoryFile)
	stats, err := loadStats(sPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load stats from %q: %v\n", sPath, err)
	}

	// ----------------------------------------------------------------
	// Load memory layers
	// ----------------------------------------------------------------
	wm, err := loadWM(resolvedWMFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load working memory from %q: %v\n", resolvedWMFile, err)
	}

	ltm, err := loadLTM(resolvedLTMFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load long-term memory from %q: %v\n", resolvedLTMFile, err)
	}

	// ----------------------------------------------------------------
	// Build system content
	// ----------------------------------------------------------------
	systemContent := *system

	// Prepend sticky-facts block (sticky-facts strategy)
	if strat == strategyStickyFacts {
		fp := factsFilePath(actualHistoryFile)
		fs, ferr := loadFacts(fp)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load facts from %q: %v\n", fp, ferr)
		}
		if block := factsSystemBlock(fs); block != "" {
			if systemContent != "" {
				systemContent = block + "\n\n" + systemContent
			} else {
				systemContent = block
			}
		}
	}

	// Prepend Layer 2: Working Memory (task facts)
	if block := wmSystemBlock(wm); block != "" {
		if systemContent != "" {
			systemContent = block + "\n\n" + systemContent
		} else {
			systemContent = block
		}
	}

	// Prepend Layer 3: Long-term Memory (profile/knowledge)
	if block := ltmSystemBlock(ltm); block != "" {
		if systemContent != "" {
			systemContent = block + "\n\n" + systemContent
		} else {
			systemContent = block
		}
	}

	// Append summary (only when --summary enabled and strategy is none/branching)
	var existingSummary string
	if *summaryEnabled && (strat == strategyNone || strat == strategyBranching) {
		sumFilePath := summaryPath(actualHistoryFile)
		existingSummary, err = loadSummary(sumFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load summary from %q: %v\n", sumFilePath, err)
		}
		if existingSummary != "" {
			if systemContent != "" {
				systemContent += "\n\n---\nSummary of earlier conversation:\n" + existingSummary
			} else {
				systemContent = "Summary of earlier conversation:\n" + existingSummary
			}
		}
	}

	// ----------------------------------------------------------------
	// Select history slice for the prompt
	// ----------------------------------------------------------------
	historyForPrompt := history
	if strat == strategySlidingWindow || strat == strategyStickyFacts {
		historyForPrompt = slidingWindow(history, *windowSize)
	}

	// ----------------------------------------------------------------
	// Build messages array
	// ----------------------------------------------------------------
	var messages []Message
	if systemContent != "" {
		messages = append(messages, Message{Role: roleSystem, Content: systemContent})
	}
	messages = append(messages, historyForPrompt...)
	messages = append(messages, Message{Role: roleUser, Content: fullQuery})

	// Pre-call context estimation
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

	// ----------------------------------------------------------------
	// Send request
	// ----------------------------------------------------------------
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

	// Update session stats
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

	// ----------------------------------------------------------------
	// Post-call: update memory layers (Layer 2 and Layer 3)
	// ----------------------------------------------------------------
	if *memoryUpdate {
		if resolvedWMFile != "" {
			fmt.Fprintf(os.Stderr, "[memory-wm] updating working memory...\n")
			updatedWM, wmErr := updateWM(wm, fullQuery, assistantContent, providerStr, apiKey, baseURL, model, *debug)
			if wmErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update working memory: %v\n", wmErr)
			} else {
				if serr := saveWM(resolvedWMFile, updatedWM); serr != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to save working memory: %v\n", serr)
				} else {
					fmt.Fprintf(os.Stderr, "[memory-wm] %d facts saved to %q\n", len(updatedWM.Facts), resolvedWMFile)
				}
			}
		}
		if resolvedLTMFile != "" {
			fmt.Fprintf(os.Stderr, "[memory-ltm] updating long-term memory...\n")
			updatedLTM, ltmErr := updateLTM(ltm, fullQuery, assistantContent, providerStr, apiKey, baseURL, model, *debug)
			if ltmErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update long-term memory: %v\n", ltmErr)
			} else {
				if serr := saveLTM(resolvedLTMFile, updatedLTM); serr != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to save long-term memory: %v\n", serr)
				} else {
					fmt.Fprintf(os.Stderr, "[memory-ltm] %d entries saved to %q\n", len(updatedLTM.Entries), resolvedLTMFile)
				}
			}
		}
	}

	// ----------------------------------------------------------------
	// Post-call: persist history (Layer 1 — STM) by strategy
	// ----------------------------------------------------------------
	newHistory := append(history, Message{Role: roleUser, Content: fullQuery})
	newHistory = append(newHistory, Message{Role: roleAssistant, Content: assistantContent})

	switch strat {

	case strategySlidingWindow:
		keep := slidingWindow(newHistory, *windowSize)
		if err := saveHistory(actualHistoryFile, keep); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	case strategyStickyFacts:
		fp := factsFilePath(actualHistoryFile)
		existingFacts, _ := loadFacts(fp)
		fmt.Fprintf(os.Stderr, "[facts] updating facts store...\n")
		updatedFacts, ferr := updateFacts(existingFacts, fullQuery, assistantContent, providerStr, apiKey, baseURL, model, *debug)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update facts: %v\n", ferr)
		} else {
			if serr := saveFacts(fp, updatedFacts); serr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save facts: %v\n", serr)
			} else {
				fmt.Fprintf(os.Stderr, "[facts] %d facts saved to %q\n", len(updatedFacts.Facts), fp)
				if *debug {
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
		}
		keep := slidingWindow(newHistory, *windowSize)
		if err := saveHistory(actualHistoryFile, keep); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
		}

	default: // strategyNone or strategyBranching
		keep, excess := trimHistory(newHistory, *historyLimit)
		if *summaryEnabled && len(excess) > 0 {
			fmt.Fprintf(os.Stderr, "[summary] %d messages exceed limit — generating summary...\n", len(excess))
			sumFilePath := summaryPath(actualHistoryFile)
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
		if err := saveHistory(actualHistoryFile, keep); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save history to %q: %v\n", actualHistoryFile, err)
		}
	}

	if err := saveStats(sPath, stats); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save stats to %q: %v\n", sPath, err)
	}
}
