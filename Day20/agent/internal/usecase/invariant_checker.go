package usecase

import (
	"context"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// CheckKind identifies the type of content being checked against invariants.
type CheckKind int

const (
	CheckKindTask      CheckKind = iota // user's original task description (conservative check)
	CheckKindPlan                        // proposed step-by-step plan (strict)
	CheckKindExecution                   // execution result / output (strict)
)

// CheckResult is the verdict produced by InvariantChecker.Check.
type CheckResult struct {
	Passed     bool
	Violations []string // non-empty when Passed == false; one entry per violated invariant
	Raw        string   // full LLM response (chain-of-thought + verdict block)
	Content    string   // original content that was checked, included in violation reports
}

// ViolationReport returns a full violation report:
// the per-invariant analysis (with identified violating blocks) from the LLM,
// followed by the original input so the reworking agent has complete context.
func (r CheckResult) ViolationReport() string {
	var sb strings.Builder
	sb.WriteString(r.Raw)
	if r.Content != "" {
		sb.WriteString("\n\n════════════════════════════════════════\n")
		sb.WriteString("ORIGINAL INPUT — rework the flagged blocks to comply with the violated invariants:\n")
		sb.WriteString("════════════════════════════════════════\n")
		sb.WriteString(r.Content)
		sb.WriteString("\n════════════════════════════════════════")
	}
	return sb.String()
}

// InvariantChecker is a stateless sub-agent that enforces invariant compliance.
// It operates independently from the main conversation: each Check call loads
// invariants fresh from the repository and makes a separate API request with no history.
//
// Three-phase operation:
//  1. Load    — reads invariants document from the repository.
//  2. Apply   — sends a stateless LLM request (no history) with invariants + content.
//  3. Verdict — parses the LLM response into a PASS/FAIL verdict with violation details.
type InvariantChecker struct {
	llm        port.LLMClient
	invariants port.InvariantsRepository
}

// NewInvariantChecker creates an InvariantChecker sub-agent.
func NewInvariantChecker(llm port.LLMClient, invariants port.InvariantsRepository) *InvariantChecker {
	return &InvariantChecker{llm: llm, invariants: invariants}
}

// LoadInvariants returns the raw invariants document.
// Callers use this to inject invariants into planning/execution system prompts.
func (c *InvariantChecker) LoadInvariants() (string, error) {
	return c.invariants.Load()
}

// Check runs the three-phase invariant compliance check:
//  1. Load invariants from the repository.
//  2. Build a stateless LLM request (no conversation history) for the given kind.
//  3. Parse the LLM response into a PASS verdict or FAIL with per-invariant bullets.
//
// Returns (CheckResult{Passed: true}, nil) when no invariants are defined.
// Returns an error only for I/O or LLM communication failures — never for violations.
func (c *InvariantChecker) Check(ctx context.Context, kind CheckKind, content, model string, debug bool) (CheckResult, error) {
	// ── Phase 1: load invariants ──────────────────────────────────────────────
	inv, err := c.invariants.Load()
	if err != nil {
		return CheckResult{}, fmt.Errorf("invariant checker: load: %w", err)
	}
	if inv == "" {
		return CheckResult{Passed: true}, nil
	}

	// ── Phase 2: build stateless messages (no conversation history) ───────────
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: checkerSystemPrompt(kind, inv)},
		{Role: domain.RoleUser, Content: checkerUserMessage(kind, content)},
	}

	// ── Phase 3: send to LLM, parse verdict ──────────────────────────────────
	resp, err := c.llm.Chat(ctx, port.LLMRequest{
		Model:    model,
		Messages: messages,
		Debug:    debug,
	})
	if err != nil {
		return CheckResult{}, fmt.Errorf("invariant checker: LLM call: %w", err)
	}

	result := parseCheckerVerdict(resp.Content)
	result.Raw = resp.Content
	result.Content = content
	return result, nil
}

// checkerSystemPrompt builds the system prompt for the invariant sub-agent.
// Task-kind checks are conservative (fail only when inherently required).
// Plan and execution checks are strict (any evidence of violation = FAIL).
func checkerSystemPrompt(kind CheckKind, invariants string) string {
	var subjectTitle, subjectLower string
	switch kind {
	case CheckKindTask:
		subjectTitle = "TASK REQUEST"
		subjectLower = "task request"
	case CheckKindPlan:
		subjectTitle = "PLAN"
		subjectLower = "plan"
	default: // CheckKindExecution
		subjectTitle = "EXECUTION RESULT"
		subjectLower = "execution result"
	}

	var conservativeNote string
	if kind == CheckKindTask {
		conservativeNote = `
NOTE: Be conservative. A task "inherently requires" a violation only when there is
NO reasonable way to fulfil it without breaking at least one invariant.
If the task could be completed in a compliant manner — even implicitly — respond PASS.
`
	}

	var contentAnalysisNote string
	if kind == CheckKindExecution {
		contentAnalysisNote = `
CRITICAL — Verify invariants against the ACTUAL content of the result, not against any
self-reported compliance section (e.g. "Invariant Compliance", "Summary", "Conclusion")
written by the agent inside its own output. Such sections are unverified claims and MUST
be ignored during your analysis. Read the result itself — the code, text, steps, artefacts,
or whatever form it takes — and judge each invariant directly against what is actually there.

General rules that apply regardless of domain or content type:

• STATED vs. ACTUAL: If an invariant requires something to be present (a technique, a tool,
  a structure, a behaviour), verify it is genuinely present in the result — not merely
  mentioned or promised. A description of what "will be done" is not the same as doing it.

• NAMED ALTERNATIVES: Invariants often name specific options (e.g. "only X or Y are allowed").
  Check that nothing outside that set appears in the result. Synonyms, near-equivalents, and
  informal substitutes count as violations if the invariant names something specific.

• STUBS AND PLACEHOLDERS: Any element that is structurally present but functionally empty
  (placeholder text, TODO markers, "to be implemented", no real logic or content) does NOT
  satisfy an invariant that requires that element to actually work or exist.

• COMPLETENESS: If the invariant requires every instance of something (every function, every
  chapter, every step) to satisfy a condition, check ALL instances — not just the first one
  or the ones that are easiest to see.

• CLAIMS IN THE RESULT: Do not treat a statement inside the result such as "this complies
  with X" or "we use Y as required" as evidence of compliance. Only observable content counts.
`
	}

	return fmt.Sprintf(`You are a strict invariant compliance sub-agent reviewing a %s.
%s%s%s
Analyse EACH invariant individually before writing your verdict.
For each invariant use exactly this block format:

CHECKING: <copy the invariant text exactly>
EVIDENCE: <what you observe in the %s that relates to this invariant>
VIOLATING BLOCK: <if failing — quote verbatim the exact fragment(s) from the %s that violate this invariant; write "none" if passing>
STATUS: PASS  — or —  FAIL: <what specifically violates it>
ACTION: <if FAIL — state exactly what must be reworked in the quoted block to comply; write "none" if passing>

Do NOT skip any invariant. Do NOT trust compliance claims inside the content — verify each invariant
against the actual content itself.
A step or element vague enough to lead to a violation counts as FAIL.

After ALL invariant checks, write your final verdict as the very last block — nothing may follow it:

If every invariant is satisfied:
PASS

If any invariant is violated:
FAIL
- <invariant text quoted verbatim>: <what violates it> — rework the identified block to comply
- <invariant text quoted verbatim>: <what violates it> — rework the identified block to comply
(one bullet per violated invariant, nothing after the bullet list)`,
		subjectTitle, invariantsBlock(invariants), conservativeNote, contentAnalysisNote, subjectLower, subjectLower)
}

// checkerUserMessage builds the user-turn message for the given check kind.
func checkerUserMessage(kind CheckKind, content string) string {
	switch kind {
	case CheckKindTask:
		return "Check this task request for inherent invariant violations:\n\n" + content
	case CheckKindPlan:
		return "Check this plan for steps that would violate the invariants:\n\n" + content
	default: // CheckKindExecution
		return "Check this execution result for invariant violations:\n\n" + content
	}
}

// parseCheckerVerdict parses the LLM response for a PASS or FAIL verdict.
// Scans from the end of the response to find the verdict line;
// all chain-of-thought analysis before it is ignored during parsing.
// FAIL is expected to be followed by bullet lines (one per violated invariant).
// Unknown format → treated as PASS (fail-open) to avoid blocking valid work.
func parseCheckerVerdict(response string) CheckResult {
	lines := strings.Split(strings.TrimSpace(response), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		upper := strings.ToUpper(line)
		if upper == "PASS" {
			return CheckResult{Passed: true}
		}
		if upper == "FAIL" {
			return CheckResult{Passed: false, Violations: parseViolationBullets(lines[i+1:])}
		}
	}
	return CheckResult{Passed: true} // no clear verdict — fail-open
}

// parseViolationBullets collects non-empty lines after the FAIL verdict line.
// Strips leading "- " or "* " bullet markers.
func parseViolationBullets(lines []string) []string {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		l = strings.TrimPrefix(l, "- ")
		l = strings.TrimPrefix(l, "* ")
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
