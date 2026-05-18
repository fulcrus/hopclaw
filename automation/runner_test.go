package automation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// Mock runtime
// ---------------------------------------------------------------------------

type mockRuntime struct {
	submitFunc    func(ctx context.Context, req SubmitRequest) (*runtimesvc.RunResult, error)
	getResultFunc func(ctx context.Context, runID string) (*runtimesvc.RunResult, error)
}

func (m *mockRuntime) Submit(ctx context.Context, req SubmitRequest) (*runtimesvc.RunResult, error) {
	if m.submitFunc != nil {
		return m.submitFunc(ctx, req)
	}
	return nil, fmt.Errorf("submit not configured")
}

func (m *mockRuntime) GetRunResult(ctx context.Context, runID string) (*runtimesvc.RunResult, error) {
	if m.getResultFunc != nil {
		return m.getResultFunc(ctx, runID)
	}
	return nil, fmt.Errorf("getRunResult not configured")
}

// ---------------------------------------------------------------------------
// NewRunner
// ---------------------------------------------------------------------------

func TestNewRunnerNilRuntime(t *testing.T) {
	t.Parallel()

	r := NewRunner(nil, time.Minute, time.Second)
	if r != nil {
		t.Fatal("expected nil runner for nil runtime")
	}
}

func TestNewRunnerDefaults(t *testing.T) {
	t.Parallel()

	r := NewRunner(&mockRuntime{}, 0, 0)
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	if r.timeout != 10*time.Minute {
		t.Fatalf("timeout = %v, want 10m", r.timeout)
	}
	if r.pollInterval != 3*time.Second {
		t.Fatalf("pollInterval = %v, want 3s", r.pollInterval)
	}
}

func TestNewRunnerCustomValues(t *testing.T) {
	t.Parallel()

	r := NewRunner(&mockRuntime{}, 5*time.Minute, 10*time.Second)
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	if r.timeout != 5*time.Minute {
		t.Fatalf("timeout = %v, want 5m", r.timeout)
	}
	if r.pollInterval != 10*time.Second {
		t.Fatalf("pollInterval = %v, want 10s", r.pollInterval)
	}
}

// ---------------------------------------------------------------------------
// Runner.Run
// ---------------------------------------------------------------------------

func TestRunNilRunner(t *testing.T) {
	t.Parallel()

	var r *Runner
	_, err := r.Run(context.Background(), SubmitRequest{})
	if err == nil {
		t.Fatal("expected error for nil runner")
	}
}

func TestRunSubmitError(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return nil, fmt.Errorf("submit failed")
		},
	}
	r := NewRunner(rt, time.Minute, time.Second)
	_, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error from submit")
	}
}

func TestRunSubmitReturnsNil(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return nil, nil
		},
	}
	r := NewRunner(rt, time.Minute, time.Second)
	_, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestRunTerminalOnSubmit(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return &runtimesvc.RunResult{
				RunID:  "run-1",
				Status: agent.RunCompleted,
			}, nil
		},
	}
	r := NewRunner(rt, time.Minute, time.Second)
	result, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RunID != "run-1" {
		t.Fatalf("RunID = %q, want %q", result.RunID, "run-1")
	}
}

func TestRunPollsUntilTerminal(t *testing.T) {
	t.Parallel()

	pollCount := 0
	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return &runtimesvc.RunResult{
				RunID:  "run-poll",
				Status: agent.RunRunning,
			}, nil
		},
		getResultFunc: func(_ context.Context, runID string) (*runtimesvc.RunResult, error) {
			pollCount++
			if pollCount >= 2 {
				return &runtimesvc.RunResult{
					RunID:  runID,
					Status: agent.RunCompleted,
				}, nil
			}
			return &runtimesvc.RunResult{
				RunID:  runID,
				Status: agent.RunRunning,
			}, nil
		},
	}
	r := NewRunner(rt, 5*time.Minute, 10*time.Millisecond)
	result, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != agent.RunCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, agent.RunCompleted)
	}
	if pollCount < 2 {
		t.Fatalf("pollCount = %d, want >= 2", pollCount)
	}
}

func TestRunNonTerminalNoRunID(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return &runtimesvc.RunResult{
				RunID:  "",
				Status: agent.RunRunning,
			}, nil
		},
	}
	r := NewRunner(rt, time.Minute, time.Second)
	_, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error for non-terminal without run ID")
	}
}

func TestRunPollReturnsNil(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return &runtimesvc.RunResult{
				RunID:  "run-nil-poll",
				Status: agent.RunRunning,
			}, nil
		},
		getResultFunc: func(_ context.Context, _ string) (*runtimesvc.RunResult, error) {
			return nil, nil
		},
	}
	r := NewRunner(rt, time.Minute, 10*time.Millisecond)
	_, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error for nil poll result")
	}
}

func TestRunPollError(t *testing.T) {
	t.Parallel()

	rt := &mockRuntime{
		submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
			return &runtimesvc.RunResult{
				RunID:  "run-err",
				Status: agent.RunRunning,
			}, nil
		},
		getResultFunc: func(_ context.Context, _ string) (*runtimesvc.RunResult, error) {
			return nil, fmt.Errorf("poll error")
		},
	}
	r := NewRunner(rt, time.Minute, 10*time.Millisecond)
	_, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
	if err == nil {
		t.Fatal("expected error from poll")
	}
}

func TestRunTerminalStatuses(t *testing.T) {
	t.Parallel()

	terminals := []agent.RunStatus{
		agent.RunCompleted,
		agent.RunFailed,
		agent.RunCancelled,
	}
	for _, status := range terminals {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			rt := &mockRuntime{
				submitFunc: func(_ context.Context, _ SubmitRequest) (*runtimesvc.RunResult, error) {
					return &runtimesvc.RunResult{
						RunID:  "run-term",
						Status: status,
					}, nil
				},
			}
			r := NewRunner(rt, time.Minute, time.Second)
			result, err := r.Run(context.Background(), SubmitRequest{Content: "test"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != status {
				t.Fatalf("Status = %q, want %q", result.Status, status)
			}
		})
	}
}
