//go:build integration

// Integration test that runs the 10 control questions through the RAG pipeline
// twice — once WITHOUT the rerank stage (retrieve → filter → LLM) and once WITH
// it (retrieve → rerank → filter → LLM) — and writes a side-by-side comparison
// report. It isolates the reranker's effect: both arms share the same retriever,
// retrieval pool (TopKRetrieve), threshold and final cap, so any difference in
// the final context — and therefore the answer — comes from reranking.
//
// Needs the LLM (answers), the embeddings endpoint (retrieval), and a rerank
// model. The rerank model defaults to the main LLM (chat scoring); point it at a
// dedicated reranker with RAG_RERANK_MODEL (+ optional RAG_RERANK_MODE /
// RAG_RERANK_PROVIDER / RAG_RERANK_URL / RAG_RERANK_KEY), mirroring the CLI flags.
//
// Run:
//
//	RUN_LLM=1 LLM_PROVIDER=openrouter LLM_MODEL=deepseek/deepseek-v4-flash \
//	  RAG_RERANK_MODEL=cohere/rerank-4-fast \
//	  go test -tags integration -run TestRAGRerankCompare -v -timeout 900s .
package main

import (
	"context"
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

const rerankCompareReport = "rag_rerank_compare_result.txt"

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// buildRerankerFromEnv wires a reranker from the same env interface main.go uses
// for its --rag-rerank-* flags, falling back to the main LLM config. Returns the
// reranker and a human-readable description for the report header.
func buildRerankerFromEnv(t *testing.T) (port.Reranker, string) {
	t.Helper()
	provider := os.Getenv("LLM_PROVIDER")
	apiKey := os.Getenv("LLM_API_KEY")
	mainModel := os.Getenv("LLM_MODEL")

	if p := os.Getenv("RAG_RERANK_PROVIDER"); p != "" {
		provider = p
	}
	if k := os.Getenv("RAG_RERANK_KEY"); k != "" {
		apiKey = k
	}
	model := os.Getenv("RAG_RERANK_MODEL")
	if model == "" {
		model = mainModel
	}

	baseURL := strings.TrimRight(os.Getenv("RAG_RERANK_URL"), "/")
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

	// Resolve api vs chat transport (mode auto → api when the model looks like a
	// dedicated reranker), mirroring main.go.
	mode := strings.ToLower(os.Getenv("RAG_RERANK_MODE"))
	if mode == "" {
		mode = "auto"
	}
	useAPI := false
	switch mode {
	case "api":
		useAPI = true
	case "chat":
		useAPI = false
	default:
		useAPI = strings.Contains(strings.ToLower(model), "rerank")
	}

	if useAPI {
		return ragadapter.NewAPIReranker(baseURL, model, apiKey),
			fmt.Sprintf("%s (provider %s, /rerank endpoint)", model, provider)
	}
	client := llm.NewClient(llm.Config{
		Provider:      provider,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		GigaChatScope: os.Getenv("GIGACHAT_SCOPE"),
	})
	return ragadapter.NewLLMReranker(client, model),
		fmt.Sprintf("%s (provider %s, chat scoring)", model, provider)
}

// sourceCoverage reports how many of a question's expected sources are present in
// the final context: a source counts as covered when any of its anchors appears
// in one of the surviving chunks. This measures the precision of the context the
// LLM actually sees, which is exactly what the rerank/filter stage changes.
func sourceCoverage(chunks []domain.RetrievedChunk, sources []evalSource) (hit, total int) {
	var blob strings.Builder
	for _, c := range chunks {
		blob.WriteString(strings.ToLower(c.Content))
		blob.WriteString("\n")
	}
	hay := blob.String()
	for _, src := range sources {
		for _, a := range src.Anchors {
			if strings.Contains(hay, strings.ToLower(a)) {
				hit++
				break
			}
		}
	}
	return hit, len(sources)
}

func TestRAGRerankCompare(t *testing.T) {
	set := loadEvalSet(t)

	if os.Getenv("RUN_LLM") != "1" {
		t.Skip("set RUN_LLM=1 (plus LLM_PROVIDER/LLM_API_KEY) to run the rerank comparison")
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if _, err := retriever.Retrieve(ctx, "ping", 1); err != nil {
		t.Skipf("embeddings endpoint %s unreachable (%v) — skipping", set.EmbedURL, err)
	}

	llmClient, llmModel := buildLLMFromEnv(t) // t.Skip inside if creds missing
	reranker, rerankDesc := buildRerankerFromEnv(t)

	// Same pipeline for both arms; only RAGConfig.Rerank differs.
	uc := usecase.NewRAGUseCase(retriever, reranker)
	topKRetrieve := envInt("RAG_TOP_K", 10)
	threshold := envFloat("RAG_THRESHOLD", 0)
	topKFinal := envInt("RAG_TOP_K_FINAL", set.TopK)

	cfgNo := usecase.RAGConfig{TopKRetrieve: topKRetrieve, Rerank: false, Threshold: threshold, TopKFinal: topKFinal}
	cfgRe := usecase.RAGConfig{TopKRetrieve: topKRetrieve, Rerank: true, Threshold: threshold, TopKFinal: topKFinal}

	f, err := os.Create(rerankCompareReport)
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
	log("  Сравнение: RAG без reranker vs с reranker")
	log("  10 контрольных вопросов")
	log("  Index:    %s", set.DB)
	log("  Embed:    %s (%s)", set.EmbedURL, set.EmbedModel)
	log("  LLM:      %s", llmModel)
	log("  Reranker: %s", rerankDesc)
	log("  Воронка:  retrieve %d → threshold %.2f → keep %d", topKRetrieve, threshold, topKFinal)
	log("  Date:     %s", time.Now().Format("2006-01-02 15:04:05"))
	log("==========================================")
	log("")

	type row struct {
		id                    int
		total                 int
		srcNo, srcRe          int // expected sources present in final context
		srcTotal              int
		hitNo, hitRe          int // answer keyword coverage
		citedNo, citedRe      bool
		finalNo, finalRe      int // #chunks in final context
	}
	var rows []row
	var sumHitNo, sumHitRe, sumKw int
	var sumSrcNo, sumSrcRe, sumSrcTotal int
	var reBetter, tie, reWorse, citedNoCount, citedReCount int

	for _, q := range set.Questions {
		log("=== Q%d: %s ===", q.ID, q.Question)
		log("Ожидаемые термины: %v", q.AnswerKeywords)

		// --- WITHOUT reranker ---
		resNo, err := uc.BuildPrompt(ctx, q.Question, cfgNo)
		if err != nil {
			t.Errorf("Q%d no-rerank build: %v", q.ID, err)
			log("  [без rerank] ОШИБКА: %v", err)
			log("")
			continue
		}
		ansNo, err := askLLM(ctx, llmClient, llmModel, resNo.Prompt)
		if err != nil {
			t.Errorf("Q%d no-rerank LLM: %v", q.ID, err)
			log("  [без rerank] ОШИБКА LLM: %v", err)
			log("")
			continue
		}

		// --- WITH reranker ---
		resRe, err := uc.BuildPrompt(ctx, q.Question, cfgRe)
		if err != nil {
			t.Errorf("Q%d rerank build: %v", q.ID, err)
			log("  [с rerank] ОШИБКА: %v", err)
			log("")
			continue
		}
		ansRe, err := askLLM(ctx, llmClient, llmModel, resRe.Prompt)
		if err != nil {
			t.Errorf("Q%d rerank LLM: %v", q.ID, err)
			log("  [с rerank] ОШИБКА LLM: %v", err)
			log("")
			continue
		}

		srcNo, srcTotal := sourceCoverage(resNo.Final, q.Sources)
		srcRe, _ := sourceCoverage(resRe.Final, q.Sources)
		hitNo := coverage(ansNo, q.AnswerKeywords)
		hitRe := coverage(ansRe, q.AnswerKeywords)
		citedNo := hasCitation(ansNo)
		citedRe := hasCitation(ansRe)

		rows = append(rows, row{
			id: q.ID, total: len(q.AnswerKeywords),
			srcNo: srcNo, srcRe: srcRe, srcTotal: srcTotal,
			hitNo: hitNo, hitRe: hitRe,
			citedNo: citedNo, citedRe: citedRe,
			finalNo: len(resNo.Final), finalRe: len(resRe.Final),
		})
		sumHitNo += hitNo
		sumHitRe += hitRe
		sumKw += len(q.AnswerKeywords)
		sumSrcNo += srcNo
		sumSrcRe += srcRe
		sumSrcTotal += srcTotal
		if citedNo {
			citedNoCount++
		}
		if citedRe {
			citedReCount++
		}

		// "better" is judged on the context precision the reranker controls
		// (expected sources kept), with answer coverage as the tie-breaker.
		switch {
		case srcRe > srcNo || (srcRe == srcNo && hitRe > hitNo):
			reBetter++
		case srcRe == srcNo && hitRe == hitNo:
			tie++
		default:
			reWorse++
		}

		log("  [без rerank] контекст %d чанк(ов), источники %d/%d, термины %d/%d, цит.%v",
			len(resNo.Final), srcNo, srcTotal, hitNo, len(q.AnswerKeywords), citedNo)
		logChunks(log, resNo.Final, false)
		log("     ответ: %s", snippet(ansNo, 140))
		log("  [с rerank ]  контекст %d чанк(ов), источники %d/%d, термины %d/%d, цит.%v",
			len(resRe.Final), srcRe, srcTotal, hitRe, len(q.AnswerKeywords), citedRe)
		logChunks(log, resRe.Final, true)
		log("     ответ: %s", snippet(ansRe, 140))
		log("")
	}

	// --- Summary table ---
	log("==========================================")
	log("  ИТОГОВАЯ ТАБЛИЦА")
	log("==========================================")
	header := fmt.Sprintf("%-4s  %-16s  %-16s  %-14s  %s", "Q", "без rerank", "с rerank", "источники", "лучше")
	log("%s", header)
	log("%s", strings.Repeat("-", len(header)))
	for _, r := range rows {
		better := "="
		switch {
		case r.srcRe > r.srcNo || (r.srcRe == r.srcNo && r.hitRe > r.hitNo):
			better = "rerank"
		case r.srcRe < r.srcNo || (r.srcRe == r.srcNo && r.hitRe < r.hitNo):
			better = "без rerank"
		}
		no := fmt.Sprintf("терм %d/%d цит %v", r.hitNo, r.total, r.citedNo)
		re := fmt.Sprintf("терм %d/%d цит %v", r.hitRe, r.total, r.citedRe)
		src := fmt.Sprintf("%d→%d /%d", r.srcNo, r.srcRe, r.srcTotal)
		log("%-4d  %-16s  %-16s  %-14s  %s", r.id, no, re, src, better)
	}
	log("%s", strings.Repeat("-", len(header)))
	log("")

	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b) * 100
	}
	log("Точность контекста (нужные источники дошли до промпта):")
	log("  без rerank: %d/%d (%.1f%%)", sumSrcNo, sumSrcTotal, pct(sumSrcNo, sumSrcTotal))
	log("  с rerank:   %d/%d (%.1f%%)", sumSrcRe, sumSrcTotal, pct(sumSrcRe, sumSrcTotal))
	log("")
	log("Полнота ответов по ключевым терминам:")
	log("  без rerank: %d/%d (%.1f%%)", sumHitNo, sumKw, pct(sumHitNo, sumKw))
	log("  с rerank:   %d/%d (%.1f%%)", sumHitRe, sumKw, pct(sumHitRe, sumKw))
	log("")
	log("Ответы со ссылками на источники: без rerank %d/%d, с rerank %d/%d",
		citedNoCount, len(rows), citedReCount, len(rows))
	log("Победы по вопросам: rerank лучше — %d, одинаково — %d, rerank хуже — %d (из %d)",
		reBetter, tie, reWorse, len(rows))
	log("")
	switch {
	case sumSrcRe > sumSrcNo:
		log("Вывод: reranker повышает точность контекста — нужные фрагменты чаще")
		log("       доходят до промпта после отсечки, что даёт более обоснованные ответы.")
	case sumSrcRe == sumSrcNo && sumHitRe > sumHitNo:
		log("Вывод: по составу источников результаты близки, но reranker улучшает")
		log("       ранжирование, и итоговые ответы полнее по ключевым терминам.")
	case sumSrcRe == sumSrcNo && sumHitRe == sumHitNo:
		log("Вывод: на этом наборе reranker не изменил итог — эмбеддинги уже хорошо")
		log("       ранжируют; выигрыш ожидается на большем пуле (увеличьте RAG_TOP_K).")
	default:
		log("Вывод: на этом наборе reranker не дал выигрыша; проверьте модель rerank")
		log("       (RAG_RERANK_MODEL) и порог/пул (RAG_THRESHOLD/RAG_TOP_K).")
	}
	log("")
	log("Отчёт: %s", rerankCompareReport)

	if len(rows) != len(set.Questions) {
		t.Errorf("comparison completed only %d/%d questions (see errors above)", len(rows), len(set.Questions))
	}
}

// logChunks prints the final chunks with the score that drove filtering for the
// arm: rerank score when reranked, similarity otherwise.
func logChunks(log func(string, ...any), chunks []domain.RetrievedChunk, reranked bool) {
	for i, c := range chunks {
		label := c.File
		if c.Section != "" {
			label += " — " + c.Section
		}
		if reranked {
			log("       %d. %s (sim %.3f, rerank %.3f)", i+1, label, c.Similarity, c.RerankScore)
		} else {
			log("       %d. %s (sim %.3f)", i+1, label, c.Similarity)
		}
	}
}
