package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
)

// APIReranker calls a dedicated rerank endpoint (the Cohere-style
// `POST /rerank` route exposed by OpenRouter, Cohere, Jina, Voyage, …) instead
// of a chat model. Purpose-built rerank models — cohere/rerank-4-fast,
// rerank-v3.5, jina-reranker-* — are cross-encoders served on this route, not on
// /chat/completions, so they cannot be driven by LLMReranker: they take a query
// plus a list of documents and return a relevance score per document directly.
type APIReranker struct {
	baseURL    string // provider base that the "/rerank" path is appended to
	model      string
	apiKey     string
	httpClient *http.Client
}

// NewAPIReranker wires a reranker to a rerank endpoint. baseURL is the provider
// base (e.g. https://openrouter.ai/api/v1); "/rerank" is appended to it.
func NewAPIReranker(baseURL, model, apiKey string) *APIReranker {
	return &APIReranker{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type rerankAPIRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type rerankAPIResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Rerank scores every chunk against the query in a single rerank-API call and
// returns the chunks sorted by descending relevance. top_n is set to the number
// of documents so the endpoint scores them all. An empty input returns nil
// without an API call.
func (r *APIReranker) Rerank(ctx context.Context, query string, chunks []domain.RetrievedChunk) ([]domain.RetrievedChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	docs := make([]string, len(chunks))
	for i, c := range chunks {
		docs[i] = strings.TrimSpace(c.Content)
	}

	payload, err := json.Marshal(rerankAPIRequest{
		Model:     r.model,
		Query:     query,
		Documents: docs,
		TopN:      len(docs),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.baseURL+"/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result rerankAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse rerank response: %w (body: %s)", err, string(body))
	}
	if result.Error != nil {
		return nil, fmt.Errorf("rerank API error: %s", result.Error.Message)
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("empty rerank results")
	}

	byIndex := make(map[int]float64, len(result.Results))
	for _, rr := range result.Results {
		byIndex[rr.Index] = rr.RelevanceScore
	}
	return applyRerankScores(chunks, byIndex), nil
}
