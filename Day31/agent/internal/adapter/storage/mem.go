package storage

import "ai-adv-agent/internal/domain"

// This file provides in-memory implementations of the repository ports. They
// back a sub-agent's ephemeral short-term and working memory: state lives only
// for the duration of the sub-agent run and is never written to disk, so each
// spawned sub-agent starts from the context the orchestrator seeds it with and
// leaves no trace behind. The shared LTM/profile stay file-backed and are read
// by every sub-agent.

// MemHistory is an in-memory HistoryRepository, optionally seeded with a
// starting conversation slice.
type MemHistory struct{ msgs []domain.Message }

// NewMemHistory returns an in-memory history seeded with seed (copied).
func NewMemHistory(seed []domain.Message) *MemHistory {
	cp := append([]domain.Message(nil), seed...)
	return &MemHistory{msgs: cp}
}

func (r *MemHistory) Load() ([]domain.Message, error) {
	return append([]domain.Message(nil), r.msgs...), nil
}

func (r *MemHistory) Save(messages []domain.Message) error {
	r.msgs = append([]domain.Message(nil), messages...)
	return nil
}

// MemStats is an in-memory StatsRepository.
type MemStats struct{ stats domain.SessionStats }

func NewMemStats() *MemStats { return &MemStats{} }

func (r *MemStats) Load() (domain.SessionStats, error) { return r.stats, nil }
func (r *MemStats) Save(s domain.SessionStats) error   { r.stats = s; return nil }

// MemSummary is an in-memory SummaryRepository.
type MemSummary struct{ text string }

func NewMemSummary() *MemSummary { return &MemSummary{} }

func (r *MemSummary) Load() (string, error) { return r.text, nil }
func (r *MemSummary) Save(content string) error {
	r.text = content
	return nil
}

// MemFacts is an in-memory FactsRepository.
type MemFacts struct{ facts domain.FactsStore }

func NewMemFacts() *MemFacts {
	return &MemFacts{facts: domain.FactsStore{Facts: make(map[string]string)}}
}

func (r *MemFacts) Load() (domain.FactsStore, error) {
	if r.facts.Facts == nil {
		r.facts.Facts = make(map[string]string)
	}
	return r.facts, nil
}

func (r *MemFacts) Save(f domain.FactsStore) error { r.facts = f; return nil }

// MemWorkingMemory is an in-memory WorkingMemoryRepository — a sub-agent's own
// scratch working memory for the task it was handed.
type MemWorkingMemory struct{ wm domain.WorkingMemory }

func NewMemWorkingMemory() *MemWorkingMemory {
	return &MemWorkingMemory{wm: domain.WorkingMemory{Facts: make(map[string]string)}}
}

func (r *MemWorkingMemory) Load() (domain.WorkingMemory, error) {
	if r.wm.Facts == nil {
		r.wm.Facts = make(map[string]string)
	}
	return r.wm, nil
}

func (r *MemWorkingMemory) Save(wm domain.WorkingMemory) error { r.wm = wm; return nil }

// MemLongTermMemory is an in-memory LongTermMemoryRepository, used when the LTM
// layer is enabled but no persistent path is configured.
type MemLongTermMemory struct{ ltm domain.LongTermMemory }

func NewMemLongTermMemory() *MemLongTermMemory {
	return &MemLongTermMemory{ltm: domain.LongTermMemory{Entries: make(map[string]string)}}
}

func (r *MemLongTermMemory) Load() (domain.LongTermMemory, error) {
	if r.ltm.Entries == nil {
		r.ltm.Entries = make(map[string]string)
	}
	return r.ltm, nil
}

func (r *MemLongTermMemory) Save(ltm domain.LongTermMemory) error { r.ltm = ltm; return nil }

// ReadOnlyLTM wraps a LongTermMemoryRepository so Save is a no-op. It lets a
// sub-agent read the shared long-term memory without ever mutating the session's
// persistent LTM file.
type ReadOnlyLTM struct {
	inner interface {
		Load() (domain.LongTermMemory, error)
	}
}

// NewReadOnlyLTM wraps a loader so its LTM is read-only.
func NewReadOnlyLTM(inner interface {
	Load() (domain.LongTermMemory, error)
}) *ReadOnlyLTM {
	return &ReadOnlyLTM{inner: inner}
}

func (r *ReadOnlyLTM) Load() (domain.LongTermMemory, error) { return r.inner.Load() }
func (r *ReadOnlyLTM) Save(domain.LongTermMemory) error     { return nil }
