package usecase

import (
	"context"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// TrimHistory splits history into keep (last limit messages) and excess (older messages).
// If limit <= 0 or len(history) <= limit, all messages are kept and excess is nil.
func TrimHistory(history []domain.Message, limit int) (keep, excess []domain.Message) {
	if limit <= 0 || len(history) <= limit {
		return history, nil
	}
	splitAt := len(history) - limit
	excess = make([]domain.Message, splitAt)
	copy(excess, history[:splitAt])
	keep = history[splitAt:]
	return keep, excess
}

// SlidingWindow returns the last n messages from history, dropping older ones.
func SlidingWindow(history []domain.Message, n int) []domain.Message {
	if n <= 0 || len(history) <= n {
		return history
	}
	return history[len(history)-n:]
}

// BuildSummary uses the LLM to produce a concise summary of trimmed messages,
// incorporating an existing prior summary when provided.
func BuildSummary(ctx context.Context, client port.LLMClient, model string, trimmed []domain.Message, existing string, debug bool) (string, error) {
	var sb strings.Builder
	if existing != "" {
		sb.WriteString("Existing summary of earlier conversation:\n")
		sb.WriteString(existing)
		sb.WriteString("\n\nNew messages to incorporate into the summary:\n")
	} else {
		sb.WriteString("Messages to summarize:\n")
	}
	for _, m := range trimmed {
		fmt.Fprintf(&sb, "[%s]: %s\n\n", m.Role, m.Content)
	}

	sysMsg := "You are a helpful assistant that creates concise summaries of conversations. " +
		"Summarize the provided conversation messages into 1-3 paragraphs, focusing on key facts, " +
		"decisions, questions asked, and context useful for continuing the conversation. " +
		"If an existing summary is provided, incorporate the new messages to produce an updated summary. " +
		"Write in the third person as a neutral observer."

	resp, err := client.Chat(ctx, port.LLMRequest{
		Model: model,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: sysMsg},
			{Role: domain.RoleUser, Content: sb.String()},
		},
		MaxTokens:   1000,
		Temperature: -1,
		Debug:       debug,
	})
	if err != nil {
		return "", fmt.Errorf("summary generation failed: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}
