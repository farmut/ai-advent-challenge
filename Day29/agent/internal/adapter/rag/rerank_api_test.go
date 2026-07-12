package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIReranker_ScoresSortsAndSendsDocuments(t *testing.T) {
	var gotReq rerankAPIRequest
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		// Rank the second document above the first.
		io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.2},{"index":1,"relevance_score":0.9}]}`)
	}))
	defer srv.Close()

	rr := NewAPIReranker(srv.URL, "cohere/rerank-4-fast", "sk-test")
	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/rerank" {
		t.Errorf("expected POST to /rerank, got %s", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected bearer auth, got %q", gotAuth)
	}
	if gotReq.Model != "cohere/rerank-4-fast" || gotReq.Query != "q" {
		t.Errorf("unexpected request model/query: %+v", gotReq)
	}
	if len(gotReq.Documents) != 2 || gotReq.Documents[0] != "alpha" || gotReq.Documents[1] != "beta" {
		t.Errorf("documents not sent as chunk contents: %+v", gotReq.Documents)
	}
	if gotReq.TopN != 2 {
		t.Errorf("expected top_n=2, got %d", gotReq.TopN)
	}
	if out[0].File != "b.md" || out[0].RerankScore != 0.9 {
		t.Errorf("expected b.md@0.9 first, got %s@%.2f", out[0].File, out[0].RerankScore)
	}
	if out[1].File != "a.md" || out[1].RerankScore != 0.2 {
		t.Errorf("expected a.md@0.2 second, got %s@%.2f", out[1].File, out[1].RerankScore)
	}
}

func TestAPIReranker_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad model"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	rr := NewAPIReranker(srv.URL, "bad", "k")
	if _, err := rr.Rerank(context.Background(), "q", chunks()); err == nil {
		t.Fatal("expected HTTP 400 to propagate as an error")
	}
}

func TestAPIReranker_MissingScoreFallsBackToSimilarity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only document 0 is scored; document 1 must keep its similarity (0.70).
		io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.1}]}`)
	}))
	defer srv.Close()

	rr := NewAPIReranker(srv.URL, "m", "k")
	out, err := rr.Rerank(context.Background(), "q", chunks())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].File != "b.md" || out[0].RerankScore != 0.70 {
		t.Errorf("expected fallback b.md@0.70 first, got %s@%.2f", out[0].File, out[0].RerankScore)
	}
}

func TestAPIReranker_EmptyInputNoCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for empty input")
	}))
	defer srv.Close()

	rr := NewAPIReranker(srv.URL, "m", "k")
	out, err := rr.Rerank(context.Background(), "q", nil)
	if err != nil || out != nil {
		t.Fatalf("empty input should return (nil, nil), got (%v, %v)", out, err)
	}
}
