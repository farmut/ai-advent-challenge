// Package gitserver exposes read-only local git inspection as MCP tools.
//
// Every tool shells out to the git CLI via exec.Command (argv, no shell), so
// arguments can never be interpreted by a shell. The repository directory is
// fixed at startup (-repo flag) and validated to be inside a git work tree;
// tool arguments cannot change it. Only read-only git commands are exposed —
// the server can never mutate the repository.
package gitserver

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// maxOutputBytes caps tool output so a huge diff cannot blow up the LLM context.
const maxOutputBytes = 64 * 1024

// Tool is the MCP tool descriptor returned by tools/list.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// prop is a shorthand for building JSON-Schema property maps.
type prop = map[string]interface{}

func schema(props prop, required ...string) prop {
	s := prop{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) prop  { return prop{"type": "string", "description": desc} }
func boolProp(desc string) prop { return prop{"type": "boolean", "description": desc} }

func enumProp(desc string, values ...string) prop {
	return prop{"type": "string", "description": desc, "enum": values}
}

// ToolDefinitions returns the complete list of MCP tools exposed by this server.
func ToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "git_current_branch",
			Description: "Get the name of the current git branch (or the commit hash when HEAD is detached).",
			InputSchema: schema(prop{}),
		},
		{
			Name: "git_list_files",
			Description: "List files in the repository. filter=changed (default) shows modified/added/deleted/untracked files with their status codes; " +
				"filter=staged shows files staged for commit; filter=untracked shows only untracked files; filter=all lists every tracked file.",
			InputSchema: schema(prop{
				"filter": enumProp("Which files to list (default: changed)", "changed", "staged", "untracked", "all"),
			}),
		},
		{
			Name: "git_diff",
			Description: "Show the diff of local changes. By default unstaged changes (working tree vs index); " +
				"set staged=true for staged changes (index vs HEAD). Optionally limit to a single file or directory path.",
			InputSchema: schema(prop{
				"staged": boolProp("Show staged changes instead of unstaged (default: false)"),
				"path":   strProp("Limit the diff to this file or directory (relative to the repository root)"),
			}),
		},
	}
}

// Server executes git tools against one fixed repository directory.
type Server struct {
	repo string
}

// New validates that dir is inside a git work tree and returns a Server bound to it.
func New(dir string) (*Server, error) {
	s := &Server{repo: dir}
	top, err := s.git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository: %w", dir, err)
	}
	s.repo = strings.TrimSpace(top)
	return s, nil
}

// Repo returns the resolved repository root.
func (s *Server) Repo() string { return s.repo }

// CallTool dispatches a tool call by name.
func (s *Server) CallTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "git_current_branch":
		return s.currentBranch()
	case "git_list_files":
		return s.listFiles(strArg(args, "filter", "changed"))
	case "git_diff":
		return s.diff(boolArg(args, "staged"), strArg(args, "path", ""))
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ---------------------------------------------------------------------------
// Tool implementations
// ---------------------------------------------------------------------------

func (s *Server) currentBranch() (string, error) {
	out, err := s.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if branch == "HEAD" { // detached HEAD — report the commit instead
		hash, err := s.git("rev-parse", "--short", "HEAD")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("detached HEAD at %s", strings.TrimSpace(hash)), nil
	}
	return branch, nil
}

func (s *Server) listFiles(filter string) (string, error) {
	switch filter {
	case "changed", "":
		out, err := s.git("status", "--porcelain")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			return "working tree clean — no changed files", nil
		}
		return truncate(out), nil
	case "staged":
		out, err := s.git("diff", "--name-status", "--cached")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			return "nothing staged for commit", nil
		}
		return truncate(out), nil
	case "untracked":
		out, err := s.git("ls-files", "--others", "--exclude-standard")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			return "no untracked files", nil
		}
		return truncate(out), nil
	case "all":
		out, err := s.git("ls-files")
		if err != nil {
			return "", err
		}
		return truncate(out), nil
	default:
		return "", fmt.Errorf("unknown filter %q (want changed|staged|untracked|all)", filter)
	}
}

func (s *Server) diff(staged bool, path string) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	if path != "" {
		if strings.HasPrefix(path, "-") {
			return "", fmt.Errorf("invalid path %q", path)
		}
		args = append(args, "--", path)
	}
	out, err := s.git(args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		kind := "unstaged"
		if staged {
			kind = "staged"
		}
		return fmt.Sprintf("no %s changes", kind), nil
	}
	return truncate(out), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// git runs one git command in the fixed repository directory.
// exec.Command passes argv directly — no shell, no injection surface.
func (s *Server) git(args ...string) (string, error) {
	full := append([]string{"-C", s.repo}, args...)
	cmd := exec.Command("git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func truncate(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + fmt.Sprintf("\n… [truncated: output exceeded %d bytes]", maxOutputBytes)
}

func strArg(args map[string]interface{}, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func boolArg(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}
