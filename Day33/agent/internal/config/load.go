package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
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
		}
	}
	return nil
}
