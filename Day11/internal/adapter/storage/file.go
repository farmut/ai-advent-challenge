package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
)

// ---- Path helpers ----

// StatsPath derives the stats file path from the history file path.
func StatsPath(historyPath string) string { return derived(historyPath, ".stats", "") }

// SummaryPath derives the summary file path from the history file path.
func SummaryPath(historyPath string) string { return derived(historyPath, ".summary", ".txt") }

// FactsPath derives the sticky-facts file path from the history file path.
func FactsPath(historyPath string) string { return derived(historyPath, ".facts", ".json") }

// WMPath derives the working memory file path from the history file path.
func WMPath(historyPath string) string { return derived(historyPath, ".wm", "") }

// LTMPath derives the long-term memory file path from the history file path.
func LTMPath(historyPath string) string { return derived(historyPath, ".ltm", "") }

// BranchStatePath derives the branch-state file path from the history file path.
func BranchStatePath(historyPath string) string {
	return derived(historyPath, ".branch-state", ".json")
}

// BranchHistoryPath returns the history file for a named branch.
// "main" (or empty) resolves to the original history file.
func BranchHistoryPath(historyPath, branchName string) string {
	if historyPath == "" {
		return ""
	}
	if branchName == "" || branchName == "main" {
		return historyPath
	}
	ext := filepath.Ext(historyPath)
	base := strings.TrimSuffix(historyPath, ext)
	return base + ".branch-" + branchName + ext
}

// CurrentBranchHistoryPath resolves the history path for the branch currently active in bs.
func CurrentBranchHistoryPath(historyPath string, bs domain.BranchState) string {
	return BranchHistoryPath(historyPath, bs.Current)
}

func derived(base, suffix, forceExt string) string {
	if base == "" {
		return ""
	}
	origExt := filepath.Ext(base)
	baseName := strings.TrimSuffix(base, origExt)
	ext := forceExt
	if ext == "" {
		ext = origExt
	}
	return baseName + suffix + ext
}

// ---- Generic JSON helpers ----

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ---- HistoryFile ----

// HistoryFile is a file-backed HistoryRepository.
type HistoryFile struct{ path string }

// NewHistoryFile creates a HistoryFile at the given path. Empty path is a no-op store.
func NewHistoryFile(path string) *HistoryFile { return &HistoryFile{path} }

func (r *HistoryFile) Load() ([]domain.Message, error) {
	if r.path == "" {
		return nil, nil
	}
	var msgs []domain.Message
	err := readJSON(r.path, &msgs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invalid history file: %w", err)
	}
	return msgs, nil
}

func (r *HistoryFile) Save(messages []domain.Message) error {
	if r.path == "" {
		return nil
	}
	return writeJSON(r.path, messages)
}

// ---- StatsFile ----

// StatsFile is a file-backed StatsRepository.
type StatsFile struct{ path string }

// NewStatsFile creates a StatsFile at the given path.
func NewStatsFile(path string) *StatsFile { return &StatsFile{path} }

func (r *StatsFile) Load() (domain.SessionStats, error) {
	if r.path == "" {
		return domain.SessionStats{}, nil
	}
	var s domain.SessionStats
	err := readJSON(r.path, &s)
	if os.IsNotExist(err) {
		return domain.SessionStats{}, nil
	}
	if err != nil {
		return domain.SessionStats{}, fmt.Errorf("invalid stats file: %w", err)
	}
	return s, nil
}

func (r *StatsFile) Save(stats domain.SessionStats) error {
	if r.path == "" {
		return nil
	}
	return writeJSON(r.path, stats)
}

// ---- SummaryFile ----

// SummaryFile is a file-backed SummaryRepository (plain text).
type SummaryFile struct{ path string }

// NewSummaryFile creates a SummaryFile at the given path.
func NewSummaryFile(path string) *SummaryFile { return &SummaryFile{path} }

func (r *SummaryFile) Load() (string, error) {
	if r.path == "" {
		return "", nil
	}
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *SummaryFile) Save(content string) error {
	if r.path == "" {
		return nil
	}
	return os.WriteFile(r.path, []byte(content+"\n"), 0644)
}

// ---- FactsFile ----

// FactsFile is a file-backed FactsRepository.
type FactsFile struct{ path string }

// NewFactsFile creates a FactsFile at the given path.
func NewFactsFile(path string) *FactsFile { return &FactsFile{path} }

func (r *FactsFile) Load() (domain.FactsStore, error) {
	empty := domain.FactsStore{Facts: make(map[string]string)}
	if r.path == "" {
		return empty, nil
	}
	var fs domain.FactsStore
	err := readJSON(r.path, &fs)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("invalid facts file: %w", err)
	}
	if fs.Facts == nil {
		fs.Facts = make(map[string]string)
	}
	return fs, nil
}

func (r *FactsFile) Save(facts domain.FactsStore) error {
	if r.path == "" {
		return nil
	}
	return writeJSON(r.path, facts)
}

// ---- WorkingMemoryFile ----

// WorkingMemoryFile is a file-backed WorkingMemoryRepository.
type WorkingMemoryFile struct{ path string }

// NewWorkingMemoryFile creates a WorkingMemoryFile at the given path.
func NewWorkingMemoryFile(path string) *WorkingMemoryFile { return &WorkingMemoryFile{path} }

func (r *WorkingMemoryFile) Load() (domain.WorkingMemory, error) {
	empty := domain.WorkingMemory{Facts: make(map[string]string)}
	if r.path == "" {
		return empty, nil
	}
	var wm domain.WorkingMemory
	err := readJSON(r.path, &wm)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("invalid working memory file: %w", err)
	}
	if wm.Facts == nil {
		wm.Facts = make(map[string]string)
	}
	return wm, nil
}

func (r *WorkingMemoryFile) Save(wm domain.WorkingMemory) error {
	if r.path == "" {
		return nil
	}
	wm.UpdatedAt = time.Now().Format(time.RFC3339)
	return writeJSON(r.path, wm)
}

// ---- LongTermMemoryFile ----

// LongTermMemoryFile is a file-backed LongTermMemoryRepository.
type LongTermMemoryFile struct{ path string }

// NewLongTermMemoryFile creates a LongTermMemoryFile at the given path.
func NewLongTermMemoryFile(path string) *LongTermMemoryFile { return &LongTermMemoryFile{path} }

func (r *LongTermMemoryFile) Load() (domain.LongTermMemory, error) {
	empty := domain.LongTermMemory{Entries: make(map[string]string)}
	if r.path == "" {
		return empty, nil
	}
	var ltm domain.LongTermMemory
	err := readJSON(r.path, &ltm)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("invalid long-term memory file: %w", err)
	}
	if ltm.Entries == nil {
		ltm.Entries = make(map[string]string)
	}
	return ltm, nil
}

func (r *LongTermMemoryFile) Save(ltm domain.LongTermMemory) error {
	if r.path == "" {
		return nil
	}
	ltm.UpdatedAt = time.Now().Format(time.RFC3339)
	return writeJSON(r.path, ltm)
}

// ---- BranchFile ----

// BranchFile is a file-backed BranchRepository.
type BranchFile struct{ basePath string }

// NewBranchFile creates a BranchFile whose state and branch histories are derived from basePath.
func NewBranchFile(basePath string) *BranchFile { return &BranchFile{basePath} }

func (r *BranchFile) LoadState() (domain.BranchState, error) {
	path := BranchStatePath(r.basePath)
	if path == "" {
		return domain.DefaultBranchState(), nil
	}
	var bs domain.BranchState
	err := readJSON(path, &bs)
	if os.IsNotExist(err) {
		return domain.DefaultBranchState(), nil
	}
	if err != nil {
		return domain.DefaultBranchState(), fmt.Errorf("invalid branch state: %w", err)
	}
	if bs.Checkpoints == nil {
		bs.Checkpoints = make(map[string]domain.BranchCheckpoint)
	}
	if bs.Current == "" {
		bs.Current = "main"
	}
	return bs, nil
}

func (r *BranchFile) SaveState(bs domain.BranchState) error {
	path := BranchStatePath(r.basePath)
	if path == "" {
		return nil
	}
	return writeJSON(path, bs)
}

func (r *BranchFile) LoadHistory(branchName string) ([]domain.Message, error) {
	path := BranchHistoryPath(r.basePath, branchName)
	if path == "" {
		return nil, nil
	}
	var msgs []domain.Message
	err := readJSON(path, &msgs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invalid branch history: %w", err)
	}
	return msgs, nil
}

func (r *BranchFile) SaveHistory(branchName string, messages []domain.Message) error {
	path := BranchHistoryPath(r.basePath, branchName)
	if path == "" {
		return nil
	}
	return writeJSON(path, messages)
}
