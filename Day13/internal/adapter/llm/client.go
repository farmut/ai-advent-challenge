package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

const (
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
)

// Config holds provider-level settings for the HTTP LLM client.
type Config struct {
	Provider string
	APIKey   string
	BaseURL  string
}

// Client is the HTTP implementation of port.LLMClient for OpenAI-compatible APIs.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient creates a new HTTP LLM client with the given provider configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// wire types for the OpenAI chat completion API
type apiRequest struct {
	Model               string           `json:"model"`
	Messages            []domain.Message `json:"messages"`
	Stop                []string         `json:"stop,omitempty"`
	MaxTokens           *int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
}

type apiChoice struct {
	Message      domain.Message `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type apiResponse struct {
	Choices []apiChoice  `json:"choices"`
	Usage   domain.Usage `json:"usage"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Chat sends a chat completion request and returns the structured response.
func (c *Client) Chat(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	body := apiRequest{Model: req.Model, Messages: req.Messages}
	if req.MaxTokens > 0 {
		v := req.MaxTokens
		body.MaxTokens = &v
		body.MaxCompletionTokens = &v
	}
	if len(req.Stop) > 0 {
		body.Stop = req.Stop
	}
	if req.Temperature >= 0 {
		t := req.Temperature
		body.Temperature = &t
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	if req.Debug {
		var pretty bytes.Buffer
		if json.Indent(&pretty, payload, "", "  ") == nil {
			fmt.Fprintf(os.Stderr, "[debug] POST %s/chat/completions\n%s\n\n", c.cfg.BaseURL, pretty.String())
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if c.cfg.Provider == ProviderOpenRouter {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/farmut/ai-advent-challenge")
		httpReq.Header.Set("X-Title", "ai-adv-agent")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return port.LLMResponse{}, fmt.Errorf("%s API error (%d): %s", c.cfg.Provider, resp.StatusCode, apiErr.Error.Message)
		}
		return port.LLMResponse{}, fmt.Errorf("%s API error (%d): %s", c.cfg.Provider, resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return port.LLMResponse{}, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}
	if len(apiResp.Choices) == 0 {
		return port.LLMResponse{}, fmt.Errorf("no choices in %s response", c.cfg.Provider)
	}

	if req.Debug {
		fmt.Fprintf(os.Stderr, "[debug] finish_reason=%s elapsed=%s\n",
			apiResp.Choices[0].FinishReason, elapsed.Round(time.Millisecond))
	}

	return port.LLMResponse{
		Content:      apiResp.Choices[0].Message.Content,
		Usage:        apiResp.Usage,
		FinishReason: apiResp.Choices[0].FinishReason,
		Elapsed:      elapsed,
	}, nil
}
