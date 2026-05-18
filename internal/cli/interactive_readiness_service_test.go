package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
)

func TestInteractiveMergeReadinessStatus(t *testing.T) {
	if got := interactiveMergeReadinessStatus("ready", "degraded"); got != "degraded" {
		t.Fatalf("interactiveMergeReadinessStatus() = %q", got)
	}
	if got := interactiveMergeReadinessStatus("blocked", "ready"); got != "blocked" {
		t.Fatalf("interactiveMergeReadinessStatus(blocked, ready) = %q", got)
	}
}

func TestInteractiveHealthStatus(t *testing.T) {
	if got := interactiveHealthStatus("warning"); got != "degraded" {
		t.Fatalf("interactiveHealthStatus(warning) = %q", got)
	}
	if got := interactiveHealthStatus("OK"); got != "ready" {
		t.Fatalf("interactiveHealthStatus(OK) = %q", got)
	}
	if got := interactiveHealthStatus("error"); got != "blocked" {
		t.Fatalf("interactiveHealthStatus(error) = %q", got)
	}
}

func TestReadinessCategoryLabel(t *testing.T) {
	if got := readinessCategoryLabel(interactiveTarget{Kind: interactiveTargetLocal, Name: localTargetName}); got != "Local runtime" {
		t.Fatalf("readinessCategoryLabel(local) = %q", got)
	}
	if got := readinessCategoryLabel(interactiveTarget{Kind: interactiveTargetRemote, Name: "prod"}); got != "Remote prod" {
		t.Fatalf("readinessCategoryLabel(remote) = %q", got)
	}
}

func TestFinalizeReadinessSnapshot(t *testing.T) {
	snapshot := finalizeReadinessSnapshot([]replpkg.ReadinessCategory{
		{ID: "gateway", Status: "ready"},
		{ID: "quality_release", Status: "blocked"},
	}, []replpkg.RecoveryCandidate{{ID: "run-1", Type: "run"}}, "quiet_when_healthy")
	if snapshot == nil {
		t.Fatal("finalizeReadinessSnapshot() = nil")
	}
	if snapshot.OverallStatus != "blocked" {
		t.Fatalf("overall status = %q", snapshot.OverallStatus)
	}
	if snapshot.StartupDiagnostics != "quiet_when_healthy" {
		t.Fatalf("startup diagnostics = %q", snapshot.StartupDiagnostics)
	}
	if len(snapshot.RecoveryCandidates) != 1 {
		t.Fatalf("recovery candidates = %d", len(snapshot.RecoveryCandidates))
	}
}

func TestBuildGatewayReadinessSnapshotUsesOperationalWarningSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":      false,
				"state":   "degraded",
				"summary": "Dynamic config store unavailable; using YAML-only mode",
			})
		case "/operator/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":       false,
				"state":    "degraded",
				"summary":  "Dynamic config store unavailable; using YAML-only mode",
				"warnings": []string{"Dynamic config store unavailable; using YAML-only mode"},
				"user_surface": map[string]any{
					"startup_diagnostics": "quiet_when_healthy",
				},
			})
		case "/runtime/runs":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := (&externalInteractiveGateway{
		client: &GatewayClient{BaseURL: server.URL, HTTP: server.Client()},
		target: interactiveTarget{Kind: interactiveTargetRemote, Name: "prod", BaseURL: server.URL},
	}).ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot() error = %v", err)
	}
	if snapshot == nil {
		t.Fatal("ReadinessSnapshot() = nil")
	}
	if snapshot.StartupDiagnostics != "quiet_when_healthy" {
		t.Fatalf("startup diagnostics = %q", snapshot.StartupDiagnostics)
	}
	var gatewayCategory *replpkg.ReadinessCategory
	for i := range snapshot.Categories {
		if snapshot.Categories[i].ID == "gateway" {
			gatewayCategory = &snapshot.Categories[i]
			break
		}
	}
	if gatewayCategory == nil {
		t.Fatal("gateway category not found")
	}
	if gatewayCategory.Status != "degraded" {
		t.Fatalf("gateway status = %q", gatewayCategory.Status)
	}
	if gatewayCategory.Summary != "Dynamic config store unavailable; using YAML-only mode" {
		t.Fatalf("gateway summary = %q", gatewayCategory.Summary)
	}
}

func TestBuildGatewayReadinessSnapshotPreservesMoreSevereReplySummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":      true,
				"state":   "ready",
				"summary": "ready",
			})
		case "/operator/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":    true,
				"state": "ready",
				"connected_channels": []map[string]any{{
					"name":   "slack",
					"status": "disconnected",
				}},
			})
		case "/operator/governance/health":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"status":  "ok",
				"summary": "delivery healthy",
			})
		case "/runtime/runs":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := (&externalInteractiveGateway{
		client: &GatewayClient{BaseURL: server.URL, HTTP: server.Client()},
		target: interactiveTarget{Kind: interactiveTargetRemote, Name: "prod", BaseURL: server.URL},
	}).ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot() error = %v", err)
	}
	var replies *replpkg.ReadinessCategory
	for i := range snapshot.Categories {
		if snapshot.Categories[i].ID == "channel_delivery" {
			replies = &snapshot.Categories[i]
			break
		}
	}
	if replies == nil {
		t.Fatal("channel_delivery category not found")
	}
	if replies.Status != "degraded" {
		t.Fatalf("reply status = %q", replies.Status)
	}
	if replies.Summary != "slack disconnected" {
		t.Fatalf("reply summary = %q", replies.Summary)
	}
}

func TestBuildGatewayReadinessSnapshotTreatsOperatorUnauthorizedAsBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":      true,
				"state":   "ready",
				"summary": "ready",
			})
		case "/operator/status":
			writeJSONResponse(t, w, http.StatusUnauthorized, map[string]any{
				"error": "missing or invalid auth credentials",
			})
		case "/runtime/runs":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := (&externalInteractiveGateway{
		client: &GatewayClient{BaseURL: server.URL, HTTP: server.Client()},
		target: interactiveTarget{Kind: interactiveTargetRemote, Name: "prod", BaseURL: server.URL},
	}).ReadinessSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ReadinessSnapshot() error = %v", err)
	}
	var gatewayCategory *replpkg.ReadinessCategory
	var remoteCategory *replpkg.ReadinessCategory
	for i := range snapshot.Categories {
		switch snapshot.Categories[i].ID {
		case "gateway":
			gatewayCategory = &snapshot.Categories[i]
		case "remote_target":
			remoteCategory = &snapshot.Categories[i]
		}
	}
	if gatewayCategory == nil || remoteCategory == nil {
		t.Fatalf("categories = %#v", snapshot.Categories)
	}
	if gatewayCategory.Status != "blocked" {
		t.Fatalf("gateway status = %q", gatewayCategory.Status)
	}
	if remoteCategory.Status != "ready" {
		t.Fatalf("remote status = %q", remoteCategory.Status)
	}
}
