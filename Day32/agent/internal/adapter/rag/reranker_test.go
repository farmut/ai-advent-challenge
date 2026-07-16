package rag

import (
	"context"
	"errors"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// stubLLM returns a canned response (or error) and records the request.
type stubLLM struct {
	resp string
	err  error
	got  port.LLMRequest
}

func (s *stubLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	s.got = req
	if s.err != nil {
		return port.LLMResponse{}, s.err
	}
	return port.LLMResponse{Content: s.resp}, nil
}

func chunks() []domain.RetrievedChunk {
	return []domain.RetrievedChunk{
		{File: "a.md", Content: "alpha", Similarity: 0.80},
		{File: "b.md", Content: "beta", Similarity: 0.70},
	}
}

func TestLLMReranker_ScoresAndSorts(t *testing.T) {
	// Model ranks the second chunk higher; note the ```json fence to exercise
	// tolerant extraction.
	llm := &stubLLM{resp: "```json\n[{\"index\":1,\"score\":0.2},{\"index\":2,\"score\":0.95}]\n```"}
	rr := NewLLMReranker(llm, "gpt-4o")

	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(out))
	}
	if out[0].File != "b.md" || out[0].RerankScore != 0.95 {
		t.Errorf("expected b.md@0.95 first, got %s@%.2f", out[0].File, out[0].RerankScore)
	}
	if out[1].File != "a.md" || out[1].RerankScore != 0.2 {
		t.Errorf("expected a.md@0.2 second, got %s@%.2f", out[1].File, out[1].RerankScore)
	}
	// Deterministic scoring: temperature must be 0.
	if llm.got.Temperature != 0 {
		t.Errorf("expected temperature 0, got %v", llm.got.Temperature)
	}
}

func TestLLMReranker_MissingScoreFallsBackToSimilarity(t *testing.T) {
	// Model scores only chunk 1; chunk 2 must keep its similarity as RerankScore.
	llm := &stubLLM{resp: "[{\"index\":1,\"score\":0.1}]"}
	rr := NewLLMReranker(llm, "m")

	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// b.md keeps 0.70 (its similarity) and thus outranks a.md's 0.1.
	if out[0].File != "b.md" || out[0].RerankScore != 0.70 {
		t.Errorf("expected fallback b.md@0.70 first, got %s@%.2f", out[0].File, out[0].RerankScore)
	}
}

func TestLLMReranker_ClampsOutOfRangeScores(t *testing.T) {
	llm := &stubLLM{resp: "[{\"index\":1,\"score\":5},{\"index\":2,\"score\":-3}]"}
	rr := NewLLMReranker(llm, "m")

	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].RerankScore != 1 {
		t.Errorf("score 5 should clamp to 1, got %.2f", out[0].RerankScore)
	}
	if out[1].RerankScore != 0 {
		t.Errorf("score -3 should clamp to 0, got %.2f", out[1].RerankScore)
	}
}

func TestLLMReranker_EmptyInputNoCall(t *testing.T) {
	llm := &stubLLM{err: errors.New("should not be called")}
	rr := NewLLMReranker(llm, "m")

	out, err := rr.Rerank(context.Background(), "q", nil)
	if err != nil || out != nil {
		t.Fatalf("empty input should return (nil, nil), got (%v, %v)", out, err)
	}
}

func TestLLMReranker_LLMErrorPropagates(t *testing.T) {
	llm := &stubLLM{err: errors.New("network down")}
	rr := NewLLMReranker(llm, "m")

	if _, err := rr.Rerank(context.Background(), "q", chunks()); err == nil {
		t.Fatal("expected LLM error to propagate")
	}
}

func TestLLMReranker_LineScoreFormat(t *testing.T) {
	// Completion-style rerankers (e.g. MedAIBase/Qwen3-VL-Reranker) ignore the
	// JSON instruction and emit "N. score" lines plus trailing prose. The parser
	// must read the scores and ignore the prose (including a bare "[1]").
	llm := &stubLLM{resp: "1. 0.2\n2. 0.95\n\nThe second passage is more relevant.\nAnswer: [2]"}
	rr := NewLLMReranker(llm, "MedAIBase/Qwen3-VL-Reranker:2b")

	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].File != "b.md" || out[0].RerankScore != 0.95 {
		t.Errorf("expected b.md@0.95 first, got %s@%.2f", out[0].File, out[0].RerankScore)
	}
	if out[1].File != "a.md" || out[1].RerankScore != 0.2 {
		t.Errorf("expected a.md@0.2 second, got %s@%.2f", out[1].File, out[1].RerankScore)
	}
}

func TestLLMReranker_UnparseableResponseErrors(t *testing.T) {
	llm := &stubLLM{resp: "I cannot do that"}
	rr := NewLLMReranker(llm, "m")

	if _, err := rr.Rerank(context.Background(), "q", chunks()); err == nil {
		t.Fatal("expected parse error for non-JSON response")
	}
}
