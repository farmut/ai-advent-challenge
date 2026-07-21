package wiki

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"rag/internal/source"
)

// runSource wires a Source over the fake server and collects the documents it
// yields plus the manifest it produced.
func runSource(t *testing.T, f *fakeWiki, tune func(*Config)) ([]source.Document, *Manifest, error) {
	t.Helper()

	c, _ := f.newTestClient(t, nil)
	cfg := Config{
		Client:      c,
		Filter:      NewFilter([]string{"docs"}, nil),
		Roots:       []string{"docs"},
		PageBaseURL: "https://wiki.example.com",
	}
	if tune != nil {
		tune(&cfg)
	}

	src, err := New(cfg)
	if err != nil {
		return nil, nil, err
	}

	var docs []source.Document
	err = src.Iterate(context.Background(), func(d source.Document) error {
		docs = append(docs, d)
		return nil
	})
	return docs, src.Manifest(), err
}

// entryFor finds the manifest entry for a slug.
func entryFor(m *Manifest, slug string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Slug == slug {
			return e, true
		}
	}
	return Entry{}, false
}

// Case 6a: a page whose ACL is restricted must not be indexed, and the manifest
// must say why — this is the check that keeps an internal page out of a support
// chat, so its outcome has to be auditable.
func TestACLRestrictedPageIsNotIndexed(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/open", "2|docs/secret"), nil
	}
	f.access = func(pageID string) (int, string, map[string]string) {
		if pageID == "2" {
			return http.StatusOK, `{"access":"team"}`, nil
		}
		return http.StatusOK, `{"access":"organization"}`, nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON(strings.TrimPrefix(slug, "docs/"), slug, "T", longBody), nil
	}

	docs, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(docs) != 1 || docs[0].Path != "docs/open" {
		t.Fatalf("indexed %+v, want only docs/open", docs)
	}

	e, ok := entryFor(m, "docs/secret")
	if !ok {
		t.Fatal("the restricted page has no manifest entry — every decision must be recorded")
	}
	if e.Decision != DecisionSkipped || e.Reason != ReasonACLRestrict {
		t.Errorf("manifest entry = {%s, %s}, want {%s, %s}", e.Decision, e.Reason, DecisionSkipped, ReasonACLRestrict)
	}

	// The restricted page's CONTENT must never have been fetched.
	if f.requestedContentFor("docs/secret") {
		t.Error("the content of a restricted page was fetched — the ACL check must gate the content request")
	}
}

// Case 6b: an ACL endpoint failure must skip the page. Assuming "open" on error
// is exactly the mistake that leaks a document.
func TestACLErrorSkipsPage(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/open", "2|docs/broken"), nil
	}
	f.access = func(pageID string) (int, string, map[string]string) {
		if pageID == "2" {
			return http.StatusInternalServerError, `{"error_code":"internal"}`, nil
		}
		return http.StatusOK, `{"access":"organization"}`, nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("x", slug, "T", longBody), nil
	}

	docs, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	for _, d := range docs {
		if d.Path == "docs/broken" {
			t.Fatal("a page whose ACL could not be determined was indexed — must fail closed")
		}
	}

	e, ok := entryFor(m, "docs/broken")
	if !ok {
		t.Fatal("no manifest entry for the ACL-error page")
	}
	if e.Reason != ReasonACLError {
		t.Errorf("reason = %q, want %q", e.Reason, ReasonACLError)
	}
	if f.requestedContentFor("docs/broken") {
		t.Error("content was fetched despite the ACL check failing")
	}
}

// An unrecognised ACL shape is recorded distinctly from a recognised-but-closed
// one, because the two need different fixes: one is a dto.go bug, the other is
// a genuinely restricted page.
func TestACLUnknownIsRecordedSeparately(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/weird"), nil
	}
	f.access = func(string) (int, string, map[string]string) {
		return http.StatusOK, `{"totally":"unexpected"}`, nil
	}

	docs, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("indexed %d documents on an unparseable ACL, want 0", len(docs))
	}

	e, _ := entryFor(m, "docs/weird")
	if e.Reason != ReasonACLUnknown {
		t.Errorf("reason = %q, want %q", e.Reason, ReasonACLUnknown)
	}
}

// Case 7: a denied prefix must cost ZERO content requests. Verified against the
// server's own request log, not against our intentions.
func TestDenyPrefixNeverFetchesContent(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/public", "2|docs/hr/salaries", "3|docs/hr/reviews"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("x", slug, "T", longBody), nil
	}

	docs, m, err := runSource(t, f, func(cfg *Config) {
		cfg.Filter = NewFilter([]string{"docs"}, []string{"docs/hr"})
	})
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(docs) != 1 || docs[0].Path != "docs/public" {
		t.Fatalf("indexed %+v, want only docs/public", docs)
	}

	for _, slug := range []string{"docs/hr/salaries", "docs/hr/reviews"} {
		if f.requestedContentFor(slug) {
			t.Errorf("content of denied page %q was fetched; request log:\n%s",
				slug, strings.Join(f.hitLog(), "\n"))
		}
		e, ok := entryFor(m, slug)
		if !ok {
			t.Errorf("no manifest entry for denied page %q", slug)
			continue
		}
		if !strings.HasPrefix(e.Reason, "deny_prefix") {
			t.Errorf("reason for %q = %q, want deny_prefix", slug, e.Reason)
		}
	}

	// A denied page must not cost an ACL request either.
	if f.count("/access") > 1 {
		t.Errorf("made %d ACL requests, want 1 (only for the allowed page)", f.count("/access"))
	}
}

// Fail-closed at the source level: no allow rules means nothing is fetched.
func TestEmptyAllowIndexesNothing(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		t.Error("content was fetched despite an empty allow list")
		return http.StatusOK, pageJSON("x", slug, "T", longBody), nil
	}

	docs, m, err := runSource(t, f, func(cfg *Config) { cfg.Filter = NewFilter(nil, nil) })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("indexed %d documents with no allow rules, want 0", len(docs))
	}
	for _, e := range m.Entries {
		if e.Reason != "no_allow_rule" {
			t.Errorf("entry %+v: want reason no_allow_rule", e)
		}
	}
}

// The URL a support answer links to. A link built from the API host would send
// the user to a JSON endpoint, so the base URL is mandatory and separate.
func TestPageURLPrefersAPISuppliedURL(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		if slug == "docs/a" {
			return http.StatusOK,
				`{"id":"1","slug":"docs/a","title":"A","content":"` + longBody + `","url":"https://corp.wiki/pages/a"}`,
				nil
		}
		return http.StatusOK, pageJSON("2", "docs/b", "B", longBody), nil
	}

	docs, _, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2", len(docs))
	}

	byPath := map[string]source.Document{}
	for _, d := range docs {
		byPath[d.Path] = d
	}

	if got := byPath["docs/a"].URL; got != "https://corp.wiki/pages/a" {
		t.Errorf("URL = %q, want the API-supplied URL", got)
	}
	if got := byPath["docs/b"].URL; got != "https://wiki.example.com/docs/b" {
		t.Errorf("URL = %q, want the configured base joined with the slug", got)
	}
}

// The real page slugs of this wiki are long and arrive with a trailing slash —
// the live probe returned one ending in ".../hp-proliant-dl180-gen9/". A link
// built by naive concatenation would carry a double slash or a trailing one, and
// a support bot handing out subtly wrong links is not something anyone notices
// quickly.
func TestPageURLJoinsWithoutDoubleSlashes(t *testing.T) {
	cases := []struct {
		name string
		slug string
	}{
		{"trailing slash, as the live API returns", "docs/hardware/hp-proliant-dl180-gen9/"},
		{"leading slash", "/docs/hardware/hp-proliant-dl180-gen9"},
		{"both", "/docs/hardware/hp-proliant-dl180-gen9/"},
		{"neither", "docs/hardware/hp-proliant-dl180-gen9"},
	}

	const want = "https://wiki.yandex.ru/docs/hardware/hp-proliant-dl180-gen9"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWiki(t)
			f.descendants = func(slug, cursor string) (int, string, map[string]string) {
				return http.StatusOK, listJSON("", "1|"+tc.slug), nil
			}
			f.page = func(slug, id string) (int, string, map[string]string) {
				return http.StatusOK, pageJSON("1", tc.slug, "T", longBody), nil
			}

			docs, _, err := runSource(t, f, func(cfg *Config) {
				cfg.PageBaseURL = "https://wiki.yandex.ru"
				cfg.Filter = NewFilter([]string{"docs"}, nil)
			})
			if err != nil {
				t.Fatalf("Iterate: %v", err)
			}
			if len(docs) != 1 {
				t.Fatalf("indexed %d documents, want 1", len(docs))
			}
			if docs[0].URL != want {
				t.Errorf("URL = %q, want %q", docs[0].URL, want)
			}
			if strings.Contains(strings.TrimPrefix(docs[0].URL, "https://"), "//") {
				t.Errorf("URL %q contains a double slash", docs[0].URL)
			}
		})
	}
}

// A trailing slash on the CONFIGURED BASE must not produce a double slash either.
func TestPageURLToleratesTrailingSlashOnBase(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("1", "docs/a", "A", longBody), nil
	}

	docs, _, err := runSource(t, f, func(cfg *Config) { cfg.PageBaseURL = "https://wiki.yandex.ru/" })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if got := docs[0].URL; got != "https://wiki.yandex.ru/docs/a" {
		t.Errorf("URL = %q, want no double slash", got)
	}
}

// ---------------------------------------------------------------------------
// ACL modes
// ---------------------------------------------------------------------------

// aclUnavailableWiki is a fake API that behaves like the real one: every
// permission endpoint answers 405 Method not allowed.
func aclUnavailableWiki(t *testing.T) *fakeWiki {
	t.Helper()
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("id-"+slug, slug, "T", longBody), nil
	}
	f.access = func(pageID string) (int, string, map[string]string) {
		return http.StatusMethodNotAllowed, `{"error_code":"method_not_allowed","debug_message":"Method not allowed"}`, nil
	}
	return f
}

// --acl-mode=require keeps its meaning exactly: no confirmation of openness, no
// indexing. Against the current API that means nothing is indexed at all, and
// that is the correct outcome for an operator who chooses this mode — an empty
// index is a visible failure, an unvetted one is not.
func TestACLModeRequireIndexesNothingWhenTheEndpointIs405(t *testing.T) {
	f := aclUnavailableWiki(t)

	docs, m, err := runSource(t, f, func(cfg *Config) { cfg.ACLMode = ACLModeRequire })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(docs) != 0 {
		t.Fatalf("indexed %d documents with --acl-mode=require against a 405 ACL endpoint, want 0", len(docs))
	}
	for _, slug := range []string{"docs/a", "docs/b"} {
		e, ok := entryFor(m, slug)
		if !ok {
			t.Errorf("no manifest entry for %s", slug)
			continue
		}
		if e.Decision != DecisionSkipped || e.Reason != ReasonACLError {
			t.Errorf("%s: entry = {%s, %s}, want {%s, %s}", slug, e.Decision, e.Reason, DecisionSkipped, ReasonACLError)
		}
	}
	// Nothing was published, so there is nothing to warn about.
	if m.ACLUnchecked {
		t.Error("a require-mode run must not be marked ACL-unchecked: it indexed nothing precisely because it could not check")
	}
	// And no page content was ever fetched.
	for _, slug := range []string{"docs/a", "docs/b"} {
		if f.requestedContentFor(slug) {
			t.Errorf("content of %s was fetched despite the ACL check failing", slug)
		}
	}
}

// --acl-mode=probe is the default, and it trades the permission check for a
// working feature. The trade is only acceptable because it is LOUD: every
// manifest entry says acl=unchecked and the run prints a warning.
func TestACLModeProbeContinuesAndMarksEveryEntryUnchecked(t *testing.T) {
	f := aclUnavailableWiki(t)

	var log bytes.Buffer
	docs, m, err := runSource(t, f, func(cfg *Config) {
		cfg.ACLMode = ACLModeProbe
		cfg.Log = &log
	})
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(docs) != 2 {
		t.Fatalf("indexed %d documents with --acl-mode=probe, want 2 — a 405 must not abort the crawl", len(docs))
	}

	if !m.ACLUnchecked {
		t.Error("the manifest does not record that permissions went unchecked")
	}
	// EVERY entry, without exception. A manifest where only some rows say
	// "unchecked" reads as "the rest were checked".
	for _, e := range m.Entries {
		if e.ACL != ACLUnchecked {
			t.Errorf("entry %q has acl=%q, want %q", e.Slug, e.ACL, ACLUnchecked)
		}
	}

	// The warning must be impossible to miss, and must name what is left standing.
	warn := m.ACLWarning()
	for _, want := range []string{"WARNING", "NOT CHECKED", "--allow", "--deny"} {
		if !strings.Contains(warn, want) {
			t.Errorf("the ACL warning does not mention %q:\n%s", want, warn)
		}
	}
	if !strings.Contains(log.String(), ACLUnchecked) {
		t.Errorf("the run log never mentions that pages are unchecked:\n%s", log.String())
	}

	// The discovery costs ONE request, not one per page.
	if n := f.count("/access"); n != 1 {
		t.Errorf("made %d ACL requests, want 1 — the absence of the endpoint must latch", n)
	}
}

// --acl-mode=off spends no requests and is honest about it from the first entry.
func TestACLModeOffChecksNothingAndSaysSo(t *testing.T) {
	f := aclUnavailableWiki(t)

	docs, m, err := runSource(t, f, func(cfg *Config) { cfg.ACLMode = ACLModeOff })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("indexed %d documents, want 2", len(docs))
	}
	if n := f.count("/access"); n != 0 {
		t.Errorf("made %d ACL requests with --acl-mode=off, want 0", n)
	}
	if !m.ACLUnchecked {
		t.Error("an off-mode run must be recorded as ACL-unchecked")
	}
	for _, e := range m.Entries {
		if e.ACL != ACLUnchecked {
			t.Errorf("entry %q has acl=%q, want %q", e.Slug, e.ACL, ACLUnchecked)
		}
	}
}

// A 403 is NOT evidence that the endpoint is absent — it says this token may not
// use it. Treating the two alike would let one refused request silently disable
// permission checking for the whole crawl, which is the mistake that leaks a
// document.
func TestACLModeProbeStillFailsClosedOnNonEndpointErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := aclUnavailableWiki(t)
			f.access = func(pageID string) (int, string, map[string]string) {
				return status, `{"error_code":"nope"}`, nil
			}

			docs, m, err := runSource(t, f, func(cfg *Config) { cfg.ACLMode = ACLModeProbe })
			if err != nil {
				t.Fatalf("Iterate: %v", err)
			}
			if len(docs) != 0 {
				t.Fatalf("HTTP %d was treated as a missing endpoint and %d page(s) were indexed, want 0",
					status, len(docs))
			}
			if m.ACLUnchecked {
				t.Errorf("HTTP %d marked the run ACL-unchecked; only 404/405 are evidence the endpoint is absent", status)
			}
		})
	}
}

// A working ACL endpoint must still be honoured in probe mode — the mode is a
// fallback for a broken API, not a licence to stop checking.
func TestACLModeProbeHonoursAWorkingEndpoint(t *testing.T) {
	f := aclUnavailableWiki(t)
	f.access = func(pageID string) (int, string, map[string]string) {
		if pageID == "2" {
			return http.StatusOK, `{"access":"team"}`, nil
		}
		return http.StatusOK, `{"access":"organization"}`, nil
	}

	docs, m, err := runSource(t, f, func(cfg *Config) { cfg.ACLMode = ACLModeProbe })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 || docs[0].Path != "docs/a" {
		t.Fatalf("indexed %+v, want only the org-readable page", docs)
	}
	if m.ACLUnchecked {
		t.Error("a run whose ACL endpoint worked must not be marked unchecked")
	}
	if e, _ := entryFor(m, "docs/b"); e.Reason != ReasonACLRestrict {
		t.Errorf("the restricted page's reason = %q, want %q", e.Reason, ReasonACLRestrict)
	}
}

func TestParseACLMode(t *testing.T) {
	if got, err := ParseACLMode(""); err != nil || got != DefaultACLMode {
		t.Errorf("ParseACLMode(\"\") = %q, %v; want the default %q", got, err, DefaultACLMode)
	}
	for _, m := range ValidACLModes {
		if got, err := ParseACLMode(strings.ToUpper(string(m))); err != nil || got != m {
			t.Errorf("ParseACLMode(%q) = %q, %v", m, got, err)
		}
	}
	if _, err := ParseACLMode("maybe"); err == nil {
		t.Error("ParseACLMode accepted an unknown mode")
	}
}

func TestNewRequiresPageBaseURL(t *testing.T) {
	f := newFakeWiki(t)
	c, _ := f.newTestClient(t, nil)

	_, err := New(Config{Client: c, Roots: []string{"docs"}})
	if err == nil {
		t.Fatal("New accepted an empty PageBaseURL")
	}
	if !strings.Contains(err.Error(), "--page-base-url") {
		t.Errorf("error does not name the flag: %v", err)
	}
	if !strings.Contains(err.Error(), "API host") {
		t.Errorf("error does not warn against using the API host: %v", err)
	}
}

// The document id must be the page id, so renaming a page updates it rather
// than creating a duplicate.
func TestDocumentIDIsPageIDNotSlug(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "page-42|docs/old-name"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("page-42", "docs/old-name", "Title", longBody), nil
	}

	docs, _, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	if docs[0].ID != "page-42" {
		t.Errorf("ID = %q, want the page id %q", docs[0].ID, "page-42")
	}
}

// Quality rejections must be recorded, not silently dropped: "why is this page
// missing from the bot?" is a question the manifest has to answer.
func TestQualityRejectionsAreInTheManifest(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/empty", "2|docs/toc", "3|docs/good"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		switch slug {
		case "docs/empty":
			return http.StatusOK, pageJSON("1", slug, "Empty", "   "), nil
		case "docs/toc":
			body := "# Оглавление\n- [Раз](/a)\n- [Два](/b)\n- [Три](/c)\n"
			return http.StatusOK, pageJSON("2", slug, "TOC", body), nil
		default:
			return http.StatusOK, pageJSON("3", slug, "Good", longBody), nil
		}
	}

	docs, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 || docs[0].Path != "docs/good" {
		t.Fatalf("indexed %+v, want only docs/good", docs)
	}

	if e, _ := entryFor(m, "docs/empty"); e.Reason != ReasonEmpty {
		t.Errorf("empty page reason = %q, want %q", e.Reason, ReasonEmpty)
	}
	if e, _ := entryFor(m, "docs/toc"); e.Reason != ReasonTOCOnly {
		t.Errorf("TOC page reason = %q, want %q", e.Reason, ReasonTOCOnly)
	}
}

// A dry run must evaluate everything (the manifest needs real sizes and hashes)
// but hand nothing on to the indexer.
func TestDryRunYieldsNoDocumentsButFullManifest(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("1", slug, "A", longBody), nil
	}

	docs, m, err := runSource(t, f, func(cfg *Config) {
		cfg.DryRun = true
		cfg.Manifest = NewManifest([]string{"docs"}, []string{"docs"}, nil, true)
	})
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}

	if len(docs) != 0 {
		t.Errorf("dry run yielded %d documents, want 0", len(docs))
	}
	e, ok := entryFor(m, "docs/a")
	if !ok {
		t.Fatal("dry run produced no manifest entry")
	}
	if e.Decision != DecisionIndexed {
		t.Errorf("decision = %q, want %q (dry run records what WOULD be indexed)", e.Decision, DecisionIndexed)
	}
	if e.Bytes == 0 || e.ContentHash == "" {
		t.Errorf("dry run must still compute size and hash, got %+v", e)
	}
}

// With --page-type NARROWED, a page of another type is excluded — and the
// manifest records the TYPE, not just "skipped". Without the value a reviewer
// cannot tell whether the page was junk or whether --page-type simply needs
// widening.
//
// Note the explicit PageTypes: narrowing is now opt-in. See
// TestDefaultPageTypePolicyIndexesEveryType for what happens without it.
func TestNarrowedPageTypeIsExcludedWithReason(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/text", "2|docs/grid", "3|docs/table"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		switch slug {
		case "docs/grid":
			return http.StatusOK, typedPageJSON("2", slug, "Grid", longBody, "grid", ""), nil
		case "docs/table":
			return http.StatusOK, typedPageJSON("3", slug, "Table", longBody, "table", ""), nil
		default:
			return http.StatusOK, typedPageJSON("1", slug, "Text", longBody, "page", ""), nil
		}
	}

	docs, m, err := runSource(t, f, func(cfg *Config) { cfg.PageTypes = []string{"page"} })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 || docs[0].Path != "docs/text" {
		t.Fatalf("indexed %+v, want only docs/text", docs)
	}

	for slug, typ := range map[string]string{"docs/grid": "grid", "docs/table": "table"} {
		e, ok := entryFor(m, slug)
		if !ok {
			t.Errorf("no manifest entry for %s", slug)
			continue
		}
		if e.Decision != DecisionSkipped {
			t.Errorf("%s: decision = %q, want %q", slug, e.Decision, DecisionSkipped)
		}
		if want := ReasonPageType + ":" + typ; e.Reason != want {
			t.Errorf("%s: reason = %q, want %q", slug, e.Reason, want)
		}
	}
}

// THE DEFAULT INDEXES EVERY TYPE, and the manifest records what each type
// actually was.
//
// This reverses an earlier default of "page only", which a live probe proved
// wrong: the real page_type of a real page was a 7-character value, so the old
// default would have skipped every page of that wiki and produced an empty index
// with no error to explain it. The manifest's per-page type is what makes
// narrowing an informed decision instead of another guess.
func TestDefaultPageTypePolicyIndexesEveryType(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/text", "2|docs/grid", "3|docs/cloud"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		switch slug {
		case "docs/grid":
			return http.StatusOK, typedPageJSON("2", slug, "Grid", longBody, "grid", ""), nil
		case "docs/cloud":
			// Seven characters, exactly like the value the live probe returned.
			return http.StatusOK, typedPageJSON("3", slug, "Cloud", longBody, "cloudpg", ""), nil
		default:
			return http.StatusOK, typedPageJSON("1", slug, "Text", longBody, "page", ""), nil
		}
	}

	docs, m, err := runSource(t, f, nil) // no PageTypes: the default
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("indexed %d documents with the default policy, want all 3 — "+
			"a default that drops unknown types produces a silently empty index", len(docs))
	}

	// And every entry is still recorded as indexed, so the reviewer sees the real
	// set of types present and can decide what to exclude.
	for _, slug := range []string{"docs/text", "docs/grid", "docs/cloud"} {
		e, ok := entryFor(m, slug)
		if !ok {
			t.Errorf("no manifest entry for %s", slug)
			continue
		}
		if e.Decision != DecisionIndexed {
			t.Errorf("%s: decision = %q, want %q", slug, e.Decision, DecisionIndexed)
		}
	}
}

// The policy is configurable, so an operator can index a type we did not
// anticipate without waiting for a code change.
func TestPageTypePolicyIsConfigurable(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/grid"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, typedPageJSON("1", slug, "Grid", longBody, "grid", ""), nil
	}

	docs, _, err := runSource(t, f, func(cfg *Config) { cfg.PageTypes = []string{"page", "grid"} })
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("indexed %d documents, want 1 — the widened policy was ignored", len(docs))
	}
}

// Breadcrumbs land in the manifest, so a reviewer of a dry run can see WHERE in
// the wiki each indexed page lives, not just its slug.
func TestBreadcrumbsReachTheManifest(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, typedPageJSON("1", slug, "A", longBody, "page", "База знаний > Кадры > Отпуска"), nil
	}

	_, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	e, ok := entryFor(m, "docs/a")
	if !ok {
		t.Fatal("no manifest entry")
	}
	if e.Breadcrumbs != "База знаний > Кадры > Отпуска" {
		t.Errorf("Breadcrumbs = %q, want the joined page path", e.Breadcrumbs)
	}
}

// A page the API refuses to send a body for must not become an empty document.
func TestPageWithoutBodyIsNotIndexed(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		return http.StatusOK, metadataOnlyJSON(1, "docs/a", "A"), nil
	}

	docs, m, err := runSource(t, f, nil)
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("indexed %d documents from body-less responses, want 0", len(docs))
	}
	if e, _ := entryFor(m, "docs/a"); e.Reason != ReasonFetchError {
		t.Errorf("reason = %q, want %q", e.Reason, ReasonFetchError)
	}
}
