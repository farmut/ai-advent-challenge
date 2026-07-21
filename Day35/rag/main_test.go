package main

// Tests for the CLI layer. Not one of them touches the real network: the wiki
// API and the embeddings endpoint are both httptest servers, and the cases that
// must fail before any request is issued assert exactly that.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rag/internal/source"
	"rag/internal/store"
)

// canaryToken is deliberately long and unique. wiki.NewClient registers the
// token as a secret and redact() then masks it as a SUBSTRING everywhere, so a
// short token like "t" would silently mangle every manifest field it appears in.
const canaryToken = "TOKEN-CANARY-0123456789-do-not-log-me"

// ---------------------------------------------------------------------------
// token resolution
// ---------------------------------------------------------------------------

func TestResolveWikiTokenFromEnv(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	got, err := resolveWikiToken("")
	if err != nil {
		t.Fatalf("resolveWikiToken: %v", err)
	}
	if got != canaryToken {
		t.Fatalf("token = %q, want the environment value", got)
	}
}

// TestResolveWikiTokenEnvBeatsFile pins the documented precedence: the
// environment wins, so a stale token file cannot silently override an explicitly
// exported one.
func TestResolveWikiTokenEnvBeatsFile(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)
	path := writeTokenFile(t, "from-the-file", 0o600)

	got, err := resolveWikiToken(path)
	if err != nil {
		t.Fatalf("resolveWikiToken: %v", err)
	}
	if got != canaryToken {
		t.Fatalf("token = %q, want the environment value to win", got)
	}
}

func TestResolveWikiTokenFilePermissions(t *testing.T) {
	tests := []struct {
		name    string
		mode    os.FileMode
		wantErr bool
	}{
		{"owner only is accepted", 0o600, false},
		{"read only for owner is accepted", 0o400, false},
		{"group readable is refused", 0o640, true},
		{"world readable is refused", 0o644, true},
		{"world writable is refused", 0o666, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Without this the developer's own exported token would satisfy the
			// lookup before the file is ever consulted.
			t.Setenv(envWikiToken, "")
			path := writeTokenFile(t, canaryToken, tc.mode)

			got, err := resolveWikiToken(path)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("mode %04o was accepted; a token file readable beyond its owner must be refused", tc.mode)
				}
				if !strings.Contains(err.Error(), "chmod 600") {
					t.Errorf("error does not tell the user how to fix it: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("mode %04o was refused: %v", tc.mode, err)
			}
			if got != canaryToken {
				t.Fatalf("token = %q, want %q", got, canaryToken)
			}
		})
	}
}

func TestResolveWikiTokenMissing(t *testing.T) {
	t.Setenv(envWikiToken, "")

	_, err := resolveWikiToken("")
	if err == nil {
		t.Fatal("expected an error when no token is available anywhere")
	}
	msg := err.Error()
	for _, want := range []string{envWikiToken, "export", "--token"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message is missing %q; it must tell the user what to do:\n%s", want, msg)
		}
	}
}

func writeTokenFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	// Set the mode explicitly: os.WriteFile applies the process umask, so the
	// requested mode is not what lands on disk.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod token file: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// flag validation happens before the network
// ---------------------------------------------------------------------------

// TestWikiIndexRequiresPageBaseURL is the guard against a support bot handing
// out links to the API host. The check must fire locally: the token is present
// and the API URL points at a server that would fail the test if contacted.
func TestWikiIndexRequiresPageBaseURL(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	var contacted int32
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&contacted, 1)
	}))
	defer unreachable.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wiki.db")

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "x",
		"--allow", "x",
		"--org-id", "org-1",
		"--api-url", unreachable.URL,
		"--db", dbPath,
		"--dry-run",
	})

	if err == nil {
		t.Fatal("indexing a wiki without --page-base-url succeeded; links would point at the API host")
	}
	if !strings.Contains(err.Error(), "--page-base-url") {
		t.Errorf("error does not name the missing flag: %v", err)
	}
	if n := atomic.LoadInt32(&contacted); n != 0 {
		t.Errorf("%d request(s) were issued; configuration must be validated before any network call", n)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Errorf("a database was created at %s despite the run failing", dbPath)
	}
}

func TestWikiIndexRejectsUnknownOrgHeader(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "x",
		"--allow", "x",
		"--org-id", "org-1",
		"--org-header", "X-Made-Up",
		"--page-base-url", "https://wiki.example.com",
		"--db", filepath.Join(t.TempDir(), "wiki.db"),
		"--dry-run",
	})
	if err == nil || !strings.Contains(err.Error(), "X-Cloud-Org-Id") {
		t.Fatalf("expected an error listing the accepted headers, got: %v", err)
	}
}

// TestWikiIndexFailsClosedWithoutAllow pins the fail-closed contract at the CLI
// boundary: a forgotten --allow must stop the run, not crawl everything.
func TestWikiIndexFailsClosedWithoutAllow(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "x",
		"--org-id", "org-1",
		"--page-base-url", "https://wiki.example.com",
		"--db", filepath.Join(t.TempDir(), "wiki.db"),
		"--dry-run",
	})
	if err == nil || !strings.Contains(err.Error(), "--allow") {
		t.Fatalf("expected a fail-closed error about --allow, got: %v", err)
	}
	// Refusing is only half the job. "Allow the roots you are crawling" is the
	// common intent, and a user who cannot see how a root maps onto a prefix is
	// tempted to reach for a wildcard — which is the outcome fail-closed exists
	// to prevent. So the message must suggest a command, built from THIS run's
	// roots rather than a generic placeholder.
	if !strings.Contains(err.Error(), "--allow x") {
		t.Errorf("the error does not suggest allowing the root that was given:\n%v", err)
	}
	// The other way in must be discoverable from the same message.
	if !strings.Contains(err.Error(), "--allow-file") {
		t.Errorf("the error does not mention --allow-file, which satisfies the same check:\n%v", err)
	}
}

func TestProbeRequiresSlugAndOrgWithoutNetwork(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	if err := cmdWikiProbe([]string{"--org-id", "org-1"}); err == nil ||
		!strings.Contains(err.Error(), "--slug") {
		t.Fatalf("expected a --slug error, got: %v", err)
	}
	if err := cmdWikiProbe([]string{"--slug", "docs"}); err == nil ||
		!strings.Contains(err.Error(), "--org-id") {
		t.Fatalf("expected an --org-id error, got: %v", err)
	}
}

// TestProbeWithoutTokenDoesNotCallTheNetwork is the headline safety property of
// `rag wiki probe`: no token means a readable message, not a panic and not a
// request.
func TestProbeWithoutTokenDoesNotCallTheNetwork(t *testing.T) {
	t.Setenv(envWikiToken, "")

	var contacted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&contacted, 1)
	}))
	defer srv.Close()

	err := cmdWikiProbe([]string{"--slug", "docs", "--org-id", "org-1", "--api-url", srv.URL})
	if err == nil {
		t.Fatal("probe without a token succeeded")
	}
	if !strings.Contains(err.Error(), envWikiToken) {
		t.Errorf("error does not mention %s: %v", envWikiToken, err)
	}
	if n := atomic.LoadInt32(&contacted); n != 0 {
		t.Errorf("%d request(s) issued without a token", n)
	}
}

// TestProbeAgainstFakeAPI exercises the happy path end to end and, through
// --dump-dir, captures the report so its two invariants can be asserted: it
// reaches a verdict on every field, and it never echoes a page's content.
func TestProbeAgainstFakeAPI(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	srv := newFakeWikiServer(t)
	defer srv.Close()

	dumpDir := filepath.Join(t.TempDir(), "dumps")

	err := cmdWikiProbe([]string{
		"--slug", "docs/public",
		"--org-id", "org-1",
		"--api-url", srv.URL,
		"--dump-dir", dumpDir,
	})
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dumpDir, "probe-*.txt"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one dump file, got %v (%v)", matches, err)
	}

	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("stat dump: %v", err)
	}
	// The report describes the layout of an internal wiki; it is owner-only.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("dump file mode = %04o, want 0600", perm)
	}

	report, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	text := string(report)

	for _, want := range []string{"page id", "body", "acl marker", "ParsePage OK", "ParsePageList OK"} {
		if !strings.Contains(text, want) {
			t.Errorf("probe report never mentions %q:\n%s", want, text)
		}
	}
	// The safety contract: shapes, never content.
	for _, forbidden := range []string{"Отпуск оформляется", canaryToken} {
		if strings.Contains(text, forbidden) {
			t.Errorf("probe report leaked %q", forbidden)
		}
	}
}

// ---------------------------------------------------------------------------
// dry run
// ---------------------------------------------------------------------------

// TestDryRunWritesManifestAndNoDatabase is the contract a reviewer relies on: a
// dry run produces something to read and changes nothing.
func TestDryRunWritesManifestAndNoDatabase(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	wikiSrv := newFakeWikiServer(t)
	defer wikiSrv.Close()

	// Any embedding request at all is a bug: a dry run must never reach the
	// embedder. This server fails the test if it is ever contacted.
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the embedder was called during a dry run (%s)", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer embed.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wiki.db")
	manifestPath := filepath.Join(dir, "manifest.json")

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "docs",
		"--allow", "docs",
		"--deny", "docs/secret",
		"--org-id", "org-1",
		"--api-url", wikiSrv.URL,
		"--page-base-url", "https://wiki.example.com",
		"--embed-url", embed.URL,
		"--db", dbPath,
		"--manifest", manifestPath,
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "docs/public") {
		t.Errorf("manifest does not mention the page that would be indexed:\n%s", body)
	}
	if !strings.Contains(body, `"decision": "indexed"`) {
		t.Errorf("manifest records no indexed page:\n%s", body)
	}
	// The denied page must appear as a decision, not vanish: the manifest is the
	// answer to "what does the bot know", and an omission is not an answer.
	if !strings.Contains(body, "deny_prefix") {
		t.Errorf("manifest does not record the denied subtree:\n%s", body)
	}

	// A human-reviewable table is written alongside the JSON.
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		t.Errorf("manifest table was not written: %v", err)
	}

	// The point of the whole exercise.
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("a dry run created a database at %s", dbPath)
	}
	assertNoTempDatabases(t, dir)

	if strings.Contains(body, canaryToken) {
		t.Error("the OAuth token leaked into the manifest")
	}
}

// ---------------------------------------------------------------------------
// atomicity
// ---------------------------------------------------------------------------

// TestIndexFailureLeavesLiveDatabaseUntouched covers the failure mode that is
// worst precisely because it looks like success: a half-written index answers
// confidently while missing pages. A failed run must leave the previous index
// byte-for-byte intact.
func TestIndexFailureLeavesLiveDatabaseUntouched(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wiki.db")
	seedDatabase(t, dbPath)
	before := hashFile(t, dbPath)

	wikiSrv := newFakeWikiServer(t)
	defer wikiSrv.Close()

	// The embedder dies partway: the crawl and chunking succeed, the write does
	// not. This is the realistic mid-run failure.
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model is loading"}}`, http.StatusInternalServerError)
	}))
	defer embed.Close()

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "docs",
		"--allow", "docs",
		"--org-id", "org-1",
		"--api-url", wikiSrv.URL,
		"--page-base-url", "https://wiki.example.com",
		"--embed-url", embed.URL,
		"--db", dbPath,
		"--manifest", filepath.Join(dir, "manifest.json"),
	})
	if err == nil {
		t.Fatal("indexing succeeded even though every embedding call failed")
	}

	if after := hashFile(t, dbPath); after != before {
		t.Error("the live database changed during a failed run; a partial index must never be published")
	}
	assertNoTempDatabases(t, dir)

	// And the pre-existing content is still queryable.
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open the live database after the failed run: %v", err)
	}
	defer st.Close()
	n, err := st.DocumentCount(context.Background())
	if err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if n != 1 {
		t.Errorf("document count = %d, want the 1 document that was there before the failed run", n)
	}
}

// TestUnchangedSkipsReindex pins the incremental rule: the content hash decides
// when it is known, the source's version marker is the fallback for rows written
// before hashes were recorded.
func TestUnchangedSkipsReindex(t *testing.T) {
	const content = "the body of a wiki page"
	hash := contentHash(content)

	tests := []struct {
		name  string
		state store.DocumentState
		doc   source.Document
		want  bool
	}{
		{"same hash", store.DocumentState{ContentHash: hash}, source.Document{}, true},
		{"different hash", store.DocumentState{ContentHash: "other"}, source.Document{Version: "v1"}, false},
		{"no hash, same version", store.DocumentState{Version: "v1"}, source.Document{Version: "v1"}, true},
		{"no hash, new version", store.DocumentState{Version: "v1"}, source.Document{Version: "v2"}, false},
		{"no hash, no version", store.DocumentState{}, source.Document{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := unchanged(tc.state, tc.doc, hash); got != tc.want {
				t.Errorf("unchanged() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestIncrementalSkipWorksWithoutAnyVersionField is the end-to-end proof for the
// ONLY mechanism that can skip an unchanged wiki page.
//
// The live API returns no version, revision or updated_at field of any kind — a
// probe confirmed the response carries exactly id, page_type, slug, title,
// content and breadcrumbs. So source.Document.Version is empty for every wiki
// page, and sha256(content) is not a fallback here, it is the whole mechanism.
// If it regresses, every run re-embeds the entire wiki: slow, expensive against
// a paid embedding endpoint, and invisible — the index stays correct, so nothing
// fails, it just quietly costs more every time.
func TestIncrementalSkipWorksWithoutAnyVersionField(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	// A wiki whose page response has NO version field at all — the live shape.
	const bodyText = "Отпуск оформляется через кадровый портал не позднее чем за две недели. " +
		"Заявление подписывает руководитель подразделения, после чего кадры вносят его в систему."

	wikiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/pages/descendants":
			fmt.Fprint(w, `{"results":[{"id":"p1","slug":"docs/public","title":"Отпуск","depth":1}]}`)
		case strings.HasSuffix(r.URL.Path, "/access"):
			// Exactly what the live API does.
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprint(w, `{"error_code":"method_not_allowed"}`)
		case r.URL.Path == "/v1/pages":
			fmt.Fprintf(w, `{"id":"p1","page_type":"cloudpg","slug":"docs/public","title":"Отпуск","content":%q}`, bodyText)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error_code":"unknown_path"}`)
		}
	}))
	defer wikiSrv.Close()

	var embedCalls int32
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&embedCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
	}))
	defer embed.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wiki.db")

	run := func() {
		t.Helper()
		err := cmdIndex([]string{
			"--source", "wiki",
			"--root", "docs",
			"--allow", "docs",
			"--org-id", "org-1",
			"--api-url", wikiSrv.URL,
			"--page-base-url", "https://wiki.yandex.ru",
			"--embed-url", embed.URL,
			"--db", dbPath,
			"--manifest", filepath.Join(dir, "manifest.json"),
		})
		if err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	run()
	first := atomic.LoadInt32(&embedCalls)
	if first == 0 {
		t.Fatal("the first run embedded nothing — there is no baseline to skip against")
	}

	// The stored state must show that NO API revision marker exists: wiki.go
	// falls back to the content hash for Version, so the two are identical. If a
	// real version ever started arriving they would differ, and that is the signal
	// that this test's premise has changed.
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	state, found, err := st.DocumentState(context.Background(), "p1")
	if err != nil || !found {
		t.Fatalf("DocumentState: found=%t err=%v", found, err)
	}
	if state.ContentHash == "" {
		t.Fatal("no content hash was stored, so nothing can ever be skipped")
	}
	if state.Version != state.ContentHash {
		t.Fatalf("stored Version = %q but ContentHash = %q — the fixture sends no version field, "+
			"so Version must be the hash standing in for one", state.Version, state.ContentHash)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Second run, same content: the page must be skipped and NOTHING re-embedded.
	run()
	if got := atomic.LoadInt32(&embedCalls); got != first {
		t.Errorf("the second run made %d additional embedding call(s), want 0 — "+
			"the content-hash skip is the only incremental mechanism this source has",
			got-first)
	}
}

func TestResolveManifestPath(t *testing.T) {
	if got := resolveManifestPath("", "/tmp/wiki.db"); got != "/tmp/wiki.manifest.json" {
		t.Errorf("derived manifest path = %q", got)
	}
	if got := resolveManifestPath("/tmp/explicit.json", "/tmp/wiki.db"); got != "/tmp/explicit.json" {
		t.Errorf("explicit manifest path = %q", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newFakeWikiServer serves the three endpoints the crawler uses, for a tiny tree
// with one indexable page and one denied page.
func newFakeWikiServer(t *testing.T) *httptest.Server {
	t.Helper()

	const bodyText = "Отпуск оформляется через кадровый портал не позднее чем за две недели. " +
		"Заявление подписывает руководитель подразделения, после чего кадры вносят его в систему."

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth "+canaryToken {
			t.Errorf("Authorization header = %q", got)
		}
		if got := r.Header.Get("X-Org-Id"); got != "org-1" {
			t.Errorf("X-Org-Id header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/v1/pages/descendants":
			fmt.Fprint(w, `{"results":[
				{"id":"p1","slug":"docs/public","title":"Отпуск","depth":1},
				{"id":"p2","slug":"docs/secret/salaries","title":"Зарплаты","depth":2}
			]}`)

		case strings.HasSuffix(r.URL.Path, "/access"):
			fmt.Fprint(w, `{"access":"organization"}`)

		case r.URL.Path == "/v1/pages":
			fmt.Fprintf(w, `{"id":"p1","slug":"docs/public","title":"Отпуск","content":%q,"version":"rev-7"}`, bodyText)

		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error_code":"unknown_path"}`)
		}
	}))
}

// seedDatabase creates a live index holding one document, so a later failure has
// something it could destroy.
func seedDatabase(t *testing.T, path string) {
	t.Helper()

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("seed: open store: %v", err)
	}
	err = st.SaveDocument(context.Background(), store.DocumentState{
		Source:      "pre-existing",
		Title:       "Already indexed",
		URL:         "https://wiki.example.com/pre-existing",
		Version:     "rev-1",
		ContentHash: contentHash("old content"),
		ChunkCount:  1,
	})
	if err != nil {
		t.Fatalf("seed: save document: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("seed: close store: %v", err)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// assertNoTempDatabases proves the failure path cleaned up after itself: a
// leftover .tmp-<pid> file next to a live index is both confusing and, on a
// small disk, expensive.
func assertNoTempDatabases(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("temporary databases were left behind: %v", matches)
	}
}

// TestEmptyResultExplainsTheFilter covers the outcome that is dangerous because
// it looks like success: the crawl reaches real pages, the filter drops all of
// them, and the run exits 0 with a manifest and a calm tally. The resulting
// index answers every support question with "I don't know".
//
// The mistake reproduced here is the one that actually happened: a page URL was
// pasted where a slug belongs.
func TestEmptyResultExplainsTheFilter(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	const root = "team/docs/user-guide"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/pages/descendants" {
			fmt.Fprintf(w, `{"results":[
				{"id":"p1","slug":%q,"title":"A","depth":0},
				{"id":"p2","slug":%q,"title":"B","depth":1}]}`,
				root, root+"/setup")
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error_code":"unexpected_path"}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "m.json")

	out := captureStdio(t, func() {
		if err := cmdIndex([]string{
			"--source", "wiki",
			"--root", root,
			"--allow", "https://wiki.example.com/" + root, // the mistake
			"--org-id", "org-1",
			"--api-url", srv.URL,
			"--page-base-url", "https://wiki.example.com",
			"--db", filepath.Join(dir, "w.db"),
			"--manifest", manifestPath,
			"--dry-run",
		}); err != nil {
			t.Errorf("dry run failed: %v", err)
		}
	})

	// The diagnosis must fire, and must not blame the parts that demonstrably work.
	if !strings.Contains(out, "NOTHING WAS INDEXED") {
		t.Fatalf("a run that kept no pages printed no diagnosis:\n%s", out)
	}
	// The rule that decided, so the operator need not reconstruct it from shell history.
	if !strings.Contains(out, "Allow rules used") || !strings.Contains(out, "wiki.example.com") {
		t.Errorf("the diagnosis does not show the allow rule that was used:\n%s", out)
	}
	// The ground truth the rule failed to match.
	if !strings.Contains(out, root+"/setup") {
		t.Errorf("the diagnosis does not show the slugs the API returned:\n%s", out)
	}
	// The fix, as the common prefix — not a leaf slug, which would keep one page
	// and look like a cure.
	if !strings.Contains(out, "--allow "+root+"\n") {
		t.Errorf("the diagnosis does not suggest the common prefix %q:\n%s", root, out)
	}

	// The rules must also survive in the manifest, so a reviewer can diff a dry
	// run against the real one and see that they were filtered alike.
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	if !strings.Contains(string(data), `"allow"`) {
		t.Errorf("the manifest does not record the allow rules:\n%s", data)
	}
}

// TestWikiIndexRejectsRootAllowRule pins the refusal at the boundary where the
// user can still see their own command. NewFilter would drop the rule and leave
// the run failing closed with "no_allow_rule" — safe, but it reads as "you
// forgot --allow" to someone looking straight at the --allow they typed.
func TestWikiIndexRejectsRootAllowRule(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	var contacted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&contacted, 1)
	}))
	defer srv.Close()

	for _, rule := range []string{"/", ".", "//"} {
		t.Run(rule, func(t *testing.T) {
			err := cmdIndex([]string{
				"--source", "wiki",
				"--root", "docs",
				"--allow", rule,
				"--org-id", "org-1",
				"--api-url", srv.URL,
				"--page-base-url", "https://wiki.example.com",
				"--db", filepath.Join(t.TempDir(), "w.db"),
				"--dry-run",
			})
			if err == nil {
				t.Fatalf("--allow %q was accepted; a root-level rule matches every slug and would crawl the whole wiki", rule)
			}
			// The message must quote the rejected rule, or the user cannot tell
			// which of their flags is the problem.
			if !strings.Contains(err.Error(), rule) {
				t.Errorf("the error does not quote the rejected rule %q: %v", rule, err)
			}
			// And it must say what to do instead, built from this run's roots.
			if !strings.Contains(err.Error(), "--allow docs") {
				t.Errorf("the error does not suggest naming the sections: %v", err)
			}
		})
	}

	if n := atomic.LoadInt32(&contacted); n != 0 {
		t.Errorf("%d request(s) were issued; a rejected filter must stop the run before the network", n)
	}
}

// TestWikiIndexRejectsRootDenyRule is the mirror image: a root-level deny
// excludes every page, so the run indexes nothing while looking configured.
func TestWikiIndexRejectsRootDenyRule(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	err := cmdIndex([]string{
		"--source", "wiki",
		"--root", "docs",
		"--allow", "docs",
		"--deny", "/",
		"--org-id", "org-1",
		"--page-base-url", "https://wiki.example.com",
		"--db", filepath.Join(t.TempDir(), "w.db"),
		"--dry-run",
	})
	if err == nil || !strings.Contains(err.Error(), "--deny") {
		t.Fatalf("expected a refusal naming --deny, got: %v", err)
	}
}

// TestWikiIndexRejectsGlobRules covers the other spelling of "everything". Unlike
// "/", a glob survives normalisation as a literal rule matching no real slug, so
// the run walks the wiki, keeps nothing, and reports it in the same calm voice as
// a successful crawl — configured-looking and empty.
func TestWikiIndexRejectsGlobRules(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	var contacted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&contacted, 1)
	}))
	defer srv.Close()

	cases := []struct{ flag, rule string }{
		{"--allow", "*"},
		{"--allow", "docs/*"},
		{"--allow", "docs/**/api"},
		{"--allow", "docs/?ublic"},
		{"--deny", "docs/hr*"},
	}
	for _, tc := range cases {
		t.Run(tc.flag+" "+tc.rule, func(t *testing.T) {
			args := []string{
				"--source", "wiki",
				"--root", "docs/team/",
				"--org-id", "org-1",
				"--api-url", srv.URL,
				"--page-base-url", "https://wiki.example.com",
				"--db", filepath.Join(t.TempDir(), "w.db"),
				"--dry-run",
				tc.flag, tc.rule,
			}
			if tc.flag == "--deny" {
				args = append(args, "--allow", "docs")
			}

			err := cmdIndex(args)
			if err == nil {
				t.Fatalf("%s %q was accepted; a glob matches no real slug and yields a silently empty index", tc.flag, tc.rule)
			}
			if !strings.Contains(err.Error(), tc.rule) {
				t.Errorf("the error does not quote the rejected rule %q: %v", tc.rule, err)
			}
			// The suggested replacement must be the NORMALISED root — the form the
			// filter uses and the manifest records — not the raw trailing-slash
			// spelling that was typed.
			if !strings.Contains(err.Error(), "--allow docs/team\n") {
				t.Errorf("the error does not suggest the normalised root: %v", err)
			}
		})
	}

	if n := atomic.LoadInt32(&contacted); n != 0 {
		t.Errorf("%d request(s) were issued; a rejected filter must stop the run before the network", n)
	}
}

// TestEmptyResultBlamesTheACLCheckNotTheFilter guards a misdiagnosis that would
// cost real time: an earlier cut of the empty-result explanation always accused
// the filter, so a run rejected entirely by the permission check would have sent
// the operator editing --allow rules that were already correct.
func TestEmptyResultBlamesTheACLCheckNotTheFilter(t *testing.T) {
	t.Setenv(envWikiToken, canaryToken)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/access"):
			// The live API's answer: the endpoint does not exist.
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprint(w, `{"debug_message":"Method not allowed"}`)
		case r.URL.Path == "/v1/pages/descendants":
			fmt.Fprint(w, `{"results":[
				{"id":"p1","slug":"docs/a","title":"A","depth":0},
				{"id":"p2","slug":"docs/b","title":"B","depth":0}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error_code":"unexpected_path"}`)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := captureStdio(t, func() {
		if err := cmdIndex([]string{
			"--source", "wiki",
			"--root", "docs",
			"--allow", "docs", // correct, and must not be blamed
			"--acl-mode", "require",
			"--org-id", "org-1",
			"--api-url", srv.URL,
			"--page-base-url", "https://wiki.example.com",
			"--db", filepath.Join(dir, "w.db"),
			"--manifest", filepath.Join(dir, "m.json"),
			"--dry-run",
		}); err != nil {
			t.Errorf("dry run failed: %v", err)
		}
	})

	if !strings.Contains(out, "PERMISSION check") {
		t.Fatalf("the diagnosis does not name the permission check as the cause:\n%s", out)
	}
	// The correct rules must not be paraded as suspects.
	if strings.Contains(out, "Allow rules used") {
		t.Errorf("the diagnosis blamed the filter, which accepted every page:\n%s", out)
	}
	// It must state the policy choice rather than just naming a flag, so the
	// reflex is not to reach for --acl-mode=off.
	for _, want := range []string{"--acl-mode=probe", "--acl-mode=off", "reading the manifest"} {
		if !strings.Contains(out, want) {
			t.Errorf("the diagnosis does not mention %q:\n%s", want, out)
		}
	}
}
