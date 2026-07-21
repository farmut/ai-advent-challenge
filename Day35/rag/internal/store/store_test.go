package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"rag/internal/domain"

	_ "modernc.org/sqlite"
)

// legacyDDL is the schema as it shipped before url/title/format existed.
// Tests build a database with it to prove the migration path from an index that
// is already committed to the repository.
const legacyDDL = `
	CREATE TABLE chunks (
		id        TEXT    PRIMARY KEY,
		source    TEXT    NOT NULL,
		file      TEXT    NOT NULL,
		section   TEXT    NOT NULL DEFAULT '',
		chunk_id  INTEGER NOT NULL,
		content   TEXT    NOT NULL,
		embedding TEXT
	);
	CREATE INDEX idx_chunks_source ON chunks(source);
`

// newLegacyDB creates an old-schema database holding one embedded chunk.
func newLegacyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(legacyDDL); err != nil {
		t.Fatalf("legacy ddl: %v", err)
	}
	emb, _ := json.Marshal([]float64{1, 0, 0})
	if _, err := db.Exec(`
		INSERT INTO chunks (id, source, file, section, chunk_id, content, embedding)
		VALUES ('old#0', '/docs/old.md', 'old.md', 'Intro', 0, 'legacy content', ?)`,
		string(emb)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

func TestMigrateAddsColumnsAndKeepsOldRows(t *testing.T) {
	path := newLegacyDB(t)

	st, err := Open(path) // runs migrate()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cols, err := st.chunkColumnSet()
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	for _, c := range chunkColumns {
		if !cols[c] {
			t.Errorf("column %q was not added by the migration", c)
		}
	}

	// The pre-existing row must still be there, readable, with empty new fields.
	res, err := st.Search(context.Background(), []float64{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want the 1 legacy row", len(res))
	}
	got := res[0].Chunk
	if got.ID != "old#0" || got.Content != "legacy content" || got.Meta.Section != "Intro" {
		t.Errorf("legacy row was altered: %+v", got)
	}
	if got.Meta.URL != "" || got.Meta.Title != "" || got.Meta.Format != "" {
		t.Errorf("new columns should default to empty, got url=%q title=%q format=%q",
			got.Meta.URL, got.Meta.Title, got.Meta.Format)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := newLegacyDB(t)

	for i := 0; i < 3; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		total, embedded, err := st.Stats(context.Background())
		if err != nil {
			t.Fatalf("stats #%d: %v", i, err)
		}
		if total != 1 || embedded != 1 {
			t.Fatalf("open #%d: total=%d embedded=%d, want 1/1", i, total, embedded)
		}
		st.Close()
	}
}

func TestSaveChunkRoundTripsNewColumns(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	want := domain.Chunk{
		ID:      "page-1#0",
		Content: "как оформить возврат",
		Meta: domain.ChunkMeta{
			Source:  "page-1",
			File:    "Возврат товара",
			Title:   "Возврат товара",
			URL:     "https://wiki.example.com/support/return",
			Format:  "markdown",
			Section: "Возврат > Сроки",
			ChunkID: 0,
		},
	}
	if err := st.SaveChunk(ctx, want, []float64{1, 0, 0}); err != nil {
		t.Fatalf("save: %v", err)
	}

	res, err := st.Search(ctx, []float64{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
	if got := res[0].Chunk.Meta; got != want.Meta {
		t.Errorf("meta round-trip mismatch:\n got %+v\nwant %+v", got, want.Meta)
	}
}

func TestDeleteChunksBySourceDropsStaleTail(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "reindex.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	save := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			c := domain.Chunk{
				ID:      "doc#" + string(rune('0'+i)),
				Content: "body",
				Meta:    domain.ChunkMeta{Source: "doc", File: "doc.md", ChunkID: i},
			}
			if err := st.SaveChunk(ctx, c, []float64{1, 0, 0}); err != nil {
				t.Fatalf("save chunk %d: %v", i, err)
			}
		}
	}

	save(5)
	if total, _, _ := st.Stats(ctx); total != 5 {
		t.Fatalf("after first index: %d chunks, want 5", total)
	}

	// Re-index the same document, now shorter. Without the delete the upsert
	// would leave chunks 2..4 behind.
	if err := st.DeleteChunksBySource(ctx, "doc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	save(2)

	total, _, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if total != 2 {
		t.Fatalf("after re-index 5→2: %d chunks, want exactly 2", total)
	}
}

func TestDeleteChunksBySourceLeavesOtherDocuments(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "multi.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	for _, src := range []string{"a", "b"} {
		c := domain.Chunk{
			ID:   src + "#0",
			Meta: domain.ChunkMeta{Source: src, File: src},
		}
		if err := st.SaveChunk(ctx, c, []float64{1, 0, 0}); err != nil {
			t.Fatalf("save %s: %v", src, err)
		}
	}
	if err := st.DeleteChunksBySource(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	total, _, _ := st.Stats(ctx)
	if total != 1 {
		t.Fatalf("got %d chunks, want 1 (only document b)", total)
	}
}

func TestDocumentStateRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "docs.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	if _, ok, err := st.DocumentState(ctx, "missing"); err != nil || ok {
		t.Fatalf("unknown document: ok=%v err=%v, want false/nil", ok, err)
	}

	want := DocumentState{
		Source:      "page-7",
		URL:         "https://wiki.example.com/support/faq",
		Title:       "FAQ",
		Version:     "rev-12",
		ContentHash: "deadbeef",
		ChunkCount:  4,
		IndexedAt:   time.Date(2026, 7, 20, 10, 30, 0, 0, time.UTC),
	}
	if err := st.SaveDocument(ctx, want); err != nil {
		t.Fatalf("save document: %v", err)
	}

	got, ok, err := st.DocumentState(ctx, "page-7")
	if err != nil || !ok {
		t.Fatalf("read back: ok=%v err=%v", ok, err)
	}
	if !got.IndexedAt.Equal(want.IndexedAt) {
		t.Errorf("IndexedAt = %v, want %v", got.IndexedAt, want.IndexedAt)
	}
	got.IndexedAt, want.IndexedAt = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	// Re-saving the same source updates in place rather than duplicating.
	want.Version = "rev-13"
	if err := st.SaveDocument(ctx, want); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, _, err = st.DocumentState(ctx, "page-7")
	if err != nil {
		t.Fatalf("read back after re-save: %v", err)
	}
	if got.Version != "rev-13" {
		t.Errorf("Version = %q, want %q", got.Version, "rev-13")
	}
	if got.IndexedAt.IsZero() {
		t.Error("IndexedAt should be stamped automatically when left zero")
	}
}

func TestIndexMetaRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	if _, ok, err := st.GetMeta(ctx, "embed_model"); err != nil || ok {
		t.Fatalf("unset key: ok=%v err=%v, want false/nil", ok, err)
	}
	if err := st.SetMeta(ctx, "embed_model", "nomic-embed-text-v2-moe"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.SetMeta(ctx, "source_kind", "wiki"); err != nil {
		t.Fatalf("set: %v", err)
	}

	v, ok, err := st.GetMeta(ctx, "embed_model")
	if err != nil || !ok || v != "nomic-embed-text-v2-moe" {
		t.Fatalf("get embed_model = %q ok=%v err=%v", v, ok, err)
	}

	// Overwrite must replace, not duplicate.
	if err := st.SetMeta(ctx, "embed_model", "other-model"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	v, _, _ = st.GetMeta(ctx, "embed_model")
	if v != "other-model" {
		t.Fatalf("get embed_model = %q, want %q", v, "other-model")
	}
	if v, _, _ = st.GetMeta(ctx, "source_kind"); v != "wiki" {
		t.Fatalf("source_kind = %q, want wiki", v)
	}
}
