package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

const (
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderGigaChat   = "gigachat"

	// GigaChat OAuth endpoint (Sber auth server).
	gigaChatOAuthURL = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"
	// DefaultGigaChatBaseURL is the primary API address for individuals and legal
	// entities paying via Sber's offer-contract (GIGACHAT_API_PERS scope).
	DefaultGigaChatBaseURL = "https://gigachat.devices.sberbank.ru/api/v1"
	// Scope for individual accounts (физические лица).
	GigaChatScopePers = "GIGACHAT_API_PERS"

	// tokenRefreshBuffer is how early (before expiry) we proactively refresh the token.
	tokenRefreshBuffer = 60 * time.Second
)

// Config holds provider-level settings for the HTTP LLM client.
type Config struct {
	Provider      string
	APIKey        string
	BaseURL       string
	GigaChatScope string // default: GigaChatScopePers; only used for ProviderGigaChat

	// CACertFile is an optional path to a PEM certificate to trust in addition to
	// the system roots. Used to reach an OpenAI-compatible endpoint fronted by a
	// self-signed cert (e.g. a private LiteLLM proxy behind nginx). The cert is
	// added to the trust pool — TLS verification stays ON (no InsecureSkipVerify).
	CACertFile string

	// Generation defaults applied at model-init time (NewClient). They belong to
	// the model instance, not to individual calls: a per-call LLMRequest only
	// overrides them when it sets them explicitly (Temperature >= 0, MaxTokens > 0).
	Temperature   *float64 // nil = provider default
	MaxTokens     int      // 0 = no completion limit
	ContextWindow int      // 0 = no client-side prompt cap; else trim oldest non-system messages to fit
}

// Client is the HTTP implementation of port.LLMClient for OpenAI-compatible APIs
// and the GigaChat API (Sber).
type Client struct {
	cfg        Config
	httpClient *http.Client // standard TLS client (OpenAI, OpenRouter)

	// GigaChat-specific state
	gcClient *http.Client // TLS-skip client required by Sber's self-signed CA
	gcMu     sync.Mutex
	gcToken  string
	gcExpiry time.Time
}

// NewClient creates a new HTTP LLM client with the given provider configuration.
func NewClient(cfg Config) *Client {
	c := &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 300 * time.Second},
	}
	// Trust an extra self-signed CA/cert if provided (e.g. a private LiteLLM
	// proxy behind nginx). Verification stays enabled; we just widen the pool.
	if cfg.CACertFile != "" {
		if pemBytes, err := os.ReadFile(cfg.CACertFile); err == nil {
			pool, perr := x509.SystemCertPool()
			if perr != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if pool.AppendCertsFromPEM(pemBytes) {
				c.httpClient.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
				}
			} else {
				fmt.Fprintf(os.Stderr, "[warn] CA cert %q contained no valid PEM certificates; using system roots only\n", cfg.CACertFile)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[warn] cannot read CA cert %q: %v; using system roots only\n", cfg.CACertFile, err)
		}
	}
	if cfg.Provider == ProviderGigaChat {
		// Sber uses their own root CA which is not in the standard Go trust store.
		// InsecureSkipVerify is intentional here for GigaChat connectivity.
		c.gcClient = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	}
	return c
}

// ---------------------------------------------------------------------------
// OpenAI / OpenRouter wire types
// ---------------------------------------------------------------------------

type toolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type toolDef struct {
	Type     string       `json:"type"` // always "function"
	Function toolFunction `json:"function"`
}

type apiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function apiToolCallFunction `json:"function"`
}

type apiRequest struct {
	Model               string           `json:"model"`
	Messages            []domain.Message `json:"messages"`
	Stop                []string         `json:"stop,omitempty"`
	MaxTokens           *int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
	Tools               []toolDef        `json:"tools,omitempty"`
}

type apiMessage struct {
	Role       domain.Role   `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type apiChoice struct {
	Message      apiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
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

// ---------------------------------------------------------------------------
// GigaChat wire types
// ---------------------------------------------------------------------------

// gcFunction mirrors GigaChat's "functions" request field.
// Unlike OpenAI's "tools" array, GigaChat uses a flat list of function descriptors.
type gcFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters"`
}

// gcFunctionCall is the function_call object in GigaChat responses.
// Note: Arguments is a JSON *object* (already parsed), not a JSON string as in OpenAI.
type gcFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// gcMessage is used both in requests and responses for the GigaChat API.
type gcMessage struct {
	Role         string          `json:"role"`
	Content      string          `json:"content,omitempty"`
	Name         string          `json:"name,omitempty"`          // function role: tool name
	FunctionCall *gcFunctionCall `json:"function_call,omitempty"` // assistant → function call
}

type gcRequest struct {
	Model        string       `json:"model"`
	Messages     []gcMessage  `json:"messages"`
	Functions    []gcFunction `json:"functions,omitempty"`
	FunctionCall string       `json:"function_call,omitempty"` // "auto" when functions present
	Stop         []string     `json:"stop,omitempty"`
	MaxTokens    *int         `json:"max_tokens,omitempty"`
	Temperature  *float64     `json:"temperature,omitempty"`
}

type gcChoice struct {
	Message      gcMessage `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

type gcResponse struct {
	Choices []gcChoice   `json:"choices"`
	Usage   domain.Usage `json:"usage"`
}

// ---------------------------------------------------------------------------
// Chat — main entry point
// ---------------------------------------------------------------------------

// Chat sends a chat completion request and returns the structured response.
func (c *Client) Chat(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	if c.cfg.Provider == ProviderGigaChat {
		return c.chatGigaChat(ctx, req)
	}
	return c.chatOpenAI(ctx, req)
}

// resolveGen merges the per-call request with the model-init generation defaults
// from Config and applies the context-window cap. Values set on the request win;
// otherwise the client's configured default (temperature / max-tokens) is used.
func (c *Client) resolveGen(req port.LLMRequest) (messages []domain.Message, maxTokens int, temperature float64) {
	maxTokens = req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.cfg.MaxTokens
	}
	temperature = req.Temperature
	if temperature < 0 && c.cfg.Temperature != nil {
		temperature = *c.cfg.Temperature
	}
	messages = req.Messages
	if c.cfg.ContextWindow > 0 {
		messages = capContextWindow(messages, c.cfg.ContextWindow, maxTokens)
	}
	return messages, maxTokens, temperature
}

// capContextWindow drops the oldest non-system messages until the estimated
// prompt fits within (window - reserve) tokens, where reserve is the completion
// budget. System messages are always kept, as is the most recent message.
func capContextWindow(messages []domain.Message, window, reserve int) []domain.Message {
	budget := window - reserve
	if budget <= 0 {
		budget = window
	}
	fits := func(ms []domain.Message) bool {
		total := 0
		for _, m := range ms {
			total += domain.EstimateTokens(m.Content)
		}
		return total <= budget
	}
	if fits(messages) {
		return messages
	}
	var system, rest []domain.Message
	for _, m := range messages {
		if m.Role == domain.RoleSystem {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}
	for len(rest) > 1 {
		combined := append(append([]domain.Message{}, system...), rest...)
		if fits(combined) {
			return combined
		}
		rest = rest[1:] // drop the oldest non-system message
	}
	return append(append([]domain.Message{}, system...), rest...)
}

// ---------------------------------------------------------------------------
// OpenAI / OpenRouter path
// ---------------------------------------------------------------------------

func (c *Client) chatOpenAI(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	messages, maxTokens, temperature := c.resolveGen(req)
	body := apiRequest{Model: req.Model, Messages: messages}
	if maxTokens > 0 {
		v := maxTokens
		body.MaxTokens = &v
		body.MaxCompletionTokens = &v
	}
	if len(req.Stop) > 0 {
		body.Stop = req.Stop
	}
	if temperature >= 0 {
		t := temperature
		body.Temperature = &t
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, toolDef{
			Type: "function",
			Function: toolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
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

	choice := apiResp.Choices[0]

	if req.Debug {
		fmt.Fprintf(os.Stderr, "[debug] finish_reason=%s elapsed=%s\n",
			choice.FinishReason, elapsed.Round(time.Millisecond))
	}

	var toolCalls []domain.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, domain.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return port.LLMResponse{
		Content:      choice.Message.Content,
		Usage:        apiResp.Usage,
		FinishReason: choice.FinishReason,
		Elapsed:      elapsed,
		ToolCalls:    toolCalls,
	}, nil
}

// ---------------------------------------------------------------------------
// GigaChat path
// ---------------------------------------------------------------------------

// gcTextToolInstruction is appended to the system message when tools are available.
// GigaChat's native function_call mechanism is unreliable in the base model, so we
// use a structured text protocol (ReAct-style) instead.
const gcTextToolInstruction = `
## Tool Calling Protocol
To invoke a tool output ONLY a single raw JSON line — no markdown, no explanation, nothing else:
{"action":"TOOL_NAME","args":{"param":"value"}}

Examples:
  {"action":"store_get_inventory","args":{}}
  {"action":"pet_get_by_id","args":{"petId":1}}

After the tool result arrives, produce the final answer in natural language.
Call the tool IMMEDIATELY — do not describe what you will do first.`

// parseGCAction extracts a text-based tool call from a GigaChat response.
// Returns name, argsJSON, true when a {"action":...,"args":...} JSON is found.
func parseGCAction(content string) (name, argsJSON string, ok bool) {
	s := strings.TrimSpace(content)
	// Strip markdown code fences that some models add.
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			if idx := strings.Index(s, "```"); idx >= 0 {
				s = s[:idx]
			}
			s = strings.TrimSpace(s)
			break
		}
	}
	i := strings.IndexByte(s, '{')
	j := strings.LastIndexByte(s, '}')
	if i < 0 || j <= i {
		return "", "", false
	}
	var call struct {
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(s[i:j+1]), &call); err != nil || call.Action == "" {
		return "", "", false
	}
	args := string(call.Args)
	if args == "" || args == "null" {
		args = "{}"
	}
	return call.Action, args, true
}

// chatGigaChat implements the GigaChat API flow:
//  1. Obtain/refresh a 30-minute OAuth access token (GIGACHAT_API_PERS scope).
//  2. When tools are present, use text-based ReAct tool calling (more reliable than
//     native functions[] for the base GigaChat model).
//  3. Parse the response: first try text {"action":...} format, then native function_call.
func (c *Client) chatGigaChat(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	token, err := c.gigaChatToken(ctx)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("gigachat auth: %w", err)
	}

	// Build message list; inject tool instructions into system message when tools present.
	trimmed, gcMaxTokens, gcTemperature := c.resolveGen(req)
	msgs := toGCMessages(trimmed)
	if len(req.Tools) > 0 {
		var toolList strings.Builder
		toolList.WriteString("\nAvailable tools:\n")
		for _, t := range req.Tools {
			toolList.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
		}
		toolList.WriteString(gcTextToolInstruction)
		injected := false
		for i, m := range msgs {
			if m.Role == "system" {
				msgs[i].Content += toolList.String()
				injected = true
				break
			}
		}
		if !injected {
			msgs = append([]gcMessage{{Role: "system", Content: toolList.String()}}, msgs...)
		}
	}

	gcReq := gcRequest{
		Model:    req.Model,
		Messages: msgs,
	}
	if gcMaxTokens > 0 {
		v := gcMaxTokens
		gcReq.MaxTokens = &v
	}
	if len(req.Stop) > 0 {
		gcReq.Stop = req.Stop
	}
	if gcTemperature >= 0 {
		t := gcTemperature
		gcReq.Temperature = &t
	}
	// Also send native functions[] as a hint — some Pro/Max models use it.
	// sanitizeSchemaForGigaChat adds "properties":{} to bare object nodes.
	for _, t := range req.Tools {
		gcReq.Functions = append(gcReq.Functions, gcFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sanitizeSchemaForGigaChat(t.InputSchema),
		})
	}
	if len(gcReq.Functions) > 0 {
		gcReq.FunctionCall = "auto"
	}

	payload, err := json.Marshal(gcReq)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("marshal gigachat request: %w", err)
	}

	if req.Debug {
		var pretty bytes.Buffer
		if json.Indent(&pretty, payload, "", "  ") == nil {
			fmt.Fprintf(os.Stderr, "[debug] GigaChat POST %s/chat/completions\n%s\n\n",
				c.cfg.BaseURL, pretty.String())
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("create gigachat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", "ai-adv-agent/0.1")

	start := time.Now()
	resp, err := c.gcClient.Do(httpReq)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("gigachat http: %w", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return port.LLMResponse{}, fmt.Errorf("read gigachat response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return port.LLMResponse{}, fmt.Errorf("gigachat API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return port.LLMResponse{}, fmt.Errorf("gigachat API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var gcResp gcResponse
	if err := json.Unmarshal(respBody, &gcResp); err != nil {
		return port.LLMResponse{}, fmt.Errorf("parse gigachat response: %w (body: %s)", err, string(respBody))
	}
	if len(gcResp.Choices) == 0 {
		return port.LLMResponse{}, fmt.Errorf("no choices in gigachat response")
	}

	choice := gcResp.Choices[0]

	fmt.Fprintf(os.Stderr, "[gigachat] finish_reason=%s elapsed=%s\n",
		choice.FinishReason, elapsed.Round(time.Millisecond))
	if req.Debug {
		fmt.Fprintf(os.Stderr, "[debug] gigachat content=%q function_call=%v\n",
			choice.Message.Content, choice.Message.FunctionCall != nil)
	}

	var toolCalls []domain.ToolCall

	// 1. Text-based ReAct tool call: parse {"action":"name","args":{...}} from content.
	//    This is the primary mechanism for GigaChat (base model ignores native functions[]).
	if len(req.Tools) > 0 {
		if name, argsJSON, ok := parseGCAction(choice.Message.Content); ok {
			fmt.Fprintf(os.Stderr, "[gigachat] text tool_call: %s(%s)\n", name, argsJSON)
			toolCalls = []domain.ToolCall{{
				ID:        "gc-" + newUUID()[:8],
				Name:      name,
				Arguments: argsJSON,
			}}
		}
	}

	// 2. Native function_call fallback (GigaChat-Pro / GigaChat-Max may use it).
	if len(toolCalls) == 0 && choice.Message.FunctionCall != nil {
		fc := choice.Message.FunctionCall
		argsStr := string(fc.Arguments)
		if len(argsStr) == 0 {
			argsStr = "{}"
		}
		fmt.Fprintf(os.Stderr, "[gigachat] native function_call: %s(%s)\n", fc.Name, argsStr)
		toolCalls = []domain.ToolCall{{
			ID:        "gc-" + newUUID()[:8],
			Name:      fc.Name,
			Arguments: argsStr,
		}}
	}

	return port.LLMResponse{
		Content:      choice.Message.Content,
		Usage:        gcResp.Usage,
		FinishReason: choice.FinishReason,
		Elapsed:      elapsed,
		ToolCalls:    toolCalls,
	}, nil
}

// gigaChatToken returns a valid access token, refreshing it when it is absent
// or about to expire.  The token cache is protected by a mutex so that
// concurrent Chat() calls share a single OAuth round-trip.
func (c *Client) gigaChatToken(ctx context.Context) (string, error) {
	c.gcMu.Lock()
	defer c.gcMu.Unlock()

	if c.gcToken != "" && time.Now().Add(tokenRefreshBuffer).Before(c.gcExpiry) {
		return c.gcToken, nil
	}

	scope := c.cfg.GigaChatScope
	if scope == "" {
		scope = GigaChatScopePers
	}

	body := url.Values{"scope": {scope}}.Encode()
	oauthReq, err := http.NewRequestWithContext(ctx, http.MethodPost, gigaChatOAuthURL,
		strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build oauth request: %w", err)
	}
	oauthReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oauthReq.Header.Set("Accept", "application/json")
	oauthReq.Header.Set("RqUID", newUUID())
	oauthReq.Header.Set("Authorization", "Basic "+c.cfg.APIKey)

	resp, err := c.gcClient.Do(oauthReq)
	if err != nil {
		return "", fmt.Errorf("gigachat oauth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gigachat oauth HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"` // unix seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse oauth response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("gigachat oauth: empty access_token in response")
	}

	c.gcToken = result.AccessToken
	c.gcExpiry = time.Unix(result.ExpiresAt, 0)
	return c.gcToken, nil
}

// toGCMessages converts domain.Message slice to the GigaChat message format.
//
// Tool-call history is serialised using the text-based ReAct convention so that
// GigaChat can follow the conversation even when native function_call was not used:
//   - role "assistant" with ToolCalls → assistant content with the JSON action text
//   - role "tool"                     → user message "Tool result: <name>\n<content>"
//
// Native function_call style is preserved as a fallback for Pro/Max models that
// support it, by also emitting function_call in the assistant turn.
func toGCMessages(msgs []domain.Message) []gcMessage {
	out := make([]gcMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case domain.RoleTool:
			// Inject tool result as a user message — compatible with both text-based
			// and native function_call history without requiring a preceding function_call.
			out = append(out, gcMessage{
				Role:    "user",
				Content: fmt.Sprintf("[Tool result: %s]\n%s", m.Name, m.Content),
			})
		case domain.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tc := m.ToolCalls[0]
				// Represent the tool call as the JSON text the model was instructed to produce.
				argsJSON := tc.Function.Arguments
				if argsJSON == "" {
					argsJSON = "{}"
				}
				content := fmt.Sprintf(`{"action":%q,"args":%s}`, tc.Function.Name, argsJSON)
				out = append(out, gcMessage{Role: "assistant", Content: content})
			} else {
				out = append(out, gcMessage{Role: "assistant", Content: m.Content})
			}
		default:
			out = append(out, gcMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	return out
}

// sanitizeSchemaForGigaChat walks a JSON Schema tree and ensures every node
// with "type":"object" has a "properties" key.  GigaChat rejects object nodes
// that omit "properties" entirely, even though the JSON Schema spec allows it.
func sanitizeSchemaForGigaChat(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			out[k] = sanitizeSchemaForGigaChat(child)
		}
		if typ, ok := out["type"].(string); ok && typ == "object" {
			if _, has := out["properties"]; !has {
				out["properties"] = map[string]interface{}{}
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, child := range val {
			out[i] = sanitizeSchemaForGigaChat(child)
		}
		return out
	default:
		return v
	}
}

// newUUID generates a random UUID v4 string.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
