package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// numberedMsg pairs a JSON-RPC response with the integer request ID used to match it.
type numberedMsg struct {
	numID int64
	resp  rpcResponse
}

// StdioSession is a persistent MCP stdio subprocess.
// It performs the initialize handshake once, then serves ListTools and CallTool
// calls over the same long-lived stdin/stdout pipe. Concurrent calls are
// serialised by mu; the session is valid until Close is called.
type StdioSession struct {
	cfg      domain.MCPServerConfig
	cmd      *exec.Cmd
	enc      *json.Encoder
	incoming chan numberedMsg
	cancel   context.CancelFunc
	mu       sync.Mutex
}

func newStdioSession(cfg domain.MCPServerConfig) (*StdioSession, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), cfg.Env...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %q (%s): %w", cfg.Name, cfg.Command, err)
	}

	incoming := make(chan numberedMsg, 64)
	s := &StdioSession{
		cfg:      cfg,
		cmd:      cmd,
		enc:      json.NewEncoder(stdinPipe),
		incoming: incoming,
		cancel:   cancel,
	}

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
			case incoming <- numberedMsg{numID: num, resp: r}:
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := s.initialize(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *StdioSession) initialize() error {
	initID := nextID()
	if err := s.enc.Encode(rpcRequest{
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
	if _, err := s.waitFor(initID, 30*time.Second); err != nil {
		return fmt.Errorf("initialize response: %w", err)
	}
	_ = s.enc.Encode(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	return nil
}

func (s *StdioSession) waitFor(id int64, timeout time.Duration) (rpcResponse, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-s.incoming:
			if !ok {
				return rpcResponse{}, fmt.Errorf("server %q closed connection", s.cfg.Name)
			}
			if msg.numID == id {
				return msg.resp, nil
			}
		case <-timer.C:
			return rpcResponse{}, fmt.Errorf("timeout waiting for response from %q", s.cfg.Name)
		}
	}
}

// ListTools returns the tools exposed by this server.
func (s *StdioSession) ListTools() ([]domain.MCPTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := nextID()
	if err := s.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}
	resp, err := s.waitFor(id, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	var result toolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool executes a named tool with the given arguments.
func (s *StdioSession) CallTool(name string, args map[string]interface{}) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := nextID()
	if err := s.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  map[string]interface{}{"name": name, "arguments": args},
	}); err != nil {
		return "", fmt.Errorf("send tools/call: %w", err)
	}
	resp, err := s.waitFor(id, 60*time.Second)
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}
	var parts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if result.IsError {
		return "", fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}

// Close terminates the subprocess.
func (s *StdioSession) Close() {
	s.cancel()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill() //nolint:errcheck
	}
	s.cmd.Wait() //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Pool — persistent stdio sessions for an entire execution phase
// ---------------------------------------------------------------------------

// Pool manages a set of persistent StdioSessions keyed by server name.
// It implements port.MCPToolLister and port.MCPToolCaller using the same
// long-lived subprocesses, avoiding per-call process spawning.
type Pool struct {
	sessions map[string]*StdioSession
	// cfgs is kept so SSE servers can be forwarded to the underlying Client.
	cfgs    map[string]domain.MCPServerConfig
	sseBase *Client
}

// NewPool starts persistent stdio sessions for each stdio server in cfgs.
// SSE servers are handled via the provided base Client (stateless HTTP).
// Errors from individual servers are collected but do not prevent others from starting.
func NewPool(cfgs []domain.MCPServerConfig, sseBase *Client) (*Pool, []error) {
	p := &Pool{
		sessions: make(map[string]*StdioSession),
		cfgs:     make(map[string]domain.MCPServerConfig),
		sseBase:  sseBase,
	}
	var errs []error
	for _, cfg := range cfgs {
		p.cfgs[cfg.Name] = cfg
		if cfg.Type != domain.MCPStdio {
			continue
		}
		sess, err := newStdioSession(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("server %q: %w", cfg.Name, err))
			continue
		}
		p.sessions[cfg.Name] = sess
	}
	return p, errs
}

// ListTools satisfies port.MCPToolLister.
// For stdio servers it uses the persistent session; for SSE servers it delegates to sseBase.
func (p *Pool) ListTools(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error) {
	if cfg.Type == domain.MCPStdio {
		sess, ok := p.sessions[cfg.Name]
		if !ok {
			return nil, fmt.Errorf("no active session for server %q", cfg.Name)
		}
		return sess.ListTools()
	}
	if p.sseBase != nil {
		return p.sseBase.listToolsSSE(ctx, cfg)
	}
	return nil, fmt.Errorf("no SSE client for server %q", cfg.Name)
}

// CallTool satisfies port.MCPToolCaller.
func (p *Pool) CallTool(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	if cfg.Type == domain.MCPStdio {
		sess, ok := p.sessions[cfg.Name]
		if !ok {
			return "", fmt.Errorf("no active session for server %q", cfg.Name)
		}
		return sess.CallTool(name, args)
	}
	if p.sseBase != nil {
		return p.sseBase.callToolSSE(ctx, cfg, name, args)
	}
	return "", fmt.Errorf("no SSE client for server %q", cfg.Name)
}

// Close terminates all persistent stdio sessions.
func (p *Pool) Close() {
	for _, sess := range p.sessions {
		sess.Close()
	}
}

// Ensure Pool satisfies the port interfaces at compile time.
var _ port.MCPToolLister = (*Pool)(nil)
var _ port.MCPToolCaller = (*Pool)(nil)
