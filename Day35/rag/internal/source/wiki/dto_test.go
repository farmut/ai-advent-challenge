package wiki

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Every candidate name for the body field must be recognised. Until the probe is
// run against the live API we do not know which one is real, so all of them have
// to work.
func TestParsePageBodyFieldVariants(t *testing.T) {
	const body = "Текст страницы про отпуска и компенсации."

	for _, field := range []string{"content", "body", "text", "markup", "source"} {
		t.Run(field, func(t *testing.T) {
			raw := mustJSON(t, map[string]any{
				"id":    "42",
				"slug":  "docs/vacation",
				"title": "Отпуска",
				field:   body,
			})

			page, err := ParsePage(raw)
			if err != nil {
				t.Fatalf("ParsePage with body in %q: %v", field, err)
			}
			if page.Body != body {
				t.Errorf("Body = %q, want %q", page.Body, body)
			}
			if page.ID != "42" || page.Slug != "docs/vacation" || page.Title != "Отпуска" {
				t.Errorf("unexpected page: %+v", page)
			}
		})
	}
}

func TestParsePageNestedBodyPaths(t *testing.T) {
	tests := []struct {
		name string
		doc  map[string]any
	}{
		{"content.raw", map[string]any{"id": "1", "content": map[string]any{"raw": "тело"}}},
		{"content.markdown", map[string]any{"id": "1", "content": map[string]any{"markdown": "тело"}}},
		{"body.content", map[string]any{"id": "1", "body": map[string]any{"content": "тело"}}},
		{"page.content", map[string]any{"id": "1", "page": map[string]any{"content": "тело"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := ParsePage(mustJSON(t, tt.doc))
			if err != nil {
				t.Fatalf("ParsePage: %v", err)
			}
			if page.Body != "тело" {
				t.Errorf("Body = %q, want %q", page.Body, "тело")
			}
		})
	}
}

// A numeric page id must survive as "42", not "42.000000" — it becomes the
// document's primary key.
func TestParsePageNumericID(t *testing.T) {
	page, err := ParsePage(json.RawMessage(`{"id": 42, "content": "тело страницы"}`))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if page.ID != "42" {
		t.Errorf("ID = %q, want %q", page.ID, "42")
	}
}

// The crucial diagnostic: when no candidate matches, the error must name the
// keys that DID arrive so dto.go can be fixed — and must not print their values,
// because this text lands in logs and bug reports.
func TestParsePageMissingBodyReportsKeysNotValues(t *testing.T) {
	const secretValue = "Зарплатная вилка: 300000 рублей"

	raw := mustJSON(t, map[string]any{
		"id":            "42",
		"title":         "Компенсации",
		"unexpected":    secretValue,
		"nested_thing":  map[string]any{"a": 1},
		"numbers":       []any{1, 2, 3},
		"another_field": secretValue,
	})

	_, err := ParsePage(raw)
	if err == nil {
		t.Fatal("ParsePage succeeded without a body field, want FieldError")
	}

	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *FieldError", err)
	}

	msg := err.Error()

	// It must name what was looked for.
	for _, tried := range []string{"content", "body", "text", "content.raw"} {
		if !strings.Contains(msg, tried) {
			t.Errorf("error does not mention candidate %q:\n%s", tried, msg)
		}
	}

	// It must list the keys actually present, with their types.
	for _, want := range []string{"unexpected:string", "nested_thing:object", "numbers:array", "title:string"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not report key %q:\n%s", want, msg)
		}
	}

	// It must NOT leak any value.
	if strings.Contains(msg, secretValue) {
		t.Errorf("error leaked a field VALUE — only names and types may be printed:\n%s", msg)
	}
	if strings.Contains(msg, "300000") {
		t.Errorf("error leaked value content:\n%s", msg)
	}
	if strings.Contains(msg, "Компенсации") {
		t.Errorf("error leaked the title value:\n%s", msg)
	}
}

func TestParsePageListVariants(t *testing.T) {
	for _, field := range []string{"results", "items", "pages", "data", "children"} {
		t.Run(field, func(t *testing.T) {
			raw := mustJSON(t, map[string]any{
				field:         []any{map[string]any{"id": "1", "slug": "a"}, map[string]any{"id": "2", "slug": "a/b"}},
				"next_cursor": "CUR",
			})

			items, cursor, err := ParsePageList(raw)
			if err != nil {
				t.Fatalf("ParsePageList: %v", err)
			}
			if len(items) != 2 {
				t.Fatalf("got %d items, want 2", len(items))
			}
			if cursor != "CUR" {
				t.Errorf("cursor = %q, want %q", cursor, "CUR")
			}
		})
	}
}

func TestParsePageListBareArray(t *testing.T) {
	items, cursor, err := ParsePageList(json.RawMessage(`[{"id":"1","slug":"a"}]`))
	if err != nil {
		t.Fatalf("ParsePageList: %v", err)
	}
	if len(items) != 1 || items[0].ID != "1" {
		t.Errorf("items = %+v, want one item with id 1", items)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty for a bare array", cursor)
	}
}

func TestParsePageListMissingListReportsKeys(t *testing.T) {
	_, _, err := ParsePageList(mustJSON(t, map[string]any{"total": 5, "whatever": "секрет"}))
	if err == nil {
		t.Fatal("ParsePageList succeeded without a list field")
	}
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *FieldError", err)
	}
	if !strings.Contains(err.Error(), "total:number") {
		t.Errorf("error does not report the real keys: %s", err)
	}
	if strings.Contains(err.Error(), "секрет") {
		t.Errorf("error leaked a value: %s", err)
	}
}

// ParseAccess is the highest-risk contract in the feature: reading it wrong
// leaks a restricted page into a support chat. It must be fail-closed.
func TestParseAccessFailsClosedOnUnrecognisedJSON(t *testing.T) {
	unrecognised := []string{
		`{}`,
		`{"foo":"bar"}`,
		`{"access":"team-only"}`,
		`{"visibility":"restricted"}`,
		`{"some":{"deeply":{"nested":"thing"}}}`,
		`[]`,
		`"just a string"`,
		`null`,
		`not json at all`,
		`{"is_public": false}`,
		`{"access": 123}`,
	}

	for _, in := range unrecognised {
		v, err := ParseAccess(json.RawMessage(in), nil)
		if err != nil {
			t.Fatalf("ParseAccess(%s) returned an error: %v", in, err)
		}
		if v.Open {
			t.Errorf("ParseAccess(%s).Open = true — MUST be fail-closed", in)
		}
		if v.Reason == "" {
			t.Errorf("ParseAccess(%s) gave no reason", in)
		}
	}
}

func TestParseAccessRecognisesOpenMarkers(t *testing.T) {
	open := []string{
		`{"access":"organization"}`,
		`{"access":"ORG"}`,
		`{"visibility":"everyone"}`,
		`{"scope":"all"}`,
		`{"access_type":"public"}`,
		`{"access":{"type":"organization"}}`,
		`{"is_public": true}`,
		`{"org_wide": true}`,
	}

	for _, in := range open {
		v, err := ParseAccess(json.RawMessage(in), nil)
		if err != nil {
			t.Fatalf("ParseAccess(%s): %v", in, err)
		}
		if !v.Open {
			t.Errorf("ParseAccess(%s).Open = false, want true (reason %q)", in, v.Reason)
		}
	}
}

func TestParseAccessCustomOpenValues(t *testing.T) {
	raw := json.RawMessage(`{"access":"company-wide"}`)

	if v, _ := ParseAccess(raw, nil); v.Open {
		t.Error("custom marker must not be open with the default list")
	}
	if v, _ := ParseAccess(raw, []string{"company-wide"}); !v.Open {
		t.Error("custom marker must be open once configured")
	}
}

// The ACL summary written into the manifest must describe the shape only.
func TestParseAccessRawSummaryHasNoValues(t *testing.T) {
	v, _ := ParseAccess(json.RawMessage(`{"access":"team","owner":"ivanov@example.com"}`), nil)

	if strings.Contains(v.Raw, "ivanov@example.com") {
		t.Errorf("ACL summary leaked a value: %q", v.Raw)
	}
	if !strings.Contains(v.Raw, "owner:string") {
		t.Errorf("ACL summary does not describe the keys: %q", v.Raw)
	}
}

func TestPickNestedAndPriority(t *testing.T) {
	m := map[string]any{
		"body":    "",                              // empty: must be skipped
		"content": map[string]any{"raw": "нужное"}, // nested
	}

	got, matched, ok := pick(m, "body", "content.raw")
	if !ok {
		t.Fatal("pick found nothing")
	}
	if got != "нужное" {
		t.Errorf("value = %q, want %q", got, "нужное")
	}
	if matched != "content.raw" {
		t.Errorf("matched = %q, want %q", matched, "content.raw")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// page_type is a CONFIRMED field of the live response. Only "page" is known to
// hold prose; the rest (tables, grids) serialise into noise. The set is
// configurable because the full list of values the API can return is unknown —
// hardcoding a white-list would mean a product change silently emptying the
// index.
func TestIsIndexablePageType(t *testing.T) {
	cases := []struct {
		name     string
		pageType string
		allowed  []string
		want     bool
	}{
		// The DEFAULT policy indexes every type. It used to be "page" only, and a
		// live probe showed the real value was a 7-character type — so that default
		// would have dropped every page of a real wiki and produced an empty index
		// with nothing anywhere to explain it.
		{"ordinary page, default policy", "page", nil, true},
		{"grid, default policy", "grid", nil, true},
		{"table, default policy", "table", nil, true},
		{"a type nobody anticipated, default policy", "cloud_page", nil, true},
		{"case is irrelevant", "PAGE", nil, true},
		{"surrounding space is irrelevant", " page ", nil, true},
		{"an absent field is not a reason to skip", "", nil, true},

		// Narrowing is what --page-type is for, and it still works exactly.
		{"an operator may narrow to prose only", "grid", []string{"page"}, false},
		{"a narrowed policy keeps what it names", "page", []string{"page"}, true},
		{"an operator may name several types", "grid", []string{"page", "grid"}, true},
		{"a star accepts everything", "whatever-ships-next", []string{"*"}, true},
		{"a narrowed policy still works", "page", []string{"cloud_page"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsIndexablePageType(tc.pageType, tc.allowed); got != tc.want {
				t.Errorf("IsIndexablePageType(%q, %v) = %t, want %t", tc.pageType, tc.allowed, got, tc.want)
			}
		})
	}
}

// The breadcrumbs shape is NOT confirmed. Parsing is therefore tolerant in both
// directions: several plausible shapes are understood, and anything else yields
// an empty string. Failing a page over a decorative field would trade a real
// document for a nicety.
func TestParseBreadcrumbs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "array of objects with titles — the expected shape",
			raw:  `{"breadcrumbs":[{"title":"A","slug":"a"},{"title":"B","slug":"a/b"},{"title":"C","slug":"a/b/c"}]}`,
			want: "A > B > C",
		},
		{
			name: "objects carrying name instead of title",
			raw:  `{"breadcrumbs":[{"name":"Отдел"},{"name":"HR"}]}`,
			want: "Отдел > HR",
		},
		{
			name: "slug is used when there is no human label",
			raw:  `{"breadcrumbs":[{"slug":"docs"},{"slug":"docs/faq"}]}`,
			want: "docs > docs/faq",
		},
		{
			name: "an array of plain strings",
			raw:  `{"breadcrumbs":["A","B"]}`,
			want: "A > B",
		},
		{
			name: "a ready-made string is passed through",
			raw:  `{"breadcrumbs":"A > B"}`,
			want: "A > B",
		},
		{
			name: "unlabelled crumbs are skipped, not fatal",
			raw:  `{"breadcrumbs":[{"id":1},{"title":"B"},{"id":3}]}`,
			want: "B",
		},
		{"garbage: a number", `{"breadcrumbs":42}`, ""},
		{"garbage: an object", `{"breadcrumbs":{"a":1}}`, ""},
		{"garbage: objects with nothing usable", `{"breadcrumbs":[{"id":1},{"id":2}]}`, ""},
		{"garbage: an empty array", `{"breadcrumbs":[]}`, ""},
		{"absent altogether", `{"title":"T"}`, ""},
		{"null", `{"breadcrumbs":null}`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := decodeObject([]byte(tc.raw))
			if err != nil {
				t.Fatalf("decodeObject: %v", err)
			}
			if got := parseBreadcrumbs(m); got != tc.want {
				t.Errorf("parseBreadcrumbs = %q, want %q", got, tc.want)
			}
		})
	}
}

// The two new fields must reach PageContent, and a body-less response must still
// carry everything else it had — that partial result is what keeps the probe
// working when only the body is missing.
func TestParsePageReadsPageTypeAndBreadcrumbs(t *testing.T) {
	raw := []byte(`{"id":48952013,"page_type":"page","slug":"docs/a","title":"T",
		"content":"тело страницы","breadcrumbs":[{"title":"Док"},{"title":"A"}]}`)

	page, err := ParsePage(raw)
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if page.PageType != "page" {
		t.Errorf("PageType = %q, want %q", page.PageType, "page")
	}
	if page.Breadcrumbs != "Док > A" {
		t.Errorf("Breadcrumbs = %q, want %q", page.Breadcrumbs, "Док > A")
	}
	if page.ID != "48952013" {
		t.Errorf("ID = %q, want the numeric id rendered without a decimal point", page.ID)
	}
}

// The exact response the live API returns without `fields`. Everything except
// the body must survive, and the error must name the real cause: this is THE
// failure mode of this API, and a misdiagnosis costs an afternoon of editing
// bodyPaths that were never wrong.
func TestParsePageOnLiveMetadataOnlyResponse(t *testing.T) {
	raw := []byte(`{"id":48952013,"page_type":"page","slug":"docs/some/long/slug","title":"Заголовок страницы"}`)

	page, err := ParsePage(raw)
	if err == nil {
		t.Fatal("a metadata-only response must be reported as a missing body")
	}

	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *FieldError", err)
	}
	if !strings.Contains(err.Error(), FieldsParam+"=content") {
		t.Errorf("the error does not name the missing parameter: %v", err)
	}
	// The keys that actually arrived are the actionable half of the message.
	if !strings.Contains(err.Error(), "page_type") {
		t.Errorf("the error does not list the keys that arrived: %v", err)
	}
	// And no VALUES leak into it — this text lands in logs.
	if strings.Contains(err.Error(), "Заголовок") {
		t.Errorf("the error leaked a field value: %v", err)
	}

	if page.ID != "48952013" || page.Title != "Заголовок страницы" || page.PageType != "page" {
		t.Errorf("partial page = %+v, want id, title and page_type despite the error", page)
	}
}

// TestParsePageEmptyBodyIsNotAParseError separates two problems that share no
// cause: "this API has no such field" and "this page is blank".
//
// pick() treats "" as not-found, so a present-but-empty content used to raise
// FieldError — a message that blames the request encoding and sends the reader
// to edit the field-name table, while the response it quotes plainly shows
// content:string(len=0). Real wikis are full of section and index pages with no
// text of their own; each one produced that accusation.
func TestParsePageEmptyBodyIsNotAParseError(t *testing.T) {
	raw := []byte(`{"id":"p1","slug":"docs/section","title":"Раздел","page_type":"cloud_p","content":""}`)

	p, err := ParsePage(raw)
	if err != nil {
		t.Fatalf("an empty body was reported as a parse error: %v", err)
	}
	if p.Body != "" {
		t.Errorf("Body = %q, want empty", p.Body)
	}
	// The metadata must survive, so the page can be recorded and skipped by the
	// quality gate rather than vanishing into an error path.
	if p.ID != "p1" || p.Slug != "docs/section" || p.Title != "Раздел" {
		t.Errorf("metadata was lost on the empty-body path: %+v", p)
	}
}

// TestParsePageMissingBodyStillErrors is the other half: when the key is absent
// entirely, that IS the field-name problem, and the loud error must stay.
func TestParsePageMissingBodyStillErrors(t *testing.T) {
	raw := []byte(`{"id":"p1","slug":"docs/section","title":"Раздел","page_type":"cloud_p"}`)

	p, err := ParsePage(raw)
	if err == nil {
		t.Fatal("a response with no body field at all was accepted")
	}
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Errorf("error is not a FieldError: %v", err)
	}
	// The id must still come back — the probe relies on it to keep working when
	// only the body is unresolved.
	if p.ID != "p1" {
		t.Errorf("ID = %q, want the id to survive the error path", p.ID)
	}
}
