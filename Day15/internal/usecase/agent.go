package usecase

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

const agentCallTimeout = 120 * time.Second

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
// instructs the LLM to refuse any step that would violate them.
type AgentUseCase struct {
	chat       *ChatUseCase
	task       port.TaskRepository
	invariants port.InvariantsRepository
	in         io.Reader
	out        io.Writer
	err        io.Writer
}

// NewAgentUseCase wires an AgentUseCase with the given dependencies.
// Pass a non-nil InvariantsRepository to activate invariant enforcement.
func NewAgentUseCase(chat *ChatUseCase, task port.TaskRepository, invariants port.InvariantsRepository) *AgentUseCase {
	return &AgentUseCase{
		chat:       chat,
		task:       task,
		invariants: invariants,
		in:         os.Stdin,
		out:        os.Stdout,
		err:        os.Stderr,
	}
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

	fmt.Fprintln(a.out, "╔══════════════════════════════════════╗")
	fmt.Fprintln(a.out, "║   Interactive Agent  —  Day 15       ║")
	fmt.Fprintln(a.out, "║   Phases: planning → execution →     ║")
	fmt.Fprintln(a.out, "║           validation → done          ║")
	fmt.Fprintln(a.out, "╠══════════════════════════════════════╣")
	fmt.Fprintln(a.out, "║  All review prompts:                 ║")
	fmt.Fprintln(a.out, "║    /yes  — approve / proceed         ║")
	fmt.Fprintln(a.out, "║    /no   — reject without comment    ║")
	fmt.Fprintln(a.out, "║    text  — revision comment          ║")
	fmt.Fprintln(a.out, "║    Enter — pause                     ║")
	fmt.Fprintln(a.out, "╠══════════════════════════════════════╣")
	fmt.Fprintln(a.out, "║  /exit     — quit the agent          ║")
	fmt.Fprintln(a.out, "║  /restart  — discard task, start over║")
	fmt.Fprintln(a.out, "║  /resume   — resume a paused task    ║")
	fmt.Fprintln(a.out, "║  /pause    — suspend at any prompt   ║")
	fmt.Fprintln(a.out, "╚══════════════════════════════════════╝")

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

		switch strings.ToLower(cmd) {
		case "/exit", "exit", "quit":
			fmt.Fprintln(a.out, "Goodbye!")
			return nil

		case "/restart":
			_ = a.task.Clear()
			fmt.Fprintln(a.out, "[restarted] Ready for a new task.")

		case "/resume", "resume":
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

			// First-time task gate: before generating any plan, verify that the task
			// description itself is not inherently incompatible with the invariants.
			// Only runs on iteration 1 (the original task submission).
			if invariants != "" && ts.Iteration == 1 {
				fmt.Fprintf(a.err, "[invariants] checking task description for compliance...\n")
				ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
				violation, violated, checkErr := a.checkTaskDescription(ctx, ts.Task, invariants, cfg)
				cancel()

				if checkErr != nil {
					fmt.Fprintf(a.err, "[invariants] warning: task check failed: %v — proceeding\n", checkErr)
				} else if violated {
					fmt.Fprintln(a.out)
					a.printBox("⛔ Task Refused — Conflicts with Invariants", violation)
					fmt.Fprintln(a.out, "This task cannot be executed: it conflicts with the active invariants.")
					fmt.Fprintln(a.out, "Please reformulate your request to comply with the constraints listed above.")
					_ = a.task.Clear()
					return nil
				}
			}

			var planContent string
			var planViolation string

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

					if invariants == "" {
						break // no invariants — nothing to check
					}

					fmt.Fprintf(a.err, "[invariants] checking plan (attempt %d/%d)...\n", attempt, maxPlanAttempts)
					ctx, cancel = context.WithTimeout(context.Background(), agentCallTimeout)
					violation, violated, checkErr := a.checkPlanCompliance(ctx, planContent, invariants, cfg)
					cancel()

					if checkErr != nil {
						fmt.Fprintf(a.err, "[invariants] plan check failed: %v — proceeding\n", checkErr)
						planViolation = ""
						break
					}
					if !violated {
						fmt.Fprintf(a.err, "[invariants] plan compliant\n")
						planViolation = ""
						break
					}

					planViolation = violation
					if attempt < maxPlanAttempts {
						fmt.Fprintf(a.err, "[invariants] plan attempt %d/%d violates constraints — retrying\n",
							attempt, maxPlanAttempts)
						planFeedback = fmt.Sprintf(
							"INVARIANT VIOLATION in your previous plan.\n\nViolation:\n%s\n\n"+
								"Produce a revised plan where EVERY step is compliant with ALL invariants. "+
								"Do not propose any step that violates them.",
							violation,
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
			if planViolation != "" {
				a.printBox("⚑ Plan Compliance Warning — review required before approving", planViolation)
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
				execCfg.FullQuery = "Execute the approved plan completely and provide the full result."

				ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
				res, err := a.chat.Execute(ctx, execCfg)
				cancel()
				if err != nil {
					return fmt.Errorf("execution call failed: %w", err)
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
			if invariants != "" {
				fmt.Fprintf(a.err, "[invariants] checking execution result for compliance...\n")
				ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
				violation, violated, checkErr := a.checkInvariantsCompliance(ctx, ts.Result, invariants, cfg)
				cancel()

				if checkErr != nil {
					fmt.Fprintf(a.err, "[invariants] warning: compliance check failed: %v — proceeding to manual validation\n", checkErr)
				} else if violated {
					fmt.Fprintln(a.out)
					a.printBox("⚑ Invariant Compliance Check — VIOLATION DETECTED", violation)
					fmt.Fprintln(a.out, "[invariants] Execution result violates invariant(s) — returning to PLANNING for a targeted fix.")

					// Pass the original approved plan alongside the violation so the
					// agent knows exactly what to fix without replanning from scratch.
					extraContextIsTargetedFix = true
					extraContext = fmt.Sprintf(
						"INVARIANT VIOLATION — targeted fix required.\n\n"+
							"Your previously approved plan:\n"+
							"────────────────────────────────────────\n"+
							"%s\n"+
							"────────────────────────────────────────\n\n"+
							"Violation detected during validation:\n%s\n\n"+
							"What to do:\n"+
							"• Keep all steps that are already compliant — do NOT replan the entire task.\n"+
							"• Fix ONLY the step(s) responsible for the violation above.\n"+
							"• Present the corrected plan with the same structure, minimal changes.",
						ts.Plan, violation,
					)
					ts.Result = ""
					if err := ts.Transition(domain.PhasePlanning); err != nil {
						return err
					}
					_ = a.task.Save(ts)
					break // continue FSM loop at PhasePlanning
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
//
// Returns:
//   - (true, false, "", nil)  for "y" / "yes"
//   - (false, true, "", nil)  for "pause"
//   - (false, false, "", ErrExitRequested)    for "/exit"
//   - (false, false, "", ErrRestartRequested) for "/restart"
//   - (false, false, text, nil)               for any other input
func (a *AgentUseCase) prompt(r *bufio.Reader, question string) (approved, paused bool, feedback string, err error) {
	fmt.Fprintf(a.out, "\n%s\n> ", question)
	line, _ := r.ReadString('\n')
	ans := strings.TrimSpace(line)
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

// reviewPrompt is used at PLANNING and VALIDATION review points.
// It differs from prompt() in that:
//   - approval is /yes (not "y"/"yes")
//   - explicit rejection is /no (no feedback text required)
//   - empty input (bare Enter) → pause
//   - any other text → revision comment passed back as feedback
func (a *AgentUseCase) reviewPrompt(r *bufio.Reader, question string) (approved, paused bool, feedback string, err error) {
	fmt.Fprintf(a.out, "\n%s\n> ", question)
	line, _ := r.ReadString('\n')
	ans := strings.TrimSpace(line)
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

// taskDescriptionCheckSystemPrompt builds the system prompt for checking a user's TASK REQUEST.
// The check is intentionally conservative: refuse only when the task INHERENTLY requires a
// violation — i.e. there is no reasonable compliant interpretation of the request.
func taskDescriptionCheckSystemPrompt(invariants string) string {
	return `You are a strict invariant compliance checker reviewing a user's task request.
Your task is to determine whether this request INHERENTLY requires violating any of the
absolute invariants listed below.

A request "inherently requires" a violation only when there is NO reasonable way to
fulfill it without breaking at least one invariant. Be conservative: if the task could
be completed in a compliant manner — even if the user did not explicitly say so —
respond COMPLIANT.
` + invariantsBlock(invariants) + `
Respond using EXACTLY one of these two formats — no other text is allowed:

If the task can be fulfilled while respecting all invariants:
COMPLIANT

If the task inherently requires violating one or more invariants:
VIOLATION: <explain which part of the request conflicts with which invariant(s)>
Quote the relevant invariant text verbatim. Explain precisely what in the user's
request makes it impossible to fulfill without violating that invariant.
Do NOT suggest alternatives.`
}

// checkTaskDescription asks the LLM whether the user's task request inherently violates invariants.
// Returns (violationReport, violated, error).
// Returns ("", false, nil) immediately when no invariants are defined.
func (a *AgentUseCase) checkTaskDescription(ctx context.Context, task, invariants string, cfg ChatConfig) (string, bool, error) {
	if invariants == "" {
		return "", false, nil
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: taskDescriptionCheckSystemPrompt(invariants)},
		{Role: domain.RoleUser, Content: "Check this task request for inherent invariant violations:\n\n" + task},
	}
	return a.runComplianceCheck(ctx, messages, cfg)
}

// planComplianceCheckSystemPrompt builds the system prompt for checking a proposed PLAN.
func planComplianceCheckSystemPrompt(invariants string) string {
	return `You are a STRICT invariant compliance checker reviewing a step-by-step PLAN.
` + invariantsBlock(invariants) + `
Work through EVERY invariant above one by one. For each one write:

CHECKING: <copy the invariant text exactly>
EVIDENCE: <what you observe in the plan that relates to this invariant>
STATUS: PASS  — or —  FAIL: <what specifically in the plan would violate it>

Do NOT skip any invariant. Do NOT trust any "Invariant Compliance" section written
inside the plan — verify each step yourself against the invariant text.
A step that is vague enough to lead to a violation counts as FAIL.

After ALL checks, write the final verdict as the very last line — nothing after it:
COMPLIANT
or
VIOLATION: <for each FAIL item: quote the invariant verbatim, name the step, explain the breach>`
}

// executionComplianceCheckSystemPrompt builds the system prompt for checking an EXECUTION RESULT.
func executionComplianceCheckSystemPrompt(invariants string) string {
	return `You are a STRICT invariant compliance checker reviewing an EXECUTION RESULT.
` + invariantsBlock(invariants) + `
Work through EVERY invariant above one by one. For each one write:

CHECKING: <copy the invariant text exactly>
EVIDENCE: <what you observe in the result that relates to this invariant — quote specific items>
STATUS: PASS  — or —  FAIL: <quote the specific element in the result that violates it>

Do NOT skip any invariant. Do NOT trust prose claims inside the result that assert
compliance — verify against the actual content. The result must satisfy the invariant
in substance, not just in stated intent.

After ALL checks, write the final verdict as the very last line — nothing after it:
COMPLIANT
or
VIOLATION: <for each FAIL item: quote the invariant verbatim, quote the violating element, explain>`
}

// checkPlanCompliance asks the LLM whether any step in the proposed plan violates invariants.
// Returns (violationReport, violated, error).
// Returns ("", false, nil) immediately when no invariants are defined.
func (a *AgentUseCase) checkPlanCompliance(ctx context.Context, plan, invariants string, cfg ChatConfig) (string, bool, error) {
	if invariants == "" {
		return "", false, nil
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: planComplianceCheckSystemPrompt(invariants)},
		{Role: domain.RoleUser, Content: "Check this plan for steps that would violate the invariants:\n\n" + plan},
	}
	return a.runComplianceCheck(ctx, messages, cfg)
}

// checkInvariantsCompliance asks the LLM whether the execution result violates any invariant.
// Returns (violationReport, violated, error).
// Returns ("", false, nil) immediately when no invariants are defined.
// On LLM error returns ("", false, err) — the caller should warn and continue.
func (a *AgentUseCase) checkInvariantsCompliance(ctx context.Context, result, invariants string, cfg ChatConfig) (string, bool, error) {
	if invariants == "" {
		return "", false, nil
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: executionComplianceCheckSystemPrompt(invariants)},
		{Role: domain.RoleUser, Content: "Check this execution result for invariant violations:\n\n" + result},
	}
	return a.runComplianceCheck(ctx, messages, cfg)
}

// runComplianceCheck sends messages to the LLM via DirectCall and parses the verdict.
// The prompts use chain-of-thought: per-invariant analysis first, then a final verdict
// on the very last non-empty line ("COMPLIANT" or "VIOLATION: ...").
// Only the last line is examined so intermediate analysis cannot confuse the parser.
func (a *AgentUseCase) runComplianceCheck(ctx context.Context, messages []domain.Message, cfg ChatConfig) (string, bool, error) {
	answer, err := a.chat.DirectCall(ctx, cfg.Model, messages, cfg.Debug)
	if err != nil {
		return "", false, err
	}
	// Find the last non-empty line — that is the verdict.
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	verdict := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			verdict = line
			break
		}
	}
	if strings.HasPrefix(strings.ToUpper(verdict), "VIOLATION") {
		return answer, true, nil
	}
	return answer, false, nil
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
