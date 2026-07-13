//go:build integration

// Integration test for the agent's RAG pipeline: it runs a mini-benchmark of 10
// control questions against the knowledge base and checks, for each question,
// that the expected sources are retrieved (deterministic given index + embed
// endpoint). Optionally (RUN_LLM=1) it also runs the full
// question → retrieve → combine → LLM pipeline and soft-checks the answer.
//
// Run:
//
//	# retrieval only (needs the embeddings endpoint on 127.0.0.1:1234)
//	go test -tags integration -run TestRAGEval -v .
//
//	# full pipeline incl. LLM answer (needs LLM_PROVIDER / LLM_API_KEY too)
//	RUN_LLM=1 LLM_PROVIDER=openrouter LLM_MODEL=deepseek/deepseek-v4-flash \
//	  go test -tags integration -run TestRAGEval -v .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/llm"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// ---- eval set schema (mirrors eval_questions.json) -------------------------

type evalSource struct {
	File    string   `json:"file"`
	Section string   `json:"section"`
	Anchors []string `json:"anchors"`
}

type evalQuestion struct {
	ID             int          `json:"id"`
	Question       string       `json:"question"`
	Expectation    string       `json:"expectation"`
	AnswerKeywords []string     `json:"answer_keywords"`
	Sources        []evalSource `json:"sources"`
}

type evalSet struct {
	DB         string         `json:"db"`
	EmbedURL   string         `json:"embed_url"`
	EmbedModel string         `json:"embed_model"`
	TopK       int            `json:"top_k"`
	Questions  []evalQuestion `json:"questions"`
}

const evalReport = "rag_eval_result.txt"

func loadEvalSet(t *testing.T) evalSet {
	t.Helper()
	raw, err := os.ReadFile("eval_questions.json")
	if err != nil {
		t.Fatalf("read eval_questions.json: %v", err)
	}
	var set evalSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("parse eval_questions.json: %v", err)
	}
	if len(set.Questions) != 10 {
		t.Fatalf("expected 10 control questions, got %d", len(set.Questions))
	}
	// RAG_MAX_QUESTIONS truncates the set (after the integrity check above) to run
	// a faster subset — useful with slow local models. All downstream assertions
	// use len(set.Questions), so they scale to the reduced set automatically.
	if v := os.Getenv("RAG_MAX_QUESTIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < len(set.Questions) {
			set.Questions = set.Questions[:n]
		}
	}
	// Env overrides so the same set can run against another index/endpoint.
	if v := os.Getenv("RAG_DB"); v != "" {
		set.DB = v
	}
	if v := os.Getenv("EMBED_URL"); v != "" {
		set.EmbedURL = v
	}
	if v := os.Getenv("EMBED_MODEL"); v != "" {
		set.EmbedModel = v
	}
	if set.TopK <= 0 {
		set.TopK = 5
	}
	return set
}

func TestRAGEval(t *testing.T) {
	set := loadEvalSet(t)

	if _, err := os.Stat(set.DB); err != nil {
		t.Skipf("index %s not found — build it with the `rag index` command; skipping", set.DB)
	}

	retriever, err := ragadapter.NewRetriever(ragadapter.Config{
		DBPath:     set.DB,
		EmbedURL:   set.EmbedURL,
		EmbedModel: set.EmbedModel,
	})
	if err != nil {
		t.Skipf("cannot open retriever (%v) — skipping", err)
	}
	defer retriever.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Probe: if the embeddings endpoint is unreachable, skip rather than fail —
	// this is an integration test that needs a live embeddings server.
	if _, err := retriever.Retrieve(ctx, "ping", 1); err != nil {
		t.Skipf("embeddings endpoint %s unreachable (%v) — skipping", set.EmbedURL, err)
	}

	f, err := os.Create(evalReport)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	defer f.Close()
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		fmt.Fprintln(f, line)
	}

	// Optional LLM stage.
	runLLM := os.Getenv("RUN_LLM") == "1"
	var llmClient port.LLMClient
	var llmModel string
	if runLLM {
		llmClient, llmModel = buildLLMFromEnv(t)
	}

	log("==========================================")
	log("  RAG Eval — 10 контрольных вопросов")
	log("  Index: %s", set.DB)
	log("  Embed: %s (%s)", set.EmbedURL, set.EmbedModel)
	log("  top-K: %d   LLM stage: %v", set.TopK, runLLM)
	log("  Date:  %s", time.Now().Format("2006-01-02 15:04:05"))
	log("==========================================")
	log("")

	var retrievalPass, answerPass int

	for _, q := range set.Questions {
		log("=== Q%d: %s ===", q.ID, q.Question)
		log("Ожидание: %s", q.Expectation)

		chunks, err := retriever.Retrieve(ctx, q.Question, set.TopK)
		if err != nil {
			t.Errorf("Q%d retrieve: %v", q.ID, err)
			log("  ОШИБКА извлечения: %v", err)
			log("")
			continue
		}

		// One lowercased blob of all retrieved content for anchor checks.
		var blob strings.Builder
		log("Извлечено %d чанк(ов):", len(chunks))
		for i, c := range chunks {
			blob.WriteString(strings.ToLower(c.Content))
			blob.WriteString("\n")
			log("  %d. %s chunk#%d sim=%.4f :: %s",
				i+1, c.File, c.ChunkID, c.Similarity, snippet(c.Content, 90))
		}
		hay := blob.String()

		// Hard check: every expected source must be represented by at least one
		// of its anchors appearing in the retrieved passages.
		retrievalOK := true
		for _, src := range q.Sources {
			found := ""
			for _, a := range src.Anchors {
				if strings.Contains(hay, strings.ToLower(a)) {
					found = a
					break
				}
			}
			if found == "" {
				retrievalOK = false
				t.Errorf("Q%d: expected source %q (%s) not retrieved — none of anchors %v found in top-%d",
					q.ID, src.File, src.Section, src.Anchors, set.TopK)
				log("  ✗ источник НЕ найден: %s [%s] — нет ни одного из %v", src.File, src.Section, src.Anchors)
			} else {
				log("  ✓ источник найден: %s [%s] (anchor %q)", src.File, src.Section, found)
			}
		}
		if retrievalOK {
			retrievalPass++
		}

		// Optional: full pipeline — combine with the question and ask the LLM.
		if runLLM {
			prompt := usecase.BuildRAGPrompt(q.Question, chunks)
			resp, err := llmClient.Chat(ctx, port.LLMRequest{
				Model:    llmModel,
				Messages: []domain.Message{{Role: domain.RoleUser, Content: prompt}},
				// Generous budget so reasoning models have room to finish
				// thinking and still emit a final answer in `content`.
				MaxTokens: 1500,
			})
			if err != nil {
				log("  LLM: ОШИБКА: %v", err)
			} else {
				ans := strings.ToLower(resp.Content)
				missing := missingKeywords(ans, q.AnswerKeywords)
				if len(missing) == 0 {
					answerPass++
					log("  ✓ ответ содержит ожидаемые термины %v", q.AnswerKeywords)
				} else {
					// Soft check: log but don't fail — LLM phrasing varies.
					log("  ~ ответ не содержит термины %v (это мягкая проверка)", missing)
				}
				log("  Ответ: %s", snippet(resp.Content, 400))
			}
		}
		log("")
	}

	log("==========================================")
	log("  ИТОГ: извлечение источников %d/%d", retrievalPass, len(set.Questions))
	if runLLM {
		log("        ответы с ожидаемыми терминами %d/%d (мягкая проверка)", answerPass, len(set.Questions))
	}
	log("  Отчёт: %s", evalReport)
	log("==========================================")

	if retrievalPass != len(set.Questions) {
		t.Errorf("retrieval passed only %d/%d control questions", retrievalPass, len(set.Questions))
	}
}

// buildLLMFromEnv wires an LLM client from the standard env interface, mirroring
// main.go's resolution. Skips the whole test when credentials are missing.
func buildLLMFromEnv(t *testing.T) (port.LLMClient, string) {
	t.Helper()
	provider := os.Getenv("LLM_PROVIDER")
	apiKey := os.Getenv("LLM_API_KEY")
	if provider == "" || apiKey == "" {
		t.Skip("RUN_LLM=1 but LLM_PROVIDER/LLM_API_KEY not set — skipping LLM stage")
	}
	model := os.Getenv("LLM_MODEL")
	baseURL := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if baseURL == "" {
		switch provider {
		case llm.ProviderOpenRouter:
			baseURL = "https://openrouter.ai/api/v1"
		case llm.ProviderGigaChat:
			baseURL = llm.DefaultGigaChatBaseURL
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}
	if provider == llm.ProviderOpenAI && model == "" {
		model = "gpt-4o"
	}
	if provider == llm.ProviderGigaChat && model == "" {
		model = "GigaChat"
	}
	return llm.NewClient(llm.Config{
		Provider:      provider,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		GigaChatScope: os.Getenv("GIGACHAT_SCOPE"),
	}), model
}

func missingKeywords(hayLower string, keywords []string) []string {
	var missing []string
	for _, k := range keywords {
		if !strings.Contains(hayLower, strings.ToLower(k)) {
			missing = append(missing, k)
		}
	}
	return missing
}

func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
