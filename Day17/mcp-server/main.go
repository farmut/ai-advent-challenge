// petstore-mcp-server — MCP server that exposes the Swagger Petstore API as tools.
//
// Transport: stdio (newline-delimited JSON-RPC 2.0).
// Protocol:  MCP 2024-11-05 (initialize → notifications/initialized → tools/list / tools/call).
//
// Usage: ./petstore-mcp-server
// The binary reads JSON-RPC requests from stdin and writes responses to stdout.
// Diagnostic logs go to stderr so they don't interfere with the protocol stream.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"petstore-mcp-server/petstore"
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
// Server
// ---------------------------------------------------------------------------

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("[petstore-mcp] ")
	log.SetFlags(0)
	log.Println("server started")

	client := petstore.NewClient()

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

		switch req.Method {

		case "initialize":
			send(enc, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name":    "petstore-mcp-server",
						"version": "1.0.0",
					},
				},
			})

		case "notifications/initialized":
			// Notification — no response required.

		case "tools/list":
			send(enc, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"tools": petstore.ToolDefinitions(),
				},
			})

		case "tools/call":
			var params toolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				send(enc, errResp(req.ID, -32602, "invalid params: "+err.Error()))
				continue
			}

			log.Printf("  calling tool %q with %v", params.Name, params.Arguments)

			text, callErr := petstore.CallTool(client, params.Name, params.Arguments)
			if callErr != nil {
				log.Printf("  tool error: %v", callErr)
				send(enc, rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result: toolResult{
						Content: []contentItem{{Type: "text", Text: callErr.Error()}},
						IsError: true,
					},
				})
				continue
			}

			log.Printf("  → %d bytes", len(text))
			send(enc, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: toolResult{
					Content: []contentItem{{Type: "text", Text: text}},
					IsError: false,
				},
			})

		default:
			if req.ID != nil {
				send(enc, errResp(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method)))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stdin error: %v", err)
		os.Exit(1)
	}
	log.Println("stdin closed, shutting down")
}

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
