package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/diagnostics"
)

func TestDiagnosticsReportUpload(t *testing.T) {
	t.Parallel()

	enabled := true
	collectorDir := filepath.Join(t.TempDir(), "collector")
	gw := newTestGatewayFull(t)
	gw.config.Diagnostics = config.DiagnosticsConfig{
		CollectorEnabled:   &enabled,
		CollectorDir:       collectorDir,
		CollectorAuthToken: "diag-token",
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	envelope := diagnostics.Envelope{
		ReportID:  "rpt-upload-1",
		InstallID: "inst-upload-1",
		Source:    "bug-report",
		Command:   "hopclaw bug-report --submit",
	}
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v", err)
	}
	if err := writer.WriteField("envelope", string(envelopeJSON)); err != nil {
		t.Fatalf("WriteField(envelope) error = %v", err)
	}
	part, err := writer.CreateFormFile("bundle", "report.zip")
	if err != nil {
		t.Fatalf("CreateFormFile(bundle) error = %v", err)
	}
	if _, err := part.Write([]byte("zip-content")); err != nil {
		t.Fatalf("Write(bundle) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	req := httptestNewMultipartRequest(t, http.MethodPost, "/diagnostics/reports", body, writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer diag-token")
	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp diagnostics.SubmitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	if !resp.OK {
		t.Fatal("response ok = false")
	}
	if resp.ReportID != "rpt-upload-1" {
		t.Fatalf("resp.ReportID = %q, want rpt-upload-1", resp.ReportID)
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		t.Fatal("resp.RequestID is empty")
	}

	dayDir := filepath.Join(collectorDir, currentUTCDateDir())
	bundlePath := filepath.Join(dayDir, "rpt-upload-1.zip")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("Stat(bundle) error = %v", err)
	}
	manifestPath := filepath.Join(dayDir, "rpt-upload-1.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if !bytes.Contains(manifestBytes, []byte(`"request_id"`)) {
		t.Fatalf("manifest missing request_id: %s", string(manifestBytes))
	}
}

func TestDiagnosticsReportUploadUnauthorized(t *testing.T) {
	t.Parallel()

	enabled := true
	gw := newTestGatewayFull(t)
	gw.config.Diagnostics = config.DiagnosticsConfig{
		CollectorEnabled:   &enabled,
		CollectorDir:       filepath.Join(t.TempDir(), "collector"),
		CollectorAuthToken: "diag-token",
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("envelope", `{"report_id":"rpt"}`); err != nil {
		t.Fatalf("WriteField(envelope) error = %v", err)
	}
	part, err := writer.CreateFormFile("bundle", "report.zip")
	if err != nil {
		t.Fatalf("CreateFormFile(bundle) error = %v", err)
	}
	if _, err := part.Write([]byte("zip-content")); err != nil {
		t.Fatalf("Write(bundle) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	req := httptestNewMultipartRequest(t, http.MethodPost, "/diagnostics/reports", body, writer.FormDataContentType())
	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func httptestNewMultipartRequest(t *testing.T, method, path string, body *bytes.Buffer, contentType string) *http.Request {
	t.Helper()

	req := makeUnauthRequest(t, method, path, "")
	req.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", contentType)
	return req
}

func currentUTCDateDir() string {
	return time.Now().UTC().Format("20060102")
}
