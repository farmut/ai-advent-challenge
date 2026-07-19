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
	Temperature float64          // negative = use provider default
	Debug       bool
	Tools       []domain.MCPTool // optional; enables tool-calling when non-empty
}

// LLMResponse is the structured result returned by an LLM call.
type LLMResponse struct {
	Content      string
	Usage        domain.Usage
	FinishReason string
	Elapsed      time.Duration
	ToolCalls    []domain.ToolCall // non-nil when finish_reason == "tool_calls"
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

// TaskMemoryRepository persists the dialogue task memory (goal, clarified points,
// fixed constraints/terms) across the turns of a RAG conversation.
type TaskMemoryRepository interface {
	Load() (domain.TaskMemory, error)
	Save(tm domain.TaskMemory) error
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

// MCPToolCaller connects to an MCP server and executes a named tool.
type MCPToolCaller interface {
	CallTool(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error)
}

// MCPPool is a set of persistent MCP server sessions that can list tools and call
// tools without spawning a new subprocess per request.  Close must be called when
// the pool is no longer needed.
type MCPPool interface {
	MCPToolLister
	MCPToolCaller
	Close()
}

// MCPPoolOpener creates a new MCPPool for the given server configurations.
// Errors from individual servers are returned alongside a valid (partial) pool.
type MCPPoolOpener interface {
	OpenPool(cfgs []domain.MCPServerConfig) (MCPPool, []error)
}

// ToolExecutor is the callback invoked by ChatUseCase.ExecuteWithTools for each tool call.
// name is the tool name; argsJSON is the JSON-encoded arguments string from the LLM.
type ToolExecutor func(ctx context.Context, name, argsJSON string) (string, error)

// Retriever performs semantic search over an indexed knowledge base and returns
// the chunks most relevant to a query. It is the retrieval step of the RAG
// pipeline (question → retrieve relevant chunks → combine with question → LLM).
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int) ([]domain.RetrievedChunk, error)
}

// Reranker is the optional second stage of the RAG pipeline: it re-scores the
// chunks returned by the Retriever for their relevance to the query and returns
// them sorted best-first with domain.RetrievedChunk.RerankScore populated. Unlike
// the embedding similarity used for retrieval, a reranker judges the query and
// each chunk jointly, so it can promote chunks that are topically on-point and
// demote ones that merely share vocabulary.
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []domain.RetrievedChunk) ([]domain.RetrievedChunk, error)
}
