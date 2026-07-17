package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// TaskMemorySystemBlock renders the dialogue task memory as a system-message
// section the assistant must honour: the dialogue goal, what the user has
// already clarified, and the fixed constraints/terms. Returns "" when the memory
// is empty so callers can skip adding an empty system message.
func TaskMemorySystemBlock(tm domain.TaskMemory) string {
	if tm.IsEmpty() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Память задачи диалога]\n")
	if g := strings.TrimSpace(tm.Goal); g != "" {
		fmt.Fprintf(&sb, "Цель диалога: %s\n", g)
	}
	if len(tm.Clarified) > 0 {
		sb.WriteString("Пользователь уже уточнил:\n")
		for _, c := range tm.Clarified {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}
	if len(tm.Constraints) > 0 {
		sb.WriteString("Зафиксированные ограничения и термины:\n")
		for _, c := range tm.Constraints {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}
	sb.WriteString("Учитывай эту память: не переспрашивай уже уточнённое, соблюдай ограничения " +
		"и держись цели диалога.")
	return sb.String()
}

// taskMemoryJSON mirrors the JSON contract used to (de)serialise task memory in
// the LLM update round-trip.
type taskMemoryJSON struct {
	Goal        string   `json:"goal"`
	Clarified   []string `json:"clarified"`
	Constraints []string `json:"constraints"`
}

// UpdateTaskMemory asks the LLM to refresh the dialogue task memory from the
// latest exchange: sharpen the goal, append newly clarified points, and record
// any new fixed constraints/terms while preserving prior entries. On any failure
// the existing memory is returned unchanged so a bad update never wipes state.
//
// maxTokens bounds the completion (0 = provider default). Pass the session's
// budget rather than a small constant: reasoning models spend tokens on hidden
// reasoning first, so a tight cap can leave the visible content empty — in which
// case the update is skipped and the prior memory kept, without erroring.
func UpdateTaskMemory(ctx context.Context, client port.LLMClient, model string, maxTokens int, existing domain.TaskMemory, userMsg, assistantMsg string, debug bool) (domain.TaskMemory, error) {
	cur, _ := json.Marshal(taskMemoryJSON{
		Goal:        existing.Goal,
		Clarified:   existing.Clarified,
		Constraints: existing.Constraints,
	})

	var sb strings.Builder
	sb.WriteString("Текущая память задачи (JSON):\n")
	sb.Write(cur)
	sb.WriteString("\n\nНовый обмен репликами:\nПользователь: ")
	sb.WriteString(userMsg)
	sb.WriteString("\n\nАссистент: ")
	sb.WriteString(assistantMsg)
	sb.WriteString("\n\nОбнови память задачи диалога. Верни ТОЛЬКО валидный JSON вида " +
		`{"goal": "...", "clarified": ["..."], "constraints": ["..."]}. ` +
		"goal — одна короткая фраза, отражающая цель диалога (уточни её, если стало яснее). " +
		"clarified — что пользователь уже уточнил или подтвердил (сохрани прежние пункты, добавь новые, без дублей). " +
		"constraints — зафиксированные ограничения, определения и термины (сохрани прежние, добавь новые). " +
		"Без markdown-обёрток и пояснений.")

	sysMsg := "Ты ведёшь память задачи для диалогового ассистента с RAG. " +
		"Отслеживай ровно три вещи: цель диалога, что пользователь уже уточнил, " +
		"и какие ограничения/термины зафиксированы. Возвращай только валидный JSON."

	resp, err := client.Chat(ctx, port.LLMRequest{
		Model:       model,
		Messages:    []domain.Message{{Role: domain.RoleSystem, Content: sysMsg}, {Role: domain.RoleUser, Content: sb.String()}},
		MaxTokens:   maxTokens,
		Temperature: -1,
		Debug:       debug,
	})
	if err != nil {
		return existing, fmt.Errorf("task memory update failed: %w", err)
	}
	// A reasoning model can return empty visible content when the token budget is
	// spent on reasoning. Treat that as "nothing to merge" and keep prior memory
	// rather than reporting a parse error every turn.
	if strings.TrimSpace(resp.Content) == "" {
		return existing, nil
	}

	parsed, err := parseTaskMemoryJSON(resp.Content)
	if err != nil {
		return existing, fmt.Errorf("failed to parse task memory JSON: %w (response: %s)", err, resp.Content)
	}
	return domain.TaskMemory{
		Goal:        strings.TrimSpace(parsed.Goal),
		Clarified:   dedupeStrings(parsed.Clarified),
		Constraints: dedupeStrings(parsed.Constraints),
	}, nil
}

// parseTaskMemoryJSON decodes the model reply, tolerating code fences and any
// surrounding prose by narrowing to the outermost {...} object.
func parseTaskMemoryJSON(raw string) (taskMemoryJSON, error) {
	content := StripJSONFences(strings.TrimSpace(raw))
	if start := strings.Index(content, "{"); start >= 0 {
		if end := strings.LastIndex(content, "}"); end > start {
			content = content[start : end+1]
		}
	}
	var m taskMemoryJSON
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return taskMemoryJSON{}, err
	}
	return m, nil
}

// dedupeStrings trims, drops empties and removes case-insensitive duplicates
// while preserving order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
