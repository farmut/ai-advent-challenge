package app

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// scriptedLLM is a fake port.LLMClient. It distinguishes orchestrator calls (the
// system prompt mentions "оркестратор") from sub-agent calls and replies with a
// scripted spawn→finish sequence for the orchestrator, and a fixed result for the
// sub-agent.
type scriptedLLM struct {
	orchCalls int
	subCalls  int
}

func (s *scriptedLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	sys := ""
	if len(req.Messages) > 0 && req.Messages[0].Role == domain.RoleSystem {
		sys = req.Messages[0].Content
	}
	if strings.Contains(sys, "оркестратор") {
		s.orchCalls++
		if s.orchCalls == 1 {
			return port.LLMResponse{Content: `{"action":"spawn","agent":"researcher","task":"найди факт","context":""}`}, nil
		}
		return port.LLMResponse{Content: `{"action":"finish","answer":"ИТОГ: 42"}`}, nil
	}
	s.subCalls++
	return port.LLMResponse{Content: "результат саб-агента: факт найден"}, nil
}

func testToolbelt(t *testing.T, llm port.LLMClient, subs []config.SubAgentConfig) *Toolbelt {
	t.Helper()
	cfg := config.Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIKey = "sk-test"
	cfg.LLM.Model = "test-model"
	cfg.Memory.Dir = t.TempDir() + "/hist.json"
	cfg.Orchestrator.SubAgents = subs
	return &Toolbelt{
		Cfg:    cfg,
		LLM:    llm,
		Memory: NewMemoryFactory(cfg.Memory),
	}
}

func TestOrchestrator_SpawnThenFinish(t *testing.T) {
	llm := &scriptedLLM{}
	tb := testToolbelt(t, llm, []config.SubAgentConfig{
		{Name: "researcher", Prompt: "ищи факты"},
	})
	o := NewOrchestrator(tb, false)

	answer, err := o.Handle(context.Background(), "реши задачу")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "ИТОГ: 42" {
		t.Fatalf("unexpected answer: %q", answer)
	}
	if llm.orchCalls != 2 {
		t.Errorf("expected 2 orchestrator calls (spawn, finish), got %d", llm.orchCalls)
	}
	if llm.subCalls != 1 {
		t.Errorf("expected 1 sub-agent call, got %d", llm.subCalls)
	}

	// The turn must be persisted to the shared history.
	hist, _ := tb.Memory.History().Load()
	if len(hist) != 2 || hist[0].Content != "реши задачу" || hist[1].Content != "ИТОГ: 42" {
		t.Errorf("history not persisted correctly: %+v", hist)
	}
}

func TestOrchestrator_UnknownAgentIsCorrected(t *testing.T) {
	// The model first names a non-existent agent, then finishes. The orchestrator
	// must feed back an error and continue rather than crash.
	llm := &stepLLM{steps: []string{
		`{"action":"spawn","agent":"ghost","task":"x","context":""}`,
		`{"action":"finish","answer":"готово"}`,
	}}
	tb := testToolbelt(t, llm, []config.SubAgentConfig{{Name: "researcher", Prompt: "p"}})
	o := NewOrchestrator(tb, false)
	answer, err := o.Handle(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "готово" {
		t.Fatalf("unexpected answer: %q", answer)
	}
}

func TestOrchestrator_NoRosterAnswersDirectly(t *testing.T) {
	llm := &scriptedLLM{}
	tb := testToolbelt(t, llm, nil)
	o := NewOrchestrator(tb, false)
	answer, err := o.Handle(context.Background(), "прямой вопрос")
	if err != nil {
		t.Fatal(err)
	}
	if answer == "" {
		t.Fatal("expected a direct answer")
	}
	if llm.orchCalls != 0 {
		t.Errorf("no-roster path must not run the orchestration loop, got %d orch calls", llm.orchCalls)
	}
}

// stepLLM replies with the next scripted step on every orchestrator call and a
// fixed sub-agent result otherwise.
type stepLLM struct {
	steps []string
	i     int
}

func (s *stepLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	sys := ""
	if len(req.Messages) > 0 && req.Messages[0].Role == domain.RoleSystem {
		sys = req.Messages[0].Content
	}
	if strings.Contains(sys, "оркестратор") {
		out := s.steps[s.i]
		if s.i < len(s.steps)-1 {
			s.i++
		}
		return port.LLMResponse{Content: out}, nil
	}
	return port.LLMResponse{Content: "sub result"}, nil
}

type fakePrompter struct {
	gotPrompt string
	reply     string
}

func (f *fakePrompter) AskUser(_ context.Context, p string) (string, error) {
	f.gotPrompt = p
	return f.reply, nil
}

// The orchestrator must pause on ask_user, hand the plan to the prompter, and
// resume with the user's reply folded into the transcript.
func TestOrchestrator_AskUserGate(t *testing.T) {
	llm := &stepLLM{steps: []string{
		`{"action":"ask_user","question":"Согласуйте план","plan":"1. research\n2. code"}`,
		`{"action":"finish","answer":"готово по согласованному плану"}`,
	}}
	tb := testToolbelt(t, llm, []config.SubAgentConfig{{Name: "researcher", Prompt: "p"}})
	o := NewOrchestrator(tb, false)
	fp := &fakePrompter{reply: "согласовано, поехали"}
	o.SetPrompter(fp)

	ans, err := o.Handle(context.Background(), "сделай с согласованием плана")
	if err != nil {
		t.Fatal(err)
	}
	if ans != "готово по согласованному плану" {
		t.Fatalf("unexpected answer: %q", ans)
	}
	if !strings.Contains(fp.gotPrompt, "1. research") {
		t.Errorf("approval prompt should carry the plan, got %q", fp.gotPrompt)
	}
}

// With no prompter (one-shot CLI), ask_user must not block — it proceeds
// autonomously and still finishes.
func TestOrchestrator_AskUserNoPrompterProceeds(t *testing.T) {
	llm := &stepLLM{steps: []string{
		`{"action":"ask_user","question":"q","plan":"p"}`,
		`{"action":"finish","answer":"done"}`,
	}}
	tb := testToolbelt(t, llm, []config.SubAgentConfig{{Name: "r", Prompt: "p"}})
	o := NewOrchestrator(tb, false)
	ans, err := o.Handle(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if ans != "done" {
		t.Fatalf("unexpected answer: %q", ans)
	}
}

func TestMCPAndToolsSummary_Disabled(t *testing.T) {
	tb := testToolbelt(t, &scriptedLLM{}, nil) // no MCP pool wired
	o := NewOrchestrator(tb, false)
	if got := o.MCPSummary(); !strings.Contains(got, "не подключён") {
		t.Errorf("MCPSummary with no MCP should say so, got %q", got)
	}
	if got := o.ToolsSummary(); !strings.Contains(got, "недоступны") {
		t.Errorf("ToolsSummary with no MCP should say so, got %q", got)
	}
}

func TestParseAction(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		action string
	}{
		{`{"action":"spawn","agent":"a"}`, true, "spawn"},
		{"```json\n{\"action\":\"finish\",\"answer\":\"x\"}\n```", true, "finish"},
		{"тут немного текста {\"action\":\"spawn\",\"agent\":\"b\"} и ещё", true, "spawn"},
		{"нет джейсона вообще", false, ""},
		{`{"answer":"no action field"}`, false, ""},
	}
	for _, c := range cases {
		act, ok := parseAction(c.in)
		if ok != c.ok {
			t.Errorf("parseAction(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && act.Action != c.action {
			t.Errorf("parseAction(%q) action=%q want %q", c.in, act.Action, c.action)
		}
	}
}
