package usecase

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// stubLLM returns a fixed reply, or delegates to replyFn if set.
type stubLLM struct {
	reply   string
	replyFn func() string
}

func (s *stubLLM) Chat(_ context.Context, _ port.LLMRequest) (port.LLMResponse, error) {
	r := s.reply
	if s.replyFn != nil {
		r = s.replyFn()
	}
	return port.LLMResponse{Content: r, Usage: domain.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
}

// memoryTaskRepo is an in-memory TaskRepository for tests.
type memoryTaskRepo struct {
	state *domain.TaskState
}

func (r *memoryTaskRepo) Load() (domain.TaskState, bool, error) {
	if r.state == nil {
		return domain.TaskState{}, false, nil
	}
	return *r.state, true, nil
}
func (r *memoryTaskRepo) Save(ts domain.TaskState) error { r.state = &ts; return nil }
func (r *memoryTaskRepo) Clear() error                   { r.state = nil; return nil }

// noopRepo stubs every repository interface with no-op implementations.
type noopHistoryRepo struct{}

func (r *noopHistoryRepo) Load() ([]domain.Message, error)    { return nil, nil }
func (r *noopHistoryRepo) Save(_ []domain.Message) error       { return nil }

type noopStatsRepo struct{}

func (r *noopStatsRepo) Load() (domain.SessionStats, error) { return domain.SessionStats{}, nil }
func (r *noopStatsRepo) Save(_ domain.SessionStats) error   { return nil }

type noopSummaryRepo struct{}

func (r *noopSummaryRepo) Load() (string, error) { return "", nil }
func (r *noopSummaryRepo) Save(_ string) error   { return nil }

type noopFactsRepo struct{}

func (r *noopFactsRepo) Load() (domain.FactsStore, error) {
	return domain.FactsStore{Facts: make(map[string]string)}, nil
}
func (r *noopFactsRepo) Save(_ domain.FactsStore) error { return nil }

type noopWMRepo struct{}

func (r *noopWMRepo) Load() (domain.WorkingMemory, error) {
	return domain.WorkingMemory{Facts: make(map[string]string)}, nil
}
func (r *noopWMRepo) Save(_ domain.WorkingMemory) error { return nil }

type noopLTMRepo struct{}

func (r *noopLTMRepo) Load() (domain.LongTermMemory, error) {
	return domain.LongTermMemory{Entries: make(map[string]string)}, nil
}
func (r *noopLTMRepo) Save(_ domain.LongTermMemory) error { return nil }

type noopProfileRepo struct{}

func (r *noopProfileRepo) Load() (domain.UserProfile, error) {
	return domain.UserProfile{Preferences: make(map[string]string)}, nil
}
func (r *noopProfileRepo) Save(_ domain.UserProfile) error { return nil }

// noopInvariantsRepo is a no-op InvariantsRepository (no invariants active).
type noopInvariantsRepo struct{}

func (r *noopInvariantsRepo) Load() (string, error) { return "", nil }

// stubInvariantsRepo returns a fixed invariants document for testing.
type stubInvariantsRepo struct{ content string }

func (r *stubInvariantsRepo) Load() (string, error) { return r.content, nil }

// capturingLLM records every LLMRequest it receives, in addition to returning a fixed reply.
// If replyFn is set it is called instead of using the reply field.
type capturingLLM struct {
	reply    string
	replyFn  func() string
	captured []port.LLMRequest
}

func (s *capturingLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	s.captured = append(s.captured, req)
	r := s.reply
	if s.replyFn != nil {
		r = s.replyFn()
	}
	return port.LLMResponse{Content: r, Usage: domain.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
}

// buildAgent wires an AgentUseCase with a stub LLM and test-supplied stdin/stdout.
func buildAgent(llmReply string, stdin string, taskRepo *memoryTaskRepo) (*AgentUseCase, *bytes.Buffer) {
	stub := &stubLLM{reply: llmReply}
	chatUC := NewChatUseCase(
		stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{},
	)
	agent := NewAgentUseCase(chatUC, taskRepo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	agent.in = strings.NewReader(stdin)
	agent.out = out
	agent.err = out
	return agent, out
}

// ── FSM unit tests ────────────────────────────────────────────────────────────

func TestExecuteTaskFSM_HappyPath(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan, continue execution, approve result
	agent, out := buildAgent("LLM response", "y\ny\ny\n", repo)

	ts := domain.NewTaskState("t1", "test task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("y\ny\ny\n")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.state != nil {
		t.Error("task file should be cleared after done")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("output should mention DONE phase")
	}
}

func TestExecuteTaskFSM_PauseAtPlanning(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("the plan", "pause\n", repo) // pause at plan approval

	ts := domain.NewTaskState("t1", "some task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("pause\n")), ts, "", ChatConfig{})
	if err == nil || err.Error() != ErrTaskPaused.Error() {
		t.Fatalf("expected ErrTaskPaused, got %v", err)
	}
	if repo.state == nil {
		t.Fatal("task state should be saved after pause")
	}
	if repo.state.Phase != domain.PhasePlanning {
		t.Errorf("paused phase should be planning, got %s", repo.state.Phase)
	}
}

func TestExecuteTaskFSM_PauseAtExecution(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → LLM runs → pause at execution continue-prompt
	input := "y\npause\n"
	agent, _ := buildAgent("the result", input, repo)

	ts := domain.NewTaskState("t1", "some task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err == nil || err.Error() != ErrTaskPaused.Error() {
		t.Fatalf("expected ErrTaskPaused, got %v", err)
	}
	if repo.state == nil {
		t.Fatal("task state should be saved after pause")
	}
	if repo.state.Phase != domain.PhaseExecution {
		t.Errorf("paused phase should be execution, got %s", repo.state.Phase)
	}
	if repo.state.Result != "the result" {
		t.Errorf("result should be persisted before pause, got %q", repo.state.Result)
	}
}

func TestExecuteTaskFSM_ResumeFromExecution(t *testing.T) {
	repo := &memoryTaskRepo{}
	// Resume with pre-computed result: approve execution-continue, approve validation
	input := "y\ny\n"
	agent, out := buildAgent("ignored", input, repo)

	ts := domain.TaskState{
		ID:        "t1",
		Phase:     domain.PhaseExecution,
		Task:      "test",
		Plan:      "the plan",
		Result:    "pre-computed result",
		Iteration: 1,
		CreatedAt: time.Now().Format(time.RFC3339),
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "pre-computed result") {
		t.Error("pre-computed result should be displayed on resume from execution")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete after resume from execution")
	}
}

func TestExecuteTaskFSM_PauseAtValidation(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → continue execution → pause at validation
	input := "y\ny\npause\n"
	agent, _ := buildAgent("result", input, repo)

	ts := domain.NewTaskState("t1", "some task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err == nil || err.Error() != ErrTaskPaused.Error() {
		t.Fatalf("expected ErrTaskPaused, got %v", err)
	}
	if repo.state == nil {
		t.Fatal("task state should be saved after pause")
	}
	if repo.state.Phase != domain.PhaseValidation {
		t.Errorf("paused phase should be validation, got %s", repo.state.Phase)
	}
	if repo.state.Result != "result" {
		t.Errorf("result should be saved before pause, got %q", repo.state.Result)
	}
}

func TestExecuteTaskFSM_ResumeFromValidation(t *testing.T) {
	repo := &memoryTaskRepo{}
	// Resume directly at validation: result must be shown (was not shown in execution this session)
	agent, out := buildAgent("ignored", "y\n", repo)

	ts := domain.TaskState{
		ID:        "t1",
		Phase:     domain.PhaseValidation,
		Task:      "test",
		Plan:      "the plan",
		Result:    "the result",
		Iteration: 1,
		CreatedAt: time.Now().Format(time.RFC3339),
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("y\n")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "the result") {
		t.Error("existing result should be displayed when resuming at validation")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete after validation approval")
	}
}

// Regression: after validation rejection the Result field must be cleared so
// the next EXECUTION round re-runs the LLM instead of showing the stale result.
func TestExecuteTaskFSM_RejectAtValidation_ClearsResult(t *testing.T) {
	calls := 0
	stub := &stubLLM{} // reply changes per call
	stub.replyFn = func() string {
		calls++
		return fmt.Sprintf("result-call-%d", calls)
	}
	repo := &memoryTaskRepo{}
	chatUC := NewChatUseCase(
		stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{},
	)
	agent := NewAgentUseCase(chatUC, repo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	agent.out = out
	agent.err = out

	// approve plan, continue execution, reject result with feedback,
	// approve re-plan, continue execution again, approve result
	input := "y\ny\nneeds revision\ny\ny\ny\n"
	agent.in = strings.NewReader(input)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// LLM should have been called twice: once for planning, once for execution in round 1,
	// once for re-planning, once for execution in round 2 = 4 calls minimum.
	// (stubLLM serves all phases: planning + execution)
	if calls < 4 {
		t.Errorf("expected ≥4 LLM calls (2 plan + 2 exec), got %d", calls)
	}
	// Both execution results must appear (not the same stale one twice)
	if !strings.Contains(out.String(), "result-call-2") {
		t.Error("first execution result should appear in output")
	}
	if !strings.Contains(out.String(), "result-call-4") {
		t.Error("second execution result should appear in output (new LLM call after rejection)")
	}
}

func TestExecuteTaskFSM_PausePreservesFeedback(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("a plan", "pause\n", repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	extraCtx := "some prior feedback"
	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("pause\n")), ts, extraCtx, ChatConfig{})
	if !isErrTaskPaused(err) {
		t.Fatalf("expected ErrTaskPaused, got %v", err)
	}
	if repo.state.PendingFeedback != extraCtx {
		t.Errorf("PendingFeedback should be %q, got %q", extraCtx, repo.state.PendingFeedback)
	}
}

func TestExecuteTaskFSM_RejectPlanThenApprove(t *testing.T) {
	repo := &memoryTaskRepo{}
	// reject plan, approve revised plan, continue execution, approve result
	input := "needs more detail\ny\ny\ny\n"
	agent, out := buildAgent("plan", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "re-plan #2") {
		t.Error("second planning round should show re-plan label")
	}
}


// ── Task description gate tests ───────────────────────────────────────────────

// TestExecuteTaskFSM_TaskViolatesInvariants verifies that when the user's task
// description itself conflicts with invariants, the FSM refuses immediately on
// the first LLM call (the task check), clears the task state, and returns nil
// without generating a plan or prompting the user.
//
// LLM call sequence:
//  1. task description check → "VIOLATION: ..."
func TestExecuteTaskFSM_TaskViolatesInvariants(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		return "VIOLATION: task explicitly requests gin framework which violates 'Go standard library only'"
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	// no user input needed — FSM should refuse before any prompt
	agent.in = strings.NewReader("")
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build a REST API using the gin framework")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("expected nil (clean refusal), got error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 LLM call (task check), got %d", calls)
	}
	if repo.state != nil {
		t.Error("task state should be cleared after refusal")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Task Refused") {
		t.Error("output should contain 'Task Refused' banner")
	}
	if !strings.Contains(outStr, "VIOLATION") {
		t.Error("output should contain the violation description")
	}
	if strings.Contains(outStr, "Proposed Plan") {
		t.Error("no plan should be shown when the task is refused")
	}
}

// TestExecuteTaskFSM_TaskCompliantProceedsToPlanning verifies that when the task
// description is compliant, the task check passes and planning proceeds normally.
//
// LLM call sequence:
//  1. task description check → "COMPLIANT"
//  2. planning                → "the plan"
//  3. plan compliance check   → "COMPLIANT"
//  4. execution               → "result"
//  5. execution compliance check → "COMPLIANT"
func TestExecuteTaskFSM_TaskCompliantProceedsToPlanning(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "COMPLIANT" // task check
		case 2:
			return "the plan"
		case 3:
			return "COMPLIANT" // plan check
		case 4:
			return "result"
		case 5:
			return "COMPLIANT" // exec check
		default:
			return "ok"
		}
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build a stdlib HTTP server")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 5 {
		t.Errorf("expected 5 LLM calls (task-check + plan + plan-check + exec + exec-check), got %d", calls)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete normally when task description is compliant")
	}
}

// TestExecuteTaskFSM_TaskCheckNotRepeatedOnReplan verifies that the task description
// check runs only on the first iteration (Iteration==1). When the FSM returns to
// PLANNING due to a user rejection (Iteration==2), the task check must NOT fire again.
//
// LLM call sequence (no invariants on task check for iteration 2):
//  1. task check (iter 1)     → "COMPLIANT"
//  2. plan attempt 1          → "plan v1"
//  3. plan check              → "COMPLIANT"
//     user rejects plan v1
//  4. plan attempt 2 (iter 2) → "plan v2"   ← no task check here
//  5. plan check              → "COMPLIANT"
//  6. execution               → "result"
//  7. exec check              → "COMPLIANT"
func TestExecuteTaskFSM_TaskCheckNotRepeatedOnReplan(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	replies := []string{
		"COMPLIANT", // task check (iter 1)
		"plan v1",
		"COMPLIANT", // plan check v1
		// user rejects → iter 2 (no task check)
		"plan v2",
		"COMPLIANT", // plan check v2
		"result",
		"COMPLIANT", // exec check
	}
	idx := 0
	stub := &capturingLLM{replyFn: func() string {
		if idx < len(replies) {
			r := replies[idx]
			idx++
			return r
		}
		return "ok"
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	// reject plan v1, approve plan v2, continue, approve result
	input := "needs revision\ny\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build a stdlib HTTP server")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 7 {
		t.Errorf("expected exactly 7 LLM calls, got %d (task-check must NOT repeat on iter 2)", idx)
	}
}

// ── Planning compliance tests ─────────────────────────────────────────────────

// TestExecuteTaskFSM_PlanCompliantFirstAttempt verifies that when a plan passes
// the invariant check on the first attempt it is shown to the user immediately
// with no warning banner.
//
// LLM call sequence:
//  1. task description check → "COMPLIANT"
//  2. planning               → "compliant plan"
//  3. plan compliance check  → "COMPLIANT"
//  4. execution              → "execution result"
//  5. execution check        → "COMPLIANT"
func TestExecuteTaskFSM_PlanCompliantFirstAttempt(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "COMPLIANT" // task description check
		case 2:
			return "compliant plan"
		case 3:
			return "COMPLIANT" // plan compliance check
		case 4:
			return "execution result"
		case 5:
			return "COMPLIANT" // execution compliance check
		default:
			return "ok"
		}
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.String(), "Compliance Warning") {
		t.Error("should not show compliance warning when plan is compliant on first attempt")
	}
	if !strings.Contains(out.String(), "compliant plan") {
		t.Error("compliant plan should be shown to user")
	}
}

// TestExecuteTaskFSM_PlanViolationSilentRetry verifies that when a plan fails
// the compliance check on attempt 1 but passes on attempt 2, the user only
// ever sees the compliant plan (no warning banner, no violation visible).
//
// LLM call sequence:
//  1. task description check → "COMPLIANT"
//  2. planning attempt 1     → "bad plan"
//  3. plan check attempt 1   → "VIOLATION: uses gin"
//  4. planning attempt 2     → "good plan"
//  5. plan check attempt 2   → "COMPLIANT"
//  6. execution              → "result"
//  7. execution check        → "COMPLIANT"
func TestExecuteTaskFSM_PlanViolationSilentRetry(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	replies := []string{
		"COMPLIANT", // task description check
		"bad plan",
		"VIOLATION: uses gin framework",
		"good plan",
		"COMPLIANT",
		"result",
		"COMPLIANT",
	}
	idx := 0
	stub := &capturingLLM{replyFn: func() string {
		if idx < len(replies) {
			r := replies[idx]
			idx++
			return r
		}
		return "ok"
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 7 {
		t.Errorf("expected 7 LLM calls, got %d", idx)
	}
	outStr := out.String()
	if strings.Contains(outStr, "bad plan") {
		t.Error("violating plan should not be shown to the user")
	}
	if !strings.Contains(outStr, "good plan") {
		t.Error("compliant plan should be shown to the user")
	}
	if strings.Contains(outStr, "Compliance Warning") {
		t.Error("no warning banner expected when retry succeeds")
	}
}

// TestExecuteTaskFSM_PlanViolationAllAttemptsFail verifies that when all
// maxPlanAttempts plans violate invariants, the last plan is shown to the
// user WITH a compliance warning banner, and the task can still complete
// if the user approves it.
//
// LLM call sequence (maxPlanAttempts=3):
//  1. task description check → "COMPLIANT"
//  2. plan attempt 1  → "bad plan 1"
//  3. check 1         → "VIOLATION: ..."
//  4. plan attempt 2  → "bad plan 2"
//  5. check 2         → "VIOLATION: ..."
//  6. plan attempt 3  → "bad plan 3"
//  7. check 3         → "VIOLATION: ..."
//  8. execution       → "result"
//  9. exec check      → "COMPLIANT"
func TestExecuteTaskFSM_PlanViolationAllAttemptsFail(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	idx := 0
	stub := &capturingLLM{replyFn: func() string {
		idx++
		switch idx {
		case 1:
			return "COMPLIANT" // task description check
		case 2, 4, 6:
			return fmt.Sprintf("bad plan %d", (idx)/2)
		case 3, 5, 7:
			return "VIOLATION: uses external library"
		case 8:
			return "execution result"
		case 9:
			return "COMPLIANT"
		default:
			return "ok"
		}
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	input := "y\ny\ny\n" // user approves anyway
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 9 {
		t.Errorf("expected 9 LLM calls, got %d", idx)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Compliance Warning") {
		t.Error("warning banner expected when all plan attempts violate invariants")
	}
	if !strings.Contains(outStr, "bad plan 3") {
		t.Error("last attempted plan should be shown to the user")
	}
	if !strings.Contains(outStr, "DONE") {
		t.Error("task should complete after user approves the warned plan")
	}
}

// ── Invariants unit tests ─────────────────────────────────────────────────────

// TestExecuteTaskFSM_ValidationInvariantViolation_AutoReplanning verifies that
// when the execution result violates an invariant the FSM automatically rejects
// it and returns to PLANNING without asking the user, then completes normally
// after a compliant re-execution.
//
// LLM call sequence:
//  1. task description check   → "COMPLIANT"
//  2. planning (v1)            → "plan v1"
//  3. plan check (v1)          → "COMPLIANT"
//  4. execution (v1)           → "bad result"
//  5. exec compliance check    → "VIOLATION: uses gin framework"
//  6. planning (v2)            → "plan v2"   [iter 2 — no task check]
//  7. plan check (v2)          → "COMPLIANT"
//  8. execution (v2)           → "good result"
//  9. exec compliance check    → "COMPLIANT"
//
// User input: y · y · y · y · y
func TestExecuteTaskFSM_ValidationInvariantViolation_AutoReplanning(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"

	replies := []string{
		"COMPLIANT", // task description check (iter 1 only)
		"plan v1",
		"COMPLIANT", // plan v1 passes plan-check
		"bad result",
		"VIOLATION: uses gin framework, violates 'Go standard library only'",
		"plan v2",
		"COMPLIANT", // plan v2 passes plan-check
		"good result",
		"COMPLIANT",
	}
	callIdx := 0
	stub := &capturingLLM{replyFn: func() string {
		if callIdx < len(replies) {
			r := replies[callIdx]
			callIdx++
			return r
		}
		return "ok"
	}}

	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	// approve plan v1 · continue to validation · approve plan v2 · continue · accept result
	input := "y\ny\ny\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callIdx != 9 {
		t.Errorf("expected 9 LLM calls, got %d", callIdx)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "VIOLATION") {
		t.Error("output should report the invariant violation")
	}
	if !strings.Contains(outStr, "re-plan #2") {
		t.Error("output should show re-planning iteration after violation")
	}
	if !strings.Contains(outStr, "DONE") {
		t.Error("task should complete after compliant re-execution")
	}
	if repo.state != nil {
		t.Error("task file should be cleared after DONE")
	}
}

// TestExecuteTaskFSM_ValidationCompliancePass_ProceedsToUserPrompt verifies that
// when all compliance checks pass the FSM proceeds to the normal user validation
// prompt and completes normally on approval.
//
// LLM call sequence:
//  1. task description check → "COMPLIANT"
//  2. planning               → "the plan"
//  3. plan check             → "COMPLIANT"
//  4. execution              → "execution result"
//  5. exec check             → "COMPLIANT"
//
// User input: y · y · y
func TestExecuteTaskFSM_ValidationCompliancePass_ProceedsToUserPrompt(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"

	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "COMPLIANT" // task description check
		case 2:
			return "the plan"
		case 3:
			return "COMPLIANT" // plan check
		case 4:
			return "execution result"
		case 5:
			return "COMPLIANT" // execution check
		default:
			return "ok"
		}
	}}

	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 5 {
		t.Errorf("expected 5 LLM calls (task-check + plan + plan-check + execution + exec-check), got %d", calls)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete normally when all compliance checks pass")
	}
}

// TestExecuteTaskFSM_InvariantsInjectedIntoSystemPrompt verifies that when an
// InvariantsRepository returns a non-empty document it appears verbatim in the
// system message sent to the LLM for both the PLANNING and EXECUTION phases.
func TestExecuteTaskFSM_InvariantsInjectedIntoSystemPrompt(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only\n- No external databases"

	stub := &capturingLLM{reply: "the plan"}
	chatUC := NewChatUseCase(
		stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{},
	)
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &stubInvariantsRepo{content: invDoc})
	out := &bytes.Buffer{}
	agent.in = strings.NewReader("y\ny\ny\n")
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("y\ny\ny\n")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls (planning + execution), got %d", len(stub.captured))
	}

	for i, req := range stub.captured[:2] { // planning=0, execution=1
		if len(req.Messages) == 0 {
			t.Fatalf("call %d: no messages", i)
		}
		sys := req.Messages[0]
		if sys.Role != domain.RoleSystem {
			t.Fatalf("call %d: first message role=%s, want system", i, sys.Role)
		}
		if !strings.Contains(sys.Content, "Go standard library only") {
			t.Errorf("call %d: invariant text not found in system message", i)
		}
		if !strings.Contains(sys.Content, "INVARIANTS") {
			t.Errorf("call %d: INVARIANTS header not found in system message", i)
		}
		if !strings.Contains(sys.Content, "NEVER VIOLATE") {
			t.Errorf("call %d: NEVER VIOLATE instruction not found in system message", i)
		}
	}
}

// TestExecuteTaskFSM_NoInvariants_NoInjection ensures that when no invariants
// document is present the system prompt is built without any invariants block,
// and the FSM still completes normally.
func TestExecuteTaskFSM_NoInvariants_NoInjection(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	chatUC := NewChatUseCase(
		stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{},
	)
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	agent.in = strings.NewReader("y\ny\ny\n")
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("y\ny\ny\n")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, req := range stub.captured {
		if len(req.Messages) == 0 {
			continue
		}
		sys := req.Messages[0]
		if sys.Role == domain.RoleSystem && strings.Contains(sys.Content, "INVARIANTS") {
			t.Errorf("call %d: INVARIANTS block should not appear when no invariants are defined", i)
		}
	}
}

// ── prompt() unit test ────────────────────────────────────────────────────────

func TestPrompt_Pause(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/pause\n", "pause\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.prompt(r, "question?")
		if approved || !paused || feedback != "" || err != nil {
			t.Errorf("input %q: got approved=%v paused=%v feedback=%q err=%v", input, approved, paused, feedback, err)
		}
	}
}

func TestPrompt_Yes(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"y\n", "yes\n", "Y\n", "YES\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, _, err := agent.prompt(r, "?")
		if !approved || paused || err != nil {
			t.Errorf("input %q: expected approved=true paused=false err=nil", input)
		}
	}
}

func TestPrompt_FeedbackText(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("needs work\n"))
	approved, paused, feedback, err := agent.prompt(r, "?")
	if approved || paused || feedback != "needs work" || err != nil {
		t.Errorf("got approved=%v paused=%v feedback=%q err=%v", approved, paused, feedback, err)
	}
}

func TestPrompt_ExitCommand(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/exit\n", "/quit\n", "/EXIT\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.prompt(r, "?")
		if approved || paused || feedback != "" || !errors.Is(err, ErrExitRequested) {
			t.Errorf("input %q: expected ErrExitRequested, got approved=%v paused=%v feedback=%q err=%v",
				input, approved, paused, feedback, err)
		}
	}
}

func TestPrompt_RestartCommand(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("/restart\n"))
	approved, paused, feedback, err := agent.prompt(r, "?")
	if approved || paused || feedback != "" || !errors.Is(err, ErrRestartRequested) {
		t.Errorf("expected ErrRestartRequested, got approved=%v paused=%v feedback=%q err=%v",
			approved, paused, feedback, err)
	}
}

// ── Memory-layer stub repos ───────────────────────────────────────────────────

type stubWMRepo struct{ data domain.WorkingMemory }

func (r *stubWMRepo) Load() (domain.WorkingMemory, error) { return r.data, nil }
func (r *stubWMRepo) Save(_ domain.WorkingMemory) error   { return nil }

type stubLTMRepo struct{ data domain.LongTermMemory }

func (r *stubLTMRepo) Load() (domain.LongTermMemory, error) { return r.data, nil }
func (r *stubLTMRepo) Save(_ domain.LongTermMemory) error   { return nil }

type stubProfileRepo struct{ data domain.UserProfile }

func (r *stubProfileRepo) Load() (domain.UserProfile, error) { return r.data, nil }
func (r *stubProfileRepo) Save(_ domain.UserProfile) error   { return nil }

// ── Memory layer injection tests ──────────────────────────────────────────────

// TestExecuteTaskFSM_WMInjectedIntoSystemPrompt verifies that working-memory facts
// are present in the system message sent to the LLM during the PLANNING phase.
func TestExecuteTaskFSM_WMInjectedIntoSystemPrompt(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	wm := domain.WorkingMemory{Facts: map[string]string{"current_lang": "Go"}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &stubWMRepo{data: wm}, &noopLTMRepo{}, &noopProfileRepo{})
	taskRepo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, taskRepo, &noopInvariantsRepo{})
	buf := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = buf
	agent.err = buf

	ts := domain.NewTaskState("t1", "task")
	_ = taskRepo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 1 {
		t.Fatal("no LLM calls captured")
	}
	sys := stub.captured[0].Messages[0] // planning call system message
	if sys.Role != domain.RoleSystem {
		t.Fatalf("first message role=%s, want system", sys.Role)
	}
	if !strings.Contains(sys.Content, "Working Memory") {
		t.Error("WM block header not found in planning system message")
	}
	if !strings.Contains(sys.Content, "current_lang") || !strings.Contains(sys.Content, "Go") {
		t.Error("WM fact value not found in planning system message")
	}
}

// TestExecuteTaskFSM_LTMInjectedIntoSystemPrompt verifies that long-term memory
// entries appear in the system message for both PLANNING and EXECUTION phases.
func TestExecuteTaskFSM_LTMInjectedIntoSystemPrompt(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	ltm := domain.LongTermMemory{Entries: map[string]string{"preferred_style": "clean architecture"}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &stubLTMRepo{data: ltm}, &noopProfileRepo{})
	taskRepo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, taskRepo, &noopInvariantsRepo{})
	buf := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = buf
	agent.err = buf

	ts := domain.NewTaskState("t1", "task")
	_ = taskRepo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls, got %d", len(stub.captured))
	}
	for i := 0; i < 2; i++ { // planning=0, execution=1
		sys := stub.captured[i].Messages[0]
		if sys.Role != domain.RoleSystem {
			t.Fatalf("call %d: first message role=%s, want system", i, sys.Role)
		}
		if !strings.Contains(sys.Content, "Long-term Memory") {
			t.Errorf("call %d: LTM block header not found in system message", i)
		}
		if !strings.Contains(sys.Content, "clean architecture") {
			t.Errorf("call %d: LTM entry value not found in system message", i)
		}
	}
}

// TestExecuteTaskFSM_ProfileInjectedIntoSystemPrompt verifies that the user profile
// appears in the system message for both PLANNING and EXECUTION phases.
func TestExecuteTaskFSM_ProfileInjectedIntoSystemPrompt(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	profile := domain.UserProfile{
		Name:        "Alice",
		Preferences: map[string]string{"output_lang": "English"},
	}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &stubProfileRepo{data: profile})
	taskRepo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, taskRepo, &noopInvariantsRepo{})
	buf := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = buf
	agent.err = buf

	ts := domain.NewTaskState("t1", "task")
	_ = taskRepo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls, got %d", len(stub.captured))
	}
	for i := 0; i < 2; i++ {
		sys := stub.captured[i].Messages[0]
		if sys.Role != domain.RoleSystem {
			t.Fatalf("call %d: first message role=%s, want system", i, sys.Role)
		}
		if !strings.Contains(sys.Content, "User Profile") {
			t.Errorf("call %d: profile block not found in system message", i)
		}
		if !strings.Contains(sys.Content, "Alice") {
			t.Errorf("call %d: profile name not found in system message", i)
		}
	}
}

// TestExecuteTaskFSM_SystemFlagPreservedInFSMCalls verifies that when the caller
// supplies a SystemMessage (--system flag), it is appended to both the PLANNING
// and EXECUTION phase system prompts rather than being silently dropped.
func TestExecuteTaskFSM_SystemFlagPreservedInFSMCalls(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	taskRepo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, taskRepo, &noopInvariantsRepo{})
	buf := &bytes.Buffer{}
	input := "y\ny\ny\n"
	agent.in = strings.NewReader(input)
	agent.out = buf
	agent.err = buf

	ts := domain.NewTaskState("t1", "task")
	_ = taskRepo.Save(ts)
	cfg := ChatConfig{SystemMessage: "You are an expert Go developer."}
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls, got %d", len(stub.captured))
	}
	for i := 0; i < 2; i++ {
		sys := stub.captured[i].Messages[0]
		if sys.Role != domain.RoleSystem {
			t.Fatalf("call %d: first message role=%s, want system", i, sys.Role)
		}
		if !strings.Contains(sys.Content, "You are an expert Go developer.") {
			t.Errorf("call %d: --system content not found in phase system message", i)
		}
	}
}

// ── buildPlanningPrompt unit tests ────────────────────────────────────────────

func TestBuildPlanningPrompt_Initial(t *testing.T) {
	p := buildPlanningPrompt("do X", "", false)
	if !strings.Contains(p, "Task: do X") {
		t.Error("task not included")
	}
	if !strings.Contains(p, "Analyse this task") {
		t.Error("initial instruction not included")
	}
}

func TestBuildPlanningPrompt_UserFeedback(t *testing.T) {
	p := buildPlanningPrompt("do X", "needs more detail", false)
	if !strings.Contains(p, "Revision feedback from user") {
		t.Error("user feedback label not included")
	}
	if !strings.Contains(p, "needs more detail") {
		t.Error("feedback content not included")
	}
	if !strings.Contains(p, "Please revise your plan") {
		t.Error("revision instruction not included")
	}
}

func TestBuildPlanningPrompt_TargetedFix(t *testing.T) {
	violation := "INVARIANT VIOLATION — targeted fix required.\n\nFix only step 3."
	p := buildPlanningPrompt("do X", violation, true)
	if strings.Contains(p, "Revision feedback from user") {
		t.Error("targeted fix should NOT use 'Revision feedback from user' label")
	}
	if !strings.Contains(p, violation) {
		t.Error("violation content not included")
	}
	if !strings.Contains(p, "Do not replan steps") {
		t.Error("targeted fix instruction not included")
	}
}

// ── Slash-command FSM tests ───────────────────────────────────────────────────

// TestExecuteTaskFSM_ExitDuringPlanning verifies that /exit at the plan-approval
// prompt bubbles up ErrExitRequested out of executeTaskFSM.
func TestExecuteTaskFSM_ExitDuringPlanning(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("a plan", "/exit\n", repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/exit\n")), ts, "", ChatConfig{})
	if !errors.Is(err, ErrExitRequested) {
		t.Fatalf("expected ErrExitRequested, got %v", err)
	}
}

// TestExecuteTaskFSM_RestartDuringPlanning verifies that /restart at the
// plan-approval prompt bubbles up ErrRestartRequested out of executeTaskFSM.
func TestExecuteTaskFSM_RestartDuringPlanning(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("a plan", "/restart\n", repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/restart\n")), ts, "", ChatConfig{})
	if !errors.Is(err, ErrRestartRequested) {
		t.Fatalf("expected ErrRestartRequested, got %v", err)
	}
}

// TestExecuteTaskFSM_ExitDuringExecution verifies /exit at the execution
// continue-prompt also propagates ErrExitRequested.
func TestExecuteTaskFSM_ExitDuringExecution(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → LLM runs → /exit at "continue to validation?"
	input := "y\n/exit\n"
	agent, _ := buildAgent("result", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if !errors.Is(err, ErrExitRequested) {
		t.Fatalf("expected ErrExitRequested, got %v", err)
	}
}

// TestExecuteTaskFSM_RestartDuringValidation verifies /restart at the validation
// prompt propagates ErrRestartRequested and does not hang.
func TestExecuteTaskFSM_RestartDuringValidation(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → continue → /restart at validation
	input := "y\ny\n/restart\n"
	agent, _ := buildAgent("result", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if !errors.Is(err, ErrRestartRequested) {
		t.Fatalf("expected ErrRestartRequested, got %v", err)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isErrTaskPaused(err error) bool {
	return err != nil && err.Error() == ErrTaskPaused.Error()
}

// Silence unused import warning for fmt when tests don't use it directly.
var _ = fmt.Sprintf
