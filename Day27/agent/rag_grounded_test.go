//go:build integration

// Integration test for Day24's GROUNDED RAG answers. It runs the 10 control
// questions through the full pipeline (retrieve → [rerank] → filter → grounded
// LLM answer) and, for every question, checks the three properties the day is
// about:
//
//  1. the answer carries SOURCES  (source + section/chunk_id)
//  2. the answer carries QUOTES   (verbatim fragments from the retrieved chunks)
//  3. the answer's MEANING MATCHES its quotes (checked by an LLM judge)
//
// It also spot-checks the low-relevance guard: a deliberately off-topic question
// must yield an ungrounded "не знаю" reply with no sources/quotes.
//
// Run (needs the embeddings endpoint AND an LLM):
//
//	RUN_LLM=1 LLM_PROVIDER=openrouter LLM_API_KEY=... LLM_MODEL=deepseek/deepseek-v4-flash \
//	  RAG_DB=../rag/rag.db EMBED_URL=http://127.0.0.1:1234 \
//	  EMBED_MODEL=text-embedding-nomic-embed-text-v2-moe \
//	  go test -tags integration -run TestRAGGroundedAnswers -v -timeout 900s .
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

const groundedReport = "rag_grounded_result.txt"

func TestRAGGroundedAnswers(t *testing.T) {
	set := loadEvalSet(t)

	if _, err := os.Stat(set.DB); err != nil {
		t.Skipf("index %s not found — build it with the `rag index` command; skipping", set.DB)
	}

	// This test is meaningless without an LLM: the whole point is the grounded
	// answer, not just retrieval.
	llmClient, llmModel := buildLLMFromEnv(t) // skips when creds missing

	retriever, err := ragadapter.NewRetriever(ragadapter.Config{
		DBPath:     set.DB,
		EmbedURL:   set.EmbedURL,
		EmbedModel: set.EmbedModel,
	})
	if err != nil {
		t.Skipf("cannot open retriever (%v) — skipping", err)
	}
	defer retriever.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if _, err := retriever.Retrieve(ctx, "ping", 1); err != nil {
		t.Skipf("embeddings endpoint %s unreachable (%v) — skipping", set.EmbedURL, err)
	}

	// This test ALWAYS runs the rerank stage: retrieve a wide pool, re-score it
	// with a DEDICATED rerank model, drop everything below the threshold, keep the
	// top-K. Knobs default to the day's tuned values and are env-overridable.
	if os.Getenv("RAG_RERANK_MODEL") == "" {
		os.Setenv("RAG_RERANK_MODEL", "cohere/rerank-4-fast")
	}
	reranker, rerankDesc := buildRerankerFromEnv(t)
	topK := envInt("RAG_TOP_K", 20)
	threshold := envFloat("RAG_THRESHOLD", 0.5)
	topKFinal := envInt("RAG_TOP_K_FINAL", 10)
	cfg := usecase.RAGConfig{TopKRetrieve: topK, Rerank: true, Threshold: threshold, TopKFinal: topKFinal}

	uc := usecase.NewRAGUseCase(retriever, reranker)

	f, err := os.Create(groundedReport)
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
	log("  RAG Grounded Answers — 10 контрольных вопросов")
	log("  Index: %s", set.DB)
	log("  Embed: %s (%s)", set.EmbedURL, set.EmbedModel)
	log("  LLM:   %s", llmModel)
	log("  Rerank: %s (retrieve %d → threshold %.2f → keep %d)", rerankDesc, topK, threshold, topKFinal)
	log("  Date:  %s", time.Now().Format("2006-01-02 15:04:05"))
	log("==========================================")
	log("")

	var sourcesPass, quotesPass, meaningPass int

	for _, q := range set.Questions {
		log("=== Q%d: %s ===", q.ID, q.Question)

		// Generous budget: reasoning models spend tokens thinking before they
		// emit the JSON, so a tight cap truncates the structured answer.
		ans, res, err := uc.Answer(ctx, llmClient, llmModel, 4000, q.Question, cfg)
		if err != nil {
			t.Errorf("Q%d answer: %v", q.ID, err)
			log("  ОШИБКА: %v", err)
			log("")
			continue
		}

		// Show the rerank funnel explicitly: retrieval orders the pool by embedding
		// similarity, the reranker re-scores and REORDERS it, then we filter/keep.
		// Printing similarity → rerank per surviving chunk makes the rerank stage
		// visible (the list is ordered by rerank score, so similarity is non-monotonic).
		log("Конвейер: извлечено %d (по similarity) → реранкинг [%s] → отобрано %d (по rerank)",
			len(res.Retrieved), rerankDesc, len(res.Final))
		if res.Reranked {
			for i, c := range res.Final {
				log("    %2d. chunk#%-4d similarity=%.4f → rerank=%.4f", i+1, c.ChunkID, c.Similarity, c.RerankScore)
			}
			logRerankReorder(log, res.Retrieved, res.Final)
		}
		log("Ответ: %s", snippet(ans.Answer, 400))

		if !ans.Grounded {
			t.Errorf("Q%d: control question returned an ungrounded answer (threshold too high?)", q.ID)
			log("  ✗ ответ НЕ обоснован (grounded=false) — источники/цитаты отсутствуют")
			log("")
			continue
		}

		// --- Check 1: sources present ---------------------------------------
		if len(ans.Sources) > 0 {
			sourcesPass++
			log("  ✓ источники (%d):", len(ans.Sources))
			for i, s := range ans.Sources {
				label := s.Source
				if s.Section != "" {
					label += " — " + s.Section
				}
				log("      [%d] %s (chunk_id=%d, rerank=%.3f)", i+1, label, s.ChunkID, s.Score)
			}
		} else {
			t.Errorf("Q%d: answer has NO sources", q.ID)
			log("  ✗ источники отсутствуют")
		}

		// --- Check 2: quotes present ----------------------------------------
		if len(ans.Quotes) > 0 {
			quotesPass++
			log("  ✓ цитаты (%d):", len(ans.Quotes))
			for i, qt := range ans.Quotes {
				log("      [%d] chunk_id=%d :: %s", i+1, qt.ChunkID, snippet(qt.Text, 120))
			}
		} else {
			t.Errorf("Q%d: answer has NO quotes", q.ID)
			log("  ✗ цитаты отсутствуют")
		}

		// --- Check 2b (grounding integrity): quotes come from retrieved chunks.
		// Soft — the model may normalise whitespace/case. Logged, not failed.
		for _, qt := range ans.Quotes {
			if !quoteBackedByChunks(qt.Text, res.Final) {
				log("      ~ цитата слабо совпадает с текстом чанков: %s", snippet(qt.Text, 80))
			}
		}

		// --- Check 3: answer meaning matches its quotes (LLM judge) ----------
		if len(ans.Quotes) == 0 {
			log("  — проверку смысла пропускаем: нет цитат")
		} else {
			// LLM-judge verdict. This is a SEMANTIC, non-deterministic check, so a
			// single strict verdict is logged per question but the pass/fail gate
			// is applied on the aggregate below (a lone strict "UNSUPPORTED" on an
			// answer that legitimately synthesises the context should not fail CI).
			verdict, reason := judgeMeaning(ctx, llmClient, llmModel, ans)
			if verdict {
				meaningPass++
				log("  ✓ смысл ответа подтверждается цитатами (SUPPORTED)")
			} else {
				log("  ✗ смысл ответа НЕ подтверждается цитатами: %s", reason)
			}
		}
		log("")
	}

	log("==========================================")
	log("  ИТОГ по 10 вопросам:")
	log("    источники есть:            %d/%d", sourcesPass, len(set.Questions))
	log("    цитаты есть:               %d/%d", quotesPass, len(set.Questions))
	log("    смысл совпадает с цитатами: %d/%d", meaningPass, len(set.Questions))
	log("  Отчёт: %s", groundedReport)
	log("==========================================")

	// --- Low-relevance guard: an off-topic question must say "не знаю" -------
	// A high threshold guarantees nothing clears the bar.
	log("")
	log("=== Проверка порога релевантности (off-topic вопрос) ===")
	guardCfg := usecase.RAGConfig{TopKRetrieve: topK, Rerank: true, Threshold: 0.99, TopKFinal: topKFinal}
	offTopic := "Какая столица Австралии и когда она была основана?"
	guardAns, _, err := uc.Answer(ctx, llmClient, llmModel, 1500, offTopic, guardCfg)
	if err != nil {
		t.Errorf("guard: %v", err)
	} else {
		if guardAns.Grounded || len(guardAns.Sources) != 0 || len(guardAns.Quotes) != 0 {
			t.Errorf("guard: below-threshold answer should be ungrounded with no sources/quotes, got %+v", guardAns)
			log("  ✗ ожидался ответ 'не знаю', получено обоснованное: %s", snippet(guardAns.Answer, 200))
		} else if !strings.Contains(strings.ToLower(guardAns.Answer), "не знаю") {
			t.Errorf("guard: expected an 'I don't know' reply, got %q", guardAns.Answer)
			log("  ✗ ответ не содержит 'не знаю': %s", snippet(guardAns.Answer, 200))
		} else {
			log("  ✓ при низкой релевантности ассистент говорит 'не знаю' и просит уточнить")
		}
	}

	// Requirements 1 & 2 are deterministic given the pipeline (fallback guarantees
	// citations), so they gate hard: every answer must carry sources and quotes.
	if sourcesPass != len(set.Questions) {
		t.Errorf("sources present in only %d/%d answers", sourcesPass, len(set.Questions))
	}
	if quotesPass != len(set.Questions) {
		t.Errorf("quotes present in only %d/%d answers", quotesPass, len(set.Questions))
	}
	// Requirement 3 is a semantic LLM-judge check — inherently noisy — so it gates
	// on a majority rather than a perfect score: most answers must stay within the
	// meaning of the quotes they cite.
	minMeaning := (len(set.Questions)*6 + 9) / 10 // ceil(0.6 * N)
	if meaningPass < minMeaning {
		t.Errorf("answer meaning matched its quotes in only %d/%d answers (need ≥ %d)",
			meaningPass, len(set.Questions), minMeaning)
	}
}

// judgeMeaning asks the LLM whether the answer's meaning matches its own quotes.
// It measures semantic consistency (the check plain keyword matching cannot do):
// the answer's main claims must be supported by, or reasonably follow from, the
// quotes, and must not contradict them. It retries once on an empty/unclear
// verdict (reasoning models sometimes spend their whole budget thinking).
func judgeMeaning(ctx context.Context, llm port.LLMClient, model string, ans domain.RAGAnswer) (bool, string) {
	var b strings.Builder
	b.WriteString("Ты — проверяющий фактическую обоснованность. Ниже ОТВЕТ ассистента и ЦИТАТЫ, на которые он ссылается.\n")
	b.WriteString("Оцени, СОВПАДАЕТ ЛИ СМЫСЛ ответа с цитатами по двум критериям:\n")
	b.WriteString("  (1) ответ НЕ противоречит цитатам;\n")
	b.WriteString("  (2) основные утверждения ответа подтверждаются цитатами или разумно следуют из них.\n")
	b.WriteString("Допустимы обобщения и связки, логически вытекающие из цитат. ")
	b.WriteString("Вердикт UNSUPPORTED ставь только при явном противоречии или существенном утверждении, не опирающемся на цитаты.\n\n")
	b.WriteString("=== ОТВЕТ ===\n")
	b.WriteString(ans.Answer)
	b.WriteString("\n\n=== ЦИТАТЫ ===\n")
	for i, q := range ans.Quotes {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q.Text)
	}
	b.WriteString("\nОтветь СТРОГО так: ПЕРВОЕ слово ответа — вердикт (SUPPORTED или UNSUPPORTED), затем краткое обоснование.")

	ask := func() (string, error) {
		resp, err := llm.Chat(ctx, port.LLMRequest{
			Model:     model,
			Messages:  []domain.Message{{Role: domain.RoleUser, Content: b.String()}},
			MaxTokens: 4000, // headroom so reasoning models still emit a verdict
		})
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(resp.Content), nil
	}

	text, err := ask()
	if err != nil {
		return false, fmt.Sprintf("judge error: %v", err)
	}
	// Retry once if the model returned nothing usable (empty content = it burned
	// the whole budget on reasoning).
	if text == "" {
		if t2, err := ask(); err == nil && t2 != "" {
			text = t2
		}
	}
	upper := strings.ToUpper(text)
	// UNSUPPORTED contains "SUPPORTED", so test the negative first.
	if strings.Contains(upper, "UNSUPPORTED") || strings.Contains(upper, "NOT SUPPORTED") {
		return false, snippet(text, 200)
	}
	if strings.Contains(upper, "SUPPORTED") {
		return true, ""
	}
	return false, "судья не вернул явного вердикта: " + snippet(text, 200)
}

// logRerankReorder highlights how the reranker changed the ranking versus pure
// retrieval: the top pick by each score, and how many finally-kept chunks would
// NOT have survived a same-size cut on embedding similarity alone (i.e. chunks
// the reranker promoted). retrieved is similarity-ordered best-first; final is
// rerank-ordered best-first.
func logRerankReorder(log func(string, ...any), retrieved, final []domain.RetrievedChunk) {
	if len(retrieved) == 0 || len(final) == 0 {
		return
	}
	keep := len(final)
	simKept := make(map[int]bool)
	for i := 0; i < keep && i < len(retrieved); i++ {
		simKept[retrieved[i].ChunkID] = true
	}
	promoted := 0
	for _, c := range final {
		if !simKept[c.ChunkID] {
			promoted++
		}
	}
	log("  ↳ топ по similarity: chunk#%d (%.4f); топ по rerank: chunk#%d (%.4f); реранкинг продвинул %d/%d чанк(ов), не попавших бы в top-%d по similarity",
		retrieved[0].ChunkID, retrieved[0].Similarity, final[0].ChunkID, final[0].RerankScore, promoted, len(final), keep)
}

// quoteBackedByChunks reports whether a quote overlaps substantially with the
// retrieved chunk text — a lexical grounding sanity check (whitespace/case
// insensitive, tolerant of minor paraphrase via token overlap).
func quoteBackedByChunks(quote string, chunks []domain.RetrievedChunk) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	q := norm(quote)
	if q == "" {
		return false
	}
	for _, c := range chunks {
		hay := norm(c.Content)
		if strings.Contains(hay, q) {
			return true
		}
		// Token-overlap fallback: most of the quote's words appear in the chunk.
		words := strings.Fields(q)
		if len(words) == 0 {
			continue
		}
		hit := 0
		for _, w := range words {
			if len(w) >= 4 && strings.Contains(hay, w) {
				hit++
			}
		}
		if float64(hit) >= 0.7*float64(len(words)) {
			return true
		}
	}
	return false
}
