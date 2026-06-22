package domain

import (
	"fmt"
	"time"
)

// TaskPhase is a stage in the 4-phase task state machine.
type TaskPhase string

const (
	PhasePlanning   TaskPhase = "planning"
	PhaseExecution  TaskPhase = "execution"
	PhaseValidation TaskPhase = "validation"
	PhaseDone       TaskPhase = "done"
)

// allowedTransitions defines the valid forward and backward moves.
var allowedTransitions = map[TaskPhase][]TaskPhase{
	PhasePlanning:   {PhaseExecution},
	PhaseExecution:  {PhaseValidation},
	PhaseValidation: {PhaseDone, PhasePlanning},
}

// TaskState is the persisted state of a single task through the state machine.
type TaskState struct {
	ID              string    `json:"id"`
	Phase           TaskPhase `json:"phase"`
	Task            string    `json:"task"`                        // original user task description
	Plan            string    `json:"plan"`                        // approved plan (set after planning approval)
	PendingPlan     string    `json:"pending_plan,omitempty"`      // generated plan shown but not yet approved (survives pause)
	Result          string    `json:"result"`                      // execution output (set after execution)
	Iteration       int       `json:"iteration"`                   // re-planning round counter (starts at 1)
	PendingFeedback string    `json:"pending_feedback,omitempty"`  // feedback carried across a pause
	CreatedAt       string    `json:"created_at"`
	UpdatedAt       string    `json:"updated_at"`
}

// NewTaskState creates a new TaskState in the planning phase.
func NewTaskState(id, task string) TaskState {
	now := time.Now().Format(time.RFC3339)
	return TaskState{
		ID:        id,
		Phase:     PhasePlanning,
		Task:      task,
		Iteration: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Transition advances the FSM to the target phase.
// Transitioning back to planning (after validation rejection) increments Iteration.
func (ts *TaskState) Transition(to TaskPhase) error {
	for _, ok := range allowedTransitions[ts.Phase] {
		if ok == to {
			ts.Phase = to
			ts.UpdatedAt = time.Now().Format(time.RFC3339)
			if to == PhasePlanning {
				ts.Iteration++
			}
			return nil
		}
	}
	return fmt.Errorf("invalid FSM transition: %s → %s", ts.Phase, to)
}

// RetryPlanning keeps the phase at PhasePlanning but increments the iteration counter.
// Used when the user rejects a proposed plan before execution begins.
func (ts *TaskState) RetryPlanning() {
	ts.Iteration++
	ts.UpdatedAt = time.Now().Format(time.RFC3339)
}
