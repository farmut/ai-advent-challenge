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

// ErrTaskPaused is returned by executeTaskFSM when the user pauses mid-task.
// The caller (Run loop) handles it by printing a confirmation and continuing.
var ErrTaskPaused = errors.New("task paused by user")

// AgentUseCase drives the interactive 4-phase task state machine:
//
//	planning → execution → validation → done
//	          (validation → planning on rejection)
//
// At any user-facing prompt the user may type "pause" to suspend the task;
// state is persisted and can be resumed later with the "resume" command.
type AgentUseCase struct {
	chat *ChatUseCase
	task port.TaskRepository
	in   io.Reader
	out  io.Writer
	err  io.Writer
}

// NewAgentUseCase wires an AgentUseCase with the given dependencies.
func NewAgentUseCase(chat *ChatUseCase, task port.TaskRepository) *AgentUseCase {
	return &AgentUseCase{chat: chat, task: task, in: os.Stdin, out: os.Stdout, err: os.Stderr}
}

// Run starts the interactive REPL. Commands at the Task> prompt:
//   - any text   → start a new task
//   - "resume"   → continue a paused task
//   - "exit"/"quit" → shut down
func (a *AgentUseCase) Run(cfg ChatConfig) error {
	reader := bufio.NewReader(a.in)

	fmt.Fprintln(a.out, "╔══════════════════════════════════════╗")
	fmt.Fprintln(a.out, "║   Interactive Agent  —  Day 13       ║")
	fmt.Fprintln(a.out, "║   Phases: planning → execution →     ║")
	fmt.Fprintln(a.out, "║           validation → done          ║")
	fmt.Fprintln(a.out, "╚══════════════════════════════════════╝")
	fmt.Fprintln(a.out, `Commands: new task text | "resume" | "exit"`)
	fmt.Fprintln(a.out, `At any prompt type "pause" to suspend and save.`)
	fmt.Fprintln(a.out)

	// ── Startup: offer to resume a previously paused task ─────────────────
	if ts, exists, _ := a.task.Load(); exists {
		a.showPausedTask(ts)
		approved, _, _ := a.prompt(reader, `Resume paused task? [y/yes | any other key to discard]`)
		if approved {
			if err := a.resumeTask(reader, ts, cfg); err != nil && !errors.Is(err, ErrTaskPaused) {
				fmt.Fprintf(a.err, "[agent] resume error: %v\n", err)
			}
		} else {
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
		case "exit", "quit":
			fmt.Fprintln(a.out, "Goodbye!")
			return nil

		case "resume":
			ts, exists, _ := a.task.Load()
			if !exists {
				fmt.Fprintln(a.out, "No paused task found.")
				continue
			}
			if err := a.resumeTask(reader, ts, cfg); err != nil && !errors.Is(err, ErrTaskPaused) {
				fmt.Fprintf(a.err, "[agent] task error: %v\n", err)
			}

		default:
			// Warn if a paused task would be discarded
			if _, exists, _ := a.task.Load(); exists {
				fmt.Fprintln(a.out, `[warning] A paused task is on disk. Starting a new task discards it.`)
				fmt.Fprintln(a.out, `           Type "resume" to continue it instead.`)
				_ = a.task.Clear()
			}
			if err := a.runTask(reader, cmd, cfg); err != nil && !errors.Is(err, ErrTaskPaused) {
				fmt.Fprintf(a.err, "[agent] task error: %v\n", err)
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
func (a *AgentUseCase) executeTaskFSM(r *bufio.Reader, ts domain.TaskState, extraContext string, cfg ChatConfig) error {
	// resultShown tracks whether we already printed the execution result in this session.
	// VALIDATION only re-prints it when resuming directly there (resultShown=false).
	resultShown := false

	for ts.Phase != domain.PhaseDone {
		switch ts.Phase {

		// ── 1. PLANNING ───────────────────────────────────────────────────
		case domain.PhasePlanning:
			a.printPhaseHeader(ts.Phase, ts.Iteration)

			planCfg := cfg
			planCfg.SystemMessage = planningSystemPrompt()
			planCfg.FullQuery = buildPlanningPrompt(ts.Task, extraContext)

			ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
			res, err := a.chat.Execute(ctx, planCfg)
			cancel()
			if err != nil {
				return fmt.Errorf("planning call failed: %w", err)
			}

			fmt.Fprintln(a.out)
			a.printBox("Proposed Plan", res.Content)

			approved, paused, feedback := a.prompt(r,
				`Approve this plan? [y/yes = approve | "pause" = suspend | any other text = revise]`)

			switch {
			case paused:
				ts.PendingFeedback = extraContext // preserve feedback across pause
				_ = a.task.Save(ts)
				a.printPaused(ts.Phase)
				return ErrTaskPaused

			case approved:
				ts.Plan = res.Content
				ts.PendingFeedback = ""
				extraContext = ""
				if err := ts.Transition(domain.PhaseExecution); err != nil {
					return err
				}

			default:
				if feedback == "" {
					feedback = "Plan rejected. Please revise."
				}
				extraContext = feedback
				ts.RetryPlanning()
			}
			_ = a.task.Save(ts)

		// ── 2. EXECUTION ─────────────────────────────────────────────────
		case domain.PhaseExecution:
			a.printPhaseHeader(ts.Phase, ts.Iteration)

			// Skip LLM call when resuming with an already-computed result.
			if ts.Result == "" {
				execCfg := cfg
				execCfg.SystemMessage = executionSystemPrompt(ts.Plan)
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

			_, paused, _ := a.prompt(r,
				`Continue to validation? [y/yes = proceed | "pause" = suspend]`)

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

			approved, paused, feedback := a.prompt(r,
				`Validate this result? [y/yes = accept | "pause" = suspend | any other text = reject]`)

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
// Returns (approved=true) for "y"/"yes", (paused=true) for "pause",
// otherwise (approved=false, paused=false, feedback=<raw input>).
func (a *AgentUseCase) prompt(r *bufio.Reader, question string) (approved, paused bool, feedback string) {
	fmt.Fprintf(a.out, "\n%s\n> ", question)
	line, _ := r.ReadString('\n')
	ans := strings.TrimSpace(line)
	switch strings.ToLower(ans) {
	case "y", "yes":
		return true, false, ""
	case "pause":
		return false, true, ""
	default:
		return false, false, ans
	}
}

func (a *AgentUseCase) printPaused(phase domain.TaskPhase) {
	fmt.Fprintln(a.out)
	fmt.Fprintf(a.out, "[paused] Task suspended at phase: %s. State saved.\n", phase)
	fmt.Fprintln(a.out, `           Type "resume" to continue or start a new task.`)
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

func planningSystemPrompt() string {
	return `You are a planning assistant operating in a 4-phase task state machine:
  Planning → Execution → Validation → Done

Current phase: PLANNING

Your responsibilities:
1. Carefully analyse the user's task.
2. Decompose it into concrete, numbered, actionable steps.
3. Present a clear plan the user can review and approve before any work begins.

If the user previously rejected a plan, incorporate their feedback into the revised version.
Be concise: list steps, not prose explanations.`
}

func executionSystemPrompt(plan string) string {
	return fmt.Sprintf(`You are an execution agent operating in a 4-phase task state machine:
  Planning → Execution → Validation → Done

Current phase: EXECUTION

The user reviewed and approved the following plan:

%s

Your responsibilities:
- Execute every step of the plan completely.
- Provide the full, concrete result — code, text, analysis, or whatever the task requires.
- Do not ask clarifying questions; the plan is already agreed.`, plan)
}

func buildPlanningPrompt(task, feedback string) string {
	var sb strings.Builder
	sb.WriteString("Task: ")
	sb.WriteString(task)
	if feedback != "" {
		sb.WriteString("\n\nRevision feedback from user:\n")
		sb.WriteString(feedback)
		sb.WriteString("\n\nPlease revise your plan based on this feedback.")
	} else {
		sb.WriteString("\n\nAnalyse this task and propose a numbered step-by-step plan.")
	}
	return sb.String()
}
