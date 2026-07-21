package wiki

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// Decision values recorded in the manifest.
const (
	DecisionIndexed = "indexed"
	DecisionSkipped = "skipped"
	DecisionError   = "error"
)

// Reasons produced by the pipeline itself (filter and quality reasons live in
// filter.go / quality.go).
const (
	ReasonDuplicate    = "duplicate"
	ReasonACLRestrict  = "acl_restricted"
	ReasonACLError     = "acl_error"
	ReasonACLUnknown   = "acl_unknown"
	ReasonFetchError   = "fetch_error"
	ReasonParseError   = "parse_error"
	ReasonOK           = "ok"
	ReasonMissingSlug  = "missing_slug"
	ReasonNoPageBase   = "no_page_base_url"
	ReasonContentEmpty = "content_empty"
	// ReasonPageType is always written as "page_type:<value>": the value is the
	// actionable part — it tells a reviewer exactly what to add to --page-type
	// if that kind of page should have been indexed after all.
	ReasonPageType = "page_type"
)

// ACLUnchecked is the ACL column's value when the page's permissions were NOT
// verified — either because --acl-mode=off, or because the API has no working
// endpoint to verify them with. It is a single fixed token so that grepping a
// manifest for it is reliable.
const ACLUnchecked = "unchecked"

// Entry is one line of the manifest: the record of what happened to one page and
// why. The manifest is the answer to "what does the bot actually know?", so it
// is written on every run, dry or not.
type Entry struct {
	PageID      string `json:"page_id"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Breadcrumbs string `json:"breadcrumbs,omitempty"`
	Depth       int    `json:"depth"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	ACL         string `json:"acl"`
	Bytes       int    `json:"bytes"`
	ContentHash string `json:"content_hash"`
	Version     string `json:"version"`
}

// redacted returns a copy with every text field masked. Manifests get committed,
// pasted into tickets and attached to emails, so nothing that could carry a
// secret leaves this package unmasked.
func (e Entry) redacted() Entry {
	e.PageID = redact(e.PageID)
	e.Slug = redact(e.Slug)
	e.URL = redact(e.URL)
	e.Title = redact(e.Title)
	e.Breadcrumbs = redact(e.Breadcrumbs)
	e.Decision = redact(e.Decision)
	e.Reason = redact(e.Reason)
	e.ACL = redact(e.ACL)
	e.ContentHash = redact(e.ContentHash)
	e.Version = redact(e.Version)
	return e
}

// Manifest collects the entries of one crawl.
type Manifest struct {
	Roots []string `json:"roots"`
	// Allow and Deny are the filter rules THIS run used, recorded verbatim.
	//
	// They are not decoration. The manifest's job is to answer "why does the bot
	// know this and not that", and the commonest answer is a rule that did not
	// match what the API actually returned. Without the rules on record, a run
	// that excluded every page says only "not_allowed" — a verdict with no
	// visible defendant, leaving the operator to guess at their own shell
	// history. Recording them also makes two manifests diffable, which is how a
	// reviewer notices that the real run was not filtered like the dry run.
	Allow []string `json:"allow"`
	Deny  []string `json:"deny,omitempty"`

	StartedAt time.Time `json:"started_at"`
	DryRun    bool      `json:"dry_run"`
	// ACLUnchecked records that page permissions were NOT verified during this
	// run. It is a property of the whole manifest, not of one entry, because the
	// cause is always global: the mode was off, or the API has no endpoint.
	ACLUnchecked bool `json:"acl_unchecked"`
	// ACLNote explains why, in one sentence a human can act on.
	ACLNote string  `json:"acl_note,omitempty"`
	Entries []Entry `json:"entries"`
}

// NewManifest starts a manifest for the given roots and filter rules.
func NewManifest(roots, allow, deny []string, dryRun bool) *Manifest {
	return &Manifest{
		Roots:     roots,
		Allow:     allow,
		Deny:      deny,
		StartedAt: time.Now().UTC(),
		DryRun:    dryRun,
	}
}

// SampleSlugs returns up to n slugs that were visited, for diagnostics; n <= 0
// returns all of them. They are the ground truth a rule has to match, so showing
// them next to the rules turns "nothing matched" from a riddle into a comparison.
func (m *Manifest) SampleSlugs(n int) []string {
	var out []string
	for _, e := range m.Entries {
		if e.Slug == "" {
			continue
		}
		out = append(out, e.Slug)
		if n > 0 && len(out) == n {
			break
		}
	}
	return out
}

// Add appends one decision. Once the run is known to be ACL-unchecked, EVERY
// entry carries that fact — including the ones recorded before the discovery was
// made, which MarkACLUnchecked backfills. A manifest where only some rows say
// "unchecked" would read as "the rest were checked", and that reading is exactly
// the one that gets a restricted page published.
func (m *Manifest) Add(e Entry) {
	if m.ACLUnchecked {
		e.ACL = ACLUnchecked
	}
	m.Entries = append(m.Entries, e.redacted())
}

// MarkACLUnchecked records that permissions could not be — or were not — checked
// and stamps every entry, past and future, with ACLUnchecked.
func (m *Manifest) MarkACLUnchecked(note string) {
	m.ACLUnchecked = true
	if note != "" {
		m.ACLNote = note
	}
	for i := range m.Entries {
		m.Entries[i].ACL = ACLUnchecked
	}
}

// ACLWarning returns the block of text that must be shown whenever permissions
// were not verified — empty when they were. It is deliberately returned as one
// prominent, multi-line string: this is the compensating control for a run that
// has no permission check at all, so it may not be reduced to a quiet line in a
// summary table.
func (m *Manifest) ACLWarning() string {
	if !m.ACLUnchecked {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n!!! WARNING — PAGE PERMISSIONS WERE NOT CHECKED !!!\n")
	if m.ACLNote != "" {
		b.WriteString("  " + m.ACLNote + "\n")
	}
	b.WriteString("  Every entry in this manifest is recorded as acl=" + ACLUnchecked + ".\n")
	b.WriteString("  The ONLY barriers between a restricted page and the index are:\n")
	b.WriteString("    1. the --allow / --deny slug rules, and\n")
	b.WriteString("    2. a human reading this manifest before the content is published.\n")
	b.WriteString("  Review the slug list below and confirm every one of those pages may be\n")
	b.WriteString("  quoted to whoever can query this index.\n")
	return b.String()
}

// Summary counts entries by "decision/reason", for the run's closing line.
func (m *Manifest) Summary() map[string]int {
	out := make(map[string]int, len(m.Entries))
	for _, e := range m.Entries {
		key := e.Decision
		if e.Reason != "" && e.Reason != ReasonOK {
			key += "/" + e.Reason
		}
		out[key]++
	}
	return out
}

// Indexed counts the entries that made it into the index.
func (m *Manifest) Indexed() int {
	var n int
	for _, e := range m.Entries {
		if e.Decision == DecisionIndexed {
			n++
		}
	}
	return n
}

// WriteJSON writes the manifest as JSON with 0600 permissions — it lists the
// full structure of an internal wiki, which is not world-readable material.
func (m *Manifest) WriteJSON(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: encode: %w", err)
	}
	return writeFile0600(path, append(data, '\n'))
}

// WriteTable renders a human-reviewable table. This is what a person reads
// before approving a real indexing run, so the columns are ordered by what a
// reviewer checks first: what was decided, why, and what the ACL said.
func (m *Manifest) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DECISION\tREASON\tACL\tDEPTH\tBYTES\tSLUG\tTITLE\tURL")
	for _, e := range m.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\n",
			e.Decision, e.Reason, truncate(e.ACL, 28), e.Depth, e.Bytes,
			truncate(e.Slug, 48), truncate(e.Title, 40), truncate(e.URL, 60))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nTotal: %d page(s), indexed: %d\n", len(m.Entries), m.Indexed())
	sum := m.Summary()
	keys := make([]string, 0, len(sum))
	for k := range sum {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %-40s %d\n", k, sum[k])
	}
	if warn := m.ACLWarning(); warn != "" {
		fmt.Fprint(w, warn)
	}
	if m.DryRun {
		fmt.Fprintln(w, "\nDRY RUN — nothing was written to the database.")
	}
	return nil
}

// WriteTableFile writes the table to a 0600 file.
func (m *Manifest) WriteTableFile(path string) error {
	var b strings.Builder
	if err := m.WriteTable(&b); err != nil {
		return err
	}
	return writeFile0600(path, []byte(b.String()))
}

// writeFile0600 writes atomically-ish with owner-only permissions. O_TRUNC on an
// existing file keeps whatever mode it already had, so the mode is set
// explicitly afterwards rather than trusted to the open flags.
func writeFile0600(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("manifest: create dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("manifest: open %s: %w", path, err)
	}
	defer f.Close()

	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("manifest: chmod %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("manifest: write %s: %w", path, err)
	}
	return f.Close()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
