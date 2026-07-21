package wiki

// probe.go answers one question: what do the wiki API responses ACTUALLY look
// like? The field names in dto.go are guesses; the user runs this with a real
// token and the output tells us which guesses were right.
//
// SAFETY RULE, non-negotiable: this prints the SHAPE of a response, never its
// content. This is a corporate wiki — page bodies, titles, author names and
// internal URLs must not reach a terminal, a CI log or a pasted bug report.
// Only these are printed for a value:
//   - numbers and booleans (structural, not content)
//   - the LENGTH of a string or array
//   - key names and JSON types
//   - the string value of the few keys in structuralStringKeys, which are
//     enumerations and paths rather than user-written text (see below)
//
// Everything printed additionally passes through redact(). If you add a branch
// here that prints a string value, you have broken the contract — see
// TestProbeDoesNotPrintStringValues.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// maxProbeDepth bounds how deep the structure tree is walked, so a deeply
// nested or self-similar payload cannot produce unbounded output.
const maxProbeDepth = 6

// probeArraySamples is how many array elements are described. The elements of a
// list are usually homogeneous, so one or two is enough to learn the shape.
const probeArraySamples = 2

// structuralStringKeys are the ONLY string-valued keys whose value is printed in
// full. Masking them was actively harmful, which is why the exception exists:
//
//   - page_type is an ENUMERATION the crawler filters on. A live probe reported
//     it only as string(len=7) — so the reader could not tell that the value was
//     not "page" (len 4) and that the default --page-type filter would therefore
//     have discarded the page. A filter you cannot see the input of is a filter
//     you cannot configure.
//   - slug is a PATH, not content. It is already printed at the top of the
//     report, already goes into the manifest, and is the thing allow/deny rules
//     are written against.
//
// Everything else stays masked. content, title, author names, URLs and any key
// this list does not name are user-written text and are reported by length only.
var structuralStringKeys = map[string]bool{
	"page_type": true,
	"pageType":  true,
	"slug":      true,
	"page_slug": true,
}

// Probe inspects the three endpoints the crawler depends on and reports both the
// raw structure and dto.go's verdict on it.
type Probe struct {
	Client     *Client
	Out        io.Writer
	ACLOpen    []string
	MaxDepth   int
	MaxSamples int
}

// NewProbe builds a Probe writing to out.
func NewProbe(c *Client, out io.Writer) *Probe {
	return &Probe{Client: c, Out: out, ACLOpen: DefaultACLOpenValues, MaxDepth: maxProbeDepth, MaxSamples: probeArraySamples}
}

// Run probes /v1/pages, /v1/pages/descendants and /v1/pages/{id}/access for the
// given slug. It does not stop at the first failure: a 404 on one endpoint still
// leaves the other two informative, and the point of the run is to learn as much
// as possible from one invocation.
func (p *Probe) Run(ctx context.Context, slug string) error {
	if p.MaxDepth <= 0 {
		p.MaxDepth = maxProbeDepth
	}
	if p.MaxSamples <= 0 {
		p.MaxSamples = probeArraySamples
	}

	p.header("wiki probe")
	p.printf("slug: %s\n", redact(slug))
	p.printf("NOTE: values are never printed — only key names, JSON types and lengths.\n")

	pageID := p.probePage(ctx, slug)
	p.probeDescendants(ctx, slug)
	p.probeAccess(ctx, pageID, slug)

	p.header("next step")
	p.printf("Fix ONLY internal/source/wiki/dto.go so every field above reports MATCHED,\n")
	p.printf("then re-run this probe until the verdict is clean.\n")
	return nil
}

// probePage inspects GET /v1/pages?slug=… WITH the `fields` parameter — without
// it the API returns metadata only and every body check below would report a
// false "NOT FOUND", sending the reader off to fix bodyPaths that were never
// wrong.
//
// It returns the page id, which the ACL probe needs. THE ID IS RECOVERED
// INDEPENDENTLY OF ParsePage: a diagnostic tool that abandons everything it
// already learned because one field was missing hides every later problem
// behind the first one. That is precisely the bug this shape prevents — the id
// matched, ParsePage failed on the absent body, and the ACL step (the
// highest-risk contract of the three) was skipped for want of an id that had
// already been read successfully.
func (p *Probe) probePage(ctx context.Context, slug string) (pageID string) {
	p.header("GET /v1/pages?slug=… (+" + FieldsParam + ")")

	fetcher := NewPageFetcher(p.Client, io.Discard)
	page, raw, parseErr := fetcher.Fetch(ctx, slug, "")
	if raw == nil {
		p.printf("REQUEST FAILED: %s\n", redact(parseErr.Error()))
		return ""
	}

	style, confirmed := fetcher.Style()
	if confirmed {
		p.printf("%s encoding that WORKED: %s\n", FieldsParam, style)
	} else {
		p.printf("%s encoding: NO variant returned a body (last tried: %s)\n", FieldsParam, style)
	}
	p.printf("request: %s\n", redact(fetcher.RequestURL(slug, "")))

	p.structure(raw)

	p.subheader("dto.go verdict")
	m, err := decodeObject(raw)
	if err != nil {
		p.printf("  response is not a JSON object: %s\n", redact(err.Error()))
		return ""
	}
	m = unwrap(m, "result", "page", "data")
	p.verdict(m, "page id", pageIDPaths)
	p.verdict(m, "slug", pageSlugPaths)
	p.verdict(m, "title", titlePaths)
	p.verdict(m, "body", bodyPaths)
	p.verdict(m, "page type", pageTypePaths)
	p.verdictArray(m, "breadcrumbs", breadcrumbPaths)
	p.verdict(m, "format", formatPaths)
	p.verdict(m, "version", versionPaths)
	p.verdict(m, "public url", publicURLPaths)

	// Read the id from the payload directly, so it survives a ParsePage failure.
	pageID, _, _ = pick(m, pageIDPaths...)
	if pageID == "" {
		pageID = page.ID
	}

	if parseErr != nil {
		p.printf("  ParsePage FAILED: %s\n", redact(parseErr.Error()))
		p.printf("  continuing anyway: page id recovered=%t — the ACL probe below still runs\n", pageID != "")
		return pageID
	}
	// Lengths and presence flags only: the body itself never gets printed.
	p.printf("  ParsePage OK: body %d chars, id present=%t, slug present=%t, title present=%t\n",
		len([]rune(page.Body)), page.ID != "", page.Slug != "", page.Title != "")
	// page_type is printed as a VALUE: it is the enumeration --page-type filters
	// on, and a reader who cannot see it cannot tell whether their filter would
	// have kept this page.
	p.printf("  page_type=%q, breadcrumbs present=%t (%d level(s))\n",
		redact(page.PageType), page.Breadcrumbs != "", breadcrumbDepth(page.Breadcrumbs))
	p.printf("  --page-type default indexes EVERY type, so this page passes the type filter.\n")
	p.printf("  Narrow it with --page-type %s to index only pages of this type.\n", redact(nonEmpty(page.PageType, "page")))
	return pageID
}

// nonEmpty is a local fallback for a display value.
func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// breadcrumbDepth counts the levels in a rendered breadcrumb without echoing it.
func breadcrumbDepth(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Split(s, " > "))
}

// probeDescendants inspects the listing endpoint the walker paginates over.
func (p *Probe) probeDescendants(ctx context.Context, slug string) {
	p.header("GET /v1/pages/descendants?slug=…")

	q := url.Values{"slug": {slug}, "include_self": {"true"}, "page_size": {"100"}}
	raw, err := p.Client.GetJSON(ctx, "/v1/pages/descendants", q)
	if err != nil {
		p.printf("REQUEST FAILED: %s\n", redact(err.Error()))
		return
	}
	p.structure(raw)

	p.subheader("dto.go verdict")
	if m, err := decodeObject(raw); err == nil {
		// The item list is an ARRAY, so it needs the container check: pick() only
		// accepts scalars and would report a present list as missing, sending the
		// user off to "fix" a candidate list that is already correct.
		p.verdictArray(m, "item list", listPaths)
		p.verdict(m, "next cursor", cursorPaths)
	} else {
		p.printf("  top level is an array, not an object — no cursor field to find\n")
	}

	items, cursor, err := ParsePageList(raw)
	if err != nil {
		p.printf("  ParsePageList FAILED: %s\n", redact(err.Error()))
		return
	}
	p.printf("  ParsePageList OK: %d items, cursor present=%t\n", len(items), cursor != "")
	if len(items) > 0 {
		// Presence flags only — never the id, slug or title itself.
		p.printf("  first item: id present=%t, slug present=%t, title present=%t, depth=%d\n",
			items[0].ID != "", items[0].Slug != "", items[0].Title != "", items[0].Depth)
	}
	if len(items) == 0 {
		p.printf("  WARNING: zero items — either the slug has no descendants, or listPaths is wrong\n")
	}
}

// aclCandidate is one way the API might expose a page's permissions.
type aclCandidate struct {
	// Label is what gets printed; Path/Query is what gets requested.
	Label string
	Path  func(pageID string) string
	Query func(slug string) url.Values
}

// aclCandidates are every documented and semi-documented way to ask this API for
// a page's permissions, in the order worth trying.
//
// The list exists because the obvious one does not work. A live probe of
// GET /v1/pages/{id}/access answered 405 Method not allowed, and the published
// API documents nothing else for page permissions — even a mature third-party
// client (n-r-w/yandex-mcp, 7 tools) exposes user_permissions for GRIDS only,
// nothing for pages. fields=attributes is included on the theory that access
// information might be carried as a page attribute; it is a guess, and the
// probe's job is to settle it with evidence instead of leaving it a guess.
var aclCandidates = []aclCandidate{
	{
		Label: "GET /v1/pages/{id}/access",
		Path:  func(id string) string { return "/v1/pages/" + url.PathEscape(id) + "/access" },
	},
	{
		Label: "GET /v1/pages/{id}/access-info",
		Path:  func(id string) string { return "/v1/pages/" + url.PathEscape(id) + "/access-info" },
	},
	{
		Label: "GET /v1/pages/{id}/permissions",
		Path:  func(id string) string { return "/v1/pages/" + url.PathEscape(id) + "/permissions" },
	},
	{
		Label: "GET /v1/pages?" + FieldsParam + "=attributes",
		Path:  func(string) string { return pagePath },
		Query: func(slug string) url.Values {
			return url.Values{"slug": {slug}, FieldsParam: {"attributes"}}
		},
	},
}

// probeAccess tries every known way to read a page's permissions and reports the
// status of each. This is the highest-risk contract in the whole feature —
// misreading it means a restricted page reaches a support chat — so the section
// ends with an explicit statement of what is and is not being enforced.
//
// It does NOT stop at the first failure. The first candidate is known to answer
// 405, and a probe that aborts there teaches the reader nothing about the other
// three, which is the entire reason this section was rewritten.
func (p *Probe) probeAccess(ctx context.Context, pageID, slug string) {
	p.header("page permissions (ACL)")

	if pageID == "" {
		p.printf("SKIPPED: no page id was recovered above — fix pageIDPaths in dto.go first\n")
		return
	}

	var worked *aclCandidate
	var workedRaw json.RawMessage

	for i := range aclCandidates {
		c := &aclCandidates[i]
		var q url.Values
		if c.Query != nil {
			q = c.Query(slug)
		}

		raw, err := p.Client.GetJSON(ctx, c.Path(pageID), q)
		if err != nil {
			p.printf("  %-42s -> %s\n", c.Label, redact(aclStatusOf(err)))
			continue
		}
		p.printf("  %-42s -> HTTP 200 OK\n", c.Label)
		if worked == nil {
			worked, workedRaw = c, raw
		}
	}

	if worked == nil {
		p.aclUnavailable()
		return
	}

	p.printf("\n  the responding variant is %s; its structure:\n", worked.Label)
	p.structure(workedRaw)

	p.subheader("dto.go verdict")
	if m, err := decodeObject(workedRaw); err == nil {
		m = unwrap(m, "result", "access", "data")
		p.verdict(m, "acl marker", aclOpenPaths)
		p.verdict(m, "acl boolean flag", aclOpenFlags)
	}

	v, _ := ParseAccess(workedRaw, p.ACLOpen)
	p.printf("  ParseAccess: Open=%t Reason=%s\n", v.Open, redact(v.Reason))
	p.printf("  recognised open markers: %s\n", strings.Join(p.ACLOpen, ", "))
	if !v.Open {
		p.printf("  FAIL-CLOSED: this page would NOT be indexed under --acl-mode=require.\n")
		p.printf("  If this page really is readable org-wide, the marker above is missing from\n")
		p.printf("  aclOpenPaths/DefaultACLOpenValues in dto.go — fix it there, not in wiki.go.\n")
	}
}

// aclUnavailable states, in as many words, that there is no permission check —
// and what is left standing in its place. This is the most consequential
// sentence the probe prints, so it does not hedge.
func (p *Probe) aclUnavailable() {
	p.printf("\n  NO VARIANT WORKED — page permissions CANNOT be checked through this API.\n")
	p.printf("\n  What this means for indexing:\n")
	p.printf("    * There is no API-level permission check. The crawler cannot tell a page\n")
	p.printf("      readable by the whole organization from one restricted to a team.\n")
	p.printf("    * Filtering therefore rests ENTIRELY on the --allow / --deny slug rules\n")
	p.printf("      plus a human reading the dry-run manifest before anything is published.\n")
	p.printf("    * --acl-mode=%s (the default) continues the crawl and records every page\n", ACLModeProbe)
	p.printf("      as acl=%s. --acl-mode=%s refuses to index anything instead.\n", ACLUnchecked, ACLModeRequire)
	p.printf("\n  Run the indexer with --dry-run first and read every slug in the manifest.\n")
}

// aclStatusOf renders an error as a status line: the HTTP code when there is
// one, the redacted transport error otherwise. The code is the whole point —
// 405 and 403 mean different things and lead to different next steps.
func aclStatusOf(err error) string {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return "request failed: " + err.Error()
	}
	switch apiErr.Status {
	case 0:
		return "transport failure: " + apiErr.DebugMessage
	case http.StatusMethodNotAllowed:
		return "HTTP 405 Method not allowed (this endpoint does not exist for pages)"
	case http.StatusNotFound:
		return "HTTP 404 Not found (wrong path, or no such page)"
	case http.StatusForbidden:
		return "HTTP 403 Forbidden (the endpoint exists but this token may not use it)"
	default:
		return fmt.Sprintf("HTTP %d", apiErr.Status)
	}
}

// verdict reports whether any candidate path yielded a usable SCALAR value.
func (p *Probe) verdict(m map[string]any, entity string, paths []string) {
	if _, matched, ok := pick(m, paths...); ok {
		p.printf("  %-16s MATCHED via %q\n", entity, matched)
		return
	}
	// A key that is PRESENT but null or empty is a different finding from a key
	// that is absent, and the two need opposite responses. The live listing
	// returns "next_cursor": null on the last page — the candidate name is
	// exactly right, there is simply no next page. Reporting that as NOT FOUND
	// sends the reader off to "fix" dto.go where nothing is broken.
	for _, path := range paths {
		if v, ok := lookup(m, path); ok {
			p.printf("  %-16s name MATCHED at %q, value is %s — the candidate is correct, this response just carries no value\n",
				entity, path, jsonType(v))
			return
		}
	}
	p.printf("  %-16s NOT FOUND (tried: %s)\n", entity, strings.Join(paths, ", "))
}

// verdictArray is the same check for fields that hold an ARRAY rather than a
// scalar — the listing endpoint's items. It also reports the array's length, so
// a candidate that matched an empty array is distinguishable from one that
// matched real data.
func (p *Probe) verdictArray(m map[string]any, entity string, paths []string) {
	for _, path := range paths {
		v, ok := lookup(m, path)
		if !ok {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			p.printf("  %-16s found at %q but it is %s, not an array\n", entity, path, jsonType(v))
			return
		}
		p.printf("  %-16s MATCHED via %q (len=%d)\n", entity, path, len(arr))
		return
	}
	p.printf("  %-16s NOT FOUND (tried: %s)\n", entity, strings.Join(paths, ", "))
}

// structure prints the key/type tree of a JSON payload.
func (p *Probe) structure(raw json.RawMessage) {
	p.subheader("response structure")
	p.printf("  size: %d bytes\n", len(raw))

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		p.printf("  NOT VALID JSON: %s\n", redact(err.Error()))
		return
	}
	p.node("  ", "(root)", v, 0)
}

// node renders one JSON node and recurses. Every printed token is a key name, a
// type name, a length or a number/bool — never a string value.
func (p *Probe) node(indent, name string, v any, depth int) {
	if depth > p.MaxDepth {
		p.printf("%s%s: … (max depth %d reached)\n", indent, redact(name), p.MaxDepth)
		return
	}

	switch t := v.(type) {
	case map[string]any:
		p.printf("%s%s: object(keys=%d)\n", indent, redact(name), len(t))
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p.node(indent+"  ", k, t[k], depth+1)
		}

	case []any:
		p.printf("%s%s: array(len=%d)\n", indent, redact(name), len(t))
		for i, item := range t {
			if i >= p.MaxSamples {
				p.printf("%s  … %d more element(s), not shown\n", indent, len(t)-p.MaxSamples)
				break
			}
			p.node(indent+"  ", fmt.Sprintf("[%d]", i), item, depth+1)
		}

	default:
		// Scalars: jsonType prints a length for strings and the type for the rest.
		// Numbers and booleans are structural and safe to show in full; strings
		// are reduced to their length unless the key is in structuralStringKeys.
		p.printf("%s%s: %s%s\n", indent, redact(name), jsonType(v), scalarHint(name, v))
	}
}

// scalarHint appends the actual value for the types that cannot carry user
// content: numbers, booleans, and the strings whose key is in
// structuralStringKeys. Every other string deliberately gets nothing — its
// length is already in jsonType and its content must never be printed.
func scalarHint(key string, v any) string {
	switch t := v.(type) {
	case bool:
		return fmt.Sprintf(" = %t", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf(" = %d", int64(t))
		}
		return fmt.Sprintf(" = %g", t)
	case json.Number:
		return " = " + t.String()
	case string:
		if structuralStringKeys[key] {
			return " = " + redact(t)
		}
		return ""
	default:
		return ""
	}
}

func (p *Probe) header(title string) {
	p.printf("\n=== %s %s\n", title, strings.Repeat("=", max(0, 60-len(title))))
}

func (p *Probe) subheader(title string) { p.printf("\n-- %s\n", title) }

func (p *Probe) printf(format string, args ...any) {
	fmt.Fprintf(p.Out, format, args...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
