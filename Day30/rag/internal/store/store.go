package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"rag/internal/domain"
	_ "modernc.org/sqlite"
)

// Store is a SQLite-backed index of chunks and their embedding vectors.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS chunks (
			id        TEXT    PRIMARY KEY,
			source    TEXT    NOT NULL,
			file      TEXT    NOT NULL,
			section   TEXT    NOT NULL DEFAULT '',
			chunk_id  INTEGER NOT NULL,
			content   TEXT    NOT NULL,
			embedding TEXT            -- JSON float64 array; NULL = not yet embedded
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_source ON chunks(source);
	`)
	return err
}

// SaveChunk upserts a chunk and its embedding vector into the index.
// embedding may be nil if the chunk is stored without a vector.
func (s *Store) SaveChunk(ctx context.Context, chunk domain.Chunk, embedding []float64) error {
	var embJSON sql.NullString
	if len(embedding) > 0 {
		b, err := json.Marshal(embedding)
		if err != nil {
			return fmt.Errorf("marshal embedding: %w", err)
		}
		embJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO chunks (id, source, file, section, chunk_id, content, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID,
		chunk.Meta.Source,
		chunk.Meta.File,
		chunk.Meta.Section,
		chunk.Meta.ChunkID,
		chunk.Content,
		embJSON,
	)
	return err
}

// SearchResult pairs a chunk with its cosine similarity to the query vector.
type SearchResult struct {
	Chunk      domain.Chunk
	Similarity float64
}

// Search returns the topK most similar chunks to queryVec.
// Similarity is computed in-process via cosine distance (linear scan).
func (s *Store) Search(ctx context.Context, queryVec []float64, topK int) ([]SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, file, section, chunk_id, content, embedding
		FROM chunks
		WHERE embedding IS NOT NULL AND embedding != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
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

		results = append(results, SearchResult{
			Chunk: domain.Chunk{
				ID:      id,
				Content: content,
				Meta: domain.ChunkMeta{
					Source:  source,
					File:    file,
					Section: section,
					ChunkID: chunkID,
				},
			},
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

// Stats returns the total number of chunks and how many have embeddings.
func (s *Store) Stats(ctx context.Context) (total, embedded int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&total)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chunks WHERE embedding IS NOT NULL AND embedding != ''`).Scan(&embedded)
	return
}

// ---------------------------------------------------------------------------
// math helpers
// ---------------------------------------------------------------------------

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
