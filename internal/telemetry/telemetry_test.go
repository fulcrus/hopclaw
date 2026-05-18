package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

func TestClientTrackOnceAndDaily(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	var received []Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-telemetry-token" {
			t.Fatalf("Authorization = %q, want Bearer test-telemetry-token", auth)
		}
		var batch Batch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Fatalf("Decode(batch) error = %v", err)
		}
		received = append(received, batch.Events...)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SubmitResult{
			OK:       true,
			Accepted: len(batch.Events),
		})
	}))
	defer server.Close()

	enabled := true
	now := time.Date(2026, time.March, 27, 9, 0, 0, 0, time.UTC)
	client := NewClient(config.DiagnosticsConfig{
		TelemetryEnabled:  &enabled,
		TelemetryEndpoint: server.URL,
		TelemetryToken:    "test-telemetry-token",
	}, WithStateDir(filepath.Join(root, ".hopclaw", "data", "telemetry-test")), WithNow(func() time.Time {
		return now
	}))

	if err := client.TrackOnce(context.Background(), "install.completed", "install.completed", map[string]any{"activation_source": "serve"}); err != nil {
		t.Fatalf("TrackOnce(install) error = %v", err)
	}
	if err := client.TrackOnce(context.Background(), "install.completed", "install.completed", map[string]any{"activation_source": "serve"}); err != nil {
		t.Fatalf("TrackOnce(install repeat) error = %v", err)
	}
	if err := client.TrackDaily(context.Background(), "runtime.active", "runtime.active", map[string]any{"surface": "serve"}); err != nil {
		t.Fatalf("TrackDaily(day1) error = %v", err)
	}
	if err := client.TrackDaily(context.Background(), "runtime.active", "runtime.active", map[string]any{"surface": "serve"}); err != nil {
		t.Fatalf("TrackDaily(day1 repeat) error = %v", err)
	}

	now = now.Add(24 * time.Hour)
	if err := client.TrackDaily(context.Background(), "runtime.active", "runtime.active", map[string]any{"surface": "serve"}); err != nil {
		t.Fatalf("TrackDaily(day2) error = %v", err)
	}

	if len(received) != 3 {
		t.Fatalf("received len = %d, want 3", len(received))
	}
	if received[0].Event != "install.completed" {
		t.Fatalf("event[0] = %q, want install.completed", received[0].Event)
	}
	if received[1].Event != "runtime.active" || received[2].Event != "runtime.active" {
		t.Fatalf("runtime events = %#v", received)
	}
	if got := received[1].Properties["active_day"]; got != "2026-03-27" {
		t.Fatalf("day1 active_day = %#v, want 2026-03-27", got)
	}
	if got := received[2].Properties["active_day"]; got != "2026-03-28" {
		t.Fatalf("day2 active_day = %#v, want 2026-03-28", got)
	}
	if received[0].InstallID == "" {
		t.Fatal("install id should be populated")
	}
}

func TestStoreBatch(t *testing.T) {
	t.Parallel()

	collectorDir := filepath.Join(t.TempDir(), "collector")
	stored, err := StoreBatch(config.DiagnosticsConfig{
		TelemetryCollectorDir: collectorDir,
	}, Batch{
		Events: []Event{{
			Event:     "plugin.installed",
			InstallID: "inst-test",
		}},
	}, "127.0.0.1:1234", "HopClaw/Test", "req-1")
	if err != nil {
		t.Fatalf("StoreBatch() error = %v", err)
	}
	if stored.BatchID == "" {
		t.Fatal("stored.BatchID is empty")
	}
	if stored.Accepted != 1 {
		t.Fatalf("stored.Accepted = %d, want 1", stored.Accepted)
	}
	if _, err := os.Stat(stored.Path); err != nil {
		t.Fatalf("Stat(stored.Path) error = %v", err)
	}
}
