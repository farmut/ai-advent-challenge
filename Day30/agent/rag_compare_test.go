//go:build integration

// Integration test that runs the 10 control questions twice — once WITH RAG
// (question → retrieve → combine → LLM) and once WITHOUT RAG (question → LLM) —
// and writes a side-by-side comparison report. Both modes need the LLM; the RAG
// mode additionally needs the embeddings endpoint.
//
// Run:
//
//	RUN_LLM=1 LLM_PROVIDER=openrouter LLM_MODEL=deepseek/deepseek-v4-flash \
//	  go test -tags integration -run TestRAGCompare -v -timeout 600s .
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

const compareReport = "rag_compare_result.txt"

// askLLM sends a single-turn prompt and returns the answer text. A generous
// token budget lets reasoning models finish thinking and still emit an answer.
func askLLM(ctx context.Context, client port.LLMClient, model, prompt string) (string, error) {
	resp, err := client.Chat(ctx, port.LLMRequest{
		Model:     model,
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: prompt}},
		MaxTokens: 1500,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// coverage returns how many of the expected keywords appear in the answer.
func coverage(answer string, keywords []string) int {
	return len(keywords) - len(missingKeywords(strings.ToLower(answer), keywords))
}

func hasCitation(answer string) bool {
	return strings.Contains(answer, "[1]") || strings.Contains(answer, "[2]") ||
		strings.Contains(answer, "[3]") || strings.Contains(answer, "[4]") ||
		strings.Contains(answer, "[5]")
}

func TestRAGCompare(t *testing.T) {
	set := loadEvalSet(t)

	if os.Getenv("RUN_LLM") != "1" {
		t.Skip("set RUN_LLM=1 (plus LLM_PROVIDER/LLM_API_KEY) to run the RAG-vs-no-RAG comparison")
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if _, err := retriever.Retrieve(ctx, "ping", 1); err != nil {
		t.Skipf("embeddings endpoint %s unreachable (%v) — skipping", set.EmbedURL, err)
	}

	llmClient, llmModel := buildLLMFromEnv(t) // t.Skip inside if creds missing

	f, err := os.Create(compareReport)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	defer f.Close()
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		fmt.Fprintln(f, line)
	}

	log("==========================================")
	log("  Сравнение: с RAG vs без RAG")
	log("  10 контрольных вопросов")
	log("  Index: %s", set.DB)
	log("  Embed: %s (%s)", set.EmbedURL, set.EmbedModel)
	log("  LLM:   %s   top-K: %d", llmModel, set.TopK)
	log("  Date:  %s", time.Now().Format("2006-01-02 15:04:05"))
	log("==========================================")
	log("")

	// per-question results
	type row struct {
		id                   int
		total                int
		hitWithout, hitWith  int
		citedWith            bool
		emptyWithout, emptyWith bool
	}
	var rows []row
	var sumWithout, sumWith, sumTotal int
	var ragBetter, tie, ragWorse, citedCount int

	for _, q := range set.Questions {
		log("=== Q%d: %s ===", q.ID, q.Question)
		log("Ожидаемые термины: %v", q.AnswerKeywords)

		// --- WITHOUT RAG: raw question ---
		ansWithout, err := askLLM(ctx, llmClient, llmModel, q.Question)
		if err != nil {
			t.Errorf("Q%d without-RAG: %v", q.ID, err)
			log("  [без RAG] ОШИБКА: %v", err)
			log("")
			continue
		}

		// --- WITH RAG: retrieve → combine → LLM ---
		chunks, err := retriever.Retrieve(ctx, q.Question, set.TopK)
		if err != nil {
			t.Errorf("Q%d retrieve: %v", q.ID, err)
			log("  [с RAG] ОШИБКА извлечения: %v", err)
			log("")
			continue
		}
		ansWith, err := askLLM(ctx, llmClient, llmModel, usecase.BuildRAGPrompt(q.Question, chunks))
		if err != nil {
			t.Errorf("Q%d with-RAG: %v", q.ID, err)
			log("  [с RAG] ОШИБКА: %v", err)
			log("")
			continue
		}

		hw := coverage(ansWithout, q.AnswerKeywords)
		hr := coverage(ansWith, q.AnswerKeywords)
		cited := hasCitation(ansWith)
		r := row{
			id: q.ID, total: len(q.AnswerKeywords),
			hitWithout: hw, hitWith: hr, citedWith: cited,
			emptyWithout: strings.TrimSpace(ansWithout) == "",
			emptyWith:    strings.TrimSpace(ansWith) == "",
		}
		rows = append(rows, r)
		sumWithout += hw
		sumWith += hr
		sumTotal += len(q.AnswerKeywords)
		if cited {
			citedCount++
		}
		switch {
		case hr > hw:
			ragBetter++
		case hr == hw:
			tie++
		default:
			ragWorse++
		}

		winner := "="
		switch {
		case hr > hw:
			winner = "RAG ✓"
		case hw > hr:
			winner = "без RAG ✓"
		}
		log("  [без RAG] термины %d/%d, источники: нет            :: %s", hw, len(q.AnswerKeywords), snippet(ansWithout, 160))
		log("  [с RAG ]  термины %d/%d, цитирование: %v :: %s", hr, len(q.AnswerKeywords), cited, snippet(ansWith, 160))
		log("  → победитель: %s", winner)
		log("")
	}

	// --- Summary table ---
	log("==========================================")
	log("  ИТОГОВАЯ ТАБЛИЦА")
	log("==========================================")
	header := fmt.Sprintf("%-4s  %-14s  %-14s  %-10s  %s", "Q", "без RAG", "с RAG", "цитаты", "победитель")
	log("%s", header)
	log("%s", strings.Repeat("-", len(header)))
	for _, r := range rows {
		winner := "="
		switch {
		case r.hitWith > r.hitWithout:
			winner = "RAG"
		case r.hitWithout > r.hitWith:
			winner = "без RAG"
		}
		wo := fmt.Sprintf("%d/%d", r.hitWithout, r.total)
		if r.emptyWithout {
			wo += " (пусто)"
		}
		wr := fmt.Sprintf("%d/%d", r.hitWith, r.total)
		if r.emptyWith {
			wr += " (пусто)"
		}
		log("%-4d  %-14s  %-14s  %-10v  %s", r.id, wo, wr, r.citedWith, winner)
	}
	log("%s", strings.Repeat("-", len(header)))
	log("")

	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b) * 100
	}
	log("Полнота по ключевым терминам:")
	log("  без RAG: %d/%d (%.1f%%)", sumWithout, sumTotal, pct(sumWithout, sumTotal))
	log("  с RAG:   %d/%d (%.1f%%)", sumWith, sumTotal, pct(sumWith, sumTotal))
	log("")
	log("Победы по вопросам: RAG лучше — %d, одинаково — %d, RAG хуже — %d (из %d)",
		ragBetter, tie, ragWorse, len(rows))
	log("Ответы с ссылками на источники (только режим RAG): %d/%d", citedCount, len(rows))
	log("")
	switch {
	case sumWith > sumWithout:
		log("Вывод: RAG повышает полноту ответов и добавляет ссылки на источники")
		log("       из базы знаний (grounding). Модель опирается на конкретный")
		log("       документ, а не только на память.")
	case sumWith == sumWithout:
		log("Вывод: по полноте терминов режимы сопоставимы, но RAG добавляет")
		log("       прослеживаемость (ссылки на источники) и опору на факты базы.")
	default:
		log("Вывод: на этом наборе базовая модель уже хорошо знает тему; выигрыш")
		log("       RAG — прежде всего прослеживаемость и опора на конкретный источник.")
	}
	log("")
	log("Отчёт: %s", compareReport)

	if len(rows) != len(set.Questions) {
		t.Errorf("comparison completed only %d/%d questions (see errors above)", len(rows), len(set.Questions))
	}
}
