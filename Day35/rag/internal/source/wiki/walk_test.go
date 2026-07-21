package wiki

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Case 1: an API that keeps handing back the same cursor must be detected, not
// followed forever. Without this the crawl never terminates and the only symptom
// is a job that runs until it is killed.
func TestWalkCursorLoopIsDetected(t *testing.T) {
	f := newFakeWiki(t)

	var requests int
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		requests++
		// Always the same cursor, with fresh-looking items each time.
		return http.StatusOK, listJSON("STUCK", "1|docs/a", "2|docs/b"), nil
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	var yielded int
	err := w.Descendants(context.Background(), "docs", func(PageRef) error {
		yielded++
		return nil
	})

	if !errors.Is(err, ErrCursorLoop) {
		t.Fatalf("error = %v, want ErrCursorLoop", err)
	}
	// The point of the test: it terminated, and quickly.
	if requests > 5 {
		t.Errorf("made %d requests before detecting the loop, want a handful", requests)
	}
	if requests < 2 {
		t.Errorf("made %d requests, want at least 2 (the loop is only visible on the second)", requests)
	}
	t.Logf("loop detected after %d request(s), %d page(s) yielded", requests, yielded)
}

// A cursor that keeps changing is still a loop if it never ends. The iteration
// counter is the backstop the seen-set cannot provide.
func TestWalkRunawayCursorIsBounded(t *testing.T) {
	f := newFakeWiki(t)

	var requests int
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		requests++
		// A fresh cursor and a fresh page id every single time.
		return http.StatusOK, listJSON(
			"cursor-"+itoa(requests),
			itoa(requests)+"|docs/p"+itoa(requests),
		), nil
	}

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRequests = 200 })
	w := NewWalker(c, Limits{})

	err := w.Descendants(context.Background(), "docs", func(PageRef) error { return nil })
	if err == nil {
		t.Fatal("an endless cursor chain returned no error")
	}
	// Either the request budget or the iteration cap must stop it. What matters
	// is that it stopped and said so.
	if !errors.Is(err, ErrCursorLoop) && !errors.Is(err, ErrRequestBudget) {
		t.Fatalf("error = %v, want ErrCursorLoop or ErrRequestBudget", err)
	}
	t.Logf("bounded after %d request(s): %v", requests, err)
}

// Case 2: the same page appearing on two pages of results must be yielded once.
func TestWalkDeduplicatesRepeatedPageID(t *testing.T) {
	f := newFakeWiki(t)

	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		if cursor == "" {
			return http.StatusOK, listJSON("PAGE2", "1|docs/a", "2|docs/b"), nil
		}
		// "2" repeats from the first page.
		return http.StatusOK, listJSON("", "2|docs/b", "3|docs/c"), nil
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	seen := map[string]int{}
	if err := w.Descendants(context.Background(), "docs", func(r PageRef) error {
		seen[r.ID]++
		return nil
	}); err != nil {
		t.Fatalf("Descendants: %v", err)
	}

	if len(seen) != 3 {
		t.Errorf("yielded %d distinct pages, want 3: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("page %s yielded %d times, want exactly 1", id, n)
		}
	}
}

// Case 9: overlapping roots must not index the same page twice. A user asking
// for both "a" and "a/b" is a normal mistake, not an error.
func TestWalkDeduplicatesAcrossRoots(t *testing.T) {
	f := newFakeWiki(t)

	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		switch slug {
		case "a":
			return http.StatusOK, listJSON("", "1|a", "2|a/b", "3|a/b/c"), nil
		case "a/b":
			// The same pages, reached from the narrower root.
			return http.StatusOK, listJSON("", "2|a/b", "3|a/b/c"), nil
		default:
			return http.StatusOK, listJSON(""), nil
		}
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	seen := map[string]int{}
	collect := func(r PageRef) error { seen[r.ID]++; return nil }

	for _, root := range []string{"a", "a/b"} {
		if err := w.Descendants(context.Background(), root, collect); err != nil {
			t.Fatalf("Descendants(%q): %v", root, err)
		}
	}

	if len(seen) != 3 {
		t.Errorf("yielded %d distinct pages across both roots, want 3: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("page %s yielded %d times across roots, want exactly 1", id, n)
		}
	}
	if w.Pages() != 3 {
		t.Errorf("Pages() = %d, want 3", w.Pages())
	}
}

// Case 8: every limit overrun must be an ERROR. Silently indexing a prefix of
// the wiki produces a bot that says "не знаю" about documented things, and the
// gap is invisible.
func TestWalkMaxPagesIsAnError(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b", "3|docs/c", "4|docs/d"), nil
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{MaxPages: 2})

	var yielded int
	err := w.Descendants(context.Background(), "docs", func(PageRef) error {
		yielded++
		return nil
	})

	if !errors.Is(err, ErrMaxPages) {
		t.Fatalf("error = %v, want ErrMaxPages — a truncated crawl must not look like success", err)
	}
	if yielded != 2 {
		t.Errorf("yielded %d pages before the limit, want 2", yielded)
	}
	if !strings.Contains(err.Error(), "INCOMPLETE") {
		t.Errorf("error does not say the crawl is incomplete: %v", err)
	}
}

func TestWalkMaxDepthIsAnError(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs|0", "2|docs/a|1", "3|docs/a/b|5"), nil
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{MaxDepth: 2})

	err := w.Descendants(context.Background(), "docs", func(PageRef) error { return nil })
	if !errors.Is(err, ErrMaxDepth) {
		t.Fatalf("error = %v, want ErrMaxDepth", err)
	}
}

func TestWalkMaxRequestsIsAnError(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		// Endless pagination, distinct cursors.
		return http.StatusOK, listJSON("c"+cursor+"x", "1|docs/a"), nil
	}

	c, _ := f.newTestClient(t, func(cfg *ClientConfig) { cfg.MaxRequests = 3 })
	w := NewWalker(c, Limits{})

	err := w.Descendants(context.Background(), "docs", func(PageRef) error { return nil })
	if !errors.Is(err, ErrRequestBudget) {
		t.Fatalf("error = %v, want ErrRequestBudget", err)
	}
	if got := c.Requests(); got != 3 {
		t.Errorf("issued %d requests, want exactly 3", got)
	}
}

// A non-empty cursor pointing at an empty result page is the API telling us to
// keep going with nothing to go on. Stop cleanly instead of spinning.
func TestWalkStopsOnEmptyPageWithCursor(t *testing.T) {
	f := newFakeWiki(t)

	var requests int
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		requests++
		if cursor == "" {
			return http.StatusOK, listJSON("NEXT", "1|docs/a"), nil
		}
		return http.StatusOK, listJSON("STILL-MORE"), nil // empty, but claims more
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	var yielded int
	err := w.Descendants(context.Background(), "docs", func(PageRef) error {
		yielded++
		return nil
	})
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if requests != 2 {
		t.Errorf("made %d requests, want 2", requests)
	}
	if yielded != 1 {
		t.Errorf("yielded %d pages, want 1", yielded)
	}
}

func TestWalkPaginatesNormally(t *testing.T) {
	f := newFakeWiki(t)

	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		switch cursor {
		case "":
			return http.StatusOK, listJSON("p2", "1|docs/a", "2|docs/b"), nil
		case "p2":
			return http.StatusOK, listJSON("p3", "3|docs/c"), nil
		default:
			return http.StatusOK, listJSON("", "4|docs/d"), nil
		}
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	var ids []string
	if err := w.Descendants(context.Background(), "docs", func(r PageRef) error {
		ids = append(ids, r.ID)
		return nil
	}); err != nil {
		t.Fatalf("Descendants: %v", err)
	}

	if strings.Join(ids, ",") != "1,2,3,4" {
		t.Errorf("ids = %v, want [1 2 3 4]", ids)
	}
}

// An error from the callback aborts the walk: the caller decides when to stop.
func TestWalkCallbackErrorAborts(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a", "2|docs/b"), nil
	}

	c, _ := f.newTestClient(t, nil)
	w := NewWalker(c, Limits{})

	sentinel := errors.New("stop here")
	var seen int
	err := w.Descendants(context.Background(), "docs", func(PageRef) error {
		seen++
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the callback's error", err)
	}
	if seen != 1 {
		t.Errorf("callback ran %d times after returning an error, want 1", seen)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// The descendants endpoint caps page_size at 100 (its own default is 50). Asking
// for more is not rejected by the server — it just returns fewer than asked, so
// an unclamped client would size its expectations against a number that never
// applied. The clamp is silent in the sense that it does not fail the run, but
// it is announced, because "I asked for 500" and "I got 100" must not diverge
// without anyone being told.
func TestWalkClampsPageSizeToAPIMaximum(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		return http.StatusOK, listJSON("", "1|docs/a"), nil
	}

	c, _ := f.newTestClient(t, nil)
	var log bytes.Buffer
	w := NewWalker(c, Limits{})
	w.Log = &log
	w.PageSize = 500

	if err := w.Descendants(context.Background(), "docs", func(PageRef) error { return nil }); err != nil {
		t.Fatalf("Descendants: %v", err)
	}

	if w.PageSize != maxPageSize {
		t.Errorf("PageSize = %d after the walk, want it clamped to %d", w.PageSize, maxPageSize)
	}
	for _, h := range f.hitLog() {
		if strings.Contains(h, "page_size=500") {
			t.Errorf("the over-large page_size went out on the wire: %s", h)
		}
	}
	if !strings.Contains(f.hitLog()[0], "page_size=100") {
		t.Errorf("request did not carry the clamped page_size: %s", f.hitLog()[0])
	}
	if !strings.Contains(log.String(), "exceeds the API maximum") {
		t.Errorf("the clamp was not reported to the user; log: %q", log.String())
	}
	t.Logf("descendants request as sent: %s", f.hitLog()[0])
}

// The cursor parameter is named `cursor` — confirmed against the live API, whose
// listing response carries next_cursor and prev_cursor.
func TestWalkSendsCursorParameter(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		if cursor == "" {
			return http.StatusOK, listJSON("NEXT", "1|docs/a"), nil
		}
		return http.StatusOK, listJSON("", "2|docs/b"), nil
	}

	c, _ := f.newTestClient(t, nil)
	var got int
	if err := NewWalker(c, Limits{}).Descendants(context.Background(), "docs", func(PageRef) error {
		got++
		return nil
	}); err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if got != 2 {
		t.Fatalf("yielded %d pages, want 2 — the second page was never requested", got)
	}
	if !strings.Contains(strings.Join(f.hitLog(), "\n"), "cursor=NEXT") {
		t.Errorf("the cursor was not sent as `cursor`:\n%s", strings.Join(f.hitLog(), "\n"))
	}
}

// The live descendants endpoint returns {id, slug} per element and NOTHING else
// — no title. That is not a defect and must never be treated as one: the title
// arrives with the page itself. A walker that skipped title-less entries would
// index an empty wiki against a perfectly healthy API.
func TestWalkAcceptsListingEntriesWithoutTitle(t *testing.T) {
	f := newFakeWiki(t)
	f.descendants = func(slug, cursor string) (int, string, map[string]string) {
		// Verbatim shape of the real response.
		return http.StatusOK, `{"next_cursor":null,"prev_cursor":null,` +
			`"results":[{"id":48952013,"slug":"docs/a"},{"id":48952014,"slug":"docs/b"}]}`, nil
	}

	c, _ := f.newTestClient(t, nil)
	var refs []PageRef
	if err := NewWalker(c, Limits{}).Descendants(context.Background(), "docs", func(r PageRef) error {
		refs = append(refs, r)
		return nil
	}); err != nil {
		t.Fatalf("Descendants: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("yielded %d pages, want 2 — title-less entries were dropped", len(refs))
	}
	for _, r := range refs {
		if r.Title != "" {
			t.Errorf("ref %+v: expected no title from the listing", r)
		}
		if r.ID == "" || r.Slug == "" {
			t.Errorf("ref %+v: id and slug are what the listing does provide", r)
		}
	}
	if refs[0].ID != "48952013" {
		t.Errorf("numeric id became %q, want %q", refs[0].ID, "48952013")
	}
}
