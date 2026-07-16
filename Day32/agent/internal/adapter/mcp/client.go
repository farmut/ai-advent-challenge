// Package mcp implements MCP (Model Context Protocol) transport clients.
// Supported transports: stdio (subprocess) and SSE (HTTP Server-Sent Events).
// Protocol: JSON-RPC 2.0 over the respective transport.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsListResult struct {
	Tools []domain.MCPTool `json:"tools"`
}

// Global monotonic request ID counter.
var idCounter atomic.Int64

func nextID() int64 { return idCounter.Add(1) }

// ---------------------------------------------------------------------------
// Client — port.MCPToolLister implementation
// ---------------------------------------------------------------------------

// Client dispatches ListTools calls to the correct transport based on server type.
type Client struct {
	httpClient *http.Client
}

// NewClient returns an MCP Client with sensible defaults.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// OpenPool creates a persistent Pool for the given server configs.
// It satisfies port.MCPPoolOpener.
func (c *Client) OpenPool(cfgs []domain.MCPServerConfig) (port.MCPPool, []error) {
	return NewPool(cfgs, c)
}

// ListTools connects to the MCP server described by cfg and returns its tool list.
func (c *Client) ListTools(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error) {
	switch cfg.Type {
	case domain.MCPStdio:
		return listToolsStdio(ctx, cfg)
	case domain.MCPSSE:
		return c.listToolsSSE(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q (supported: stdio, sse)", cfg.Type)
	}
}

// CallTool connects to the MCP server, sends a tools/call request, and returns the text result.
func (c *Client) CallTool(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	switch cfg.Type {
	case domain.MCPStdio:
		return callToolStdio(ctx, cfg, name, args)
	case domain.MCPSSE:
		return c.callToolSSE(ctx, cfg, name, args)
	default:
		return "", fmt.Errorf("unsupported MCP transport %q (supported: stdio, sse)", cfg.Type)
	}
}

// ---------------------------------------------------------------------------
// stdio transport — shared session helper
// ---------------------------------------------------------------------------

// runStdioSession starts the MCP subprocess, performs the initialize handshake,
// then calls fn with an encoder and a waitFor helper.  The subprocess is killed
// when fn returns (or the context is cancelled).
func runStdioSession(ctx context.Context, cfg domain.MCPServerConfig, fn func(*json.Encoder, func(int64) (rpcResponse, error)) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), cfg.Env...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting MCP server %q (%s): %w", cfg.Name, cfg.Command, err)
	}
	defer func() {
		stdinPipe.Close()
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	}()

	enc := json.NewEncoder(stdinPipe)

	type numbered struct {
		numID int64
		resp  rpcResponse
	}
	incoming := make(chan numbered, 32)

	go func() {
		defer close(incoming)
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var r rpcResponse
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			if r.ID == nil {
				continue
			}
			var num int64
			switch v := r.ID.(type) {
			case float64:
				num = int64(v)
			case int64:
				num = v
			default:
				continue
			}
			select {
			case incoming <- numbered{numID: num, resp: r}:
			case <-ctx.Done():
				return
			}
		}
	}()

	waitFor := func(id int64) (rpcResponse, error) {
		for {
			select {
			case msg, ok := <-incoming:
				if !ok {
					return rpcResponse{}, fmt.Errorf("server closed connection")
				}
				if msg.numID == id {
					return msg.resp, nil
				}
			case <-ctx.Done():
				return rpcResponse{}, ctx.Err()
			}
		}
	}

	// initialize handshake
	initID := nextID()
	if err := enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      initID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]interface{}{"name": "ai-adv-agent", "version": "0.1.0"},
		},
	}); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}
	if _, err := waitFor(initID); err != nil {
		return fmt.Errorf("initialize response: %w", err)
	}
	_ = enc.Encode(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})

	return fn(enc, waitFor)
}

func listToolsStdio(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error) {
	var tools []domain.MCPTool
	err := runStdioSession(ctx, cfg, func(enc *json.Encoder, waitFor func(int64) (rpcResponse, error)) error {
		toolsID := nextID()
		if err := enc.Encode(rpcRequest{
			JSONRPC: "2.0",
			ID:      toolsID,
			Method:  "tools/list",
			Params:  map[string]interface{}{},
		}); err != nil {
			return fmt.Errorf("send tools/list: %w", err)
		}
		resp, err := waitFor(toolsID)
		if err != nil {
			return fmt.Errorf("tools/list response: %w", err)
		}
		if resp.Error != nil {
			return fmt.Errorf("tools/list error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		var result toolsListResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return fmt.Errorf("parse tools/list result: %w", err)
		}
		tools = result.Tools
		return nil
	})
	return tools, err
}

func callToolStdio(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	var text string
	err := runStdioSession(ctx, cfg, func(enc *json.Encoder, waitFor func(int64) (rpcResponse, error)) error {
		callID := nextID()
		if err := enc.Encode(rpcRequest{
			JSONRPC: "2.0",
			ID:      callID,
			Method:  "tools/call",
			Params:  map[string]interface{}{"name": name, "arguments": args},
		}); err != nil {
			return fmt.Errorf("send tools/call: %w", err)
		}
		resp, err := waitFor(callID)
		if err != nil {
			return fmt.Errorf("tools/call response: %w", err)
		}
		if resp.Error != nil {
			return fmt.Errorf("tools/call error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		var result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return fmt.Errorf("parse tools/call result: %w", err)
		}
		var parts []string
		for _, c := range result.Content {
			if c.Type == "text" {
				parts = append(parts, c.Text)
			}
		}
		text = strings.Join(parts, "\n")
		if result.IsError {
			return fmt.Errorf("tool error: %s", text)
		}
		return nil
	})
	return text, err
}

// ---------------------------------------------------------------------------
// SSE transport
// ---------------------------------------------------------------------------
// The MCP SSE transport works as follows:
//  1. Client connects to GET <url> with Accept: text/event-stream.
//  2. Server emits an "endpoint" SSE event whose data is the session POST URL.
//  3. Client sends JSON-RPC requests via POST to the session URL.
//  4. Server streams JSON-RPC responses back as "message" SSE events.

func (c *Client) listToolsSSE(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Connect to SSE endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	sseResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect to %s: %w", cfg.URL, err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SSE connect: HTTP %d", sseResp.StatusCode)
	}

	// Read SSE events.  We need to collect:
	//   - the "endpoint" event → session POST URL
	//   - subsequent "message" events → JSON-RPC responses
	type sseEvent struct {
		eventType string
		data      string
	}
	events := make(chan sseEvent, 32)

	go func() {
		defer close(events)
		scanner := bufio.NewScanner(sseResp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		var currentType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				currentType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				select {
				case events <- sseEvent{eventType: currentType, data: data}:
				case <-ctx.Done():
					return
				}
			case line == "":
				currentType = ""
			}
		}
	}()

	// Wait for the "endpoint" event
	var sessionURL string
	for sessionURL == "" {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("SSE stream closed before endpoint event")
			}
			if ev.eventType == "endpoint" {
				sessionURL = ev.data
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Resolve relative session URL against the base SSE URL
	if !strings.HasPrefix(sessionURL, "http") {
		base, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("parse base URL: %w", err)
		}
		rel, err := url.Parse(sessionURL)
		if err != nil {
			return nil, fmt.Errorf("parse session URL: %w", err)
		}
		sessionURL = base.ResolveReference(rel).String()
	}

	// Route "message" events to a channel by numeric ID
	type numbered struct {
		numID int64
		resp  rpcResponse
	}
	responses := make(chan numbered, 32)

	go func() {
		defer close(responses)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev.eventType != "message" {
					continue
				}
				var r rpcResponse
				if err := json.Unmarshal([]byte(ev.data), &r); err != nil {
					continue
				}
				if r.ID == nil {
					continue
				}
				var num int64
				switch v := r.ID.(type) {
				case float64:
					num = int64(v)
				case int64:
					num = v
				default:
					continue
				}
				select {
				case responses <- numbered{numID: num, resp: r}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	post := func(msg interface{}) error {
		body, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		postReq.Header.Set("Content-Type", "application/json")
		postResp, err := c.httpClient.Do(postReq)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, postResp.Body) //nolint:errcheck
		postResp.Body.Close()
		return nil
	}

	waitFor := func(id int64) (rpcResponse, error) {
		for {
			select {
			case msg, ok := <-responses:
				if !ok {
					return rpcResponse{}, fmt.Errorf("SSE stream closed")
				}
				if msg.numID == id {
					return msg.resp, nil
				}
			case <-ctx.Done():
				return rpcResponse{}, ctx.Err()
			}
		}
	}

	// initialize
	initID := nextID()
	if err := post(rpcRequest{
		JSONRPC: "2.0",
		ID:      initID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]interface{}{"name": "ai-adv-agent", "version": "0.1.0"},
		},
	}); err != nil {
		return nil, fmt.Errorf("send initialize: %w", err)
	}
	if _, err := waitFor(initID); err != nil {
		return nil, fmt.Errorf("initialize response: %w", err)
	}

	// notifications/initialized
	_ = post(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})

	// tools/list
	toolsID := nextID()
	if err := post(rpcRequest{
		JSONRPC: "2.0",
		ID:      toolsID,
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}
	resp, err := waitFor(toolsID)
	if err != nil {
		return nil, fmt.Errorf("tools/list response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result toolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}
	return result.Tools, nil
}

func (c *Client) callToolSSE(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return "", fmt.Errorf("build SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	sseResp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("SSE connect to %s: %w", cfg.URL, err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SSE connect: HTTP %d", sseResp.StatusCode)
	}

	type sseEvent struct {
		eventType string
		data      string
	}
	events := make(chan sseEvent, 32)

	go func() {
		defer close(events)
		scanner := bufio.NewScanner(sseResp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		var currentType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				currentType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				select {
				case events <- sseEvent{eventType: currentType, data: data}:
				case <-ctx.Done():
					return
				}
			case line == "":
				currentType = ""
			}
		}
	}()

	var sessionURL string
	for sessionURL == "" {
		select {
		case ev, ok := <-events:
			if !ok {
				return "", fmt.Errorf("SSE stream closed before endpoint event")
			}
			if ev.eventType == "endpoint" {
				sessionURL = ev.data
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if !strings.HasPrefix(sessionURL, "http") {
		base, err := url.Parse(cfg.URL)
		if err != nil {
			return "", fmt.Errorf("parse base URL: %w", err)
		}
		rel, err := url.Parse(sessionURL)
		if err != nil {
			return "", fmt.Errorf("parse session URL: %w", err)
		}
		sessionURL = base.ResolveReference(rel).String()
	}

	type numbered struct {
		numID int64
		resp  rpcResponse
	}
	responses := make(chan numbered, 32)

	go func() {
		defer close(responses)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev.eventType != "message" {
					continue
				}
				var r rpcResponse
				if err := json.Unmarshal([]byte(ev.data), &r); err != nil {
					continue
				}
				if r.ID == nil {
					continue
				}
				var num int64
				switch v := r.ID.(type) {
				case float64:
					num = int64(v)
				case int64:
					num = v
				default:
					continue
				}
				select {
				case responses <- numbered{numID: num, resp: r}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	post := func(msg interface{}) error {
		body, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		postReq.Header.Set("Content-Type", "application/json")
		postResp, err := c.httpClient.Do(postReq)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, postResp.Body) //nolint:errcheck
		postResp.Body.Close()
		return nil
	}

	waitFor := func(id int64) (rpcResponse, error) {
		for {
			select {
			case msg, ok := <-responses:
				if !ok {
					return rpcResponse{}, fmt.Errorf("SSE stream closed")
				}
				if msg.numID == id {
					return msg.resp, nil
				}
			case <-ctx.Done():
				return rpcResponse{}, ctx.Err()
			}
		}
	}

	initID := nextID()
	if err := post(rpcRequest{
		JSONRPC: "2.0",
		ID:      initID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]interface{}{"name": "ai-adv-agent", "version": "0.1.0"},
		},
	}); err != nil {
		return "", fmt.Errorf("send initialize: %w", err)
	}
	if _, err := waitFor(initID); err != nil {
		return "", fmt.Errorf("initialize response: %w", err)
	}
	_ = post(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})

	callID := nextID()
	if err := post(rpcRequest{
		JSONRPC: "2.0",
		ID:      callID,
		Method:  "tools/call",
		Params:  map[string]interface{}{"name": name, "arguments": args},
	}); err != nil {
		return "", fmt.Errorf("send tools/call: %w", err)
	}
	resp, err := waitFor(callID)
	if err != nil {
		return "", fmt.Errorf("tools/call response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		return "", fmt.Errorf("parse tools/call result: %w", err)
	}
	var parts []string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if callResult.IsError {
		return "", fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}
