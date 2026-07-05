package storage_test

import (
	"os"
	"strings"
	"testing"

	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/domain"
)

// ---- StatsPath ----

func TestStatsPath_Standard(t *testing.T) {
	got := storage.StatsPath("chat_history.json")
	want := "chat_history.stats.json"
	if got != want {
		t.Errorf("StatsPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestStatsPath_EmptyDisabled(t *testing.T) {
	if p := storage.StatsPath(""); p != "" {
		t.Errorf("StatsPath(\"\") should be empty, got %q", p)
	}
}

func TestStatsPath_NoExtension(t *testing.T) {
	got := storage.StatsPath("history")
	want := "history.stats"
	if got != want {
		t.Errorf("StatsPath(history) = %q, want %q", got, want)
	}
}

func TestStatsPath_WithDirectory(t *testing.T) {
	got := storage.StatsPath("/tmp/my_chat.json")
	want := "/tmp/my_chat.stats.json"
	if got != want {
		t.Errorf("StatsPath(/tmp/my_chat.json) = %q, want %q", got, want)
	}
}

// ---- SummaryPath ----

func TestSummaryPath_Standard(t *testing.T) {
	got := storage.SummaryPath("chat_history.json")
	want := "chat_history.summary.txt"
	if got != want {
		t.Errorf("SummaryPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestSummaryPath_Empty(t *testing.T) {
	if p := storage.SummaryPath(""); p != "" {
		t.Errorf("SummaryPath(\"\") should be empty, got %q", p)
	}
}

func TestSummaryPath_WithDirectory(t *testing.T) {
	got := storage.SummaryPath("/tmp/my_chat.json")
	want := "/tmp/my_chat.summary.txt"
	if got != want {
		t.Errorf("SummaryPath(/tmp/my_chat.json) = %q, want %q", got, want)
	}
}

func TestSummaryPath_NoExtension(t *testing.T) {
	got := storage.SummaryPath("history")
	want := "history.summary.txt"
	if got != want {
		t.Errorf("SummaryPath(history) = %q, want %q", got, want)
	}
}

// ---- FactsPath ----

func TestFactsPath_Standard(t *testing.T) {
	got := storage.FactsPath("chat_history.json")
	want := "chat_history.facts.json"
	if got != want {
		t.Errorf("FactsPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestFactsPath_Empty(t *testing.T) {
	if p := storage.FactsPath(""); p != "" {
		t.Errorf("FactsPath(\"\") should be empty, got %q", p)
	}
}

func TestFactsPath_WithDirectory(t *testing.T) {
	got := storage.FactsPath("/tmp/chat.json")
	want := "/tmp/chat.facts.json"
	if got != want {
		t.Errorf("FactsPath(/tmp/chat.json) = %q, want %q", got, want)
	}
}

// ---- WMPath ----

func TestWMPath_Standard(t *testing.T) {
	got := storage.WMPath("chat_history.json")
	want := "chat_history.wm.json"
	if got != want {
		t.Errorf("WMPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestWMPath_Empty(t *testing.T) {
	if p := storage.WMPath(""); p != "" {
		t.Errorf("WMPath(\"\") should be empty, got %q", p)
	}
}

func TestWMPath_WithDirectory(t *testing.T) {
	got := storage.WMPath("/tmp/chat.json")
	want := "/tmp/chat.wm.json"
	if got != want {
		t.Errorf("WMPath(/tmp/chat.json) = %q, want %q", got, want)
	}
}

// ---- LTMPath ----

func TestLTMPath_Standard(t *testing.T) {
	got := storage.LTMPath("chat_history.json")
	want := "chat_history.ltm.json"
	if got != want {
		t.Errorf("LTMPath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestLTMPath_Empty(t *testing.T) {
	if p := storage.LTMPath(""); p != "" {
		t.Errorf("LTMPath(\"\") should be empty, got %q", p)
	}
}

func TestLTMPath_WithDirectory(t *testing.T) {
	got := storage.LTMPath("/tmp/chat.json")
	want := "/tmp/chat.ltm.json"
	if got != want {
		t.Errorf("LTMPath(/tmp/chat.json) = %q, want %q", got, want)
	}
}

// ---- BranchStatePath / BranchHistoryPath ----

func TestBranchStatePath_Standard(t *testing.T) {
	got := storage.BranchStatePath("chat_history.json")
	want := "chat_history.branch-state.json"
	if got != want {
		t.Errorf("BranchStatePath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestBranchStatePath_Empty(t *testing.T) {
	if p := storage.BranchStatePath(""); p != "" {
		t.Errorf("BranchStatePath(\"\") should be empty, got %q", p)
	}
}

func TestBranchHistoryPath_Main(t *testing.T) {
	got := storage.BranchHistoryPath("chat_history.json", "main")
	want := "chat_history.json"
	if got != want {
		t.Errorf("BranchHistoryPath for main = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_Empty(t *testing.T) {
	got := storage.BranchHistoryPath("chat_history.json", "")
	want := "chat_history.json"
	if got != want {
		t.Errorf("BranchHistoryPath for empty = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_Named(t *testing.T) {
	got := storage.BranchHistoryPath("chat_history.json", "feature-a")
	want := "chat_history.branch-feature-a.json"
	if got != want {
		t.Errorf("BranchHistoryPath for feature-a = %q, want %q", got, want)
	}
}

func TestBranchHistoryPath_EmptyBase(t *testing.T) {
	got := storage.BranchHistoryPath("", "feature-a")
	if got != "" {
		t.Errorf("BranchHistoryPath with empty base should be empty, got %q", got)
	}
}

func TestCurrentBranchHistoryPath_Main(t *testing.T) {
	bs := domain.DefaultBranchState()
	got := storage.CurrentBranchHistoryPath("chat_history.json", bs)
	want := "chat_history.json"
	if got != want {
		t.Errorf("CurrentBranchHistoryPath(main) = %q, want %q", got, want)
	}
}

func TestCurrentBranchHistoryPath_Named(t *testing.T) {
	bs := domain.BranchState{Current: "topic-a"}
	got := storage.CurrentBranchHistoryPath("chat_history.json", bs)
	want := "chat_history.branch-topic-a.json"
	if got != want {
		t.Errorf("CurrentBranchHistoryPath(topic-a) = %q, want %q", got, want)
	}
}

// ---- HistoryFile ----

func TestLoadHistory_FileNotExist(t *testing.T) {
	r := storage.NewHistoryFile("/tmp/nonexistent_history_xyz.json")
	msgs, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil slice for missing file, got: %v", msgs)
	}
}

func TestLoadHistory_EmptyPath(t *testing.T) {
	r := storage.NewHistoryFile("")
	msgs, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil for empty path, got: %v", msgs)
	}
}

func TestSaveAndLoadHistory(t *testing.T) {
	f, err := os.CreateTemp("", "history_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewHistoryFile(f.Name())
	original := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "world"},
	}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != len(original) {
		t.Fatalf("expected %d messages, got %d", len(original), len(loaded))
	}
	for i, msg := range loaded {
		if msg.Role != original[i].Role || msg.Content != original[i].Content {
			t.Errorf("message[%d] mismatch: got {%s, %q}, want {%s, %q}", i, msg.Role, msg.Content, original[i].Role, original[i].Content)
		}
	}
}

func TestLoadHistory_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "history_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewHistoryFile(f.Name())
	_, err = r.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSaveHistory_EmptyPath(t *testing.T) {
	r := storage.NewHistoryFile("")
	if err := r.Save([]domain.Message{{Role: domain.RoleUser, Content: "test"}}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

// ---- StatsFile ----

func TestSaveAndLoadStats(t *testing.T) {
	f, err := os.CreateTemp("", "stats_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewStatsFile(f.Name())
	original := domain.SessionStats{
		PromptTokens:         1000,
		CompletionTokens:     200,
		TotalTokens:          1200,
		EstimatedCostUSD:     0.00042,
		Calls:                3,
		LastCallPromptTokens: 450,
	}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != original {
		t.Errorf("loaded stats %+v != original %+v", loaded, original)
	}
}

func TestLoadStats_FileNotExist(t *testing.T) {
	r := storage.NewStatsFile("/tmp/nonexistent_stats_xyz.json")
	s, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if s.Calls != 0 || s.TotalTokens != 0 {
		t.Errorf("expected zero stats for missing file, got: %+v", s)
	}
}

func TestLoadStats_EmptyPath(t *testing.T) {
	r := storage.NewStatsFile("")
	s, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Calls != 0 {
		t.Errorf("expected zero stats for empty path, got: %+v", s)
	}
}

func TestSaveStats_EmptyPath(t *testing.T) {
	r := storage.NewStatsFile("")
	if err := r.Save(domain.SessionStats{Calls: 5}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadStats_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "stats_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewStatsFile(f.Name())
	_, err = r.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON stats, got nil")
	}
}

func TestSaveAndLoadStats_WithLastCallPrompt(t *testing.T) {
	f, err := os.CreateTemp("", "stats_ctx_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewStatsFile(f.Name())
	original := domain.SessionStats{
		PromptTokens:         3000,
		CompletionTokens:     600,
		TotalTokens:          3600,
		EstimatedCostUSD:     0.00090,
		Calls:                5,
		LastCallPromptTokens: 1200,
	}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.LastCallPromptTokens != original.LastCallPromptTokens {
		t.Errorf("LastCallPromptTokens: got %d, want %d", loaded.LastCallPromptTokens, original.LastCallPromptTokens)
	}
	if loaded != original {
		t.Errorf("full stats mismatch: got %+v, want %+v", loaded, original)
	}
}

// ---- SummaryFile ----

func TestLoadSummary_FileNotExist(t *testing.T) {
	r := storage.NewSummaryFile("/tmp/nonexistent_summary_xyz.txt")
	s, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if s != "" {
		t.Fatalf("expected empty string for missing file, got: %q", s)
	}
}

func TestLoadSummary_EmptyPath(t *testing.T) {
	r := storage.NewSummaryFile("")
	s, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "" {
		t.Fatalf("expected empty string for empty path, got: %q", s)
	}
}

func TestSaveAndLoadSummary(t *testing.T) {
	f, err := os.CreateTemp("", "summary_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewSummaryFile(f.Name())
	content := "The user asked about Go programming. The assistant explained goroutines and channels."
	if err := r.Save(content); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != content {
		t.Errorf("loaded summary %q != original %q", loaded, content)
	}
}

func TestSaveSummary_EmptyPath(t *testing.T) {
	r := storage.NewSummaryFile("")
	if err := r.Save("some content"); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

func TestSaveAndLoadSummary_TrimsWhitespace(t *testing.T) {
	f, err := os.CreateTemp("", "summary_ws_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewSummaryFile(f.Name())
	content := "Some summary text"
	if err := r.Save(content); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != content {
		t.Errorf("trailing newline not trimmed: got %q, want %q", loaded, content)
	}
}

// ---- FactsFile ----

func TestLoadFacts_FileNotExist(t *testing.T) {
	r := storage.NewFactsFile("/tmp/nonexistent_facts_xyz.json")
	fs, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if fs.Facts == nil {
		t.Fatal("expected non-nil Facts map")
	}
	if len(fs.Facts) != 0 {
		t.Errorf("expected empty map, got: %v", fs.Facts)
	}
}

func TestLoadFacts_EmptyPath(t *testing.T) {
	r := storage.NewFactsFile("")
	fs, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs.Facts == nil {
		t.Fatal("expected non-nil Facts map for empty path")
	}
}

func TestSaveAndLoadFacts(t *testing.T) {
	f, err := os.CreateTemp("", "facts_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewFactsFile(f.Name())
	original := domain.FactsStore{Facts: map[string]string{
		"goal":       "Build a REST API",
		"language":   "Go",
		"constraint": "No external libraries",
	}}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Facts) != len(original.Facts) {
		t.Fatalf("expected %d facts, got %d", len(original.Facts), len(loaded.Facts))
	}
	for k, v := range original.Facts {
		if loaded.Facts[k] != v {
			t.Errorf("facts[%q] = %q, want %q", k, loaded.Facts[k], v)
		}
	}
}

func TestSaveFacts_EmptyPath(t *testing.T) {
	r := storage.NewFactsFile("")
	if err := r.Save(domain.FactsStore{Facts: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadFacts_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "facts_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewFactsFile(f.Name())
	_, err = r.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---- WorkingMemoryFile ----

func TestLoadWM_FileNotExist(t *testing.T) {
	r := storage.NewWorkingMemoryFile("/tmp/nonexistent_wm_xyz.json")
	wm, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if wm.Facts == nil {
		t.Fatal("expected non-nil Facts map")
	}
	if len(wm.Facts) != 0 {
		t.Errorf("expected empty map, got: %v", wm.Facts)
	}
}

func TestLoadWM_EmptyPath(t *testing.T) {
	r := storage.NewWorkingMemoryFile("")
	wm, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wm.Facts == nil {
		t.Fatal("expected non-nil Facts map for empty path")
	}
}

func TestSaveAndLoadWM(t *testing.T) {
	f, err := os.CreateTemp("", "wm_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewWorkingMemoryFile(f.Name())
	original := domain.WorkingMemory{Facts: map[string]string{
		"goal":       "Build a REST API",
		"language":   "Go",
		"constraint": "No external libs",
	}}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Facts) != len(original.Facts) {
		t.Fatalf("expected %d facts, got %d", len(original.Facts), len(loaded.Facts))
	}
	for k, v := range original.Facts {
		if loaded.Facts[k] != v {
			t.Errorf("wm.Facts[%q] = %q, want %q", k, loaded.Facts[k], v)
		}
	}
	if loaded.UpdatedAt == "" {
		t.Error("UpdatedAt should be set after Save")
	}
}

func TestSaveWM_EmptyPath(t *testing.T) {
	r := storage.NewWorkingMemoryFile("")
	if err := r.Save(domain.WorkingMemory{Facts: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadWM_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "wm_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewWorkingMemoryFile(f.Name())
	_, err = r.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---- LongTermMemoryFile ----

func TestLoadLTM_FileNotExist(t *testing.T) {
	r := storage.NewLongTermMemoryFile("/tmp/nonexistent_ltm_xyz.json")
	ltm, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if ltm.Entries == nil {
		t.Fatal("expected non-nil Entries map")
	}
	if len(ltm.Entries) != 0 {
		t.Errorf("expected empty map, got: %v", ltm.Entries)
	}
}

func TestLoadLTM_EmptyPath(t *testing.T) {
	r := storage.NewLongTermMemoryFile("")
	ltm, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ltm.Entries == nil {
		t.Fatal("expected non-nil Entries map for empty path")
	}
}

func TestSaveAndLoadLTM(t *testing.T) {
	f, err := os.CreateTemp("", "ltm_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewLongTermMemoryFile(f.Name())
	original := domain.LongTermMemory{Entries: map[string]string{
		"user_name": "Alice",
		"preferred": "Go over Python",
		"long_goal": "Build production-grade APIs",
	}}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Entries) != len(original.Entries) {
		t.Fatalf("expected %d entries, got %d", len(original.Entries), len(loaded.Entries))
	}
	for k, v := range original.Entries {
		if loaded.Entries[k] != v {
			t.Errorf("ltm.Entries[%q] = %q, want %q", k, loaded.Entries[k], v)
		}
	}
	if loaded.UpdatedAt == "" {
		t.Error("UpdatedAt should be set after Save")
	}
}

func TestSaveLTM_EmptyPath(t *testing.T) {
	r := storage.NewLongTermMemoryFile("")
	if err := r.Save(domain.LongTermMemory{Entries: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

func TestLoadLTM_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "ltm_bad_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not json")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewLongTermMemoryFile(f.Name())
	_, err = r.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---- BranchFile ----

func TestDefaultBranchState(t *testing.T) {
	bs := domain.DefaultBranchState()
	if bs.Current != "main" {
		t.Errorf("default current = %q, want main", bs.Current)
	}
	if len(bs.Branches) != 1 || bs.Branches[0] != "main" {
		t.Errorf("default branches = %v, want [main]", bs.Branches)
	}
	if bs.Checkpoints == nil {
		t.Error("default checkpoints should be non-nil map")
	}
}

func TestLoadBranchState_FileNotExist(t *testing.T) {
	r := storage.NewBranchFile("/tmp/nonexistent_branch_xyz.json")
	bs, err := r.LoadState()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if bs.Current != "main" {
		t.Errorf("expected default current=main, got %q", bs.Current)
	}
}

func TestLoadBranchState_EmptyPath(t *testing.T) {
	r := storage.NewBranchFile("")
	bs, err := r.LoadState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bs.Current != "main" {
		t.Errorf("empty path should return default state, got current=%q", bs.Current)
	}
}

func TestSaveAndLoadBranchState(t *testing.T) {
	f, err := os.CreateTemp("", "branch_state_*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	// BranchFile derives state path from basePath, so use a temp history-like path
	basePath := f.Name()
	r := storage.NewBranchFile(basePath)

	original := domain.BranchState{
		Current:  "feature-x",
		Branches: []string{"main", "feature-x", "hotfix"},
		Checkpoints: map[string]domain.BranchCheckpoint{
			"cp1": {
				Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
				CreatedAt: "2026-01-01T00:00:00Z",
			},
		},
	}
	if err := r.SaveState(original); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}
	loaded, err := r.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded.Current != original.Current {
		t.Errorf("current: got %q, want %q", loaded.Current, original.Current)
	}
	if len(loaded.Branches) != len(original.Branches) {
		t.Errorf("branches len: got %d, want %d", len(loaded.Branches), len(original.Branches))
	}
	cp, ok := loaded.Checkpoints["cp1"]
	if !ok {
		t.Fatal("checkpoint cp1 not found after load")
	}
	if len(cp.Messages) != 1 || cp.Messages[0].Content != "hello" {
		t.Errorf("checkpoint messages mismatch: %v", cp.Messages)
	}
}

func TestSaveBranchState_EmptyPath(t *testing.T) {
	r := storage.NewBranchFile("")
	if err := r.SaveState(domain.DefaultBranchState()); err != nil {
		t.Fatalf("SaveState with empty path should be a no-op, got: %v", err)
	}
}

// ---- ProfilePath ----

func TestProfilePath_Standard(t *testing.T) {
	got := storage.ProfilePath("chat_history.json")
	want := "chat_history.profile.md"
	if got != want {
		t.Errorf("ProfilePath(chat_history.json) = %q, want %q", got, want)
	}
}

func TestProfilePath_Empty(t *testing.T) {
	if p := storage.ProfilePath(""); p != "" {
		t.Errorf("ProfilePath(\"\") should be empty, got %q", p)
	}
}

func TestProfilePath_WithDirectory(t *testing.T) {
	got := storage.ProfilePath("/tmp/chat.json")
	want := "/tmp/chat.profile.md"
	if got != want {
		t.Errorf("ProfilePath(/tmp/chat.json) = %q, want %q", got, want)
	}
}

// ---- ProfileFile (Markdown) ----

func TestLoadProfile_FileNotExist(t *testing.T) {
	r := storage.NewProfileFile("/tmp/nonexistent_profile_xyz.md")
	p, err := r.Load()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if p.Preferences == nil {
		t.Fatal("expected non-nil Preferences map")
	}
	if len(p.Preferences) != 0 {
		t.Errorf("expected empty preferences for missing file, got: %v", p.Preferences)
	}
	if p.Name != "" {
		t.Errorf("expected empty name for missing file, got: %q", p.Name)
	}
}

func TestLoadProfile_EmptyPath(t *testing.T) {
	r := storage.NewProfileFile("")
	p, err := r.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Preferences == nil {
		t.Fatal("expected non-nil Preferences map for empty path")
	}
}

func TestSaveAndLoadProfile(t *testing.T) {
	f, err := os.CreateTemp("", "profile_*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewProfileFile(f.Name())
	original := domain.UserProfile{
		Name: "Alice",
		Preferences: map[string]string{
			"style":    "concise",
			"format":   "markdown",
			"language": "russian",
		},
	}
	if err := r.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, original.Name)
	}
	if len(loaded.Preferences) != len(original.Preferences) {
		t.Fatalf("Preferences len: got %d, want %d", len(loaded.Preferences), len(original.Preferences))
	}
	for k, v := range original.Preferences {
		if loaded.Preferences[k] != v {
			t.Errorf("Preferences[%q] = %q, want %q", k, loaded.Preferences[k], v)
		}
	}
}

func TestSaveAndLoadProfile_MarkdownContent(t *testing.T) {
	f, err := os.CreateTemp("", "profile_md_*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewProfileFile(f.Name())
	if err := r.Save(domain.UserProfile{
		Name:        "Bob",
		Preferences: map[string]string{"language": "russian", "style": "concise"},
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw, _ := os.ReadFile(f.Name())
	content := string(raw)
	for _, want := range []string{
		"# User Profile",
		"**Name:** Bob",
		"## Preferences",
		"- **language:** russian",
		"- **style:** concise",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("saved Markdown missing %q\nfull content:\n%s", want, content)
		}
	}
}

func TestSaveProfile_EmptyPath(t *testing.T) {
	r := storage.NewProfileFile("")
	if err := r.Save(domain.UserProfile{Name: "Bob", Preferences: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("Save with empty path should be a no-op, got: %v", err)
	}
}

// Markdown parser is forgiving: unrecognised content returns an empty profile, not an error.
func TestLoadProfile_UnrecognisedContent(t *testing.T) {
	f, err := os.CreateTemp("", "profile_bad_*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not a valid profile\nrandom text\n")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewProfileFile(f.Name())
	p, err := r.Load()
	if err != nil {
		t.Fatalf("expected no error for unrecognised content, got: %v", err)
	}
	if p.Name != "" {
		t.Errorf("expected empty name, got: %q", p.Name)
	}
	if len(p.Preferences) != 0 {
		t.Errorf("expected empty preferences, got: %v", p.Preferences)
	}
}

func TestSaveProfile_NilPreferencesBecomesEmptySection(t *testing.T) {
	f, err := os.CreateTemp("", "profile_nil_*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewProfileFile(f.Name())
	if err := r.Save(domain.UserProfile{Name: "Eve"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Preferences == nil {
		t.Error("Preferences should be non-nil after load")
	}
	if loaded.Name != "Eve" {
		t.Errorf("Name: got %q, want Eve", loaded.Name)
	}
}

func TestLoadProfile_ManuallyWrittenMarkdown(t *testing.T) {
	f, err := os.CreateTemp("", "profile_manual_*.md")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a file edited by hand (extra blank lines, comments)
	f.WriteString("# User Profile\n\n**Name:** Charlie\n\n## Preferences\n\n- **language:** english\n- **style:** technical\n\n<!-- updated: 2026-01-01T00:00:00Z -->\n")
	f.Close()
	defer os.Remove(f.Name())

	r := storage.NewProfileFile(f.Name())
	p, err := r.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.Name != "Charlie" {
		t.Errorf("Name: got %q, want Charlie", p.Name)
	}
	if p.Preferences["language"] != "english" {
		t.Errorf("language: got %q, want english", p.Preferences["language"])
	}
	if p.Preferences["style"] != "technical" {
		t.Errorf("style: got %q, want technical", p.Preferences["style"])
	}
}
