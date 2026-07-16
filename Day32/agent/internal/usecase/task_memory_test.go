package usecase

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// recordingLLM captures every request it receives so tests can assert on the
// full message list (system directive + history + grounded prompt).
type recordingLLM struct {
	reply string
	reqs  []port.LLMRequest
}

func (r *recordingLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	r.reqs = append(r.reqs, req)
	return port.LLMResponse{Content: r.reply}, nil
}

func TestTaskMemorySystemBlock(t *testing.T) {
	if got := TaskMemorySystemBlock(domain.TaskMemory{}); got != "" {
		t.Fatalf("empty memory should render empty block, got %q", got)
	}
	tm := domain.TaskMemory{
		Goal:        "разобраться с MVCC в PostgreSQL",
		Clarified:   []string{"версия 15", "интересует только heap"},
		Constraints: []string{"xmin — id транзакции-создателя"},
	}
	got := TaskMemorySystemBlock(tm)
	for _, want := range []string{
		"Цель диалога: разобраться с MVCC в PostgreSQL",
		"Пользователь уже уточнил:",
		"- версия 15",
		"- интересует только heap",
		"Зафиксированные ограничения и термины:",
		"- xmin — id транзакции-создателя",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q\n---\n%s", want, got)
		}
	}
}

func TestUpdateTaskMemory_ParsesAndDedupes(t *testing.T) {
	llm := &recordingLLM{reply: "```json\n{\"goal\":\"понять VACUUM\"," +
		"\"clarified\":[\"версия 15\",\"версия 15\",\" \"]," +
		"\"constraints\":[\"autovacuum включён\"]}\n```"}
	existing := domain.TaskMemory{Goal: "старая цель"}

	got, err := UpdateTaskMemory(context.Background(), llm, "m", 0, existing, "как работает vacuum?", "VACUUM удаляет мёртвые строки", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Goal != "понять VACUUM" {
		t.Errorf("goal = %q", got.Goal)
	}
	if len(got.Clarified) != 1 || got.Clarified[0] != "версия 15" {
		t.Errorf("clarified not deduped/cleaned: %+v", got.Clarified)
	}
	if len(got.Constraints) != 1 || got.Constraints[0] != "autovacuum включён" {
		t.Errorf("constraints = %+v", got.Constraints)
	}
}

func TestUpdateTaskMemory_BadJSON_KeepsExisting(t *testing.T) {
	llm := &recordingLLM{reply: "sorry, I cannot do that"}
	existing := domain.TaskMemory{Goal: "цель", Clarified: []string{"a"}}

	got, err := UpdateTaskMemory(context.Background(), llm, "m", 0, existing, "u", "a", false)
	if err == nil {
		t.Fatal("expected an error on unparseable JSON")
	}
	if got.Goal != "цель" || len(got.Clarified) != 1 {
		t.Errorf("existing memory should be preserved on failure, got %+v", got)
	}
}

func TestUpdateTaskMemory_EmptyContent_KeepsExistingNoError(t *testing.T) {
	// A reasoning model can burn the whole budget on reasoning and return empty
	// visible content — this must not error, just keep the prior memory.
	llm := &recordingLLM{reply: "   "}
	existing := domain.TaskMemory{Goal: "цель", Constraints: []string{"версия 15"}}

	got, err := UpdateTaskMemory(context.Background(), llm, "m", 0, existing, "u", "a", false)
	if err != nil {
		t.Fatalf("empty content should not error, got %v", err)
	}
	if got.Goal != "цель" || len(got.Constraints) != 1 {
		t.Errorf("existing memory should be preserved on empty content, got %+v", got)
	}
}

func TestAnswerWithContext_InjectsMemoryHistoryAndAnchorsRetrieval(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "MVCC", ChunkID: 3, Content: "xmin хранит id транзакции", Similarity: 0.9},
	}}
	llm := &recordingLLM{reply: `{"answer":"ответ","sources":[1],"quotes":[{"marker":1,"text":"xmin хранит id транзакции"}]}`}
	uc := NewRAGUseCase(fr, nil)

	conv := ConversationContext{
		History: []domain.Message{
			{Role: domain.RoleUser, Content: "что такое xmin?"},
			{Role: domain.RoleAssistant, Content: "это номер транзакции"},
		},
		TaskMemory: domain.TaskMemory{Goal: "разобраться с MVCC", Constraints: []string{"версия 15"}},
	}

	ans, _, err := uc.AnswerWithContext(context.Background(), llm, "m", 500, "а xmax?", RAGConfig{TopKRetrieve: 5}, conv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ans.Grounded {
		t.Fatal("expected grounded answer")
	}

	// Retrieval query must be anchored to the dialogue goal so a terse follow-up
	// still fetches on-topic context.
	if !strings.Contains(fr.gotQuery, "разобраться с MVCC") || !strings.Contains(fr.gotQuery, "а xmax?") {
		t.Errorf("retrieval query not anchored to goal: %q", fr.gotQuery)
	}

	// The LLM request must carry: a system task-memory directive, the replayed
	// history, and the grounded prompt as the final user message.
	if len(llm.reqs) != 1 {
		t.Fatalf("expected 1 llm call, got %d", len(llm.reqs))
	}
	msgs := llm.reqs[0].Messages
	if len(msgs) < 4 {
		t.Fatalf("expected system + 2 history + grounded prompt, got %d messages", len(msgs))
	}
	if msgs[0].Role != domain.RoleSystem || !strings.Contains(msgs[0].Content, "Память задачи") {
		t.Errorf("first message should be the task-memory system directive, got %+v", msgs[0])
	}
	if msgs[1].Content != "что такое xmin?" || msgs[2].Content != "это номер транзакции" {
		t.Errorf("history not replayed in order: %+v", msgs[1:3])
	}
	last := msgs[len(msgs)-1]
	if last.Role != domain.RoleUser || !strings.Contains(last.Content, "а xmax?") {
		t.Errorf("grounded prompt should be the final user message, got %+v", last)
	}
}

func TestAnswer_StatelessUsesSingleUserMessage(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", ChunkID: 1, Content: "текст", Similarity: 0.9},
	}}
	llm := &recordingLLM{reply: `{"answer":"ok","sources":[1],"quotes":[{"marker":1,"text":"текст"}]}`}
	uc := NewRAGUseCase(fr, nil)

	if _, _, err := uc.Answer(context.Background(), llm, "m", 500, "вопрос", RAGConfig{TopKRetrieve: 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Backward compatibility: no context → exactly one user message, no system prefix.
	if len(llm.reqs) != 1 || len(llm.reqs[0].Messages) != 1 {
		t.Fatalf("stateless Answer should send exactly one message, got %d", len(llm.reqs[0].Messages))
	}
	if llm.reqs[0].Messages[0].Role != domain.RoleUser {
		t.Errorf("stateless message should be a user turn, got %q", llm.reqs[0].Messages[0].Role)
	}
}
