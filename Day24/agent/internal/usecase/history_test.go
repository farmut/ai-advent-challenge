package usecase_test

import (
	"fmt"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase"
)

// ---- TrimHistory ----

func TestTrimHistory_BelowLimit(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "q1"},
		{Role: domain.RoleAssistant, Content: "a1"},
	}
	keep, excess := usecase.TrimHistory(history, 10)
	if len(excess) != 0 {
		t.Errorf("expected no excess for 2 messages with limit 10, got %d", len(excess))
	}
	if len(keep) != 2 {
		t.Errorf("expected 2 keep messages, got %d", len(keep))
	}
}

func TestTrimHistory_AtLimit(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "q1"},
		{Role: domain.RoleAssistant, Content: "a1"},
		{Role: domain.RoleUser, Content: "q2"},
		{Role: domain.RoleAssistant, Content: "a2"},
	}
	keep, excess := usecase.TrimHistory(history, 4)
	if len(excess) != 0 {
		t.Errorf("expected no excess at limit, got %d", len(excess))
	}
	if len(keep) != 4 {
		t.Errorf("expected 4 keep messages at limit, got %d", len(keep))
	}
}

func TestTrimHistory_AboveLimit(t *testing.T) {
	history := make([]domain.Message, 12)
	for i := range history {
		if i%2 == 0 {
			history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("q%d", i/2+1)}
		} else {
			history[i] = domain.Message{Role: domain.RoleAssistant, Content: fmt.Sprintf("a%d", i/2+1)}
		}
	}
	keep, excess := usecase.TrimHistory(history, 10)
	if len(excess) != 2 {
		t.Errorf("expected 2 excess messages, got %d", len(excess))
	}
	if len(keep) != 10 {
		t.Errorf("expected 10 keep messages, got %d", len(keep))
	}
	if excess[0].Content != "q1" {
		t.Errorf("excess[0] should be q1 (oldest), got %q", excess[0].Content)
	}
	if keep[0].Content != "q2" {
		t.Errorf("keep[0] should be q2, got %q", keep[0].Content)
	}
}

func TestTrimHistory_ZeroLimit(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "q1"},
		{Role: domain.RoleAssistant, Content: "a1"},
	}
	keep, excess := usecase.TrimHistory(history, 0)
	if len(excess) != 0 {
		t.Errorf("zero limit should disable trimming, got %d excess", len(excess))
	}
	if len(keep) != 2 {
		t.Errorf("expected all 2 messages kept with zero limit, got %d", len(keep))
	}
}

func TestTrimHistory_NilHistory(t *testing.T) {
	keep, excess := usecase.TrimHistory(nil, 10)
	if len(excess) != 0 {
		t.Errorf("nil history should produce no excess, got %d", len(excess))
	}
	if keep != nil && len(keep) != 0 {
		t.Errorf("nil history should produce nil/empty keep, got %d", len(keep))
	}
}

func TestTrimHistory_ExcessContainsOldest(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "oldest"},
		{Role: domain.RoleAssistant, Content: "oldest-reply"},
		{Role: domain.RoleUser, Content: "newest"},
		{Role: domain.RoleAssistant, Content: "newest-reply"},
	}
	keep, excess := usecase.TrimHistory(history, 2)
	if len(excess) != 2 || excess[0].Content != "oldest" {
		t.Errorf("excess should contain oldest messages, got: %v", excess)
	}
	if len(keep) != 2 || keep[0].Content != "newest" {
		t.Errorf("keep should contain newest messages, got: %v", keep)
	}
}

// ---- SlidingWindow ----

func TestSlidingWindow_BelowLimit(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "q1"},
		{Role: domain.RoleAssistant, Content: "a1"},
	}
	got := usecase.SlidingWindow(history, 5)
	if len(got) != 2 {
		t.Errorf("expected all 2 messages kept, got %d", len(got))
	}
}

func TestSlidingWindow_AtLimit(t *testing.T) {
	history := make([]domain.Message, 5)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := usecase.SlidingWindow(history, 5)
	if len(got) != 5 {
		t.Errorf("expected 5 messages at limit, got %d", len(got))
	}
}

func TestSlidingWindow_AboveLimit(t *testing.T) {
	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := usecase.SlidingWindow(history, 5)
	if len(got) != 5 {
		t.Errorf("expected 5 messages, got %d", len(got))
	}
	if got[0].Content != "m5" {
		t.Errorf("first kept message should be m5, got %q", got[0].Content)
	}
	if got[4].Content != "m9" {
		t.Errorf("last kept message should be m9, got %q", got[4].Content)
	}
}

func TestSlidingWindow_ZeroDisables(t *testing.T) {
	history := make([]domain.Message, 10)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("m%d", i)}
	}
	got := usecase.SlidingWindow(history, 0)
	if len(got) != 10 {
		t.Errorf("window=0 should return all messages, got %d", len(got))
	}
}

func TestSlidingWindow_Nil(t *testing.T) {
	got := usecase.SlidingWindow(nil, 5)
	if len(got) != 0 {
		t.Errorf("nil history should return empty slice, got %d", len(got))
	}
}

func TestSummaryDisabledByDefault_NoTrim(t *testing.T) {
	history := make([]domain.Message, 6)
	for i := range history {
		history[i] = domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("m%d", i)}
	}
	keep, excess := usecase.TrimHistory(history, 10)
	if len(excess) != 0 {
		t.Errorf("no excess expected when below limit, got %d", len(excess))
	}
	if len(keep) != 6 {
		t.Errorf("expected all 6 kept, got %d", len(keep))
	}
}
