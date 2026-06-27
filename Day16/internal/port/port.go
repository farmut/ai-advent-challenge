package port

import (
	"context"
	"time"

	"ai-adv-agent/internal/domain"
)

// LLMRequest bundles all parameters for a single chat completion call.
type LLMRequest struct {
	Model       string
	Messages    []domain.Message
	MaxTokens   int
	Stop        []string
	Temperature float64 // negative = use provider default
	Debug       bool
}

// LLMResponse is the structured result returned by an LLM call.
type LLMResponse struct {
	Content      string
	Usage        domain.Usage
	FinishReason string
	Elapsed      time.Duration
}

// LLMClient is the outbound port for any OpenAI-compatible chat-completion provider.
type LLMClient interface {
	Chat(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// HistoryRepository manages the Layer 1 (STM) conversation log.
type HistoryRepository interface {
	Load() ([]domain.Message, error)
	Save(messages []domain.Message) error
}

// StatsRepository persists per-session token / cost statistics.
type StatsRepository interface {
	Load() (domain.SessionStats, error)
	Save(stats domain.SessionStats) error
}

// SummaryRepository stores the rolling text summary of older conversation turns.
type SummaryRepository interface {
	Load() (string, error)
	Save(content string) error
}

// FactsRepository manages the KV facts store used by the sticky-facts strategy.
type FactsRepository interface {
	Load() (domain.FactsStore, error)
	Save(facts domain.FactsStore) error
}

// WorkingMemoryRepository manages Layer 2 (task facts).
type WorkingMemoryRepository interface {
	Load() (domain.WorkingMemory, error)
	Save(wm domain.WorkingMemory) error
}

// LongTermMemoryRepository manages Layer 3 (profile / knowledge).
type LongTermMemoryRepository interface {
	Load() (domain.LongTermMemory, error)
	Save(ltm domain.LongTermMemory) error
}

// BranchRepository manages branching state and per-branch conversation histories.
type BranchRepository interface {
	LoadState() (domain.BranchState, error)
	SaveState(bs domain.BranchState) error
	LoadHistory(branchName string) ([]domain.Message, error)
	SaveHistory(branchName string, messages []domain.Message) error
}

// UserProfileRepository manages the explicit user profile (name + preferences).
type UserProfileRepository interface {
	Load() (domain.UserProfile, error)
	Save(profile domain.UserProfile) error
}

// TaskRepository persists the active task state across the 4-phase state machine.
// Load returns (state, true, nil) when a task is in progress, or (zero, false, nil) when none.
type TaskRepository interface {
	Load() (domain.TaskState, bool, error)
	Save(ts domain.TaskState) error
	Clear() error
}

// InvariantsRepository loads the user-defined invariants document (Markdown).
// Invariants are absolute constraints the agent must never violate.
// The repository is read-only: the file is managed by the user directly.
// Load returns an empty string when no invariants file exists (no-op, not an error).
type InvariantsRepository interface {
	Load() (string, error)
}

// MCPRepository persists MCP server configurations to a YAML file.
type MCPRepository interface {
	Load() (domain.MCPConfig, error)
	Save(cfg domain.MCPConfig) error
}

// MCPToolLister connects to an MCP server and retrieves its tool list.
type MCPToolLister interface {
	ListTools(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error)
}
