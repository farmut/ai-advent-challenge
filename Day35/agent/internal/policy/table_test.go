package policy

import (
	"sort"
	"strings"
	"testing"
)

// The table is the fail-closed contract: it must cover exactly the tools the
// pinned servers expose — no gaps (a missing tool would be denied at runtime,
// breaking the agent) and no extras (an extra entry would whitelist a tool that
// does not exist, and would silently keep working if a server renamed one).
func TestDefaultSpecs_CoversExactlyTheKnownTools(t *testing.T) {
	specs := DefaultSpecs()

	fsNames := FSToolNames()
	searchNames := SearchToolNames()

	if len(fsNames) != 14 {
		t.Fatalf("server-filesystem@0.2.0 exposes 14 tools, FSToolNames has %d", len(fsNames))
	}
	if len(searchNames) != 3 {
		t.Fatalf("fff-mcp 0.10.0 exposes 3 tools, SearchToolNames has %d", len(searchNames))
	}

	expected := map[string]bool{}
	for _, n := range append(append([]string{}, fsNames...), searchNames...) {
		if expected[n] {
			t.Fatalf("duplicate tool name %q", n)
		}
		expected[n] = true
	}
	if len(expected) != 17 {
		t.Fatalf("expected 17 distinct tool names, got %d", len(expected))
	}

	for name := range expected {
		if _, ok := specs[name]; !ok {
			t.Errorf("tool %q is missing from DefaultSpecs (it would be denied at runtime)", name)
		}
	}
	var extra []string
	for name := range specs {
		if !expected[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("DefaultSpecs has entries for unknown tools: %v", extra)
	}
	if len(specs) != len(expected) {
		t.Errorf("DefaultSpecs has %d entries, want %d", len(specs), len(expected))
	}
}

func TestDefaultSpecs_WriteToolsAreMarkedWrite(t *testing.T) {
	specs := DefaultSpecs()

	writeTools := []string{"write_file", "edit_file", "create_directory", "move_file"}
	for _, name := range writeTools {
		t.Run(name, func(t *testing.T) {
			spec, ok := specs[name]
			if !ok {
				t.Fatalf("%q missing from the table", name)
			}
			if spec.Kind != KindWrite {
				t.Fatalf("%q must be Kind=Write, got %v", name, spec.Kind)
			}
			if len(spec.PathArgs) == 0 && len(spec.ListArgs) == 0 {
				t.Fatalf("%q is a write tool with no path arguments to guard", name)
			}
		})
	}

	// Nothing outside that list may be a write tool.
	isWrite := map[string]bool{}
	for _, n := range writeTools {
		isWrite[n] = true
	}
	for name, spec := range specs {
		if spec.Kind == KindWrite && !isWrite[name] {
			t.Errorf("%q is marked Write but is not in the known write set", name)
		}
	}
}

func TestDefaultSpecs_ReadToolsGuardTheirPaths(t *testing.T) {
	specs := DefaultSpecs()

	readTools := []string{
		"read_file", "read_text_file", "read_media_file", "read_multiple_files",
		"list_directory", "list_directory_with_sizes", "directory_tree",
		"search_files", "get_file_info",
	}
	for _, name := range readTools {
		t.Run(name, func(t *testing.T) {
			spec, ok := specs[name]
			if !ok {
				t.Fatalf("%q missing from the table", name)
			}
			if spec.Kind != KindRead {
				t.Fatalf("%q must be Kind=Read, got %v", name, spec.Kind)
			}
			if len(spec.PathArgs)+len(spec.ListArgs) == 0 {
				t.Fatalf("%q is a read tool with no path arguments to guard", name)
			}
			if !spec.MustExist {
				t.Fatalf("%q reads an existing path, MustExist must be true", name)
			}
		})
	}
}

// read_file is an undocumented legacy alias; forgetting it is the exact mistake
// the fail-closed table exists to prevent, so it gets its own guard.
func TestDefaultSpecs_CoversLegacyReadFileAlias(t *testing.T) {
	spec, ok := DefaultSpecs()["read_file"]
	if !ok {
		t.Fatal("read_file (legacy alias) must be in the table")
	}
	if spec.Kind != KindRead {
		t.Fatalf("read_file must be Kind=Read, got %v", spec.Kind)
	}
}

// The fff-mcp tools take no path argument at all — their scope is fixed when
// the server process starts — so they must stay neutral.
func TestDefaultSpecs_SearchToolsAreNeutral(t *testing.T) {
	specs := DefaultSpecs()
	for _, name := range SearchToolNames() {
		spec := specs[name]
		if spec.Kind != KindNeutral {
			t.Errorf("%q must be Kind=Neutral, got %v", name, spec.Kind)
		}
		if len(spec.PathArgs)+len(spec.ListArgs) != 0 {
			t.Errorf("%q has no path arguments in its schema", name)
		}
	}
	if specs["list_allowed_directories"].Kind != KindNeutral {
		t.Error("list_allowed_directories takes no arguments and must be Neutral")
	}
}

// move_file is the only tool whose two path arguments differ in existence
// requirements; the table must express that declaratively.
func TestDefaultSpecs_MoveFileSplitsExistenceRequirement(t *testing.T) {
	spec := DefaultSpecs()["move_file"]

	if strings.Join(spec.PathArgs, ",") != "source,destination" {
		t.Fatalf("move_file PathArgs = %v", spec.PathArgs)
	}
	if !spec.mustExistFor("source") {
		t.Error("move_file source must exist")
	}
	if spec.mustExistFor("destination") {
		t.Error("move_file destination must NOT be required to exist")
	}
}

func TestDefaultDenyGlobs(t *testing.T) {
	globs := DefaultDenyGlobs()

	required := []string{
		".git/**", ".env*", "**/*.pem", "**/*.key", "id_rsa*", ".ssh/**",
		"agent.config.yaml", "agent.review.yaml", "*.db", "**/node_modules/**",
	}
	have := map[string]bool{}
	for _, g := range globs {
		have[g] = true
	}
	for _, r := range required {
		if !have[r] {
			t.Errorf("default deny list is missing %q", r)
		}
	}
	if len(globs) != len(required) {
		t.Errorf("deny list has %d entries, want %d", len(globs), len(required))
	}
}
