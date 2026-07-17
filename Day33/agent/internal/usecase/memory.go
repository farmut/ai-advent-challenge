package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// StripJSONFences removes ```json or ``` fences from LLM output.
func StripJSONFences(s string) string {
	if after, found := strings.CutPrefix(s, "```json"); found {
		s = strings.TrimSpace(after)
	} else if after, found := strings.CutPrefix(s, "```"); found {
		s = strings.TrimSpace(after)
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// WMSystemBlock formats working memory as a section to prepend to the system message.
func WMSystemBlock(wm domain.WorkingMemory) string {
	if len(wm.Facts) == 0 {
		return ""
	}
	keys := sortedKeys(wm.Facts)
	var sb strings.Builder
	sb.WriteString("[Working Memory — Task Facts]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, wm.Facts[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

// LTMSystemBlock formats long-term memory as a section to prepend to the system message.
func LTMSystemBlock(ltm domain.LongTermMemory) string {
	if len(ltm.Entries) == 0 {
		return ""
	}
	keys := sortedKeys(ltm.Entries)
	var sb strings.Builder
	sb.WriteString("[Long-term Memory — Profile & Knowledge]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, ltm.Entries[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

// FactsSystemBlock formats the sticky-facts store as a section to prepend to the system message.
func FactsSystemBlock(fs domain.FactsStore) string {
	if len(fs.Facts) == 0 {
		return ""
	}
	keys := sortedKeys(fs.Facts)
	var sb strings.Builder
	sb.WriteString("[Sticky Facts]\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s: %s\n", k, fs.Facts[k])
	}
	return strings.TrimRight(sb.String(), "\n")
}

// UpdateWM calls the LLM to extract and update Layer-2 task facts from the latest exchange.
func UpdateWM(ctx context.Context, client port.LLMClient, model string, existing domain.WorkingMemory, userMsg, assistantMsg string, debug bool) (domain.WorkingMemory, error) {
	prompt := buildUpdatePrompt(existing.Facts, userMsg, assistantMsg,
		"Update the task facts with information about the current task: "+
			"active goals, current requirements, constraints, intermediate results, short-term decisions. "+
			"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store for an AI agent's working memory (Layer 2: task facts). " +
		"Focus on task-specific data: active goals, current state, constraints, decisions made this session. " +
		"Return ONLY valid JSON with string keys and string values."

	resp, err := client.Chat(ctx, port.LLMRequest{
		Model:       model,
		Messages:    []domain.Message{{Role: domain.RoleSystem, Content: sysMsg}, {Role: domain.RoleUser, Content: prompt}},
		MaxTokens:   500,
		Temperature: -1,
		Debug:       debug,
	})
	if err != nil {
		return existing, fmt.Errorf("WM update failed: %w", err)
	}
	updated, err := parseKVJSON(resp.Content)
	if err != nil {
		return existing, fmt.Errorf("failed to parse WM JSON: %w (response: %s)", err, resp.Content)
	}
	return domain.WorkingMemory{Facts: updated}, nil
}

// UpdateLTM calls the LLM to extract and update Layer-3 profile/knowledge from the latest exchange.
func UpdateLTM(ctx context.Context, client port.LLMClient, model string, existing domain.LongTermMemory, userMsg, assistantMsg string, debug bool) (domain.LongTermMemory, error) {
	prompt := buildUpdatePrompt(existing.Entries, userMsg, assistantMsg,
		"Update the long-term memory with information relevant across sessions: "+
			"user profile, stable preferences, long-term goals, important strategic decisions with rationale, "+
			"accumulated domain knowledge. Ignore transient task state. "+
			"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store for an AI agent's long-term memory (Layer 3: profile/knowledge). " +
		"Focus on: user profile, stable preferences, long-term goals, strategic decisions, " +
		"accumulated domain knowledge. Ignore ephemeral task state. " +
		"Return ONLY valid JSON with string keys and string values."

	resp, err := client.Chat(ctx, port.LLMRequest{
		Model:       model,
		Messages:    []domain.Message{{Role: domain.RoleSystem, Content: sysMsg}, {Role: domain.RoleUser, Content: prompt}},
		MaxTokens:   500,
		Temperature: -1,
		Debug:       debug,
	})
	if err != nil {
		return existing, fmt.Errorf("LTM update failed: %w", err)
	}
	updated, err := parseKVJSON(resp.Content)
	if err != nil {
		return existing, fmt.Errorf("failed to parse LTM JSON: %w (response: %s)", err, resp.Content)
	}
	return domain.LongTermMemory{Entries: updated}, nil
}

// UpdateFacts calls the LLM to extract and update sticky-facts from the latest exchange.
func UpdateFacts(ctx context.Context, client port.LLMClient, model string, existing domain.FactsStore, userMsg, assistantMsg string, debug bool) (domain.FactsStore, error) {
	prompt := buildUpdatePrompt(existing.Facts, userMsg, assistantMsg,
		"Update the facts JSON with any important new information (goals, constraints, preferences, decisions, agreements). "+
			"Return ONLY a valid JSON object with string key-value pairs. No explanation, no markdown fences.")

	sysMsg := "You maintain a key-value store of important persistent facts from a conversation. " +
		"Return ONLY valid JSON with string keys and string values. Include only genuinely important facts."

	resp, err := client.Chat(ctx, port.LLMRequest{
		Model:       model,
		Messages:    []domain.Message{{Role: domain.RoleSystem, Content: sysMsg}, {Role: domain.RoleUser, Content: prompt}},
		MaxTokens:   500,
		Temperature: -1,
		Debug:       debug,
	})
	if err != nil {
		return existing, fmt.Errorf("facts extraction failed: %w", err)
	}
	updated, err := parseKVJSON(resp.Content)
	if err != nil {
		return existing, fmt.Errorf("failed to parse facts JSON: %w (response: %s)", err, resp.Content)
	}
	return domain.FactsStore{Facts: updated}, nil
}

// ---- internal helpers ----

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func buildUpdatePrompt(existing map[string]string, userMsg, assistantMsg, instruction string) string {
	var sb strings.Builder
	if len(existing) > 0 {
		b, _ := json.Marshal(existing)
		sb.WriteString("Current data (JSON):\n")
		sb.Write(b)
		sb.WriteString("\n\n")
	}
	sb.WriteString("New exchange:\nUser: ")
	sb.WriteString(userMsg)
	sb.WriteString("\n\nAssistant: ")
	sb.WriteString(assistantMsg)
	sb.WriteString("\n\n")
	sb.WriteString(instruction)
	return sb.String()
}

func parseKVJSON(raw string) (map[string]string, error) {
	content := StripJSONFences(strings.TrimSpace(raw))
	var m map[string]string
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return nil, err
	}
	return m, nil
}
