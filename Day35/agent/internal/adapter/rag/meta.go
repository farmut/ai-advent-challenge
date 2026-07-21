package rag

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// IndexedAt reports when the index at path was last built, read from the
// `index_meta` table's `indexed_at` key (RFC3339, written by `rag index`).
//
// The bool is false — with a nil error — when the age simply cannot be known:
// the table is missing (an index built before index_meta existed) or the key is
// unset. That is deliberately not an error: an old index must stay readable.
// Callers that enforce a freshness policy decide what an unknown age means; the
// support mode treats it as "cannot prove it is fresh".
func IndexedAt(path string) (time.Time, bool, error) {
	if _, err := os.Stat(path); err != nil {
		return time.Time{}, false, fmt.Errorf("index %q not found: %w", path, err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return time.Time{}, false, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var raw string
	err = db.QueryRow(`SELECT value FROM index_meta WHERE key = 'indexed_at'`).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return time.Time{}, false, nil // key not set
	case err != nil:
		// Most likely "no such table: index_meta" — an old-schema index. Not an
		// error: unknown age, same as a missing key.
		return time.Time{}, false, nil
	}

	ts, perr := time.Parse(time.RFC3339, raw)
	if perr != nil {
		// A value we cannot parse is worse than a missing one: something wrote a
		// timestamp in a format nobody agreed on. Surface it instead of silently
		// treating the index as ageless.
		return time.Time{}, false, fmt.Errorf("index %q: unparseable indexed_at %q: %w", path, raw, perr)
	}
	return ts, true, nil
}
