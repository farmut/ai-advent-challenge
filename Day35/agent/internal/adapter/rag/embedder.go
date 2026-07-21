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
)

// embedder calls an OpenAI-compatible embeddings endpoint (POST /v1/embeddings).
// It works with any local runtime that exposes that route — Ollama, LM Studio,
// llama.cpp server — as well as the hosted OpenAI API. It must use the same
// model that produced the vectors stored in the index, otherwise similarity
// scores are meaningless.
type embedder struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func newEmbedder(baseURL, model, apiKey string) *embedder {
	return &embedder{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// embed returns the embedding vector for text.
func (e *embedder) embed(ctx context.Context, text string) ([]float64, error) {
	payload, err := json.Marshal(embedRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result embedResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse embed response: %w (body: %s)", err, string(body))
	}
	if result.Error != nil {
		return nil, fmt.Errorf("embeddings API error: %s", result.Error.Message)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("empty embeddings response")
	}
	return result.Data[0].Embedding, nil
}
