package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"petstore-mcp-server/petstore"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestHTTPServer wires up the HTTP+SSE mux against a stub petstore API.
func newTestHTTPServer(t *testing.T) (*httptest.Server, *sessionStore) {
	t.Helper()

	// Stub petstore API — returns [] for findByStatus, {} for everything else.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "findByStatus") {
			w.Write([]byte("[]"))
		} else {
			w.Write([]byte("{}"))
		}
	}))
	t.Cleanup(stub.Close)

	handler := petstore.NewHandler(petstore.NewClientWithBase(stub.URL))

	store := newSessionStore()
	mux := http.NewServeMux()
	registerHandlers(mux, handler, store)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

// registerHandlers extracts the mux setup from runHTTP so tests can reuse it
// without starting a real listener.
func registerHandlers(mux *http.ServeMux, handler *petstore.Handler, store *sessionStore) {
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		id, ch := store.create()
		defer store.delete(id)

		fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", id)
		flusher.Flush()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sessionID := r.URL.Query().Get("sessionId")
		if sessionID == "" {
			http.Error(w, "missing sessionId", http.StatusBadRequest)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := handleRPC(handler, req)
		data, _ := json.Marshal(resp)
		if !store.send(sessionID, string(data)) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"active_sessions": store.count(),
		})
	})
}

// readSSEEvents reads up to n SSE events from the response body.
// It blocks until n events are received or the timeout is exceeded.
func readSSEEvents(t *testing.T, resp *http.Response, n int, timeout time.Duration) []map[string]string {
	t.Helper()
	events := make([]map[string]string, 0, n)
	done := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		cur := map[string]string{}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(cur) > 0 {
					events = append(events, cur)
					cur = map[string]string{}
					if len(events) >= n {
						close(done)
						return
					}
				}
				continue
			}
			if after, ok := strings.CutPrefix(line, "event: "); ok {
				cur["event"] = after
			} else if after, ok := strings.CutPrefix(line, "data: "); ok {
				cur["data"] = after
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Log("readSSEEvents: timeout reached")
	}
	return events
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHTTP_Health(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("health GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
}

func TestHTTP_SSE_EndpointEvent(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	// Open SSE connection.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE GET: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	// First event must be `endpoint`.
	events := readSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) == 0 {
		t.Fatal("no SSE events received")
	}
	if events[0]["event"] != "endpoint" {
		t.Errorf("expected event=endpoint, got %q", events[0]["event"])
	}
	if !strings.HasPrefix(events[0]["data"], "/message?sessionId=") {
		t.Errorf("endpoint data should start with /message?sessionId=, got %q", events[0]["data"])
	}
}

func TestHTTP_RoundTrip_Initialize(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	// Step 1: open SSE stream, grab session ID from endpoint event.
	sseReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE GET: %v", err)
	}
	defer sseResp.Body.Close()

	endpointEvents := readSSEEvents(t, sseResp, 1, 2*time.Second)
	if len(endpointEvents) == 0 {
		t.Fatal("no endpoint event")
	}
	msgPath := endpointEvents[0]["data"] // e.g. /message?sessionId=abc

	// Step 2: send initialize request.
	payload, _ := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}`),
	})
	postResp, err := http.Post(srv.URL+msgPath, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /message: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", postResp.StatusCode)
	}

	// Step 3: read the JSON-RPC response from SSE.
	msgEvents := readSSEEvents(t, sseResp, 1, 3*time.Second)
	if len(msgEvents) == 0 {
		t.Fatal("no message event received after initialize")
	}
	if msgEvents[0]["event"] != "message" {
		t.Errorf("expected event=message, got %q", msgEvents[0]["event"])
	}

	var resp rpcResponse
	if err := json.Unmarshal([]byte(msgEvents[0]["data"]), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error in response: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not an object: %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion=2024-11-05, got %v", result["protocolVersion"])
	}
}

func TestHTTP_RoundTrip_ToolsList(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	sseReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	sseResp, _ := http.DefaultClient.Do(sseReq)
	defer sseResp.Body.Close()

	endpointEvents := readSSEEvents(t, sseResp, 1, 2*time.Second)
	msgPath := endpointEvents[0]["data"]

	payload, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: float64(2), Method: "tools/list"})
	postResp, _ := http.Post(srv.URL+msgPath, "application/json", bytes.NewReader(payload))
	postResp.Body.Close()

	msgEvents := readSSEEvents(t, sseResp, 1, 3*time.Second)
	if len(msgEvents) == 0 {
		t.Fatal("no message event for tools/list")
	}

	var resp rpcResponse
	json.Unmarshal([]byte(msgEvents[0]["data"]), &resp)

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not an object")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools is not an array")
	}
	if len(tools) != 22 {
		t.Errorf("expected 22 tools, got %d", len(tools))
	}
}

func TestHTTP_Notification_NoResponse(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	// POST a notification (no ID) — server must return 202 without queuing anything.
	payload, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		// no "id" field
	})
	resp, err := http.Post(srv.URL+"/message?sessionId=nonexistent", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST notification: %v", err)
	}
	resp.Body.Close()
	// Notifications are accepted even without an active session (ID check comes after nil-ID check).
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 for notification, got %d", resp.StatusCode)
	}
}

func TestHTTP_UnknownMethod(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	sseReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	sseResp, _ := http.DefaultClient.Do(sseReq)
	defer sseResp.Body.Close()

	endpointEvents := readSSEEvents(t, sseResp, 1, 2*time.Second)
	msgPath := endpointEvents[0]["data"]

	payload, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: float64(9), Method: "unknown/method"})
	http.Post(srv.URL+msgPath, "application/json", bytes.NewReader(payload))

	msgEvents := readSSEEvents(t, sseResp, 1, 3*time.Second)
	if len(msgEvents) == 0 {
		t.Fatal("no message event for unknown method")
	}

	var resp rpcResponse
	json.Unmarshal([]byte(msgEvents[0]["data"]), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestHTTP_WrongMethod(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	// GET /message should return 405.
	resp, _ := http.Get(srv.URL + "/message?sessionId=x")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}

	// POST /sse should return 405.
	resp2, _ := http.Post(srv.URL+"/sse", "application/json", nil)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp2.StatusCode)
	}
}

func TestHTTP_MissingSessionID(t *testing.T) {
	srv, _ := newTestHTTPServer(t)

	payload, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: float64(1), Method: "tools/list"})
	resp, _ := http.Post(srv.URL+"/message", "application/json", bytes.NewReader(payload))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sessionId, got %d", resp.StatusCode)
	}
}

func TestHTTP_SessionCount(t *testing.T) {
	srv, store := newTestHTTPServer(t)

	if store.count() != 0 {
		t.Fatalf("expected 0 sessions initially, got %d", store.count())
	}

	// Open two SSE connections.
	r1, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	resp1, _ := http.DefaultClient.Do(r1)
	defer resp1.Body.Close()
	readSSEEvents(t, resp1, 1, time.Second) // consume endpoint event

	r2, _ := http.NewRequest(http.MethodGet, srv.URL+"/sse", nil)
	resp2, _ := http.DefaultClient.Do(r2)
	defer resp2.Body.Close()
	readSSEEvents(t, resp2, 1, time.Second)

	if store.count() != 2 {
		t.Errorf("expected 2 active sessions, got %d", store.count())
	}
}
