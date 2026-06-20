package usecase

import (
	"bufio"
	"bytes"
	"context"
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

// buildAgent wires an AgentUseCase with a stub LLM and test-supplied stdin/stdout.
func buildAgent(llmReply string, stdin string, taskRepo *memoryTaskRepo) (*AgentUseCase, *bytes.Buffer) {
	stub := &stubLLM{reply: llmReply}
	chatUC := NewChatUseCase(
		stub,
		&noopHistoryRepo{}, &noopStatsRepo{}, &noopSummaryRepo{},
		&noopFactsRepo{}, &noopWMRepo{}, &noopLTMRepo{}, &noopProfileRepo{},
	)
	agent := NewAgentUseCase(chatUC, taskRepo)
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
	agent := NewAgentUseCase(chatUC, repo)
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


// ── prompt() unit test ────────────────────────────────────────────────────────

func TestPrompt_Pause(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("pause\n"))
	approved, paused, feedback := agent.prompt(r, "question?")
	if approved || !paused || feedback != "" {
		t.Errorf("pause input: got approved=%v paused=%v feedback=%q", approved, paused, feedback)
	}
}

func TestPrompt_Yes(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	for _, input := range []string{"y\n", "yes\n", "Y\n", "YES\n"} {
		r := bufio.NewReader(strings.NewReader(input))
		approved, paused, _ := agent.prompt(r, "?")
		if !approved || paused {
			t.Errorf("input %q: expected approved=true paused=false", input)
		}
	}
}

func TestPrompt_FeedbackText(t *testing.T) {
	repo := &memoryTaskRepo{}
	agent, _ := buildAgent("", "", repo)
	r := bufio.NewReader(strings.NewReader("needs work\n"))
	approved, paused, feedback := agent.prompt(r, "?")
	if approved || paused || feedback != "needs work" {
		t.Errorf("got approved=%v paused=%v feedback=%q", approved, paused, feedback)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isErrTaskPaused(err error) bool {
	return err != nil && err.Error() == ErrTaskPaused.Error()
}

// Silence unused import warning for fmt when tests don't use it directly.
var _ = fmt.Sprintf
