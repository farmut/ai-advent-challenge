package domain

// SessionStats accumulates token usage and cost across multiple LLM calls.
type SessionStats struct {
	PromptTokens         int     `json:"prompt_tokens"`
	CompletionTokens     int     `json:"completion_tokens"`
	TotalTokens          int     `json:"total_tokens"`
	EstimatedCostUSD     float64 `json:"estimated_cost_usd"`
	Calls                int     `json:"calls"`
	LastCallPromptTokens int     `json:"last_call_prompt_tokens"`
}
