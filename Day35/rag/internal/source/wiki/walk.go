package wiki

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

// Walk sentinel errors. All of them are HARD failures, never silent truncation:
// an index that quietly stopped halfway is an index that answers "не знаю" to
// questions it should have answered, and nobody notices for weeks.
var (
	// ErrCursorLoop means the API kept handing back a cursor that had already
	// been seen. Without this check the walk never terminates.
	ErrCursorLoop = errors.New("wiki: pagination cursor loop detected")
	// ErrMaxPages / ErrMaxDepth / ErrMaxBytes report an exceeded budget.
	ErrMaxPages = errors.New("wiki: page limit exceeded")
	ErrMaxDepth = errors.New("wiki: depth limit exceeded")
	ErrMaxBytes = errors.New("wiki: byte limit exceeded")
)

// maxCursorIterations bounds pagination even if the cursor value changes on
// every page (a server bug that seen-set detection alone would not catch).
const maxCursorIterations = 10000

// Page sizing for the descendants endpoint. The API's own default is 50 and its
// MAXIMUM is 100 — asking for more is not an error the server reports, it just
// silently gives back fewer, so a caller that asked for 500 and got 100 would
// have no idea. The walker clamps instead, and says so.
const (
	defaultPageSize = 100
	maxPageSize     = 100
)

// Limits bound a crawl. A zero field means "no limit" for that dimension.
type Limits struct {
	MaxPages    int
	MaxDepth    int
	MaxRequests int
	MaxBytes    int64
}

// Walker paginates the descendants endpoint.
type Walker struct {
	Client   *Client
	Limits   Limits
	PageSize int
	// Log receives warnings such as a clamped page size; nil discards them.
	Log io.Writer

	// seen deduplicates by page id ACROSS roots, so overlapping --root a and
	// --root a/b yield each page exactly once.
	seen  map[string]bool
	pages int
}

// NewWalker builds a Walker. The dedup set lives on the Walker, not on a single
// Descendants call, precisely so that repeated roots share it.
func NewWalker(c *Client, lim Limits) *Walker {
	return &Walker{Client: c, Limits: lim, PageSize: defaultPageSize, seen: make(map[string]bool)}
}

// Seen reports whether the walker has already yielded this page id.
func (w *Walker) Seen(id string) bool { return w.seen[id] }

// Descendants walks root and every page beneath it, calling fn once per
// previously unseen page. An error from fn aborts the walk.
func (w *Walker) Descendants(ctx context.Context, root string, fn func(PageRef) error) error {
	if w.seen == nil {
		w.seen = make(map[string]bool)
	}
	if w.PageSize <= 0 {
		w.PageSize = defaultPageSize
	}
	if w.PageSize > maxPageSize {
		// Not an error: the crawl is still complete, it just takes more requests.
		// Erroring here would block a run over a harmless over-ask. But it is not
		// silent either — a user who asked for 500 must learn they got 100, or
		// they will size their rate limits against a number that never applied.
		w.warnf("wiki: page_size %d exceeds the API maximum of %d — using %d (the crawl is unaffected, it just needs more requests)\n",
			w.PageSize, maxPageSize, maxPageSize)
		w.PageSize = maxPageSize
	}

	var (
		cursor      string
		usedCursors = map[string]bool{}
		iterations  int
	)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		iterations++
		if iterations > maxCursorIterations {
			return fmt.Errorf("%w: %d pagination requests for root %q without an end",
				ErrCursorLoop, iterations, redact(root))
		}

		q := url.Values{
			"slug":         {root},
			"include_self": {"true"},
			"page_size":    {strconv.Itoa(w.PageSize)},
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		raw, err := w.Client.GetJSON(ctx, "/v1/pages/descendants", q)
		if err != nil {
			return fmt.Errorf("wiki: list descendants of %q: %w", redact(root), err)
		}

		items, next, err := ParsePageList(raw)
		if err != nil {
			return fmt.Errorf("wiki: parse descendants of %q: %w", redact(root), err)
		}

		if err := w.emit(ctx, items, fn); err != nil {
			return err
		}

		// Terminal conditions, in order of certainty.
		if next == "" {
			return nil
		}
		if len(items) == 0 {
			// A non-empty cursor pointing at an empty page: the API is telling us
			// to keep going and giving us nothing to go on. Stop rather than spin.
			return nil
		}
		if next == cursor || usedCursors[next] {
			return fmt.Errorf("%w: the API returned an already-seen cursor after %d request(s) for root %q",
				ErrCursorLoop, iterations, redact(root))
		}

		usedCursors[next] = true
		cursor = next
	}
}

// emit applies the per-page budgets and hands unseen pages to fn.
func (w *Walker) emit(ctx context.Context, items []PageRef, fn func(PageRef) error) error {
	for _, ref := range items {
		if err := ctx.Err(); err != nil {
			return err
		}

		id := ref.ID
		if id == "" {
			// No id: fall back to the normalised slug so the page is still
			// deduplicated rather than emitted once per listing that mentions it.
			id = normalizeSlug(ref.Slug)
		}
		if id == "" || w.seen[id] {
			continue
		}

		if w.Limits.MaxDepth > 0 && ref.Depth > w.Limits.MaxDepth {
			return fmt.Errorf("%w: page %q is at depth %d, limit is %d (raise --max-depth or narrow --root)",
				ErrMaxDepth, redact(ref.Slug), ref.Depth, w.Limits.MaxDepth)
		}
		if w.Limits.MaxPages > 0 && w.pages >= w.Limits.MaxPages {
			return fmt.Errorf("%w: reached %d pages (raise --max-pages or narrow --allow; the crawl is INCOMPLETE)",
				ErrMaxPages, w.Limits.MaxPages)
		}

		w.seen[id] = true
		w.pages++

		if err := fn(ref); err != nil {
			return err
		}
	}
	return nil
}

// Pages reports how many pages have been yielded across all roots.
func (w *Walker) Pages() int { return w.pages }

func (w *Walker) warnf(format string, args ...any) {
	if w.Log == nil {
		return
	}
	fmt.Fprintf(w.Log, format, args...)
}
