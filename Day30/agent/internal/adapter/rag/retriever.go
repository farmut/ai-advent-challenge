// Package rag provides the agent's retrieval adapter: it embeds a query with an
// OpenAI-compatible embeddings endpoint and searches the SQLite vector index
// built by the `rag` component, implementing port.Retriever.
package rag

import (
	"context"
	"fmt"

	"ai-adv-agent/internal/domain"
)

// Config wires a Retriever to an embeddings endpoint and a SQLite index.
type Config struct {
	DBPath     string // path to the SQLite index (rag.db)
	EmbedURL   string // embeddings API base URL (e.g. http://localhost:11434)
	EmbedModel string // embeddings model — must match the one used at index time
	EmbedKey   string // API key (empty for local runtimes)
}

// Retriever implements port.Retriever over a SQLite index + embeddings endpoint.
type Retriever struct {
	emb   *embedder
	store *vectorStore
}

// NewRetriever opens the index and prepares the embeddings client.
// Call Close when done to release the database connection.
func NewRetriever(cfg Config) (*Retriever, error) {
	st, err := openStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &Retriever{
		emb:   newEmbedder(cfg.EmbedURL, cfg.EmbedModel, cfg.EmbedKey),
		store: st,
	}, nil
}

// Retrieve embeds the query and returns the topK most similar indexed chunks.
func (r *Retriever) Retrieve(ctx context.Context, query string, topK int) ([]domain.RetrievedChunk, error) {
	vec, err := r.emb.embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	chunks, err := r.store.search(ctx, vec, topK)
	if err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	return chunks, nil
}

// Close releases the underlying database connection.
func (r *Retriever) Close() error { return r.store.close() }
