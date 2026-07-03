package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

// fakeRetriever is a stub port.Retriever for testing the pipeline without a
// SQLite index or an embeddings endpoint.
type fakeRetriever struct {
	chunks   []domain.RetrievedChunk
	err      error
	gotQuery string
	gotTopK  int
}

func (f *fakeRetriever) Retrieve(_ context.Context, query string, topK int) ([]domain.RetrievedChunk, error) {
	f.gotQuery = query
	f.gotTopK = topK
	return f.chunks, f.err
}

func TestBuildRAGPrompt_NoChunks_ReturnsQuestionUnchanged(t *testing.T) {
	q := "What is MVCC?"
	if got := BuildRAGPrompt(q, nil); got != q {
		t.Fatalf("expected question unchanged, got %q", got)
	}
}

func TestBuildRAGPrompt_CombinesContextAndQuestion(t *testing.T) {
	q := "How does PostgreSQL isolate transactions?"
	chunks := []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "MVCC", Content: "  snapshot isolation text  ", Similarity: 0.91},
		{File: "pg.pdf", Content: "vacuum text", Similarity: 0.80},
	}

	prompt := BuildRAGPrompt(q, chunks)

	for _, want := range []string{
		"=== CONTEXT ===",
		"[1] source: pg.pdf — MVCC (similarity 0.910)",
		"snapshot isolation text",
		"[2] source: pg.pdf (similarity 0.800)",
		"=== QUESTION ===",
		q,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	// Content should be trimmed of surrounding whitespace.
	if strings.Contains(prompt, "  snapshot isolation text  ") {
		t.Error("chunk content was not trimmed")
	}
	// Question must come after the context block.
	if strings.Index(prompt, "=== QUESTION ===") < strings.Index(prompt, "=== CONTEXT ===") {
		t.Error("question block appears before context block")
	}
}

func TestRAGUseCase_BuildPrompt_ForwardsQueryAndTopK(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{{File: "a.md", Content: "ctx", Similarity: 0.5}}}
	uc := NewRAGUseCase(fr)

	prompt, chunks, err := uc.BuildPrompt(context.Background(), "q?", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fr.gotQuery != "q?" || fr.gotTopK != 3 {
		t.Errorf("retriever got (%q, %d), want (%q, %d)", fr.gotQuery, fr.gotTopK, "q?", 3)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(prompt, "=== QUESTION ===") || !strings.Contains(prompt, "q?") {
		t.Errorf("prompt not grounded correctly: %s", prompt)
	}
}

func TestRAGUseCase_BuildPrompt_PropagatesError(t *testing.T) {
	fr := &fakeRetriever{err: errors.New("boom")}
	uc := NewRAGUseCase(fr)

	if _, _, err := uc.BuildPrompt(context.Background(), "q", 5); err == nil {
		t.Fatal("expected error to propagate")
	}
}
