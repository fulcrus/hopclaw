package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestGatewayClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/runtime/sessions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "count": 0})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	var resp struct {
		Items []any `json:"items"`
		Count int   `json:"count"`
	}
	if err := client.Get(context.Background(), "/runtime/sessions", &resp); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("expected count 0, got %d", resp.Count)
	}
}

func TestGatewayClient_Post(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["content"] != "hello" {
			t.Errorf("expected content 'hello', got %q", body["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "run-001", "status": "queued"})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	reqBody := map[string]string{"content": "hello"}
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := client.Post(context.Background(), "/runtime/runs", reqBody, &resp); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if resp.ID != "run-001" {
		t.Errorf("expected id 'run-001', got %q", resp.ID)
	}
	if resp.Status != "queued" {
		t.Errorf("expected status 'queued', got %q", resp.Status)
	}
}

func TestGatewayClient_Delete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	var resp struct {
		OK bool `json:"ok"`
	}
	if err := client.Delete(context.Background(), "/runtime/memory/test-key", &resp); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
}

func TestGatewayClient_Put(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	if err := client.Put(context.Background(), "/runtime/memory/k", map[string]string{"value": "v"}, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func TestGatewayClient_AuthHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(authHeaderName)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL:   srv.URL,
		AuthToken: "secret-token",
		HTTP:      srv.Client(),
	}

	if err := client.Get(context.Background(), "/test", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotHeader != "secret-token" {
		t.Errorf("expected auth header 'secret-token', got %q", gotHeader)
	}
}

func TestGatewayClient_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	err := client.Get(context.Background(), "/missing", nil)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	expected := "gateway error (HTTP 404): not found"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestGatewayClient_NoAuth(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(authHeaderName)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	if err := client.Get(context.Background(), "/test", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotHeader != "" {
		t.Errorf("expected no auth header, got %q", gotHeader)
	}
}

func TestGatewayClient_GetRawWithStatusIncludesAuthAndPreservesHTTPStatus(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(authHeaderName)
		if r.URL.Path != operatorStatusPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid auth credentials"})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL:   srv.URL,
		AuthToken: "secret-token",
		HTTP:      srv.Client(),
	}

	body, statusCode, err := client.GetRawWithStatus(context.Background(), operatorStatusPath)
	if err != nil {
		t.Fatalf("GetRawWithStatus: %v", err)
	}
	if gotHeader != "secret-token" {
		t.Fatalf("auth header = %q, want secret-token", gotHeader)
	}
	if statusCode != http.StatusUnauthorized {
		t.Fatalf("statusCode = %d, want 401", statusCode)
	}
	if !strings.Contains(string(body), "missing or invalid auth credentials") {
		t.Fatalf("body = %q", string(body))
	}
}

func TestGatewayClient_DecodeHTMLPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>Docker Error</title></head><body>daemon unavailable</body></html>"))
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	var resp struct {
		OK bool `json:"ok"`
	}
	err := client.Get(context.Background(), "/operator/sandbox/images", &resp)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "gateway returned HTML instead of JSON (Docker Error)") {
		t.Fatalf("error = %q", err)
	}
}

func TestResolveConfigOperatorToken(t *testing.T) {
	t.Run("bearer", func(t *testing.T) {
		cfg := config.Config{
			Auth: config.AuthConfig{BearerToken: "bearer-secret"},
		}
		if got := resolveConfigOperatorToken(cfg); got != "bearer-secret" {
			t.Fatalf("resolveConfigOperatorToken() = %q", got)
		}
	})

	t.Run("api key", func(t *testing.T) {
		cfg := config.Config{
			Auth: config.AuthConfig{
				APIKeys: []config.AuthKeyEntry{
					{Name: "disabled", Key: "disabled", Enabled: false},
					{Name: "operator", Key: "api-secret", Enabled: true},
				},
			},
		}
		if got := resolveConfigOperatorToken(cfg); got != "api-secret" {
			t.Fatalf("resolveConfigOperatorToken() = %q", got)
		}
	})
}

func TestNewGatewayClientUsesSavedRemoteTargetWhenFlagRemoteSet(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("HOPCLAW_REMOTE_TOKEN", "remote-secret")

	var localStatusRequests int
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == operatorStatusPath {
			localStatusRequests++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()

	var remoteAuthHeader string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case operatorStatusPath:
			remoteAuthHeader = r.Header.Get(authHeaderName)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	flagConfig = writeTestCLIConfig(t, local.URL)
	flagRemote = "prod"
	flagLocal = false

	if err := addSavedTargetProfile(savedTargetProfile{
		Name:     "prod",
		Kind:     targetKindRemote,
		BaseURL:  remote.URL,
		AuthType: targetAuthTypeBearer,
		AuthRef:  "env:HOPCLAW_REMOTE_TOKEN",
	}); err != nil {
		t.Fatalf("addSavedTargetProfile() error = %v", err)
	}

	client, err := NewGatewayClient()
	if err != nil {
		t.Fatalf("NewGatewayClient() error = %v", err)
	}
	if client.BaseURL != remote.URL {
		t.Fatalf("client.BaseURL = %q, want %q", client.BaseURL, remote.URL)
	}
	if client.AuthToken != "remote-secret" {
		t.Fatalf("client.AuthToken = %q, want remote-secret", client.AuthToken)
	}

	var resp map[string]any
	if err := client.Get(context.Background(), operatorStatusPath, &resp); err != nil {
		t.Fatalf("client.Get(operator status) error = %v", err)
	}
	if remoteAuthHeader != "remote-secret" {
		t.Fatalf("remote auth header = %q, want remote-secret", remoteAuthHeader)
	}
	if localStatusRequests != 0 {
		t.Fatalf("expected operator request to bypass configured local gateway, got %d local status request(s)", localStatusRequests)
	}
}

func TestNewGatewayClientRejectsLocalAndRemoteFlagsTogether(t *testing.T) {
	restore := snapshotInteractiveFlags()
	defer restore()

	flagRemote = "prod"
	flagLocal = true

	_, err := NewGatewayClient()
	if err == nil {
		t.Fatal("expected error when --local and --remote are combined")
	}
	if !strings.Contains(err.Error(), "--local and --remote cannot be used together") {
		t.Fatalf("error = %q", err)
	}
}
