package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"

	"ai-adv-agent/internal/domain"

	_ "modernc.org/sqlite"
)

// vectorStore is a read-only view over the SQLite index produced by the `rag`
// component's `index` command. The agent never writes to it — it only reads
// chunks + embeddings to answer queries.
type vectorStore struct {
	db *sql.DB
}

// openStore opens the SQLite index at path in read-only mode.
func openStore(path string) (*vectorStore, error) {
	// Fail with a clear message if the index is missing, rather than the opaque
	// SQLITE_CANTOPEN the driver would surface. Run `rag index` to build it.
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("index %q not found — build it with the `rag index` command: %w", path, err)
	}
	// mode=ro opens the existing index read-only instead of creating an empty
	// database that would silently return zero results.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open index %s: %w", path, err)
	}
	return &vectorStore{db: db}, nil
}

func (s *vectorStore) close() error { return s.db.Close() }

// search returns the topK chunks most similar to queryVec, ranked by cosine
// similarity computed in-process (linear scan — fine for the corpus sizes here).
func (s *vectorStore) search(ctx context.Context, queryVec []float64, topK int) ([]domain.RetrievedChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, file, section, chunk_id, content, embedding
		FROM chunks
		WHERE embedding IS NOT NULL AND embedding != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.RetrievedChunk
	for rows.Next() {
		var (
			id, source, file, section, content string
			chunkID                            int
			embJSON                            sql.NullString
		)
		if err := rows.Scan(&id, &source, &file, &section, &chunkID, &content, &embJSON); err != nil {
			return nil, err
		}
		if !embJSON.Valid || embJSON.String == "" {
			continue
		}
		var vec []float64
		if err := json.Unmarshal([]byte(embJSON.String), &vec); err != nil {
			continue // skip corrupted rows
		}
		results = append(results, domain.RetrievedChunk{
			ID:         id,
			Source:     source,
			File:       file,
			Section:    section,
			ChunkID:    chunkID,
			Content:    content,
			Similarity: cosineSimilarity(queryVec, vec),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
