// Package wiki implements source.Source over the Yandex 360 wiki API.
//
// The crawl is deliberately conservative. Two failure modes drive every design
// decision here, because both are silent and expensive:
//
//   - Indexing a page the user was not allowed to read. It surfaces later as a
//     support bot quoting an internal document to the wrong audience. Hence a
//     page whose permissions come back restricted, or whose ACL request fails
//     for any reason other than "the endpoint does not exist", is skipped.
//     The live API has no working permission endpoint at all, so see ACLMode
//     for what is done about that and what it costs.
//   - Silently indexing less than was asked for. It surfaces as a bot that says
//     "не знаю" about documented things, and nobody notices for weeks. Hence
//     every limit overrun is an error, never a truncation.
package wiki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"rag/internal/source"
)

// ACLMode selects how the crawler behaves when the page-permission endpoint
// cannot answer.
//
// The endpoint problem is real and confirmed, not hypothetical: a live probe of
// GET /v1/pages/{id}/access returned HTTP 405 Method not allowed, and neither
// /access-info, /permissions nor fields=attributes carries the information
// either. The published API simply has no way to read a page's permissions —
// the closest thing that exists, user_permissions, applies to grids, not pages.
//
// So the crawler cannot both check permissions and index anything. Which of
// those two failures to accept is the OPERATOR's decision, not the code's, and
// that is exactly why this is a mode rather than a hardcoded policy.
type ACLMode string

const (
	// ACLModeRequire is fail-closed: a page whose permissions cannot be confirmed
	// open is NOT indexed. Against the current API this indexes nothing at all —
	// which is the correct behaviour for anyone who would rather have an empty
	// index than an unvetted one.
	ACLModeRequire ACLMode = "require"

	// ACLModeProbe attempts the check and, on a 404/405 — i.e. "this API has no
	// such endpoint" — stops attempting it, continues the crawl, and records the
	// whole run as ACL-unchecked. A permission error that is NOT an endpoint
	// problem (403, 500, a transport failure) still fails closed for that page:
	// "the endpoint does not exist" and "the endpoint refused me" are different
	// findings and only the first one is safe to shrug off.
	ACLModeProbe ACLMode = "probe"

	// ACLModeOff does not check at all and spends no requests doing it. Honest,
	// and recorded as such in every manifest entry.
	ACLModeOff ACLMode = "off"
)

// DefaultACLMode is "probe".
//
// This default is NOT fail-closed, and that is a deliberate, documented choice
// rather than an oversight. With the live API returning 405 for every ACL
// request, a "require" default means the feature indexes zero pages for
// everyone, and the user discovers this as an empty database with no error to
// explain it — a silent failure, which is the failure mode this package spends
// most of its effort avoiding.
//
// The safety that the default gives up is repaid by controls that cannot be
// missed rather than by a quieter default: every manifest entry is stamped
// acl=unchecked, and a loud multi-line warning is printed on BOTH a dry run and
// a real indexing run. Anyone who wants the fail-closed behaviour back has it
// one flag away: --acl-mode=require.
const DefaultACLMode = ACLModeProbe

// ValidACLModes lists the accepted values, for flag validation and help text.
var ValidACLModes = []ACLMode{ACLModeRequire, ACLModeProbe, ACLModeOff}

// ParseACLMode validates a user-supplied mode.
func ParseACLMode(s string) (ACLMode, error) {
	m := ACLMode(strings.ToLower(strings.TrimSpace(s)))
	if m == "" {
		return DefaultACLMode, nil
	}
	for _, v := range ValidACLModes {
		if m == v {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown --acl-mode %q: use require (fail-closed, indexes nothing while the API has no ACL endpoint), "+
		"probe (try, and continue as unchecked if the endpoint is absent) or off (do not check)", s)
}

// aclUnavailableNote is the manifest/CLI explanation for a run whose permission
// check could not run.
const aclUnavailableNote = "the wiki API has no working page-permission endpoint " +
	"(/access answers 405 Method not allowed), so no page's permissions were verified"

// aclOffNote is the same, for a run that chose not to check.
const aclOffNote = "--acl-mode=off was given, so no page's permissions were verified"

// Config configures the wiki source.
type Config struct {
	Client *Client
	Filter Filter
	Limits Limits

	// Roots are the slugs to crawl recursively.
	Roots []string
	// PageBaseURL is the BROWSER-facing base for page links, e.g.
	// "https://wiki.yandex.ru". It is mandatory and has no default: building
	// links from the API host would produce URLs that land the user on an API
	// endpoint, and a support bot handing out broken links is not something
	// anyone notices quickly.
	PageBaseURL string
	// ACLOpenValues is the fail-closed allow-list of "readable org-wide" markers.
	ACLOpenValues []string
	// ACLMode selects what happens when the ACL endpoint cannot answer. Empty
	// means DefaultACLMode. See the ACLMode constants.
	ACLMode ACLMode
	// PageTypes lists the page_type values worth indexing; empty means
	// DefaultIndexablePageTypes, which is "*" — every type. It is configurable
	// because the API's full set of types is not known.
	PageTypes []string
	// Manifest records every decision. Never nil after New().
	Manifest *Manifest
	// Log receives progress lines; nil discards them.
	Log io.Writer
	// DryRun still fetches and evaluates everything (the manifest needs real
	// sizes and hashes) but the caller skips embedding and storage.
	DryRun bool
}

// Source is the wiki implementation of source.Source.
type Source struct {
	cfg     Config
	walker  *Walker
	fetcher *PageFetcher

	// aclUnavailable latches once the ACL endpoint has proved absent, so the
	// remaining pages of the crawl do not each spend a request rediscovering it.
	aclUnavailable bool
}

// New validates the configuration and builds a Source.
func New(cfg Config) (*Source, error) {
	if cfg.Client == nil {
		return nil, errors.New("wiki: client is required")
	}
	if len(cfg.Roots) == 0 {
		return nil, errors.New("wiki: at least one --root is required")
	}
	if strings.TrimSpace(cfg.PageBaseURL) == "" {
		return nil, errors.New("wiki: --page-base-url is required and has no default — " +
			"it is the browser base for page links (e.g. https://wiki.yandex.ru or your corporate wiki domain). " +
			"It must NOT be the API host: links built from the API host do not open for a human")
	}
	if len(cfg.ACLOpenValues) == 0 {
		cfg.ACLOpenValues = DefaultACLOpenValues
	}
	if len(cfg.PageTypes) == 0 {
		cfg.PageTypes = DefaultIndexablePageTypes
	}
	if cfg.ACLMode == "" {
		cfg.ACLMode = DefaultACLMode
	}
	if _, err := ParseACLMode(string(cfg.ACLMode)); err != nil {
		return nil, err
	}
	if cfg.Manifest == nil {
		// The rules come from the Filter, not from the caller's slices: the
		// Filter has already normalised them, and the normalised form is what
		// actually decided each page. Recording the raw input would put a rule
		// in the manifest that nothing was ever compared against.
		cfg.Manifest = NewManifest(cfg.Roots, cfg.Filter.Allow, cfg.Filter.Deny, cfg.DryRun)
	}
	if cfg.Log == nil {
		cfg.Log = io.Discard
	}

	// An "off" run is unchecked from its very first entry, so the manifest is
	// stamped before any page is seen rather than at the end.
	if cfg.ACLMode == ACLModeOff {
		cfg.Manifest.MarkACLUnchecked(aclOffNote)
	}

	walker := NewWalker(cfg.Client, cfg.Limits)
	walker.Log = cfg.Log

	return &Source{
		cfg:     cfg,
		walker:  walker,
		fetcher: NewPageFetcher(cfg.Client, cfg.Log),
	}, nil
}

// Manifest exposes the run's manifest so the CLI can write it even when the
// crawl aborted halfway — a partial manifest still documents what was reached.
func (s *Source) Manifest() *Manifest { return s.cfg.Manifest }

// Iterate crawls every root and yields the pages that pass filtering, the ACL
// check and the quality gate. Pages are deduplicated across roots by page id.
func (s *Source) Iterate(ctx context.Context, fn func(source.Document) error) error {
	for _, root := range s.cfg.Roots {
		err := s.walker.Descendants(ctx, root, func(ref PageRef) error {
			return s.handle(ctx, ref, fn)
		})
		if err != nil {
			return err
		}
	}

	s.logf("wiki: %d page(s) visited, %d indexed, %d HTTP request(s)\n",
		len(s.cfg.Manifest.Entries), s.cfg.Manifest.Indexed(), s.cfg.Client.Requests())
	if warn := s.cfg.Manifest.ACLWarning(); warn != "" {
		s.logf("%s", warn)
	}
	return nil
}

// handle runs one page through the pipeline. Every exit path writes exactly one
// manifest entry, so the manifest is a complete account of the crawl.
func (s *Source) handle(ctx context.Context, ref PageRef, fn func(source.Document) error) error {
	entry := Entry{PageID: ref.ID, Slug: ref.Slug, Title: ref.Title, Depth: ref.Depth}

	slug := normalizeSlug(ref.Slug)
	if slug == "" {
		// Without a slug there is no way to build a link, and a support answer
		// without a source link is not useful.
		entry.Decision = DecisionSkipped
		entry.Reason = ReasonMissingSlug
		s.cfg.Manifest.Add(entry)
		return nil
	}
	entry.Slug = slug

	// 1. Filter — before any further request, so a denied subtree costs nothing.
	if d := s.cfg.Filter.Decide(slug); !d.Include {
		entry.Decision = DecisionSkipped
		entry.Reason = d.Reason
		if d.Rule != "" {
			entry.Reason += ":" + d.Rule
		}
		s.cfg.Manifest.Add(entry)
		return nil
	}

	// 2. ACL — before the content request, so a restricted page's body never even
	// enters this process's memory.
	verdict, checked, err := s.access(ctx, ref)
	switch {
	case err != nil:
		entry.Decision = DecisionSkipped
		entry.Reason = ReasonACLError
		entry.ACL = redact(err.Error())
		s.cfg.Manifest.Add(entry)
		s.logf("wiki: skip %s: ACL check failed: %s\n", redact(slug), redact(err.Error()))
		return nil

	case !checked:
		// The check did not happen — off, or the endpoint is absent. The page
		// proceeds; the manifest and the run's warning carry the fact.
		entry.ACL = ACLUnchecked

	case !verdict.Open:
		entry.ACL = verdict.Reason
		entry.Decision = DecisionSkipped
		entry.Reason = ReasonACLRestrict
		if strings.HasPrefix(verdict.Reason, "acl_unknown") {
			entry.Reason = ReasonACLUnknown
		}
		s.cfg.Manifest.Add(entry)
		return nil

	default:
		entry.ACL = verdict.Reason
	}

	// 3. Content.
	page, err := s.fetchPage(ctx, ref)
	if err != nil {
		entry.Decision = DecisionError
		entry.Reason = ReasonFetchError
		entry.ACL = redact(entry.ACL)
		s.cfg.Manifest.Add(entry)
		s.logf("wiki: skip %s: %s\n", redact(slug), redact(err.Error()))
		return nil
	}
	if page.Title != "" {
		entry.Title = page.Title
	}
	if page.Slug != "" {
		entry.Slug = normalizeSlug(page.Slug)
		slug = entry.Slug
	}
	entry.Version = firstNonEmpty(page.Version, page.UpdatedAt)
	entry.Breadcrumbs = page.Breadcrumbs

	// 4. Page type — a non-prose page (a table, a grid) is not an answer to a
	// support question, and its serialised form only adds noise to the index.
	// The reason carries the actual value so an unknown type shows up in the
	// manifest as something to widen --page-type with, not as a mystery.
	if !IsIndexablePageType(page.PageType, s.cfg.PageTypes) {
		entry.Decision = DecisionSkipped
		entry.Reason = ReasonPageType + ":" + page.PageType
		s.cfg.Manifest.Add(entry)
		return nil
	}

	// 5. Quality.
	content := NormalizeContent(page.Body)
	if ok, reason := CheckQuality(entry.Title, content); !ok {
		entry.Decision = DecisionSkipped
		entry.Reason = reason
		entry.Bytes = len(content)
		s.cfg.Manifest.Add(entry)
		return nil
	}

	// 6. Document.
	pageURL, err := s.pageURL(page, slug)
	if err != nil {
		entry.Decision = DecisionError
		entry.Reason = ReasonNoPageBase
		s.cfg.Manifest.Add(entry)
		return err // a configuration error, not a per-page problem: stop the run
	}

	id := firstNonEmpty(page.ID, ref.ID, slug)
	entry.PageID = id
	entry.URL = pageURL
	entry.Bytes = len(content)
	entry.ContentHash = contentHash(content)
	entry.Decision = DecisionIndexed
	entry.Reason = ReasonOK
	s.cfg.Manifest.Add(entry)

	if s.cfg.DryRun {
		// The content was fetched (the manifest needs its size and hash) but it is
		// never handed on and never stored.
		return nil
	}

	return fn(source.Document{
		ID:      id, // page id, never the slug: a rename must not duplicate the page
		URL:     pageURL,
		Title:   firstNonEmpty(entry.Title, slug),
		Path:    slug,
		Content: content,
		Format:  "markdown",
		Version: firstNonEmpty(entry.Version, entry.ContentHash),
	})
}

// access applies the configured ACLMode to one page.
//
// checked=false means the permission check did not run — the caller must let the
// page through AND make sure the run is recorded as unchecked. checked=true with
// a non-open verdict means the page was positively determined not to be readable
// org-wide, and it is skipped.
func (s *Source) access(ctx context.Context, ref PageRef) (verdict AccessVerdict, checked bool, err error) {
	if s.cfg.ACLMode == ACLModeOff || s.aclUnavailable {
		return AccessVerdict{}, false, nil
	}

	verdict, err = s.checkAccess(ctx, ref)
	if err == nil {
		return verdict, true, nil
	}

	// Only in probe mode, and only for "there is no such endpoint", is a failed
	// check something to continue past.
	if s.cfg.ACLMode == ACLModeProbe && isEndpointAbsent(err) {
		s.aclUnavailable = true
		s.cfg.Manifest.MarkACLUnchecked(aclUnavailableNote)
		s.logf("wiki: the ACL endpoint answered %s — page permissions CANNOT be verified.\n"+
			"wiki: continuing with --acl-mode=probe; every page is recorded as acl=%s.\n"+
			"wiki: the only remaining barrier is the --allow/--deny slug filter. Use --acl-mode=require to stop instead.\n",
			redact(err.Error()), ACLUnchecked)
		return AccessVerdict{}, false, nil
	}

	return AccessVerdict{}, false, err
}

// isEndpointAbsent reports whether the error says "this API has no such
// endpoint" — 404 Not found or 405 Method not allowed — as opposed to "you may
// not use it" (403) or "it broke" (5xx, transport). Only the first is evidence
// about the API's capabilities; the others are evidence about this request, and
// treating them as "no ACL exists" would turn one refused request into a
// permanently unchecked crawl.
func isEndpointAbsent(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusMethodNotAllowed
}

// checkAccess queries the ACL endpoint. A page whose ACL cannot be determined is
// treated as closed by the caller — see ParseAccess for why this is fail-closed.
func (s *Source) checkAccess(ctx context.Context, ref PageRef) (AccessVerdict, error) {
	if ref.ID == "" {
		return AccessVerdict{}, errors.New("no page id: cannot check access")
	}
	raw, err := s.cfg.Client.GetJSON(ctx, "/v1/pages/"+url.PathEscape(ref.ID)+"/access", nil)
	if err != nil {
		return AccessVerdict{}, err
	}
	return ParseAccess(raw, s.cfg.ACLOpenValues)
}

// fetchPage retrieves one page's content by slug, asking for the parts the API
// does not send by default — see fetch.go.
func (s *Source) fetchPage(ctx context.Context, ref PageRef) (PageContent, error) {
	page, _, err := s.fetcher.Fetch(ctx, ref.Slug, ref.ID)
	return page, err
}

// pageURL builds the browser-facing link. A URL supplied by the API wins: it is
// authoritative and already correct for whatever domain the organization uses.
// Otherwise the configured page base is joined with the slug — and the API host
// is never used as a fallback.
func (s *Source) pageURL(page PageContent, slug string) (string, error) {
	if u := strings.TrimSpace(page.PublicURL); u != "" {
		return u, nil
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg.PageBaseURL), "/")
	if base == "" {
		return "", errors.New("wiki: --page-base-url is empty and the API returned no page URL")
	}
	return base + "/" + strings.TrimLeft(slug, "/"), nil
}

// contentHash identifies a revision of the content, so the indexer can skip a
// page whose text has not changed even when the API's version field is absent
// or unreliable.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (s *Source) logf(format string, args ...any) {
	fmt.Fprintf(s.cfg.Log, format, args...)
}
