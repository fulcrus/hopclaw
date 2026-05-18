package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newOperatorStatusTestClient(t *testing.T, handler http.HandlerFunc, authToken string) *GatewayClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &GatewayClient{
		BaseURL:   srv.URL,
		AuthToken: authToken,
		HTTP:      srv.Client(),
	}
}

func TestRunStatusWithClientPrettyPrintsOperatorStatus(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != operatorStatusPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(operatorStatusResponse{
			OK:              true,
			Version:         "1.2.3",
			Uptime:          "3m0s",
			CapabilityCount: 4,
			ActiveRuns:      2,
			QueuedRuns:      1,
			Channels: []operatorStatusChannel{
				{Name: "discord", Status: "disconnected"},
				{Name: "slack", Status: "connected"},
			},
		})
	}, "")

	var out bytes.Buffer
	if err := runStatusWithClient(context.Background(), client, "127.0.0.1:16280", &out, false); err != nil {
		t.Fatalf("runStatusWithClient() error = %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"Gateway: 127.0.0.1:16280",
		"Status:  ready",
		"Version: 1.2.3",
		"Uptime:  3m0s",
		"Caps:    4",
		"Runs:    2 active, 1 queued",
		"Channels:",
		"discord          disconnected",
		"slack            connected",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunStatusWithClientPrettyPrintsUpdateAvailability(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != operatorStatusPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(operatorStatusResponse{
			OK: true,
			Update: map[string]any{
				"current_channel": "stable",
				"up_to_date":      false,
				"latest_version":  "1.2.4",
				"update_url":      "https://example.com/releases/1.2.4",
			},
		})
	}, "")

	var out bytes.Buffer
	if err := runStatusWithClient(context.Background(), client, "127.0.0.1:16280", &out, false); err != nil {
		t.Fatalf("runStatusWithClient() error = %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"Gateway: 127.0.0.1:16280",
		"Status:  ready",
		"Channel: stable",
		"Update:  1.2.4 available",
		"Release: https://example.com/releases/1.2.4",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunStatusWithClientReturnsHTTPError(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid auth credentials"})
	}, "")

	err := runStatusWithClient(context.Background(), client, "127.0.0.1:16280", &bytes.Buffer{}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "gateway at 127.0.0.1:16280: gateway error (HTTP 401): missing or invalid auth credentials"
	if err.Error() != expected {
		t.Fatalf("error = %q, want %q", err.Error(), expected)
	}
}

func TestRunHealthWithClientUsesPublicHealthEndpoint(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != publicHealthPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gatewayHealthResponse{
			OK:      true,
			State:   "ready",
			Summary: "ready",
		})
	}, "")

	var out bytes.Buffer
	healthy, err := runHealthWithClient(context.Background(), client, "127.0.0.1:16280", &out, false)
	if err != nil {
		t.Fatalf("runHealthWithClient() error = %v", err)
	}
	if !healthy {
		t.Fatalf("healthy = false, output=%q", out.String())
	}
	if !strings.Contains(out.String(), "HEALTHY: gateway at 127.0.0.1:16280 is ready (HTTP 200)") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestRunHealthWithClientReturnsDegradedState(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != publicHealthPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gatewayHealthResponse{
			OK:       false,
			State:    "degraded",
			Summary:  "Dynamic config store unavailable; using YAML-only mode",
			Warnings: []string{"Dynamic config store unavailable; using YAML-only mode"},
		})
	}, "")

	var out bytes.Buffer
	healthy, err := runHealthWithClient(context.Background(), client, "127.0.0.1:16280", &out, false)
	if err != nil {
		t.Fatalf("runHealthWithClient() error = %v", err)
	}
	if healthy {
		t.Fatalf("healthy = true, output=%q", out.String())
	}
	if !strings.Contains(out.String(), "DEGRADED: gateway at 127.0.0.1:16280 needs attention: Dynamic config store unavailable; using YAML-only mode") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestRunHealthReturnsExitErrorWhenUnhealthy(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "gateway warming up"})
	}, "")

	old := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) { return client, nil }
	t.Cleanup(func() { newGatewayClient = old })

	cmd := newHealthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runHealth(cmd, nil)
	var exitErr *cliExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("runHealth() error = %v, want cliExitError", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
	}
	if !strings.Contains(out.String(), "UNHEALTHY: gateway error (HTTP 503): gateway warming up") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestCheckGatewayWithClientUsesAuthenticatedStatus(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(authHeaderName); got != "secret-token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid auth credentials"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(operatorStatusResponse{OK: true})
	}, "secret-token")

	result := checkGatewayWithClient(context.Background(), client, "127.0.0.1:16280")
	if result.Status != "ok" {
		t.Fatalf("result.Status = %q, detail=%q", result.Status, result.Detail)
	}
	if !strings.Contains(result.Detail, "running at 127.0.0.1:16280 (HTTP 200)") {
		t.Fatalf("unexpected detail %q", result.Detail)
	}
}

func TestCheckGatewayHealthWithClientFailsWhenPublicHealthProbeReturnsHTTPError(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "gateway warming up"})
	}, "")

	result := checkGatewayHealthWithClient(context.Background(), client, "127.0.0.1:16280")
	if result.Status != "fail" {
		t.Fatalf("result.Status = %q, detail=%q", result.Status, result.Detail)
	}
	if !strings.Contains(result.Detail, "gateway error (HTTP 503): gateway warming up") {
		t.Fatalf("unexpected detail %q", result.Detail)
	}
}

func TestCheckGatewayHealthWithClientWarnsOnDegradedGatewayStatus(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gatewayHealthResponse{
			OK:       false,
			State:    "degraded",
			Summary:  "Dynamic config store unavailable; using YAML-only mode",
			Warnings: []string{"Dynamic config store unavailable; using YAML-only mode"},
		})
	}, "")

	result := checkGatewayHealthWithClient(context.Background(), client, "127.0.0.1:16280")
	if result.Status != "warn" {
		t.Fatalf("result.Status = %q, detail=%q", result.Status, result.Detail)
	}
	if result.Detail != "Dynamic config store unavailable; using YAML-only mode" {
		t.Fatalf("detail = %q", result.Detail)
	}
}

func TestCollectGatewayStatusWithClientPreservesHTTPErrorPayload(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid auth credentials"})
	}, "")

	status := collectGatewayStatusWithClient(context.Background(), client)
	if got, ok := status["status_code"].(int); !ok || got != http.StatusUnauthorized {
		t.Fatalf("status_code = %#v", status["status_code"])
	}
	if got, _ := status["error"].(string); got != "missing or invalid auth credentials" {
		t.Fatalf("error = %#v", status["error"])
	}
}

func TestVerifyGatewayConnectivityWithClientReportsHTTPError(t *testing.T) {
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid auth credentials"})
	}, "")

	var out bytes.Buffer
	verifyGatewayConnectivityWithClient(context.Background(), &out, client, "127.0.0.1:16280")

	output := out.String()
	for _, want := range []string{"127.0.0.1:16280", "HTTP 401", "missing or invalid auth credentials"} {
		if !strings.Contains(output, want) {
			t.Fatalf("unexpected output %q", output)
		}
	}
}
