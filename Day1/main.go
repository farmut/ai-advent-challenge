package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/sashabaranov/go-openai"
)

type provider string

const (
	providerOpenAI     provider = "openai"
	providerOpenRouter provider = "openrouter"
)

func main() {
	query := flag.String("query", "", "The query string to send to the LLM (required)")
	flag.Parse()

	if *query == "" {
		fmt.Fprintf(os.Stderr, "Error: --query flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	providerStr := os.Getenv("LLM_PROVIDER")
	if providerStr == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_PROVIDER environment variable is required\n")
		os.Exit(1)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: LLM_API_KEY environment variable is required\n")
		os.Exit(1)
	}

	model := os.Getenv("LLM_MODEL")
	baseURL := os.Getenv("LLM_BASE_URL")

	var response string
	var err error

	switch provider(providerStr) {
	case providerOpenAI:
		if model == "" {
			model = openai.GPT4o
		}
		response, err = queryOpenAI(apiKey, baseURL, model, *query)
	case providerOpenRouter:
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		response, err = queryOpenRouter(apiKey, baseURL, model, *query)
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported LLM_PROVIDER '%s'\nSupported: openai, openrouter\n", providerStr)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying LLM: %v\n", err)
		os.Exit(1)
	}

	rendered, err := renderMarkdown(response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to render markdown, showing raw response: %v\n", err)
		fmt.Println(response)
		return
	}

	fmt.Print(rendered)
}

func renderMarkdown(content string) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(content)
}

func queryOpenAI(apiKey, baseURL, model, query string) (string, error) {
	config := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: query,
				},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("openai API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

type openRouterTransport struct {
	http.RoundTripper
}

func (t *openRouterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("HTTP-Referer", "https://github.com/amachulin/ai-advent-challenge")
	req.Header.Set("X-Title", "ai-adv-agent")
	return t.RoundTripper.RoundTrip(req)
}

func queryOpenRouter(apiKey, baseURL, model, query string) (string, error) {
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL
	config.HTTPClient = &http.Client{
		Transport: &openRouterTransport{
			RoundTripper: http.DefaultTransport,
		},
		Timeout: 120 * time.Second,
	}

	client := openai.NewClientWithConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: query,
				},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("openrouter API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenRouter")
	}

	return resp.Choices[0].Message.Content, nil
}
