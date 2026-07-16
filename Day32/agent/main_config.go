package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"ai-adv-agent/internal/app"
	"ai-adv-agent/internal/config"
)

// cliFlags bundles the CLI flag pointers the orchestrator path reads. main
// populates it from its existing flag declarations, so the legacy code keeps
// using the same pointers unchanged.
type cliFlags struct {
	query          *string
	interactive    *bool
	noTUI          *bool
	debug          *bool
	maxTokens      *int
	temperature    *float64
	caCert         *string
	historyFile    *string
	historyLimit   *int
	summaryEnabled *bool
	strategyFlag   *string
	windowSize     *int
	memoryUpdate   *bool
	profileFile    *string
	invariantsFile *string

	ragEnabled        *bool
	ragDB             *string
	ragTopK           *int
	ragRerank         *bool
	ragRerankModel    *string
	ragRerankMode     *string
	ragRerankProvider *string
	ragRerankURL      *string
	ragRerankKey      *string
	ragThreshold      *float64
	ragTopKFinal      *int
	ragEmbedURL       *string
	ragEmbedModel     *string
	ragEmbedKey       *string

	mcpConfigFile *string
}

// buildOverrides collects the CLI flags the user actually set (via flag.Visit)
// into a config.Overrides. Only explicitly-passed flags override the YAML config,
// so flags win where present without clobbering config with their zero-defaults.
func buildOverrides(f *cliFlags) config.Overrides {
	set := map[string]bool{}
	flag.Visit(func(fl *flag.Flag) { set[fl.Name] = true })

	ov := config.Overrides{}
	if set["max-tokens"] {
		ov.MaxTokens = f.maxTokens
	}
	if set["temperature"] && *f.temperature >= 0 {
		ov.Temperature = f.temperature
	}
	if set["ca-cert"] {
		ov.CACert = f.caCert
	}
	if set["history"] {
		ov.Dir = f.historyFile
	}
	if set["history-limit"] {
		ov.STMLimit = f.historyLimit
	}
	if set["summary"] {
		ov.STMSummary = f.summaryEnabled
	}
	if set["strategy"] {
		ov.STMStrategy = f.strategyFlag
	}
	if set["window-size"] {
		ov.STMWindowSize = f.windowSize
	}
	if set["memory-update"] {
		ov.AutoUpdate = f.memoryUpdate
	}
	if set["profile"] {
		on := true
		ov.ProfileEnabled = &on
		ov.ProfilePath = f.profileFile
	}
	if set["invariants"] {
		on := true
		ov.InvariantsEnabled = &on
		ov.InvariantsPath = f.invariantsFile
	}
	if set["rag"] {
		ov.RAGEnabled = f.ragEnabled
	}
	if set["rag-db"] {
		ov.RAGDB = f.ragDB
	}
	if set["rag-top-k"] {
		ov.RAGTopK = f.ragTopK
	}
	if set["rag-rerank"] {
		ov.RerankEnabled = f.ragRerank
	}
	if set["rag-rerank-model"] {
		ov.RerankModel = f.ragRerankModel
	}
	if set["rag-rerank-mode"] {
		ov.RerankMode = f.ragRerankMode
	}
	if set["rag-rerank-provider"] {
		ov.RerankProvider = f.ragRerankProvider
	}
	if set["rag-rerank-url"] {
		ov.RerankURL = f.ragRerankURL
	}
	if set["rag-rerank-key"] {
		ov.RerankKey = f.ragRerankKey
	}
	if set["rag-threshold"] {
		ov.RAGThreshold = f.ragThreshold
	}
	if set["rag-top-k-final"] {
		ov.RAGTopKFinal = f.ragTopKFinal
	}
	if set["rag-embed-url"] {
		ov.RAGEmbedURL = f.ragEmbedURL
	}
	if set["rag-embed-model"] {
		ov.RAGEmbedModel = f.ragEmbedModel
	}
	if set["rag-embed-key"] {
		ov.RAGEmbedKey = f.ragEmbedKey
	}
	if set["mcp-config"] {
		on := true
		ov.MCPEnabled = &on
		ov.MCPFile = f.mcpConfigFile
	}
	return ov
}

// runOrchestrator loads the config, applies flag overrides, builds the toolbelt,
// and runs the orchestrator either once (CLI) or as a REPL (interactive).
func runOrchestrator(configPath string, f *cliFlags) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	config.ResolveEnv(&cfg)
	buildOverrides(f).Apply(&cfg)
	if err := config.Validate(cfg); err != nil {
		return err
	}

	// In TUI mode, swap os.Stderr for a log file BEFORE building the toolbelt:
	// any raw stderr write while tcell owns the screen corrupts the TUI. This
	// catches both our adapters (fmt.Fprintf(os.Stderr, …) — rerank fallback
	// notices, MCP warnings) and the MCP stdio subprocesses, which inherit
	// whatever os.Stderr points at when they are spawned in app.Build
	// (cmd.Stderr = os.Stderr) — the git-mcp-server call log was landing
	// straight on the terminal. Progress meant for the user still reaches the
	// log widget via Orchestrator.SetOutput / Consultant.SetOutput.
	useTUI := *f.interactive && !*f.noTUI && term.IsTerminal(int(os.Stdout.Fd()))
	if useTUI {
		if logFile, err := os.Create("agent-tui.log"); err == nil {
			realStderr := os.Stderr
			os.Stderr = logFile
			defer func() {
				os.Stderr = realStderr
				_ = logFile.Close()
				fmt.Fprintln(realStderr, "[orchestrator] журнал stderr TUI-сеанса: agent-tui.log")
			}()
		}
	}

	tb, err := app.Build(cfg)
	if err != nil {
		return err
	}
	defer tb.Close()

	orch := app.NewOrchestrator(tb, *f.debug)

	fmt.Fprintf(os.Stderr, "[orchestrator] model=%s  subagents=%d  rag=%t  mcp=%t\n",
		cfg.LLM.Model, len(cfg.Orchestrator.SubAgents), cfg.RAG.Enabled, cfg.MCP.Enabled)

	if *f.interactive {
		// Prefer the full-screen TUI on a real terminal; fall back to the plain
		// line REPL when --no-tui is set or stdout is not a TTY (pipes, CI).
		if useTUI {
			status := fmt.Sprintf("model=%s · agents=%d · rag=%t · mcp=%t",
				cfg.LLM.Model, len(cfg.Orchestrator.SubAgents), cfg.RAG.Enabled, cfg.MCP.Enabled)
			return runOrchestratorTUI(orch, tb, status)
		}
		return runOrchestratorREPL(orch, tb)
	}

	if *f.query == "" {
		return fmt.Errorf("--query is required (or use --interactive)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	answer, err := orch.Handle(ctx, *f.query)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

// replPrompter implements app.UserPrompter for the plain REPL: it prints the
// approval prompt and reads the reply from the shared stdin scanner.
type replPrompter struct{ scanner *bufio.Scanner }

func (p *replPrompter) AskUser(_ context.Context, prompt string) (string, error) {
	fmt.Println("\n" + prompt)
	fmt.Print("согласование> ")
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("ввод завершён")
	}
	return strings.TrimSpace(p.scanner.Text()), nil
}

// runOrchestratorREPL reads a task per line and prints the orchestrator's answer.
// Slash commands (see orchestratorCommands) are handled locally and never sent to
// the model; /exit or /quit (or EOF) ends the session. /help switches into the
// documentation-consultant mode (grounded Q&A over docs.db + git MCP tools);
// /end switches back. This is the fallback used when stdout is not a terminal or
// --no-tui is set.
func runOrchestratorREPL(orch *app.Orchestrator, tb *app.Toolbelt) error {
	fmt.Fprintln(os.Stderr, "[orchestrator] Interactive session. Type a task and press Enter. /help — консультант по документации.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// The orchestrator can pause to get a plan approved; it reads the reply from
	// the same scanner (Handle runs synchronously here, so there is no contention).
	orch.SetPrompter(&replPrompter{scanner: scanner})
	var lastAnswer string
	var consultant *app.Consultant // non-nil while in documentation-consultant mode
	defer func() {
		if consultant != nil {
			consultant.Close()
		}
	}()
	for {
		if consultant != nil {
			fmt.Print("\ndocs> ")
		} else {
			fmt.Print("\nagent> ")
		}
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr, "\n[orchestrator] session ended")
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch classifyInput(line, orchestratorCommands) {
		case cmdQuit:
			return nil
		case cmdHelp:
			fmt.Println(renderCommandHelp(orchestratorCommands))
			if consultant != nil {
				fmt.Println("\nВы уже в режиме консультанта по документации. Выход — /end.")
				continue
			}
			c, err := tb.NewConsultant()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Режим консультанта недоступен: %v\n", err)
				continue
			}
			consultant = c
			fmt.Println("\n" + consultant.Intro())
			continue
		case cmdEnd:
			if consultant == nil {
				fmt.Println("Вы не в режиме консультанта. /help — войти в него.")
				continue
			}
			consultant.Close()
			consultant = nil
			fmt.Println("Режим консультанта завершён — снова оркестратор задач.")
			continue
		case cmdAgents:
			fmt.Println(orch.AgentsSummary())
			continue
		case cmdMemory:
			fmt.Println(orch.MemorySummary())
			continue
		case cmdMCP:
			fmt.Println(orch.MCPSummary())
			continue
		case cmdTools:
			fmt.Println(orch.ToolsSummary())
			continue
		case cmdCopy:
			if strings.TrimSpace(lastAnswer) == "" {
				fmt.Println("Нет ответа для копирования.")
			} else if m, err := copyToClipboard(lastAnswer); err != nil {
				fmt.Fprintf(os.Stderr, "Копирование не удалось: %v\n", err)
			} else {
				fmt.Printf("Скопировано в буфер обмена (%s).\n", m)
			}
			continue
		case cmdSelect:
			fmt.Println("В обычном режиме (без TUI) текст выделяется мышью нативно; /copy копирует последний ответ.")
			continue
		case cmdKeys:
			fmt.Println("Диагностика клавиш доступна только в TUI (запустите без --no-tui).")
			continue
		case cmdClear:
			fmt.Print("\033[2J\033[H") // ANSI clear screen + home
			continue
		case cmdUnknown:
			fmt.Printf("Неизвестная команда %q. /help — список команд.\n", strings.Fields(line)[0])
			continue
		}
		// cmdInput — forward to the consultant (in docs mode) or the orchestrator.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		var answer string
		var err error
		if consultant != nil {
			answer, err = consultant.Ask(ctx, line)
		} else {
			answer, err = orch.Handle(ctx, line)
		}
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] error: %v\n", err)
			continue
		}
		lastAnswer = answer
		fmt.Println(answer)
	}
}
