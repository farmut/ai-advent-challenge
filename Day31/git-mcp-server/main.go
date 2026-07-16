// git-mcp-server — MCP server exposing read-only local git inspection as tools.
//
// Transport: stdio (newline-delimited JSON-RPC 2.0 on stdin/stdout) — the
// server is launched as a subprocess by an MCP client (the Day31 agent).
//
// Tools: git_current_branch, git_list_files, git_diff.
//
// Usage:
//
//	./git-mcp-server                 # repository = current directory
//	./git-mcp-server -repo /path     # repository = /path (validated at startup)
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"git-mcp-server/gitserver"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// MCP protocol types
// ---------------------------------------------------------------------------

type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	repo := flag.String("repo", ".", "path to the git repository the tools operate on")
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.SetPrefix("[git-mcp] ")
	log.SetFlags(0)

	srv, err := gitserver.New(*repo)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	log.Printf("server started (stdio), repo=%s", srv.Repo())

	runStdio(srv)
}

// ---------------------------------------------------------------------------
// stdio transport
// ---------------------------------------------------------------------------

func runStdio(srv *gitserver.Server) {
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			log.Printf("parse error: %v", err)
			continue
		}

		log.Printf("← %s (id=%v)", req.Method, req.ID)

		// Notifications carry no ID and require no response.
		if req.ID == nil {
			continue
		}

		send(enc, handleRPC(srv, req))
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stdin error: %v", err)
		os.Exit(1)
	}
	log.Println("stdin closed, shutting down")
}

// ---------------------------------------------------------------------------
// Request handler
// ---------------------------------------------------------------------------

func handleRPC(srv *gitserver.Server, req rpcRequest) rpcResponse {
	switch req.Method {

	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "git-mcp-server", "version": "1.0.0"},
			},
		}

	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"tools": gitserver.ToolDefinitions()},
		}

	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, -32602, "invalid params: "+err.Error())
		}

		log.Printf("  calling tool %q with %v", params.Name, params.Arguments)

		text, callErr := srv.CallTool(params.Name, params.Arguments)
		if callErr != nil {
			log.Printf("  tool error: %v", callErr)
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  toolResult{Content: []contentItem{{Type: "text", Text: callErr.Error()}}, IsError: true},
			}
		}

		log.Printf("  → %d bytes", len(text))
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  toolResult{Content: []contentItem{{Type: "text", Text: text}}, IsError: false},
		}

	default:
		return errResp(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func send(enc *json.Encoder, v interface{}) {
	if err := enc.Encode(v); err != nil {
		log.Printf("encode error: %v", err)
	}
}

func errResp(id interface{}, code int, msg string) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}
