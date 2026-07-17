package domain

// FactsStore is the KV store used by the sticky-facts strategy.
type FactsStore struct {
	Facts map[string]string `json:"facts"`
}

// WorkingMemory is Layer 2: task-specific facts for the current session.
type WorkingMemory struct {
	Facts     map[string]string `json:"facts"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// LongTermMemory is Layer 3: user profile, strategic decisions, accumulated knowledge.
type LongTermMemory struct {
	Entries   map[string]string `json:"entries"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// BranchCheckpoint is a named snapshot of conversation history.
type BranchCheckpoint struct {
	Messages  []Message `json:"messages"`
	CreatedAt string    `json:"created_at"`
}

// BranchState tracks the active branch and all named checkpoints.
type BranchState struct {
	Current     string                      `json:"current"`
	Branches    []string                    `json:"branches"`
	Checkpoints map[string]BranchCheckpoint `json:"checkpoints"`
}

// DefaultBranchState returns the initial branch state with only the "main" branch.
func DefaultBranchState() BranchState {
	return BranchState{
		Current:     "main",
		Branches:    []string{"main"},
		Checkpoints: make(map[string]BranchCheckpoint),
	}
}
