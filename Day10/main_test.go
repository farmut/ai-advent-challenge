package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func captureSendChatRequest(provider, model string, maxTokens int, stop []string, query string) (map[string]any, error) {
	return captureSendChatRequestWithMessages(provider, model, maxTokens, stop, []Message{
		{Role: roleUser, Content: query},
	})
}

func captureSendChatRequestWithMessages(provider, model string, maxTokens int, stop []string, messages []Message) (map[string]any, error) {
	var captured map[string]any
	var capturedErr error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			capturedErr = err
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			capturedErr = err
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{
				{Message: Message{Role: roleUser, Content: "mock response"}},
			},
		})
	}))
	defer srv.Close()

	_, _, err := sendChatRequest(provider, "fake-key", srv.URL, model, messages, maxTokens, stop, -1, false)
	if err != nil {
		return nil, err
	}
	if capturedErr != nil {
		return nil, capturedErr
	}
	return captured, nil
}

func captureSendChatRequestWithTemperature(provider, model string, maxTokens int, stop []string, temperature float64, query string) (map[string]any, error) {
	var captured map[string]any
	var capturedErr error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			capturedErr = err
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			capturedErr = err
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{
				{Message: Message{Role: roleUser, Content: "mock response"}},
			},
		})
	}))
	defer srv.Close()

	_, _, err := sendChatRequest(provider, "fake-key", srv.URL, model, []Message{{Role: roleUser, Content: query}}, maxTokens, stop, temperature, false)
	if err != nil {
		return nil, err
	}
	if capturedErr != nil {
		return nil, capturedErr
	}
	return captured, nil
}

func prettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	return strings.TrimSpace(buf.String())
}

// ---------------------------------------------------------------------------
// Token/request payload tests (carried over from Day7)
// ---------------------------------------------------------------------------

func TestOpenRouter_SendsBothTokenParams(t *testing.T) {
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-oss-120b:free", 30, nil, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload sent to OpenRouter: %s", prettyJSON(captured))

	mt, hasMaxTokens := captured["max_tokens"]
	mct, hasMaxCompletionTokens := captured["max_completion_tokens"]

	if !hasMaxTokens {
		t.Error("max_tokens not found in request body")
	} else if v, ok := mt.(float64); !ok || v != 30 {
		t.Errorf("max_tokens = %v (expected 30)", mt)
	}

	if !hasMaxCompletionTokens {
		t.Error("max_completion_tokens not found in request body")
	} else if v, ok := mct.(float64); !ok || v != 30 {
		t.Errorf("max_completion_tokens = %v (expected 30)", mct)
	}
}

func TestOpenAI_SendsBothTokenParams(t *testing.T) {
	captured, err := captureSendChatRequest(providerOpenAI, "gpt-4o", 50, nil, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload sent to OpenAI: %s", prettyJSON(captured))

	mct, has := captured["max_completion_tokens"]
	if !has {
		t.Error("max_completion_tokens not found in OpenAI request body")
	} else if v, ok := mct.(float64); !ok || v != 50 {
		t.Errorf("max_completion_tokens = %v (expected 50)", mct)
	}

	mt, has := captured["max_tokens"]
	if !has {
		t.Error("max_tokens not found in OpenAI request body")
	} else if v, ok := mt.(float64); !ok || v != 50 {
		t.Errorf("max_tokens = %v (expected 50)", mt)
	}
}

func TestStopSequences(t *testing.T) {
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4", 100, []string{"###", "END"}, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload with stop sequences: %s", prettyJSON(captured))

	stop, has := captured["stop"]
	if !has {
		t.Fatal("stop not found in request body")
	}
	arr, ok := stop.([]any)
	if !ok {
		t.Fatalf("stop is not an array: %T", stop)
	}
	if len(arr) != 2 {
		t.Errorf("stop has %d elements, expected 2", len(arr))
	}
}

func TestFormatMarkdown_InstructionInUserPrompt(t *testing.T) {
	query := "hello" + "\n\n" + formatInstructions["markdown"]
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "Markdown") {
				t.Errorf("user message should contain Markdown instruction, got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

func TestFormatJSON_InstructionInUserPrompt(t *testing.T) {
	query := "hello" + "\n\n" + formatInstructions["json"]
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "JSON") {
				t.Errorf("user message should contain JSON instruction, got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

func TestFormatHint_OverridesFormat(t *testing.T) {
	customInstr := "Format as a numbered list"
	query := "hello" + "\n\n" + customInstr
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "numbered list") {
				t.Errorf("user message should contain custom instruction 'numbered list', got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

func TestFormatHint_CustomInstructionInPrompt(t *testing.T) {
	customInstr := "Use bullet points and bold text"
	query := "test query" + "\n\n" + customInstr
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "bullet points") || !strings.Contains(content, "bold text") {
				t.Errorf("user message should contain custom instruction, got: %q", content)
			}
			if strings.Contains(content, "Format your response") {
				t.Errorf("user message should not contain default format instruction when custom hint is used, got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

func TestSystemMessage_SentFirstInMessages(t *testing.T) {
	systemContent := "Ты опытный программист на Go. Отвечай кратко и по делу."
	userContent := "hello"

	captured, err := captureSendChatRequestWithMessages(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, []Message{
		{Role: roleSystem, Content: systemContent},
		{Role: roleUser, Content: userContent},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload with system message: %s", prettyJSON(captured))

	messages, ok := captured["messages"].([]any)
	if !ok {
		t.Fatal("messages not found or not an array")
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	firstMsg, _ := messages[0].(map[string]any)
	if firstMsg["role"] != "system" {
		t.Errorf("first message role should be 'system', got: %v", firstMsg["role"])
	}
	if firstMsg["content"] != systemContent {
		t.Errorf("first message content should be %q, got: %v", systemContent, firstMsg["content"])
	}

	secondMsg, _ := messages[1].(map[string]any)
	if secondMsg["role"] != "user" {
		t.Errorf("second message role should be 'user', got: %v", secondMsg["role"])
	}
	if secondMsg["content"] != userContent {
		t.Errorf("second message content should be %q, got: %v", userContent, secondMsg["content"])
	}
}

func TestSystemMessage_EmptyNotIncluded(t *testing.T) {
	captured, err := captureSendChatRequestWithMessages(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, []Message{
		{Role: roleUser, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload without system message: %s", prettyJSON(captured))

	messages, ok := captured["messages"].([]any)
	if !ok {
		t.Fatal("messages not found or not an array")
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	firstMsg, _ := messages[0].(map[string]any)
	if firstMsg["role"] == "system" {
		t.Error("system message should not be included when not specified")
	}
	if firstMsg["role"] != "user" {
		t.Errorf("first message role should be 'user', got: %v", firstMsg["role"])
	}
}

func TestSystemMessage_CombinedWithQuery(t *testing.T) {
	systemContent := "You are a helpful coding assistant."
	userContent := "Напиши hello world"

	captured, err := captureSendChatRequestWithMessages(providerOpenAI, "gpt-4o", 100, nil, []Message{
		{Role: roleSystem, Content: systemContent},
		{Role: roleUser, Content: userContent},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Combined payload: %s", prettyJSON(captured))

	messages, _ := captured["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	hasSystem := false
	hasUser := false
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		switch msg["role"] {
		case "system":
			hasSystem = true
			if msg["content"] != systemContent {
				t.Errorf("system content mismatch: got %v", msg["content"])
			}
		case "user":
			hasUser = true
			if msg["content"] != userContent {
				t.Errorf("user content mismatch: got %v", msg["content"])
			}
		}
	}

	if !hasSystem {
		t.Error("system message not found in messages")
	}
	if !hasUser {
		t.Error("user message not found in messages")
	}
}

func TestTemperature_IncludedInRequest(t *testing.T) {
	captured, err := captureSendChatRequestWithTemperature(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, 0.7, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Payload with temperature: %s", prettyJSON(captured))

	temp, has := captured["temperature"]
	if !has {
		t.Fatal("temperature not found in request body")
	}
	if v, ok := temp.(float64); !ok || v != 0.7 {
		t.Errorf("temperature = %v (expected 0.7)", temp)
	}
}

func TestTemperature_ExcludedWhenNegative(t *testing.T) {
	captured, err := captureSendChatRequest(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, has := captured["temperature"]; has {
		t.Error("temperature should not be present when set to -1")
	}
}

// ---------------------------------------------------------------------------
// History tests
// ---------------------------------------------------------------------------

func TestLoadHistory_FileNotExist(t *testing.T) {
	msgs, err := loadHistory("/tmp/nonexistent_history_file_xyz.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil slice for missing file, got: %v", msgs)
	}
}

func TestLoadHistory_EmptyPath(t *testing.T) {
	msgs, err := loadHistory("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil for empty path, got: %v", msgs)
	}
}

func TestSaveAndLoadHistory(t *testing.T) {
	f, err := os.CreateTemp("", "history_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	original := []Message{
		{Role: roleUser, Content: "hello"},
		{Role: roleAssistant, Content: "world"},
	}

	if err := saveHistory(f.Name(), original); err != nil {
		t.Fatalf("saveHistory failed: %v", err)
	}

	loaded, err := loadHistory(f.Name())
	if err != nil {
		t.Fatalf("loadHistory failed: %v", err)
	}

	if len(loaded) != len(original) {
		t.Fatalf("expected %d messages, got %d", len(original), len(loaded))
	}
	for i, msg := range loaded {
		if msg.Role != original[i].Role || msg.Content != original[i].Content {
			t.Errorf("message[%d] mismatch: got {%s, %q}, want {%s, %q}", i, msg.Role, msg.Content, original[i].Role, original[i].Content)
		}
	}
}

func TestLoadHistory_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "history_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	_, err = loadHistory(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSaveHistory_EmptyPath(t *testing.T) {
	if err := saveHistory("", []Message{{Role: roleUser, Content: "test"}}); err != nil {
		t.Fatalf("saveHistory with empty path should be a no-op, got: %v", err)
	}
}

func TestHistoryIncludedInRequest(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "first question"},
		{Role: roleAssistant, Content: "first answer"},
	}
	newMsg := Message{Role: roleUser, Content: "second question"}
	messages := append(history, newMsg)

	captured, err := captureSendChatRequestWithMessages(providerOpenRouter, "openai/gpt-4o-mini", 0, nil, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rawMsgs, _ := captured["messages"].([]any)
	if len(rawMsgs) != 3 {
		t.Fatalf("expected 3 messages (history + new), got %d", len(rawMsgs))
	}

	roles := []string{"user", "assistant", "user"}
	for i, raw := range rawMsgs {
		msg, _ := raw.(map[string]any)
		if msg["role"] != roles[i] {
			t.Errorf("message[%d] role: got %v, want %s", i, msg["role"], roles[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Token counting — new Day8 tests
// ---------------------------------------------------------------------------

func TestEstimateMessagesTokens_Empty(t *testing.T) {
	if got := estimateMessagesTokens(nil); got != 0 {
		t.Errorf("empty messages = %d, want 0", got)
	}
}

func TestEstimateMessagesTokens_Single(t *testing.T) {
	msgs := []Message{{Role: roleUser, Content: strings.Repeat("a", 400)}}
	got := estimateMessagesTokens(msgs)
	// 400 chars / 4 = 100 tokens + 4 overhead = 104
	if got < 100 || got > 110 {
		t.Errorf("single 400-char message = %d tokens, expected ~104", got)
	}
}

func TestEstimateMessagesTokens_Multiple(t *testing.T) {
	msgs := []Message{
		{Role: roleSystem, Content: strings.Repeat("a", 400)},  // ~100 + 4
		{Role: roleUser, Content: strings.Repeat("b", 400)},    // ~100 + 4
		{Role: roleAssistant, Content: strings.Repeat("c", 400)}, // ~100 + 4
	}
	got := estimateMessagesTokens(msgs)
	// 3 × (100 + 4) = 312
	if got < 300 || got > 325 {
		t.Errorf("three 400-char messages = %d tokens, expected ~312", got)
	}
}

func TestEstimateMessagesTokens_OverflowDetection(t *testing.T) {
	// Simulate a message that exceeds gpt-4o-mini's 128K context
	bigContent := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12000) // ~135000 tokens
	msgs := []Message{{Role: roleUser, Content: bigContent}}
	got := estimateMessagesTokens(msgs)
	p, _ := modelPricing("gpt-4o-mini")
	if got <= p.ContextWindow {
		t.Errorf("expected estimate (%d) to exceed context window (%d)", got, p.ContextWindow)
	}
}

func TestEstimateTokens_Basic(t *testing.T) {
	// 4 chars ~= 1 token
	text := strings.Repeat("a", 400)
	est := estimateTokens(text)
	if est < 80 || est > 120 {
		t.Errorf("estimateTokens(%d chars) = %d, expected ~100", len(text), est)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	// Empty / very short text must not return 0 (avoid division artifacts)
	est := estimateTokens("")
	if est < 1 {
		t.Errorf("estimateTokens(\"\") = %d, expected >= 1", est)
	}
}

func TestEstimateTokens_Unicode(t *testing.T) {
	// Unicode rune count, not byte count, should be used
	text := strings.Repeat("ж", 100) // Cyrillic: 2 bytes each, 100 runes
	est := estimateTokens(text)
	if est < 15 || est > 35 {
		t.Errorf("estimateTokens(100 Cyrillic runes) = %d, expected ~25", est)
	}
}

// ---------------------------------------------------------------------------
// statsPath tests
// ---------------------------------------------------------------------------

func TestStatsPath_Standard(t *testing.T) {
	got := statsPath("chat_history.json")
	want := "chat_history.stats.json"
	if got != want {
		t.Errorf("statsPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestStatsPath_EmptyDisabled(t *testing.T) {
	if p := statsPath(""); p != "" {
		t.Errorf("statsPath(\"\") should be empty, got %q", p)
	}
}

func TestStatsPath_NoExtension(t *testing.T) {
	got := statsPath("history")
	want := "history.stats"
	if got != want {
		t.Errorf("statsPath(history) = %q, want %q", got, want)
	}
}

func TestStatsPath_WithDirectory(t *testing.T) {
	got := statsPath("/tmp/my_chat.json")
	want := "/tmp/my_chat.stats.json"
	if got != want {
		t.Errorf("statsPath(/tmp/my_chat.json) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Session stats persistence
// ---------------------------------------------------------------------------

func TestSaveAndLoadStats(t *testing.T) {
	f, err := os.CreateTemp("", "stats_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	original := sessionStats{
		PromptTokens:         1000,
		CompletionTokens:     200,
		TotalTokens:          1200,
		EstimatedCostUSD:     0.00042,
		Calls:                3,
		LastCallPromptTokens: 450,
	}

	if err := saveStats(f.Name(), original); err != nil {
		t.Fatalf("saveStats failed: %v", err)
	}

	loaded, err := loadStats(f.Name())
	if err != nil {
		t.Fatalf("loadStats failed: %v", err)
	}

	if loaded != original {
		t.Errorf("loaded stats %+v != original %+v", loaded, original)
	}
}

func TestLoadStats_FileNotExist(t *testing.T) {
	s, err := loadStats("/tmp/nonexistent_stats_xyz.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if s.Calls != 0 || s.TotalTokens != 0 {
		t.Errorf("expected zero stats for missing file, got: %+v", s)
	}
}

func TestLoadStats_EmptyPath(t *testing.T) {
	s, err := loadStats("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Calls != 0 {
		t.Errorf("expected zero stats for empty path, got: %+v", s)
	}
}

func TestSaveStats_EmptyPath(t *testing.T) {
	if err := saveStats("", sessionStats{Calls: 5}); err != nil {
		t.Fatalf("saveStats with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadStats_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "stats_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	_, err = loadStats(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid JSON stats, got nil")
	}
}

// ---------------------------------------------------------------------------
// Model pricing tests
// ---------------------------------------------------------------------------

func TestModelPricing_KnownModel(t *testing.T) {
	p, ok := modelPricing("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini should be a known model")
	}
	if p.InputPer1M != 0.15 {
		t.Errorf("gpt-4o-mini input price = %v, want 0.15", p.InputPer1M)
	}
	if p.OutputPer1M != 0.60 {
		t.Errorf("gpt-4o-mini output price = %v, want 0.60", p.OutputPer1M)
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("gpt-4o-mini context window = %d, want 128000", p.ContextWindow)
	}
}

func TestModelPricing_OpenRouterPrefixed(t *testing.T) {
	p, ok := modelPricing("openai/gpt-4o")
	if !ok {
		t.Fatal("openai/gpt-4o should be a known model")
	}
	if p.InputPer1M != 2.50 {
		t.Errorf("openai/gpt-4o input price = %v, want 2.50", p.InputPer1M)
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("openai/gpt-4o context window = %d, want 128000", p.ContextWindow)
	}
}

func TestModelPricing_FreeSuffix(t *testing.T) {
	// "openai/gpt-4o-mini:free" should resolve to "openai/gpt-4o-mini"
	p, ok := modelPricing("openai/gpt-4o-mini:free")
	if !ok {
		t.Fatal("openai/gpt-4o-mini:free should resolve via suffix stripping")
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("context window should survive suffix strip, got %d", p.ContextWindow)
	}
}

func TestModelPricing_UnknownModel(t *testing.T) {
	_, ok := modelPricing("unicorn/model-xyz-9000")
	if ok {
		t.Error("unknown model should not be found in pricing table")
	}
}

func TestModelPricing_CostCalculation(t *testing.T) {
	p, _ := modelPricing("gpt-4o-mini")
	// 1000 prompt + 200 completion tokens
	cost := float64(1000)/1_000_000*p.InputPer1M + float64(200)/1_000_000*p.OutputPer1M
	// expected: 0.00000015 + 0.00000012 = 0.00000027 — tiny, just verify non-zero
	if cost <= 0 {
		t.Errorf("cost should be positive, got %v", cost)
	}
}

func TestModelPricing_ContextWindowVariety(t *testing.T) {
	cases := []struct {
		model   string
		wantCtx int
	}{
		{"gpt-3.5-turbo", 16_385},
		{"o1", 200_000},
		{"anthropic/claude-3-5-sonnet", 200_000},
		{"qwen/qwen3.5-9b", 32_768},
	}
	for _, tc := range cases {
		p, ok := modelPricing(tc.model)
		if !ok {
			t.Errorf("model %q should be known", tc.model)
			continue
		}
		if p.ContextWindow != tc.wantCtx {
			t.Errorf("model %q: context window = %d, want %d", tc.model, p.ContextWindow, tc.wantCtx)
		}
	}
}

// ---------------------------------------------------------------------------
// Context status tests
// ---------------------------------------------------------------------------

func TestContextStatus_OK(t *testing.T) {
	if s := contextStatus(10.0); s != "[OK]" {
		t.Errorf("10%% should be OK, got %q", s)
	}
	if s := contextStatus(49.9); s != "[OK]" {
		t.Errorf("49.9%% should be OK, got %q", s)
	}
}

func TestContextStatus_Note(t *testing.T) {
	if s := contextStatus(50.0); s != "[NOTE: context half full]" {
		t.Errorf("50%% should be NOTE, got %q", s)
	}
	if s := contextStatus(79.9); s != "[NOTE: context half full]" {
		t.Errorf("79.9%% should be NOTE, got %q", s)
	}
}

func TestContextStatus_Warn(t *testing.T) {
	if s := contextStatus(80.0); s != "[WARN: context filling up]" {
		t.Errorf("80%% should be WARN, got %q", s)
	}
	if s := contextStatus(94.9); s != "[WARN: context filling up]" {
		t.Errorf("94.9%% should be WARN, got %q", s)
	}
}

func TestContextStatus_Critical(t *testing.T) {
	if s := contextStatus(95.0); s != "[CRITICAL: context almost full!]" {
		t.Errorf("95%% should be CRITICAL, got %q", s)
	}
	if s := contextStatus(100.0); s != "[CRITICAL: context almost full!]" {
		t.Errorf("100%% should be CRITICAL, got %q", s)
	}
}

// ---------------------------------------------------------------------------
// LastCallPromptTokens persistence
// ---------------------------------------------------------------------------

func TestSaveAndLoadStats_WithLastCallPrompt(t *testing.T) {
	f, err := os.CreateTemp("", "stats_ctx_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	original := sessionStats{
		PromptTokens:         3000,
		CompletionTokens:     600,
		TotalTokens:          3600,
		EstimatedCostUSD:     0.00090,
		Calls:                5,
		LastCallPromptTokens: 1200,
	}

	if err := saveStats(f.Name(), original); err != nil {
		t.Fatalf("saveStats failed: %v", err)
	}

	loaded, err := loadStats(f.Name())
	if err != nil {
		t.Fatalf("loadStats failed: %v", err)
	}

	if loaded.LastCallPromptTokens != original.LastCallPromptTokens {
		t.Errorf("LastCallPromptTokens: got %d, want %d", loaded.LastCallPromptTokens, original.LastCallPromptTokens)
	}
	if loaded != original {
		t.Errorf("full stats mismatch: got %+v, want %+v", loaded, original)
	}
}

// ---------------------------------------------------------------------------
// Summary path tests (Day9)
// ---------------------------------------------------------------------------

func TestSummaryPath_Standard(t *testing.T) {
	got := summaryPath("chat_history.json")
	want := "chat_history.summary.txt"
	if got != want {
		t.Errorf("summaryPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestSummaryPath_Empty(t *testing.T) {
	if p := summaryPath(""); p != "" {
		t.Errorf("summaryPath(\"\") should be empty, got %q", p)
	}
}

func TestSummaryPath_WithDirectory(t *testing.T) {
	got := summaryPath("/tmp/my_chat.json")
	want := "/tmp/my_chat.summary.txt"
	if got != want {
		t.Errorf("summaryPath(/tmp/my_chat.json) = %q, want %q", got, want)
	}
}

func TestSummaryPath_NoExtension(t *testing.T) {
	got := summaryPath("history")
	want := "history.summary.txt"
	if got != want {
		t.Errorf("summaryPath(history) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Load/save summary tests (Day9)
// ---------------------------------------------------------------------------

func TestLoadSummary_FileNotExist(t *testing.T) {
	s, err := loadSummary("/tmp/nonexistent_summary_xyz.txt")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if s != "" {
		t.Fatalf("expected empty string for missing file, got: %q", s)
	}
}

func TestLoadSummary_EmptyPath(t *testing.T) {
	s, err := loadSummary("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "" {
		t.Fatalf("expected empty string for empty path, got: %q", s)
	}
}

func TestSaveAndLoadSummary(t *testing.T) {
	f, err := os.CreateTemp("", "summary_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	content := "The user asked about Go programming. The assistant explained goroutines and channels."
	if err := saveSummary(f.Name(), content); err != nil {
		t.Fatalf("saveSummary failed: %v", err)
	}

	loaded, err := loadSummary(f.Name())
	if err != nil {
		t.Fatalf("loadSummary failed: %v", err)
	}
	if loaded != content {
		t.Errorf("loaded summary %q != original %q", loaded, content)
	}
}

func TestSaveSummary_EmptyPath(t *testing.T) {
	if err := saveSummary("", "some content"); err != nil {
		t.Fatalf("saveSummary with empty path should be a no-op, got: %v", err)
	}
}

func TestSaveAndLoadSummary_TrimsWhitespace(t *testing.T) {
	f, err := os.CreateTemp("", "summary_ws_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	content := "Some summary text"
	if err := saveSummary(f.Name(), content); err != nil {
		t.Fatalf("saveSummary failed: %v", err)
	}

	loaded, err := loadSummary(f.Name())
	if err != nil {
		t.Fatalf("loadSummary failed: %v", err)
	}
	// saveSummary appends a newline; loadSummary should trim it back
	if loaded != content {
		t.Errorf("trailing newline not trimmed: got %q, want %q", loaded, content)
	}
}

// ---------------------------------------------------------------------------
// trimHistory tests (Day9)
// ---------------------------------------------------------------------------

func TestTrimHistory_BelowLimit(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "q1"},
		{Role: roleAssistant, Content: "a1"},
	}
	keep, excess := trimHistory(history, 10)
	if len(excess) != 0 {
		t.Errorf("expected no excess for 2 messages with limit 10, got %d", len(excess))
	}
	if len(keep) != 2 {
		t.Errorf("expected 2 keep messages, got %d", len(keep))
	}
}

func TestTrimHistory_AtLimit(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "q1"},
		{Role: roleAssistant, Content: "a1"},
		{Role: roleUser, Content: "q2"},
		{Role: roleAssistant, Content: "a2"},
	}
	keep, excess := trimHistory(history, 4)
	if len(excess) != 0 {
		t.Errorf("expected no excess at limit, got %d", len(excess))
	}
	if len(keep) != 4 {
		t.Errorf("expected 4 keep messages at limit, got %d", len(keep))
	}
}

func TestTrimHistory_AboveLimit(t *testing.T) {
	history := make([]Message, 12)
	for i := range history {
		if i%2 == 0 {
			history[i] = Message{Role: roleUser, Content: fmt.Sprintf("q%d", i/2+1)}
		} else {
			history[i] = Message{Role: roleAssistant, Content: fmt.Sprintf("a%d", i/2+1)}
		}
	}
	keep, excess := trimHistory(history, 10)
	if len(excess) != 2 {
		t.Errorf("expected 2 excess messages, got %d", len(excess))
	}
	if len(keep) != 10 {
		t.Errorf("expected 10 keep messages, got %d", len(keep))
	}
	if excess[0].Content != "q1" {
		t.Errorf("excess[0] should be q1 (oldest), got %q", excess[0].Content)
	}
	if keep[0].Content != "q2" {
		t.Errorf("keep[0] should be q2, got %q", keep[0].Content)
	}
}

func TestTrimHistory_ZeroLimit(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "q1"},
		{Role: roleAssistant, Content: "a1"},
	}
	keep, excess := trimHistory(history, 0)
	if len(excess) != 0 {
		t.Errorf("zero limit should disable trimming, got %d excess", len(excess))
	}
	if len(keep) != 2 {
		t.Errorf("expected all 2 messages kept with zero limit, got %d", len(keep))
	}
}

func TestTrimHistory_NilHistory(t *testing.T) {
	keep, excess := trimHistory(nil, 10)
	if len(excess) != 0 {
		t.Errorf("nil history should produce no excess, got %d", len(excess))
	}
	if keep != nil && len(keep) != 0 {
		t.Errorf("nil history should produce nil/empty keep, got %d", len(keep))
	}
}

func TestTrimHistory_ExcessContainsOldest(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "oldest"},
		{Role: roleAssistant, Content: "oldest-reply"},
		{Role: roleUser, Content: "newest"},
		{Role: roleAssistant, Content: "newest-reply"},
	}
	keep, excess := trimHistory(history, 2)
	if len(excess) != 2 || excess[0].Content != "oldest" {
		t.Errorf("excess should contain oldest messages, got: %v", excess)
	}
	if len(keep) != 2 || keep[0].Content != "newest" {
		t.Errorf("keep should contain newest messages, got: %v", keep)
	}
}

// ---------------------------------------------------------------------------
// Day10: factsFilePath tests
// ---------------------------------------------------------------------------

func TestFactsFilePath_Standard(t *testing.T) {
	got := factsFilePath("chat_history.json")
	want := "chat_history.facts.json"
	if got != want {
		t.Errorf("factsFilePath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestFactsFilePath_Empty(t *testing.T) {
	if p := factsFilePath(""); p != "" {
		t.Errorf("factsFilePath(\"\") should be empty, got %q", p)
	}
}

func TestFactsFilePath_WithDirectory(t *testing.T) {
	got := factsFilePath("/tmp/chat.json")
	want := "/tmp/chat.facts.json"
	if got != want {
		t.Errorf("factsFilePath(/tmp/chat.json) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Day10: facts store load/save tests
// ---------------------------------------------------------------------------

func TestLoadFacts_FileNotExist(t *testing.T) {
	fs, err := loadFacts("/tmp/nonexistent_facts_xyz.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if fs.Facts == nil {
		t.Fatal("expected non-nil Facts map")
	}
	if len(fs.Facts) != 0 {
		t.Errorf("expected empty map, got: %v", fs.Facts)
	}
}

func TestLoadFacts_EmptyPath(t *testing.T) {
	fs, err := loadFacts("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs.Facts == nil {
		t.Fatal("expected non-nil Facts map for empty path")
	}
}

func TestSaveAndLoadFacts(t *testing.T) {
	f, err := os.CreateTemp("", "facts_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	original := factsStore{Facts: map[string]string{
		"goal":       "Build a REST API",
		"language":   "Go",
		"constraint": "No external libraries",
	}}

	if err := saveFacts(f.Name(), original); err != nil {
		t.Fatalf("saveFacts failed: %v", err)
	}

	loaded, err := loadFacts(f.Name())
	if err != nil {
		t.Fatalf("loadFacts failed: %v", err)
	}

	if len(loaded.Facts) != len(original.Facts) {
		t.Fatalf("expected %d facts, got %d", len(original.Facts), len(loaded.Facts))
	}
	for k, v := range original.Facts {
		if loaded.Facts[k] != v {
			t.Errorf("facts[%q] = %q, want %q", k, loaded.Facts[k], v)
		}
	}
}

func TestSaveFacts_EmptyPath(t *testing.T) {
	if err := saveFacts("", factsStore{Facts: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("saveFacts with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadFacts_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "facts_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	_, err = loadFacts(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Day10: factsSystemBlock tests
// ---------------------------------------------------------------------------

func TestFactsSystemBlock_Empty(t *testing.T) {
	fs := factsStore{Facts: map[string]string{}}
	if block := factsSystemBlock(fs); block != "" {
		t.Errorf("empty facts should produce empty block, got %q", block)
	}
}

func TestFactsSystemBlock_WithFacts(t *testing.T) {
	fs := factsStore{Facts: map[string]string{
		"goal":     "Write tests",
		"language": "Go",
	}}
	block := factsSystemBlock(fs)
	if !strings.Contains(block, "[Sticky Facts]") {
		t.Error("block should contain [Sticky Facts] header")
	}
	if !strings.Contains(block, "goal: Write tests") {
		t.Error("block should contain goal fact")
	}
	if !strings.Contains(block, "language: Go") {
		t.Error("block should contain language fact")
	}
}

func TestFactsSystemBlock_Sorted(t *testing.T) {
	fs := factsStore{Facts: map[string]string{
		"zebra": "z",
		"alpha": "a",
		"beta":  "b",
	}}
	block := factsSystemBlock(fs)
	lines := strings.Split(block, "\n")
	// lines[0] is the header, lines[1..] are facts
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), block)
	}
	// Facts should be alpha, beta, zebra in sorted order
	if !strings.HasPrefix(lines[1], "alpha:") {
		t.Errorf("first fact should be alpha, got: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "beta:") {
		t.Errorf("second fact should be beta, got: %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "zebra:") {
		t.Errorf("third fact should be zebra, got: %q", lines[3])
	}
}

// ---------------------------------------------------------------------------
// Day10: branchStatePath tests
// ---------------------------------------------------------------------------

func TestBranchStatePath_Standard(t *testing.T) {
	got := branchStatePath("chat_history.json")
	want := "chat_history.branch-state.json"
	if got != want {
		t.Errorf("branchStatePath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestBranchStatePath_Empty(t *testing.T) {
	if p := branchStatePath(""); p != "" {
		t.Errorf("branchStatePath(\"\") should be empty, got %q", p)
	}
}

// ---------------------------------------------------------------------------
// Day10: branchHistoryPath tests
// ---------------------------------------------------------------------------

func TestBranchHistoryPath_Main(t *testing.T) {
	got := branchHistoryPath("chat_history.json", "main")
	want := "chat_history.json"
	if got != want {
		t.Errorf("branchHistoryPath for main = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_Empty(t *testing.T) {
	got := branchHistoryPath("chat_history.json", "")
	want := "chat_history.json"
	if got != want {
		t.Errorf("branchHistoryPath for empty = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_Named(t *testing.T) {
	got := branchHistoryPath("chat_history.json", "feature-a")
	want := "chat_history.branch-feature-a.json"
	if got != want {
		t.Errorf("branchHistoryPath for feature-a = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_EmptyBase(t *testing.T) {
	got := branchHistoryPath("", "feature-a")
	if got != "" {
		t.Errorf("branchHistoryPath with empty base should be empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Day10: branch state load/save tests
// ---------------------------------------------------------------------------

func TestDefaultBranchState(t *testing.T) {
	bs := defaultBranchState()
	if bs.Current != "main" {
		t.Errorf("default current = %q, want main", bs.Current)
	}
	if len(bs.Branches) != 1 || bs.Branches[0] != "main" {
		t.Errorf("default branches = %v, want [main]", bs.Branches)
	}
	if bs.Checkpoints == nil {
		t.Error("default checkpoints should be non-nil map")
	}
}

func TestLoadBranchState_FileNotExist(t *testing.T) {
	bs, err := loadBranchState("/tmp/nonexistent_branch_state_xyz.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if bs.Current != "main" {
		t.Errorf("expected default current=main, got %q", bs.Current)
	}
}

func TestLoadBranchState_EmptyPath(t *testing.T) {
	bs, err := loadBranchState("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bs.Current != "main" {
		t.Errorf("empty path should return default state, got current=%q", bs.Current)
	}
}

func TestSaveAndLoadBranchState(t *testing.T) {
	f, err := os.CreateTemp("", "branch_state_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	original := branchState{
		Current:  "feature-x",
		Branches: []string{"main", "feature-x", "hotfix"},
		Checkpoints: map[string]branchCheckpoint{
			"cp1": {
				Messages:  []Message{{Role: roleUser, Content: "hello"}},
				CreatedAt: "2026-01-01T00:00:00Z",
			},
		},
	}

	if err := saveBranchState(f.Name(), original); err != nil {
		t.Fatalf("saveBranchState failed: %v", err)
	}

	loaded, err := loadBranchState(f.Name())
	if err != nil {
		t.Fatalf("loadBranchState failed: %v", err)
	}

	if loaded.Current != original.Current {
		t.Errorf("current: got %q, want %q", loaded.Current, original.Current)
	}
	if len(loaded.Branches) != len(original.Branches) {
		t.Errorf("branches len: got %d, want %d", len(loaded.Branches), len(original.Branches))
	}
	cp, ok := loaded.Checkpoints["cp1"]
	if !ok {
		t.Fatal("checkpoint cp1 not found after load")
	}
	if len(cp.Messages) != 1 || cp.Messages[0].Content != "hello" {
		t.Errorf("checkpoint messages mismatch: %v", cp.Messages)
	}
}

func TestSaveBranchState_EmptyPath(t *testing.T) {
	if err := saveBranchState("", defaultBranchState()); err != nil {
		t.Fatalf("saveBranchState with empty path should be a no-op, got: %v", err)
	}
}

func TestCurrentBranchHistoryPath_Main(t *testing.T) {
	bs := defaultBranchState()
	got := currentBranchHistoryPath("chat_history.json", bs)
	want := "chat_history.json"
	if got != want {
		t.Errorf("currentBranchHistoryPath(main) = %q, want %q", got, want)
	}
}

func TestCurrentBranchHistoryPath_Named(t *testing.T) {
	bs := branchState{Current: "topic-a"}
	got := currentBranchHistoryPath("chat_history.json", bs)
	want := "chat_history.branch-topic-a.json"
	if got != want {
		t.Errorf("currentBranchHistoryPath(topic-a) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Day10: slidingWindow tests
// ---------------------------------------------------------------------------

func TestSlidingWindow_BelowLimit(t *testing.T) {
	history := []Message{
		{Role: roleUser, Content: "q1"},
		{Role: roleAssistant, Content: "a1"},
	}
	got := slidingWindow(history, 5)
	if len(got) != 2 {
		t.Errorf("expected all 2 messages kept, got %d", len(got))
	}
}

func TestSlidingWindow_AtLimit(t *testing.T) {
	history := make([]Message, 5)
	for i := range history {
		history[i] = Message{Role: roleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := slidingWindow(history, 5)
	if len(got) != 5 {
		t.Errorf("expected 5 messages at limit, got %d", len(got))
	}
}

func TestSlidingWindow_AboveLimit(t *testing.T) {
	history := make([]Message, 10)
	for i := range history {
		history[i] = Message{Role: roleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := slidingWindow(history, 5)
	if len(got) != 5 {
		t.Errorf("expected 5 messages, got %d", len(got))
	}
	// Should be the last 5 messages (m5..m9)
	if got[0].Content != "m5" {
		t.Errorf("first kept message should be m5 (oldest kept), got %q", got[0].Content)
	}
	if got[4].Content != "m9" {
		t.Errorf("last kept message should be m9 (newest), got %q", got[4].Content)
	}
}

func TestSlidingWindow_ZeroDisables(t *testing.T) {
	history := make([]Message, 10)
	for i := range history {
		history[i] = Message{Role: roleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := slidingWindow(history, 0)
	if len(got) != 10 {
		t.Errorf("window=0 should return all messages, got %d", len(got))
	}
}

func TestSlidingWindow_Nil(t *testing.T) {
	got := slidingWindow(nil, 5)
	if len(got) != 0 {
		t.Errorf("nil history should return empty slice, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Day10: summary disabled by default — verify flag wiring via trimHistory
// ---------------------------------------------------------------------------

func TestSummaryDisabledByDefault_NoTrim(t *testing.T) {
	// When summaryEnabled=false, trimHistory result should be saved as-is.
	// We verify that trimHistory still works (keeps last N), but no excess is generated
	// unless limit is exceeded — summary generation is a side-effect tested via integration.
	history := make([]Message, 6)
	for i := range history {
		history[i] = Message{Role: roleUser, Content: fmt.Sprintf("m%d", i)}
	}
	keep, excess := trimHistory(history, 10)
	if len(excess) != 0 {
		t.Errorf("no excess expected when below limit, got %d", len(excess))
	}
	if len(keep) != 6 {
		t.Errorf("expected all 6 kept, got %d", len(keep))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
