package usecase

import (
	"context"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// RAGUseCase implements the retrieval-augmented generation pipeline up to (but
// not including) the LLM call:
//
//	question → retrieve → [rerank] → filter by threshold → keep top-K → prompt
//
// The caller feeds the returned prompt to ChatUseCase.Execute, completing the
// final "→ LLM" step.
type RAGUseCase struct {
	retriever port.Retriever
	reranker  port.Reranker // optional; nil disables the rerank stage
}

// RAGConfig tunes the two-stage retrieval pipeline.
type RAGConfig struct {
	// TopKRetrieve is how many chunks the retriever fetches (top-K before
	// filtering). A larger pool gives the reranker more to work with.
	TopKRetrieve int
	// Rerank turns on the second stage: re-score the retrieved chunks with the
	// reranker before filtering. Ignored when no reranker is wired.
	Rerank bool
	// Threshold drops chunks whose relevance score is below it (0 disables the
	// cutoff). The score compared is the rerank score when reranking ran,
	// otherwise the retrieval similarity.
	Threshold float64
	// TopKFinal caps how many chunks survive into the prompt (top-K after
	// filtering). 0 keeps all that clear the threshold.
	TopKFinal int
}

// RAGResult reports what each pipeline stage produced so callers can surface the
// retrieval/rerank/filter funnel to the user.
type RAGResult struct {
	Prompt    string                  // grounded prompt for the LLM
	Retrieved []domain.RetrievedChunk // stage 1 output (post-retrieval)
	Reranked  bool                    // whether the rerank stage ran
	Final     []domain.RetrievedChunk // chunks that survived filtering (used in Prompt)
	// RerankErr is non-nil when the rerank stage was requested but failed. The
	// pipeline then degrades to the similarity-ranked pool (Reranked stays false)
	// instead of aborting, so a flaky reranker never sinks the whole answer. The
	// caller surfaces this so the fallback is visible, not silent.
	RerankErr error
}

// NewRAGUseCase wires the pipeline to a retriever and an optional reranker.
// Pass a nil reranker to run retrieval-only.
func NewRAGUseCase(retriever port.Retriever, reranker port.Reranker) *RAGUseCase {
	return &RAGUseCase{retriever: retriever, reranker: reranker}
}

// BuildPrompt runs the full pipeline and returns the grounded prompt together
// with a breakdown of every stage.
func (uc *RAGUseCase) BuildPrompt(ctx context.Context, question string, cfg RAGConfig) (RAGResult, error) {
	retrieved, err := uc.retriever.Retrieve(ctx, question, cfg.TopKRetrieve)
	if err != nil {
		return RAGResult{}, err
	}

	chunks := retrieved
	reranked := false
	var rerankErr error
	if cfg.Rerank && uc.reranker != nil && len(chunks) > 0 {
		rr, err := uc.reranker.Rerank(ctx, question, chunks)
		if err != nil {
			// Degrade to the similarity-ranked pool rather than abort; report
			// the failure via RerankErr so the fallback is visible.
			rerankErr = err
		} else {
			chunks = rr
			reranked = true
		}
	}

	final := filterChunks(chunks, cfg.Threshold, cfg.TopKFinal, reranked)

	return RAGResult{
		Prompt:    BuildRAGPrompt(question, final),
		Retrieved: retrieved,
		Reranked:  reranked,
		Final:     final,
		RerankErr: rerankErr,
	}, nil
}

// filterChunks drops chunks scoring below threshold and then keeps at most
// topKFinal of them. Input is assumed already sorted best-first (both the
// retriever and the reranker guarantee this), so the top-K cut is a simple
// prefix. reranked selects which score the threshold is compared against.
func filterChunks(chunks []domain.RetrievedChunk, threshold float64, topKFinal int, reranked bool) []domain.RetrievedChunk {
	var kept []domain.RetrievedChunk
	for _, c := range chunks {
		if c.Score(reranked) >= threshold {
			kept = append(kept, c)
		}
	}
	if topKFinal > 0 && len(kept) > topKFinal {
		kept = kept[:topKFinal]
	}
	return kept
}

// BuildRAGPrompt combines retrieved context chunks with the user's question.
// With no chunks it returns the question unchanged so the pipeline degrades to
// a plain LLM query rather than failing.
func BuildRAGPrompt(question string, chunks []domain.RetrievedChunk) string {
	if len(chunks) == 0 {
		return question
	}

	var b strings.Builder
	b.WriteString("Answer the question using only the context below. ")
	b.WriteString("If the context does not contain the answer, say so instead of guessing. ")
	b.WriteString("Cite the sources you rely on by their [n] marker. ")
	b.WriteString("When a fragment carries a URL, reproduce it exactly as given — never shorten or invent links.\n\n")
	b.WriteString("=== CONTEXT ===\n")
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] source: %s", i+1, c.File)
		if c.Section != "" {
			fmt.Fprintf(&b, " — %s", c.Section)
		}
		fmt.Fprintf(&b, " (similarity %.3f", c.Similarity)
		if c.RerankScore > 0 {
			fmt.Fprintf(&b, ", rerank %.3f", c.RerankScore)
		}
		b.WriteString(")\n")
		// Only when present: an empty "URL:" line would invite an invented link.
		if c.URL != "" {
			fmt.Fprintf(&b, "URL: %s\n", c.URL)
		}
		b.WriteString(strings.TrimSpace(c.Content))
		b.WriteString("\n\n")
	}
	b.WriteString("=== QUESTION ===\n")
	b.WriteString(question)
	return b.String()
}
