package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// LLMReranker is a cross-encoder-style reranker built on top of a chat LLM.
// Embedding retrieval (stage 1) ranks chunks by vector similarity, which is fast
// but judges query and chunk in isolation. This reranker (stage 2) shows the LLM
// the query alongside every candidate chunk at once and asks it to score each
// chunk's relevance from 0 to 1, letting it reason about the pair jointly.
//
// It reuses the agent's existing port.LLMClient, so no extra provider or model
// deployment is required — the rerank call goes to the same endpoint as the main
// completion, just with a small, deterministic (temperature 0) scoring prompt.
type LLMReranker struct {
	client port.LLMClient
	model  string
}

// NewLLMReranker wires a reranker to an LLM client and the model used for scoring.
func NewLLMReranker(client port.LLMClient, model string) *LLMReranker {
	return &LLMReranker{client: client, model: model}
}

const rerankSystemPrompt = `You are a search result reranker. You are given a user QUERY and a numbered list of candidate passages.
For each passage, judge how well it helps answer the QUERY and assign a relevance score between 0.0 (irrelevant) and 1.0 (directly answers the query).
Respond with ONLY a JSON array of objects, one per passage, in the form:
[{"index": 1, "score": 0.0}, {"index": 2, "score": 0.0}]
Do not include any prose, explanation, or markdown fences.`

type rerankScore struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Rerank scores every chunk against the query in a single LLM call and returns
// the chunks sorted by descending rerank score. Any chunk the model omits keeps
// a fallback score equal to its retrieval similarity, so a partial response
// never silently drops candidates. An empty input returns nil without an API call.
func (r *LLMReranker) Rerank(ctx context.Context, query string, chunks []domain.RetrievedChunk) ([]domain.RetrievedChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "QUERY: %s\n\nPASSAGES:\n", query)
	for i, c := range chunks {
		label := c.File
		if c.Section != "" {
			label += " — " + c.Section
		}
		fmt.Fprintf(&b, "[%d] (%s)\n%s\n\n", i+1, label, strings.TrimSpace(c.Content))
	}

	resp, err := r.client.Chat(ctx, port.LLMRequest{
		Model: r.model,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: rerankSystemPrompt},
			{Role: domain.RoleUser, Content: b.String()},
		},
		Temperature: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("rerank LLM call: %w", err)
	}

	scores, err := parseRerankScores(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("rerank parse: %w (response: %q)", err, resp.Content)
	}

	// The scoring prompt uses 1-based passage numbers; convert to the 0-based
	// chunk indices applyRerankScores expects.
	byIndex := make(map[int]float64, len(scores))
	for _, s := range scores {
		byIndex[s.Index-1] = s.Score
	}
	return applyRerankScores(chunks, byIndex), nil
}

// applyRerankScores overlays reranker scores (keyed by 0-based chunk index) onto
// a copy of chunks and returns them sorted best-first. A chunk the reranker did
// not score keeps its retrieval similarity as the rerank score, so a partial
// response never silently drops a candidate to the bottom. Scores are clamped
// to 0..1. Both the LLM and the native-API reranker share this logic.
func applyRerankScores(chunks []domain.RetrievedChunk, scores map[int]float64) []domain.RetrievedChunk {
	out := make([]domain.RetrievedChunk, len(chunks))
	for i, c := range chunks {
		c.RerankScore = c.Similarity
		if s, ok := scores[i]; ok {
			c.RerankScore = clamp01(s)
		}
		out[i] = c
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RerankScore > out[j].RerankScore
	})
	return out
}

// jsonArrayRe extracts the first JSON array from a model response, tolerating
// stray prose or ```json fences some models wrap the answer in.
var jsonArrayRe = regexp.MustCompile(`(?s)\[.*\]`)

func parseRerankScores(content string) ([]rerankScore, error) {
	raw := jsonArrayRe.FindString(content)
	if raw == "" {
		return nil, fmt.Errorf("no JSON array found")
	}
	var scores []rerankScore
	if err := json.Unmarshal([]byte(raw), &scores); err != nil {
		return nil, err
	}
	if len(scores) == 0 {
		return nil, fmt.Errorf("empty score array")
	}
	return scores, nil
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
