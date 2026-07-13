package usecase

import (
	"context"
	"errors"
	"sort"
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

// fakeReranker reverses similarity into RerankScore so tests can tell whether
// the rerank stage ran and reordered the chunks.
type fakeReranker struct {
	err       error
	gotQuery  string
	gotChunks []domain.RetrievedChunk
	scores    map[string]float64 // File → RerankScore override
}

func (f *fakeReranker) Rerank(_ context.Context, query string, chunks []domain.RetrievedChunk) ([]domain.RetrievedChunk, error) {
	f.gotQuery = query
	f.gotChunks = chunks
	if f.err != nil {
		return nil, f.err
	}
	out := make([]domain.RetrievedChunk, len(chunks))
	copy(out, chunks)
	for i := range out {
		out[i].RerankScore = f.scores[out[i].File]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RerankScore > out[j].RerankScore })
	return out, nil
}

func TestRAGUseCase_BuildPrompt_ForwardsQueryAndTopK(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{{File: "a.md", Content: "ctx", Similarity: 0.5}}}
	uc := NewRAGUseCase(fr, nil)

	res, err := uc.BuildPrompt(context.Background(), "q?", RAGConfig{TopKRetrieve: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fr.gotQuery != "q?" || fr.gotTopK != 3 {
		t.Errorf("retriever got (%q, %d), want (%q, %d)", fr.gotQuery, fr.gotTopK, "q?", 3)
	}
	if len(res.Final) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(res.Final))
	}
	if res.Reranked {
		t.Error("Reranked should be false when no reranker is wired")
	}
	if !strings.Contains(res.Prompt, "=== QUESTION ===") || !strings.Contains(res.Prompt, "q?") {
		t.Errorf("prompt not grounded correctly: %s", res.Prompt)
	}
}

func TestRAGUseCase_BuildPrompt_PropagatesError(t *testing.T) {
	fr := &fakeRetriever{err: errors.New("boom")}
	uc := NewRAGUseCase(fr, nil)

	if _, err := uc.BuildPrompt(context.Background(), "q", RAGConfig{TopKRetrieve: 5}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestRAGUseCase_BuildPrompt_ThresholdFiltersLowSimilarity(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "hi.md", Content: "keep", Similarity: 0.9},
		{File: "lo.md", Content: "drop", Similarity: 0.3},
	}}
	uc := NewRAGUseCase(fr, nil)

	res, err := uc.BuildPrompt(context.Background(), "q", RAGConfig{TopKRetrieve: 10, Threshold: 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Retrieved) != 2 {
		t.Fatalf("expected 2 retrieved, got %d", len(res.Retrieved))
	}
	if len(res.Final) != 1 || res.Final[0].File != "hi.md" {
		t.Fatalf("threshold should keep only hi.md, got %+v", res.Final)
	}
}

func TestRAGUseCase_BuildPrompt_TopKFinalCapsAfterFiltering(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "a.md", Content: "a", Similarity: 0.9},
		{File: "b.md", Content: "b", Similarity: 0.8},
		{File: "c.md", Content: "c", Similarity: 0.7},
	}}
	uc := NewRAGUseCase(fr, nil)

	res, err := uc.BuildPrompt(context.Background(), "q", RAGConfig{TopKRetrieve: 10, TopKFinal: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Final) != 2 {
		t.Fatalf("TopKFinal=2 should keep 2, got %d", len(res.Final))
	}
	if res.Final[0].File != "a.md" || res.Final[1].File != "b.md" {
		t.Errorf("expected the two highest-scoring chunks, got %+v", res.Final)
	}
}

func TestRAGUseCase_BuildPrompt_RerankReordersAndThresholdUsesRerankScore(t *testing.T) {
	// lo.md wins retrieval but the reranker judges hi.md far more relevant.
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "lo.md", Content: "low", Similarity: 0.95},
		{File: "hi.md", Content: "high", Similarity: 0.60},
	}}
	rr := &fakeReranker{scores: map[string]float64{"hi.md": 0.9, "lo.md": 0.1}}
	uc := NewRAGUseCase(fr, rr)

	res, err := uc.BuildPrompt(context.Background(), "q", RAGConfig{TopKRetrieve: 10, Rerank: true, Threshold: 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Reranked {
		t.Fatal("expected Reranked=true")
	}
	if rr.gotQuery != "q" || len(rr.gotChunks) != 2 {
		t.Errorf("reranker got (%q, %d chunks)", rr.gotQuery, len(rr.gotChunks))
	}
	// Threshold compares the rerank score, so lo.md (0.1) is dropped despite its
	// high similarity and hi.md (0.9) is the sole survivor.
	if len(res.Final) != 1 || res.Final[0].File != "hi.md" {
		t.Fatalf("rerank+threshold should keep only hi.md, got %+v", res.Final)
	}
}

func TestRAGUseCase_BuildPrompt_RerankErrorFallsBackToSimilarity(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{{File: "a.md", Content: "x", Similarity: 0.9}}}
	rr := &fakeReranker{err: errors.New("rerank boom")}
	uc := NewRAGUseCase(fr, rr)

	// A flaky reranker must not abort the pipeline: the query still returns,
	// degraded to the similarity-ranked pool, with the failure reported.
	res, err := uc.BuildPrompt(context.Background(), "q", RAGConfig{TopKRetrieve: 5, Rerank: true})
	if err != nil {
		t.Fatalf("rerank failure should not propagate as an error, got %v", err)
	}
	if res.RerankErr == nil {
		t.Error("expected RerankErr to be set when the reranker fails")
	}
	if res.Reranked {
		t.Error("Reranked should be false after a rerank failure")
	}
	if len(res.Final) != 1 || res.Final[0].File != "a.md" {
		t.Fatalf("expected the similarity-ranked chunk to survive, got %+v", res.Final)
	}
}
