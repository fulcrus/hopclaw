package agent

import (
	"testing"
	"time"
)

func TestTransitionRunValidTransition(t *testing.T) {
	t.Parallel()
	run := &Run{ID: "test-run", Status: RunQueued}
	transitionRun(run, RunRunning, PhasePreparing)
	if run.Status != RunRunning {
		t.Fatalf("Status = %q, want running", run.Status)
	}
	if run.Phase != PhasePreparing {
		t.Fatalf("Phase = %q, want preparing", run.Phase)
	}
}

func TestTransitionRunInvalidTransitionBlocked(t *testing.T) {
	t.Parallel()
	run := &Run{ID: "test-run", Status: RunCompleted}
	transitionRun(run, RunRunning, PhasePreparing)
	if run.Status != RunCompleted {
		t.Fatalf("Status = %q, want completed (transition should be blocked)", run.Status)
	}
}

func TestTransitionRunTerminalCannotTransition(t *testing.T) {
	t.Parallel()
	for _, terminal := range []RunStatus{RunCompleted, RunFailed, RunCancelled} {
		run := &Run{ID: "test-" + string(terminal), Status: terminal}
		transitionRun(run, RunRunning, PhasePreparing)
		if run.Status != terminal {
			t.Errorf("Status = %q after transition from %q, want %q (terminal)", run.Status, terminal, terminal)
		}
	}
}

func TestTransitionRunEmptyStatusSkipsValidation(t *testing.T) {
	t.Parallel()
	run := &Run{ID: "test-run", Status: RunRunning, Phase: PhasePreparing}
	transitionRun(run, "", PhaseWaitingModel)
	if run.Phase != PhaseWaitingModel {
		t.Fatalf("Phase = %q, want waiting_model", run.Phase)
	}
	if run.Status != RunRunning {
		t.Fatalf("Status = %q, want running (unchanged)", run.Status)
	}
}

func TestTransitionRunSameStatusSkipsValidation(t *testing.T) {
	t.Parallel()
	run := &Run{ID: "test-run", Status: RunRunning, Phase: PhasePreparing}
	transitionRun(run, RunRunning, PhaseExecutingTools)
	if run.Phase != PhaseExecutingTools {
		t.Fatalf("Phase = %q, want executing_tools", run.Phase)
	}
}

func TestTransitionRunNilSafe(t *testing.T) {
	t.Parallel()
	transitionRun(nil, RunRunning, PhasePreparing)
}

func TestTransitionRunOptions(t *testing.T) {
	t.Parallel()
	run := &Run{ID: "test-run", Status: RunRunning}
	now := time.Now().UTC()
	transitionRun(run, RunCompleted, PhaseFinalize,
		withRunError("something failed"),
		withRunApproval(""),
		withRunPendingTools(nil),
		withRunFinishedAt(now),
	)
	if run.Status != RunCompleted {
		t.Fatalf("Status = %q, want completed", run.Status)
	}
	if run.Error != "something failed" {
		t.Fatalf("Error = %q", run.Error)
	}
	if run.ApprovalID != "" {
		t.Fatalf("ApprovalID = %q, want empty", run.ApprovalID)
	}
	if run.FinishedAt.IsZero() {
		t.Fatal("FinishedAt should be set")
	}
}
