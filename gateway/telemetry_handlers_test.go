package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/telemetry"
)

func TestTelemetryEventUpload(t *testing.T) {
	t.Parallel()

	enabled := true
	collectorDir := filepath.Join(t.TempDir(), "collector")
	gw := newTestGatewayFull(t)
	gw.config.Diagnostics = config.DiagnosticsConfig{
		TelemetryCollectorEnabled:   &enabled,
		TelemetryCollectorDir:       collectorDir,
		TelemetryCollectorAuthToken: "telemetry-token",
	}

	body, err := json.Marshal(telemetry.Batch{
		Events: []telemetry.Event{{
			ID:        "evt-1",
			Event:     "install.completed",
			InstallID: "inst-1",
		}},
	})
	if err != nil {
		t.Fatalf("Marshal(batch) error = %v", err)
	}

	req := makeUnauthRequest(t, http.MethodPost, "/telemetry/events", "")
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Authorization", "Bearer telemetry-token")
	req.Header.Set("Content-Type", "application/json")

	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var resp telemetry.SubmitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(response) error = %v", err)
	}
	if !resp.OK {
		t.Fatal("resp.OK = false")
	}
	if resp.Accepted != 1 {
		t.Fatalf("resp.Accepted = %d, want 1", resp.Accepted)
	}
	if strings.TrimSpace(resp.BatchID) == "" {
		t.Fatal("resp.BatchID is empty")
	}

	dayDir := filepath.Join(collectorDir, time.Now().UTC().Format("20060102"))
	entries, err := os.ReadDir(dayDir)
	if err != nil {
		t.Fatalf("ReadDir(dayDir) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("collector files = %d, want 1", len(entries))
	}
	content, err := os.ReadFile(filepath.Join(dayDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(collector entry) error = %v", err)
	}
	if !bytes.Contains(content, []byte(`"install.completed"`)) {
		t.Fatalf("stored batch missing event: %s", string(content))
	}
}

func TestTelemetryEventUploadUnauthorized(t *testing.T) {
	t.Parallel()

	enabled := true
	gw := newTestGatewayFull(t)
	gw.config.Diagnostics = config.DiagnosticsConfig{
		TelemetryCollectorEnabled:   &enabled,
		TelemetryCollectorDir:       filepath.Join(t.TempDir(), "collector"),
		TelemetryCollectorAuthToken: "telemetry-token",
	}

	req := makeUnauthRequest(t, http.MethodPost, "/telemetry/events", `{"events":[{"event":"runtime.active"}]}`)
	req.Header.Set("Content-Type", "application/json")
	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
