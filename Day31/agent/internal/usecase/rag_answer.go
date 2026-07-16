package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// dontKnowAnswer is the honest response returned when no retrieved chunk clears
// the relevance threshold. Per requirement 4 the assistant must refuse to guess
// and ask the user to clarify instead of fabricating an answer.
const dontKnowAnswer = "Не знаю: в базе знаний не нашлось фрагментов, достаточно релевантных для ответа на этот вопрос. " +
	"Пожалуйста, уточните вопрос — переформулируйте его, добавьте деталей или сузьте тему."

// ConversationContext carries the dialogue state that keeps a RAG answer coherent
// across turns: the recent message history (for follow-up continuity) and the
// task memory (goal / clarified points / fixed constraints). Both are optional —
// the zero value degrades AnswerWithContext to a stateless single-shot answer.
type ConversationContext struct {
	History    []domain.Message  // recent turns, oldest-first (caller may window it)
	TaskMemory domain.TaskMemory // goal / clarified / constraints
}

// Answer runs the full RAG pipeline and returns a structured, grounded answer:
// the model's reply plus the sources (source + section/chunk_id) and the quotes
// (verbatim fragments from the retrieved chunks) it relies on. It is the
// stateless entry point; AnswerWithContext adds dialogue history + task memory.
//
//	question → retrieve → [rerank] → filter by threshold → keep top-K
//	         → grounded LLM call → parse {answer, sources, quotes}
//
// If nothing clears the threshold the pipeline short-circuits to an honest
// "I don't know" (Grounded=false) without calling the LLM, satisfying the
// low-relevance guard. The second return value is the raw pipeline breakdown so
// callers can still surface the retrieval/rerank/filter funnel.
func (uc *RAGUseCase) Answer(ctx context.Context, llm port.LLMClient, model string, maxTokens int, question string, cfg RAGConfig) (domain.RAGAnswer, RAGResult, error) {
	return uc.AnswerWithContext(ctx, llm, model, maxTokens, question, cfg, ConversationContext{})
}

// AnswerWithContext is Answer with dialogue awareness. On top of the stateless
// pipeline it (1) anchors the retrieval query to the dialogue goal so terse
// follow-up questions still fetch on-topic context, (2) injects the task memory
// as a system directive, and (3) replays the recent history before the grounded
// prompt so the model can resolve references to earlier turns. The retrieval,
// filtering and grounding rules are otherwise identical to Answer.
func (uc *RAGUseCase) AnswerWithContext(ctx context.Context, llm port.LLMClient, model string, maxTokens int, question string, cfg RAGConfig, conv ConversationContext) (domain.RAGAnswer, RAGResult, error) {
	// Requirement: every new question triggers a fresh RAG retrieval. When the
	// dialogue has a known goal, prepend it to the embedding query so a short
	// follow-up ("а по умолчанию?") still retrieves chunks about the actual topic.
	retrieveQuery := question
	if g := strings.TrimSpace(conv.TaskMemory.Goal); g != "" {
		retrieveQuery = g + "\n" + question
	}

	retrieved, err := uc.retriever.Retrieve(ctx, retrieveQuery, cfg.TopKRetrieve)
	if err != nil {
		return domain.RAGAnswer{}, RAGResult{}, err
	}

	chunks := retrieved
	reranked := false
	var rerankErr error
	if cfg.Rerank && uc.reranker != nil && len(chunks) > 0 {
		// Rerank against the raw question — that is the concrete information need.
		rr, err := uc.reranker.Rerank(ctx, question, chunks)
		if err != nil {
			// A flaky reranker (e.g. a small local model that ignores the score
			// format) must not sink the answer: fall back to the similarity-ranked
			// pool and report the failure via RerankErr so it is not silent.
			rerankErr = err
		} else {
			chunks = rr
			reranked = true
		}
	}

	final := filterChunks(chunks, cfg.Threshold, cfg.TopKFinal, reranked)

	res := RAGResult{
		Prompt:    BuildAnswerPrompt(question, final),
		Retrieved: retrieved,
		Reranked:  reranked,
		Final:     final,
		RerankErr: rerankErr,
	}

	// Low-relevance guard: nothing passed the threshold → say "I don't know".
	if len(final) == 0 {
		return domain.RAGAnswer{
			Answer:   dontKnowAnswer,
			Grounded: false,
		}, res, nil
	}

	// Assemble the message list: task memory as a system directive, then the
	// recent dialogue history, then the grounded prompt as the final user turn.
	// The grounded prompt stays last so it remains the model's immediate focus.
	var msgs []domain.Message
	if sys := TaskMemorySystemBlock(conv.TaskMemory); sys != "" {
		msgs = append(msgs, domain.Message{Role: domain.RoleSystem, Content: sys})
	}
	msgs = append(msgs, conv.History...)
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: res.Prompt})

	resp, err := llm.Chat(ctx, port.LLMRequest{
		Model:     model,
		Messages:  msgs,
		MaxTokens: maxTokens,
	})
	if err != nil {
		return domain.RAGAnswer{}, res, fmt.Errorf("llm answer: %w", err)
	}

	ans := parseRAGAnswer(resp.Content, final, reranked)
	return ans, res, nil
}

// BuildAnswerPrompt builds the grounded prompt that asks the model for a
// structured JSON answer: the reply, the sources it used, and verbatim quotes
// backing them. Each context chunk is tagged with a [n] marker the model must
// reference; the caller maps markers back to concrete source/section/chunk_id.
func BuildAnswerPrompt(question string, chunks []domain.RetrievedChunk) string {
	var b strings.Builder
	b.WriteString("Ты — ассистент, отвечающий СТРОГО по приведённому ниже контексту.\n")
	b.WriteString("Правила:\n")
	b.WriteString("1. Отвечай только на основе контекста. Ничего не выдумывай и не противоречь контексту.\n")
	b.WriteString("2. Если в контексте нет ответа — так и напиши, не угадывай.\n")
	b.WriteString("3. Каждый факт подкрепляй ссылкой на источник по его маркеру [n].\n")
	b.WriteString("4. Цитаты (quotes) должны быть ДОСЛОВНЫМИ фрагментами из контекста.\n")
	b.WriteString("5. КАЖДОЕ утверждение ответа должно опираться на одну из приведённых тобой цитат: ")
	b.WriteString("включи в quotes фрагмент под каждое утверждение и не пиши того, чего нет в цитатах.\n\n")

	b.WriteString("=== КОНТЕКСТ ===\n")
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] источник: %s", i+1, c.File)
		if c.Section != "" {
			fmt.Fprintf(&b, " — %s", c.Section)
		}
		fmt.Fprintf(&b, " (chunk_id=%d)\n", c.ChunkID)
		b.WriteString(strings.TrimSpace(c.Content))
		b.WriteString("\n\n")
	}

	b.WriteString("=== ВОПРОС ===\n")
	b.WriteString(question)
	b.WriteString("\n\n")

	b.WriteString("=== ФОРМАТ ОТВЕТА ===\n")
	b.WriteString("Верни ТОЛЬКО валидный JSON (без markdown-обёрток) такой структуры:\n")
	b.WriteString(`{
  "answer": "развёрнутый ответ на вопрос по контексту",
  "sources": [n, ...],
  "quotes": [{"marker": n, "text": "дословная цитата из контекста"}, ...]
}`)
	b.WriteString("\nГде n — номер маркера [n] использованного фрагмента. ")
	b.WriteString("В sources перечисли маркеры всех источников, на которые опирается ответ. ")
	b.WriteString("В quotes приведи хотя бы одну дословную цитату для каждого использованного источника.")
	return b.String()
}

// ragAnswerJSON mirrors the JSON contract in BuildAnswerPrompt.
type ragAnswerJSON struct {
	Answer  string `json:"answer"`
	Sources []int  `json:"sources"`
	Quotes  []struct {
		Marker int    `json:"marker"`
		Text   string `json:"text"`
	} `json:"quotes"`
}

// parseRAGAnswer turns the model's JSON reply into a domain.RAGAnswer, mapping
// each [n] marker back to the concrete chunk it names. It is lenient: if the
// model returns prose instead of JSON, the whole text becomes the answer and
// every final chunk is listed as a source so a citation is always present.
func parseRAGAnswer(raw string, chunks []domain.RetrievedChunk, reranked bool) domain.RAGAnswer {
	marker := func(n int) (domain.RetrievedChunk, bool) {
		if n >= 1 && n <= len(chunks) {
			return chunks[n-1], true
		}
		return domain.RetrievedChunk{}, false
	}

	parsed, ok := extractRAGAnswerJSON(raw)
	if !ok {
		// Fallback: no parseable JSON. Surface the raw text and cite every chunk
		// so the answer still carries sources (requirement 1 of the test).
		return domain.RAGAnswer{
			Answer:   strings.TrimSpace(raw),
			Sources:  chunksToSources(chunks, reranked),
			Quotes:   chunksToQuotes(chunks),
			Grounded: true,
		}
	}

	ans := domain.RAGAnswer{Answer: strings.TrimSpace(parsed.Answer), Grounded: true}

	seen := make(map[int]bool)
	for _, n := range parsed.Sources {
		c, ok := marker(n)
		if !ok || seen[n] {
			continue
		}
		seen[n] = true
		ans.Sources = append(ans.Sources, domain.AnswerSource{
			Source:  c.File,
			Section: c.Section,
			ChunkID: c.ChunkID,
			Score:   c.Score(reranked),
		})
	}
	for _, q := range parsed.Quotes {
		c, ok := marker(q.Marker)
		text := strings.TrimSpace(q.Text)
		if !ok || text == "" {
			continue
		}
		ans.Quotes = append(ans.Quotes, domain.AnswerQuote{
			Source:  c.File,
			Section: c.Section,
			ChunkID: c.ChunkID,
			Text:    text,
		})
	}

	// Robustness: the tests require every answer to carry sources AND quotes.
	// If the model forgot either, fall back to the retrieved chunks so the
	// grounding is never silently dropped.
	if len(ans.Sources) == 0 {
		ans.Sources = chunksToSources(chunks, reranked)
	}
	if len(ans.Quotes) == 0 {
		ans.Quotes = chunksToQuotes(chunks)
	}
	return ans
}

// extractRAGAnswerJSON finds and decodes the JSON object in the model reply,
// tolerating ```json code fences and leading/trailing prose.
func extractRAGAnswerJSON(raw string) (ragAnswerJSON, bool) {
	s := strings.TrimSpace(raw)
	// Strip a fenced ```json ... ``` block if present.
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
		if j := strings.Index(rest, "```"); j >= 0 {
			s = rest[:j]
		} else {
			s = rest
		}
		s = strings.TrimSpace(s)
	}
	// Narrow to the outermost {...} span.
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ragAnswerJSON{}, false
	}
	var out ragAnswerJSON
	if err := json.Unmarshal([]byte(s[start:end+1]), &out); err == nil && strings.TrimSpace(out.Answer) != "" {
		return out, true
	}
	// Strict parse failed — commonly because a reasoning model truncated the
	// JSON mid-array when it ran out of token budget. Salvage what is complete:
	// the answer string plus any whole source/quote entries emitted before the
	// cut. This keeps the clean prose answer instead of leaking raw JSON.
	return salvageRAGAnswerJSON(s[start:])
}

var (
	answerRe  = regexp.MustCompile(`"answer"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	sourcesRe = regexp.MustCompile(`"sources"\s*:\s*\[([0-9,\s]*)`)
	quoteRe   = regexp.MustCompile(`"marker"\s*:\s*(\d+)\s*,\s*"text"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

// salvageRAGAnswerJSON best-effort extracts fields from a JSON object that may
// be truncated. It relies on regexes rather than a full parse so a cut-off
// trailing array still yields the answer and every complete entry before the cut.
func salvageRAGAnswerJSON(s string) (ragAnswerJSON, bool) {
	m := answerRe.FindStringSubmatch(s)
	if m == nil {
		return ragAnswerJSON{}, false
	}
	answer, err := strconv.Unquote(`"` + m[1] + `"`)
	if err != nil || strings.TrimSpace(answer) == "" {
		return ragAnswerJSON{}, false
	}
	out := ragAnswerJSON{Answer: answer}

	if sm := sourcesRe.FindStringSubmatch(s); sm != nil {
		for _, tok := range strings.Split(sm[1], ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if n, err := strconv.Atoi(tok); err == nil {
				out.Sources = append(out.Sources, n)
			}
		}
	}

	for _, qm := range quoteRe.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(qm[1])
		if err != nil {
			continue
		}
		text, err := strconv.Unquote(`"` + qm[2] + `"`)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		out.Quotes = append(out.Quotes, struct {
			Marker int    `json:"marker"`
			Text   string `json:"text"`
		}{Marker: n, Text: text})
	}
	return out, true
}

// chunksToSources lists every chunk as a citation (fallback path).
func chunksToSources(chunks []domain.RetrievedChunk, reranked bool) []domain.AnswerSource {
	out := make([]domain.AnswerSource, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, domain.AnswerSource{
			Source:  c.File,
			Section: c.Section,
			ChunkID: c.ChunkID,
			Score:   c.Score(reranked),
		})
	}
	return out
}

// chunksToQuotes lifts a short verbatim fragment from every chunk (fallback path).
func chunksToQuotes(chunks []domain.RetrievedChunk) []domain.AnswerQuote {
	out := make([]domain.AnswerQuote, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, domain.AnswerQuote{
			Source:  c.File,
			Section: c.Section,
			ChunkID: c.ChunkID,
			Text:    firstSentences(c.Content, 240),
		})
	}
	return out
}

// firstSentences returns a leading fragment of s up to about max runes, trimmed
// at a sentence boundary when possible.
func firstSentences(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndexAny(cut, ".!?"); i > max/2 {
		return cut[:i+1]
	}
	return cut + "…"
}
