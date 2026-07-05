package usecase

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// agentCallTimeout is the total deadline for one EXECUTION phase, which may
// involve many sequential tool-call rounds.  600 s gives ~20 rounds × 30 s/call.
const agentCallTimeout = 600 * time.Second

// maxPlanAttempts is how many times the FSM will silently regenerate a plan that
// fails the automatic invariant compliance check before showing it to the user
// with a warning banner.
const maxPlanAttempts = 3

// ErrTaskPaused is returned by executeTaskFSM when the user types "pause".
var ErrTaskPaused = errors.New("task paused by user")

// ErrExitRequested is returned when the user types /exit at any prompt.
var ErrExitRequested = errors.New("exit requested")

// ErrRestartRequested is returned when the user types /restart at any prompt.
// The caller discards the current task and returns to the Task> prompt.
var ErrRestartRequested = errors.New("restart requested")

// AgentUseCase drives the interactive 4-phase task state machine:
//
//	planning → execution → validation → done
//	          (validation → planning on rejection)
//
// At any user-facing prompt the user may type "pause" to suspend the task;
// state is persisted and can be resumed later with the "resume" command.
//
// When an InvariantsRepository is provided (and the file is non-empty) the agent
// embeds the invariants into every planning and execution system prompt, and
// runs an InvariantChecker sub-agent at three compliance gates: task, plan, execution.
type AgentUseCase struct {
	chat       *ChatUseCase
	task       port.TaskRepository
	invariants port.InvariantsRepository
	checker    *InvariantChecker   // nil when no InvariantsRepository supplied
	mcp        *MCPUseCase         // optional; nil when no MCP config is wired
	caller     port.MCPToolCaller  // optional; enables real tool execution (stateless fallback)
	poolOpener port.MCPPoolOpener  // optional; creates persistent session pools for execution
	in         io.Reader
	out        io.Writer
	err        io.Writer
}

// NewAgentUseCase wires an AgentUseCase with the given dependencies.
// Pass a non-nil InvariantsRepository to activate invariant enforcement.
// An InvariantChecker sub-agent is created automatically from the same LLM client.
func NewAgentUseCase(chat *ChatUseCase, task port.TaskRepository, invariants port.InvariantsRepository) *AgentUseCase {
	var checker *InvariantChecker
	if invariants != nil {
		checker = NewInvariantChecker(chat.LLM(), invariants)
	}
	return &AgentUseCase{
		chat:       chat,
		task:       task,
		invariants: invariants,
		checker:    checker,
		in:         os.Stdin,
		out:        os.Stdout,
		err:        os.Stderr,
	}
}

// WithMCP attaches an MCPUseCase so that /mcp-* slash commands are available
// at every interactive prompt inside the agent.
func (a *AgentUseCase) WithMCP(mcp *MCPUseCase) *AgentUseCase {
	a.mcp = mcp
	return a
}

// WithMCPCaller attaches an MCPToolCaller so that the execution phase can
// route LLM tool_calls to real MCP servers instead of hallucinating results.
func (a *AgentUseCase) WithMCPCaller(caller port.MCPToolCaller) *AgentUseCase {
	a.caller = caller
	return a
}

// WithMCPPoolOpener attaches a factory that creates persistent MCP session pools.
// When set, the EXECUTION phase opens a pool (one subprocess per server, kept alive
// for the duration of the execution) instead of spawning a new process per tool call.
func (a *AgentUseCase) WithMCPPoolOpener(opener port.MCPPoolOpener) *AgentUseCase {
	a.poolOpener = opener
	return a
}

// Run starts the interactive REPL. Commands at the Task> prompt:
//   - any text    → start a new task
//   - /resume     → continue a paused task (also: "resume" without slash)
//   - /restart    → discard current/paused task, return to Task> prompt
//   - /exit       → shut down (also: "exit"/"quit" without slash)
//
// Slash commands work at ANY prompt inside the FSM (planning, execution, validation).
func (a *AgentUseCase) Run(cfg ChatConfig) error {
	reader := bufio.NewReader(a.in)

	fmt.Fprintln(a.out, "╔══════════════════════════════════════════╗")
	fmt.Fprintln(a.out, "║   Interactive Agent  —  Day 16          ║")
	fmt.Fprintln(a.out, "║   Phases: planning → execution →        ║")
	fmt.Fprintln(a.out, "║           validation → done             ║")
	fmt.Fprintln(a.out, "╠══════════════════════════════════════════╣")
	fmt.Fprintln(a.out, "║  All review prompts:                     ║")
	fmt.Fprintln(a.out, "║    /yes  — approve / proceed             ║")
	fmt.Fprintln(a.out, "║    /no   — reject without comment        ║")
	fmt.Fprintln(a.out, "║    text  — revision comment              ║")
	fmt.Fprintln(a.out, "║    Enter — pause                         ║")
	fmt.Fprintln(a.out, "╠══════════════════════════════════════════╣")
	fmt.Fprintln(a.out, "║  /exit     — quit the agent              ║")
	fmt.Fprintln(a.out, "║  /restart  — discard task, start over    ║")
	fmt.Fprintln(a.out, "║  /resume   — resume a paused task        ║")
	fmt.Fprintln(a.out, "║  /pause    — suspend at any prompt       ║")
	fmt.Fprintln(a.out, "╠══════════════════════════════════════════╣")
	fmt.Fprintln(a.out, "║  MCP (available everywhere):             ║")
	fmt.Fprintln(a.out, "║  /mcp-list           — list servers      ║")
	fmt.Fprintln(a.out, "║  /mcp-tools [name]   — list tools        ║")
	fmt.Fprintln(a.out, "║  /mcp-add stdio/sse  — add server        ║")
	fmt.Fprintln(a.out, "║  /mcp-remove <name>  — remove server     ║")
	fmt.Fprintln(a.out, "╚══════════════════════════════════════════╝")

	// Report active memory layers to stderr so operators can confirm what is loaded.
	if p, _ := a.chat.profile.Load(); p.Name != "" || len(p.Preferences) > 0 {
		items := len(p.Preferences)
		if p.Name != "" {
			items++
		}
		fmt.Fprintf(a.err, "[memory] profile: loaded (%d items)\n", items)
	} else {
		fmt.Fprintf(a.err, "[memory] profile: empty\n")
	}
	if wm, _ := a.chat.wm.Load(); len(wm.Facts) > 0 {
		fmt.Fprintf(a.err, "[memory] WM: %d facts loaded\n", len(wm.Facts))
	} else {
		fmt.Fprintf(a.err, "[memory] WM: empty\n")
	}
	if ltm, _ := a.chat.ltm.Load(); len(ltm.Entries) > 0 {
		fmt.Fprintf(a.err, "[memory] LTM: %d entries loaded\n", len(ltm.Entries))
	} else {
		fmt.Fprintf(a.err, "[memory] LTM: empty\n")
	}
	if a.invariants != nil {
		if inv, _ := a.invariants.Load(); inv != "" {
			fmt.Fprintf(a.err, "[memory] invariants: active (%d bytes)\n", len(inv))
		} else {
			fmt.Fprintf(a.err, "[memory] invariants: none\n")
		}
	}
	fmt.Fprintln(a.out)

	// ── Startup: offer to resume a previously paused task ─────────────────
	if ts, exists, _ := a.task.Load(); exists {
		a.showPausedTask(ts)
		approved, _, _, err := a.reviewPrompt(reader, `Resume paused task? [/yes to resume | /no or Enter = discard | /restart | /exit]`)
		switch {
		case errors.Is(err, ErrExitRequested):
			fmt.Fprintln(a.out, "Goodbye!")
			return nil
		case errors.Is(err, ErrRestartRequested):
			_ = a.task.Clear()
			fmt.Fprintln(a.out, "[restarted] Paused task discarded. Enter a new task below.")
		case approved:
			if err := a.resumeTask(reader, ts, cfg); err != nil {
				if errors.Is(err, ErrExitRequested) {
					fmt.Fprintln(a.out, "Goodbye!")
					return nil
				}
				if !errors.Is(err, ErrTaskPaused) && !errors.Is(err, ErrRestartRequested) {
					fmt.Fprintf(a.err, "[agent] resume error: %v\n", err)
				}
			}
		default: // /no, Enter, or any text → discard
			_ = a.task.Clear()
			fmt.Fprintln(a.out, "[paused task discarded]")
		}
		fmt.Fprintln(a.out)
	}

	for {
		fmt.Fprint(a.out, "Task> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(a.out, "\nGoodbye!")
				return nil
			}
			return fmt.Errorf("stdin read error: %w", err)
		}
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}

		switch {
		case strings.ToLower(cmd) == "/exit" || strings.ToLower(cmd) == "exit" || strings.ToLower(cmd) == "quit":
			fmt.Fprintln(a.out, "Goodbye!")
			return nil

		case strings.ToLower(cmd) == "/restart":
			_ = a.task.Clear()
			fmt.Fprintln(a.out, "[restarted] Ready for a new task.")

		case strings.ToLower(cmd) == "/resume" || strings.ToLower(cmd) == "resume":
			ts, exists, _ := a.task.Load()
			if !exists {
				fmt.Fprintln(a.out, "No paused task found.")
				continue
			}
			if err := a.resumeTask(reader, ts, cfg); err != nil {
				if errors.Is(err, ErrExitRequested) {
					fmt.Fprintln(a.out, "Goodbye!")
					return nil
				}
				if errors.Is(err, ErrRestartRequested) {
					_ = a.task.Clear()
					fmt.Fprintln(a.out, "[restarted] Task discarded. Enter a new task.")
				} else if !errors.Is(err, ErrTaskPaused) {
					fmt.Fprintf(a.err, "[agent] task error: %v\n", err)
				}
			}

		case strings.HasPrefix(strings.ToLower(cmd), "/mcp"):
			a.handleMCPCommand(cmd)

		default:
			// Warn if a paused task would be discarded by starting a new one
			if _, exists, _ := a.task.Load(); exists {
				fmt.Fprintln(a.out, `[warning] A paused task is on disk. Starting a new task discards it.`)
				fmt.Fprintln(a.out, `           Type /resume to continue it or /restart to discard.`)
				_ = a.task.Clear()
			}
			if err := a.runTask(reader, cmd, cfg); err != nil {
				if errors.Is(err, ErrExitRequested) {
					fmt.Fprintln(a.out, "Goodbye!")
					return nil
				}
				if errors.Is(err, ErrRestartRequested) {
					_ = a.task.Clear()
					fmt.Fprintln(a.out, "[restarted] Task discarded. Enter a new task.")
				} else if !errors.Is(err, ErrTaskPaused) {
					fmt.Fprintf(a.err, "[agent] task error: %v\n", err)
				}
			}
		}

		fmt.Fprintln(a.out)
	}
}

// runTask creates a fresh TaskState and drives it through the FSM.
func (a *AgentUseCase) runTask(r *bufio.Reader, taskDesc string, cfg ChatConfig) error {
	id := fmt.Sprintf("task-%d", time.Now().UnixMilli())
	ts := domain.NewTaskState(id, taskDesc)
	_ = a.task.Save(ts)
	return a.executeTaskFSM(r, ts, "", cfg)
}

// resumeTask continues an existing TaskState from where it was paused.
func (a *AgentUseCase) resumeTask(r *bufio.Reader, ts domain.TaskState, cfg ChatConfig) error {
	fmt.Fprintf(a.out, "[resuming] phase=%s  iteration=%d\n", ts.Phase, ts.Iteration)
	return a.executeTaskFSM(r, ts, ts.PendingFeedback, cfg)
}

// executeTaskFSM is the core FSM loop shared by runTask and resumeTask.
// extraContext carries user feedback from a prior rejection or pause into the first
// planning round. The loop exits when the task reaches PhaseDone or is paused.
// extraContextIsTargetedFix is true when extraContext was set by an automatic invariant
// violation (not by the user), so buildPlanningPrompt can use targeted-fix wording.
func (a *AgentUseCase) executeTaskFSM(r *bufio.Reader, ts domain.TaskState, extraContext string, cfg ChatConfig) error {
	extraContextIsTargetedFix := false
	// Load invariants once for the lifetime of this FSM run.
	var invariants string
	if a.invariants != nil {
		if inv, err := a.invariants.Load(); err != nil {
			fmt.Fprintf(a.err, "[invariants] warning: failed to load: %v\n", err)
		} else {
			invariants = inv
			if invariants != "" {
				fmt.Fprintf(a.err, "[invariants] enforcing %d bytes of constraints\n", len(invariants))
			}
		}
	}

	// resultShown tracks whether we already printed the execution result in this session.
	// VALIDATION only re-prints it when resuming directly there (resultShown=false).
	resultShown := false

	for ts.Phase != domain.PhaseDone {
		switch ts.Phase {

		// ── 1. PLANNING ───────────────────────────────────────────────────
		case domain.PhasePlanning:
			a.printPhaseHeader(ts.Phase, ts.Iteration)

			var planContent string
			var planViolation string    // full report (with original input) — passed to LLM
			var planViolationRaw string // LLM analysis only — shown to user

			if ts.PendingPlan != "" {
				// Resuming from a pause at the plan-review prompt: restore the plan
				// that was generated and shown before the pause. Skip the LLM call
				// and compliance checks — they already ran before the user paused.
				fmt.Fprintf(a.err, "[planning] restoring pending plan from before pause\n")
				planContent = ts.PendingPlan
			} else {
				// planFeedback/planIsTargetedFix are local so that silent auto-retries
				// don't pollute extraContext (which is only updated after user interaction).
				planFeedback := extraContext
				planIsTargetedFix := extraContextIsTargetedFix

				for attempt := 1; attempt <= maxPlanAttempts; attempt++ {
					planCfg := cfg
					planCfg.SystemMessage = withDomainContext(planningSystemPrompt(invariants), cfg.SystemMessage)
					planCfg.FullQuery = buildPlanningPrompt(ts.Task, planFeedback, planIsTargetedFix)

					ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
					res, err := a.chat.Execute(ctx, planCfg)
					cancel()
					if err != nil {
						return fmt.Errorf("planning call failed: %w", err)
					}
					planContent = res.Content

					if a.checker == nil || invariants == "" {
						break // no invariants — nothing to check
					}

					fmt.Fprintf(a.err, "[invariants] checking plan (attempt %d/%d)...\n", attempt, maxPlanAttempts)
					ctx, cancel = context.WithTimeout(context.Background(), agentCallTimeout)
					planCheck, checkErr := a.checker.Check(ctx, CheckKindPlan, planContent, cfg.Model, cfg.Debug)
					cancel()

					if checkErr != nil {
						fmt.Fprintf(a.err, "[invariants] plan check failed: %v — proceeding\n", checkErr)
						planViolation = ""
						planViolationRaw = ""
						break
					}
					if planCheck.Passed {
						fmt.Fprintf(a.err, "[invariants] plan compliant\n")
						planViolation = ""
						planViolationRaw = ""
						break
					}

					planViolation = planCheck.ViolationReport()
					planViolationRaw = planCheck.Raw
					if attempt < maxPlanAttempts {
						fmt.Fprintf(a.err, "[invariants] plan attempt %d/%d violates constraints — retrying\n",
							attempt, maxPlanAttempts)
						planFeedback = fmt.Sprintf(
							"INVARIANT VIOLATION in your previous plan.\n\nViolation:\n%s\n\n"+
								"Produce a revised plan where EVERY step is compliant with ALL invariants. "+
								"Do not propose any step that violates them.",
							planViolation,
						)
						planIsTargetedFix = true
					} else {
						fmt.Fprintf(a.err, "[invariants] plan still violates after %d attempts — showing with warning\n",
							maxPlanAttempts)
					}
				}

				// Persist the generated plan before showing it to the user so that a
				// pause at the review prompt preserves it exactly for resume.
				ts.PendingPlan = planContent
				_ = a.task.Save(ts)
			}

			fmt.Fprintln(a.out)
			if planViolationRaw != "" {
				a.printBox("⚑ Plan Compliance Warning — review required before approving", planViolationRaw)
			}
			a.printBox("Proposed Plan", planContent)

			approved, paused, feedback, promptErr := a.reviewPrompt(r,
				`Approve this plan? [/yes = approve | /no = reject | Enter = pause | text = revision comment]`)
			if promptErr != nil {
				return promptErr
			}

			switch {
			case paused:
				ts.PendingFeedback = extraContext // preserve outer context across pause
				_ = a.task.Save(ts)
				a.printPaused(ts.Phase)
				return ErrTaskPaused

			case approved:
				ts.Plan = planContent
				ts.PendingPlan = ""
				ts.PendingFeedback = ""
				extraContext = ""
				extraContextIsTargetedFix = false
				if err := ts.Transition(domain.PhaseExecution); err != nil {
					return err
				}

			default:
				ts.PendingPlan = ""
				if feedback == "" {
					feedback = "Plan rejected. Please revise."
				}
				extraContext = feedback
				extraContextIsTargetedFix = false
				ts.RetryPlanning()
			}
			_ = a.task.Save(ts)

		// ── 2. EXECUTION ─────────────────────────────────────────────────
		case domain.PhaseExecution:
			a.printPhaseHeader(ts.Phase, ts.Iteration)

			// Guard: execution requires an approved plan stored in ts.Plan.
			// In normal flow this is always set before transitioning here.
			// A tampered task state file could arrive with phase=execution and plan="";
			// we detect this and force a return to planning rather than running the LLM
			// with an empty plan. Direct assignment is intentional — this is an error-
			// recovery path that bypasses the Transition guard by design.
			if ts.Plan == "" {
				fmt.Fprintln(a.err, "[fsm] corrupt state: execution reached without an approved plan — returning to planning")
				fmt.Fprintln(a.out, "⚠ Cannot execute: no approved plan found. Returning to planning phase.")
				ts.Phase = domain.PhasePlanning
				_ = a.task.Save(ts)
				break
			}

			// Skip LLM call when resuming with an already-computed result.
			if ts.Result == "" {
				execCfg := cfg
				execCfg.SystemMessage = withDomainContext(executionSystemPrompt(ts.Plan, invariants), cfg.SystemMessage)
				// When returning from a validation invariant violation, extraContext carries the
				// full violation report so the execution agent knows exactly what to fix.
				if extraContext != "" {
					execCfg.FullQuery = extraContext
					extraContext = ""
				} else {
					execCfg.FullQuery = "Execute the approved plan completely and provide the full result."
				}

				ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)

				// Open a persistent MCP session pool for this execution run.
				// Each MCP server is started once and its stdio connection reused for
				// all tools/list and tools/call requests, avoiding per-call subprocess spawn.
				var pool port.MCPPool
				if a.poolOpener != nil && a.mcp != nil {
					if servers, err := a.mcp.ListServers(); err == nil && len(servers) > 0 {
						var poolErrs []error
						pool, poolErrs = a.poolOpener.OpenPool(servers)
						for _, e := range poolErrs {
							fmt.Fprintf(a.err, "[mcp] warning starting session: %v\n", e)
						}
					}
				}

				// Collect MCP tools via the pool (persistent) or fall back to stateless caller.
				var res ChatResult
				var callErr error
				var mcpTools []domain.MCPTool
				var mcpRouting map[string]domain.MCPServerConfig
				if pool != nil {
					mcpTools, mcpRouting = a.collectMCPToolsFromPool(ctx, pool)
				} else {
					mcpTools, mcpRouting = a.collectMCPTools(ctx)
				}

				if len(mcpTools) > 0 {
					fmt.Fprintf(a.err, "[mcp] %d tool(s) available for execution\n", len(mcpTools))
					execCfg.SystemMessage = withMCPToolsContext(execCfg.SystemMessage, mcpTools)
					var toolCaller port.MCPToolCaller
					if pool != nil {
						toolCaller = pool
					} else {
						toolCaller = a.caller
					}
					executor := func(toolCtx context.Context, name, argsJSON string) (string, error) {
						srvCfg, ok := mcpRouting[name]
						if !ok {
							return "", fmt.Errorf("unknown tool %q", name)
						}
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
							return "", fmt.Errorf("parse tool args: %w", err)
						}
						return toolCaller.CallTool(toolCtx, srvCfg, name, args)
					}
					res, callErr = a.chat.ExecuteWithTools(ctx, execCfg, mcpTools, executor)
				} else {
					res, callErr = a.chat.Execute(ctx, execCfg)
				}

				cancel()
				if pool != nil {
					pool.Close()
				}
				if callErr != nil {
					return fmt.Errorf("execution call failed: %w", callErr)
				}
				ts.Result = res.Content
				_ = a.task.Save(ts)
			}

			fmt.Fprintln(a.out)
			a.printBox("Execution Result", ts.Result)
			resultShown = true

			approved, _, _, promptErr := a.reviewPrompt(r,
				`Continue to validation? [/yes = proceed | Enter = pause | /restart | /exit]`)
			if promptErr != nil {
				return promptErr
			}

			if !approved {
				// Any non-/yes input (Enter, /no, text) suspends at execution phase.
				_ = a.task.Save(ts)
				a.printPaused(ts.Phase)
				return ErrTaskPaused
			}

			if err := ts.Transition(domain.PhaseValidation); err != nil {
				return err
			}
			_ = a.task.Save(ts)

		// ── 3. VALIDATION ─────────────────────────────────────────────────
		case domain.PhaseValidation:
			a.printPhaseHeader(ts.Phase, ts.Iteration)

			// Guard: validation requires an execution result stored in ts.Result.
			// In normal flow this is always set before transitioning here.
			// A tampered task state file could arrive with phase=validation and result="";
			// validating an empty result is meaningless, so we force re-execution.
			// Direct assignment is intentional — error-recovery path.
			if ts.Result == "" {
				fmt.Fprintln(a.err, "[fsm] corrupt state: validation reached without execution result — re-running execution")
				fmt.Fprintln(a.out, "⚠ Cannot validate: no execution result found. Re-running execution.")
				ts.Phase = domain.PhaseExecution
				_ = a.task.Save(ts)
				break
			}

			// Only show result when resuming directly at this phase (not shown in EXECUTION).
			if !resultShown && ts.Result != "" {
				fmt.Fprintln(a.out)
				a.printBox("Execution Result", ts.Result)
			}

			// ── Automatic invariant compliance check ──────────────────────
			// Runs before the user sees a prompt. If the execution result violates
			// any invariant the FSM auto-rejects and returns to PLANNING without
			// asking the user — the violation details become the revision context.
			if a.checker != nil && invariants != "" {
				fmt.Fprintf(a.err, "[invariants] checking execution result for compliance...\n")
				ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
				execCheck, checkErr := a.checker.Check(ctx, CheckKindExecution, ts.Result, cfg.Model, cfg.Debug)
				cancel()

				if checkErr != nil {
					fmt.Fprintf(a.err, "[invariants] warning: compliance check failed: %v — proceeding to manual validation\n", checkErr)
				} else if !execCheck.Passed {
					fmt.Fprintln(a.out)
					a.printBox("⚑ Invariant Compliance Check — VIOLATION DETECTED", execCheck.Raw)
					fmt.Fprintln(a.out, "[invariants] Execution result violates invariant(s) — returning to EXECUTION for a targeted fix.")

					extraContext = fmt.Sprintf(
						"INVARIANT VIOLATION — your previous execution result failed the compliance check.\n\n"+
							"Violation detected during validation:\n%s\n\n"+
							"Re-execute the approved plan. Fix ONLY the elements responsible for the "+
							"violations identified above. All other parts of the result must remain unchanged.",
						execCheck.ViolationReport(),
					)
					ts.Result = ""
					if err := ts.Transition(domain.PhaseExecution); err != nil {
						return err
					}
					_ = a.task.Save(ts)
					break // continue FSM loop at PhaseExecution
				} else {
					fmt.Fprintf(a.err, "[invariants] compliance check passed\n")
				}
			}

			// ── Manual validation by the user ─────────────────────────────
			// Reached only when the compliance check passed (or no invariants defined).
			approved, paused, feedback, promptErr := a.reviewPrompt(r,
				`Validate this result? [/yes = accept | /no = reject | Enter = pause | text = revision comment]`)
			if promptErr != nil {
				return promptErr
			}

			switch {
			case paused:
				ts.PendingFeedback = ""
				_ = a.task.Save(ts)
				a.printPaused(ts.Phase)
				return ErrTaskPaused

			case approved:
				if err := ts.Transition(domain.PhaseDone); err != nil {
					return err
				}

			default:
				if feedback == "" {
					feedback = "Result rejected. Please revise."
				}
				extraContext = fmt.Sprintf(
					"The previous execution result was rejected by the user.\nUser feedback: %s\n\nRevise the plan to address this feedback.",
					feedback,
				)
				extraContextIsTargetedFix = false
				// Clear the stale result so EXECUTION re-runs the LLM with the new plan.
				ts.Result = ""
				if err := ts.Transition(domain.PhasePlanning); err != nil {
					return err
				}
			}
			_ = a.task.Save(ts)

		// ── Unknown phase ─────────────────────────────────────
		// Reached only via a tampered or corrupt task state file. The phase value
		// is not one of the four defined constants, so no case above matched.
		// The for-loop would otherwise spin forever; we detect and abort.
		default:
			fmt.Fprintf(a.err, "[fsm] corrupt state: unknown phase %q — task cleared\n", ts.Phase)
			fmt.Fprintln(a.out, "⚠ Task state is corrupt (unknown phase). Task cleared.")
			_ = a.task.Clear()
			return fmt.Errorf("unknown task phase %q", ts.Phase)
		}
	}

	// ── 4. DONE ───────────────────────────────────────────────────────────
	a.printPhaseHeader(domain.PhaseDone, ts.Iteration)
	fmt.Fprintf(a.out, "Task completed after %d planning iteration(s).\n", ts.Iteration)
	fmt.Fprintln(a.out, "────────────────────────────────────────")
	_ = a.task.Clear()
	return nil
}

// ── Prompt helpers ────────────────────────────────────────────────────────────

// prompt shows a question and reads one line.
// /mcp-* commands are handled in-place and the question is re-shown.
//
// Returns:
//   - (true, false, "", nil)  for "y" / "yes"
//   - (false, true, "", nil)  for "pause"
//   - (false, false, "", ErrExitRequested)    for "/exit"
//   - (false, false, "", ErrRestartRequested) for "/restart"
//   - (false, false, text, nil)               for any other input
func (a *AgentUseCase) prompt(r *bufio.Reader, question string) (approved, paused bool, feedback string, err error) {
	for {
		fmt.Fprintf(a.out, "\n%s\n> ", question)
		line, _ := r.ReadString('\n')
		ans := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(ans), "/mcp") {
			a.handleMCPCommand(ans)
			continue
		}
		switch strings.ToLower(ans) {
		case "y", "yes":
			return true, false, "", nil
		case "/pause", "pause":
			return false, true, "", nil
		case "/exit", "/quit", "exit", "quit":
			return false, false, "", ErrExitRequested
		case "/restart", "restart":
			return false, false, "", ErrRestartRequested
		default:
			return false, false, ans, nil
		}
	}
}

// reviewPrompt is used at PLANNING and VALIDATION review points.
// /mcp-* commands are handled in-place without affecting the review flow.
// It differs from prompt() in that:
//   - approval is /yes (not "y"/"yes")
//   - explicit rejection is /no (no feedback text required)
//   - empty input (bare Enter) → pause
//   - any other text → revision comment passed back as feedback
func (a *AgentUseCase) reviewPrompt(r *bufio.Reader, question string) (approved, paused bool, feedback string, err error) {
	for {
		fmt.Fprintf(a.out, "\n%s\n> ", question)
		line, _ := r.ReadString('\n')
		ans := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(ans), "/mcp") {
			a.handleMCPCommand(ans)
			continue
		}
		switch strings.ToLower(ans) {
		case "/yes":
			return true, false, "", nil
		case "/no":
			return false, false, "", nil
		case "", "/pause", "pause":
			return false, true, "", nil
		case "/exit", "/quit", "exit", "quit":
			return false, false, "", ErrExitRequested
		case "/restart", "restart":
			return false, false, "", ErrRestartRequested
		default:
			return false, false, ans, nil
		}
	}
}

func (a *AgentUseCase) printPaused(phase domain.TaskPhase) {
	fmt.Fprintln(a.out)
	fmt.Fprintf(a.out, "[paused] Task suspended at phase: %s. State saved.\n", phase)
	fmt.Fprintln(a.out, `           /resume to continue · /restart to discard · /exit to quit`)
}

func (a *AgentUseCase) showPausedTask(ts domain.TaskState) {
	fmt.Fprintln(a.out, "┌─ [PAUSED TASK FOUND] ──────────────────")
	fmt.Fprintf(a.out, "│  Phase:     %s\n", ts.Phase)
	fmt.Fprintf(a.out, "│  Iteration: %d\n", ts.Iteration)
	fmt.Fprintf(a.out, "│  Task:      %s\n", ts.Task)
	fmt.Fprintln(a.out, "└────────────────────────────────────────")
}

// ── Print helpers ─────────────────────────────────────────────────────────────

func (a *AgentUseCase) printPhaseHeader(phase domain.TaskPhase, iteration int) {
	labels := map[domain.TaskPhase]string{
		domain.PhasePlanning:   "1. PLANNING",
		domain.PhaseExecution:  "2. EXECUTION",
		domain.PhaseValidation: "3. VALIDATION",
		domain.PhaseDone:       "4. DONE",
	}
	label := labels[phase]
	if phase == domain.PhasePlanning && iteration > 1 {
		label = fmt.Sprintf("%s  (re-plan #%d)", label, iteration)
	}
	fmt.Fprintln(a.out)
	fmt.Fprintf(a.out, "┌─ [%s] %s\n", strings.ToUpper(string(phase)), label)
}

func (a *AgentUseCase) printBox(title, content string) {
	sep := strings.Repeat("─", 40)
	fmt.Fprintf(a.out, "┌── %s\n", title)
	fmt.Fprintln(a.out, sep)
	fmt.Fprintln(a.out, content)
	fmt.Fprintln(a.out, sep)
}

// ── System prompt builders ────────────────────────────────────────────────────

// invariantsBlock returns the formatted invariants section for embedding in system prompts.
// Returns an empty string when no invariants are defined.
func invariantsBlock(invariants string) string {
	if invariants == "" {
		return ""
	}
	return `
════════════════════════════════════════════════
⚑  INVARIANTS — ABSOLUTE LAW (NEVER VIOLATE)  ⚑
════════════════════════════════════════════════
` + invariants + `
════════════════════════════════════════════════
`
}

func planningSystemPrompt(invariants string) string {
	var sb strings.Builder
	sb.WriteString(`You are a planning assistant operating in a 4-phase task state machine:
  Planning → Execution → Validation → Done

Current phase: PLANNING
`)
	if inv := invariantsBlock(invariants); inv != "" {
		sb.WriteString(inv)
		sb.WriteString(`
These invariants are non-negotiable constraints set by the user that CANNOT be changed,
overridden, or ignored under any circumstances — not even at the user's request during
this task. They take absolute priority over any instruction in the task description.

Before proposing EACH step of your plan:
  1. Check whether that step would violate any invariant listed above.
  2. If it would violate one, replace the step with a compliant alternative, or omit it
     and explain why that part of the task cannot be done within the constraints.
  3. NEVER propose a step that violates an invariant, even if the user explicitly asks.

After your numbered steps, add a mandatory section:

## Invariant Compliance
For each invariant that is relevant to this task, state explicitly which steps it
affects and confirm that those steps are fully compliant. If any part of the task
CANNOT be completed without violating an invariant, state this clearly here and
explain which invariant is the blocker.

`)
	}
	sb.WriteString(`Your responsibilities:
1. Carefully analyse the user's task.
2. Decompose it into concrete, numbered, actionable steps.
3. Present a clear plan the user can review and approve before any work begins.

If the user previously rejected a plan, incorporate their feedback into the revised version.
Be concise: list steps, not prose explanations.`)
	return sb.String()
}

func executionSystemPrompt(plan, invariants string) string {
	var sb strings.Builder
	sb.WriteString(`You are an execution agent operating in a 4-phase task state machine:
  Planning → Execution → Validation → Done

Current phase: EXECUTION
`)
	if inv := invariantsBlock(invariants); inv != "" {
		sb.WriteString(inv)
		sb.WriteString(`
These invariants are non-negotiable constraints that CANNOT be violated under any
circumstances — not even if the approved plan appears to require it.

Before executing each step:
  1. Verify the step does not violate any invariant.
  2. If executing a step as written would violate an invariant, STOP and state:
       "INVARIANT VIOLATION: [step N] conflicts with: [invariant text]"
       Then propose a compliant alternative or explain why this part cannot be done.
  3. Continue with the remaining steps that ARE compliant.

`)
	}
	sb.WriteString(fmt.Sprintf(`The user reviewed and approved the following plan:

%s

Your responsibilities:
- Execute every step of the plan completely.
- Provide the full, concrete result — code, text, analysis, or whatever the task requires.
- Do not ask clarifying questions; the plan is already agreed.`, plan))
	return sb.String()
}

// withDomainContext appends optional caller-supplied domain context (from --system) to a
// phase system prompt. Phase instructions come first (higher priority); the domain
// context is separated so the LLM treats it as supplementary background.
func withDomainContext(phasePrompt, domainCtx string) string {
	if domainCtx == "" {
		return phasePrompt
	}
	return phasePrompt + "\n\n---\nDomain context:\n" + domainCtx
}

// collectMCPTools fetches all tools from all configured MCP servers and builds a
// routing map (tool name → server config) for the executor callback.
// Returns nil, nil when MCP is not wired or no tools are available.
func (a *AgentUseCase) collectMCPTools(ctx context.Context) ([]domain.MCPTool, map[string]domain.MCPServerConfig) {
	if a.mcp == nil || a.caller == nil {
		return nil, nil
	}
	results, errs := a.mcp.ListTools(ctx, "")
	for _, e := range errs {
		fmt.Fprintf(a.err, "[mcp] warning collecting tools: %v\n", e)
	}
	servers, _ := a.mcp.ListServers()
	srvMap := make(map[string]domain.MCPServerConfig, len(servers))
	for _, s := range servers {
		srvMap[s.Name] = s
	}
	var allTools []domain.MCPTool
	routing := make(map[string]domain.MCPServerConfig)
	for srvName, tools := range results {
		srvCfg, ok := srvMap[srvName]
		if !ok {
			continue
		}
		for _, t := range tools {
			allTools = append(allTools, t)
			routing[t.Name] = srvCfg
		}
	}
	return allTools, routing
}

// collectMCPToolsFromPool is the pool-backed variant of collectMCPTools.
// It queries tool lists through persistent sessions instead of spawning new processes.
func (a *AgentUseCase) collectMCPToolsFromPool(ctx context.Context, pool port.MCPPool) ([]domain.MCPTool, map[string]domain.MCPServerConfig) {
	if a.mcp == nil {
		return nil, nil
	}
	servers, err := a.mcp.ListServers()
	if err != nil || len(servers) == 0 {
		return nil, nil
	}
	srvMap := make(map[string]domain.MCPServerConfig, len(servers))
	for _, s := range servers {
		srvMap[s.Name] = s
	}
	var allTools []domain.MCPTool
	routing := make(map[string]domain.MCPServerConfig)
	for _, srv := range servers {
		tools, err := pool.ListTools(ctx, srv)
		if err != nil {
			fmt.Fprintf(a.err, "[mcp] warning listing tools for %q: %v\n", srv.Name, err)
			continue
		}
		for _, t := range tools {
			allTools = append(allTools, t)
			routing[t.Name] = srv
		}
	}
	return allTools, routing
}

// withMCPToolsContext appends a brief tool-calling instruction to the execution system prompt.
// Full tool definitions are already passed as structured "tools" to the API, so we only
// add a short reminder — listing all names + descriptions here would double-count the tokens.
func withMCPToolsContext(prompt string, tools []domain.MCPTool) string {
	if len(tools) == 0 {
		return prompt
	}
	instruction := fmt.Sprintf(
		"\n\n## Tools\n\nYou have %d tools available via the function-call mechanism. "+
			"ALWAYS call the appropriate tool to complete each step — do NOT write pseudo-code or "+
			"describe what you would do. Call the tool, wait for the result, then continue.",
		len(tools),
	)
	return prompt + instruction
}

// ── MCP slash-command handler ─────────────────────────────────────────────────

// handleMCPCommand parses and executes a /mcp-* command typed at any prompt.
// Available commands:
//
//	/mcp-list                          — list configured servers
//	/mcp-tools [server-name]           — list tools (all or one server)
//	/mcp-add stdio <name> <cmd> [args] — add a stdio server
//	/mcp-add sse   <name> <url>        — add an SSE server
//	/mcp-remove <name>                 — remove a server
func (a *AgentUseCase) handleMCPCommand(cmd string) {
	if a.mcp == nil {
		fmt.Fprintln(a.out, "[mcp] MCP is not configured. Start the agent with --mcp-config or --history to enable it.")
		return
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch strings.ToLower(parts[0]) {

	case "/mcp-list":
		servers, err := a.mcp.ListServers()
		if err != nil {
			fmt.Fprintf(a.err, "[mcp] error: %v\n", err)
			return
		}
		if len(servers) == 0 {
			fmt.Fprintln(a.out, "[mcp] No MCP servers configured.")
			fmt.Fprintln(a.out, "      Use /mcp-add to register one.")
			return
		}
		fmt.Fprintf(a.out, "[mcp] Configured servers (%d):\n", len(servers))
		for _, s := range servers {
			switch s.Type {
			case domain.MCPStdio:
				fmt.Fprintf(a.out, "  • %-20s  stdio  %s %s\n", s.Name, s.Command, strings.Join(s.Args, " "))
			case domain.MCPSSE:
				fmt.Fprintf(a.out, "  • %-20s  sse    %s\n", s.Name, s.URL)
			}
		}

	case "/mcp-tools":
		serverName := ""
		if len(parts) > 1 {
			serverName = parts[1]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		results, errs := a.mcp.ListTools(ctx, serverName)
		for _, e := range errs {
			fmt.Fprintf(a.err, "[mcp] warning: %v\n", e)
		}
		if len(results) == 0 {
			fmt.Fprintln(a.out, "[mcp] No tools found.")
			return
		}
		names := make([]string, 0, len(results))
		for n := range results {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			tools := results[n]
			fmt.Fprintf(a.out, "[mcp] Server %q — %d tool(s):\n", n, len(tools))
			for i, t := range tools {
				if t.Description != "" {
					fmt.Fprintf(a.out, "  %2d. %-30s  %s\n", i+1, t.Name, t.Description)
				} else {
					fmt.Fprintf(a.out, "  %2d. %s\n", i+1, t.Name)
				}
			}
		}

	case "/mcp-add":
		// /mcp-add stdio <name> <command> [arg1 arg2 ...]
		// /mcp-add sse   <name> <url>
		if len(parts) < 4 {
			fmt.Fprintln(a.out, "[mcp] Usage:")
			fmt.Fprintln(a.out, "  /mcp-add stdio <name> <command> [arg1 arg2 ...]")
			fmt.Fprintln(a.out, "  /mcp-add sse   <name> <url>")
			return
		}
		transport := strings.ToLower(parts[1])
		name := parts[2]

		var cfg domain.MCPServerConfig
		switch transport {
		case "stdio":
			cfg = domain.MCPServerConfig{
				Name:    name,
				Type:    domain.MCPStdio,
				Command: parts[3],
				Args:    parts[4:],
			}
		case "sse":
			cfg = domain.MCPServerConfig{
				Name: name,
				Type: domain.MCPSSE,
				URL:  parts[3],
			}
		default:
			fmt.Fprintf(a.out, "[mcp] Unknown transport %q. Use stdio or sse.\n", transport)
			return
		}

		if err := a.mcp.AddServer(cfg); err != nil {
			fmt.Fprintf(a.err, "[mcp] error: %v\n", err)
			return
		}
		fmt.Fprintf(a.out, "[mcp] Server %q added.\n", name)

	case "/mcp-remove":
		if len(parts) < 2 {
			fmt.Fprintln(a.out, "[mcp] Usage: /mcp-remove <name>")
			return
		}
		if err := a.mcp.RemoveServer(parts[1]); err != nil {
			fmt.Fprintf(a.err, "[mcp] error: %v\n", err)
			return
		}
		fmt.Fprintf(a.out, "[mcp] Server %q removed.\n", parts[1])

	default:
		fmt.Fprintf(a.out, "[mcp] Unknown command %q\n", parts[0])
		fmt.Fprintln(a.out, "  Commands: /mcp-list  /mcp-tools [name]  /mcp-add stdio/sse ...  /mcp-remove <name>")
	}
}

// buildPlanningPrompt constructs the user message for the PLANNING phase.
// isTargetedFix=true is used when returning from a failed invariant compliance check:
// the feedback already contains the original plan and a precise violation description,
// so the prompt instructs the agent to apply a minimal targeted correction rather
// than replanning the entire task from scratch.
func buildPlanningPrompt(task, feedback string, isTargetedFix bool) string {
	var sb strings.Builder
	sb.WriteString("Task: ")
	sb.WriteString(task)
	switch {
	case feedback != "" && isTargetedFix:
		sb.WriteString("\n\n")
		sb.WriteString(feedback)
		sb.WriteString("\n\nApply only the targeted correction described above. Do not replan steps that are already compliant.")
	case feedback != "":
		sb.WriteString("\n\nRevision feedback from user:\n")
		sb.WriteString(feedback)
		sb.WriteString("\n\nPlease revise your plan based on this feedback.")
	default:
		sb.WriteString("\n\nAnalyse this task and propose a numbered step-by-step plan.")
	}
	return sb.String()
}
