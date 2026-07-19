package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"ai-adv-agent/internal/policy"
)

// Load reads the YAML config at path, layered on top of Default(). When path is
// empty the defaults are returned unchanged. Because unmarshalling merges into
// the pre-filled defaults, any field the file omits keeps its default — so an
// omitted `enabled:` leaves a memory layer on.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

// ResolveEnv fills empty LLM fields from the LLM_* environment variables and
// applies provider-specific defaults, mirroring the historical env-driven flow.
// It does not error on missing values — Validate reports those.
func ResolveEnv(cfg *Config) {
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = os.Getenv("LLM_PROVIDER")
	}
	if cfg.LLM.APIKey == "" {
		cfg.LLM.APIKey = os.Getenv("LLM_API_KEY")
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = os.Getenv("LLM_MODEL")
	}
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	}
	if cfg.LLM.CACert == "" {
		cfg.LLM.CACert = os.Getenv("LLM_CA_CERT")
	}
}

// Path substitution tokens usable inside an MCP server's args. They are replaced
// by ResolvePaths with the absolute roots, so a server started as a subprocess
// receives a path that does not depend on the agent's working directory.
const (
	TokenReadRoot  = "${READ_ROOT}"
	TokenWriteRoot = "${WRITE_ROOT}"
)

// ResolvePaths turns every configured filesystem root into an absolute path and
// substitutes the ${READ_ROOT} / ${WRITE_ROOT} tokens in the MCP servers' args.
//
// Roots are resolved against the directory holding the config file, not against
// the process working directory: both file servers take their directory as a
// command-line argument and inherit the agent's cwd, so a relative ".." would
// mean something different depending on where the agent was launched from.
//
// It never checks that a directory exists — that would make config tests require
// a filesystem. EvalSymlinks is best-effort for the same reason: when the root is
// not there yet (workspace/ is created by the Makefile) the absolute path is kept
// as is, and the guard refuses the call at request time instead.
func ResolvePaths(cfg *Config, configPath string) error {
	base, err := configBase(configPath)
	if err != nil {
		return err
	}

	readRoot, err := resolveRoot(base, cfg.MCP.FSGuard.ReadRoot, base)
	if err != nil {
		return fmt.Errorf("mcp.fs_guard.read_root: %w", err)
	}
	cfg.MCP.FSGuard.ReadRoot = readRoot

	// The first non-empty role write root becomes ${WRITE_ROOT}. Roles share one
	// workspace by design; a server process gets a single directory argument, so
	// there is nothing per-role to substitute.
	writeRoot := ""
	resolveTools := func(tp *ToolPolicy, what string) error {
		r, err := resolveRoot(base, tp.ReadRoot, "")
		if err != nil {
			return fmt.Errorf("%s.read_root: %w", what, err)
		}
		tp.ReadRoot = r
		w, err := resolveRoot(base, tp.WriteRoot, "")
		if err != nil {
			return fmt.Errorf("%s.write_root: %w", what, err)
		}
		tp.WriteRoot = w
		if writeRoot == "" {
			writeRoot = w
		}
		return nil
	}

	for i := range cfg.Orchestrator.SubAgents {
		sa := &cfg.Orchestrator.SubAgents[i]
		if err := resolveTools(&sa.Tools, fmt.Sprintf("orchestrator.subagents[%d].tools", i)); err != nil {
			return err
		}
	}
	if err := resolveTools(&cfg.Consultant.Tools, "consultant.tools"); err != nil {
		return err
	}

	for i := range cfg.MCP.Servers {
		args := cfg.MCP.Servers[i].Args
		for j, a := range args {
			a = strings.ReplaceAll(a, TokenReadRoot, readRoot)
			a = strings.ReplaceAll(a, TokenWriteRoot, writeRoot)
			args[j] = a
		}
	}
	return nil
}

// configBase returns the directory holding the config file, or the working
// directory when no config path was given.
func configBase(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("working directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	dir := filepath.Dir(abs)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real, nil
	}
	return dir, nil
}

// resolveRoot makes p absolute relative to base and resolves symlinks. An empty
// p yields fallback (itself possibly empty, which means "not granted").
func resolveRoot(base, p, fallback string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return fallback, nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(base, abs)
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	return abs, nil
}

// Supported LLM providers (kept here to avoid importing the llm adapter, which
// would create an import cycle for callers of the config package).
const (
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderGigaChat   = "gigachat"
)

// Validate checks that the resolved configuration is internally consistent and
// has the credentials needed for the enabled features. Call after ResolveEnv and
// any flag overrides.
func Validate(cfg Config) error {
	if cfg.LLM.Provider == "" {
		return fmt.Errorf("llm.provider is required (set it in the config or LLM_PROVIDER)")
	}
	switch cfg.LLM.Provider {
	case ProviderOpenAI, ProviderOpenRouter, ProviderGigaChat:
	default:
		return fmt.Errorf("unsupported llm.provider %q (supported: openai, openrouter, gigachat)", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("llm.api_key is required (set it in the config or LLM_API_KEY)")
	}
	if s := cfg.Memory.STM.Strategy; s != "" &&
		s != "sliding-window" && s != "sticky-facts" && s != "branching" {
		return fmt.Errorf("invalid memory.stm.strategy %q (want: sliding-window, sticky-facts, branching)", s)
	}
	if cfg.RAG.Rerank.Enabled {
		switch strings.ToLower(cfg.RAG.Rerank.Mode) {
		case "", "api", "chat", "auto":
		default:
			return fmt.Errorf("invalid rag.rerank.mode %q (want: api, chat, auto)", cfg.RAG.Rerank.Mode)
		}
	}
	if cfg.Orchestrator.Enabled {
		seen := map[string]bool{}
		for i, sa := range cfg.Orchestrator.SubAgents {
			if strings.TrimSpace(sa.Name) == "" {
				return fmt.Errorf("orchestrator.subagents[%d]: name is required", i)
			}
			if seen[sa.Name] {
				return fmt.Errorf("orchestrator.subagents: duplicate name %q", sa.Name)
			}
			seen[sa.Name] = true
			if err := validateToolPolicy(sa.Tools, fmt.Sprintf("orchestrator.subagents[%d] (%s)", i, sa.Name)); err != nil {
				return err
			}
		}
	}
	if err := validateToolPolicy(cfg.Consultant.Tools, "consultant"); err != nil {
		return err
	}
	return nil
}

// validateToolPolicy rejects a role whose tool grants contradict themselves: a
// tool listed in both allow and deny, or a write tool granted with no write root
// (which would be a permission the guard could never honour).
func validateToolPolicy(tp ToolPolicy, what string) error {
	denied := make(map[string]bool, len(tp.Deny))
	for _, d := range tp.Deny {
		denied[d] = true
	}
	specs := policy.DefaultSpecs()
	for _, a := range tp.Allow {
		if denied[a] {
			return fmt.Errorf("%s.tools: tool %q is in both allow and deny", what, a)
		}
		if tp.WriteRoot == "" && specs[a].Kind == policy.KindWrite {
			return fmt.Errorf("%s.tools: write tool %q is allowed but tools.write_root is empty (writes are forbidden)", what, a)
		}
	}
	return nil
}
