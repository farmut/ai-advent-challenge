package policy

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

// fixture builds the on-disk layout used by every path test.
//
//	<tmp>/root/                       ReadRoot
//	<tmp>/root/workspace/             WriteRoot
//	<tmp>/root/internal/app/toolbelt.go
//	<tmp>/root/.env  (+ .ENV where the filesystem allows a distinct file)
//	<tmp>/root/.git/config
//	<tmp>/root/workspace/link -> <tmp>/outside      (symlink out of the roots)
//	<tmp>/root/evil       -> <tmp>/root-evil        (twin dir sharing the prefix)
//	<tmp>/root-evil/secret.txt
//
// On macOS t.TempDir() lives under /var, itself a symlink to /private/var, so
// the roots are returned already EvalSymlinks-resolved — otherwise every
// containment check would compare a resolved path against an unresolved root.
func fixture(t *testing.T) (readRoot, writeRoot string) {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(tempdir): %v", err)
	}

	root := filepath.Join(base, "root")
	mkdirs := []string{
		filepath.Join(root, "workspace"),
		filepath.Join(root, "internal", "app"),
		filepath.Join(root, ".git"),
		filepath.Join(base, "outside"),
		filepath.Join(base, "root-evil"),
	}
	for _, d := range mkdirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(root, "internal", "app", "toolbelt.go"): "package app\n",
		filepath.Join(root, ".env"):                           "SECRET=1\n",
		filepath.Join(root, ".ENV"):                           "SECRET=1\n",
		filepath.Join(root, ".git", "config"):                 "[core]\n",
		filepath.Join(root, "workspace", "a"):                 "a\n",
		filepath.Join(base, "root-evil", "secret.txt"):        "pwned\n",
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	links := map[string]string{
		filepath.Join(root, "workspace", "link"): filepath.Join(base, "outside"),
		filepath.Join(root, "evil"):              filepath.Join(base, "root-evil"),
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink %s -> %s: %v", link, target, err)
		}
	}

	return root, filepath.Join(root, "workspace")
}

// guard returns a fully configured guard over the fixture.
func guard(t *testing.T, readRoot, writeRoot string) FS {
	t.Helper()
	return FS{
		ReadRoot:  readRoot,
		WriteRoot: writeRoot,
		Deny:      DefaultDenyGlobs(),
		Specs:     DefaultSpecs(),
		FSServers: []string{"fs"},
		Role:      "coder",
		Audit:     io.Discard,
	}
}

// Regression: the git tools report paths from the repository root, so they arrive
// prefixed with our own root's name ("Day34/workspace/bot.py"). Taken literally
// that resolves to Day34/Day34/... and can never succeed, which left a sub-agent
// retrying the same doomed call until its round budget ran out.
func TestCheck_StripsRedundantRootPrefix(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	t.Run("read path prefixed with the root name resolves", func(t *testing.T) {
		args := map[string]interface{}{"path": "root/internal/app/toolbelt.go"}
		if err := p.Check("fs", "read_text_file", args); err != nil {
			t.Fatalf("prefixed read path must be accepted, got: %v", err)
		}
		got, _ := args["path"].(string)
		want := filepath.Join(readRoot, "internal", "app", "toolbelt.go")
		if got != want {
			t.Fatalf("path must be normalised to %q, got %q", want, got)
		}
	})

	t.Run("write path prefixed with the root name resolves", func(t *testing.T) {
		args := map[string]interface{}{"path": "root/workspace/new.md", "content": "x"}
		if err := p.Check("fs", "write_file", args); err != nil {
			t.Fatalf("prefixed write path must be accepted, got: %v", err)
		}
	})

	// The alias is a convenience, not a hole: it re-runs the same validation, so a
	// prefixed path pointing outside the write root is still refused.
	t.Run("prefix does not grant escape", func(t *testing.T) {
		args := map[string]interface{}{"path": "root/internal/app/toolbelt.go", "content": "x"}
		err := p.Check("fs", "write_file", args)
		if err == nil {
			t.Fatal("prefixed path outside the write root must still be denied")
		}
		if CodeOf(err) != CodeReadOnlyPath {
			t.Fatalf("want %s, got %v", CodeReadOnlyPath, err)
		}
	})

	t.Run("prefix does not bypass deny globs", func(t *testing.T) {
		args := map[string]interface{}{"path": "root/.env"}
		err := p.Check("fs", "read_text_file", args)
		if err == nil {
			t.Fatal("prefixed path to a denied file must stay denied")
		}
		if CodeOf(err) != CodeDeniedByPolicy {
			t.Fatalf("want %s, got %v", CodeDeniedByPolicy, err)
		}
	})
}

// The message is the model's only feedback channel, and a bare "does not exist"
// left it guessing which root paths are counted from.
func TestCheck_NotFoundMessageNamesTheRoot(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	err := p.Check("fs", "read_text_file", map[string]interface{}{"path": "nope/missing.go"})
	if err == nil {
		t.Fatal("missing file must be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, filepath.Base(readRoot)+"/") {
		t.Errorf("message must name the root %q, got: %s", filepath.Base(readRoot), msg)
	}
	if !strings.Contains(msg, "git") {
		t.Errorf("message must warn that git paths use a different root, got: %s", msg)
	}
}

func TestCheck(t *testing.T) {
	readRoot, writeRoot := fixture(t)

	cases := []struct {
		name     string
		noWrite  bool // build the guard with an empty WriteRoot
		server   string
		tool     string
		args     map[string]interface{}
		wantCode string // "" = ALLOW
	}{
		{
			name: "1_write_parent_traversal_denied",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "../../etc/passwd", "content": "x"},
			wantCode: CodeReadOnlyPath,
		},
		{
			name: "2_write_absolute_path_not_rebased",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "/etc/passwd", "content": "x"},
			wantCode: CodeBadArgument,
		},
		{
			name: "3_write_through_symlink_out_of_root",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "workspace/link/x.txt", "content": "x"},
			wantCode: CodeReadOnlyPath,
		},
		{
			// The twin directory <tmp>/root-evil shares the textual prefix of
			// <tmp>/root, so strings.HasPrefix(abs, root) would let this pass.
			// filepath.Rel does not.
			name: "4_prefix_twin_directory_denied",
			tool: "read_text_file", server: "fs",
			args:     map[string]interface{}{"path": "evil/secret.txt"},
			wantCode: CodePermissionDenied,
		},
		{
			name: "5_deny_glob_is_case_insensitive",
			tool: "read_text_file", server: "fs",
			args:     map[string]interface{}{"path": ".ENV"},
			wantCode: CodeDeniedByPolicy,
		},
		{
			name: "6a_empty_path_is_bad_argument",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "", "content": "x"},
			wantCode: CodeBadArgument,
		},
		{
			name: "6b_nil_path_is_bad_argument",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": nil, "content": "x"},
			wantCode: CodeBadArgument,
		},
		{
			name: "6c_missing_path_is_bad_argument",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"content": "x"},
			wantCode: CodeBadArgument,
		},
		{
			name: "7_git_config_denied_by_glob",
			tool: "read_text_file", server: "fs",
			args:     map[string]interface{}{"path": ".git/config"},
			wantCode: CodeDeniedByPolicy,
		},
		{
			name: "8_write_new_file_resolves_parent",
			tool: "write_file", server: "fs",
			args: map[string]interface{}{"path": "workspace/new.txt", "content": "x"},
		},
		{
			name: "9_write_missing_parent_dir",
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "workspace/nodir/new.txt", "content": "x"},
			wantCode: CodeBadArgument,
		},
		{
			name: "10_edit_outside_write_root_is_read_only",
			tool: "edit_file", server: "fs",
			args:     map[string]interface{}{"path": "internal/app/toolbelt.go", "edits": []interface{}{}},
			wantCode: CodeReadOnlyPath,
		},
		{
			name: "11_read_outside_write_root_allowed",
			tool: "read_text_file", server: "fs",
			args: map[string]interface{}{"path": "internal/app/toolbelt.go"},
		},
		{
			name: "12_move_file_destination_escapes",
			tool: "move_file", server: "fs",
			args:     map[string]interface{}{"source": "workspace/a", "destination": "../b"},
			wantCode: CodeReadOnlyPath,
		},
		{
			name: "13_read_multiple_files_one_bad_path_fails_call",
			tool: "read_multiple_files", server: "fs",
			args: map[string]interface{}{"paths": []interface{}{
				"internal/app/toolbelt.go", "../../x",
			}},
			wantCode: CodePermissionDenied,
		},
		{
			name: "14_read_multiple_files_numbers_are_bad_argument",
			tool: "read_multiple_files", server: "fs",
			args:     map[string]interface{}{"paths": []interface{}{float64(1), float64(2)}},
			wantCode: CodeBadArgument,
		},
		{
			name: "15_write_without_write_root_denied", noWrite: true,
			tool: "write_file", server: "fs",
			args:     map[string]interface{}{"path": "workspace/new.txt", "content": "x"},
			wantCode: CodePermissionDenied,
		},
		{
			name: "16_non_fs_server_tool_allowed",
			tool: "git_diff", server: "git",
			args: map[string]interface{}{},
		},
		{
			name: "17_clean_normalises_dot_segments",
			tool: "write_file", server: "fs",
			args: map[string]interface{}{"path": "workspace/./sub/../ok.txt", "content": "x"},
		},
		{
			name: "18_unknown_tool_from_fs_server_fails_closed",
			tool: "delete_everything", server: "fs",
			args:     map[string]interface{}{"path": "workspace/a"},
			wantCode: CodeDeniedByPolicy,
		},
		{
			name: "19_neutral_search_tool_allowed",
			tool: "grep", server: "search",
			args: map[string]interface{}{"query": "x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wr := writeRoot
			if tc.noWrite {
				wr = ""
			}
			p := guard(t, readRoot, wr)

			err := p.Check(tc.server, tc.tool, tc.args)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("expected ALLOW, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected DENY %s, got ALLOW", tc.wantCode)
			}
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("expected code %s, got %s (%v)", tc.wantCode, got, err)
			}
		})
	}
}

// Case 9 must actively steer the model, so assert the hint, not just the code.
func TestCheck_MissingParentSuggestsCreateDirectory(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	err := p.Check("fs", "write_file", map[string]interface{}{"path": "workspace/nodir/new.txt", "content": "x"})
	if err == nil {
		t.Fatal("expected denial")
	}
	if !strings.Contains(err.Error(), "create_directory") {
		t.Fatalf("expected a create_directory hint, got %q", err.Error())
	}
}

// Case 12 must be rejected because of the destination, not the source.
func TestCheck_MoveFileSourceMustExistDestinationMustNot(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	// destination "b" does not exist yet — that is the point of a move.
	if err := p.Check("fs", "move_file", map[string]interface{}{
		"source": "workspace/a", "destination": "workspace/b",
	}); err != nil {
		t.Fatalf("expected ALLOW for existing source + new destination, got %v", err)
	}

	// A source that does not exist is rejected.
	err := p.Check("fs", "move_file", map[string]interface{}{
		"source": "workspace/nope", "destination": "workspace/b",
	})
	if CodeOf(err) != CodeBadArgument {
		t.Fatalf("expected BAD_ARGUMENT for a missing source, got %v", err)
	}
}

// Every denial message must start with one of exactly four codes, because the
// message is handed to the LLM as a tool result and drives its next step.
func TestErrorMessagesUseTheFourCodes(t *testing.T) {
	readRoot, writeRoot := fixture(t)

	denials := []struct {
		name    string
		noWrite bool
		server  string
		tool    string
		args    map[string]interface{}
	}{
		{name: "traversal", server: "fs", tool: "write_file", args: map[string]interface{}{"path": "../x"}},
		{name: "absolute", server: "fs", tool: "write_file", args: map[string]interface{}{"path": "/etc/passwd"}},
		{name: "read_only", server: "fs", tool: "edit_file", args: map[string]interface{}{"path": "internal/app/toolbelt.go"}},
		{name: "deny_glob", server: "fs", tool: "read_text_file", args: map[string]interface{}{"path": ".git/config"}},
		{name: "bad_argument", server: "fs", tool: "write_file", args: map[string]interface{}{}},
		{name: "unknown_tool", server: "fs", tool: "rm_rf", args: map[string]interface{}{}},
		{name: "no_write_root", noWrite: true, server: "fs", tool: "write_file", args: map[string]interface{}{"path": "workspace/x"}},
	}

	valid := map[string]bool{}
	for _, c := range Codes() {
		valid[c] = true
	}
	if len(valid) != 4 {
		t.Fatalf("expected exactly 4 codes, got %d", len(valid))
	}

	for _, d := range denials {
		t.Run(d.name, func(t *testing.T) {
			wr := writeRoot
			if d.noWrite {
				wr = ""
			}
			p := guard(t, readRoot, wr)

			err := p.Check(d.server, d.tool, d.args)
			if err == nil {
				t.Fatal("expected denial")
			}
			msg := err.Error()
			idx := strings.Index(msg, ": ")
			if idx < 0 {
				t.Fatalf("message has no %q separator: %q", ": ", msg)
			}
			code := msg[:idx]
			if !valid[code] {
				t.Fatalf("message starts with unknown code %q: %q", code, msg)
			}
			// The message must also explain what to do next, not just refuse.
			if len(strings.TrimSpace(msg[idx+2:])) < 20 {
				t.Fatalf("message carries no guidance for the model: %q", msg)
			}
		})
	}
}

// The deny globs are checked directly, so the case-insensitivity rule holds
// even on filesystems where ".env" and ".ENV" cannot coexist as distinct files.
func TestDenied(t *testing.T) {
	p := FS{Deny: DefaultDenyGlobs()}

	denied := []string{
		".env", ".ENV", ".env.local", ".Env.Production",
		".git/config", ".git", ".git/hooks/pre-commit",
		"certs/server.pem", "deep/nested/private.key",
		"id_rsa", "id_rsa.pub",
		".ssh/known_hosts",
		"agent.config.yaml", "agent.review.yaml",
		"rag/docs.db", "docs.db",
		"web/node_modules/react/index.js",
	}
	for _, rel := range denied {
		t.Run("deny_"+rel, func(t *testing.T) {
			if _, hit := p.denied(rel); !hit {
				t.Fatalf("%q must be denied", rel)
			}
		})
	}

	allowed := []string{
		"internal/app/toolbelt.go",
		"workspace/report.md",
		"README.md",
		"environment.go",       // must not be caught by ".env*"
		"litellm-ca.crt",       // a public certificate, not a key
		"pkg/keys/registry.go", // directory named keys, not a *.key file
		"gitignore.md",
	}
	for _, rel := range allowed {
		t.Run("allow_"+rel, func(t *testing.T) {
			if pattern, hit := p.denied(rel); hit {
				t.Fatalf("%q must be allowed, matched %q", rel, pattern)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{".git/**", ".git", true},
		{".git/**", ".git/config", true},
		{".git/**", "internal/.git/config", false},
		{"**/*.pem", "a/b/c.pem", true},
		{"**/*.pem", "c.pem", true},
		{"**/*.pem", "c.pem.txt", false},
		{"**/node_modules/**", "a/node_modules/b/c.js", true},
		{"**/node_modules/**", "node_modules/b.js", true},
		{"**/node_modules/**", "a/nodemodules/b.js", false},
		{"*.db", "rag/docs.db", true},
		{"*.db", "docs.db", true},
		{"agent.config.yaml", "agent.config.yaml", true},
		{"agent.config.yaml", "sub/agent.config.yaml", true},
		{"agent.config.yaml", "agent.review.yaml", false},
		{".env*", ".env.local", true},
		{".env*", "sub/.env", true},
		{"", "a", false},
		{"a", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"_vs_"+tc.rel, func(t *testing.T) {
			if got := matchGlob(tc.pattern, tc.rel); got != tc.want {
				t.Fatalf("matchGlob(%q,%q) = %v, want %v", tc.pattern, tc.rel, got, tc.want)
			}
		})
	}
}

func TestResolveUnder_DoesNotUsePrefixComparison(t *testing.T) {
	readRoot, _ := fixture(t)

	// <tmp>/root/evil is a symlink to <tmp>/root-evil: the resolved path starts
	// with the root string but is not inside the root.
	_, _, err := resolveUnder(readRoot, readRoot, "evil/secret.txt", true, KindRead, "workspace/")
	if err == nil {
		t.Fatal("prefix-twin directory must be rejected")
	}

	// Sanity: a prefix check really would have accepted it.
	real, symErr := filepath.EvalSymlinks(filepath.Join(readRoot, "evil", "secret.txt"))
	if symErr != nil {
		t.Fatalf("EvalSymlinks: %v", symErr)
	}
	if !strings.HasPrefix(real, readRoot) {
		t.Skipf("fixture no longer exercises the prefix trap (%s vs %s)", real, readRoot)
	}
}

func TestResolveUnder_ReturnsSlashRelativePath(t *testing.T) {
	readRoot, _ := fixture(t)

	abs, rel, err := resolveUnder(readRoot, readRoot, "internal/app/toolbelt.go", true, KindRead, "workspace/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "internal/app/toolbelt.go" {
		t.Fatalf("rel = %q", rel)
	}
	want := filepath.Join(readRoot, "internal", "app", "toolbelt.go")
	if abs != want {
		t.Fatalf("abs = %q, want %q", abs, want)
	}
}

func TestAllowsTool(t *testing.T) {
	readRoot, writeRoot := fixture(t)

	full := guard(t, readRoot, writeRoot)
	readOnly := guard(t, readRoot, "")

	if !full.AllowsTool("write_file") {
		t.Fatal("write_file must be allowed when WriteRoot is set")
	}
	if readOnly.AllowsTool("write_file") {
		t.Fatal("write_file must be hidden when WriteRoot is empty")
	}
	if !readOnly.AllowsTool("read_text_file") {
		t.Fatal("read_text_file must stay allowed without a WriteRoot")
	}
	if !readOnly.AllowsTool("git_diff") {
		t.Fatal("unknown (neutral) tools must be allowed")
	}
	if (FS{}).AllowsTool("read_text_file") != true {
		t.Fatal("the zero guard must treat everything as neutral")
	}
}

func TestFilter(t *testing.T) {
	readRoot, _ := fixture(t)
	p := guard(t, readRoot, "")

	tools := []domain.MCPTool{
		{Name: "read_text_file"},
		{Name: "write_file"},
		{Name: "edit_file"},
		{Name: "git_diff"},
		{Name: "grep"},
	}

	got := p.Filter(tools)
	var names []string
	for _, tl := range got {
		names = append(names, tl.Name)
	}
	want := []string{"read_text_file", "git_diff", "grep"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("Filter = %v, want %v", names, want)
	}
}

func TestFilterServer_DropsUnknownFSTools(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	tools := []domain.MCPTool{{Name: "read_text_file"}, {Name: "delete_everything"}}

	fsTools := p.FilterServer("fs", tools)
	if len(fsTools) != 1 || fsTools[0].Name != "read_text_file" {
		t.Fatalf("unknown fs tool must be dropped, got %v", fsTools)
	}

	// The same names coming from a non-file server stay untouched.
	gitTools := p.FilterServer("git", tools)
	if len(gitTools) != 2 {
		t.Fatalf("non-fs server tools must pass through, got %v", gitTools)
	}
}

func TestZeroGuardDoesNotPanic(t *testing.T) {
	var p FS
	if err := p.Check("fs", "write_file", map[string]interface{}{"path": "x"}); err != nil {
		t.Fatalf("zero guard: unknown server means neutral, got %v", err)
	}
	if got := p.Filter(nil); got != nil {
		t.Fatalf("Filter(nil) = %v", got)
	}
	if got := p.FilterServer("fs", nil); got != nil {
		t.Fatalf("FilterServer(nil) = %v", got)
	}
}

func TestAudit(t *testing.T) {
	readRoot, writeRoot := fixture(t)

	t.Run("denial_is_logged", func(t *testing.T) {
		var buf bytes.Buffer
		p := guard(t, readRoot, writeRoot)
		p.Audit = &buf

		_ = p.Check("fs", "edit_file", map[string]interface{}{"path": "internal/app/toolbelt.go"})

		line := buf.String()
		for _, want := range []string{"[fsguard]", "role=coder", "tool=edit_file", "verdict=DENY", "code=" + CodeReadOnlyPath} {
			if !strings.Contains(line, want) {
				t.Fatalf("audit line %q missing %q", line, want)
			}
		}
	})

	t.Run("write_allow_is_logged", func(t *testing.T) {
		var buf bytes.Buffer
		p := guard(t, readRoot, writeRoot)
		p.Audit = &buf

		if err := p.Check("fs", "write_file", map[string]interface{}{"path": "workspace/new.txt"}); err != nil {
			t.Fatalf("unexpected denial: %v", err)
		}
		if !strings.Contains(buf.String(), "verdict=ALLOW") {
			t.Fatalf("write must be audited, got %q", buf.String())
		}
	})

	t.Run("read_allow_is_not_logged", func(t *testing.T) {
		var buf bytes.Buffer
		p := guard(t, readRoot, writeRoot)
		p.Audit = &buf

		if err := p.Check("fs", "read_text_file", map[string]interface{}{"path": "internal/app/toolbelt.go"}); err != nil {
			t.Fatalf("unexpected denial: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("allowed reads must stay out of the audit log, got %q", buf.String())
		}
	})
}
