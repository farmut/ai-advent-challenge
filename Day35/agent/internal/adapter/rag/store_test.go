package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// The agent opens indexes read-only, so it can never migrate them. Both schema
// generations therefore have to be readable as-is: the databases committed to
// the repo (rag/docs.db, agent/testdata/crm/shop.db) predate url/title.
const (
	oldSchemaDDL = `
		CREATE TABLE chunks (
			id        TEXT    PRIMARY KEY,
			source    TEXT    NOT NULL,
			file      TEXT    NOT NULL,
			section   TEXT    NOT NULL DEFAULT '',
			chunk_id  INTEGER NOT NULL,
			content   TEXT    NOT NULL,
			embedding TEXT
		);`

	newSchemaDDL = `
		CREATE TABLE chunks (
			id        TEXT    PRIMARY KEY,
			source    TEXT    NOT NULL,
			file      TEXT    NOT NULL,
			section   TEXT    NOT NULL DEFAULT '',
			chunk_id  INTEGER NOT NULL,
			content   TEXT    NOT NULL,
			embedding TEXT,
			url       TEXT    NOT NULL DEFAULT '',
			title     TEXT    NOT NULL DEFAULT '',
			format    TEXT    NOT NULL DEFAULT ''
		);`
)

// buildDB creates a database with the given DDL and one embedded chunk.
// extraCols/extraVals carry the columns that only exist in the new schema.
func buildDB(t *testing.T, name, ddl, insert string, args ...any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := db.Exec(insert, args...); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return path
}

func embJSON(t *testing.T, vec []float64) string {
	t.Helper()
	b, err := json.Marshal(vec)
	if err != nil {
		t.Fatalf("marshal embedding: %v", err)
	}
	return string(b)
}

func TestSearchOnOldSchema(t *testing.T) {
	path := buildDB(t, "old.db", oldSchemaDDL, `
		INSERT INTO chunks (id, source, file, section, chunk_id, content, embedding)
		VALUES ('old#0', '/docs/old.md', 'old.md', 'Intro', 0, 'legacy content', ?)`,
		embJSON(t, []float64{1, 0, 0}))

	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer st.close()

	if st.hasURL || st.hasTitle {
		t.Fatalf("old schema detected as hasURL=%v hasTitle=%v, want false/false",
			st.hasURL, st.hasTitle)
	}

	rows, err := st.searchRows(context.Background(), []float64{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search on old schema must not error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d results, want 1", len(rows))
	}
	if rows[0].URL != "" || rows[0].Title != "" {
		t.Errorf("missing columns should read as empty, got url=%q title=%q",
			rows[0].URL, rows[0].Title)
	}
	if rows[0].Content != "legacy content" || rows[0].Section != "Intro" {
		t.Errorf("unexpected row: %+v", rows[0])
	}
	if rows[0].Similarity < 0.99 {
		t.Errorf("similarity = %v, want ~1", rows[0].Similarity)
	}
}

func TestSearchOnNewSchema(t *testing.T) {
	path := buildDB(t, "new.db", newSchemaDDL, `
		INSERT INTO chunks (id, source, file, section, chunk_id, content, embedding, url, title, format)
		VALUES ('page-1#0', 'page-1', 'Возврат товара', 'Возврат > Сроки', 0, 'как оформить возврат', ?,
		        'https://wiki.example.com/support/return', 'Возврат товара', 'markdown')`,
		embJSON(t, []float64{1, 0, 0}))

	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer st.close()

	if !st.hasURL || !st.hasTitle {
		t.Fatalf("new schema detected as hasURL=%v hasTitle=%v, want true/true",
			st.hasURL, st.hasTitle)
	}

	rows, err := st.searchRows(context.Background(), []float64{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d results, want 1", len(rows))
	}
	if rows[0].URL != "https://wiki.example.com/support/return" {
		t.Errorf("URL = %q, want the stored link", rows[0].URL)
	}
	if rows[0].Title != "Возврат товара" {
		t.Errorf("Title = %q, want %q", rows[0].Title, "Возврат товара")
	}
}

// TestSearchOnCommittedIndex guards the actual regression: the docs index
// committed to the repository is old-schema and is opened read-only, so it can
// never be migrated. If the SELECT stops adapting, this fails.
func TestSearchOnCommittedIndex(t *testing.T) {
	const committed = "../../../../rag/docs.db"
	if _, err := os.Stat(committed); err != nil {
		t.Skipf("committed index not present at %s", committed)
	}

	st, err := openStore(committed)
	if err != nil {
		t.Fatalf("openStore on the committed index: %v", err)
	}
	defer st.close()

	// A zero vector scores 0 against everything, which is fine: the point is
	// that every row scans and decodes without error.
	rows, err := st.searchRows(context.Background(), make([]float64, 768), 5)
	if err != nil {
		t.Fatalf("search on the committed index: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("committed index returned no rows")
	}
	for _, r := range rows {
		if r.URL != "" || r.Title != "" {
			t.Errorf("old-schema index yielded url=%q title=%q, want empty", r.URL, r.Title)
		}
	}
}

// The public search() must keep working identically on both schemas — it is what
// the retriever calls.
func TestSearchWrapperWorksOnBothSchemas(t *testing.T) {
	old := buildDB(t, "old.db", oldSchemaDDL, `
		INSERT INTO chunks (id, source, file, section, chunk_id, content, embedding)
		VALUES ('old#0', '/docs/old.md', 'old.md', '', 0, 'legacy', ?)`,
		embJSON(t, []float64{0, 1, 0}))

	fresh := buildDB(t, "new.db", newSchemaDDL, `
		INSERT INTO chunks (id, source, file, section, chunk_id, content, embedding, url, title, format)
		VALUES ('new#0', 'new', 'new.md', '', 0, 'fresh', ?, 'https://x/y', 'New', 'markdown')`,
		embJSON(t, []float64{0, 1, 0}))

	for _, path := range []string{old, fresh} {
		st, err := openStore(path)
		if err != nil {
			t.Fatalf("openStore %s: %v", path, err)
		}
		chunks, err := st.search(context.Background(), []float64{0, 1, 0}, 5)
		if err != nil {
			t.Fatalf("search %s: %v", path, err)
		}
		if len(chunks) != 1 {
			t.Fatalf("%s: got %d chunks, want 1", path, len(chunks))
		}
		st.close()
	}
}
