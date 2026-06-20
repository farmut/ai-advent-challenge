package domain

import (
	"testing"
)

func TestNewTaskState_InitialPhase(t *testing.T) {
	ts := NewTaskState("id-1", "build a REST API")
	if ts.Phase != PhasePlanning {
		t.Errorf("expected initial phase %q, got %q", PhasePlanning, ts.Phase)
	}
	if ts.Iteration != 1 {
		t.Errorf("expected initial iteration 1, got %d", ts.Iteration)
	}
	if ts.Task != "build a REST API" {
		t.Errorf("unexpected task: %q", ts.Task)
	}
}

func TestTransition_PlanningToExecution(t *testing.T) {
	ts := NewTaskState("id", "task")
	if err := ts.Transition(PhaseExecution); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Phase != PhaseExecution {
		t.Errorf("expected %q, got %q", PhaseExecution, ts.Phase)
	}
}

func TestTransition_ExecutionToValidation(t *testing.T) {
	ts := NewTaskState("id", "task")
	_ = ts.Transition(PhaseExecution)
	if err := ts.Transition(PhaseValidation); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Phase != PhaseValidation {
		t.Errorf("expected %q, got %q", PhaseValidation, ts.Phase)
	}
}

func TestTransition_ValidationToDone(t *testing.T) {
	ts := NewTaskState("id", "task")
	_ = ts.Transition(PhaseExecution)
	_ = ts.Transition(PhaseValidation)
	if err := ts.Transition(PhaseDone); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Phase != PhaseDone {
		t.Errorf("expected %q, got %q", PhaseDone, ts.Phase)
	}
}

func TestTransition_ValidationToPlanning_IncrementsIteration(t *testing.T) {
	ts := NewTaskState("id", "task")
	_ = ts.Transition(PhaseExecution)
	_ = ts.Transition(PhaseValidation)
	if err := ts.Transition(PhasePlanning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Phase != PhasePlanning {
		t.Errorf("expected %q, got %q", PhasePlanning, ts.Phase)
	}
	if ts.Iteration != 2 {
		t.Errorf("expected iteration 2 after backward transition, got %d", ts.Iteration)
	}
}

func TestTransition_Invalid_PlanningToDone(t *testing.T) {
	ts := NewTaskState("id", "task")
	if err := ts.Transition(PhaseDone); err == nil {
		t.Error("expected error for invalid transition planning→done, got nil")
	}
}

func TestTransition_Invalid_PlanningToValidation(t *testing.T) {
	ts := NewTaskState("id", "task")
	if err := ts.Transition(PhaseValidation); err == nil {
		t.Error("expected error for invalid transition planning→validation, got nil")
	}
}

func TestTransition_Invalid_ExecutionToDone(t *testing.T) {
	ts := NewTaskState("id", "task")
	_ = ts.Transition(PhaseExecution)
	if err := ts.Transition(PhaseDone); err == nil {
		t.Error("expected error for invalid transition execution→done, got nil")
	}
}

func TestRetryPlanning_IncrementsIteration(t *testing.T) {
	ts := NewTaskState("id", "task")
	if ts.Iteration != 1 {
		t.Fatalf("expected iteration 1, got %d", ts.Iteration)
	}
	ts.RetryPlanning()
	if ts.Iteration != 2 {
		t.Errorf("expected iteration 2 after RetryPlanning, got %d", ts.Iteration)
	}
	if ts.Phase != PhasePlanning {
		t.Errorf("phase should remain planning, got %q", ts.Phase)
	}
}

func TestFullCycle_WithReplanning(t *testing.T) {
	ts := NewTaskState("id", "task")

	// Round 1: plan rejected
	if err := ts.Transition(PhaseExecution); err != nil {
		t.Fatal(err)
	}
	if err := ts.Transition(PhaseValidation); err != nil {
		t.Fatal(err)
	}
	if err := ts.Transition(PhasePlanning); err != nil { // rejected
		t.Fatal(err)
	}
	if ts.Iteration != 2 {
		t.Errorf("expected iteration 2, got %d", ts.Iteration)
	}

	// Round 2: plan accepted
	if err := ts.Transition(PhaseExecution); err != nil {
		t.Fatal(err)
	}
	if err := ts.Transition(PhaseValidation); err != nil {
		t.Fatal(err)
	}
	if err := ts.Transition(PhaseDone); err != nil {
		t.Fatal(err)
	}
	if ts.Phase != PhaseDone {
		t.Errorf("expected done, got %q", ts.Phase)
	}
	if ts.Iteration != 2 {
		t.Errorf("expected iteration 2 at completion, got %d", ts.Iteration)
	}
}
