package main

// One property, checked everywhere it can fail: the OAuth token must not end up
// in anything that outlives the process.
//
// This is a separate file from main_test.go because it is not a test of a
// feature. A wiki OAuth token reads the entire corporate wiki, and every
// artifact below is one somebody copies without thinking — a database handed to
// the support agent, a manifest attached to a review ticket, a terminal scroll
// pasted into a chat, a dump directory zipped up for a bug report. The leak
// classes are structural, so they are enumerated rather than sampled: whatever
// new artifact a future change writes, it lands in one of these directories and
// gets scanned by TestTokenNeverReachesAnyArtifact walking the whole tree.
//
// The token is searched for as RAW BYTES, not through any accessor. A test that
// asks the code whether it redacted something only ever confirms that redact()
// calls redact().

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rag/internal/store"
)

// newFakeEmbedServer returns an embeddings endpoint that answers every request
// with a fixed vector, so an index run reaches completion.
//
// It also asserts the wiki token never reaches the EMBEDDER. That is not a
// hypothetical: the wiki client and the embedder client are configured a few
// lines apart, both take a bearer-style credential, and the embedder is
// routinely a third-party host. Sending a wiki token there hands a corporate
// wiki to an unrelated vendor.
func newFakeEmbedServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(canaryToken)) {
			t.Error("the wiki OAuth token was sent to the embeddings endpoint in the request body")
		}
		for name, values := range r.Header {
			for _, v := range values {
				if strings.Contains(v, canaryToken) {
					t.Errorf("the wiki OAuth token was sent to the embeddings endpoint in header %s", name)
				}
			}
		}

		vec := make([]string, dim)
		for i := range vec {
			vec[i] = "0.01"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"embedding":[%s]}]}`, strings.Join(vec, ","))
	}))
}

// captureStdio redirects os.Stdout and os.Stderr for the duration of fn and
// returns everything written to either.
//
// The pipes are drained by goroutines started BEFORE fn runs. Reading after the
// fact would deadlock the moment the output exceeds the pipe buffer (64 KiB on
// Linux, 8 KiB by default on macOS) — an index run over a real wiki prints far
// more than that, so the naive version would hang exactly when the corpus gets
// big enough to matter.
func captureStdio(t *testing.T, fn func()) string {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	// One buffer per stream. Draining both into a shared buffer would be a data
	// race on bytes.Buffer — unsynchronised concurrent writes — which `go test
	// -race` rightly fails, and which corrupts the captured text even when it
	// does not. They are concatenated after both goroutines have finished.
	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	drain := func(dst *bytes.Buffer, r *os.File) {
		_, _ = io.Copy(dst, r)
		done <- struct{}{}
	}
	go drain(&outBuf, outR)
	go drain(&errBuf, errR)

	func() {
		defer func() {
			os.Stdout, os.Stderr = realOut, realErr
			_ = outW.Close()
			_ = errW.Close()
		}()
		fn()
	}()

	<-done
	<-done
	return outBuf.String() + errBuf.String()
}

// runIndexForLeakCheck performs a full, non-dry index run against fake servers
// and returns the working directory it wrote into plus everything it printed.
func runIndexForLeakCheck(t *testing.T) (dir, output string) {
	t.Helper()
	t.Setenv(envWikiToken, canaryToken)

	wikiSrv := newFakeWikiServer(t)
	defer wikiSrv.Close()
	embed := newFakeEmbedServer(t, 8)
	defer embed.Close()

	dir = t.TempDir()
	dumpDir := filepath.Join(dir, "dumps")

	var runErr error
	output = captureStdio(t, func() {
		runErr = cmdIndex([]string{
			"--source", "wiki",
			"--root", "docs",
			"--allow", "docs",
			"--org-id", "org-1",
			"--api-url", wikiSrv.URL,
			"--page-base-url", "https://wiki.example.com",
			"--embed-url", embed.URL,
			"--db", filepath.Join(dir, "wiki.db"),
			"--manifest", filepath.Join(dir, "manifest.json"),
			"--dump-dir", dumpDir,
		})
	})
	if runErr != nil {
		t.Fatalf("index run failed, so the leak check proves nothing: %v\noutput:\n%s", runErr, output)
	}
	return dir, output
}

// TestTokenNeverReachesAnyArtifact walks every file a real index run produced —
// database, manifest JSON, manifest table, dumps, journals, anything a future
// change adds — and fails if the token's bytes appear in any of them.
//
// Walking the tree rather than naming files is the point: the test keeps
// covering artifacts nobody has written yet.
func TestTokenNeverReachesAnyArtifact(t *testing.T) {
	dir, _ := runIndexForLeakCheck(t)

	needle := []byte(canaryToken)
	var scanned int

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		scanned++
		if bytes.Contains(data, needle) {
			rel, _ := filepath.Rel(dir, path)
			t.Errorf("the OAuth token appears verbatim in %s (%d bytes)", rel, len(data))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the run directory: %v", err)
	}

	// A run that wrote nothing would pass the scan above while proving nothing.
	if scanned == 0 {
		t.Fatal("no files were produced by the index run; the scan had nothing to examine")
	}
}

// TestTokenNeverReachesTheTerminal covers the artifact with the shortest path to
// a public place: the scrollback somebody pastes into a chat or a CI log.
func TestTokenNeverReachesTheTerminal(t *testing.T) {
	_, output := runIndexForLeakCheck(t)

	if strings.Contains(output, canaryToken) {
		t.Error("the OAuth token was printed to stdout/stderr")
	}
	// Guard against the check passing because nothing was printed at all.
	if strings.TrimSpace(output) == "" {
		t.Fatal("the index run printed nothing; there was no output to check")
	}
}

// TestIndexedDatabaseIsUsableAndTokenFree ties the two halves together: the
// database is a real index the agent can query AND it is token-free. Checked in
// one test because each half alone is satisfiable by a bug — an empty database
// leaks nothing, and a leaky one can still answer questions.
func TestIndexedDatabaseIsUsableAndTokenFree(t *testing.T) {
	dir, _ := runIndexForLeakCheck(t)
	dbPath := filepath.Join(dir, "wiki.db")

	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read the database: %v", err)
	}
	if bytes.Contains(data, []byte(canaryToken)) {
		t.Error("the OAuth token is stored in the database that gets shipped to the agent")
	}
	// The page body must be there — otherwise "no token in the database" is just
	// a statement about an empty file.
	if !bytes.Contains(data, []byte("Отпуск оформляется")) {
		t.Error("the indexed page body is not in the database; nothing was actually indexed")
	}

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer st.Close()
	n, err := st.DocumentCount(context.Background())
	if err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if n != 1 {
		t.Errorf("document count = %d, want 1 (docs/public; docs/secret/salaries is denied)", n)
	}
}
