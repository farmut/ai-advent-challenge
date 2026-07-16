package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// subAgentTimeout bounds a single sub-agent run (which may loop over several tool
// calls).
const subAgentTimeout = 600 * time.Second

// Orchestrator is the main agent process. It does not solve the task directly:
// it drives an LLM planning loop that spawns sub-agents from the configured
// roster and routes their results between one another until it can answer. The
// orchestrator is memory-aware — it injects the shared profile / long-term /
// working / task memory into its planning prompt and persists each dialogue turn.
// UserPrompter lets the orchestrator pause mid-run to consult the user — e.g. to
// get a proposed plan approved, commented on, or sent back for rework before it
// routes the work to the next sub-agent. AskUser blocks until the user replies
// (or the context is cancelled) and returns their free-text response.
type UserPrompter interface {
	AskUser(ctx context.Context, prompt string) (string, error)
}

type Orchestrator struct {
	tb       *Toolbelt
	debug    bool
	out      io.Writer    // progress sink (spawn/route logs); default os.Stderr
	prompter UserPrompter // optional; enables the ask_user human-in-the-loop gate

	roster map[string]*SubAgent
	names  []string // stable roster order for the prompt
}

// NewOrchestrator builds the orchestrator and its sub-agent roster from config.
func NewOrchestrator(tb *Toolbelt, debug bool) *Orchestrator {
	o := &Orchestrator{tb: tb, debug: debug, out: os.Stderr, roster: map[string]*SubAgent{}}
	for _, sa := range tb.Cfg.Orchestrator.SubAgents {
		o.roster[sa.Name] = NewSubAgent(sa, tb, debug)
		o.names = append(o.names, sa.Name)
	}
	return o
}

// SetOutput redirects the orchestrator's (and every sub-agent's) progress output
// to w. The TUI uses this to route spawn/route logs into a widget instead of the
// terminal. Passing nil restores os.Stderr.
func (o *Orchestrator) SetOutput(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	o.out = w
	for _, sa := range o.roster {
		sa.out = w
	}
}

// SetPrompter enables the interactive ask_user gate. With no prompter the
// orchestrator runs fully autonomously (an ask_user action degrades to
// proceeding on its own judgement), which is what the one-shot CLI wants.
func (o *Orchestrator) SetPrompter(p UserPrompter) { o.prompter = p }

// orchestratorAction is the JSON contract the orchestrator LLM emits each round.
type orchestratorAction struct {
	Action   string `json:"action"` // "spawn" | "ask_user" | "finish"
	Agent    string `json:"agent"`
	Task     string `json:"task"`
	Context  string `json:"context"`
	Answer   string `json:"answer"`
	Question string `json:"question"` // ask_user: what to ask the user
	Plan     string `json:"plan"`     // ask_user: the plan to put up for approval
}

// Handle solves userTask and returns the final answer. With an empty roster it
// degrades to a plain memory-aware answer via the shared ChatUseCase.
func (o *Orchestrator) Handle(ctx context.Context, userTask string) (string, error) {
	if len(o.names) == 0 {
		return o.answerDirectly(ctx, userTask)
	}

	system := o.systemPrompt()

	// Seed the planning transcript with the prior dialogue so the orchestrator
	// remembers earlier tasks in the session (e.g. a follow-up "теперь добавь…"
	// referring to code produced last turn). Without this the loop starts blank
	// each call and forgets everything but the memory blocks. The window matches
	// persistTurn's, and the shared history holds only completed user/assistant
	// pairs — the current task is appended after, so there is no duplication.
	transcript := o.priorDialogue()
	transcript = append(transcript, domain.Message{Role: domain.RoleUser, Content: userTask})

	maxRounds := o.tb.Cfg.Orchestrator.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 8
	}

	var answer string
	for round := 0; round < maxRounds; round++ {
		msgs := append([]domain.Message{{Role: domain.RoleSystem, Content: system}}, transcript...)
		resp, err := o.tb.LLM.Chat(ctx, port.LLMRequest{
			Model:       o.tb.Cfg.LLM.Model,
			Messages:    msgs,
			Temperature: temperatureOrDefault(o.tb.Cfg.LLM.Temperature),
			MaxTokens:   o.tb.Cfg.LLM.MaxTokens,
			Debug:       o.debug,
		})
		if err != nil {
			return "", fmt.Errorf("orchestrator LLM call: %w", err)
		}

		act, ok := parseAction(resp.Content)
		if !ok {
			// The model answered in prose instead of the JSON protocol — take that
			// as the final answer rather than failing the run.
			answer = strings.TrimSpace(resp.Content)
			break
		}

		switch act.Action {
		case "finish":
			answer = strings.TrimSpace(act.Answer)
			if answer == "" {
				answer = strings.TrimSpace(resp.Content)
			}
			round = maxRounds // fall through to exit
		case "spawn":
			sa, exists := o.roster[act.Agent]
			if !exists {
				transcript = append(transcript,
					domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
					domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("Ошибка: агент %q не найден. Доступные: %s. Выбери из списка.", act.Agent, strings.Join(o.names, ", "))},
				)
				continue
			}
			fmt.Fprintf(o.out, "[orchestrator] round %d → spawn %q: %s\n", round+1, act.Agent, truncate(act.Task, 80))
			sctx, cancel := context.WithTimeout(ctx, subAgentTimeout)
			result, err := sa.Run(sctx, act.Task, act.Context)
			cancel()
			if err != nil {
				result = fmt.Sprintf("(агент завершился с ошибкой: %v)", err)
			}
			transcript = append(transcript,
				domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
				domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("Результат агента %q:\n%s", act.Agent, result)},
			)
		case "ask_user":
			reply, ok := o.askUser(ctx, act)
			if !ok {
				// No interactive prompter (e.g. one-shot CLI) — proceed autonomously.
				transcript = append(transcript,
					domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
					domain.Message{Role: domain.RoleUser, Content: "(интерактивное согласование недоступно — прими решение сам и продолжай)"},
				)
				continue
			}
			transcript = append(transcript,
				domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
				domain.Message{Role: domain.RoleUser, Content: "Ответ пользователя на согласование: " + reply},
			)
		default:
			// Unknown action — nudge the model back to the protocol.
			transcript = append(transcript,
				domain.Message{Role: domain.RoleAssistant, Content: resp.Content},
				domain.Message{Role: domain.RoleUser, Content: `Неизвестное действие. Ответь строго JSON: {"action":"spawn",...} или {"action":"finish","answer":"..."}.`},
			)
		}

		if answer != "" {
			break
		}
	}

	if answer == "" {
		answer = "Не удалось завершить задачу за отведённое число раундов оркестрации."
	}

	o.persistTurn(ctx, userTask, answer)
	return answer, nil
}

// answerDirectly handles the no-roster case with a plain memory-aware turn.
func (o *Orchestrator) answerDirectly(ctx context.Context, userTask string) (string, error) {
	chat := o.tb.Memory.SharedChat(o.tb.LLM)
	res, err := chat.Execute(ctx, usecase.ChatConfig{
		Model:        o.tb.Cfg.LLM.Model,
		FullQuery:    userTask,
		MaxTokens:    o.tb.Cfg.LLM.MaxTokens,
		Temperature:  temperatureOrDefault(o.tb.Cfg.LLM.Temperature),
		HistoryLimit: o.tb.Cfg.Memory.STM.Limit,
		MemoryUpdate: o.tb.Cfg.Memory.AutoUpdate,
		Debug:        o.debug,
	})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// systemPrompt renders the orchestration instructions: the roster and the JSON
// protocol, prefixed with the shared memory blocks so the orchestrator plans with
// the user's profile, long-term, working and task memory in view.
func (o *Orchestrator) systemPrompt() string {
	var b strings.Builder

	// Memory blocks (best-effort; empty layers contribute nothing).
	if o.tb.Memory.Config().Profile.Enabled {
		if p, err := o.tb.Memory.Profile().Load(); err == nil {
			if block := usecase.ProfileSystemBlock(p); block != "" {
				b.WriteString(block + "\n\n")
			}
		}
	}
	if o.tb.Memory.Config().LTM.Enabled {
		if ltm, err := o.tb.Memory.LTM().Load(); err == nil {
			if block := usecase.LTMSystemBlock(ltm); block != "" {
				b.WriteString(block + "\n\n")
			}
		}
	}
	if o.tb.Memory.Config().WM.Enabled {
		if wm, err := o.tb.Memory.WM().Load(); err == nil {
			if block := usecase.WMSystemBlock(wm); block != "" {
				b.WriteString(block + "\n\n")
			}
		}
	}
	if o.tb.Memory.Config().TaskMemory.Enabled {
		if tm, err := o.tb.Memory.TaskMemory().Load(); err == nil {
			if block := usecase.TaskMemorySystemBlock(tm); block != "" {
				b.WriteString(block + "\n\n")
			}
		}
	}

	b.WriteString("Ты — оркестратор. Ты не решаешь задачу сам, а делегируешь её саб-агентам и маршрутизируешь их результаты.\n\n")
	b.WriteString("Доступные саб-агенты:\n")
	for _, name := range o.names {
		sa := o.roster[name]
		access := describeAccess(sa)
		fmt.Fprintf(&b, "- %s: %s [%s]\n", name, summarize(sa.role.Prompt, 120), access)
	}
	b.WriteString("\nПравила:\n")
	b.WriteString("- На каждом шаге отвечай СТРОГО одним JSON-объектом. Без markdown, без пояснений вокруг.\n")
	b.WriteString("- Запустить саб-агента: {\"action\":\"spawn\",\"agent\":\"<имя>\",\"task\":\"<что сделать>\",\"context\":\"<контекст, в т.ч. результаты прошлых агентов>\"}\n")
	b.WriteString("- Согласовать план с пользователем: {\"action\":\"ask_user\",\"question\":\"<что спросить>\",\"plan\":\"<план на согласование>\"}\n")
	b.WriteString("- Завершить: {\"action\":\"finish\",\"answer\":\"<итоговый ответ пользователю>\"}\n")
	b.WriteString("- Ты видишь результаты предыдущих саб-агентов и можешь передавать их дальше через поле context (маршрутизация).\n")
	b.WriteString("- Если задача требует согласования плана — СНАЧАЛА выпусти ask_user с планом и ДОЖДИСЬ ответа пользователя. По его ответу: согласовано → передавай следующему саб-агенту; замечания → верни на доработку (spawn с учётом комментариев) или уточни снова через ask_user. Не запускай исполнение до согласования.\n")
	b.WriteString("- Используй только агентов из списка. Заверши, как только задача решена.\n")
	return b.String()
}

func describeAccess(sa *SubAgent) string {
	parts := []string{}
	if sa.role.RAG {
		parts = append(parts, "RAG")
	}
	if len(sa.role.MCP) > 0 {
		parts = append(parts, "MCP:"+strings.Join(sa.role.MCP, ","))
	}
	if len(parts) == 0 {
		return "только LLM"
	}
	return strings.Join(parts, " ")
}

// askUser presents the plan/question to the user and returns their reply. ok is
// false when no prompter is wired, so the caller can fall back to autonomous
// behaviour instead of blocking forever.
func (o *Orchestrator) askUser(ctx context.Context, act orchestratorAction) (reply string, ok bool) {
	if o.prompter == nil {
		return "", false
	}
	var b strings.Builder
	b.WriteString("=== Требуется согласование ===\n")
	if q := strings.TrimSpace(act.Question); q != "" {
		b.WriteString(q + "\n")
	}
	if p := strings.TrimSpace(act.Plan); p != "" {
		b.WriteString("\nПредлагаемый план:\n" + p + "\n")
	}
	b.WriteString("\nОтветьте: согласуйте, прокомментируйте или верните на доработку.")

	fmt.Fprintln(o.out, "[orchestrator] жду согласование плана от пользователя…")
	r, err := o.prompter.AskUser(ctx, b.String())
	if err != nil {
		fmt.Fprintf(o.out, "[orchestrator] согласование прервано: %v\n", err)
		return "", false
	}
	return r, true
}

// AgentsSummary renders the configured sub-agent roster (name, access, role
// summary) for the /agents command. Reports an empty roster honestly.
func (o *Orchestrator) AgentsSummary() string {
	if len(o.names) == 0 {
		return "Саб-агенты не настроены — оркестратор отвечает напрямую."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Саб-агенты (%d):\n", len(o.names))
	for _, name := range o.names {
		sa := o.roster[name]
		fmt.Fprintf(&b, "  • %s [%s]\n      %s\n", name, describeAccess(sa), summarize(sa.role.Prompt, 100))
	}
	return strings.TrimRight(b.String(), "\n")
}

// MCPSummary lists the connected MCP servers and their tool counts for the /mcp
// command. Reports honestly when MCP is disabled or unreachable.
func (o *Orchestrator) MCPSummary() string {
	tb := o.tb
	if tb.MCPPool == nil || len(tb.MCPServers) == 0 {
		return "MCP не подключён (mcp.enabled=false или серверы не заданы в конфиге)."
	}
	counts := map[string]int{}
	for _, srv := range tb.ToolRouting {
		counts[srv.Name]++
	}
	names := make([]string, 0, len(tb.MCPServers))
	for n := range tb.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "=== Подключённые MCP-серверы (%d) ===\n", len(names))
	for _, n := range names {
		s := tb.MCPServers[n]
		loc := string(s.Type)
		switch {
		case s.URL != "":
			loc += " " + s.URL
		case s.Command != "":
			loc += " " + s.Command
		}
		fmt.Fprintf(&b, "  • %s [%s] — %d инструмент(ов)\n", n, loc, counts[n])
	}
	b.WriteString("Список инструментов: /tools")
	return b.String()
}

// ToolsSummary lists every available MCP tool (grouped by server) for the /tools
// command.
func (o *Orchestrator) ToolsSummary() string {
	tb := o.tb
	if tb.MCPPool == nil || len(tb.MCPTools) == 0 {
		return "MCP-инструменты недоступны (MCP отключён или инструментов нет)."
	}
	byServer := map[string][]domain.MCPTool{}
	for _, t := range tb.MCPTools {
		srv := tb.ToolRouting[t.Name].Name
		byServer[srv] = append(byServer[srv], t)
	}
	servers := make([]string, 0, len(byServer))
	for n := range byServer {
		servers = append(servers, n)
	}
	sort.Strings(servers)

	var b strings.Builder
	fmt.Fprintf(&b, "=== MCP-инструменты (%d) ===\n", len(tb.MCPTools))
	for _, srv := range servers {
		fmt.Fprintf(&b, "[%s]\n", srv)
		for _, t := range byServer[srv] {
			if d := strings.TrimSpace(t.Description); d != "" {
				fmt.Fprintf(&b, "  • %s — %s\n", t.Name, truncate(d, 80))
			} else {
				fmt.Fprintf(&b, "  • %s\n", t.Name)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// MemorySummary renders the orchestrator's shared memory (task memory, working
// memory, long-term memory) for the /memory command, honouring the enable
// toggles. Disabled or empty layers are reported as such.
func (o *Orchestrator) MemorySummary() string {
	mc := o.tb.Memory.Config()
	var b strings.Builder
	b.WriteString("=== Память оркестратора ===\n")

	if mc.TaskMemory.Enabled {
		if tm, err := o.tb.Memory.TaskMemory().Load(); err == nil {
			if block := usecase.TaskMemorySystemBlock(tm); block != "" {
				b.WriteString(block + "\n")
			} else {
				b.WriteString("Память задачи: пусто\n")
			}
		}
	}
	if mc.WM.Enabled {
		if wm, err := o.tb.Memory.WM().Load(); err == nil {
			if block := usecase.WMSystemBlock(wm); block != "" {
				b.WriteString(block + "\n")
			} else {
				b.WriteString("Рабочая память (WM): пусто\n")
			}
		}
	}
	if mc.LTM.Enabled {
		if ltm, err := o.tb.Memory.LTM().Load(); err == nil {
			if block := usecase.LTMSystemBlock(ltm); block != "" {
				b.WriteString(block + "\n")
			} else {
				b.WriteString("Долговременная память (LTM): пусто\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// priorDialogue loads the shared history (short-term memory) and returns it as a
// windowed message slice to seed the planning transcript. Honours the STM enable
// toggle and the same window as persistTurn. Returns nil on any error or empty
// history so the caller just starts with the current task.
func (o *Orchestrator) priorDialogue() []domain.Message {
	if !o.tb.Cfg.Memory.STM.Enabled {
		return nil
	}
	hist, err := o.tb.Memory.History().Load()
	if err != nil || len(hist) == 0 {
		return nil
	}
	if o.tb.Cfg.Memory.STM.Limit > 0 {
		hist = usecase.SlidingWindow(hist, o.tb.Cfg.Memory.STM.Limit)
	}
	return hist
}

// persistTurn appends the user task and final answer to the shared history and,
// when auto-update is on, refreshes the shared WM/LTM from the exchange.
func (o *Orchestrator) persistTurn(ctx context.Context, userTask, answer string) {
	hist, _ := o.tb.Memory.History().Load()
	hist = append(hist,
		domain.Message{Role: domain.RoleUser, Content: userTask},
		domain.Message{Role: domain.RoleAssistant, Content: answer},
	)
	if o.tb.Cfg.Memory.STM.Limit > 0 {
		hist = usecase.SlidingWindow(hist, o.tb.Cfg.Memory.STM.Limit)
	}
	if err := o.tb.Memory.History().Save(hist); err != nil {
		fmt.Fprintf(o.out, "[orchestrator] warning: could not save history: %v\n", err)
	}

	if !o.tb.Cfg.Memory.AutoUpdate {
		return
	}
	if o.tb.Cfg.Memory.WM.Enabled {
		if wm, err := o.tb.Memory.WM().Load(); err == nil {
			if updated, err := usecase.UpdateWM(ctx, o.tb.LLM, o.tb.Cfg.LLM.Model, wm, userTask, answer, o.debug); err == nil {
				_ = o.tb.Memory.WM().Save(updated)
			}
		}
	}
	if o.tb.Cfg.Memory.LTM.Enabled {
		if ltm, err := o.tb.Memory.LTM().Load(); err == nil {
			if updated, err := usecase.UpdateLTM(ctx, o.tb.LLM, o.tb.Cfg.LLM.Model, ltm, userTask, answer, o.debug); err == nil {
				_ = o.tb.Memory.LTM().Save(updated)
			}
		}
	}
}

// parseAction extracts the first JSON object from the model's reply and decodes
// it into an orchestratorAction. Returns ok=false when no JSON object is present.
func parseAction(content string) (orchestratorAction, bool) {
	s := usecase.StripJSONFences(strings.TrimSpace(content))
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return orchestratorAction{}, false
	}
	var act orchestratorAction
	if err := json.Unmarshal([]byte(s[start:end+1]), &act); err != nil {
		return orchestratorAction{}, false
	}
	if act.Action == "" {
		return orchestratorAction{}, false
	}
	return act, true
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func summarize(s string, n int) string { return truncate(s, n) }
