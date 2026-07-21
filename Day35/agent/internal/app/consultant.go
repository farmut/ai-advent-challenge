package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase"
)

// consultantHistoryWindow caps how many prior consultant messages are replayed
// into each turn's prompt (raw question/answer pairs, not the grounded prompts,
// so retrieved chunks never bloat the history).
const consultantHistoryWindow = 12

// defaultConsultantPrompt is the system instruction for the documentation
// consultant; config.Consultant.Prompt overrides it when set.
const defaultConsultantPrompt = `Ты консультант по документации этого проекта (AI Advent Challenge).
Отвечай на вопросы об устройстве проекта, его компонентах, конфигурации и истории по дням.
Опирайся в первую очередь на приложенный контекст из документации; ссылайся на источники.
Для вопросов о текущем состоянии репозитория (ветка, изменённые файлы, diff) используй доступные git-инструменты.
Если ответа нет ни в документации, ни в инструментах — честно скажи об этом. Отвечай кратко и по делу.`

// Consultant is the documentation-consultant mode behind /help: a grounded Q&A
// session over the docs RAG index plus the MCP tools from its allow-list. It is
// created lazily on entering the mode and closed on /end; its dialogue history
// lives only for the mode session (raw questions and answers, windowed).
type Consultant struct {
	tb      *Toolbelt
	rag     *usecase.RAGUseCase
	ragCfg  usecase.RAGConfig
	history []domain.Message
	out     io.Writer
	closers []func()
}

// NewConsultant builds the consultant from cfg.Consultant. The docs retriever is
// opened here (not in Build) so the agent starts fine when docs.db is absent —
// the error surfaces only when the user actually enters the mode.
func (tb *Toolbelt) NewConsultant() (*Consultant, error) {
	cc := tb.Cfg.Consultant
	if !cc.Enabled {
		return nil, fmt.Errorf("режим консультанта выключен в конфиге (consultant.enabled: false)")
	}

	c := &Consultant{tb: tb, out: os.Stderr}

	if cc.RAG.Enabled {
		retriever, err := ragadapter.NewRetriever(ragadapter.Config{
			DBPath:     cc.RAG.DB,
			EmbedURL:   cc.RAG.EmbedURL,
			EmbedModel: cc.RAG.EmbedModel,
			EmbedKey:   cc.RAG.EmbedKey,
		})
		if err != nil {
			return nil, fmt.Errorf("индекс документации недоступен (%s): %w", cc.RAG.DB, err)
		}
		c.closers = append(c.closers, func() { _ = retriever.Close() })

		reranker, _ := buildReranker(cc.RAG.Rerank, tb.Cfg.LLM, os.Getenv("GIGACHAT_SCOPE"))
		c.rag = usecase.NewRAGUseCase(retriever, reranker)
		c.ragCfg = usecase.RAGConfig{
			TopKRetrieve: cc.RAG.TopK,
			Rerank:       cc.RAG.Rerank.Enabled,
			Threshold:    cc.RAG.Threshold,
			TopKFinal:    cc.RAG.TopKFinal,
		}
	}

	return c, nil
}

// SetOutput redirects the consultant's progress lines (RAG grounding notices).
func (c *Consultant) SetOutput(w io.Writer) {
	if w != nil {
		c.out = w
	}
}

// Ask answers one question grounded on the docs index, with the MCP tools from
// the consultant allow-list available to the LLM. The dialogue history keeps the
// raw question/answer pairs so follow-up questions have context.
func (c *Consultant) Ask(ctx context.Context, question string) (string, error) {
	query := question
	if c.rag != nil {
		res, err := c.rag.BuildPrompt(ctx, question, c.ragCfg)
		if err != nil {
			fmt.Fprintf(c.out, "[consultant] поиск по документации не удался: %v\n", err)
		} else {
			if res.RerankErr != nil {
				fmt.Fprintf(c.out, "[consultant] rerank failed, using similarity order: %v\n", res.RerankErr)
			}
			fmt.Fprintf(c.out, "[consultant] контекст: %d фрагмент(ов) документации\n", len(res.Final))
			// Not res.Prompt: the standard grounding template forbids going
			// beyond the context ("using only the context"), which would stop
			// the model from calling the git tools for repo-state questions.
			query = buildConsultantPrompt(question, res.Final)
		}
	}

	prompt := c.tb.Cfg.Consultant.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultConsultantPrompt
	}

	// A fresh ephemeral chat per turn, seeded with the windowed raw dialogue:
	// the grounded prompt (with its retrieved chunks) is the turn's input only
	// and never enters the persisted history.
	seed := usecase.SlidingWindow(c.history, consultantHistoryWindow)
	chat := c.tb.Memory.EphemeralChat(c.tb.LLM, seed, false)

	grant := c.grant()
	tools := c.tb.MCPToolsForGrant(grant)
	executor := c.tb.ToolExecutorForGrant(grant)

	cfg := usecase.ChatConfig{
		Model:         c.tb.Cfg.LLM.Model,
		FullQuery:     query,
		SystemMessage: prompt,
		MaxTokens:     c.tb.Cfg.LLM.MaxTokens,
		Temperature:   temperatureOrDefault(c.tb.Cfg.LLM.Temperature),
		Strategy:      usecase.StrategyNone,
	}

	res, err := chat.ExecuteWithTools(ctx, cfg, tools, executor)
	if err != nil {
		return "", fmt.Errorf("consultant: %w", err)
	}

	c.history = append(c.history,
		domain.Message{Role: domain.RoleUser, Content: question},
		domain.Message{Role: domain.RoleAssistant, Content: res.Content},
	)
	return res.Content, nil
}

// grant is the consultant's tool access: its configured MCP servers and tool
// policy. With no `tools:` section the policy is empty, i.e. read-only.
func (c *Consultant) grant() Grant {
	return Grant{
		Servers: c.tb.Cfg.Consultant.MCP,
		Tools:   c.tb.Cfg.Consultant.Tools,
		Role:    "consultant",
	}
}

// buildConsultantPrompt grounds the question on the retrieved documentation
// chunks with a tool-friendly instruction: unlike usecase.BuildRAGPrompt it does
// NOT restrict the model to the context alone, so repo-state questions (branch,
// changed files, diff) still trigger the git MCP tools.
func buildConsultantPrompt(question string, chunks []domain.RetrievedChunk) string {
	if len(chunks) == 0 {
		return question
	}
	var b strings.Builder
	b.WriteString("Ниже — контекст из документации проекта. Отвечай на вопрос по нему и ссылайся на источники маркером [n]. ")
	b.WriteString("Если вопрос касается текущего состояния репозитория (ветка, изменённые файлы, diff) — вызови подходящий git-инструмент вместо ответа по документации. ")
	b.WriteString("Если ответа нет ни в контексте, ни в инструментах — скажи об этом честно. ")
	b.WriteString("Если у фрагмента указан URL — приводи его ровно как есть, не сокращая и не выдумывая.\n\n")
	b.WriteString("=== КОНТЕКСТ ===\n")
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] %s", i+1, c.File)
		if c.Section != "" {
			fmt.Fprintf(&b, " — %s", c.Section)
		}
		b.WriteString("\n")
		// Only when present — an empty "URL:" line would invite an invented link.
		if c.URL != "" {
			fmt.Fprintf(&b, "URL: %s\n", c.URL)
		}
		b.WriteString(strings.TrimSpace(c.Content))
		b.WriteString("\n\n")
	}
	b.WriteString("=== ВОПРОС ===\n")
	b.WriteString(question)
	return b.String()
}

// Intro returns the banner shown when entering the mode.
func (c *Consultant) Intro() string {
	var b strings.Builder
	b.WriteString("Режим консультанта по документации проекта.\n")
	if c.rag != nil {
		fmt.Fprintf(&b, "  Документация: %s (retrieve %d → threshold %.2f → keep %d)\n",
			c.tb.Cfg.Consultant.RAG.DB, c.ragCfg.TopKRetrieve, c.ragCfg.Threshold, c.ragCfg.TopKFinal)
	} else {
		b.WriteString("  Документация: RAG выключен — отвечаю без индекса\n")
	}
	if tools := c.tb.MCPToolsForGrant(c.grant()); len(tools) > 0 {
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
		}
		fmt.Fprintf(&b, "  Инструменты: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("Задавайте вопросы о проекте. Выход — /end.")
	return b.String()
}

// Close releases the docs index handle.
func (c *Consultant) Close() {
	for i := len(c.closers) - 1; i >= 0; i-- {
		c.closers[i]()
	}
	c.closers = nil
}
