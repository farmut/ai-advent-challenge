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
	// Which optional columns this index actually has. Databases built by an
	// older `rag index` lack url/title, and because the agent opens them
	// read-only (mode=ro) they cannot be migrated on the fly — the SELECT has to
	// adapt instead, or reading a committed index fails outright.
	hasURL   bool
	hasTitle bool
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

	s := &vectorStore{db: db}
	cols, err := chunkColumns(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("inspect index %s: %w", path, err)
	}
	s.hasURL, s.hasTitle = cols["url"], cols["title"]
	return s, nil
}

// chunkColumns reports the column names of the `chunks` table.
func chunkColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(chunks)`)
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

func (s *vectorStore) close() error { return s.db.Close() }

// search returns the topK chunks most similar to queryVec, ranked by cosine
// similarity computed in-process (linear scan — fine for the corpus sizes here).
func (s *vectorStore) search(ctx context.Context, queryVec []float64, topK int) ([]domain.RetrievedChunk, error) {
	return s.searchRows(ctx, queryVec, topK)
}

// searchRows scans the index into domain chunks. url/title land directly in the
// domain fields; on an old-schema index they are selected as '' and stay empty.
func (s *vectorStore) searchRows(ctx context.Context, queryVec []float64, topK int) ([]domain.RetrievedChunk, error) {
	// Columns absent from an older index are selected as literal '' so the scan
	// target list stays fixed regardless of schema version.
	urlCol, titleCol := `''`, `''`
	if s.hasURL {
		urlCol = "url"
	}
	if s.hasTitle {
		titleCol = "title"
	}
	query := fmt.Sprintf(`
		SELECT id, source, file, section, chunk_id, content, embedding, %s, %s
		FROM chunks
		WHERE embedding IS NOT NULL AND embedding != ''`, urlCol, titleCol)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.RetrievedChunk
	for rows.Next() {
		var (
			id, source, file, section, content string
			url, title                         string
			chunkID                            int
			embJSON                            sql.NullString
		)
		if err := rows.Scan(&id, &source, &file, &section, &chunkID, &content, &embJSON,
			&url, &title); err != nil {
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
			Title:      title,
			URL:        url,
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
