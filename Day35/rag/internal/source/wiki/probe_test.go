package wiki

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

// These strings stand in for real corporate wiki content. If any of them appears
// in the probe output, the probe has leaked page content to a terminal, a CI log
// or a pasted bug report.
var confidential = []string{
	"Зарплатная вилка senior разработчика составляет 400000 рублей",
	"Договор с ООО Контрагент подписан 15 марта",
	"ivanov@corp.example.com",
	"Пароль от стенда: hunter2",
	"Внутренний регламент увольнения сотрудников",
	"secret-project-codename-borealis",
}

// probeFixture builds a fake wiki whose every CONTENT-bearing string field
// carries confidential text, so any accidental value printing is caught.
//
// slug and page_type deliberately do NOT carry canaries: they are a path and an
// enumeration, they are printed on purpose (see structuralStringKeys), and
// seeding them with secrets would encode "these must stay hidden" — the opposite
// of the contract.
func probeFixture(t *testing.T) *fakeWiki {
	t.Helper()
	f := newFakeWiki(t)

	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, `{
			"id": "42",
			"slug": "docs/secret",
			"page_type": "cloud_page",
			"title": "` + confidential[0] + `",
			"content": "` + confidential[1] + `",
			"author": "` + confidential[2] + `",
			"url": "https://corp.wiki/` + confidential[5] + `",
			"version": "` + confidential[3] + `",
			"nested": {"note": "` + confidential[4] + `", "count": 7, "flag": true},
			"tags": ["` + confidential[5] + `", "` + confidential[0] + `"]
		}`, nil
	}

	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{
			"results": [
				{"id": "1", "slug": "docs/a", "title": "` + confidential[0] + `"},
				{"id": "2", "slug": "docs/b", "title": "` + confidential[4] + `"}
			],
			"next_cursor": "` + confidential[3] + `"
		}`, nil
	}

	f.access = func(pageID string) (int, string, map[string]string) {
		return http.StatusOK, `{
			"access": "team-only",
			"owner": "` + confidential[2] + `",
			"note": "` + confidential[4] + `"
		}`, nil
	}

	return f
}

// The Definition of Done for this feature: the probe reports SHAPE, never
// content. It prints key names, JSON types and lengths — plus, deliberately, the
// value of page_type and slug, which TestProbePrintsStructuralStringValues below
// pins down. Everything else stays masked.
func TestProbeDoesNotPrintStringValues(t *testing.T) {
	f := probeFixture(t)
	c, _ := f.newTestClient(t, nil)

	var out bytes.Buffer
	p := NewProbe(c, &out)
	if err := p.Run(context.Background(), "docs/secret"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if got == "" {
		t.Fatal("probe produced no output")
	}

	for _, secret := range confidential {
		if strings.Contains(got, secret) {
			t.Errorf("probe printed confidential content %q\n--- output ---\n%s", secret, got)
		}
		// Also check individual words: a truncated value is still a leak.
		for _, word := range strings.Fields(secret) {
			if len([]rune(word)) < 6 {
				continue // short common words would false-positive
			}
			if strings.Contains(got, word) {
				t.Errorf("probe printed a fragment of confidential content: %q\n--- output ---\n%s", word, got)
			}
		}
	}
}

// The other half of the contract, and a real bug this pins shut: page_type and
// slug must be printed AS VALUES.
//
// The observed failure: a live probe reported the page's type only as
// string(len=7). The reader therefore could not see that the value was not
// "page" (four characters) and that the then-default --page-type filter would
// have discarded the page — the whole wiki would have indexed as empty, with the
// one fact needed to diagnose it masked by a safety rule aimed at page bodies.
// A filter whose input you cannot see is a filter you cannot configure.
func TestProbePrintsStructuralStringValues(t *testing.T) {
	f := probeFixture(t)
	c, _ := f.newTestClient(t, nil)

	var out bytes.Buffer
	if err := NewProbe(c, &out).Run(context.Background(), "docs/secret"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	// The enumeration the crawler filters on, in the structure tree...
	if !strings.Contains(got, "page_type: string(len=10) = cloud_page") {
		t.Errorf("probe masked the page_type VALUE — the reader cannot tell whether --page-type would keep this page\n%s", got)
	}
	// ...and again in the ParsePage summary, where it is easiest to spot.
	if !strings.Contains(got, `page_type="cloud_page"`) {
		t.Errorf("the ParsePage summary does not report the page_type value\n%s", got)
	}
	// The path. Already printed at the top of the report and already in the
	// manifest, so masking it in the tree was pure inconsistency.
	if !strings.Contains(got, "slug: string(len=11) = docs/secret") {
		t.Errorf("probe masked the slug VALUE, which is a path, not content\n%s", got)
	}

	// The exception is narrow: title and content are STILL length-only.
	if !strings.Contains(got, "title: string(len=") {
		t.Errorf("title should still be reported by length\n%s", got)
	}
	for _, leaked := range []string{confidential[0], confidential[1]} {
		if strings.Contains(got, leaked) {
			t.Errorf("widening the exception leaked %q\n%s", leaked, got)
		}
	}
}

// Having proved it prints no values, prove it prints enough to be useful:
// the structure and the verdict are the whole point of the command.
func TestProbePrintsStructureAndVerdict(t *testing.T) {
	f := probeFixture(t)
	c, _ := f.newTestClient(t, nil)

	var out bytes.Buffer
	if err := NewProbe(c, &out).Run(context.Background(), "docs/secret"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	// Key NAMES are safe and necessary — they are what dto.go must be taught.
	for _, want := range []string{"title:", "content:", "author:", "nested:", "tags:", "results:", "next_cursor:", "access:"} {
		if !strings.Contains(got, want) {
			t.Errorf("probe output does not mention key %q\n%s", want, got)
		}
	}

	// Types and lengths.
	for _, want := range []string{"object(keys=", "array(len=", "string(len="} {
		if !strings.Contains(got, want) {
			t.Errorf("probe output lacks type/length information %q\n%s", want, got)
		}
	}

	// Numbers and booleans are structural, not content, and ARE printed.
	if !strings.Contains(got, "= 7") {
		t.Errorf("probe did not print a numeric value (safe and useful)\n%s", got)
	}
	if !strings.Contains(got, "= true") {
		t.Errorf("probe did not print a boolean value (safe and useful)\n%s", got)
	}

	// The verdict: which candidate matched, and what did not.
	if !strings.Contains(got, "MATCHED via") {
		t.Errorf("probe printed no MATCHED verdict\n%s", got)
	}

	// The item list is an array. Reporting a present list as NOT FOUND would
	// send the user to "fix" a candidate list that is already correct.
	if !strings.Contains(got, `item list        MATCHED via "results"`) {
		t.Errorf("probe did not recognise the array-valued item list\n%s", got)
	}
	if !strings.Contains(got, "ParsePage OK") {
		t.Errorf("probe did not report ParsePage's outcome\n%s", got)
	}

	// The ACL section must be explicit about failing closed: this is the check
	// whose misreading leaks a document.
	if !strings.Contains(got, "FAIL-CLOSED") {
		t.Errorf("probe did not flag the fail-closed ACL verdict\n%s", got)
	}
	if !strings.Contains(got, "dto.go") {
		t.Errorf("probe did not point at the file to fix\n%s", got)
	}
}

// A missing field must be reported as NOT FOUND with the candidates tried —
// that is the actionable half of the probe.
func TestProbeReportsUnmatchedFields(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		// No recognisable body field at all.
		return http.StatusOK, `{"id":"1","slug":"docs/a","payload":"тайна"}`, nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[]}`, nil
	}

	c, _ := f.newTestClient(t, nil)
	var out bytes.Buffer
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "NOT FOUND") {
		t.Errorf("probe did not report the missing body field\n%s", got)
	}
	if !strings.Contains(got, "payload") {
		t.Errorf("probe did not surface the key that actually arrived\n%s", got)
	}
	if strings.Contains(got, "тайна") {
		t.Errorf("probe leaked the value of the unknown field\n%s", got)
	}
}

// A failing endpoint must not abort the whole probe: the other two endpoints
// still teach us something, and the user should learn all of it in one run.
func TestProbeContinuesAfterEndpointFailure(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusNotFound, `{"error_code":"not_found"}`, nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[{"id":"1","slug":"docs/a"}]}`, nil
	}

	c, _ := f.newTestClient(t, nil)
	var out bytes.Buffer
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run returned an error instead of reporting it: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "REQUEST FAILED") {
		t.Errorf("probe did not report the failed endpoint\n%s", got)
	}
	if !strings.Contains(got, "ParsePageList OK") {
		t.Errorf("probe stopped after the first failure instead of probing the rest\n%s", got)
	}
}

// The probe's own output must be redacted too — the token can reach it through
// an echoed error message.
func TestProbeRedactsTokenInErrors(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)

	const tok = "y0_PROBE_CANARY_TOKEN_VALUE"

	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusBadRequest,
			`{"error_code":"bad","debug_message":"header was Authorization: OAuth ` + tok + `"}`, nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[]}`, nil
	}

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) { cfg.Token = tok })

	var out bytes.Buffer
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(out.String(), tok) {
		t.Errorf("probe leaked the OAuth token:\n%s", out.String())
	}
}

// Depth and array sampling must bound the output so a huge or deeply nested
// payload cannot flood a terminal.
func TestProbeBoundsOutput(t *testing.T) {
	f := newFakeWiki(t)
	f.page = func(slug, id string) (int, string, map[string]string) {
		return http.StatusOK, `{"id":"1","content":"x","a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":1}}}}}}},
			"many":[{"k":1},{"k":2},{"k":3},{"k":4},{"k":5}]}`, nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[]}`, nil
	}

	c, _ := f.newTestClient(t, nil)
	var out bytes.Buffer
	p := NewProbe(c, &out)
	p.MaxDepth = 3
	p.MaxSamples = 2
	if err := p.Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "max depth 3 reached") {
		t.Errorf("deep nesting was not truncated\n%s", got)
	}
	if !strings.Contains(got, "3 more element(s), not shown") {
		t.Errorf("long array was not sampled\n%s", got)
	}
}

// REGRESSION. The observed bug: probe printed
//
//	page id          MATCHED via "id"
//
// and then, three lines later,
//
//	ACL SKIPPED: no page id was recovered above — fix pageIDPaths first
//
// The id had been read successfully; ParsePage then failed on the absent body
// and discarded the whole PageContent, id included, so the ACL step — the
// highest-risk contract of the three, the one whose misreading leaks an internal
// document into a support chat — never ran. One problem masked every problem
// behind it, which is the exact opposite of what a diagnostic tool is for.
func TestProbeChecksACLEvenWhenTheBodyIsMissing(t *testing.T) {
	f := newFakeWiki(t)
	// The real API's answer: metadata only, no body under any name.
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		return http.StatusOK, metadataOnlyJSON(48952013, "docs/a", "Заголовок"), nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[{"id":48952013,"slug":"docs/a"}],"next_cursor":null,"prev_cursor":null}`, nil
	}

	var aclRequested string
	f.access = func(pageID string) (int, string, map[string]string) {
		aclRequested = pageID
		return http.StatusOK, `{"access":"organization"}`, nil
	}

	var out bytes.Buffer
	c, _ := f.newTestClient(t, nil)
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	if aclRequested != "48952013" {
		t.Fatalf("the ACL endpoint was called with %q, want the recovered page id — "+
			"a failed body parse must not skip the ACL check\n--- probe output ---\n%s", aclRequested, got)
	}
	if strings.Contains(got, "SKIPPED: no page id") {
		t.Errorf("probe still reports a missing id it had already matched\n%s", got)
	}
	if !strings.Contains(got, "ParsePage FAILED") {
		t.Errorf("probe hid the body failure instead of reporting it\n%s", got)
	}
	if !strings.Contains(got, "ParseAccess:") {
		t.Errorf("the ACL verdict is missing from the report\n%s", got)
	}
	if !strings.Contains(got, "page id recovered=true") {
		t.Errorf("probe did not say it was continuing with a recovered id\n%s", got)
	}
}

// The probe must request the page the way the crawler does — with `fields` —
// or it reports a false "body NOT FOUND" and sends the reader off to fix
// bodyPaths that were never wrong.
func TestProbeRequestsFieldsAndNamesTheEncoding(t *testing.T) {
	f := newFakeWiki(t)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		if !asksFor(r, "content") {
			return http.StatusOK, metadataOnlyJSON(1, "docs/a", "A"), nil
		}
		return http.StatusOK, typedPageJSON("1", "docs/a", "A", longBody, "page", "База > A"), nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[]}`, nil
	}

	var out bytes.Buffer
	c, _ := f.newTestClient(t, nil)
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	// Every CONTENT request must carry fields=content. The ACL section also hits
	// /v1/pages, with fields=attributes, and that one is not a content request.
	var contentHits int
	for _, h := range f.pageHits() {
		if strings.Contains(h, FieldsParam+"=attributes") {
			continue // the ACL "is it an attribute?" probe
		}
		contentHits++
		if !strings.Contains(h, FieldsParam+"=content") {
			t.Errorf("probe issued a page request without %s=content: %s", FieldsParam, h)
		}
	}
	if contentHits == 0 {
		t.Error("the probe never requested page content")
	}
	if !strings.Contains(got, "encoding that WORKED: comma-separated") {
		t.Errorf("probe did not report which %s encoding worked\n%s", FieldsParam, got)
	}
	// The request itself is printed, so a user can paste it into a bug report.
	if !strings.Contains(got, "request: ") || !strings.Contains(got, "/v1/pages?") {
		t.Errorf("probe did not print the request URL it issued\n%s", got)
	}
	if !strings.Contains(got, "ParsePage OK") {
		t.Errorf("the body was not found even though fields was sent\n%s", got)
	}
	// New fields get their own verdict lines.
	if !strings.Contains(got, "page type") || !strings.Contains(got, "breadcrumbs") {
		t.Errorf("probe does not report on page_type / breadcrumbs\n%s", got)
	}
	// And still no values: the breadcrumb is described by depth, never printed.
	if strings.Contains(got, "База") {
		t.Errorf("probe leaked breadcrumb content\n%s", got)
	}
}

// The fallback must be visible in the report: it is the answer to "which
// encoding does THIS deployment want", and the user running the probe is exactly
// the person who needs it. The comma form is confirmed against the live API, so
// the case worth reporting now is a deployment that disagrees with it.
func TestProbeReportsRepeatedEncodingFallback(t *testing.T) {
	f := newFakeWiki(t)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		if usesCommaEncoding(r) {
			return http.StatusOK, metadataOnlyJSON(1, "docs/a", "A"), nil
		}
		return http.StatusOK, pageJSON("1", "docs/a", "A", longBody), nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[]}`, nil
	}

	var out bytes.Buffer
	c, _ := f.newTestClient(t, nil)
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "encoding that WORKED: repeated") {
		t.Errorf("probe did not report the repeated-parameter fallback\n%s", got)
	}
}

// The live listing ends with "next_cursor": null. That is a present, correct
// field with no value — reporting it as NOT FOUND would send the reader to fix
// cursorPaths, which are right, instead of reading it as "this is the last
// page". The two findings need opposite responses, so they must read differently.
func TestProbeDistinguishesNullFieldFromMissingField(t *testing.T) {
	f := newFakeWiki(t)
	f.pageRaw = func(r *http.Request) (int, string, map[string]string) {
		return http.StatusOK, pageJSON("1", "docs/a", "A", longBody), nil
	}
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, `{"results":[{"id":1,"slug":"docs/a"}],"next_cursor":null,"prev_cursor":null}`, nil
	}

	var out bytes.Buffer
	c, _ := f.newTestClient(t, nil)
	if err := NewProbe(c, &out).Run(context.Background(), "docs/a"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, `next cursor      name MATCHED at "next_cursor"`) {
		t.Errorf("a present-but-null cursor was not distinguished from a missing one\n%s", got)
	}
	if strings.Contains(got, "next cursor      NOT FOUND") {
		t.Errorf("probe blames cursorPaths for a null value\n%s", got)
	}
}
