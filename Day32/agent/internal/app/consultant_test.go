package app

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// echoLLM records the requests it receives and answers with a fixed reply.
type echoLLM struct {
	reqs []port.LLMRequest
}

func (e *echoLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	e.reqs = append(e.reqs, req)
	return port.LLMResponse{Content: "ответ консультанта"}, nil
}

func TestConsultant_DisabledInConfig(t *testing.T) {
	tb := testToolbelt(t, &echoLLM{}, nil)
	tb.Cfg.Consultant.Enabled = false
	if _, err := tb.NewConsultant(); err == nil {
		t.Fatal("expected error when consultant.enabled=false")
	}
}

func TestConsultant_AskKeepsRawHistory(t *testing.T) {
	llm := &echoLLM{}
	tb := testToolbelt(t, llm, nil)
	tb.Cfg.Consultant.Enabled = true
	tb.Cfg.Consultant.RAG.Enabled = false // no docs index in unit tests

	c, err := tb.NewConsultant()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Ask(context.Background(), "первый вопрос"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(context.Background(), "второй вопрос"); err != nil {
		t.Fatal(err)
	}

	// The second call's prompt must replay the raw first Q/A pair.
	last := llm.reqs[len(llm.reqs)-1]
	var haveQ, haveA bool
	for _, m := range last.Messages {
		if m.Role == domain.RoleUser && m.Content == "первый вопрос" {
			haveQ = true
		}
		if m.Role == domain.RoleAssistant && m.Content == "ответ консультанта" {
			haveA = true
		}
	}
	if !haveQ || !haveA {
		t.Errorf("second turn must carry the first Q/A pair (haveQ=%t haveA=%t): %+v", haveQ, haveA, last.Messages)
	}

	// The consultant system prompt must be present.
	if len(last.Messages) == 0 || last.Messages[0].Role != domain.RoleSystem ||
		!strings.Contains(last.Messages[0].Content, "консультант по документации") {
		t.Error("system message must carry the consultant prompt")
	}
}

func TestConsultant_DoesNotTouchSharedHistory(t *testing.T) {
	tb := testToolbelt(t, &echoLLM{}, nil)
	tb.Cfg.Consultant.Enabled = true
	tb.Cfg.Consultant.RAG.Enabled = false

	c, err := tb.NewConsultant()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Ask(context.Background(), "вопрос"); err != nil {
		t.Fatal(err)
	}

	// Consultant dialogue is mode-local: the orchestrator's shared history file
	// must stay empty.
	hist, _ := tb.Memory.History().Load()
	if len(hist) != 0 {
		t.Errorf("consultant must not write the shared history, got %d messages", len(hist))
	}
}

func TestConsultant_PromptOverride(t *testing.T) {
	llm := &echoLLM{}
	tb := testToolbelt(t, llm, nil)
	tb.Cfg.Consultant.Enabled = true
	tb.Cfg.Consultant.RAG.Enabled = false
	tb.Cfg.Consultant.Prompt = "Ты кастомный помощник."

	c, err := tb.NewConsultant()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Ask(context.Background(), "вопрос"); err != nil {
		t.Fatal(err)
	}
	first := llm.reqs[0]
	if first.Messages[0].Content != "Ты кастомный помощник." {
		t.Errorf("prompt override not applied: %q", first.Messages[0].Content)
	}
}
