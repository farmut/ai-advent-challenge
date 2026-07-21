package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	_ "modernc.org/sqlite"
	"rag/internal/domain"
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

// chunkColumns are the columns added to `chunks` after the initial schema.
// They are applied with ALTER TABLE so that a database created by an earlier
// version is brought up to date in place, without losing its rows.
var chunkColumns = []string{"url", "title", "format"}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
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

		-- One row per indexed document: lets the indexer skip documents whose
		-- version/hash has not changed, and records what the index contains.
		CREATE TABLE IF NOT EXISTS documents (
			source       TEXT PRIMARY KEY,
			url          TEXT    NOT NULL DEFAULT '',
			title        TEXT    NOT NULL DEFAULT '',
			version      TEXT    NOT NULL DEFAULT '',
			content_hash TEXT    NOT NULL DEFAULT '',
			chunk_count  INTEGER NOT NULL DEFAULT 0,
			indexed_at   TEXT    NOT NULL DEFAULT ''
		);

		-- Index-wide key/value facts: embed_model, source_kind, indexed_at,
		-- manifest_path. Readers use them to detect a stale or mismatched index.
		CREATE TABLE IF NOT EXISTS index_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return err
	}

	existing, err := s.chunkColumnSet()
	if err != nil {
		return err
	}
	for _, col := range chunkColumns {
		if existing[col] {
			continue
		}
		// NOT NULL DEFAULT '' keeps pre-existing rows valid without a rewrite.
		if _, err := s.db.Exec(
			fmt.Sprintf(`ALTER TABLE chunks ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, col),
		); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	return nil
}

// chunkColumnSet reports which columns the `chunks` table currently has.
func (s *Store) chunkColumnSet() (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(chunks)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid          int
			name, ctype  string
			notNull, pk  int
			defaultValue sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
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
		INSERT OR REPLACE INTO chunks
			(id, source, file, section, chunk_id, content, embedding, url, title, format)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID,
		chunk.Meta.Source,
		chunk.Meta.File,
		chunk.Meta.Section,
		chunk.Meta.ChunkID,
		chunk.Content,
		embJSON,
		chunk.Meta.URL,
		chunk.Meta.Title,
		chunk.Meta.Format,
	)
	return err
}

// DeleteChunksBySource removes every chunk belonging to one document.
// Re-indexing must call it first: SaveChunk is an upsert keyed by chunk id, so
// without a delete a document that shrinks from 5 chunks to 2 would leave the
// three stale chunks behind and keep serving them in search results.
func (s *Store) DeleteChunksBySource(ctx context.Context, source string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chunks WHERE source = ?`, source)
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
		SELECT id, source, file, section, chunk_id, content, embedding, url, title, format
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
			url, title, format                 string
			chunkID                            int
			embJSON                            sql.NullString
		)
		if err := rows.Scan(&id, &source, &file, &section, &chunkID, &content, &embJSON,
			&url, &title, &format); err != nil {
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
					Title:   title,
					URL:     url,
					Format:  format,
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
// documents
// ---------------------------------------------------------------------------

// DocumentCount returns how many documents the index holds. Chunk counts alone
// do not answer "how many pages does the bot know about?", which is the number a
// person actually checks after a crawl.
func (s *Store) DocumentCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&n)
	return n, err
}

// DocumentState is what the index remembers about one indexed document.
// The indexer compares Version/ContentHash against the freshly fetched document
// to decide whether it needs re-chunking and re-embedding.
type DocumentState struct {
	Source      string
	URL         string
	Title       string
	Version     string
	ContentHash string
	ChunkCount  int
	IndexedAt   time.Time
}

// DocumentState returns the recorded state of source.
// The bool is false (with a nil error) when the document is not in the index.
func (s *Store) DocumentState(ctx context.Context, source string) (DocumentState, bool, error) {
	var (
		doc       DocumentState
		indexedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT source, url, title, version, content_hash, chunk_count, indexed_at
		FROM documents WHERE source = ?`, source,
	).Scan(&doc.Source, &doc.URL, &doc.Title, &doc.Version, &doc.ContentHash,
		&doc.ChunkCount, &indexedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DocumentState{}, false, nil
	}
	if err != nil {
		return DocumentState{}, false, err
	}
	if indexedAt != "" {
		// A malformed timestamp must not hide an otherwise usable row: the
		// caller compares Version/ContentHash, IndexedAt is informational.
		if t, perr := time.Parse(time.RFC3339, indexedAt); perr == nil {
			doc.IndexedAt = t
		}
	}
	return doc, true, nil
}

// SaveDocument upserts the document row. A zero IndexedAt is stamped with the
// current time so callers do not have to.
func (s *Store) SaveDocument(ctx context.Context, doc DocumentState) error {
	if doc.IndexedAt.IsZero() {
		doc.IndexedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO documents
			(source, url, title, version, content_hash, chunk_count, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		doc.Source, doc.URL, doc.Title, doc.Version, doc.ContentHash,
		doc.ChunkCount, doc.IndexedAt.UTC().Format(time.RFC3339))
	return err
}

// ---------------------------------------------------------------------------
// index_meta
// ---------------------------------------------------------------------------

// SetMeta stores an index-wide key/value fact (embed_model, source_kind, ...).
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO index_meta (key, value) VALUES (?, ?)`, key, value)
	return err
}

// GetMeta returns the value for key; the bool is false when key is not set.
func (s *Store) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM index_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
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
