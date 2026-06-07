package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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

func prettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	return strings.TrimSpace(buf.String())
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
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
