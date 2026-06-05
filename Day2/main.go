package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL     = "https://api.openai.com/v1"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenAIModel       = "gpt-4o"
)

const (
	providerOpenAI     = "openai"
	providerOpenRouter = "openrouter"
)

type role string

const (
	roleUser role = "user"
)

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type Message struct {
	Role    role   `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stop     []string  `json:"stop,omitempty"`

	MaxTokens           *int `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
}

type chatChoice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   usage        `json:"usage"`
}

type errorPayload struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

var formatInstructions = map[string]string{
	"markdown": "Format your response using Markdown.",
	"json":     "Respond in JSON format.",
}

func main() {
	query := flag.String("query", "", "The query string to send to the LLM (required)")
	format := flag.String("format", "markdown", "Response format: markdown or json (adds instruction to prompt)")
	maxTokens := flag.Int("max-tokens", 0, "Maximum number of tokens in response (0 = no limit)")
	debug := flag.Bool("debug", false, "Print request payload and token usage to stderr")
	formatHint := flag.String("format-hint", "", "Custom formatting instruction (overrides --format)")
	var stopSequences stringSlice
	flag.Var(&stopSequences, "stop", "Stop sequence (can be specified multiple times)")
	flag.Parse()

	if *query == "" {
		fmt.Fprintf(os.Stderr, "Error: --query flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	var instruction string
	if *formatHint != "" {
		if *format != "markdown" {
			fmt.Fprintf(os.Stderr, "Warning: --format-hint overrides --format, ignoring --format=%s\n", *format)
		}
		instruction = *formatHint
	} else {
		var formatOk bool
		instruction, formatOk = formatInstructions[*format]
		if !formatOk {
			fmt.Fprintf(os.Stderr, "Error: --format must be 'markdown' or 'json'\n")
			os.Exit(1)
		}
	}

	providerStr := os.Getenv("LLM_PROVIDER")
	if providerStr == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}

	if providerStr != providerOpenAI && providerStr != providerOpenRouter {
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER '%s'\nSupported: openai, openrouter\n", providerStr)
		os.Exit(1)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_API_KEY environment variable is required\n")
		os.Exit(1)
	}

	model := os.Getenv("LLM_MODEL")
	baseURL := os.Getenv("LLM_BASE_URL")

	if baseURL == "" {
		if providerStr == providerOpenRouter {
			baseURL = defaultOpenRouterBaseURL
		} else {
			baseURL = defaultOpenAIBaseURL
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if providerStr == providerOpenAI && model == "" {
		model = defaultOpenAIModel
	}

	fullQuery := *query + "\n\n" + instruction

	messages := []Message{
		{
			Role:    roleUser,
			Content: fullQuery,
		},
	}

	chatResp, err := sendChatRequest(providerStr, apiKey, baseURL, model, messages, *maxTokens, []string(stopSequences), *debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying LLM: %v\n", err)
		os.Exit(1)
	}

	if *debug {
		finish := ""
		if len(chatResp.Choices) > 0 {
			finish = chatResp.Choices[0].FinishReason
		}
		fmt.Fprintf(os.Stderr, "\n[usage] prompt_tokens=%d, completion_tokens=%d, total_tokens=%d, finish_reason=%s\n\n",
			chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens, finish)
	}

	fmt.Println(chatResp.Choices[0].Message.Content)
}

func sendChatRequest(provider, apiKey, baseURL, model string, messages []Message, maxTokens int, stop []string, debug bool) (*chatResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	reqBody := chatRequest{
		Model:    model,
		Messages: messages,
	}

	if maxTokens > 0 {
		val := maxTokens
		reqBody.MaxCompletionTokens = &val
		reqBody.MaxTokens = &val
	}

	if len(stop) > 0 {
		reqBody.Stop = stop
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if debug {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "", "  ") == nil {
			fmt.Fprintf(os.Stderr, "[debug] POST %s/chat/completions\n%s\n\n", baseURL, pretty.String())
		}
	}

	url := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	if provider == providerOpenRouter {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/farmut/ai-advent-challenge")
		httpReq.Header.Set("X-Title", "ai-adv-agent")
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp errorPayload
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("%s API error (status %d): %s", provider, resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("%s API error (status %d): %s", provider, resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from %s", provider)
	}

	return &chatResp, nil
}
