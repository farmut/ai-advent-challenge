package usecase

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// fixedLLM returns a fixed reply string for every Chat call.
type fixedLLM struct{ reply string }

func (f *fixedLLM) Chat(_ context.Context, _ port.LLMRequest) (port.LLMResponse, error) {
	return port.LLMResponse{Content: f.reply}, nil
}

// capturingCheckerLLM records every request it receives.
type capturingCheckerLLM struct {
	reply    string
	captured []port.LLMRequest
}

func (c *capturingCheckerLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	c.captured = append(c.captured, req)
	return port.LLMResponse{Content: c.reply}, nil
}

// ── parseCheckerVerdict unit tests ────────────────────────────────────────────

func TestParseCheckerVerdict_StandalonePASS(t *testing.T) {
	result := parseCheckerVerdict("PASS")
	if !result.Passed {
		t.Error("standalone PASS should produce Passed=true")
	}
}

func TestParseCheckerVerdict_PassCaseInsensitive(t *testing.T) {
	for _, s := range []string{"pass", "Pass", "PASS"} {
		if r := parseCheckerVerdict(s); !r.Passed {
			t.Errorf("input %q: expected Passed=true", s)
		}
	}
}

func TestParseCheckerVerdict_StandaloneFAIL(t *testing.T) {
	result := parseCheckerVerdict("FAIL")
	if result.Passed {
		t.Error("standalone FAIL should produce Passed=false")
	}
	if len(result.Violations) != 0 {
		t.Errorf("no bullets after FAIL: expected empty violations, got %v", result.Violations)
	}
}

func TestParseCheckerVerdict_FailWithBullets(t *testing.T) {
	response := "FAIL\n- invariant A: step 3 uses gin\n- invariant B: writes to /etc"
	result := parseCheckerVerdict(response)
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if len(result.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %v", len(result.Violations), result.Violations)
	}
	if !strings.Contains(result.Violations[0], "gin") {
		t.Error("first violation should mention 'gin'")
	}
	if !strings.Contains(result.Violations[1], "/etc") {
		t.Error("second violation should mention '/etc'")
	}
}

func TestParseCheckerVerdict_ChainOfThoughtBeforePass(t *testing.T) {
	response := `CHECKING: Go standard library only
EVIDENCE: plan uses net/http
STATUS: PASS

PASS`
	result := parseCheckerVerdict(response)
	if !result.Passed {
		t.Error("chain-of-thought with PASS verdict should produce Passed=true")
	}
}

func TestParseCheckerVerdict_ChainOfThoughtBeforeFail(t *testing.T) {
	response := `CHECKING: Go standard library only
EVIDENCE: step 3 imports gin
STATUS: FAIL: gin is an external library

FAIL
- Go standard library only: step 3 imports gin`
	result := parseCheckerVerdict(response)
	if result.Passed {
		t.Error("chain-of-thought with FAIL verdict should produce Passed=false")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
}

func TestParseCheckerVerdict_StatusPassLineDoesNotTriggerPass(t *testing.T) {
	// "STATUS: PASS" contains "PASS" but is not a standalone line
	response := "STATUS: PASS for invariant 1\nFAIL\n- something violated"
	result := parseCheckerVerdict(response)
	if result.Passed {
		t.Error("'STATUS: PASS' line must not be treated as a PASS verdict")
	}
}

func TestParseCheckerVerdict_UnknownFormatIsFailOpen(t *testing.T) {
	result := parseCheckerVerdict("the quick brown fox")
	if !result.Passed {
		t.Error("unknown verdict format should fail-open (Passed=true)")
	}
}

func TestParseCheckerVerdict_EmptyResponseIsFailOpen(t *testing.T) {
	result := parseCheckerVerdict("")
	if !result.Passed {
		t.Error("empty response should fail-open (Passed=true)")
	}
}

// ── parseViolationBullets unit tests ──────────────────────────────────────────

func TestParseViolationBullets_DashPrefix(t *testing.T) {
	lines := []string{"- violation one", "- violation two"}
	out := parseViolationBullets(lines)
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
	if out[0] != "violation one" {
		t.Errorf("expected 'violation one', got %q", out[0])
	}
}

func TestParseViolationBullets_StarPrefix(t *testing.T) {
	lines := []string{"* violation one"}
	out := parseViolationBullets(lines)
	if len(out) != 1 || out[0] != "violation one" {
		t.Errorf("expected ['violation one'], got %v", out)
	}
}

func TestParseViolationBullets_SkipsEmptyLines(t *testing.T) {
	lines := []string{"", "- item one", "", "- item two", ""}
	out := parseViolationBullets(lines)
	if len(out) != 2 {
		t.Fatalf("expected 2 items (empty lines skipped), got %d: %v", len(out), out)
	}
}

// ── CheckResult.ViolationReport unit tests ────────────────────────────────────

func TestCheckResult_ViolationReport_WithViolations(t *testing.T) {
	r := CheckResult{
		Passed:     false,
		Violations: []string{"inv A: step 1", "inv B: step 3"},
		Raw:        "FAIL\n- inv A: step 1 — rework the identified block to comply\n- inv B: step 3 — rework the identified block to comply",
		Content:    "original input content",
	}
	report := r.ViolationReport()
	if !strings.Contains(report, "inv A") {
		t.Error("ViolationReport should contain first violation from Raw")
	}
	if !strings.Contains(report, "inv B") {
		t.Error("ViolationReport should contain second violation from Raw")
	}
	if !strings.Contains(report, "original input content") {
		t.Error("ViolationReport should include original Content")
	}
	if !strings.Contains(report, "ORIGINAL INPUT") {
		t.Error("ViolationReport should include ORIGINAL INPUT separator")
	}
}

func TestCheckResult_ViolationReport_NoContentOmitsSeparator(t *testing.T) {
	r := CheckResult{Passed: false, Raw: "FAIL\n- inv A: violation"}
	report := r.ViolationReport()
	if strings.Contains(report, "ORIGINAL INPUT") {
		t.Error("ViolationReport must not include ORIGINAL INPUT section when Content is empty")
	}
	if report != "FAIL\n- inv A: violation" {
		t.Errorf("ViolationReport should return Raw unchanged when Content is empty, got %q", report)
	}
}

func TestCheckResult_ViolationReport_FallsBackToRaw(t *testing.T) {
	r := CheckResult{Passed: false, Raw: "full LLM analysis text"}
	report := r.ViolationReport()
	if report != "full LLM analysis text" {
		t.Errorf("ViolationReport should return Raw, got %q", report)
	}
}

// ── checkerSystemPrompt unit tests ────────────────────────────────────────────

func TestCheckerSystemPrompt_TaskKind(t *testing.T) {
	p := checkerSystemPrompt(CheckKindTask, "## Rule\n- no gin")
	if !strings.Contains(strings.ToLower(p), "task request") {
		t.Error("task system prompt must mention 'task request'")
	}
	if !strings.Contains(p, "conservative") {
		t.Error("task system prompt must include conservative note")
	}
	if !strings.Contains(p, "no gin") {
		t.Error("task system prompt must embed invariants text")
	}
	if !strings.Contains(p, "PASS") {
		t.Error("task system prompt must describe PASS verdict format")
	}
	if !strings.Contains(p, "FAIL") {
		t.Error("task system prompt must describe FAIL verdict format")
	}
}

func TestCheckerSystemPrompt_PlanKind(t *testing.T) {
	p := checkerSystemPrompt(CheckKindPlan, "## Rule\n- stdlib only")
	if !strings.Contains(strings.ToLower(p), "plan") {
		t.Error("plan system prompt must mention 'plan'")
	}
	if strings.Contains(p, "conservative") {
		t.Error("plan system prompt must NOT include conservative note")
	}
}

func TestCheckerSystemPrompt_ExecutionKind(t *testing.T) {
	p := checkerSystemPrompt(CheckKindExecution, "## Rule\n- no side effects")
	if !strings.Contains(strings.ToLower(p), "execution result") {
		t.Error("execution system prompt must mention 'execution result'")
	}
	if strings.Contains(p, "conservative") {
		t.Error("execution system prompt must NOT include conservative note")
	}
}

func TestCheckerSystemPrompt_InvariantsEmbedded(t *testing.T) {
	inv := "## Constraints\n- rule one\n- rule two"
	p := checkerSystemPrompt(CheckKindPlan, inv)
	if !strings.Contains(p, "rule one") || !strings.Contains(p, "rule two") {
		t.Error("system prompt must embed full invariants text verbatim")
	}
	if !strings.Contains(p, "INVARIANTS") {
		t.Error("system prompt must include INVARIANTS block header")
	}
}

// ── checkerUserMessage unit tests ─────────────────────────────────────────────

func TestCheckerUserMessage_TaskKind(t *testing.T) {
	msg := checkerUserMessage(CheckKindTask, "build a REST API")
	if !strings.Contains(msg, "task request") {
		t.Error("task user message must mention 'task request'")
	}
	if !strings.Contains(msg, "build a REST API") {
		t.Error("task user message must include the content")
	}
}

func TestCheckerUserMessage_PlanKind(t *testing.T) {
	msg := checkerUserMessage(CheckKindPlan, "step 1: do X")
	if !strings.Contains(msg, "plan") {
		t.Error("plan user message must mention 'plan'")
	}
	if !strings.Contains(msg, "step 1: do X") {
		t.Error("plan user message must include the content")
	}
}

func TestCheckerUserMessage_ExecutionKind(t *testing.T) {
	msg := checkerUserMessage(CheckKindExecution, "result: built server")
	if !strings.Contains(msg, "execution result") {
		t.Error("execution user message must mention 'execution result'")
	}
	if !strings.Contains(msg, "result: built server") {
		t.Error("execution user message must include the content")
	}
}

// ── InvariantChecker integration tests ───────────────────────────────────────

func TestInvariantChecker_NoInvariants_AlwaysPasses(t *testing.T) {
	checker := NewInvariantChecker(&fixedLLM{reply: "FAIL"}, &noopInvariantsRepo{})
	result, err := checker.Check(context.Background(), CheckKindPlan, "some plan", "model", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("checker with no invariants must always return PASS without calling the LLM")
	}
}

func TestInvariantChecker_LoadInvariants_ReturnsRepoContent(t *testing.T) {
	inv := "## Rule\n- no gin"
	checker := NewInvariantChecker(&fixedLLM{}, &stubInvariantsRepo{content: inv})
	loaded, err := checker.LoadInvariants()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != inv {
		t.Errorf("LoadInvariants: expected %q, got %q", inv, loaded)
	}
}

func TestInvariantChecker_Check_PassVerdict(t *testing.T) {
	checker := NewInvariantChecker(&fixedLLM{reply: "PASS"}, &stubInvariantsRepo{content: "## Rule\n- no gin"})
	result, err := checker.Check(context.Background(), CheckKindPlan, "good plan", "model", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true for PASS verdict")
	}
	if result.Raw != "PASS" {
		t.Errorf("Raw should be full LLM response, got %q", result.Raw)
	}
}

func TestInvariantChecker_Check_FailVerdict(t *testing.T) {
	checker := NewInvariantChecker(
		&fixedLLM{reply: "FAIL\n- no gin: step 3 imports gin\n- stdlib only: gin is external"},
		&stubInvariantsRepo{content: "## Rule\n- no gin\n- stdlib only"},
	)
	result, err := checker.Check(context.Background(), CheckKindExecution, "result uses gin", "model", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for FAIL verdict")
	}
	if len(result.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %v", len(result.Violations), result.Violations)
	}
}

func TestInvariantChecker_Check_StatelessNoHistory(t *testing.T) {
	capture := &capturingCheckerLLM{reply: "PASS"}
	checker := NewInvariantChecker(capture, &stubInvariantsRepo{content: "## Rule\n- stdlib only"})
	_, err := checker.Check(context.Background(), CheckKindPlan, "step 1: net/http", "gpt-4o", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capture.captured) != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", len(capture.captured))
	}
	req := capture.captured[0]
	if len(req.Messages) != 2 {
		t.Errorf("checker must send exactly 2 messages (system + user), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != domain.RoleSystem {
		t.Error("first message must be system role")
	}
	if req.Messages[1].Role != domain.RoleUser {
		t.Error("second message must be user role")
	}
}

func TestInvariantChecker_Check_UsesCorrectModel(t *testing.T) {
	capture := &capturingCheckerLLM{reply: "PASS"}
	checker := NewInvariantChecker(capture, &stubInvariantsRepo{content: "## Rule\n- stdlib"})
	_, _ = checker.Check(context.Background(), CheckKindTask, "task desc", "specific-model", false)
	if len(capture.captured) != 1 {
		t.Fatalf("expected 1 LLM call")
	}
	if capture.captured[0].Model != "specific-model" {
		t.Errorf("checker must use the model passed to Check, got %q", capture.captured[0].Model)
	}
}

func TestInvariantChecker_Check_InvariantsEmbeddedInSystemMessage(t *testing.T) {
	const invText = "## Rules\n- no gin framework\n- stdlib only"
	capture := &capturingCheckerLLM{reply: "PASS"}
	checker := NewInvariantChecker(capture, &stubInvariantsRepo{content: invText})
	_, _ = checker.Check(context.Background(), CheckKindExecution, "result", "model", false)
	if len(capture.captured) == 0 {
		t.Fatal("no LLM calls captured")
	}
	sysMsg := capture.captured[0].Messages[0].Content
	if !strings.Contains(sysMsg, "no gin framework") {
		t.Error("system message must embed invariants text verbatim")
	}
	if !strings.Contains(sysMsg, "INVARIANTS") {
		t.Error("system message must include INVARIANTS block header")
	}
}

func TestInvariantChecker_Check_ContentInUserMessage(t *testing.T) {
	const content = "step 1: build http server\nstep 2: add handler"
	capture := &capturingCheckerLLM{reply: "PASS"}
	checker := NewInvariantChecker(capture, &stubInvariantsRepo{content: "## Rule\n- stdlib"})
	_, _ = checker.Check(context.Background(), CheckKindPlan, content, "model", false)
	if len(capture.captured) == 0 {
		t.Fatal("no LLM calls captured")
	}
	userMsg := capture.captured[0].Messages[1].Content
	if !strings.Contains(userMsg, content) {
		t.Error("user message must include the content to be checked")
	}
}
