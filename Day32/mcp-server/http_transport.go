package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"petstore-mcp-server/petstore"
)

// ---------------------------------------------------------------------------
// Session store — one SSE session per connected client
// ---------------------------------------------------------------------------

type session struct {
	ch chan string // buffered channel; server writes JSON-RPC responses here
}

type sessionStore struct {
	mu   sync.Mutex
	data map[string]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: make(map[string]*session)}
}

// create allocates a new session and returns its ID and message channel.
func (s *sessionStore) create() (string, chan string) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	ch := make(chan string, 32)
	s.mu.Lock()
	s.data[id] = &session{ch: ch}
	s.mu.Unlock()
	return id, ch
}

// send enqueues a message for the session; returns false if session is gone or buffer is full.
func (s *sessionStore) send(id, msg string) bool {
	s.mu.Lock()
	sess, ok := s.data[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case sess.ch <- msg:
		return true
	default:
		return false // buffer full — client is too slow
	}
}

// delete removes the session and closes its channel.
func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	if sess, ok := s.data[id]; ok {
		close(sess.ch)
		delete(s.data, id)
	}
	s.mu.Unlock()
}

// count returns the number of active sessions.
func (s *sessionStore) count() int {
	s.mu.Lock()
	n := len(s.data)
	s.mu.Unlock()
	return n
}

// ---------------------------------------------------------------------------
// HTTP+SSE server
// ---------------------------------------------------------------------------

// runHTTP starts the MCP server in HTTP+SSE mode and blocks until the process
// receives SIGINT or SIGTERM.
//
// MCP HTTP+SSE protocol (spec 2024-11-05):
//
//	GET  /sse              — client opens a persistent SSE stream.
//	                         Server responds with:
//	                           event: endpoint
//	                           data: /message?sessionId=<id>
//	                         Then streams JSON-RPC responses as:
//	                           event: message
//	                           data: <json>
//
//	POST /message?sessionId=<id>
//	                       — client sends a JSON-RPC request.
//	                         Server returns 202 Accepted and delivers the
//	                         JSON-RPC response via the SSE stream.
//
//	GET  /health           — liveness probe; returns {"status":"ok"}.
func runHTTP(handler *petstore.Handler, addr string) {
	store := newSessionStore()
	mux := http.NewServeMux()

	// ── GET /sse ────────────────────────────────────────────────────────────

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported by this server", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Disable the per-connection write deadline so the SSE stream can stay
		// open indefinitely (the server-level WriteTimeout does not apply here).
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		id, ch := store.create()
		defer store.delete(id)

		log.Printf("[http] SSE connected    session=%s remote=%s  (active=%d)", id, r.RemoteAddr, store.count())

		// Spec: send an `endpoint` event so the client knows where to POST.
		fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", id)
		flusher.Flush()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return // channel closed by store.delete
				}
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
				log.Printf("[http] → message sent   session=%s (%d bytes)", id, len(msg))
			case <-r.Context().Done():
				log.Printf("[http] SSE disconnected session=%s remote=%s", id, r.RemoteAddr)
				return
			}
		}
	})

	// ── POST /message ────────────────────────────────────────────────────────

	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.URL.Query().Get("sessionId")
		if sessionID == "" {
			http.Error(w, "missing sessionId query parameter", http.StatusBadRequest)
			return
		}

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON-RPC: "+err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("[http] ← %s (id=%v session=%s)", req.Method, req.ID, sessionID)

		// Notifications have no ID — acknowledge and skip the response.
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		resp := handleRPC(handler, req)

		data, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "internal marshal error", http.StatusInternalServerError)
			return
		}

		if !store.send(sessionID, string(data)) {
			http.Error(w, "session not found or response buffer full", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	})

	// ── GET /health ───────────────────────────────────────────────────────────

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"transport":       "http+sse",
			"active_sessions": store.count(),
		})
	})

	srv := &http.Server{
		Addr:        addr,
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
		// WriteTimeout is intentionally left at zero (disabled): a non-zero
		// value would terminate long-lived SSE connections prematurely.
		IdleTimeout: 120 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("[http] shutdown signal received, draining connections…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[http] shutdown error: %v", err)
		}
	}()

	log.Printf("[http] petstore-mcp-server listening on %s (HTTP+SSE)", addr)
	log.Printf("[http]   GET  /sse              — open SSE stream")
	log.Printf("[http]   POST /message?sessionId=<id> — send JSON-RPC")
	log.Printf("[http]   GET  /health           — liveness probe")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[http] ListenAndServe: %v", err)
	}
	log.Println("[http] server stopped")
}
