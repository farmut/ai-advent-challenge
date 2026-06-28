// petstore-mcp-server — MCP server that exposes the Swagger Petstore API as tools.
//
// Transports:
//
//	stdio (default)  — newline-delimited JSON-RPC 2.0 on stdin/stdout; used when
//	                   launched as a subprocess by an MCP client.
//	HTTP+SSE         — persistent service mode; enabled with the -addr flag.
//	                   Clients connect to GET /sse, then POST to /message?sessionId=<id>.
//
// Usage:
//
//	./petstore-mcp-server               # stdio (subprocess) mode
//	./petstore-mcp-server -addr :8080   # HTTP+SSE service mode
package main

import (
	"bufio"
	"encoding/json"
	"flag"
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
// Entry point
// ---------------------------------------------------------------------------

func main() {
	addr := flag.String("addr", "", "HTTP+SSE listen address (e.g. :8080); if empty, stdio transport is used")
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.SetPrefix("[petstore-mcp] ")
	log.SetFlags(0)

	handler := petstore.NewHandler(petstore.NewClient())

	if *addr != "" {
		runHTTP(handler, *addr)
	} else {
		log.Println("server started (stdio)")
		runStdio(handler)
	}
}

// ---------------------------------------------------------------------------
// stdio transport
// ---------------------------------------------------------------------------

func runStdio(handler *petstore.Handler) {
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

		resp := handleRPC(handler, req)
		send(enc, resp)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stdin error: %v", err)
		os.Exit(1)
	}
	log.Println("stdin closed, shutting down")
}

// ---------------------------------------------------------------------------
// Shared request handler (used by both transports)
// ---------------------------------------------------------------------------

func handleRPC(handler *petstore.Handler, req rpcRequest) rpcResponse {
	switch req.Method {

	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "petstore-mcp-server", "version": "1.0.0"},
			},
		}

	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"tools": petstore.ToolDefinitions()},
		}

	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, -32602, "invalid params: "+err.Error())
		}

		log.Printf("  calling tool %q with %v", params.Name, params.Arguments)

		text, callErr := handler.CallTool(params.Name, params.Arguments)
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
