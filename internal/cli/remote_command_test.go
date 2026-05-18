package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWebhooksListUsesSavedRemoteTarget(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	t.Setenv("HOME", t.TempDir())

	var localWebhookRequests int
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == webhooksBasePath {
			localWebhookRequests++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()

	var remoteWebhookRequests int
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case webhooksBasePath:
			remoteWebhookRequests++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(webhookListResponse{
				Items: []webhookEntry{{ID: "hook-1", URL: "https://example.com/hook", Enabled: true, CreatedAt: "2026-04-04T00:00:00Z"}},
				Count: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	flagConfig = writeTestCLIConfig(t, local.URL)
	flagRemote = "prod"
	flagLocal = false

	if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: remote.URL}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	restoreStdout := captureStdout(t)
	if err := runWebhooksList(context.Background()); err != nil {
		t.Fatalf("runWebhooksList() error = %v", err)
	}
	output := restoreStdout()
	if !strings.Contains(output, "hook-1") {
		t.Fatalf("stdout = %q, want webhook id", output)
	}
	if remoteWebhookRequests != 1 {
		t.Fatalf("remote webhook requests = %d, want 1", remoteWebhookRequests)
	}
	if localWebhookRequests != 0 {
		t.Fatalf("local webhook requests = %d, want 0", localWebhookRequests)
	}
}

func TestRunSandboxStatusUsesSavedRemoteTarget(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	t.Setenv("HOME", t.TempDir())

	var localStatusRequests int
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sandboxStatusPath {
			localStatusRequests++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()

	var remoteStatusRequests int
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case sandboxStatusPath:
			remoteStatusRequests++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sandboxStatusResponse{
				Available:     true,
				Runtime:       "docker",
				AllowedImages: []string{"python:3.12-slim"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	flagConfig = writeTestCLIConfig(t, local.URL)
	flagRemote = "prod"
	flagLocal = false

	if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: remote.URL}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	restoreStdout := captureStdout(t)
	if err := runSandboxStatus(context.Background()); err != nil {
		t.Fatalf("runSandboxStatus() error = %v", err)
	}
	output := restoreStdout()
	if !strings.Contains(output, "Sandbox runtime: available") {
		t.Fatalf("stdout = %q, want remote sandbox availability", output)
	}
	if remoteStatusRequests != 1 {
		t.Fatalf("remote sandbox status requests = %d, want 1", remoteStatusRequests)
	}
	if localStatusRequests != 0 {
		t.Fatalf("local sandbox status requests = %d, want 0", localStatusRequests)
	}
}

func TestRunMessageSendUsesSavedRemoteTarget(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	t.Setenv("HOME", t.TempDir())

	var localRunRequests int
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/runs", "/runtime/runs/run-local/completion":
			localRunRequests++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()

	var remoteRunRequests int
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/runtime/runs":
			remoteRunRequests++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(messageRunResponse{
				ID:        "run-remote",
				SessionID: "sess-remote",
				Status:    "completed",
			})
		case "/runtime/runs/run-remote/completion":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(messageCompletionResponse{
				Bundle: &messageResultBundle{FinalText: "remote ok"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	flagConfig = writeTestCLIConfig(t, local.URL)
	flagRemote = "prod"
	flagLocal = false

	if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: remote.URL}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	restoreStdout := captureStdout(t)
	if err := runMessageSend(context.Background(), "demo", "cli", "hello", nil); err != nil {
		t.Fatalf("runMessageSend() error = %v", err)
	}
	output := strings.TrimSpace(restoreStdout())
	if output != "remote ok" {
		t.Fatalf("stdout = %q, want %q", output, "remote ok")
	}
	if remoteRunRequests != 1 {
		t.Fatalf("remote run requests = %d, want 1", remoteRunRequests)
	}
	if localRunRequests != 0 {
		t.Fatalf("local run requests = %d, want 0", localRunRequests)
	}
}
