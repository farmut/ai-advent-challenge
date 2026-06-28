package usecase_test

import (
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase"
)

// ---- StripJSONFences ----

func TestStripJSONFences_Plain(t *testing.T) {
	input := `{"key": "value"}`
	got := usecase.StripJSONFences(input)
	if got != input {
		t.Errorf("plain JSON should be unchanged, got %q", got)
	}
}

func TestStripJSONFences_WithJSONFence(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	got := usecase.StripJSONFences(input)
	want := `{"key": "value"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripJSONFences_WithGenericFence(t *testing.T) {
	input := "```\n{\"key\": \"value\"}\n```"
	got := usecase.StripJSONFences(input)
	want := `{"key": "value"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---- WMSystemBlock ----

func TestWMSystemBlock_Empty(t *testing.T) {
	wm := domain.WorkingMemory{Facts: map[string]string{}}
	if block := usecase.WMSystemBlock(wm); block != "" {
		t.Errorf("empty WM should produce empty block, got %q", block)
	}
}

func TestWMSystemBlock_WithFacts(t *testing.T) {
	wm := domain.WorkingMemory{Facts: map[string]string{
		"goal":     "Write tests",
		"language": "Go",
	}}
	block := usecase.WMSystemBlock(wm)
	if !strings.Contains(block, "[Working Memory") {
		t.Error("block should contain [Working Memory header")
	}
	if !strings.Contains(block, "goal: Write tests") {
		t.Error("block should contain goal fact")
	}
	if !strings.Contains(block, "language: Go") {
		t.Error("block should contain language fact")
	}
}

func TestWMSystemBlock_Sorted(t *testing.T) {
	wm := domain.WorkingMemory{Facts: map[string]string{
		"zebra": "z",
		"alpha": "a",
		"beta":  "b",
	}}
	block := usecase.WMSystemBlock(wm)
	lines := strings.Split(block, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), block)
	}
	if !strings.HasPrefix(lines[1], "alpha:") {
		t.Errorf("first fact should be alpha, got: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "beta:") {
		t.Errorf("second fact should be beta, got: %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "zebra:") {
		t.Errorf("third fact should be zebra, got: %q", lines[3])
	}
}

// ---- LTMSystemBlock ----

func TestLTMSystemBlock_Empty(t *testing.T) {
	ltm := domain.LongTermMemory{Entries: map[string]string{}}
	if block := usecase.LTMSystemBlock(ltm); block != "" {
		t.Errorf("empty LTM should produce empty block, got %q", block)
	}
}

func TestLTMSystemBlock_WithEntries(t *testing.T) {
	ltm := domain.LongTermMemory{Entries: map[string]string{
		"user_name": "Alice",
		"preferred": "concise answers",
	}}
	block := usecase.LTMSystemBlock(ltm)
	if !strings.Contains(block, "[Long-term Memory") {
		t.Error("block should contain [Long-term Memory header")
	}
	if !strings.Contains(block, "user_name: Alice") {
		t.Error("block should contain user_name entry")
	}
	if !strings.Contains(block, "preferred: concise answers") {
		t.Error("block should contain preferred entry")
	}
}

func TestLTMSystemBlock_Sorted(t *testing.T) {
	ltm := domain.LongTermMemory{Entries: map[string]string{
		"zebra": "z",
		"alpha": "a",
		"beta":  "b",
	}}
	block := usecase.LTMSystemBlock(ltm)
	lines := strings.Split(block, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[1], "alpha:") {
		t.Errorf("first entry should be alpha, got: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "beta:") {
		t.Errorf("second entry should be beta, got: %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "zebra:") {
		t.Errorf("third entry should be zebra, got: %q", lines[3])
	}
}

// ---- FactsSystemBlock ----

func TestFactsSystemBlock_Empty(t *testing.T) {
	fs := domain.FactsStore{Facts: map[string]string{}}
	if block := usecase.FactsSystemBlock(fs); block != "" {
		t.Errorf("empty facts should produce empty block, got %q", block)
	}
}

func TestFactsSystemBlock_WithFacts(t *testing.T) {
	fs := domain.FactsStore{Facts: map[string]string{
		"goal":     "Write tests",
		"language": "Go",
	}}
	block := usecase.FactsSystemBlock(fs)
	if !strings.Contains(block, "[Sticky Facts]") {
		t.Error("block should contain [Sticky Facts] header")
	}
	if !strings.Contains(block, "goal: Write tests") {
		t.Error("block should contain goal fact")
	}
	if !strings.Contains(block, "language: Go") {
		t.Error("block should contain language fact")
	}
}

func TestFactsSystemBlock_Sorted(t *testing.T) {
	fs := domain.FactsStore{Facts: map[string]string{
		"zebra": "z",
		"alpha": "a",
		"beta":  "b",
	}}
	block := usecase.FactsSystemBlock(fs)
	lines := strings.Split(block, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d: %q", len(lines), block)
	}
	if !strings.HasPrefix(lines[1], "alpha:") {
		t.Errorf("first fact should be alpha, got: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "beta:") {
		t.Errorf("second fact should be beta, got: %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "zebra:") {
		t.Errorf("third fact should be zebra, got: %q", lines[3])
	}
}
