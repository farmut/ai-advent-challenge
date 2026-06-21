package domain

import "strings"

// ModelPrice contains per-token pricing and context window size for a model.
type ModelPrice struct {
	InputPer1M    float64
	OutputPer1M   float64
	ContextWindow int
}

// KnownPrices maps model identifiers to their pricing data.
var KnownPrices = map[string]ModelPrice{
	"gpt-4o":               {2.50, 10.00, 128_000},
	"gpt-4o-mini":          {0.15, 0.60, 128_000},
	"gpt-4-turbo":          {10.00, 30.00, 128_000},
	"gpt-3.5-turbo":        {0.50, 1.50, 16_385},
	"o1":                   {15.00, 60.00, 200_000},
	"o1-mini":              {3.00, 12.00, 128_000},
	"openai/gpt-4o":        {2.50, 10.00, 128_000},
	"openai/gpt-4o-mini":   {0.15, 0.60, 128_000},
	"openai/gpt-4-turbo":   {10.00, 30.00, 128_000},
	"openai/gpt-3.5-turbo": {0.50, 1.50, 16_385},
	"openai/o1":            {15.00, 60.00, 200_000},
	"openai/o1-mini":       {3.00, 12.00, 128_000},

	"anthropic/claude-opus-4":     {15.00, 75.00, 200_000},
	"anthropic/claude-sonnet-4":   {3.00, 15.00, 200_000},
	"anthropic/claude-3-5-sonnet": {3.00, 15.00, 200_000},
	"anthropic/claude-3-haiku":    {0.25, 1.25, 200_000},
	"anthropic/claude-3-opus":     {15.00, 75.00, 200_000},

	"qwen/qwen3.5-9b":        {0.06, 0.24, 32_768},
	"qwen/qwen3.5-122b-a10b": {0.14, 0.56, 32_768},
	"qwen/qwen3.5-397b-a17b": {0.40, 1.60, 32_768},
}

// PricingFor returns pricing for model, stripping any ":variant" suffix if needed.
func PricingFor(model string) (ModelPrice, bool) {
	if p, ok := KnownPrices[model]; ok {
		return p, true
	}
	if idx := strings.LastIndex(model, ":"); idx != -1 {
		if p, ok := KnownPrices[model[:idx]]; ok {
			return p, true
		}
	}
	return ModelPrice{}, false
}

// EstimateTokens estimates the token count for a piece of text (≈ 4 chars/token).
func EstimateTokens(text string) int {
	n := len([]rune(text)) / 4
	if n < 1 {
		return 1
	}
	return n
}

// EstimateMessagesTokens estimates the total token count for a slice of messages.
func EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += EstimateTokens(m.Content) + 4
	}
	return total
}

// ContextStatus returns a human-readable fill-level label for the given percentage.
func ContextStatus(pct float64) string {
	switch {
	case pct >= 95:
		return "[CRITICAL: context almost full!]"
	case pct >= 80:
		return "[WARN: context filling up]"
	case pct >= 50:
		return "[NOTE: context half full]"
	default:
		return "[OK]"
	}
}
