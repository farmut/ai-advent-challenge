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
	fmt.Fprintln(a.out, "║   Interactive Agent  —  Day 14       ║")
	fmt.Fprintln(a.out, "║   Phases: planning → execution →     ║")
	fmt.Fprintln(a.out, "║           validation → done          ║")
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
		approved, _, _, err := a.prompt(reader, `Resume paused task? [y/yes | /restart to discard | /exit to quit]`)
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
		default:
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

			// planFeedback/planIsTargetedFix are local so that silent auto-retries
			// don't pollute extraContext (which is only updated after user interaction).
			planFeedback := extraContext
			planIsTargetedFix := extraContextIsTargetedFix
			var planContent string
			var planViolation string

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

			fmt.Fprintln(a.out)
			if planViolation != "" {
				a.printBox("⚑ Plan Compliance Warning — review required before approving", planViolation)
			}
			a.printBox("Proposed Plan", planContent)

			approved, paused, feedback, promptErr := a.prompt(r,
				`Approve this plan? [y/yes = approve | /pause | /restart | /exit | text = revise]`)
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
				ts.PendingFeedback = ""
				extraContext = ""
				extraContextIsTargetedFix = false
				if err := ts.Transition(domain.PhaseExecution); err != nil {
					return err
				}

			default:
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

			_, paused, _, promptErr := a.prompt(r,
				`Continue to validation? [y/yes = proceed | /pause | /restart | /exit]`)
			if promptErr != nil {
				return promptErr
			}

			if paused {
				_ = a.task.Save(ts) // Phase still = execution; Result already persisted above
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
			approved, paused, feedback, promptErr := a.prompt(r,
				`Validate this result? [y/yes = accept | /pause | /restart | /exit | text = reject]`)
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
	return `You are a strict invariant compliance checker reviewing a step-by-step PLAN.
Your ONLY task is to determine whether any step in this plan would, if executed, violate
any of the absolute invariants listed below.
` + invariantsBlock(invariants) + `
Respond using EXACTLY one of these two formats — no other text is allowed:

If all steps are compliant with every invariant:
COMPLIANT

If one or more steps would violate an invariant:
VIOLATION: <describe precisely which step(s) and which invariant(s) they would violate>
Quote the relevant invariant text verbatim, then explain specifically how each step
violates it. List ALL violations if there are multiple.
Do NOT suggest fixes.`
}

// executionComplianceCheckSystemPrompt builds the system prompt for checking an EXECUTION RESULT.
func executionComplianceCheckSystemPrompt(invariants string) string {
	return `You are a strict invariant compliance checker reviewing an EXECUTION RESULT.
Your ONLY task is to determine whether this execution result violates any of the
absolute invariants listed below.
` + invariantsBlock(invariants) + `
Respond using EXACTLY one of these two formats — no other text is allowed:

If all invariants are respected by the result:
COMPLIANT

If one or more invariants are violated:
VIOLATION: <describe precisely which invariant(s) are violated>
Quote the relevant invariant text verbatim, then explain specifically how the result
violates it. List ALL violations if there are multiple.
Do NOT suggest fixes.`
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

// runComplianceCheck sends messages to the LLM via DirectCall and parses the COMPLIANT/VIOLATION response.
func (a *AgentUseCase) runComplianceCheck(ctx context.Context, messages []domain.Message, cfg ChatConfig) (string, bool, error) {
	answer, err := a.chat.DirectCall(ctx, cfg.Model, messages, cfg.Debug)
	if err != nil {
		return "", false, err
	}
	answer = strings.TrimSpace(answer)
	if strings.HasPrefix(strings.ToUpper(answer), "VIOLATION") {
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
