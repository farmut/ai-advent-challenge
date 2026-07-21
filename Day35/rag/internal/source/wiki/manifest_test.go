package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A manifest maps the structure of an internal wiki. It must not be
// world-readable.
func TestManifestFilesAre0600(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest([]string{"docs"}, []string{"docs"}, nil, false)
	m.Add(Entry{PageID: "1", Slug: "docs/a", Decision: DecisionIndexed, Reason: ReasonOK})

	jsonPath := filepath.Join(dir, "manifest.json")
	tablePath := filepath.Join(dir, "manifest.txt")

	if err := m.WriteJSON(jsonPath); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if err := m.WriteTableFile(tablePath); err != nil {
		t.Fatalf("WriteTableFile: %v", err)
	}

	for _, p := range []string{jsonPath, tablePath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s has mode %o, want 600", p, perm)
		}
	}
}

// Overwriting an existing, loosely-permissioned file must tighten it: O_TRUNC
// alone keeps whatever mode the file already had.
func TestManifestOverwriteTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	m := NewManifest([]string{"docs"}, []string{"docs"}, nil, false)
	if err := m.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("overwritten manifest has mode %o, want 600", perm)
	}
}

// Manifests get committed and pasted into tickets. Nothing in one may carry a
// secret.
func TestManifestRedactsEveryField(t *testing.T) {
	resetSecrets()
	t.Cleanup(resetSecrets)
	RegisterSecret(canary)

	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := NewManifest([]string{"docs"}, []string{"docs"}, nil, false)
	m.Add(Entry{
		PageID:      canary,
		Slug:        "docs/" + canary,
		URL:         "https://wiki.example.com/x?access_token=" + canary,
		Title:       "Title " + canary,
		Decision:    DecisionIndexed,
		Reason:      ReasonOK,
		ACL:         "acl_open:access=organization " + canary,
		ContentHash: "abc",
		Version:     canary,
	})

	if err := m.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), canary) {
		t.Errorf("manifest JSON leaked the token:\n%s", raw)
	}

	var table strings.Builder
	if err := m.WriteTable(&table); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	if strings.Contains(table.String(), canary) {
		t.Errorf("manifest table leaked the token:\n%s", table.String())
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")

	m := NewManifest([]string{"docs", "wiki"}, []string{"docs", "wiki"}, nil, true)
	m.Add(Entry{PageID: "1", Slug: "docs/a", Decision: DecisionIndexed, Reason: ReasonOK, Bytes: 100})
	m.Add(Entry{PageID: "2", Slug: "docs/b", Decision: DecisionSkipped, Reason: ReasonACLRestrict})

	if err := m.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var back Manifest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(back.Entries))
	}
	if !back.DryRun {
		t.Error("DryRun flag was lost")
	}
	if back.Indexed() != 1 {
		t.Errorf("Indexed() = %d, want 1", back.Indexed())
	}
}

func TestManifestSummaryGroupsByDecisionAndReason(t *testing.T) {
	m := NewManifest([]string{"docs"}, []string{"docs"}, nil, false)
	m.Add(Entry{Decision: DecisionIndexed, Reason: ReasonOK})
	m.Add(Entry{Decision: DecisionIndexed, Reason: ReasonOK})
	m.Add(Entry{Decision: DecisionSkipped, Reason: ReasonACLRestrict})
	m.Add(Entry{Decision: DecisionSkipped, Reason: ReasonEmpty})

	sum := m.Summary()
	if sum[DecisionIndexed] != 2 {
		t.Errorf("indexed count = %d, want 2", sum[DecisionIndexed])
	}
	if sum[DecisionSkipped+"/"+ReasonACLRestrict] != 1 {
		t.Errorf("acl_restricted count = %d, want 1", sum[DecisionSkipped+"/"+ReasonACLRestrict])
	}
}

func TestManifestTableMarksDryRun(t *testing.T) {
	m := NewManifest([]string{"docs"}, []string{"docs"}, nil, true)
	m.Add(Entry{Slug: "docs/a", Decision: DecisionIndexed, Reason: ReasonOK})

	var b strings.Builder
	if err := m.WriteTable(&b); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	if !strings.Contains(b.String(), "DRY RUN") {
		t.Errorf("dry-run table does not say so:\n%s", b.String())
	}
}

func TestManifestCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeper")
	path := filepath.Join(dir, "m.json")

	if err := NewManifest(nil, nil, nil, false).WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON into a missing directory: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest was not created: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("created directory has mode %o, want 700", perm)
	}
}
