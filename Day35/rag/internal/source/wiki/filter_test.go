package wiki

import "testing"

// The single most important property of the filter: forgetting --allow must
// index NOTHING, not everything.
func TestFilterEmptyAllowExcludesEverything(t *testing.T) {
	f := NewFilter(nil, nil)

	for _, slug := range []string{"", "docs", "docs/api", "/anything/at/all", "secret/hr/salaries"} {
		d := f.Decide(slug)
		if d.Include {
			t.Errorf("Decide(%q).Include = true, want false: an empty allow list must exclude everything", slug)
		}
		if d.Reason != "no_allow_rule" {
			t.Errorf("Decide(%q).Reason = %q, want %q", slug, d.Reason, "no_allow_rule")
		}
	}
}

func TestFilterDenyBeatsAllow(t *testing.T) {
	f := NewFilter([]string{"docs"}, []string{"docs/internal"})

	tests := []struct {
		slug       string
		wantInc    bool
		wantReason string
	}{
		{"docs", true, "allow_prefix"},
		{"docs/api", true, "allow_prefix"},
		{"docs/internal", false, "deny_prefix"},
		{"docs/internal/keys", false, "deny_prefix"},
		{"other", false, "not_allowed"},
	}

	for _, tt := range tests {
		d := f.Decide(tt.slug)
		if d.Include != tt.wantInc || d.Reason != tt.wantReason {
			t.Errorf("Decide(%q) = {%t, %q}, want {%t, %q}",
				tt.slug, d.Include, d.Reason, tt.wantInc, tt.wantReason)
		}
	}
}

// A prefix must match on segment boundaries. Allowing "docs" must not drag in a
// sibling section whose name merely starts with the same letters.
func TestFilterPrefixIsSegmentWise(t *testing.T) {
	f := NewFilter([]string{"docs"}, nil)

	if d := f.Decide("docsecret/salaries"); d.Include {
		t.Errorf(`allow "docs" must not match "docsecret/salaries" (got %+v)`, d)
	}
	if d := f.Decide("docs/api"); !d.Include {
		t.Errorf(`allow "docs" must match "docs/api" (got %+v)`, d)
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docs", "docs"},
		{"/docs/", "docs"},
		{"  /Docs/API/  ", "docs/api"},
		{"docs//api", "docs/api"},
		{"docs///api////v2", "docs/api/v2"},
		{"DOCS/API", "docs/api"},
		{"docs\\api", "docs/api"},
		{"./docs", "docs"},
		{"docs/./api", "docs/api"},
		{"", ""},
		{"/", ""},
		{"///", ""},
	}

	for _, tt := range tests {
		if got := normalizeSlug(tt.in); got != tt.want {
			t.Errorf("normalizeSlug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Traversal must be resolved BEFORE the prefix test, otherwise a slug like
// "public/../private" walks straight through an allow rule of "public".
func TestFilterTraversalCannotEscapeAllow(t *testing.T) {
	f := NewFilter([]string{"public"}, nil)

	escapes := []string{
		"public/../private",
		"public/../../private",
		"public/./../private",
		"public//../private",
		"/public/../private/salaries",
		"public/sub/../../private",
	}
	for _, slug := range escapes {
		if d := f.Decide(slug); d.Include {
			t.Errorf("Decide(%q).Include = true — traversal escaped the allow rule (%+v)", slug, d)
		}
	}

	// Traversal that stays inside the allowed subtree is fine.
	stays := []string{
		"public/a/../b",
		"public/./docs",
		"public//docs",
	}
	for _, slug := range stays {
		if d := f.Decide(slug); !d.Include {
			t.Errorf("Decide(%q).Include = false, want true (%+v)", slug, d)
		}
	}
}

// ".." above the root must not produce a slug starting with "..", which would
// otherwise be compared literally against the rules.
func TestNormalizeSlugTraversalAboveRoot(t *testing.T) {
	for _, in := range []string{"../secret", "../../secret", "/../secret", "a/../../secret"} {
		got := normalizeSlug(in)
		if got != "secret" {
			t.Errorf("normalizeSlug(%q) = %q, want %q", in, got, "secret")
		}
	}
}

func TestFilterRulesAreNormalized(t *testing.T) {
	// Rules written sloppily must behave the same as clean ones.
	f := NewFilter([]string{"  /Docs/API/  "}, []string{"/DOCS/api/Internal"})

	if d := f.Decide("docs/api/v1"); !d.Include {
		t.Errorf("sloppy allow rule did not match: %+v", d)
	}
	if d := f.Decide("Docs/API/Internal/x"); d.Include {
		t.Errorf("sloppy deny rule did not match: %+v", d)
	}
}

// TestRootRuleIsNotAWildcard closes the hole that made the whole fail-closed
// design bypassable by one character: a rule normalising to "" is a prefix of
// every slug, so `--allow /` used to crawl an entire corporate wiki silently —
// no warning, and a manifest whose matched rule was the empty string.
//
// "/" is what a person types when they mean "everything", which is precisely why
// it must not work.
func TestRootRuleIsNotAWildcard(t *testing.T) {
	for _, rule := range []string{"/", ".", "//", "./", "  /  ", "/./", "a/.."} {
		t.Run(rule, func(t *testing.T) {
			f := NewFilter([]string{rule}, nil)

			if len(f.Allow) != 0 {
				t.Fatalf("rule %q survived normalisation as %q; it must be dropped", rule, f.Allow)
			}
			// Dropped, therefore no allow rules, therefore fail-closed.
			d := f.Decide("hr/salaries/2026")
			if d.Include {
				t.Errorf("rule %q admitted an unrelated page (reason=%s rule=%q)", rule, d.Reason, d.Rule)
			}
			if d.Reason != "no_allow_rule" {
				t.Errorf("reason = %q, want no_allow_rule so the manifest says why", d.Reason)
			}
		})
	}
}

// TestRootRuleIsReportedAsDropped pins the other half: the rule is not discarded
// in silence, so a caller can refuse the run and name what it refused.
func TestRootRuleIsReportedAsDropped(t *testing.T) {
	kept, dropped := NormalizeRules([]string{"docs", "/", "howto"})

	if len(kept) != 2 || kept[0] != "docs" || kept[1] != "howto" {
		t.Errorf("kept = %q, want the two real rules", kept)
	}
	if len(dropped) != 1 || dropped[0] != "/" {
		t.Errorf("dropped = %q, want the raw root rule so an error can quote it", dropped)
	}
}

// TestDirectlyBuiltFilterCannotWildcard covers the constructor being bypassed.
// Filter is an exported struct with exported fields; a future caller, or a config
// decoded straight into it, must not get a wildcard for free. Fail-closed has to
// hold in the code that actually decides, not only in the constructor.
func TestDirectlyBuiltFilterCannotWildcard(t *testing.T) {
	f := Filter{Allow: []string{""}}

	if d := f.Decide("hr/salaries/2026"); d.Include {
		t.Errorf("an empty prefix matched (reason=%s rule=%q); an empty prefix must never match", d.Reason, d.Rule)
	}
}
