package gitserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo creates a throw-away git repository with one commit on branch "main".
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "initial")
	return dir
}

func TestNewRejectsNonRepo(t *testing.T) {
	if _, err := New(t.TempDir()); err == nil {
		t.Fatal("expected error for a directory that is not a git repository")
	}
}

func TestCurrentBranch(t *testing.T) {
	srv, err := New(newTestRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := srv.CallTool("git_current_branch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "main" {
		t.Fatalf("branch = %q, want main", out)
	}
}

func TestListFilesChangedAndAll(t *testing.T) {
	dir := newTestRepo(t)
	srv, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Clean tree.
	out, err := srv.CallTool("git_list_files", map[string]interface{}{"filter": "changed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "clean") {
		t.Fatalf("clean tree: got %q", out)
	}

	// Modify + add untracked.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err = srv.CallTool("git_list_files", map[string]interface{}{"filter": "changed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "new.txt") {
		t.Fatalf("changed list missing files: %q", out)
	}

	out, err = srv.CallTool("git_list_files", map[string]interface{}{"filter": "untracked"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "new.txt") || strings.Contains(out, "a.txt") {
		t.Fatalf("untracked list wrong: %q", out)
	}

	out, err = srv.CallTool("git_list_files", map[string]interface{}{"filter": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("all list missing tracked file: %q", out)
	}

	if _, err := srv.CallTool("git_list_files", map[string]interface{}{"filter": "bogus"}); err == nil {
		t.Fatal("expected error for unknown filter")
	}
}

func TestDiffUnstagedStagedAndPath(t *testing.T) {
	dir := newTestRepo(t)
	srv, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// No changes yet.
	out, err := srv.CallTool("git_diff", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no unstaged changes") {
		t.Fatalf("clean diff: got %q", out)
	}

	// Unstaged modification.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = srv.CallTool("git_diff", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+changed") || !strings.Contains(out, "-hello") {
		t.Fatalf("unstaged diff wrong: %q", out)
	}

	// Path filter: diff of an unrelated path is empty.
	out, err = srv.CallTool("git_diff", map[string]interface{}{"path": "other.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no unstaged changes") {
		t.Fatalf("path-filtered diff should be empty: %q", out)
	}

	// Stage it: staged diff shows the change, unstaged is empty again.
	cmd := exec.Command("git", "-C", dir, "add", "a.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	out, err = srv.CallTool("git_diff", map[string]interface{}{"staged": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+changed") {
		t.Fatalf("staged diff wrong: %q", out)
	}
	out, err = srv.CallTool("git_diff", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no unstaged changes") {
		t.Fatalf("unstaged after add should be empty: %q", out)
	}

	// Option-looking path is rejected.
	if _, err := srv.CallTool("git_diff", map[string]interface{}{"path": "--exec=evil"}); err == nil {
		t.Fatal("expected error for option-looking path")
	}
}

func TestUnknownTool(t *testing.T) {
	srv, err := New(newTestRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CallTool("git_push", nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestToolDefinitions(t *testing.T) {
	defs := ToolDefinitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}
	want := map[string]bool{"git_current_branch": true, "git_list_files": true, "git_diff": true}
	for _, d := range defs {
		if !want[d.Name] {
			t.Fatalf("unexpected tool %q", d.Name)
		}
		if d.Description == "" || d.InputSchema == nil {
			t.Fatalf("tool %q missing description or schema", d.Name)
		}
	}
}
