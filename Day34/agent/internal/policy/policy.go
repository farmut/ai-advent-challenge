// Package policy implements the client-side file-access guard for MCP tools.
//
// It is deliberately isolated: it depends only on the standard library and
// internal/domain, so the guard can be tested without touching LLM or MCP
// machinery (app -> policy -> domain, no cycles).
//
// The guard enforces an asymmetric permission model: reading/searching is
// allowed anywhere under ReadRoot, while writing is confined to WriteRoot.
// It is a policy, not a sandbox — see the README for what it does not guarantee.
package policy

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"ai-adv-agent/internal/domain"
)

// ToolKind classifies a tool by the kind of filesystem access it performs.
type ToolKind int

const (
	// KindNeutral marks tools without file path arguments (git_*, grep,
	// find_files, multi_grep). The guard never inspects their arguments.
	KindNeutral ToolKind = iota
	// KindRead marks tools whose paths must resolve inside ReadRoot.
	KindRead
	// KindWrite marks tools whose paths must resolve inside WriteRoot.
	KindWrite
)

// String renders the kind for audit lines and error messages.
func (k ToolKind) String() string {
	switch k {
	case KindRead:
		return "read"
	case KindWrite:
		return "write"
	default:
		return "neutral"
	}
}

// Error codes. Exactly four codes exist; every denial message starts with one
// of them, because the message is fed back to the LLM as a tool result and
// therefore acts as a prompt telling the model what to do next.
const (
	CodePermissionDenied = "PERMISSION_DENIED"
	CodeReadOnlyPath     = "READ_ONLY_PATH"
	CodeDeniedByPolicy   = "DENIED_BY_POLICY"
	CodeBadArgument      = "BAD_ARGUMENT"
)

// Codes lists every valid error code.
func Codes() []string {
	return []string{CodePermissionDenied, CodeReadOnlyPath, CodeDeniedByPolicy, CodeBadArgument}
}

// Error is a policy verdict. Error() renders "CODE: explanation + next step".
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func newErr(code, format string, a ...interface{}) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// CodeOf extracts the policy code from an error, or "" if it is not a policy error.
func CodeOf(err error) string {
	if pe, ok := err.(*Error); ok {
		return pe.Code
	}
	return ""
}

// ToolSpec describes how the guard must treat a single tool.
type ToolSpec struct {
	Kind ToolKind
	// PathArgs are argument names holding a single path string.
	PathArgs []string
	// ListArgs are argument names holding an array of path strings
	// (read_multiple_files.paths).
	ListArgs []string
	// MustExist is the default resolution mode for this tool's paths:
	// true resolves the path itself, false resolves its parent directory.
	MustExist bool
	// NewPathArgs overrides MustExist to false for the listed argument names.
	// This exists for move_file, whose "source" must exist while its
	// "destination" must not.
	NewPathArgs []string
}

// mustExistFor reports the resolution mode for a concrete argument.
func (s ToolSpec) mustExistFor(arg string) bool {
	for _, n := range s.NewPathArgs {
		if n == arg {
			return false
		}
	}
	return s.MustExist
}

// FS is the guard itself. The zero value is safe: no specs, no roots, so every
// tool is treated as neutral and nothing panics (see internal/app testToolbelt).
type FS struct {
	// ReadRoot is absolute and already symlink-resolved; "" forbids fs reads.
	ReadRoot string
	// WriteRoot is absolute; "" forbids writes entirely (fail-closed).
	WriteRoot string
	// Deny holds glob patterns, compared case-insensitively.
	Deny []string
	// Specs maps a tool name to its spec; a missing name means KindNeutral.
	Specs map[string]ToolSpec
	// FSServers lists MCP server names whose tools are file tools. A tool
	// coming from one of these servers but missing from Specs is denied
	// (fail-closed). Tools from any other server are allowed through.
	FSServers []string
	// Role is recorded in the audit log.
	Role string
	// Audit receives audit lines; nil means os.Stderr.
	Audit io.Writer
}

// isFSServer reports whether the named MCP server is guarded as a file server.
func (p FS) isFSServer(server string) bool {
	for _, s := range p.FSServers {
		if s == server {
			return true
		}
	}
	return false
}

// AllowsTool reports whether the tool may be exposed to the model at all.
// Unknown tools are neutral and therefore allowed; a write tool without a
// WriteRoot (or a read tool without a ReadRoot) is hidden.
func (p FS) AllowsTool(tool string) bool {
	spec, ok := p.Specs[tool]
	if !ok {
		return true
	}
	switch spec.Kind {
	case KindWrite:
		return p.WriteRoot != ""
	case KindRead:
		return p.ReadRoot != ""
	default:
		return true
	}
}

// Filter drops tools that AllowsTool rejects. It is a UX filter (do not offer
// what will always fail); the executor's Check is the actual security boundary.
func (p FS) Filter(tools []domain.MCPTool) []domain.MCPTool {
	if len(tools) == 0 {
		return tools
	}
	out := make([]domain.MCPTool, 0, len(tools))
	for _, t := range tools {
		if p.AllowsTool(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// FilterServer is Filter plus the fail-closed rule: a tool from a guarded file
// server that has no spec is not exposed at all.
func (p FS) FilterServer(server string, tools []domain.MCPTool) []domain.MCPTool {
	if len(tools) == 0 {
		return tools
	}
	fsSrv := p.isFSServer(server)
	out := make([]domain.MCPTool, 0, len(tools))
	for _, t := range tools {
		if _, known := p.Specs[t.Name]; fsSrv && !known {
			continue
		}
		if p.AllowsTool(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// Check validates a tool invocation before it is forwarded to the MCP server.
// The server name is required to apply the fail-closed rule for file servers.
//
// On success Check REWRITES every path argument in args to the absolute path it
// approved. This is load-bearing, not a convenience: the file server resolves a
// relative path against its own working directory, which is not our read root,
// so forwarding the raw path would execute a different path than the one that
// passed the check — a permission bypass. Callers must pass the mutated args to
// the server.
func (p FS) Check(server, tool string, args map[string]interface{}) error {
	spec, ok := p.Specs[tool]
	if !ok {
		if p.isFSServer(server) {
			return p.deny(tool, "", "", newErr(CodeDeniedByPolicy,
				"инструмент %q пришёл с файлового сервера, но политика для него не определена. Он недоступен — используй только перечисленные файловые инструменты.", tool))
		}
		// Non-file server (git, search): neutral, out of the guard's scope.
		return nil
	}

	if spec.Kind == KindNeutral {
		return nil
	}

	root := p.ReadRoot
	if spec.Kind == KindWrite {
		root = p.WriteRoot
		if root == "" {
			return p.deny(tool, "", "", newErr(CodePermissionDenied,
				"инструмент %s не выдан этой роли — запись файлов запрещена. Верни результат текстом в ответе.", tool))
		}
	} else if root == "" {
		return p.deny(tool, "", "", newErr(CodePermissionDenied,
			"инструмент %s не выдан этой роли — чтение файлов запрещено. Опирайся на то, что уже есть в контексте.", tool))
	}

	for _, arg := range spec.PathArgs {
		raw, present := args[arg]
		if !present || raw == nil {
			return p.deny(tool, arg, "", p.badArg(spec.Kind, arg))
		}
		s, ok := raw.(string)
		if !ok {
			return p.deny(tool, arg, "", newErr(CodeBadArgument,
				"аргумент %q должен быть строкой с относительным путём, а пришёл %T. Передай путь строкой, например %q.", arg, raw, p.sampleRel(spec.Kind)))
		}
		abs, err := p.checkPath(tool, arg, s, spec, root)
		if err != nil {
			return err
		}
		// Hand the server the exact path we approved. Relative paths would be
		// resolved against the server process's own cwd, which is not our root.
		args[arg] = abs
	}

	for _, arg := range spec.ListArgs {
		raw, present := args[arg]
		if !present || raw == nil {
			return p.deny(tool, arg, "", newErr(CodeBadArgument,
				"аргумент %q обязателен и должен быть непустым массивом относительных путей, например [%q].", arg, p.sampleRel(spec.Kind)))
		}
		list, ok := raw.([]interface{})
		if !ok {
			return p.deny(tool, arg, "", newErr(CodeBadArgument,
				"аргумент %q должен быть массивом строк, а пришёл %T. Передай список путей массивом.", arg, raw))
		}
		if len(list) == 0 {
			return p.deny(tool, arg, "", newErr(CodeBadArgument,
				"аргумент %q пуст. Укажи хотя бы один относительный путь.", arg))
		}
		normalised := make([]interface{}, len(list))
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				return p.deny(tool, arg, "", newErr(CodeBadArgument,
					"элемент %d аргумента %q должен быть строкой с относительным путём, а пришёл %T. Передай пути строками.", i, arg, item))
			}
			abs, err := p.checkPath(tool, arg, s, spec, root)
			if err != nil {
				return err
			}
			normalised[i] = abs
		}
		args[arg] = normalised
	}

	if spec.Kind == KindWrite {
		p.audit(tool, "", "", "ALLOW", "")
	}
	return nil
}

// checkPath resolves one path argument and applies the deny globs.
//
// Paths arrive in a single namespace — relative to the project root the file
// server was started with — so they are always joined onto the base root; the
// access kind only decides which root the result must stay inside.
// checkPath validates one path argument and returns the absolute path the guard
// approved. Callers must forward that absolute path to the server instead of the
// raw one — see the normalisation note on Check.
func (p FS) checkPath(tool, arg, raw string, spec ToolSpec, root string) (string, error) {
	// Deny globs are checked twice. First lexically, before the path is resolved:
	// a denied path must be refused as policy regardless of whether it exists, so
	// the error cannot be used to probe which forbidden files are present. The
	// second check runs after resolution and catches a symlink aimed at a denied
	// target, which the lexical form cannot see.
	if !filepath.IsAbs(raw) {
		if pattern, hit := p.denied(filepath.ToSlash(filepath.Clean(raw))); hit {
			return "", p.deny(tool, arg, raw, newErr(CodeDeniedByPolicy,
				"путь %q закрыт политикой безопасности (правило %q: секреты и служебные файлы). Не пытайся получить его другим способом — работай с другими файлами.", raw, pattern))
		}
	}

	abs, rel, err := resolveUnder(p.baseRoot(), root, raw, spec.mustExistFor(arg), spec.Kind, p.writeHint())
	if err != nil {
		return "", p.deny(tool, arg, raw, err)
	}
	if pattern, hit := p.denied(rel); hit {
		return "", p.deny(tool, arg, rel, newErr(CodeDeniedByPolicy,
			"путь %q закрыт политикой безопасности (правило %q: секреты и служебные файлы). Не пытайся получить его другим способом — работай с другими файлами.", rel, pattern))
	}
	return abs, nil
}

// baseRoot is the root every relative path is joined onto: the read root when
// there is one, otherwise the write root.
func (p FS) baseRoot() string {
	if p.ReadRoot != "" {
		return p.ReadRoot
	}
	return p.WriteRoot
}

// badArg builds the missing-argument message.
func (p FS) badArg(kind ToolKind, arg string) *Error {
	return newErr(CodeBadArgument,
		"аргумент %q обязателен и должен быть относительным путём, например %q. Пустой путь недопустим — корень по умолчанию не подставляется.", arg, p.sampleRel(kind))
}

// sampleRel produces an example path for the given access kind.
func (p FS) sampleRel(kind ToolKind) string {
	if kind == KindWrite {
		return p.writeHint() + "notes.md"
	}
	return "internal/app/toolbelt.go"
}

// writeHint renders the write root as a short human-facing prefix.
func (p FS) writeHint() string {
	if p.WriteRoot == "" {
		return "workspace/"
	}
	return filepath.Base(p.WriteRoot) + "/"
}

// denied reports whether the resolved relative path matches a deny glob.
// Comparison is lower-cased because macOS filesystems are case-insensitive,
// so a deny rule of ".env*" must also catch ".ENV".
func (p FS) denied(rel string) (string, bool) {
	norm := strings.ToLower(filepath.ToSlash(rel))
	for _, pattern := range p.Deny {
		if matchGlob(strings.ToLower(pattern), norm) {
			return pattern, true
		}
	}
	return "", false
}

// matchGlob implements the glob dialect used by deny rules. path.Match has no
// "**" support, so the recursive forms are handled by hand.
func matchGlob(pattern, rel string) bool {
	if pattern == "" || rel == "" {
		return false
	}
	switch {
	// "**/Y/**": any path segment equals Y.
	case strings.HasPrefix(pattern, "**/") && strings.HasSuffix(pattern, "/**") && len(pattern) > 6:
		mid := pattern[3 : len(pattern)-3]
		for _, seg := range strings.Split(rel, "/") {
			if ok, err := path.Match(mid, seg); err == nil && ok {
				return true
			}
		}
		return false
	// "**/Y": match the last segment.
	case strings.HasPrefix(pattern, "**/"):
		suffix := pattern[3:]
		ok, err := path.Match(suffix, path.Base(rel))
		return err == nil && ok
	// "X/**": the prefix itself or anything beneath it.
	case strings.HasSuffix(pattern, "/**"):
		prefix := pattern[:len(pattern)-3]
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	default:
		if ok, err := path.Match(pattern, rel); err == nil && ok {
			return true
		}
		ok, err := path.Match(pattern, path.Base(rel))
		return err == nil && ok
	}
}

// resolveUnder resolves rel against base and proves that the result lies
// inside constraint. base is the namespace the model addresses paths in (the
// project root); constraint is the root this access kind may touch — the same
// root for reads, the narrower write root for writes.
//
// It returns the resolved absolute path and the path relative to base (that is
// what the deny globs are written against).
//
// Containment is established ONLY through filepath.Rel. A prefix comparison
// such as strings.HasPrefix(abs, root) is wrong: "/tmp/root-evil/secret.txt"
// passes it while lying outside "/tmp/root".
func resolveUnder(base, constraint, rel string, mustExist bool, kind ToolKind, writeHint string) (string, string, error) {
	escape := func(format string, a ...interface{}) *Error {
		if kind == KindWrite {
			return newErr(CodeReadOnlyPath, format+" Запись разрешена только внутри %s — сохрани результат туда, например %snotes.md.",
				append(a, writeHint, writeHint)...)
		}
		return newErr(CodePermissionDenied, format+" Чтение разрешено только внутри корня проекта — используй путь относительно него.", a...)
	}

	// 1. Empty path: never default to the root.
	if rel == "" {
		return "", "", newErr(CodeBadArgument,
			"путь пуст, а он обязателен. Укажи относительный путь, например %q.", "internal/app/toolbelt.go")
	}

	// 2. Absolute paths are refused, not rebased.
	if filepath.IsAbs(rel) {
		return "", "", newErr(CodeBadArgument,
			"путь %q абсолютный. Передавай только относительный путь — абсолютные пути не перебазируются.", rel)
	}

	// 3. Textual escape via "..".
	c := filepath.Clean(rel)
	if c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", "", escape("путь %q выходит за пределы разрешённого корня.", rel)
	}

	// 4. Both roots must exist.
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", "", newErr(CodeDeniedByPolicy,
			"разрешённый корень %q недоступен (%v). Файловые операции сейчас невозможны.", base, err)
	}
	constraintReal := baseReal
	if constraint != base {
		constraintReal, err = filepath.EvalSymlinks(constraint)
		if err != nil {
			return "", "", newErr(CodeDeniedByPolicy,
				"разрешённый корень %q недоступен (%v). Файловые операции сейчас невозможны.", constraint, err)
		}
	}

	full := filepath.Join(baseReal, c)

	var real string
	if mustExist {
		// 5a. Resolve the target itself.
		real, err = filepath.EvalSymlinks(full)
		if err != nil {
			return "", "", newErr(CodeBadArgument,
				"путь %q не существует или недоступен. Проверь путь — например, посмотри каталог через list_directory.", rel)
		}
	} else {
		// 5b. Resolve the parent and re-attach the base name.
		name := filepath.Base(full)
		if name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
			return "", "", newErr(CodeBadArgument,
				"путь %q не указывает на имя файла. Укажи путь вида каталог/имя_файла.", rel)
		}
		parentReal, err := filepath.EvalSymlinks(filepath.Dir(full))
		if err != nil {
			return "", "", newErr(CodeBadArgument,
				"родительский каталог для %q не существует. Сначала создай его через create_directory, затем повтори запись.", rel)
		}
		real = filepath.Join(parentReal, name)
	}

	// 6. Containment via Rel only — never via a string prefix.
	if _, err := relUnder(constraintReal, real); err != nil {
		return "", "", escape("путь %q после разыменования ссылок оказывается за пределами разрешённого корня.", rel)
	}
	r, err := relUnder(baseReal, real)
	if err != nil {
		return "", "", escape("путь %q после разыменования ссылок оказывается за пределами разрешённого корня.", rel)
	}

	return real, filepath.ToSlash(r), nil
}

// relUnder returns the path of target relative to root, or an error when target
// is not contained in root.
func relUnder(root, target string) (string, error) {
	r, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", target, root)
	}
	return r, nil
}

// deny records a denial in the audit log and returns the error unchanged.
func (p FS) deny(tool, arg, rel string, err error) error {
	p.audit(tool, arg, rel, "DENY", CodeOf(err))
	return err
}

// audit writes one structured line per write attempt and per denial.
func (p FS) audit(tool, arg, rel, verdict, code string) {
	w := p.Audit
	if w == nil {
		w = os.Stderr
	}
	role := p.Role
	if role == "" {
		role = "-"
	}
	line := fmt.Sprintf("[fsguard] role=%s tool=%s", role, tool)
	if arg != "" {
		line += " arg=" + arg
	}
	if rel != "" {
		line += " rel=" + rel
	}
	line += " verdict=" + verdict
	if code != "" {
		line += " code=" + code
	}
	fmt.Fprintln(w, line)
}
