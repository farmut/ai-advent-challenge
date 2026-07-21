package wiki

// dto.go is QUARANTINE.
//
// This is the ONLY file that knows the field names of the Yandex 360 wiki API.
// Those names are NOT VERIFIED against the live API — the documentation renders
// via JS and an unauthenticated probe is useless (401 is returned before
// routing). Everything here is a candidate list plus a tolerant picker.
//
// When `rag wiki probe` is finally run with a real user token, its output tells
// which candidate matched and which did not. Fixing this package to the real API
// must mean editing THIS FILE ONLY. Do not let API field names leak into
// client.go, walk.go or wiki.go.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PageRef is a page as it appears in a listing: enough to decide whether to
// fetch it, not enough to index it.
type PageRef struct {
	ID    string
	Slug  string
	Title string
	Depth int
}

// PageContent is a fetched page.
type PageContent struct {
	ID        string
	Slug      string
	Title     string
	Body      string
	Format    string
	Version   string
	UpdatedAt string
	// PageType is the API's own classification of the page. CONFIRMED to exist:
	// the live metadata response carries it. "page" is an ordinary text page;
	// other values (tables, grids, boards) are not prose and indexing them adds
	// noise. The full set of values is NOT known, which is why the accepted set
	// is configurable rather than a hardcoded white-list.
	PageType string
	// Breadcrumbs is the page's place in the tree, "A > B > C", assembled from
	// the API's breadcrumbs array. Its exact shape is NOT confirmed, so a shape
	// we do not recognise yields an empty string, never an error.
	Breadcrumbs string
	// PublicURL is the browser-facing link when the API supplies one. Empty means
	// the caller must build the link from the configured page base URL — never
	// from the API host.
	PublicURL string
}

// AccessVerdict is the outcome of an ACL check. Open is true ONLY when a
// positively recognised "the whole organization can read this" marker was found.
type AccessVerdict struct {
	Open   bool
	Reason string
	// Raw is a redacted, truncated echo of the ACL payload for the manifest, so a
	// human reviewing a dry run can see what the decision was based on.
	Raw string
}

// Candidate field paths. A path may be nested, "content.raw" meaning
// {"content": {"raw": "..."}}. Order is priority order: the first path that
// yields a usable value wins.
var (
	pageIDPaths   = []string{"id", "page_id", "pageId", "page.id"}
	pageSlugPaths = []string{"slug", "page_slug", "path", "page.slug"}
	titlePaths    = []string{"title", "name", "page.title"}
	// bodyPaths: "content" is CONFIRMED — with fields=content the live API puts
	// the body in a top-level "content" string. The rest stay as fallbacks for
	// other endpoints/versions; they cost nothing and are checked after.
	bodyPaths      = []string{"content", "body", "text", "markup", "source", "content.raw", "content.markdown", "body.content", "page.content"}
	listPaths      = []string{"results", "items", "pages", "data", "children"}
	cursorPaths    = []string{"next_cursor", "cursor", "next_page_token", "next", "paging.next"}
	versionPaths   = []string{"version", "revision", "updated_at", "modified_at", "edited_at"}
	publicURLPaths = []string{"url", "public_url", "web_url", "href"}
	formatPaths    = []string{"format", "content_type", "markup_type", "content.format"}
	depthPaths     = []string{"depth", "level", "nesting_level"}

	// pageTypePaths: "page_type" is CONFIRMED — it is one of the four keys the
	// live metadata response returns.
	pageTypePaths = []string{"page_type", "pageType", "type", "page.page_type"}
	// breadcrumbPaths hold the ancestry array requested via fields=breadcrumbs.
	breadcrumbPaths = []string{"breadcrumbs", "breadcrumb", "page.breadcrumbs"}
	// breadcrumbTitlePaths are read from each breadcrumb element. A crumb with a
	// title is what we want; slug is a last resort so a recognised structure is
	// never thrown away for want of a nicer label.
	breadcrumbTitlePaths = []string{"title", "name", "slug"}

	// aclOpenPaths are where an "who can read this" marker might live.
	aclOpenPaths = []string{"access", "access_type", "visibility", "scope", "type", "level", "acl.type", "access.type", "permission", "permissions.type"}
	// aclOpenFlags are boolean fields that, when true, mean org-wide readable.
	aclOpenFlags = []string{"is_public", "public", "org_wide", "is_org_wide", "everyone"}
)

// FieldsParam is the query parameter that asks GET /v1/pages for the optional
// parts of a page.
//
// THIS IS THE SINGLE MOST IMPORTANT FACT ABOUT THIS API, learned the hard way:
// WITHOUT `fields` THE ENDPOINT RETURNS METADATA ONLY. A live probe of
// GET /v1/pages?slug=… came back with exactly four keys — id, page_type, slug,
// title — and no body under ANY of the nine names in bodyPaths. The body is not
// hidden behind a differently-named field; it is simply not sent unless asked
// for. Documented values: attributes, breadcrumbs, content, redirect.
const FieldsParam = "fields"

// PageFields is what the crawler asks for. Keep it minimal: every extra part is
// more payload over a corporate API for data nothing downstream reads.
var PageFields = []string{"content", "breadcrumbs"}

// DefaultIndexablePageTypes is the default answer to "which page_type values
// hold prose worth indexing", and the answer is ALL OF THEM.
//
// It used to be []string{"page"}, and that was wrong in the most expensive way
// available: a live probe of a real page returned a page_type of SEVEN
// characters — not the four of "page" — so the default would have discarded
// every page of that wiki and produced an empty index with no error anywhere.
// Silently indexing less than was asked for is the failure nobody notices, so
// the default now errs the other way.
//
// Narrowing is the operator's decision, made with evidence: every page's actual
// type is written to the manifest, so a --dry-run shows the real set of values
// present in the wiki and what to exclude. This stays a variable and stays
// consulted through IsIndexablePageType — the full set of values the API can
// return is NOT known, so it must be adjustable without a code change.
var DefaultIndexablePageTypes = []string{"*"}

// IsIndexablePageType reports whether a page_type is one we index.
//
// An EMPTY page_type is accepted: the field is optional in older responses and
// on other endpoints, and refusing a page because a field is absent would be
// fail-closed in the wrong direction — it silently shrinks the index, which is
// the failure nobody notices. "*" in allowed accepts everything.
func IsIndexablePageType(pageType string, allowed []string) bool {
	t := strings.ToLower(strings.TrimSpace(pageType))
	if t == "" {
		return true
	}
	if len(allowed) == 0 {
		allowed = DefaultIndexablePageTypes
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "*" || a == t {
			return true
		}
	}
	return false
}

// parseBreadcrumbs renders the ancestry array as "A > B > C".
//
// The structure is NOT confirmed against the live API, so this is tolerant by
// design: an array of objects, an array of plain strings and a ready-made
// string all work, and anything else yields "" rather than an error. A page is
// perfectly indexable without a breadcrumb — failing the whole page over a
// decorative field would trade a real document for a nicety.
func parseBreadcrumbs(m map[string]any) string {
	for _, p := range breadcrumbPaths {
		v, ok := lookup(m, p)
		if !ok {
			continue
		}
		if s := breadcrumbString(v); s != "" {
			return s
		}
	}
	return ""
}

func breadcrumbString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)

	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			switch crumb := item.(type) {
			case string:
				if s := strings.TrimSpace(crumb); s != "" {
					parts = append(parts, s)
				}
			case map[string]any:
				if s, _, ok := pick(crumb, breadcrumbTitlePaths...); ok {
					if s = strings.TrimSpace(s); s != "" {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, " > ")

	default:
		return ""
	}
}

// DefaultACLOpenValues is the fail-closed allow-list of markers that count as
// "readable by the whole organization". Anything not on this list leaves the
// page closed. Overridable via --acl-open-values.
var DefaultACLOpenValues = []string{"organization", "org", "everyone", "all", "public"}

// FieldError reports that no candidate path matched. It prints what was looked
// for and which keys ACTUALLY arrived — names and types only, NEVER values,
// because this text ends up in logs and this is a corporate wiki.
type FieldError struct {
	Entity   string   // "page body", "page id", ...
	Tried    []string // candidate paths, in priority order
	Keys     []string // "name:type" of the keys actually present
	DumpPath string   // where the redacted raw payload was written, "" if not dumped
	Hint     string   // the known cause, when there is one
}

func (e *FieldError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "wiki API: no field found for %s", e.Entity)
	fmt.Fprintf(&b, "; tried: %s", strings.Join(e.Tried, ", "))
	if len(e.Keys) == 0 {
		b.WriteString("; response has no object keys at all")
	} else {
		fmt.Fprintf(&b, "; response keys: %s", strings.Join(e.Keys, ", "))
	}
	if e.DumpPath != "" {
		fmt.Fprintf(&b, "; raw response dumped to %s", e.DumpPath)
	}
	if e.Hint != "" {
		fmt.Fprintf(&b, "; %s", e.Hint)
	}
	b.WriteString("; fix internal/source/wiki/dto.go — it is the only file that knows API field names")
	return redact(b.String())
}

// pick resolves the first of paths that yields a non-empty scalar, and reports
// which path matched. Dotted paths descend into nested objects.
func pick(m map[string]any, paths ...string) (value string, matched string, ok bool) {
	for _, p := range paths {
		v, found := lookup(m, p)
		if !found {
			continue
		}
		s, ok := scalar(v)
		if !ok || s == "" {
			continue
		}
		return s, p, true
	}
	return "", "", false
}

// lookup walks a dotted path through nested maps.
func lookup(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// scalar renders a JSON scalar as a string. Objects and arrays are not scalars;
// numbers keep an integral form so a numeric page id becomes "42", not "42.000000".
func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case json.Number:
		return t.String(), true
	default:
		return "", false
	}
}

// keyTypes describes an object's keys as "name:type" — names and types only, no
// values. Used by FieldError and by probe.
func keyTypes(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+":"+jsonType(v))
	}
	sort.Strings(out)
	return out
}

// jsonType names the JSON type of v, adding a size for strings and arrays. Sizes
// are safe to print: a length is not content.
func jsonType(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64, json.Number:
		return "number"
	case string:
		return fmt.Sprintf("string(len=%d)", len([]rune(t)))
	case []any:
		return fmt.Sprintf("array(len=%d)", len(t))
	case map[string]any:
		return fmt.Sprintf("object(keys=%d)", len(t))
	default:
		return "unknown"
	}
}

// decodeObject unmarshals raw into a generic object.
func decodeObject(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("wiki API: response is not a JSON object: %w", err)
	}
	return m, nil
}

// ParsePage extracts an indexable page from a single-page response. The body is
// mandatory — a page we cannot read the text of is a hard error, because
// silently indexing an empty page would poison the index.
func ParsePage(raw json.RawMessage) (PageContent, error) {
	m, err := decodeObject(raw)
	if err != nil {
		return PageContent{}, err
	}
	// Some APIs wrap the payload; unwrap a single "result"/"page"/"data" object.
	m = unwrap(m, "result", "page", "data")

	var p PageContent
	p.ID, _, _ = pick(m, pageIDPaths...)
	p.Slug, _, _ = pick(m, pageSlugPaths...)
	p.Title, _, _ = pick(m, titlePaths...)
	p.Format, _, _ = pick(m, formatPaths...)
	p.Version, _, _ = pick(m, versionPaths...)
	p.UpdatedAt, _, _ = pick(m, "updated_at", "modified_at", "edited_at", "updatedAt")
	p.PublicURL, _, _ = pick(m, publicURLPaths...)
	p.PageType, _, _ = pick(m, pageTypePaths...)
	p.Breadcrumbs = parseBreadcrumbs(m)

	body, _, ok := pick(m, bodyPaths...)
	switch {
	case ok:
		p.Body = body

	case hasAny(m, bodyPaths...):
		// The key IS there, it is just empty — a section or index page with no
		// text of its own, which every real wiki has plenty of.
		//
		// This must not be a parse error. pick() treats "" as "not found", so the
		// old code reported "no field found for page body" and blamed both the
		// request encoding and dto.go — while the response plainly showed
		// content:string(len=0). Sending someone to edit the field-name table
		// because a page is blank is a diagnostic lie, and it hides the real
		// outcome: the page is empty and the quality gate should skip it as
		// content_empty.
		p.Body = ""

	default:
		// Note that everything above is already filled in: the returned
		// PageContent is usable even on this error path. Callers — the probe
		// above all — need the id to keep working when only the body is missing.
		return p, &FieldError{Entity: "page body", Tried: bodyPaths, Keys: keyTypes(m),
			Hint: "the live API returns METADATA ONLY unless the request carries " +
				FieldsParam + "=content — check that the request includes it"}
	}

	return p, nil
}

// hasAny reports whether any of the paths is present in the payload, whatever
// its value. It is the difference between "this API does not have such a field"
// and "this page has nothing in it" — two problems with nothing in common.
func hasAny(m map[string]any, paths ...string) bool {
	for _, p := range paths {
		if _, found := lookup(m, p); found {
			return true
		}
	}
	return false
}

// unwrap descends into a single-object envelope when one of the given keys holds
// an object and the payload itself carries none of the fields we need.
func unwrap(m map[string]any, keys ...string) map[string]any {
	if _, _, ok := pick(m, pageIDPaths...); ok {
		return m
	}
	if _, _, ok := pick(m, bodyPaths...); ok {
		return m
	}
	for _, k := range keys {
		if inner, ok := m[k].(map[string]any); ok {
			return inner
		}
	}
	return m
}

// ParsePageList extracts the page refs and the pagination cursor from a listing
// response. It tolerates both a bare array and an object envelope.
func ParsePageList(raw json.RawMessage) (items []PageRef, nextCursor string, err error) {
	// A bare top-level array: no envelope, hence no cursor.
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		return refsFrom(arr), "", nil
	}

	m, err := decodeObject(raw)
	if err != nil {
		return nil, "", err
	}

	var list []any
	for _, p := range listPaths {
		v, ok := lookup(m, p)
		if !ok {
			continue
		}
		if a, ok := v.([]any); ok {
			list = a
			break
		}
	}
	if list == nil {
		return nil, "", &FieldError{Entity: "page list", Tried: listPaths, Keys: keyTypes(m)}
	}

	cursor, _, _ := pick(m, cursorPaths...)
	return refsFrom(list), cursor, nil
}

// refsFrom converts list entries into PageRefs, skipping entries that carry no
// usable id — an entry we cannot address is an entry we cannot fetch.
//
// A MISSING TITLE IS NORMAL, not a defect: the live descendants endpoint returns
// exactly {id, slug} per element. The title only exists on the page response, so
// it is filled in later, when the page itself is fetched. Nothing here or in the
// walker may treat an empty Title as a reason to skip a page.
func refsFrom(list []any) []PageRef {
	out := make([]PageRef, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var r PageRef
		r.ID, _, _ = pick(obj, pageIDPaths...)
		r.Slug, _, _ = pick(obj, pageSlugPaths...)
		r.Title, _, _ = pick(obj, titlePaths...)
		if d, _, ok := pick(obj, depthPaths...); ok {
			r.Depth, _ = strconv.Atoi(d)
		}
		if r.ID == "" && r.Slug == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ParseAccess interprets an ACL response. It is strictly FAIL-CLOSED: Open is
// true only when a marker from openValues is positively recognised. Anything
// unparsed, unknown, or merely absent leaves the page closed with reason
// "acl_unknown" — the alternative, defaulting to open, would leak a restricted
// page into a support chat, and that failure is silent and expensive.
func ParseAccess(raw json.RawMessage, openValues []string) (AccessVerdict, error) {
	if len(openValues) == 0 {
		openValues = DefaultACLOpenValues
	}

	v := AccessVerdict{Open: false, Reason: "acl_unknown", Raw: rawSummary(raw)}

	m, err := decodeObject(raw)
	if err != nil {
		// Not even an object: stay closed, and do not treat it as a hard failure —
		// the caller records "acl_unknown" in the manifest and skips the page.
		return v, nil
	}
	m = unwrap(m, "result", "access", "data")

	// A recognised string marker.
	if got, path, ok := pick(m, aclOpenPaths...); ok {
		norm := strings.ToLower(strings.TrimSpace(got))
		for _, want := range openValues {
			if norm == strings.ToLower(strings.TrimSpace(want)) {
				v.Open = true
				v.Reason = "acl_open:" + path + "=" + norm
				return v, nil
			}
		}
		// Recognised the field but not the value: still closed, but say so, since
		// this is the single most likely thing a human needs to fix.
		v.Reason = "acl_restricted:" + path + "=" + norm
		return v, nil
	}

	// A boolean flag.
	for _, p := range aclOpenFlags {
		val, ok := lookup(m, p)
		if !ok {
			continue
		}
		b, ok := val.(bool)
		if !ok {
			continue
		}
		if b {
			v.Open = true
			v.Reason = "acl_open:" + p + "=true"
		} else {
			v.Reason = "acl_restricted:" + p + "=false"
		}
		return v, nil
	}

	return v, nil
}

// rawSummary renders an ACL payload for the manifest: key names and types only,
// never values, then redacted. A human reviewing a dry run needs to see the
// SHAPE of what the ACL endpoint returned to judge whether ParseAccess read it
// correctly — the values themselves are not needed for that.
func rawSummary(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "unparsed"
	}
	return redact(strings.Join(keyTypes(m), " "))
}
