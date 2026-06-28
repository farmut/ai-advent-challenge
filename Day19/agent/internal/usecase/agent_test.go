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
	agent, out := buildAgent("LLM response", "/yes\n/yes\n/yes\n", repo)

	ts := domain.NewTaskState("t1", "test task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/yes\n/yes\n/yes\n")), ts, "", ChatConfig{})
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
	input := "/yes\npause\n"
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
	input := "/yes\n/yes\n"
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
	input := "/yes\n/yes\npause\n"
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
	agent, out := buildAgent("ignored", "/yes\n", repo)

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

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/yes\n")), ts, "", ChatConfig{})
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
	input := "/yes\n/yes\nneeds revision\n/yes\n/yes\n/yes\n"
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
	input := "needs more detail\n/yes\n/yes\n/yes\n"
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

// TestExecuteTaskFSM_TaskGateDisabled verifies that the task-description invariant
// gate is disabled: even a task that explicitly conflicts with invariants proceeds
// to planning without being refused up front.
//
// LLM call sequence (no task-check call expected):
//  1. planning     → "the plan"
//  2. plan check   → "PASS"
//  3. execution    → "result"
//  4. exec check   → "PASS"
func TestExecuteTaskFSM_TaskGateDisabled(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "the plan"
		case 2:
			return "PASS" // plan check
		case 3:
			return "result"
		case 4:
			return "PASS" // exec check
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	// Task description explicitly conflicts with invariants.
	ts := domain.NewTaskState("t1", "build a REST API using the gin framework")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must NOT refuse up front: planning must run as call 1.
	if strings.Contains(out.String(), "Task Refused") {
		t.Error("task-gate is disabled — should not refuse the task up front")
	}
	if calls != 4 {
		t.Errorf("expected 4 LLM calls (plan + plan-check + exec + exec-check), got %d", calls)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete normally when task gate is disabled")
	}
}

// TestExecuteTaskFSM_TaskCompliantProceedsToPlanning verifies that planning proceeds
// normally and completes with the correct number of LLM calls (no task-gate).
//
// LLM call sequence:
//  1. planning               → "the plan"
//  2. plan compliance check  → "PASS"
//  3. execution              → "result"
//  4. execution compliance check → "PASS"
func TestExecuteTaskFSM_TaskCompliantProceedsToPlanning(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "the plan"
		case 2:
			return "PASS" // plan check
		case 3:
			return "result"
		case 4:
			return "PASS" // exec check
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build a stdlib HTTP server")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 {
		t.Errorf("expected 4 LLM calls (plan + plan-check + exec + exec-check), got %d", calls)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete normally")
	}
}

// TestExecuteTaskFSM_TaskCheckNotRepeatedOnReplan verifies that on re-planning
// (Iteration==2) only the plan and plan-check calls run — no extra gate call.
//
// LLM call sequence:
//  1. plan attempt 1  → "plan v1"
//  2. plan check      → "PASS"
//     user rejects plan v1
//  3. plan attempt 2  → "plan v2"
//  4. plan check      → "PASS"
//  5. execution       → "result"
//  6. exec check      → "PASS"
func TestExecuteTaskFSM_TaskCheckNotRepeatedOnReplan(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	replies := []string{
		"plan v1",
		"PASS", // plan check v1
		// user rejects → iter 2
		"plan v2",
		"PASS", // plan check v2
		"result",
		"PASS", // exec check
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
	input := "needs revision\n/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build a stdlib HTTP server")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 6 {
		t.Errorf("expected exactly 6 LLM calls, got %d", idx)
	}
}

// ── Planning compliance tests ─────────────────────────────────────────────────

// TestExecuteTaskFSM_PlanCompliantFirstAttempt verifies that when a plan passes
// the invariant check on the first attempt it is shown to the user immediately
// with no warning banner.
//
// LLM call sequence:
//  1. planning               → "compliant plan"
//  2. plan compliance check  → "PASS"
//  3. execution              → "execution result"
//  4. execution check        → "PASS"
func TestExecuteTaskFSM_PlanCompliantFirstAttempt(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "compliant plan"
		case 2:
			return "PASS" // plan compliance check
		case 3:
			return "execution result"
		case 4:
			return "PASS" // execution compliance check
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
	input := "/yes\n/yes\n/yes\n"
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
//  1. planning attempt 1     → "bad plan"
//  2. plan check attempt 1   → "FAIL\n- uses gin"
//  3. planning attempt 2     → "good plan"
//  4. plan check attempt 2   → "PASS"
//  5. execution              → "result"
//  6. execution check        → "PASS"
func TestExecuteTaskFSM_PlanViolationSilentRetry(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	replies := []string{
		"bad plan",
		"FAIL\n- uses gin framework",
		"good plan",
		"PASS",
		"result",
		"PASS",
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 6 {
		t.Errorf("expected 6 LLM calls, got %d", idx)
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
//  1. plan attempt 1  → "bad plan 1"
//  2. check 1         → "FAIL\n- ..."
//  3. plan attempt 2  → "bad plan 2"
//  4. check 2         → "FAIL\n- ..."
//  5. plan attempt 3  → "bad plan 3"
//  6. check 3         → "FAIL\n- ..."
//  7. execution       → "result"
//  8. exec check      → "PASS"
func TestExecuteTaskFSM_PlanViolationAllAttemptsFail(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"
	idx := 0
	stub := &capturingLLM{replyFn: func() string {
		idx++
		switch idx {
		case 1, 3, 5:
			return fmt.Sprintf("bad plan %d", (idx+1)/2)
		case 2, 4, 6:
			return "FAIL\n- uses external library"
		case 7:
			return "execution result"
		case 8:
			return "PASS"
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
	input := "/yes\n/yes\n/yes\n" // user approves anyway
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 8 {
		t.Errorf("expected 8 LLM calls, got %d", idx)
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

// TestExecuteTaskFSM_ValidationInvariantViolation_AutoReExecution verifies that
// when the execution result violates an invariant the FSM automatically returns
// to EXECUTION (not planning) without asking the user, then completes normally
// after a compliant re-execution with the same approved plan.
//
// LLM call sequence:
//  1. planning (v1)         → "plan v1"
//  2. plan check (v1)       → "PASS"
//  3. execution (v1)        → "bad result"
//  4. exec compliance check → "FAIL\n- uses gin framework, violates 'Go standard library only'"
//  5. re-execution (v1 fix) → "good result"   [same plan, violation context injected]
//  6. exec compliance check → "PASS"
//
// User input: /yes · /yes · /yes · /yes
func TestExecuteTaskFSM_ValidationInvariantViolation_AutoReExecution(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"

	replies := []string{
		"plan v1",
		"PASS", // plan v1 passes plan-check
		"bad result",
		"FAIL\n- uses gin framework, violates 'Go standard library only'",
		"good result", // re-execution with violation context
		"PASS",
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
	// approve plan v1 · continue to validation · [auto-reexecute, no prompt] · continue to validation · accept result
	input := "/yes\n/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callIdx != 6 {
		t.Errorf("expected 6 LLM calls, got %d", callIdx)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "gin framework") {
		t.Error("output should report the invariant violation details")
	}
	if strings.Contains(outStr, "re-plan #2") {
		t.Error("output must NOT show re-planning iteration — violation returns to EXECUTION, not PLANNING")
	}
	if !strings.Contains(outStr, "EXECUTION") {
		t.Error("output should mention returning to EXECUTION for targeted fix")
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
//  1. planning               → "the plan"
//  2. plan check             → "PASS"
//  3. execution              → "execution result"
//  4. exec check             → "PASS"
//
// User input: /yes · /yes · /yes
func TestExecuteTaskFSM_ValidationCompliancePass_ProceedsToUserPrompt(t *testing.T) {
	const invDoc = "## Stack\n- Go standard library only"

	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return "the plan"
		case 2:
			return "PASS" // plan check
		case 3:
			return "execution result"
		case 4:
			return "PASS" // execution check
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 {
		t.Errorf("expected 4 LLM calls (plan + plan-check + execution + exec-check), got %d", calls)
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
	agent.in = strings.NewReader("/yes\n/yes\n/yes\n")
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/yes\n/yes\n/yes\n")), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls (planning + execution), got %d", len(stub.captured))
	}

	// With invariants active the call order is:
	//   0: planning (Execute)
	//   1: plan compliance check (DirectCall)
	//   2: execution (Execute)
	//   3: execution compliance check (DirectCall)
	// We verify calls 0 and 2 (planning and execution) both embed the invariants.
	planningCall := stub.captured[0]
	execCall := stub.captured[2]
	for i, req := range []port.LLMRequest{planningCall, execCall} { // 0=planning, 1=execution
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
	agent.in = strings.NewReader("/yes\n/yes\n/yes\n")
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("/yes\n/yes\n/yes\n")), ts, "", ChatConfig{})
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
	input := "/yes\n/yes\n/yes\n"
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
	input := "/yes\n/yes\n/yes\n"
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
	input := "/yes\n/yes\n/yes\n"
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
	input := "/yes\n/yes\n/yes\n"
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
	input := "/yes\n/exit\n"
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
	input := "/yes\n/yes\n/restart\n"
	agent, _ := buildAgent("result", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if !errors.Is(err, ErrRestartRequested) {
		t.Fatalf("expected ErrRestartRequested, got %v", err)
	}
}

// ── Phase isolation: system prompt builder unit tests ─────────────────────────

// TestPlanningSystemPrompt_PhaseIsolation verifies that the planning system prompt
// declares the PLANNING phase and does not contain execution-phase instructions.
func TestPlanningSystemPrompt_PhaseIsolation(t *testing.T) {
	p := planningSystemPrompt("")
	if !strings.Contains(p, "Current phase: PLANNING") {
		t.Error("planning prompt must declare 'Current phase: PLANNING'")
	}
	if strings.Contains(p, "Current phase: EXECUTION") {
		t.Error("planning prompt must not claim to be in EXECUTION phase")
	}
	if strings.Contains(p, "Execute every step") {
		t.Error("planning prompt must not contain execution-phase instruction 'Execute every step'")
	}
	if !strings.Contains(p, "Decompose") {
		t.Error("planning prompt must contain planning instruction 'Decompose'")
	}
}

// TestPlanningSystemPrompt_EmbedsInvariants verifies that invariants are embedded
// in the planning system prompt with the required enforcement header.
func TestPlanningSystemPrompt_EmbedsInvariants(t *testing.T) {
	inv := "## Stack\n- stdlib only"
	p := planningSystemPrompt(inv)
	if !strings.Contains(p, inv) {
		t.Error("planning prompt must embed invariants text verbatim")
	}
	if !strings.Contains(p, "INVARIANTS") {
		t.Error("planning prompt must include INVARIANTS block header")
	}
	if !strings.Contains(p, "NEVER VIOLATE") {
		t.Error("planning prompt must include NEVER VIOLATE instruction")
	}
}

// TestPlanningSystemPrompt_NoInvariantsBlockWhenEmpty verifies that no invariants block
// is injected when no invariants document is provided.
func TestPlanningSystemPrompt_NoInvariantsBlockWhenEmpty(t *testing.T) {
	p := planningSystemPrompt("")
	if strings.Contains(p, "INVARIANTS") {
		t.Error("planning prompt must not contain INVARIANTS block when none are defined")
	}
}

// TestExecutionSystemPrompt_PhaseIsolation verifies that the execution system prompt
// declares the EXECUTION phase and does not contain planning-phase instructions.
func TestExecutionSystemPrompt_PhaseIsolation(t *testing.T) {
	p := executionSystemPrompt("the plan", "")
	if !strings.Contains(p, "Current phase: EXECUTION") {
		t.Error("execution prompt must declare 'Current phase: EXECUTION'")
	}
	if strings.Contains(p, "Current phase: PLANNING") {
		t.Error("execution prompt must not claim to be in PLANNING phase")
	}
	if strings.Contains(p, "Decompose") {
		t.Error("execution prompt must not contain planning instruction 'Decompose'")
	}
	if !strings.Contains(p, "Execute every step") {
		t.Error("execution prompt must contain execution instruction 'Execute every step'")
	}
}

// TestExecutionSystemPrompt_EmbedsPlanAndInvariants verifies that the execution prompt
// contains both the approved plan text and the invariants block.
func TestExecutionSystemPrompt_EmbedsPlanAndInvariants(t *testing.T) {
	plan := "step 1: use net/http\nstep 2: write handler"
	inv := "## Stack\n- stdlib only"
	p := executionSystemPrompt(plan, inv)
	if !strings.Contains(p, plan) {
		t.Error("execution prompt must embed the approved plan text verbatim")
	}
	if !strings.Contains(p, inv) {
		t.Error("execution prompt must embed invariants text verbatim")
	}
	if !strings.Contains(p, "INVARIANTS") {
		t.Error("execution prompt must include INVARIANTS block header")
	}
}

// TestCheckerSystemPrompt_ContentSpecificity verifies that each checker system prompt
// targets the correct artefact type (task request / plan / execution result).
func TestCheckerSystemPrompt_ContentSpecificity(t *testing.T) {
	inv := "## Stack\n- stdlib only"

	taskPrompt := checkerSystemPrompt(CheckKindTask, inv)
	planPrompt := checkerSystemPrompt(CheckKindPlan, inv)
	execPrompt := checkerSystemPrompt(CheckKindExecution, inv)

	if !strings.Contains(strings.ToLower(taskPrompt), "task request") {
		t.Error("task checker prompt must mention 'task request'")
	}
	if !strings.Contains(strings.ToLower(planPrompt), "plan") {
		t.Error("plan checker prompt must mention 'plan'")
	}
	if !strings.Contains(strings.ToLower(execPrompt), "execution result") {
		t.Error("execution checker prompt must mention 'execution result'")
	}
	// Conservative note only for task kind.
	if !strings.Contains(taskPrompt, "conservative") {
		t.Error("task checker prompt must include conservative note")
	}
	if strings.Contains(planPrompt, "conservative") {
		t.Error("plan checker prompt must not include conservative note")
	}
	// Cross-contamination guards.
	if strings.Contains(strings.ToLower(execPrompt), "decompose") {
		t.Error("execution checker prompt must not contain planning instructions")
	}
	if strings.Contains(strings.ToLower(planPrompt), "execution result") {
		t.Error("plan checker prompt must not reference execution result")
	}
}

// ── Phase isolation: FSM integration tests ────────────────────────────────────

// TestFSM_PlanningPhaseUsesCorrectSystemPrompt verifies that the LLM call made
// during the PLANNING phase receives a system message that declares PLANNING
// (not EXECUTION) and contains planning responsibilities.
func TestFSM_PlanningPhaseUsesCorrectSystemPrompt(t *testing.T) {
	stub := &capturingLLM{reply: "the plan"}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without invariants: call 0 = planning, call 1 = execution.
	if len(stub.captured) < 1 {
		t.Fatal("no LLM calls captured")
	}
	planSys := stub.captured[0].Messages[0]
	if planSys.Role != domain.RoleSystem {
		t.Fatalf("first message must be system role, got %s", planSys.Role)
	}
	if !strings.Contains(planSys.Content, "PLANNING") {
		t.Error("planning LLM call system message must declare PLANNING phase")
	}
	if strings.Contains(planSys.Content, "Current phase: EXECUTION") {
		t.Error("planning LLM call must not declare EXECUTION phase")
	}
}

// TestFSM_ExecutionPhaseUsesApprovedPlan verifies that the LLM call made during
// the EXECUTION phase receives the exact plan text that the planning LLM produced
// (stored in ts.Plan after user approval) — not the original task description.
func TestFSM_ExecutionPhaseUsesApprovedPlan(t *testing.T) {
	const planText = "step 1: build http server\nstep 2: add handler"
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		if calls == 1 {
			return planText // planning reply
		}
		return "execution result"
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "original task description")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without invariants: call 0 = planning, call 1 = execution.
	if len(stub.captured) < 2 {
		t.Fatalf("expected ≥2 LLM calls, got %d", len(stub.captured))
	}
	execSys := stub.captured[1].Messages[0]
	if execSys.Role != domain.RoleSystem {
		t.Fatalf("execution call first message must be system, got %s", execSys.Role)
	}
	if !strings.Contains(execSys.Content, planText) {
		t.Error("execution LLM call must embed the approved plan text verbatim in system message")
	}
	if !strings.Contains(execSys.Content, "EXECUTION") {
		t.Error("execution LLM call system message must declare EXECUTION phase")
	}
	if strings.Contains(execSys.Content, "Current phase: PLANNING") {
		t.Error("execution LLM call must not declare PLANNING phase")
	}
}

// TestFSM_ExecutionPhaseInvariantsInjected verifies that invariants are embedded in
// the EXECUTION system message.
// With invariants active (no task gate) call order is:
// planning(0), plan-check(1), execution(2), exec-check(3) — so captured[2] is the execution call.
func TestFSM_ExecutionPhaseInvariantsInjected(t *testing.T) {
	const invDoc = "## Stack\n- stdlib only"
	replies := []string{
		"the plan",  // planning
		"PASS", // plan compliance check
		"result",    // execution
		"PASS", // execution compliance check
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "build something")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 4 {
		t.Fatalf("expected 4 LLM calls, got %d", idx)
	}
	// captured[2] is the execution call.
	execSys := stub.captured[2].Messages[0]
	if execSys.Role != domain.RoleSystem {
		t.Fatalf("execution call first message must be system, got %s", execSys.Role)
	}
	if !strings.Contains(execSys.Content, invDoc) {
		t.Error("execution LLM call system message must embed invariants text")
	}
	if !strings.Contains(execSys.Content, "INVARIANTS") {
		t.Error("execution LLM call system message must include INVARIANTS block header")
	}
}

// TestFSM_ExecutionPhase_ArbitraryTextPauses verifies that typing non-/yes input
// at the execution "Continue to validation?" prompt suspends the task.
// Only /yes advances to VALIDATION; everything else is treated as a pause signal.
func TestFSM_ExecutionPhase_ArbitraryTextPauses(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → arbitrary text at execution → task paused (no validation reached)
	input := "/yes\nsome random text\n"
	agent, _ := buildAgent("result", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if !isErrTaskPaused(err) {
		t.Fatalf("arbitrary text at execution must pause the task, got err=%v", err)
	}
	if repo.state == nil || repo.state.Phase != domain.PhaseExecution {
		t.Error("paused task should be saved at execution phase")
	}
}

// TestFSM_ValidationPhase_ComplianceCheckSendsExecutionResult verifies that the
// invariant compliance check in the VALIDATION phase sends ts.Result (the execution
// output) to the LLM checker — not ts.Plan or the original task description.
//
// With invariants (no task gate), call order:
//
//	0: planning (Execute)
//	1: plan compliance check (DirectCall)
//	2: execution (Execute)
//	3: execution compliance check (DirectCall)  ← THIS is the validation check
func TestFSM_ValidationPhase_ComplianceCheckSendsExecutionResult(t *testing.T) {
	const invDoc = "## Stack\n- stdlib only"
	const planText = "plan: use net/http"
	const resultText = "result: built stdlib HTTP server"

	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		switch calls {
		case 1:
			return planText // planning
		case 2:
			return "PASS" // plan compliance check
		case 3:
			return resultText // execution
		case 4:
			return "PASS" // execution compliance check
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
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	ts := domain.NewTaskState("t1", "the task")
	_ = repo.Save(ts)
	if err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 {
		t.Fatalf("expected 4 LLM calls, got %d", calls)
	}
	// captured[3] is the execution compliance check (validation-phase invariant check).
	complianceCall := stub.captured[3]
	if len(complianceCall.Messages) < 2 {
		t.Fatalf("compliance check must have ≥2 messages, got %d", len(complianceCall.Messages))
	}
	userMsg := complianceCall.Messages[len(complianceCall.Messages)-1].Content
	if !strings.Contains(userMsg, resultText) {
		t.Errorf("validation compliance check must send execution result; user msg: %q", userMsg)
	}
	if strings.Contains(userMsg, planText) {
		t.Errorf("validation compliance check must NOT send plan text; user msg: %q", userMsg)
	}
}

// ── Corrupt-state guard tests ─────────────────────────────────────────────────

// TestFSM_ExecutionWithEmptyPlan_ResetsToPlanning verifies that when the FSM is
// resumed at PhaseExecution with an empty Plan (tampered state file), it resets
// to PhasePlanning and continues normally rather than running the LLM with no plan.
func TestFSM_ExecutionWithEmptyPlan_ResetsToPlanning(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan → continue execution → approve validation → DONE
	input := "/yes\n/yes\n/yes\n"
	agent, out := buildAgent("llm response", input, repo)

	ts := domain.TaskState{
		ID:        "t1",
		Phase:     domain.PhaseExecution,
		Task:      "do something",
		Plan:      "", // corrupt: execution reached without an approved plan
		Result:    "",
		Iteration: 1,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("FSM should recover and complete normally after resetting to planning")
	}
	if !strings.Contains(out.String(), "PLANNING") {
		t.Error("FSM should have passed through PLANNING after the corrupt-state reset")
	}
}

// TestFSM_ValidationWithEmptyResult_ResetsToExecution verifies that when the FSM is
// resumed at PhaseValidation with an empty Result (tampered state file), it resets
// to PhaseExecution, runs the LLM, and then validates normally.
func TestFSM_ValidationWithEmptyResult_ResetsToExecution(t *testing.T) {
	repo := &memoryTaskRepo{}
	// continue execution → approve validation → DONE
	input := "/yes\n/yes\n"
	agent, out := buildAgent("execution result", input, repo)

	ts := domain.TaskState{
		ID:        "t1",
		Phase:     domain.PhaseValidation,
		Task:      "do something",
		Plan:      "approved plan",
		Result:    "", // corrupt: validation reached without execution result
		Iteration: 1,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("FSM should recover and complete normally after resetting to execution")
	}
	if !strings.Contains(out.String(), "execution result") {
		t.Error("FSM should have run execution and shown the result after the corrupt-state reset")
	}
}

// TestFSM_UnknownPhase_ReturnsError verifies that a tampered task state file with
// an unrecognised phase value does not cause an infinite loop. The FSM must detect
// the unknown phase, clear the task, and return an error.
func TestFSM_UnknownPhase_ReturnsError(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, out := buildAgent("unused", "", repo)

	ts := domain.TaskState{
		ID:        "t1",
		Phase:     "unknown_phase", // not a valid TaskPhase constant
		Task:      "do something",
		Iteration: 1,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("")), ts, "", ChatConfig{})
	if err == nil {
		t.Fatal("expected an error for unknown phase, got nil")
	}
	if !strings.Contains(err.Error(), "unknown task phase") {
		t.Errorf("error should mention 'unknown task phase', got: %v", err)
	}
	if repo.state != nil {
		t.Error("task state should be cleared after unknown-phase detection")
	}
	if !strings.Contains(out.String(), "corrupt") {
		t.Error("output should warn about corrupt state")
	}
}

// ── reviewPrompt() unit tests ─────────────────────────────────────────────────

func TestReviewPrompt_SlashYes(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/yes\n", "/YES\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.reviewPrompt(r, "?")
		if !approved || paused || feedback != "" || err != nil {
			t.Errorf("input %q: expected approved=true, got approved=%v paused=%v feedback=%q err=%v",
				input, approved, paused, feedback, err)
		}
	}
}

func TestReviewPrompt_SlashNo(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/no\n", "/NO\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.reviewPrompt(r, "?")
		if approved || paused || feedback != "" || err != nil {
			t.Errorf("input %q: expected explicit reject (approved=false, paused=false, feedback=''), got approved=%v paused=%v feedback=%q err=%v",
				input, approved, paused, feedback, err)
		}
	}
}

func TestReviewPrompt_EmptyInput_Pauses(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("\n"))
	approved, paused, feedback, err := agent.reviewPrompt(r, "?")
	if approved || !paused || feedback != "" || err != nil {
		t.Errorf("empty input must pause: got approved=%v paused=%v feedback=%q err=%v",
			approved, paused, feedback, err)
	}
}

func TestReviewPrompt_PauseCommand(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/pause\n", "pause\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.reviewPrompt(r, "?")
		if approved || !paused || feedback != "" || err != nil {
			t.Errorf("input %q: expected paused=true, got approved=%v paused=%v feedback=%q err=%v",
				input, approved, paused, feedback, err)
		}
	}
}

func TestReviewPrompt_Text_ReturnsFeedback(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("add more detail\n"))
	approved, paused, feedback, err := agent.reviewPrompt(r, "?")
	if approved || paused || feedback != "add more detail" || err != nil {
		t.Errorf("text should return as feedback: got approved=%v paused=%v feedback=%q err=%v",
			approved, paused, feedback, err)
	}
}

func TestReviewPrompt_ExitCommand(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"/exit\n", "/quit\n", "exit\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, feedback, err := agent.reviewPrompt(r, "?")
		if approved || paused || feedback != "" || !errors.Is(err, ErrExitRequested) {
			t.Errorf("input %q: expected ErrExitRequested, got approved=%v paused=%v feedback=%q err=%v",
				input, approved, paused, feedback, err)
		}
	}
}

func TestReviewPrompt_RestartCommand(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("/restart\n"))
	approved, paused, feedback, err := agent.reviewPrompt(r, "?")
	if approved || paused || feedback != "" || !errors.Is(err, ErrRestartRequested) {
		t.Errorf("expected ErrRestartRequested, got approved=%v paused=%v feedback=%q err=%v",
			approved, paused, feedback, err)
	}
}

// TestFSM_EmptyInputAtPlanningPauses verifies that pressing Enter (empty string) at
// the PLANNING review prompt pauses the task instead of returning to planning with
// an empty feedback string. Also checks that the generated plan is persisted.
func TestFSM_EmptyInputAtPlanningPauses(t *testing.T) {
	repo := &memoryTaskRepo{}
	// LLM generates plan; user presses Enter at review → should pause
	agent, _ := buildAgent("the plan", "\n", repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader("\n")), ts, "", ChatConfig{})
	if !isErrTaskPaused(err) {
		t.Fatalf("expected ErrTaskPaused on empty planning input, got %v", err)
	}
	if repo.state == nil {
		t.Fatal("task state should be saved after pause")
	}
	if repo.state.Phase != domain.PhasePlanning {
		t.Errorf("paused phase should be planning, got %s", repo.state.Phase)
	}
	if repo.state.PendingPlan != "the plan" {
		t.Errorf("PendingPlan should be saved on pause, got %q", repo.state.PendingPlan)
	}
}

// TestFSM_ResumeAtPlanning_ReusesPendingPlan verifies that when a task is resumed
// with a PendingPlan already set (paused at the plan-review prompt), the FSM skips
// the LLM call and shows the same plan without generating a new one.
func TestFSM_ResumeAtPlanning_ReusesPendingPlan(t *testing.T) {
	calls := 0
	stub := &capturingLLM{replyFn: func() string {
		calls++
		return fmt.Sprintf("plan-call-%d", calls)
	}}
	chatUC := NewChatUseCase(stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{})
	repo := &memoryTaskRepo{}
	agent := NewAgentUseCase(chatUC, repo, &noopInvariantsRepo{})
	out := &bytes.Buffer{}
	// approve → exec continue → approve validation → DONE
	input := "/yes\n/yes\n/yes\n"
	agent.in = strings.NewReader(input)
	agent.out = out
	agent.err = out

	// Simulate resuming with a pending plan (paused before approval)
	ts := domain.TaskState{
		ID:          "t1",
		Phase:       domain.PhasePlanning,
		Task:        "do something",
		PendingPlan: "pre-existing pending plan",
		Iteration:   1,
		CreatedAt:   "2024-01-01T00:00:00Z",
		UpdatedAt:   "2024-01-01T00:00:00Z",
	}
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without invariants and with a pending plan: only 1 LLM call (execution).
	// Planning must be skipped — PendingPlan is used directly.
	if calls != 1 {
		t.Errorf("expected 1 LLM call (execution only), got %d — planning LLM must not fire when PendingPlan is set", calls)
	}
	if !strings.Contains(out.String(), "pre-existing pending plan") {
		t.Error("output should show the pre-existing pending plan, not a newly generated one")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete normally after resuming with pending plan")
	}
	if repo.state != nil {
		t.Error("task state should be cleared after DONE")
	}
}

// TestFSM_EmptyInputAtValidationPauses verifies that pressing Enter (empty string) at
// the VALIDATION review prompt pauses the task instead of going back to planning.
func TestFSM_EmptyInputAtValidationPauses(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan, continue execution, press Enter at validation → pause
	input := "/yes\n/yes\n\n"
	agent, _ := buildAgent("response", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if !isErrTaskPaused(err) {
		t.Fatalf("expected ErrTaskPaused on empty validation input, got %v", err)
	}
	if repo.state == nil {
		t.Fatal("task state should be saved after pause")
	}
	if repo.state.Phase != domain.PhaseValidation {
		t.Errorf("paused phase should be validation, got %s", repo.state.Phase)
	}
}

// TestFSM_SlashNoAtPlanning_RejectsAndReplans verifies that typing /no at the
// PLANNING prompt rejects the plan without feedback and returns to planning.
func TestFSM_SlashNoAtPlanning_RejectsAndReplans(t *testing.T) {
	repo := &memoryTaskRepo{}
	// /no at plan review → replan → approve → exec → approve validation → DONE
	input := "/no\n/yes\n/yes\n/yes\n"
	agent, out := buildAgent("the plan", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "re-plan #2") {
		t.Error("/no at planning must trigger a re-plan (iteration 2)")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete after replan + approval")
	}
}

// TestFSM_SlashNoAtValidation_RejectsAndReplans verifies that typing /no at the
// VALIDATION prompt rejects the result without feedback and returns to planning.
func TestFSM_SlashNoAtValidation_RejectsAndReplans(t *testing.T) {
	repo := &memoryTaskRepo{}
	// approve plan, exec, /no at validation → replan → exec → approve → DONE
	input := "/yes\n/yes\n/no\n/yes\n/yes\n/yes\n"
	agent, out := buildAgent("response", input, repo)

	ts := domain.NewTaskState("t1", "task")
	_ = repo.Save(ts)

	err := agent.executeTaskFSM(bufio.NewReader(strings.NewReader(input)), ts, "", ChatConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "re-plan #2") {
		t.Error("/no at validation must trigger re-planning (iteration 2)")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("task should complete after /no rejection + replan + approval")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isErrTaskPaused(err error) bool {
	return err != nil && err.Error() == ErrTaskPaused.Error()
}

// Silence unused import warning for fmt when tests don't use it directly.
var _ = fmt.Sprintf
