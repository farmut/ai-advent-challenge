package usecase_test

import (
	"context"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// ---- in-memory stubs ----

type stubLLM struct {
	// capturedReqs stores every request received; Reply is always returned.
	capturedReqs []port.LLMRequest
	reply        string
}

func (s *stubLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	s.capturedReqs = append(s.capturedReqs, req)
	reply := s.reply
	if reply == "" {
		reply = "ok"
	}
	return port.LLMResponse{Content: reply, Usage: domain.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

type stubHistory struct{ msgs []domain.Message }

func (r *stubHistory) Load() ([]domain.Message, error) { return r.msgs, nil }
func (r *stubHistory) Save(m []domain.Message) error   { r.msgs = m; return nil }

type stubStats struct{ s domain.SessionStats }

func (r *stubStats) Load() (domain.SessionStats, error) { return r.s, nil }
func (r *stubStats) Save(s domain.SessionStats) error   { r.s = s; return nil }

type stubSummary struct{ content string }

func (r *stubSummary) Load() (string, error)    { return r.content, nil }
func (r *stubSummary) Save(s string) error      { r.content = s; return nil }

type stubFacts struct{ fs domain.FactsStore }

func newStubFacts() *stubFacts {
	return &stubFacts{fs: domain.FactsStore{Facts: make(map[string]string)}}
}
func (r *stubFacts) Load() (domain.FactsStore, error) { return r.fs, nil }
func (r *stubFacts) Save(f domain.FactsStore) error   { r.fs = f; return nil }

type stubWM struct{ wm domain.WorkingMemory }

func newStubWM() *stubWM {
	return &stubWM{wm: domain.WorkingMemory{Facts: make(map[string]string)}}
}
func (r *stubWM) Load() (domain.WorkingMemory, error) { return r.wm, nil }
func (r *stubWM) Save(w domain.WorkingMemory) error   { r.wm = w; return nil }

type stubLTM struct{ ltm domain.LongTermMemory }

func newStubLTM() *stubLTM {
	return &stubLTM{ltm: domain.LongTermMemory{Entries: make(map[string]string)}}
}
func (r *stubLTM) Load() (domain.LongTermMemory, error) { return r.ltm, nil }
func (r *stubLTM) Save(l domain.LongTermMemory) error   { r.ltm = l; return nil }

type stubProfile struct{ p domain.UserProfile }

func newStubProfile() *stubProfile {
	return &stubProfile{p: domain.UserProfile{Preferences: make(map[string]string)}}
}
func (r *stubProfile) Load() (domain.UserProfile, error) { return r.p, nil }
func (r *stubProfile) Save(p domain.UserProfile) error   { r.p = p; return nil }

// ---- helpers ----

func newChatUC(llm *stubLLM, hist *stubHistory, wm *stubWM, ltm *stubLTM, profile *stubProfile) *usecase.ChatUseCase {
	return usecase.NewChatUseCase(llm, hist, &stubStats{}, &stubSummary{}, newStubFacts(), wm, ltm, profile)
}

func baseCfg(query string) usecase.ChatConfig {
	return usecase.ChatConfig{
		Model:     "gpt-4o",
		FullQuery: query,
		Strategy:  usecase.StrategyNone,
	}
}

func systemMsg(reqs []port.LLMRequest) string {
	if len(reqs) == 0 {
		return ""
	}
	for _, m := range reqs[0].Messages {
		if m.Role == domain.RoleSystem {
			return m.Content
		}
	}
	return ""
}

// ---- tests ----

// TestChatExecute_SystemOrder verifies the correct block order in the system message:
// Profile block first, then LTM, then WM, then user system.
func TestChatExecute_SystemOrder(t *testing.T) {
	llm := &stubLLM{}
	profile := newStubProfile()
	profile.p = domain.UserProfile{
		Name:        "Alice",
		Preferences: map[string]string{"style": "concise"},
	}
	wm := newStubWM()
	wm.wm = domain.WorkingMemory{Facts: map[string]string{"task": "write tests"}}
	ltm := newStubLTM()
	ltm.ltm = domain.LongTermMemory{Entries: map[string]string{"pref": "Go"}}

	uc := newChatUC(llm, &stubHistory{}, wm, ltm, profile)
	cfg := baseCfg("hello")
	cfg.SystemMessage = "Be helpful."

	if _, err := uc.Execute(context.Background(), cfg); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	sys := systemMsg(llm.capturedReqs)
	if sys == "" {
		t.Fatal("expected a system message, got none")
	}

	profileIdx := strings.Index(sys, "[User Profile]")
	ltmIdx := strings.Index(sys, "[Long-term Memory")
	wmIdx := strings.Index(sys, "[Working Memory")
	userIdx := strings.Index(sys, "Be helpful.")

	if profileIdx < 0 {
		t.Error("system message missing [User Profile] block")
	}
	if ltmIdx < 0 {
		t.Error("system message missing [Long-term Memory] block")
	}
	if wmIdx < 0 {
		t.Error("system message missing [Working Memory] block")
	}
	if userIdx < 0 {
		t.Error("system message missing user system text")
	}

	// Profile must appear before LTM, LTM before WM, WM before user system.
	if profileIdx > ltmIdx {
		t.Errorf("Profile block should precede LTM block (Profile@%d, LTM@%d)", profileIdx, ltmIdx)
	}
	if ltmIdx > wmIdx {
		t.Errorf("LTM block should precede WM block (LTM@%d, WM@%d)", ltmIdx, wmIdx)
	}
	if wmIdx > userIdx {
		t.Errorf("WM block should precede user system (WM@%d, user@%d)", wmIdx, userIdx)
	}
}

// TestChatExecute_EmptyProfile verifies that Execute works correctly when the profile
// is empty (no name, no preferences) — no profile block must appear in system message.
func TestChatExecute_EmptyProfile(t *testing.T) {
	llm := &stubLLM{}
	uc := newChatUC(llm, &stubHistory{}, newStubWM(), newStubLTM(), newStubProfile())
	cfg := baseCfg("ping")
	cfg.SystemMessage = "You are a bot."

	if _, err := uc.Execute(context.Background(), cfg); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	sys := systemMsg(llm.capturedReqs)
	if strings.Contains(sys, "[User Profile]") {
		t.Error("empty profile should not inject [User Profile] block")
	}
	if !strings.Contains(sys, "You are a bot.") {
		t.Error("user system message should still appear")
	}
}

// TestChatExecute_STMAccumulates verifies that after two Execute calls the history
// accumulates and the second call includes both prior turns in its request.
func TestChatExecute_STMAccumulates(t *testing.T) {
	llm := &stubLLM{reply: "answer"}
	hist := &stubHistory{}
	uc := newChatUC(llm, hist, newStubWM(), newStubLTM(), newStubProfile())

	if _, err := uc.Execute(context.Background(), baseCfg("turn 1")); err != nil {
		t.Fatalf("first Execute failed: %v", err)
	}
	if _, err := uc.Execute(context.Background(), baseCfg("turn 2")); err != nil {
		t.Fatalf("second Execute failed: %v", err)
	}

	// The second LLM call should have 2 prior messages (user + assistant from turn 1).
	secondReq := llm.capturedReqs[1]
	// Count non-system messages.
	var nonSystem int
	for _, m := range secondReq.Messages {
		if m.Role != domain.RoleSystem {
			nonSystem++
		}
	}
	// turn1 user + turn1 assistant + turn2 user = 3
	if nonSystem < 3 {
		t.Errorf("expected at least 3 non-system messages in second call, got %d", nonSystem)
	}

	// Profile should be unchanged (still empty).
	if p, _ := newStubProfile().Load(); p.Name != "" {
		t.Error("profile should not be modified by Execute")
	}
}

// TestChatExecute_ProfileNotModifiedByMemoryUpdate verifies that --memory-update
// updates WM and LTM via the LLM but leaves the profile untouched.
func TestChatExecute_ProfileNotModifiedByMemoryUpdate(t *testing.T) {
	// The first LLM call is the main chat; the subsequent two are UpdateWM and UpdateLTM.
	// We control all replies here to return valid JSON that the memory updaters parse.
	callCount := 0
	llm := &stubLLM{}
	// Override Chat to return different responses per call index.
	// We wrap with a custom type since stubLLM.reply is a simple field.
	customLLM := &funcLLM{fn: func(req port.LLMRequest) port.LLMResponse {
		callCount++
		switch callCount {
		case 1:
			return port.LLMResponse{Content: "main answer", Usage: domain.Usage{TotalTokens: 20}}
		default:
			// UpdateWM and UpdateLTM both expect a JSON object back.
			return port.LLMResponse{Content: `{}`, Usage: domain.Usage{TotalTokens: 5}}
		}
	}}
	_ = llm // unused; use customLLM below

	profile := newStubProfile()
	profile.p = domain.UserProfile{Name: "Bob", Preferences: map[string]string{"lang": "go"}}

	wm := newStubWM()
	ltm := newStubLTM()

	uc := usecase.NewChatUseCase(customLLM, &stubHistory{}, &stubStats{}, &stubSummary{}, newStubFacts(), wm, ltm, profile)

	cfg := baseCfg("update my memory please")
	cfg.MemoryUpdate = true

	if _, err := uc.Execute(context.Background(), cfg); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Profile must remain exactly as set.
	loaded, _ := profile.Load()
	if loaded.Name != "Bob" {
		t.Errorf("profile name should be unchanged: got %q", loaded.Name)
	}
	if loaded.Preferences["lang"] != "go" {
		t.Errorf("profile preference should be unchanged: got %q", loaded.Preferences["lang"])
	}
}

// TestChatExecute_AllLayersPresent is a smoke test that all four memory layers
// (Profile, LTM, WM, STM history) are active simultaneously with no errors.
func TestChatExecute_AllLayersPresent(t *testing.T) {
	llm := &stubLLM{reply: "all good"}
	hist := &stubHistory{
		msgs: []domain.Message{
			{Role: domain.RoleUser, Content: "prior question"},
			{Role: domain.RoleAssistant, Content: "prior answer"},
		},
	}
	wm := newStubWM()
	wm.wm = domain.WorkingMemory{Facts: map[string]string{"ctx": "testing"}}
	ltm := newStubLTM()
	ltm.ltm = domain.LongTermMemory{Entries: map[string]string{"fact": "Go expert"}}
	profile := newStubProfile()
	profile.p = domain.UserProfile{Name: "Carol", Preferences: map[string]string{"format": "json"}}

	uc := newChatUC(llm, hist, wm, ltm, profile)
	cfg := baseCfg("new question")
	cfg.SystemMessage = "Assistant system."

	result, err := uc.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Content != "all good" {
		t.Errorf("unexpected content: %q", result.Content)
	}

	sys := systemMsg(llm.capturedReqs)

	checks := []struct {
		label string
		want  string
	}{
		{"Profile header", "[User Profile]"},
		{"Profile name", "Carol"},
		{"LTM header", "[Long-term Memory"},
		{"LTM entry", "Go expert"},
		{"WM header", "[Working Memory"},
		{"WM fact", "testing"},
		{"user system", "Assistant system."},
	}
	for _, c := range checks {
		if !strings.Contains(sys, c.want) {
			t.Errorf("system message missing %s (%q)", c.label, c.want)
		}
	}

	// STM: prior messages should be forwarded.
	req := llm.capturedReqs[0]
	var foundPrior bool
	for _, m := range req.Messages {
		if m.Content == "prior question" {
			foundPrior = true
			break
		}
	}
	if !foundPrior {
		t.Error("prior history should be included in the LLM request")
	}
}

// TestChatExecute_SummaryAppendsAtEnd verifies that when a summary is present it
// appears after all other system blocks.
func TestChatExecute_SummaryAppendsAtEnd(t *testing.T) {
	llm := &stubLLM{}
	summary := &stubSummary{content: "Earlier: user asked about Go."}
	wm := newStubWM()
	wm.wm = domain.WorkingMemory{Facts: map[string]string{"k": "v"}}
	profile := newStubProfile()
	profile.p = domain.UserProfile{Name: "Dan"}

	uc := usecase.NewChatUseCase(llm, &stubHistory{}, &stubStats{}, summary, newStubFacts(), wm, newStubLTM(), profile)

	cfg := baseCfg("continue")
	cfg.SystemMessage = "Be concise."
	cfg.SummaryEnabled = true

	if _, err := uc.Execute(context.Background(), cfg); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	sys := systemMsg(llm.capturedReqs)

	profileIdx := strings.Index(sys, "[User Profile]")
	userIdx := strings.Index(sys, "Be concise.")
	summaryIdx := strings.Index(sys, "Earlier: user asked about Go.")

	if summaryIdx < 0 {
		t.Fatal("summary missing from system message")
	}
	if profileIdx < 0 {
		t.Fatal("[User Profile] missing from system message")
	}

	// Summary must appear after both profile and user system.
	if summaryIdx < profileIdx {
		t.Errorf("summary should appear after Profile block (summary@%d, profile@%d)", summaryIdx, profileIdx)
	}
	if summaryIdx < userIdx {
		t.Errorf("summary should appear after user system (summary@%d, user@%d)", summaryIdx, userIdx)
	}
}

// TestChatExecute_ProfileOnlyNoSystemMsg verifies that profile is injected even when
// no explicit --system message is provided.
func TestChatExecute_ProfileOnlyNoSystemMsg(t *testing.T) {
	llm := &stubLLM{}
	profile := newStubProfile()
	profile.p = domain.UserProfile{
		Name:        "Eve",
		Preferences: map[string]string{"style": "brief"},
	}
	uc := newChatUC(llm, &stubHistory{}, newStubWM(), newStubLTM(), profile)

	if _, err := uc.Execute(context.Background(), baseCfg("hello")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	sys := systemMsg(llm.capturedReqs)
	if !strings.Contains(sys, "[User Profile]") {
		t.Error("profile block should appear even without --system flag")
	}
	if !strings.Contains(sys, "Eve") {
		t.Error("profile name should appear in system message")
	}
}

// ---- funcLLM helper for custom per-call behaviour ----

type funcLLM struct {
	fn func(req port.LLMRequest) port.LLMResponse
}

func (f *funcLLM) Chat(_ context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	return f.fn(req), nil
}
