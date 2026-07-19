package usecase

import (
	"fmt"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// BranchUseCase manages branching operations: checkpoints, branch creation, and switching.
type BranchUseCase struct {
	repo port.BranchRepository
}

// NewBranchUseCase creates a BranchUseCase backed by the given repository.
func NewBranchUseCase(repo port.BranchRepository) *BranchUseCase {
	return &BranchUseCase{repo: repo}
}

// ListBranches returns the current branch state.
func (uc *BranchUseCase) ListBranches() (domain.BranchState, error) {
	return uc.repo.LoadState()
}

// SaveCheckpoint snapshots the current branch's history under the given name.
func (uc *BranchUseCase) SaveCheckpoint(name string) (int, error) {
	bs, err := uc.repo.LoadState()
	if err != nil {
		return 0, fmt.Errorf("load branch state: %w", err)
	}
	history, err := uc.repo.LoadHistory(bs.Current)
	if err != nil {
		return 0, fmt.Errorf("load history: %w", err)
	}
	bs.Checkpoints[name] = domain.BranchCheckpoint{
		Messages:  history,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := uc.repo.SaveState(bs); err != nil {
		return 0, fmt.Errorf("save branch state: %w", err)
	}
	return len(history), nil
}

// CreateBranch creates a new branch (optionally from a named checkpoint) and switches to it.
// Returns the number of messages copied to the new branch.
func (uc *BranchUseCase) CreateBranch(name, fromCheckpoint string) (int, error) {
	bs, err := uc.repo.LoadState()
	if err != nil {
		return 0, fmt.Errorf("load branch state: %w", err)
	}

	var src []domain.Message
	if fromCheckpoint != "" {
		cp, ok := bs.Checkpoints[fromCheckpoint]
		if !ok {
			return 0, fmt.Errorf("checkpoint %q not found; use --branch-list to see checkpoints", fromCheckpoint)
		}
		src = cp.Messages
	} else {
		src, _ = uc.repo.LoadHistory(bs.Current)
	}

	if err := uc.repo.SaveHistory(name, src); err != nil {
		return 0, fmt.Errorf("save branch history: %w", err)
	}

	exists := false
	for _, b := range bs.Branches {
		if b == name {
			exists = true
			break
		}
	}
	if !exists {
		bs.Branches = append(bs.Branches, name)
	}
	bs.Current = name

	if err := uc.repo.SaveState(bs); err != nil {
		return len(src), fmt.Errorf("save branch state: %w", err)
	}
	return len(src), nil
}

// Switch sets the active branch to name. Returns an error if the branch does not exist.
func (uc *BranchUseCase) Switch(name string) error {
	bs, err := uc.repo.LoadState()
	if err != nil {
		return fmt.Errorf("load branch state: %w", err)
	}
	found := false
	for _, b := range bs.Branches {
		if b == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("branch %q not found; use --branch-list to see branches", name)
	}
	bs.Current = name
	if err := uc.repo.SaveState(bs); err != nil {
		return fmt.Errorf("save branch state: %w", err)
	}
	return nil
}
