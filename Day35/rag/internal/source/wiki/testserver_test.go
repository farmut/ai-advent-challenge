package wiki

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWiki is an httptest-backed stand-in for the wiki API. It records every
// request path, which is what lets a test assert on what was NOT fetched — the
// only way to prove that a denied subtree or a restricted page never had its
// content pulled.
type fakeWiki struct {
	t      *testing.T
	server *httptest.Server

	mu   sync.Mutex
	hits []string

	// handlers, keyed by the path prefix they serve.
	descendants func(slug, cursor string) (status int, body string, headers map[string]string)
	page        func(slug, id string) (status int, body string, headers map[string]string)
	access      func(pageID string) (status int, body string, headers map[string]string)

	// pageRaw takes precedence over page when a test needs the whole request —
	// the `fields` parameter above all, since the live API's decision to send a
	// body at all hinges on it.
	pageRaw func(r *http.Request) (status int, body string, headers map[string]string)
}

func newFakeWiki(t *testing.T) *fakeWiki {
	t.Helper()
	f := &fakeWiki{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeWiki) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()

	q := r.URL.Query()
	var status int
	var body string
	var headers map[string]string

	switch {
	case r.URL.Path == "/v1/pages/descendants":
		if f.descendants == nil {
			status, body = http.StatusNotFound, `{"error_code":"not_found"}`
		} else {
			status, body, headers = f.descendants(q.Get("slug"), q.Get("cursor"))
		}

	case strings.HasSuffix(r.URL.Path, "/access"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/pages/"), "/access")
		if f.access == nil {
			status, body = http.StatusOK, `{"access":"organization"}`
		} else {
			status, body, headers = f.access(id)
		}

	case r.URL.Path == "/v1/pages":
		switch {
		case f.pageRaw != nil:
			status, body, headers = f.pageRaw(r)
		case f.page != nil:
			status, body, headers = f.page(q.Get("slug"), q.Get("id"))
		default:
			status, body = http.StatusNotFound, `{"error_code":"not_found"}`
		}

	default:
		status, body = http.StatusNotFound, `{"error_code":"unknown_path"}`
	}

	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// hitLog returns a copy of the recorded requests.
func (f *fakeWiki) hitLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// count returns how many recorded requests contain substr.
func (f *fakeWiki) count(substr string) int {
	var n int
	for _, h := range f.hitLog() {
		if strings.Contains(h, substr) {
			n++
		}
	}
	return n
}

// requestedContentFor reports whether the page-content endpoint was hit for slug.
func (f *fakeWiki) requestedContentFor(slug string) bool {
	for _, h := range f.hitLog() {
		if strings.HasPrefix(h, "/v1/pages?") && strings.Contains(h, "slug="+strings.ReplaceAll(slug, "/", "%2F")) {
			return true
		}
		if strings.HasPrefix(h, "/v1/pages?") && strings.Contains(h, "slug="+slug) {
			return true
		}
	}
	return false
}

// newTestClient builds a Client pointed at the fake server, with backoff
// neutralised: sleeps are recorded rather than performed, so the retry ladder
// runs in microseconds and the schedule can still be asserted on.
func (f *fakeWiki) newTestClient(t *testing.T, tune func(*ClientConfig)) (*Client, *sleepLog) {
	t.Helper()

	sl := &sleepLog{}
	cfg := ClientConfig{
		APIURL:      f.server.URL,
		Token:       "test-token-value-1234567890",
		OrgID:       "org-1",
		OrgHeader:   "X-Org-Id",
		MaxRetries:  3,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  10 * time.Millisecond,
		Sleep:       sl.Sleep,
	}
	if tune != nil {
		tune(&cfg)
	}

	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, sl
}

// sleepLog records requested delays instead of waiting.
type sleepLog struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *sleepLog) Sleep(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, d)
}

func (s *sleepLog) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// pageJSON renders a minimal page-content response.
func pageJSON(id, slug, title, body string) string {
	return fmt.Sprintf(`{"id":%q,"slug":%q,"title":%q,"content":%q,"version":"v1"}`, id, slug, title, body)
}

// typedPageJSON is pageJSON plus the page_type and breadcrumbs that the live
// API returns.
func typedPageJSON(id, slug, title, body, pageType, breadcrumbs string) string {
	crumbs := "[]"
	if breadcrumbs != "" {
		var objs []string
		for _, c := range strings.Split(breadcrumbs, ">") {
			objs = append(objs, fmt.Sprintf(`{"title":%q,"slug":"x"}`, strings.TrimSpace(c)))
		}
		crumbs = "[" + strings.Join(objs, ",") + "]"
	}
	return fmt.Sprintf(`{"id":%q,"slug":%q,"title":%q,"content":%q,"page_type":%q,"breadcrumbs":%s,"version":"v1"}`,
		id, slug, title, body, pageType, crumbs)
}

// metadataOnlyJSON is the REAL response of GET /v1/pages when the request omits
// `fields`: four keys, and no body under any name. Reproduced verbatim from a
// live probe — id is a NUMBER there, not a string.
func metadataOnlyJSON(id int, slug, title string) string {
	return fmt.Sprintf(`{"id":%d,"page_type":"page","slug":%q,"title":%q}`, id, slug, title)
}

// fieldsOf returns the `fields` values of a request, flattening BOTH encodings
// so a test can assert on what was asked for without caring how it was written.
func fieldsOf(r *http.Request) []string {
	var out []string
	for _, v := range r.URL.Query()[FieldsParam] {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// asksFor reports whether the request requested the named field.
func asksFor(r *http.Request, field string) bool {
	for _, f := range fieldsOf(r) {
		if f == field {
			return true
		}
	}
	return false
}

// usesCommaEncoding reports whether `fields` arrived as one comma-joined value.
func usesCommaEncoding(r *http.Request) bool {
	vals := r.URL.Query()[FieldsParam]
	return len(vals) == 1 && strings.Contains(vals[0], ",")
}

// pageHits returns the recorded requests to the single-page endpoint.
func (f *fakeWiki) pageHits() []string {
	var out []string
	for _, h := range f.hitLog() {
		if strings.HasPrefix(h, pagePath+"?") {
			out = append(out, h)
		}
	}
	return out
}

// listJSON renders a descendants response. Each item is "id|slug|depth".
func listJSON(cursor string, items ...string) string {
	var parts []string
	for _, it := range items {
		f := strings.Split(it, "|")
		depth := "0"
		if len(f) > 2 {
			depth = f[2]
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"slug":%q,"title":%q,"depth":%s}`, f[0], f[1], f[1], depth))
	}
	body := `{"results":[` + strings.Join(parts, ",") + `]`
	if cursor != "" {
		body += fmt.Sprintf(`,"next_cursor":%q`, cursor)
	}
	return body + "}"
}

// longBody is prose long enough to clear the quality gate's minimum length.
const longBody = "Это содержательная страница документации. Здесь описан процесс оформления отпуска, сроки согласования и необходимые документы."
