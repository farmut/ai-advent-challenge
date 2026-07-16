package storage

import (
	"testing"

	"ai-adv-agent/internal/domain"
)

func TestMemHistory_SeedIsCopied(t *testing.T) {
	seed := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	h := NewMemHistory(seed)
	// Mutating the caller's slice must not leak into the repo.
	seed[0].Content = "mutated"
	got, _ := h.Load()
	if got[0].Content != "hi" {
		t.Fatalf("seed not defensively copied: %q", got[0].Content)
	}
	// Save then Load round-trips.
	_ = h.Save([]domain.Message{{Role: domain.RoleAssistant, Content: "yo"}})
	got, _ = h.Load()
	if len(got) != 1 || got[0].Content != "yo" {
		t.Fatalf("save/load round-trip failed: %+v", got)
	}
}

func TestReadOnlyLTM_SaveIsNoOp(t *testing.T) {
	backing := NewMemLongTermMemory()
	_ = backing.Save(domain.LongTermMemory{Entries: map[string]string{"k": "v"}})

	ro := NewReadOnlyLTM(backing)
	got, _ := ro.Load()
	if got.Entries["k"] != "v" {
		t.Fatalf("read-only LTM must read through, got %+v", got)
	}
	// A sub-agent trying to overwrite the shared LTM must be ignored.
	_ = ro.Save(domain.LongTermMemory{Entries: map[string]string{"k": "OVERWRITTEN"}})
	got, _ = backing.Load()
	if got.Entries["k"] != "v" {
		t.Fatalf("read-only Save must not mutate the shared LTM, got %+v", got)
	}
}

func TestMemWorkingMemory_EmptyByDefault(t *testing.T) {
	wm := NewMemWorkingMemory()
	got, _ := wm.Load()
	if got.Facts == nil || len(got.Facts) != 0 {
		t.Fatalf("fresh WM must be empty non-nil map, got %+v", got)
	}
}
