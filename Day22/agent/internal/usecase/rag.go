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
//	question → retrieve relevant chunks → combine with question → (prompt)
//
// The caller feeds the returned prompt to ChatUseCase.Execute, completing the
// final "→ LLM" step.
type RAGUseCase struct {
	retriever port.Retriever
}

// NewRAGUseCase wires the pipeline to a retriever.
func NewRAGUseCase(retriever port.Retriever) *RAGUseCase {
	return &RAGUseCase{retriever: retriever}
}

// BuildPrompt retrieves the topK chunks most relevant to question and combines
// them with the question into a single grounded prompt. The retrieved chunks
// are returned as well so the caller can surface the sources to the user.
func (uc *RAGUseCase) BuildPrompt(ctx context.Context, question string, topK int) (string, []domain.RetrievedChunk, error) {
	chunks, err := uc.retriever.Retrieve(ctx, question, topK)
	if err != nil {
		return "", nil, err
	}
	return BuildRAGPrompt(question, chunks), chunks, nil
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
	b.WriteString("Cite the sources you rely on by their [n] marker.\n\n")
	b.WriteString("=== CONTEXT ===\n")
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] source: %s", i+1, c.File)
		if c.Section != "" {
			fmt.Fprintf(&b, " — %s", c.Section)
		}
		fmt.Fprintf(&b, " (similarity %.3f)\n", c.Similarity)
		b.WriteString(strings.TrimSpace(c.Content))
		b.WriteString("\n\n")
	}
	b.WriteString("=== QUESTION ===\n")
	b.WriteString(question)
	return b.String()
}
