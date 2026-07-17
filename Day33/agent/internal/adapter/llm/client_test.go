package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ---- Test helpers ----

func chatCapture(t *testing.T, provider string, req port.LLMRequest) map[string]any {
	t.Helper()
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("failed to unmarshal body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "mock"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	t.Cleanup(srv.Close)

	client := llm.NewClient(llm.Config{Provider: provider, APIKey: "fake", BaseURL: srv.URL})
	_, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat() failed: %v", err)
	}
	return captured
}

func prettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	return strings.TrimSpace(buf.String())
}

// ---- Token parameter tests ----

func TestOpenRouter_SendsBothTokenParams(t *testing.T) {
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:     "openai/gpt-oss-120b:free",
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		MaxTokens: 30,
	})
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
	captured := chatCapture(t, llm.ProviderOpenAI, port.LLMRequest{
		Model:     "gpt-4o",
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		MaxTokens: 50,
	})
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

// ---- Stop sequences ----

func TestStopSequences(t *testing.T) {
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:     "openai/gpt-4",
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		MaxTokens: 100,
		Stop:      []string{"###", "END"},
	})
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

// ---- Temperature ----

func TestTemperature_IncludedInRequest(t *testing.T) {
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:       "openai/gpt-4o-mini",
		Messages:    []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		Temperature: 0.7,
	})
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
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:       "openai/gpt-4o-mini",
		Messages:    []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		Temperature: -1,
	})
	if _, has := captured["temperature"]; has {
		t.Error("temperature should not be present when set to -1")
	}
}

// ---- Format instruction in user prompt ----

func TestFormatMarkdown_InstructionInUserPrompt(t *testing.T) {
	query := "hello\n\nFormat your response using Markdown."
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: query}},
	})

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
	query := "hello\n\nRespond in JSON format."
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: query}},
	})

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
	query := "hello\n\nFormat as a numbered list"
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: query}},
	})

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "numbered list") {
				t.Errorf("user message should contain custom instruction, got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

func TestFormatHint_CustomInstructionInPrompt(t *testing.T) {
	query := "test query\n\nUse bullet points and bold text"
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: query}},
	})

	messages, _ := captured["messages"].([]any)
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if !strings.Contains(content, "bullet points") || !strings.Contains(content, "bold text") {
				t.Errorf("user message should contain custom instruction, got: %q", content)
			}
			if strings.Contains(content, "Format your response") {
				t.Errorf("user message should not contain default format instruction, got: %q", content)
			}
			return
		}
	}
	t.Error("no user message found")
}

// ---- System message placement ----

func TestSystemMessage_SentFirstInMessages(t *testing.T) {
	systemContent := "Ты опытный программист на Go. Отвечай кратко и по делу."
	userContent := "hello"

	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: systemContent},
			{Role: domain.RoleUser, Content: userContent},
		},
	})
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
}

func TestSystemMessage_EmptyNotIncluded(t *testing.T) {
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	})
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

	captured := chatCapture(t, llm.ProviderOpenAI, port.LLMRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: systemContent},
			{Role: domain.RoleUser, Content: userContent},
		},
		MaxTokens: 100,
	})
	t.Logf("Combined payload: %s", prettyJSON(captured))

	messages, _ := captured["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	hasSystem, hasUser := false, false
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

// ---- History included in request ----

func TestHistoryIncludedInRequest(t *testing.T) {
	captured := chatCapture(t, llm.ProviderOpenRouter, port.LLMRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "first question"},
			{Role: domain.RoleAssistant, Content: "first answer"},
			{Role: domain.RoleUser, Content: "second question"},
		},
	})

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

// ---- Self-signed CA (CACertFile) tests ----

// mockTLSResponse writes a minimal valid chat completion.
func writeMockChat(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
		},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

// TestCACertFile_TrustsSelfSignedServer verifies that pointing CACertFile at a
// server's self-signed cert lets the client connect with TLS verification ON.
func TestCACertFile_TrustsSelfSignedServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMockChat(w)
	}))
	t.Cleanup(srv.Close)

	// Persist the server's self-signed cert to a temp PEM file.
	certPath := filepath.Join(t.TempDir(), "server.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	req := port.LLMRequest{
		Model:    "gemma4:12b",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	}

	// With the cert trusted → success.
	client := llm.NewClient(llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL, CACertFile: certPath})
	if _, err := client.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat() with trusted CA cert failed: %v", err)
	}

	// Without the cert → TLS verification must reject the self-signed server.
	bad := llm.NewClient(llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL})
	if _, err := bad.Chat(context.Background(), req); err == nil {
		t.Fatalf("Chat() without CA cert unexpectedly succeeded against self-signed server")
	}
}
