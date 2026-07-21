package wiki

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The finding that motivates this whole file: a live GET /v1/pages?slug=… came
// back with FOUR keys — id, page_type, slug, title — and no body under any of
// the nine candidate names. The body is not misnamed, it is simply not sent
// unless `fields` asks for it. A client that forgets the parameter does not
// fail: it indexes nothing, quietly.
func TestPageRequestAsksForContent(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		if !asksFor(r, "content") {
			// Exactly what the real API does.
			return http.StatusOK, metadataOnlyJSON(48952013, "docs/a", "A"), nil
		}
		return http.StatusOK, pageJSON("1", "docs/a", "A", longBody), nil
	}

	docs, _, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("indexed %d documents, want 1 — the body never arrived, so `fields` was not sent", len(docs))
	}
	if !strings.Contains(docs[0].Content, "отпуска") {
		t.Errorf("content = %q, want the page body", docs[0].Content)
	}

	// Prove the parameter went out on the wire, not merely that the fake was kind.
	hits := f.pageHits()
	if len(hits) == 0 {
		t.Fatal("the page endpoint was never called")
	}
	for _, h := range hits {
		if !strings.Contains(h, FieldsParam+"=content") {
			t.Errorf("page request %q carries no %s=content", h, FieldsParam)
		}
		if !strings.Contains(h, "breadcrumbs") {
			t.Errorf("page request %q does not ask for breadcrumbs", h)
		}
	}
	t.Logf("page request as sent: %s", hits[0])
}

// Definition of Done: show the exact URL the client builds. This is the string
// a human compares against the API documentation when something does not work,
// so it is asserted rather than merely printed — a silent change to the query
// shape is the kind of regression that reads as "the wiki has no content".
func TestPageRequestURL(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)

	c, err := NewClient(ClientConfig{
		APIURL:    "https://api.wiki.yandex.net",
		Token:     "url-shape-test-token-value",
		OrgID:     "org-1",
		OrgHeader: "X-Org-Id",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	fetcher := NewPageFetcher(c, io.Discard)
	got := fetcher.RequestURL("docs/public/faq", "")
	t.Logf("PAGE REQUEST URL (default, comma encoding): %s", got)

	// Confirmed against the live API: ONE `fields` parameter carrying a
	// comma-joined list. The repeated form was ignored and answered with metadata
	// only, so this must not silently drift back.
	const want = "https://api.wiki.yandex.net/v1/pages?fields=content%2Cbreadcrumbs&slug=docs%2Fpublic%2Ffaq"
	if got != want {
		t.Errorf("page request URL\n got: %s\nwant: %s", got, want)
	}

	// And the repeated variant, which is now only the fallback.
	fetcher.style = fieldsRepeated
	got = fetcher.RequestURL("docs/public/faq", "")
	t.Logf("PAGE REQUEST URL (fallback, repeated):      %s", got)
	const wantRepeated = "https://api.wiki.yandex.net/v1/pages?fields=content&fields=breadcrumbs&slug=docs%2Fpublic%2Ffaq"
	if got != wantRepeated {
		t.Errorf("repeated-encoding URL\n got: %s\nwant: %s", got, wantRepeated)
	}
}

// DEFINITION OF DONE for the encoding fix: on a fresh fetcher the very first
// request already carries fields as a SINGLE comma-joined value. Asserted on the
// wire, not on the URL builder, because what matters is what the server sees.
func TestDefaultEncodingIsOneCommaJoinedFieldsParam(t *testing.T) {
	f := newFakeWiki(t)
	var (
		gotRaw    []string
		gotValues []string
	)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		gotRaw = append(gotRaw, r.URL.RawQuery)
		gotValues = r.URL.Query()[FieldsParam]
		return http.StatusOK, pageJSON("1", "docs/a", "A", longBody), nil
	}

	c, _ := f.newTestClient(t, nil)
	if _, _, err := NewPageFetcher(c, io.Discard).Fetch(context.Background(), "docs/a", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(gotRaw) != 1 {
		t.Fatalf("made %d requests for one page, want 1 — the confirmed encoding must be tried first:\n%s",
			len(gotRaw), strings.Join(gotRaw, "\n"))
	}
	t.Logf("query as the server saw it: %s", gotRaw[0])

	if len(gotValues) != 1 {
		t.Fatalf("%s arrived as %d separate parameters, want 1 comma-joined value: %q", FieldsParam, len(gotValues), gotValues)
	}
	if gotValues[0] != "content,breadcrumbs" {
		t.Errorf("%s = %q, want %q", FieldsParam, gotValues[0], "content,breadcrumbs")
	}
	if !strings.Contains(gotRaw[0], "fields=content%2Cbreadcrumbs") {
		t.Errorf("raw query = %q, want the comma-encoded fields parameter", gotRaw[0])
	}
}

// The confirmed encoding is tried first, so the ordinary crawl costs exactly one
// request per page. This is the point of confirming it: the previous default
// spent a second request on the first pages of every run rediscovering an answer
// the live API had already given us.
func TestCommaEncodingCostsOneRequestPerPage(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b"), nil
	}
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		slug := r.URL.Query().Get("slug")
		if !usesCommaEncoding(r) {
			// The live API's behaviour: repeated parameters are ignored, so the
			// response carries no body.
			return http.StatusOK, metadataOnlyJSON(1, slug, "T"), nil
		}
		return http.StatusOK, pageJSON("id-"+slug, slug, "T", longBody), nil
	}

	docs, _, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("indexed %d documents, want 2", len(docs))
	}
	if n := len(f.pageHits()); n != 2 {
		t.Errorf("made %d page requests for 2 pages, want 2 — no negotiation should have happened:\n%s",
			n, strings.Join(f.pageHits(), "\n"))
	}
}

// The mirror case, and the reason the fallback survives: a deployment that only
// understands repeated parameters must still be crawlable rather than silently
// empty — and the working encoding must be learned ONCE, not re-tried on every
// page, because doubling every request against a rate-limited corporate API is
// its own outage.
func TestFieldsEncodingFallsBackToRepeated(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b", "3|docs/c"), nil
	}
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		slug := r.URL.Query().Get("slug")
		if usesCommaEncoding(r) {
			return http.StatusOK, metadataOnlyJSON(1, slug, "T"), nil
		}
		return http.StatusOK, pageJSON("id-"+slug, slug, "T", longBody), nil
	}

	var log bytes.Buffer
	docs, _, err := runSource(t, f, func(cfg *Config) { cfg.Log = &log })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("indexed %d documents, want 3 — the repeated fallback did not take effect", len(docs))
	}

	// One page paid for the negotiation; the remaining two must not.
	hits := f.pageHits()
	if len(hits) != 4 {
		t.Errorf("made %d page requests, want 4 (2 for the first page, 1 each after):\n%s",
			len(hits), strings.Join(hits, "\n"))
	}
	for _, h := range hits[2:] {
		if strings.Contains(h, "%2C") { // the encoded comma
			t.Errorf("request %q did not reuse the encoding that worked", h)
		}
	}

	if !strings.Contains(log.String(), "repeated") {
		t.Errorf("the chosen encoding was not logged; log:\n%s", log.String())
	}
	if strings.Count(log.String(), "array encoding") != 1 {
		t.Errorf("the encoding should be announced exactly once; log:\n%s", log.String())
	}
}

// Negotiation must not run forever. A wiki whose pages genuinely have no body
// would otherwise double every request of the entire crawl to answer a question
// that has no answer.
func TestFieldsNegotiationGivesUpAfterABudget(t *testing.T) {
	f := newFakeWiki(t)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		return http.StatusOK, metadataOnlyJSON(7, r.URL.Query().Get("slug"), "T"), nil
	}

	c, _ := f.newTestClient(t, nil)
	var log bytes.Buffer
	fetcher := NewPageFetcher(c, &log)

	for i := 0; i < maxNegotiationAttempts+2; i++ {
		if _, _, err := fetcher.Fetch(context.Background(), "docs/a", ""); err == nil {
			t.Fatal("Fetch reported success on a body-less response")
		}
	}

	want := 2*maxNegotiationAttempts + 2
	if n := len(f.pageHits()); n != want {
		t.Errorf("made %d requests, want %d (%d negotiated pages at 2 requests, then 1 each)",
			n, want, maxNegotiationAttempts)
	}
	if !strings.Contains(log.String(), "bodyPaths") {
		t.Errorf("giving up did not point at the file to fix; log:\n%s", log.String())
	}
}

// A body-less response must still yield everything else it contained. The probe
// depends on this, and so does any future diagnostic.
func TestFetchReturnsPartialPageOnMissingBody(t *testing.T) {
	f := newFakeWiki(t)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		return http.StatusOK, metadataOnlyJSON(48952013, "docs/a", "Заголовок"), nil
	}

	c, _ := f.newTestClient(t, nil)
	page, raw, err := NewPageFetcher(c, io.Discard).Fetch(context.Background(), "docs/a", "")
	if err == nil {
		t.Fatal("a response with no body must be an error")
	}
	if raw == nil {
		t.Error("the raw response was dropped along with the error")
	}
	if page.ID != "48952013" {
		t.Errorf("ID = %q, want the id parsed out of the metadata-only response", page.ID)
	}
	if page.Title != "Заголовок" || page.PageType != "page" {
		t.Errorf("partial page = %+v, want title and page_type filled in", page)
	}
	// The error must name the actual cause, since this is THE failure mode of
	// this API and the wrong diagnosis costs an afternoon in dto.go.
	if !strings.Contains(err.Error(), FieldsParam+"=content") {
		t.Errorf("error does not mention the missing parameter: %v", err)
	}
}
