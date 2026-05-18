package diagnostics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

func TestSubmitBundle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(bundlePath, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var gotEnvelope Envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-upload-token" {
			t.Fatalf("Authorization = %q, want Bearer test-upload-token", auth)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		if err := json.Unmarshal([]byte(r.FormValue("envelope")), &gotEnvelope); err != nil {
			t.Fatalf("json.Unmarshal(envelope) error = %v", err)
		}
		file, _, err := r.FormFile("bundle")
		if err != nil {
			t.Fatalf("FormFile(bundle) error = %v", err)
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll(bundle) error = %v", err)
		}
		if string(body) != "zip-bytes" {
			t.Fatalf("bundle body = %q, want zip-bytes", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SubmitResult{
			OK:        true,
			ReportID:  "rpt-server-1",
			RequestID: "req-123",
		})
	}))
	defer server.Close()

	cfg := config.DiagnosticsConfig{
		UploadURL:     server.URL,
		UploadToken:   "test-upload-token",
		UploadTimeout: 2 * time.Second,
	}
	env := Envelope{
		ReportID:    "rpt-client-1",
		InstallID:   "inst-1",
		Source:      "bug-report",
		Command:     "hopclaw bug-report --submit",
		GeneratedAt: time.Now().UTC(),
		Metadata: map[string]any{
			"include_logs": true,
		},
	}

	result, err := SubmitBundle(context.Background(), cfg, bundlePath, env, "", "")
	if err != nil {
		t.Fatalf("SubmitBundle() error = %v", err)
	}
	if !result.OK {
		t.Fatal("SubmitBundle() returned ok=false")
	}
	if result.ReportID != "rpt-server-1" {
		t.Fatalf("result.ReportID = %q, want rpt-server-1", result.ReportID)
	}
	if result.RequestID != "req-123" {
		t.Fatalf("result.RequestID = %q, want req-123", result.RequestID)
	}
	if gotEnvelope.ReportID != "rpt-client-1" {
		t.Fatalf("gotEnvelope.ReportID = %q, want rpt-client-1", gotEnvelope.ReportID)
	}
	if gotEnvelope.InstallID != "inst-1" {
		t.Fatalf("gotEnvelope.InstallID = %q, want inst-1", gotEnvelope.InstallID)
	}
	if gotEnvelope.Source != "bug-report" {
		t.Fatalf("gotEnvelope.Source = %q, want bug-report", gotEnvelope.Source)
	}
}

func TestStoreBundle(t *testing.T) {
	t.Parallel()

	cfg := config.DiagnosticsConfig{
		CollectorDir: filepath.Join(t.TempDir(), "collector"),
	}
	stored, err := StoreBundle(cfg, Envelope{
		ReportID:  "rpt-store-1",
		InstallID: "inst-store-1",
		Source:    "panic",
		Command:   "hopclaw serve",
	}, "panic.zip", []byte("bundle-content"), "127.0.0.1:12345", "HopClaw/Test", "req-store")
	if err != nil {
		t.Fatalf("StoreBundle() error = %v", err)
	}

	if !strings.HasSuffix(stored.BundlePath, "rpt-store-1.zip") {
		t.Fatalf("stored.BundlePath = %q", stored.BundlePath)
	}
	if _, err := os.Stat(stored.BundlePath); err != nil {
		t.Fatalf("Stat(bundle) error = %v", err)
	}
	manifestBytes, err := os.ReadFile(stored.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	var manifest struct {
		ReportID  string   `json:"report_id"`
		RequestID string   `json:"request_id"`
		Envelope  Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("json.Unmarshal(manifest) error = %v", err)
	}
	if manifest.ReportID != "rpt-store-1" {
		t.Fatalf("manifest.ReportID = %q, want rpt-store-1", manifest.ReportID)
	}
	if manifest.RequestID != "req-store" {
		t.Fatalf("manifest.RequestID = %q, want req-store", manifest.RequestID)
	}
	if manifest.Envelope.InstallID != "inst-store-1" {
		t.Fatalf("manifest.Envelope.InstallID = %q, want inst-store-1", manifest.Envelope.InstallID)
	}
}
