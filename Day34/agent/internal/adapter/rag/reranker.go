package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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

// rerankSystemPrompt asks for one plain "N score" line per passage rather than
// JSON. Purpose-built rerankers served through a chat endpoint (e.g. Ollama's
// MedAIBase/Qwen3-VL-Reranker) are not instruction-tuned and mangle JSON, but
// they reliably emit the leading numbered score lines; parseRerankScores reads
// those and ignores any trailing prose (which MaxTokens also clips).
const rerankSystemPrompt = `You score how relevant each passage is to the QUERY.
Output EXACTLY one line per passage and nothing else.
Each line is the passage number, a space, then a relevance score from 0.0 (irrelevant) to 1.0 (directly answers the query).
Example for three passages:
1 0.9
2 0.0
3 0.4
No words, no explanation, no reasoning, no markdown — only the numbered score lines.`

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

	// One short "N score" line per passage is all we need; cap the budget so a
	// non-instruction-tuned reranker that starts rambling after the scores is cut
	// off instead of burning tokens (the parser already ignores trailing prose).
	resp, err := r.client.Chat(ctx, port.LLMRequest{
		Model: r.model,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: rerankSystemPrompt},
			{Role: domain.RoleUser, Content: b.String()},
		},
		Temperature: 0,
		MaxTokens:   len(chunks)*8 + 24,
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

// jsonArrayRe extracts the first JSON array of objects from a model response,
// tolerating stray prose or ```json fences some models wrap the answer in. It
// requires object braces so a bare "[1]" in prose is not mistaken for a score
// array (which would fail to unmarshal and mask the line-format fallback below).
var jsonArrayRe = regexp.MustCompile(`(?s)\[\s*\{.*\}\s*\]`)

// lineScoreRe matches completion-style rerankers that emit one "N. score" line
// per passage instead of JSON (e.g. MedAIBase/Qwen3-VL-Reranker: "1. 0.5\n2. 0.0").
// The score is constrained to a 0..1 probability so prose numbers are not caught.
var lineScoreRe = regexp.MustCompile(`(?m)^\s*\[?(\d+)\]?\s*[.):]?\s+([01](?:\.\d+)?|\.\d+)\b`)

// parseRerankScores reads per-passage relevance scores from a model response.
// It prefers a JSON array of {index, score} objects; if none is present or it
// does not unmarshal, it falls back to reading "N. score" lines. Only when both
// yield nothing does it error, so a purpose-built reranker that ignores the JSON
// instruction still drives the pipeline.
func parseRerankScores(content string) ([]rerankScore, error) {
	if raw := jsonArrayRe.FindString(content); raw != "" {
		var scores []rerankScore
		if err := json.Unmarshal([]byte(raw), &scores); err == nil && len(scores) > 0 {
			return scores, nil
		}
	}
	if scores := parseLineScores(content); len(scores) > 0 {
		return scores, nil
	}
	return nil, fmt.Errorf("no rerank scores found (neither JSON array nor 'N. score' lines)")
}

// parseLineScores extracts (index, score) pairs from "N. score" lines.
func parseLineScores(content string) []rerankScore {
	matches := lineScoreRe.FindAllStringSubmatch(content, -1)
	scores := make([]rerankScore, 0, len(matches))
	for _, m := range matches {
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		score, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		scores = append(scores, rerankScore{Index: idx, Score: score})
	}
	return scores
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
