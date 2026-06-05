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

	_, err := sendChatRequest(provider, "fake-key", srv.URL, model, []Message{
		{Role: roleUser, Content: query},
	}, maxTokens, stop, false)
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
