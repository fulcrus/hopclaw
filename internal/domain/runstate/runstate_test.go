package runstate

import "testing"

func TestValidTransitionAllowedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to Status
	}{
		{RunQueued, RunRunning},
		{RunQueued, RunWaitingInput},
		{RunQueued, RunCancelled},
		{RunWaitingInput, RunRunning},
		{RunWaitingInput, RunCancelled},
		{RunRunning, RunWaitingApproval},
		{RunRunning, RunWaitingInput},
		{RunRunning, RunStreaming},
		{RunRunning, RunCompleted},
		{RunRunning, RunFailed},
		{RunRunning, RunCancelled},
		{RunWaitingApproval, RunQueued},
		{RunWaitingApproval, RunRunning},
		{RunWaitingApproval, RunCompleted},
		{RunWaitingApproval, RunFailed},
		{RunWaitingApproval, RunCancelled},
		{RunStreaming, RunRunning},
		{RunStreaming, RunCompleted},
		{RunStreaming, RunFailed},
		{RunStreaming, RunCancelled},
	}
	for _, tc := range cases {
		if !ValidTransition(tc.from, tc.to) {
			t.Errorf("ValidTransition(%q, %q) = false, want true", tc.from, tc.to)
		}
	}
}

func TestValidTransitionBlockedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to Status
	}{
		// Terminal states cannot transition.
		{RunCompleted, RunRunning},
		{RunCompleted, RunFailed},
		{RunCompleted, RunCancelled},
		{RunFailed, RunRunning},
		{RunFailed, RunCompleted},
		{RunCancelled, RunRunning},
		// Skip nonsensical transitions.
		{RunQueued, RunCompleted},
		{RunQueued, RunFailed},
		{RunQueued, RunStreaming},
		{RunWaitingInput, RunCompleted},
		{RunWaitingInput, RunFailed},
	}
	for _, tc := range cases {
		if ValidTransition(tc.from, tc.to) {
			t.Errorf("ValidTransition(%q, %q) = true, want false", tc.from, tc.to)
		}
	}
}

func TestValidTransitionSameStatus(t *testing.T) {
	t.Parallel()
	// Same-status "transitions" are no-ops in transitionRun (skipped when status == run.Status).
	// But ValidTransition should return false since they're not in the map.
	for _, s := range []Status{RunQueued, RunRunning, RunCompleted, RunFailed, RunCancelled} {
		if ValidTransition(s, s) {
			t.Errorf("ValidTransition(%q, %q) = true, want false (self-transition)", s, s)
		}
	}
}

func TestAllowedTransitionsCoversAllStatuses(t *testing.T) {
	t.Parallel()
	allStatuses := []Status{
		RunQueued, RunWaitingInput, RunRunning, RunWaitingApproval,
		RunStreaming, RunCompleted, RunFailed, RunCancelled,
	}
	for _, s := range allStatuses {
		if _, ok := AllowedTransitions[s]; !ok {
			t.Errorf("AllowedTransitions missing entry for %q", s)
		}
	}
}
