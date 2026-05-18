package plugin

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessManagerSupervise(t *testing.T) {
	pm := NewProcessManager()

	var callCount atomic.Int32
	err := pm.Supervise(ProcessConfig{
		Name: "test-proc",
	}, func(ctx context.Context) error {
		callCount.Add(1)
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}

	// Let it start.
	time.Sleep(100 * time.Millisecond)

	handle, ok := pm.Handle("test-proc")
	if !ok {
		t.Fatal("handle not found")
	}
	if handle.Status != ProcessStatusRunning {
		t.Errorf("status = %q, want %q", handle.Status, ProcessStatusRunning)
	}

	// Stop.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pm.Stop(ctx)

	if callCount.Load() < 1 {
		t.Error("spawn function was never called")
	}
}

func TestProcessManagerRestart(t *testing.T) {
	pm := NewProcessManager()

	var callCount atomic.Int32
	ready := make(chan struct{})
	err := pm.Supervise(ProcessConfig{
		Name: "restart-proc",
	}, func(ctx context.Context) error {
		n := callCount.Add(1)
		if n <= 2 {
			return fmt.Errorf("simulated crash %d", n)
		}
		close(ready)
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}

	// Wait for process to stabilize (after 2 crashes + backoff: ~6s).
	select {
	case <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for restart")
	}

	if callCount.Load() < 3 {
		t.Errorf("call_count = %d, expected at least 3", callCount.Load())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pm.Stop(ctx)
}

func TestProcessManagerMaxRestarts(t *testing.T) {
	pm := NewProcessManager()

	var callCount atomic.Int32
	done := make(chan struct{})
	err := pm.Supervise(ProcessConfig{
		Name:        "limited-proc",
		MaxRestarts: 3,
		OnExit: func(err error) {
			if callCount.Load() >= 3 {
				select {
				case <-done:
				default:
					close(done)
				}
			}
		},
	}, func(ctx context.Context) error {
		callCount.Add(1)
		return fmt.Errorf("always fail")
	})
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}

	// Wait for it to exhaust restarts (backoff: 2+4+8 = ~14s).
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for max restarts")
	}

	// Small delay for status update.
	time.Sleep(100 * time.Millisecond)

	handle, ok := pm.Handle("limited-proc")
	if !ok {
		t.Fatal("handle not found")
	}
	if handle.Status != ProcessStatusFailed {
		t.Errorf("status = %q, want %q", handle.Status, ProcessStatusFailed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pm.Stop(ctx)
}

func TestProcessManagerDuplicate(t *testing.T) {
	pm := NewProcessManager()

	err := pm.Supervise(ProcessConfig{Name: "dup"}, func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("first supervise: %v", err)
	}

	err = pm.Supervise(ProcessConfig{Name: "dup"}, func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	if err == nil {
		t.Error("expected error for duplicate name")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pm.Stop(ctx)
}

func TestProcessManagerRemove(t *testing.T) {
	pm := NewProcessManager()

	err := pm.Supervise(ProcessConfig{Name: "removable"}, func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	pm.Remove("removable")

	_, ok := pm.Handle("removable")
	if ok {
		t.Error("expected handle to be removed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pm.Stop(ctx)
}

func TestProcessManagerHandles(t *testing.T) {
	pm := NewProcessManager()

	for _, name := range []string{"a", "b", "c"} {
		n := name
		pm.Supervise(ProcessConfig{Name: n}, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})
	}

	time.Sleep(100 * time.Millisecond)

	handles := pm.Handles()
	if len(handles) != 3 {
		t.Errorf("handles count = %d, want 3", len(handles))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pm.Stop(ctx)
}

func TestResolveCommand(t *testing.T) {
	tests := []struct {
		dir, cmd, want string
	}{
		{"/opt/plugins/foo", "./channel", "/opt/plugins/foo/channel"},
		{"/opt/plugins/foo", "channel", "/opt/plugins/foo/channel"},
		{"/opt/plugins/foo", "/usr/bin/channel", "/usr/bin/channel"},
	}
	for _, tt := range tests {
		got := ResolveCommand(tt.dir, tt.cmd)
		if got != tt.want {
			t.Errorf("ResolveCommand(%q, %q) = %q, want %q", tt.dir, tt.cmd, got, tt.want)
		}
	}
}
