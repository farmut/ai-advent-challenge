package usecase

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// fakeLLM is a stub port.LLMClient that returns a canned reply and records the
// prompt it was given.
type fakeLLM struct {
	reply     string
	err       error
	gotPrompt string
	called    bool
}

func (f *fakeLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	f.called = true
	if len(req.Messages) > 0 {
		f.gotPrompt = req.Messages[len(req.Messages)-1].Content
	}
	return port.LLMResponse{Content: f.reply}, f.err
}

func TestRAGUseCase_Answer_ParsesStructuredJSON(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "MVCC", ChunkID: 7, Content: "xmin и xmax хранят номера транзакций", Similarity: 0.9},
		{File: "pg.pdf", Section: "VACUUM", ChunkID: 12, Content: "очистка удаляет мёртвые версии", Similarity: 0.8},
	}}
	reply := "```json\n{\"answer\":\"xmin и xmax — это номера транзакций.\"," +
		"\"sources\":[1],\"quotes\":[{\"marker\":1,\"text\":\"xmin и xmax хранят номера транзакций\"}]}\n```"
	llm := &fakeLLM{reply: reply}
	uc := NewRAGUseCase(fr, nil)

	ans, res, err := uc.Answer(context.Background(), llm, "m", 500, "Что такое xmin?", RAGConfig{TopKRetrieve: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ans.Grounded {
		t.Fatal("expected grounded answer")
	}
	if !strings.Contains(ans.Answer, "xmin") {
		t.Errorf("answer missing content: %q", ans.Answer)
	}
	if len(ans.Sources) != 1 || ans.Sources[0].ChunkID != 7 || ans.Sources[0].Source != "pg.pdf" || ans.Sources[0].Section != "MVCC" {
		t.Errorf("unexpected sources: %+v", ans.Sources)
	}
	if len(ans.Quotes) != 1 || !strings.Contains(ans.Quotes[0].Text, "xmin и xmax") || ans.Quotes[0].ChunkID != 7 {
		t.Errorf("unexpected quotes: %+v", ans.Quotes)
	}
	if len(res.Final) != 2 {
		t.Errorf("expected 2 final chunks in pipeline result, got %d", len(res.Final))
	}
	// The prompt must ask for the structured JSON contract.
	if !strings.Contains(llm.gotPrompt, "quotes") || !strings.Contains(llm.gotPrompt, "sources") {
		t.Error("prompt did not request the structured answer format")
	}
}

func TestRAGUseCase_Answer_BelowThreshold_SaysDontKnow(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", Content: "нерелевантно", Similarity: 0.2},
	}}
	llm := &fakeLLM{reply: "should not be called"}
	uc := NewRAGUseCase(fr, nil)

	ans, res, err := uc.Answer(context.Background(), llm, "m", 500, "вопрос", RAGConfig{TopKRetrieve: 5, Threshold: 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llm.called {
		t.Error("LLM must NOT be called when nothing clears the threshold")
	}
	if ans.Grounded {
		t.Error("answer should not be grounded below threshold")
	}
	if !strings.Contains(strings.ToLower(ans.Answer), "не знаю") {
		t.Errorf("expected an 'I don't know' answer, got %q", ans.Answer)
	}
	if len(ans.Sources) != 0 || len(ans.Quotes) != 0 {
		t.Error("don't-know answer must carry no sources/quotes")
	}
	if len(res.Final) != 0 {
		t.Errorf("expected 0 final chunks, got %d", len(res.Final))
	}
}

func TestRAGUseCase_Answer_NonJSONReply_FallsBackToChunkCitations(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "WAL", ChunkID: 3, Content: "журнал предзаписи фиксирует изменения", Similarity: 0.9},
	}}
	llm := &fakeLLM{reply: "WAL — это журнал предзаписи."} // plain prose, no JSON
	uc := NewRAGUseCase(fr, nil)

	ans, _, err := uc.Answer(context.Background(), llm, "m", 500, "Что такое WAL?", RAGConfig{TopKRetrieve: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ans.Answer != "WAL — это журнал предзаписи." {
		t.Errorf("expected raw prose as answer, got %q", ans.Answer)
	}
	// Even without JSON, sources and quotes must be present (fallback to chunks).
	if len(ans.Sources) != 1 || ans.Sources[0].ChunkID != 3 {
		t.Errorf("fallback should cite the retrieved chunk, got %+v", ans.Sources)
	}
	if len(ans.Quotes) != 1 || ans.Quotes[0].Text == "" {
		t.Errorf("fallback should quote the retrieved chunk, got %+v", ans.Quotes)
	}
}

func TestRAGUseCase_Answer_SalvagesTruncatedJSON(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "MVCC", ChunkID: 7, Content: "xmin создаёт версию", Similarity: 0.9},
		{File: "pg.pdf", Section: "VACUUM", ChunkID: 9, Content: "vacuum удаляет мёртвые версии", Similarity: 0.8},
	}}
	// A reasoning model that ran out of budget: valid up to the second quote,
	// then cut off mid-array (no closing ] or }).
	truncated := `{"answer":"xmin — номер создавшей транзакции.","sources":[1,2],` +
		`"quotes":[{"marker":1,"text":"xmin создаёт версию"},{"marker":2,"text":"vacuum удаляет`
	llm := &fakeLLM{reply: truncated}
	uc := NewRAGUseCase(fr, nil)

	ans, _, err := uc.Answer(context.Background(), llm, "m", 500, "Что такое xmin?", RAGConfig{TopKRetrieve: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Clean prose answer, not the raw JSON.
	if ans.Answer != "xmin — номер создавшей транзакции." {
		t.Errorf("salvage should recover the clean answer, got %q", ans.Answer)
	}
	if len(ans.Sources) != 2 {
		t.Errorf("salvage should recover both sources, got %+v", ans.Sources)
	}
	// The first quote is complete and must be salvaged; the truncated second one dropped.
	if len(ans.Quotes) != 1 || ans.Quotes[0].ChunkID != 7 || !strings.Contains(ans.Quotes[0].Text, "xmin создаёт") {
		t.Errorf("salvage should recover the one complete quote, got %+v", ans.Quotes)
	}
}

// The support chat's whole value is the link back to the source page, and the
// link must come from the index rather than from the model's text — a URL the
// model typed could be invented. These tests pin that path end to end.

func TestRAGUseCase_Answer_URLReachesSourcesAndQuotes(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{
			File: "Возврат товара", Section: "Сроки", ChunkID: 0,
			Title:   "Возврат товара",
			URL:     "https://wiki.example.com/support/return",
			Content: "Вернуть заказ можно в течение 14 дней.", Similarity: 0.9,
		},
	}}
	reply := `{"answer":"Вернуть можно за 14 дней.","sources":[1],` +
		`"quotes":[{"marker":1,"text":"Вернуть заказ можно в течение 14 дней."}]}`
	uc := NewRAGUseCase(fr, nil)

	ans, _, err := uc.Answer(context.Background(), &fakeLLM{reply: reply}, "m", 500, "Как вернуть товар?", RAGConfig{TopKRetrieve: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "https://wiki.example.com/support/return"
	if len(ans.Sources) != 1 || ans.Sources[0].URL != want {
		t.Errorf("source URL = %+v, want %q", ans.Sources, want)
	}
	if len(ans.Quotes) != 1 || ans.Quotes[0].URL != want {
		t.Errorf("quote URL = %+v, want %q", ans.Quotes, want)
	}
}

// When the model returns prose instead of JSON the pipeline falls back to citing
// every chunk. The URL must survive that path too — it is exactly the case where
// the model gave us no citation of its own.
func TestRAGUseCase_Answer_URLSurvivesProseFallback(t *testing.T) {
	fr := &fakeRetriever{chunks: []domain.RetrievedChunk{
		{
			File: "Гарантия", Section: "Срок", ChunkID: 2,
			URL:     "https://wiki.example.com/support/warranty",
			Content: "Гарантия действует 12 месяцев.", Similarity: 0.88,
		},
	}}
	uc := NewRAGUseCase(fr, nil)

	ans, _, err := uc.Answer(context.Background(),
		&fakeLLM{reply: "Гарантия — 12 месяцев."}, // plain prose, no JSON
		"m", 500, "Какая гарантия?", RAGConfig{TopKRetrieve: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "https://wiki.example.com/support/warranty"
	if len(ans.Sources) != 1 || ans.Sources[0].URL != want {
		t.Errorf("fallback source URL = %+v, want %q", ans.Sources, want)
	}
	if len(ans.Quotes) != 1 || ans.Quotes[0].URL != want {
		t.Errorf("fallback quote URL = %+v, want %q", ans.Quotes, want)
	}
}

// An index without a url column (docs.db, shop.db) must stay usable: no error,
// no invented link, and — critically — no empty "URL:" line in the prompt that
// would invite the model to fill one in.
func TestBuildAnswerPrompt_OmitsEmptyURL(t *testing.T) {
	withURL := BuildAnswerPrompt("вопрос?", []domain.RetrievedChunk{
		{File: "page.md", ChunkID: 0, Content: "текст", URL: "https://wiki.example.com/p"},
	})
	if !strings.Contains(withURL, "URL: https://wiki.example.com/p") {
		t.Errorf("prompt must carry the chunk URL:\n%s", withURL)
	}
	if !strings.Contains(withURL, "как есть") {
		t.Errorf("prompt must instruct the model to reproduce URLs verbatim:\n%s", withURL)
	}

	noURL := BuildAnswerPrompt("вопрос?", []domain.RetrievedChunk{
		{File: "page.md", ChunkID: 0, Content: "текст"},
	})
	if strings.Contains(noURL, "URL:") {
		t.Errorf("prompt must not emit a URL line for a chunk without one:\n%s", noURL)
	}
}

func TestBuildAnswerPrompt_IncludesChunkMetadataAndContract(t *testing.T) {
	chunks := []domain.RetrievedChunk{
		{File: "pg.pdf", Section: "MVCC", ChunkID: 7, Content: "текст чанка", Similarity: 0.9},
	}
	p := BuildAnswerPrompt("вопрос?", chunks)
	for _, want := range []string{"[1] источник: pg.pdf — MVCC (chunk_id=7)", "текст чанка", "вопрос?", "\"answer\"", "\"sources\"", "\"quotes\""} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, p)
		}
	}
}
