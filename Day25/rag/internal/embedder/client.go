package embedder

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

// Client calls an OpenAI-compatible embeddings endpoint.
// Works with any local runtime that exposes POST /v1/embeddings
// (Ollama ≥ 0.1.32, LM Studio, llama.cpp server, etc.).
type Client struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

// New creates an embeddings client.
// baseURL example: "http://localhost:11434" (Ollama) or "http://localhost:1234" (LM Studio).
func New(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// --- wire types (OpenAI-compatible) -----------------------------------------

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed sends text to the embeddings endpoint and returns the vector.
func (c *Client) Embed(ctx context.Context, text string) ([]float64, error) {
	payload, err := json.Marshal(embedRequest{Model: c.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
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
