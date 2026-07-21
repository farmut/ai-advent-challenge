package wiki

// fetch.go owns page retrieval and the one piece of the request contract that
// is still guesswork.
//
// CONFIRMED: GET /v1/pages returns metadata only — id, page_type, slug, title —
// unless the request asks for more via `fields`. With fields=content the body
// arrives in a top-level "content" string.
//
// CONFIRMED: the array parameter is COMMA-SEPARATED. A live probe against
// api.wiki.yandex.net answered
//
//	?fields=content%2Cbreadcrumbs&slug=…   →  body present
//
// while the repeated form (?fields=content&fields=breadcrumbs) was ignored and
// produced a metadata-only response. So the comma form is the default and the
// ordinary run costs exactly one request per page.
//
// The repeated form is kept only as a FALLBACK. Guessing wrong here is not a
// loud failure — it is a body-less response, i.e. an index that quietly contains
// nothing — so a deployment that behaves differently must still be crawlable
// rather than silently empty. Whichever encoding produced a body is remembered
// for the rest of the run, so at most a couple of pages ever cost two requests,
// and the choice is logged once.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
)

// fieldsStyle is an encoding for the `fields` array parameter.
type fieldsStyle int

const (
	// fieldsComma is the default: ONE parameter carrying a comma-joined list.
	// Confirmed against the live API — see the file comment.
	fieldsComma fieldsStyle = iota
	// fieldsRepeated is the fallback: one parameter occurrence per value.
	fieldsRepeated
)

func (s fieldsStyle) String() string {
	if s == fieldsComma {
		return "comma-separated (" + FieldsParam + "=a,b)"
	}
	return "repeated (" + FieldsParam + "=a&" + FieldsParam + "=b)"
}

// maxNegotiationAttempts bounds how many pages may cost a double request while
// the encoding is still unknown.
//
// Without this bound a wiki whose first pages are legitimately empty — a real
// and common case — would retry EVERY page in the other encoding forever,
// doubling the request count of the whole crawl against a rate-limited
// corporate API to answer a question that has no answer. After this many
// inconclusive pages the fetcher settles on the default encoding and says so.
const maxNegotiationAttempts = 3

// PageFetcher retrieves pages and resolves the `fields` encoding once per run.
// It is safe for concurrent use; the crawl is sequential today, but the resolved
// encoding is per-run state and must not become a data race the day it is not.
type PageFetcher struct {
	client *Client
	log    io.Writer

	mu       sync.Mutex
	style    fieldsStyle
	resolved bool
	attempts int
	settled  bool
}

// NewPageFetcher builds a fetcher. log receives the one line naming the encoding
// that worked; nil discards it.
func NewPageFetcher(c *Client, log io.Writer) *PageFetcher {
	if log == nil {
		log = io.Discard
	}
	return &PageFetcher{client: c, log: log}
}

// Fetch retrieves one page by slug, or by id when slug is empty. It returns the
// parsed page, the raw response (for diagnostics) and the parse error, if any.
//
// A parse error is returned ALONG WITH the partially filled page rather than
// instead of it: an id recovered from a body-less response is exactly what the
// probe needs to keep going.
func (f *PageFetcher) Fetch(ctx context.Context, slug, id string) (PageContent, json.RawMessage, error) {
	var (
		firstPage PageContent
		firstRaw  json.RawMessage
		firstErr  error
	)

	for i, style := range f.plan() {
		raw, err := f.client.GetJSON(ctx, pagePath, pageQuery(slug, id, style))
		if err != nil {
			// A transport or API failure says nothing about the encoding. Retrying
			// it in the other encoding would misattribute an outage to a query
			// format and burn the request budget doing it.
			return PageContent{}, nil, err
		}

		page, perr := ParsePage(raw)
		if perr == nil {
			f.confirm(style)
			return page, raw, nil
		}
		if i == 0 {
			firstPage, firstRaw, firstErr = page, raw, perr
		}
	}

	f.inconclusive()
	return firstPage, firstRaw, firstErr
}

// RequestURL renders the exact URL Fetch would issue right now, for diagnostics
// and tests. It goes through the same query builder as the real request, so it
// cannot drift from it.
func (f *PageFetcher) RequestURL(slug, id string) string {
	f.mu.Lock()
	style := f.style
	f.mu.Unlock()
	return f.client.URL(pagePath, pageQuery(slug, id, style))
}

// Style reports the encoding in use and whether a response confirmed it.
func (f *PageFetcher) Style() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.style.String(), f.resolved
}

// plan returns the encodings to try, in order: the current one first — which on
// a fresh fetcher is the confirmed comma form — then the other as a fallback.
// Once one is confirmed it is the only one tried.
func (f *PageFetcher) plan() []fieldsStyle {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.resolved || f.attempts >= maxNegotiationAttempts {
		return []fieldsStyle{f.style}
	}
	if f.style == fieldsRepeated {
		return []fieldsStyle{fieldsRepeated, fieldsComma}
	}
	return []fieldsStyle{fieldsComma, fieldsRepeated}
}

// confirm records the encoding that produced a body and announces it once.
func (f *PageFetcher) confirm(s fieldsStyle) {
	f.mu.Lock()
	announce := !f.resolved
	f.style, f.resolved = s, true
	f.mu.Unlock()

	if announce {
		fmt.Fprintf(f.log, "wiki: %s array encoding: %s\n", FieldsParam, s)
	}
}

// inconclusive records a page that produced no body in either encoding, and
// gives up negotiating once the budget is spent.
func (f *PageFetcher) inconclusive() {
	f.mu.Lock()
	f.attempts++
	give := !f.resolved && !f.settled && f.attempts >= maxNegotiationAttempts
	if give {
		f.settled = true
	}
	style := f.style
	f.mu.Unlock()

	if give {
		fmt.Fprintf(f.log, "wiki: %d page(s) returned no body in either %s encoding; "+
			"settling on %s and stopping the retries. If the whole crawl indexes nothing, "+
			"the body field name is wrong — fix bodyPaths in internal/source/wiki/dto.go\n",
			maxNegotiationAttempts, FieldsParam, style)
	}
}

// pagePath is the single-page endpoint.
const pagePath = "/v1/pages"

// pageQuery builds the query for one page request, including `fields` — without
// which the response carries no body at all.
func pageQuery(slug, id string, style fieldsStyle) url.Values {
	q := url.Values{}
	if strings.TrimSpace(slug) != "" {
		q.Set("slug", slug)
	} else if strings.TrimSpace(id) != "" {
		q.Set("id", id)
	}
	setFields(q, style)
	return q
}

// setFields writes the `fields` parameter in the given encoding.
func setFields(q url.Values, style fieldsStyle) {
	if style == fieldsComma {
		q.Set(FieldsParam, strings.Join(PageFields, ","))
		return
	}
	for _, f := range PageFields {
		q.Add(FieldsParam, f)
	}
}
