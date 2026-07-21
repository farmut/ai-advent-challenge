package wiki

import (
	"path"
	"strings"
)

// Decision is the filter's answer for one slug.
type Decision struct {
	Include bool
	// Reason names the rule that decided: "no_allow_rule", "deny_prefix",
	// "allow_prefix", "not_allowed". It goes straight into the manifest.
	Reason string
	// Rule is the matching allow/deny prefix, for the manifest.
	Rule string
}

// Filter decides which slugs are indexed. It is FAIL-CLOSED: an empty Allow list
// excludes everything. The alternative — empty means "everything" — turns a
// forgotten flag into a full crawl of a corporate wiki, which is exactly the
// accident this feature must not have.
type Filter struct {
	Allow []string
	Deny  []string
}

// NewFilter normalises the rules once, at construction, so Decide never has to.
// Root-level rules are DROPPED — see NormalizeRules.
func NewFilter(allow, deny []string) Filter {
	a, _ := NormalizeRules(allow)
	d, _ := NormalizeRules(deny)
	return Filter{Allow: a, Deny: d}
}

// NormalizeRules canonicalises a rule list and splits off the rules that mean
// "the root": "/", ".", "//", "" — anything that normalises to an empty slug.
// It returns the usable rules and the raw dropped ones, so a caller can refuse
// the run and name what it refused.
//
// Dropping them is a security decision, not tidiness. A rule that normalises to
// "" is a prefix of every slug, so a single `--allow /` silently turns the
// fail-closed filter into a full crawl of a corporate wiki — the exact accident
// the fail-closed design exists to prevent, reached by the most natural thing a
// person types when they mean "everything". There is deliberately no wildcard:
// to index the whole wiki, name the top-level sections explicitly, and they land
// in the manifest where a reviewer can see them.
// GlobRules returns the rules that look like a glob pattern.
//
// Rules are literal path prefixes; "*" is not special and never has been. Left
// alone, `--allow "*"` becomes a literal rule matching no real slug, so the run
// walks the wiki, keeps nothing, and reports it in the same calm voice as a
// successful crawl — configured-looking and empty, which is the failure mode
// hardest to notice.
//
// They are refused rather than implemented. A glob is precisely the "one
// forgotten flag crawls the entire corporate wiki" risk the fail-closed filter
// exists to prevent, and the safe alternative — naming the sections — costs one
// line and puts them in the manifest where a reviewer sees them.
func GlobRules(in []string) []string {
	var out []string
	for _, raw := range in {
		if strings.ContainsAny(raw, "*?[") {
			out = append(out, raw)
		}
	}
	return out
}

func NormalizeRules(in []string) (kept, dropped []string) {
	for _, raw := range in {
		if s := normalizeSlug(raw); s != "" {
			kept = append(kept, s)
		} else {
			dropped = append(dropped, raw)
		}
	}
	return kept, dropped
}

// Decide evaluates slug against the rules. Order is fixed and deliberate:
//  1. no allow rules at all      -> exclude (fail-closed)
//  2. a deny prefix matches      -> exclude (deny always beats allow, so a
//     narrow exception can be carved out of a broad allow)
//  3. an allow prefix matches    -> include
//  4. otherwise                  -> exclude
func (f Filter) Decide(slug string) Decision {
	if len(f.Allow) == 0 {
		return Decision{Include: false, Reason: "no_allow_rule"}
	}

	s := normalizeSlug(slug)

	if rule, ok := matchPrefix(s, f.Deny); ok {
		return Decision{Include: false, Reason: "deny_prefix", Rule: rule}
	}
	if rule, ok := matchPrefix(s, f.Allow); ok {
		return Decision{Include: true, Reason: "allow_prefix", Rule: rule}
	}
	return Decision{Include: false, Reason: "not_allowed"}
}

// matchPrefix reports whether s is at or below one of the prefixes. Matching is
// SEGMENT-WISE: prefix "docs" matches "docs" and "docs/api" but NOT "docsecret".
// A plain strings.HasPrefix would let an unrelated sibling section in.
//
// An empty prefix NEVER matches. NewFilter already drops those, so this is the
// second lock on the same door: Filter is an exported struct with exported
// fields, and a Filter{Allow: []string{""}} built directly — by a future caller
// or a decoded config — must not become a wildcard just because it skipped the
// constructor. Fail-closed has to hold at the layer that actually decides.
func matchPrefix(s string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if s == p || strings.HasPrefix(s, p+"/") {
			return p, true
		}
	}
	return "", false
}

// normalizeSlug is the ONE place a slug is canonicalised. Every comparison —
// filter rules, dedup keys, URL building — must go through it, otherwise
// "/Docs/" and "docs" become two different pages.
//
// It lowercases, strips leading/trailing slashes, collapses repeated slashes and
// resolves "." / "..". The traversal handling matters: a slug like
// "public/../private" must not slip past an allow rule of "public/", and
// path.Clean is what collapses it to "private" before the prefix test runs.
func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "\\", "/")
	if s == "" {
		return ""
	}

	// path.Clean on an absolute path resolves ".." without leaving a leading
	// "..": "/a/../../b" becomes "/b", so traversal cannot escape the root.
	cleaned := path.Clean("/" + s)
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}
