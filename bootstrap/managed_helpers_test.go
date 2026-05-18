package bootstrap

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedHelperSupervisorStartsOnDemand(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32
	var stops atomic.Int32
	var sessions atomic.Int32

	helper := newManagedHelperSupervisor("test", time.Hour, func(context.Context) (*managedHelperInstance, error) {
		starts.Add(1)
		return newStubManagedInstance(t, &sessions, &stops)
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = helper.Stop(ctx)
	}()

	endpoint1, err := helper.Endpoint(context.Background())
	if err != nil {
		t.Fatalf("Endpoint() error = %v", err)
	}
	if endpoint1.BaseURL == "" {
		t.Fatal("Endpoint() returned empty base URL")
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("starts = %d, want 1", got)
	}

	endpoint2, err := helper.Endpoint(context.Background())
	if err != nil {
		t.Fatalf("Endpoint() second call error = %v", err)
	}
	if endpoint2.BaseURL != endpoint1.BaseURL {
		t.Fatalf("base URL changed unexpectedly: %q != %q", endpoint2.BaseURL, endpoint1.BaseURL)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("starts after reuse = %d, want 1", got)
	}
}

func TestManagedHelperSupervisorStopsWhenIdle(t *testing.T) {
	t.Parallel()

	var stops atomic.Int32
	var sessions atomic.Int32

	helper := newManagedHelperSupervisor("test", 120*time.Millisecond, func(context.Context) (*managedHelperInstance, error) {
		return newStubManagedInstance(t, &sessions, &stops)
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = helper.Stop(ctx)
	}()

	if _, err := helper.Endpoint(context.Background()); err != nil {
		t.Fatalf("Endpoint() error = %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if stops.Load() > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("managed helper did not stop after becoming idle")
}

func TestManagedHelperSupervisorKeepsLiveSessions(t *testing.T) {
	t.Parallel()

	var stops atomic.Int32
	var sessions atomic.Int32
	sessions.Store(1)

	helper := newManagedHelperSupervisor("test", 120*time.Millisecond, func(context.Context) (*managedHelperInstance, error) {
		return newStubManagedInstance(t, &sessions, &stops)
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = helper.Stop(ctx)
	}()

	if _, err := helper.Endpoint(context.Background()); err != nil {
		t.Fatalf("Endpoint() error = %v", err)
	}

	time.Sleep(350 * time.Millisecond)
	if got := stops.Load(); got != 0 {
		t.Fatalf("stops with active session = %d, want 0", got)
	}

	sessions.Store(0)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if stops.Load() > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("managed helper did not stop after sessions drained")
}

func TestManagedHelperSupervisorStatusOmitsZeroLastUseAt(t *testing.T) {
	t.Parallel()

	helper := newManagedHelperSupervisor("test", time.Hour, func(context.Context) (*managedHelperInstance, error) {
		return newStubManagedInstance(t, new(atomic.Int32), new(atomic.Int32))
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = helper.Stop(ctx)
	}()

	state := helper.status(context.Background())
	if state.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", state.Status)
	}
	if state.LastUseAt != "" {
		t.Fatalf("last_use_at = %q, want empty", state.LastUseAt)
	}
}

func TestAppCloseStopsManagedHelpers(t *testing.T) {
	t.Parallel()

	var browserStops atomic.Int32
	var desktopStops atomic.Int32
	var sessions atomic.Int32

	browserHelper := newManagedHelperSupervisor("browser", time.Hour, func(context.Context) (*managedHelperInstance, error) {
		return newStubManagedInstance(t, &sessions, &browserStops)
	})
	desktopHelper := newManagedHelperSupervisor("desktop", time.Hour, func(context.Context) (*managedHelperInstance, error) {
		return newStubManagedInstance(t, &sessions, &desktopStops)
	})

	if _, err := browserHelper.Endpoint(context.Background()); err != nil {
		t.Fatalf("browser Endpoint() error = %v", err)
	}
	if _, err := desktopHelper.Endpoint(context.Background()); err != nil {
		t.Fatalf("desktop Endpoint() error = %v", err)
	}

	app := &App{
		AppRuntimeState: AppRuntimeState{
			ManagedHelpers: &managedHelpers{Browser: browserHelper, Desktop: desktopHelper},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := browserStops.Load(); got == 0 {
		t.Fatal("browser helper was not stopped by App.Close")
	}
	if got := desktopStops.Load(); got == 0 {
		t.Fatal("desktop helper was not stopped by App.Close")
	}
}

func newStubManagedInstance(t *testing.T, sessions, stops *atomic.Int32) (*managedHelperInstance, error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"session_count": sessions.Load(),
		})
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	return &managedHelperInstance{
		managedEndpoint: managedEndpoint{BaseURL: "http://" + listener.Addr().String()},
		close: func(ctx context.Context) error {
			stops.Add(1)
			return server.Shutdown(ctx)
		},
	}, nil
}
