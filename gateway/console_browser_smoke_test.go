package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestConsoleBrowserOverviewShowsUnavailableSignalsWithoutErrorToast(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "operator unavailable"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/approvals":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/overview")
	waitForConsoleTestID(t, ctx, "overview-view")
	waitForConsoleTestIDAttr(t, ctx, "overview-view", "data-status-unavailable", "true")
	waitForConsoleTestIDAttr(t, ctx, "overview-view", "data-backend-issue", "true")
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserLanguageToggleSwitchesOverviewCopy(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/approvals":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/overview")
	waitForConsoleTestID(t, ctx, "overview-view")
	waitForConsoleStoreLang(t, ctx, "en")
	waitForConsoleTestID(t, ctx, "overview-open-workspace")

	clickConsoleElementByTestID(t, ctx, "shell-lang-toggle")
	waitForConsoleStoreLang(t, ctx, "zh")
	waitForConsoleTestID(t, ctx, "overview-open-workspace")
}

func TestConsoleBrowserOverviewShowsQualitySignalsAndRunsEvalSuite(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/approvals":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"name": "slack", "state": "connected"},
			}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"name": "browser", "health": "healthy"},
			}})
		case "/operator/quality/summary":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"run_count":          8,
				"terminal_run_count": 8,
				"task_success":       map[string]any{"count": 7, "total": 8, "rate": 0.875},
				"false_success":      map[string]any{"count": 0, "total": 7, "rate": 0.0},
				"fallback":           map[string]any{"count": 1, "total": 8, "rate": 0.125},
				"profile_hit":        map[string]any{"count": 7, "total": 8, "rate": 0.875},
			})
		case "/operator/quality/release-readiness":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"ready": false,
				"checks": []map[string]any{
					{"id": "sample_size", "status": "passed", "summary": "terminal run evidence is sufficient", "count": 8, "total": 5, "comparator": ">="},
					{"id": "task_success_rate", "status": "passed", "summary": "task success rate is within threshold", "measured": 0.875, "threshold": 0.8, "count": 7, "total": 8, "comparator": ">="},
					{"id": "fallback_rate", "status": "blocked", "summary": "fallback reliance violates threshold", "measured": 0.125, "threshold": 0.1, "count": 1, "total": 8, "comparator": "<="},
				},
				"blockers": []map[string]any{
					{"id": "fallback_rate", "status": "blocked", "summary": "fallback reliance violates threshold", "measured": 0.125, "threshold": 0.1, "count": 1, "total": 8, "comparator": "<="},
				},
			})
		case "/operator/evals/suites":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":            "browser.smoke",
						"name":          "Browser smoke",
						"description":   "Validate the browser surface stays green.",
						"surface":       "browser",
						"prerequisites": []string{"playwright"},
						"cases": []map[string]any{
							{"id": "visit-home", "name": "Visit home"},
							{"id": "open-result", "name": "Open result"},
						},
					},
				},
			})
		case "/operator/evals/run":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"suite":      map[string]any{"id": "browser.smoke", "name": "Browser smoke"},
				"status":     "passed",
				"case_count": 2,
				"passed":     2,
				"failed":     0,
				"errored":    0,
				"duration_ms": 1400,
				"quality": map[string]any{
					"task_success": map[string]any{"count": 2, "total": 2, "rate": 1.0},
					"fallback":     map[string]any{"count": 0, "total": 2, "rate": 0.0},
					"profile_hit":  map[string]any{"count": 2, "total": 2, "rate": 1.0},
				},
				"cases": []map[string]any{
					{
						"id":                   "visit-home",
						"name":                 "Visit home",
						"status":               "passed",
						"run_id":               "run-eval-1",
						"verification_status":  "passed",
						"verification_summary": "verification passed",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/overview")
	waitForConsoleTestID(t, ctx, "overview-quality-panel")
	waitForConsoleTestIDAttr(t, ctx, "overview-release-readiness-card", "data-state", "blocked")
	waitForConsoleBodyText(t, ctx, "Browser smoke")

	clickConsoleElementByTestID(t, ctx, "overview-eval-run-browser-smoke")
	waitForConsoleTestID(t, ctx, "overview-eval-run-report")
	waitForConsoleBodyText(t, ctx, "2 passed")
	waitForConsoleBodyText(t, ctx, "run-eval-1")
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserShowsSingleDeadLetterToastFromGlobalSSE(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/approvals":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/dashboard/sse":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer does not support flushing")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			event := map[string]any{
				"id":         "evt-dead-letter-1",
				"type":       "governance.delivery.dead_lettered",
				"session_id": "webchat",
				"time":       "2026-04-10T10:00:00Z",
				"attrs": map[string]any{
					"adapter_name":    "slack-alerts",
					"delivery_status": "dead_letter",
					"summary":         "Slack alert delivery failed",
				},
			}
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal dead-letter event: %v", err)
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/overview")
	waitForConsoleTestID(t, ctx, "overview-view")
	waitForConsoleToastText(t, ctx, "Slack alert delivery failed")
	waitForConsoleCount(t, ctx, `.hc-toast`, 1)
}

func TestConsoleBrowserSettingsInfrastructureShowsUnavailableInsteadOfFakeEmpty(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers":     []any{},
				"channels":      []any{},
				"provider_apis": []any{},
			})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/nodes":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "nodes unavailable"})
		case "/operator/devices":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/helpers/status":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "helpers unavailable"})
		case "/operator/instances":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "instances unavailable"})
		case "/operator/pairing":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "pairing unavailable"})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/infrastructure")
	waitForConsoleTestID(t, ctx, "settings-infra-nodes-unavailable")
	waitForConsoleTestID(t, ctx, "settings-infra-helpers-unavailable")
	body := consoleBodyText(t, ctx)
	if strings.Contains(body, "No nodes registered") {
		t.Fatalf("body should not render fake empty state when nodes endpoint is unavailable:\n%s", body)
	}
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserSettingsReadinessShowsCapabilityPacks(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers":     []any{},
				"channels":      []any{},
				"provider_apis": []any{},
			})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"modules": []map[string]any{
					{
						"id":       "builtin:integration-pack",
						"name":     "integration-pack",
						"source":   "builtin",
						"delivery": "bundled",
						"health":   map[string]any{"status": "ready"},
					},
					{
						"id":       "plugin:demo-pack",
						"name":     "demo-pack",
						"source":   "plugin",
						"delivery": "manifest",
						"health":   map[string]any{"status": "ready"},
					},
				},
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
					{"name": "desktop", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"providers": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/models")
	waitForConsoleTestID(t, ctx, "settings-tab-models")
	waitForConsoleTestID(t, ctx, "settings-runtime-capability-packs")
	waitForConsoleBodyText(t, ctx, "Ready: 2 pack(s) loaded (1 builtin, 1 plugin)")
	waitForConsoleBodyText(t, ctx, "5/5")
	assertConsoleNoPageErrors(t, ctx)
}

func TestConsoleBrowserSettingsDiagnosticsShowsUnavailableSignals(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "status unavailable"})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers":     []any{},
				"channels":      []any{},
				"provider_apis": []any{},
			})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/usage/summary":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "usage unavailable"})
		case "/operator/audit/events":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "audit unavailable"})
		case "/operator/wire/entries":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "logs unavailable"})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/diagnostics")
	waitForConsoleTestIDAttr(t, ctx, "settings-diagnostics-status", "data-unavailable", "true")
	waitForConsoleTestID(t, ctx, "settings-diagnostics-usage-unavailable")
	waitForConsoleTestID(t, ctx, "settings-diagnostics-audit-unavailable")
	waitForConsoleTestID(t, ctx, "settings-diagnostics-logs-unavailable")
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserAutomationSchedulesTreats503AsUnavailable(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/automation/templates":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/agents":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/automation/items":
			kinds := r.URL.Query().Get("kinds")
			switch kinds {
			case "cron,wakeup":
				writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduling unavailable"})
			case "watch":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"watch": map[string]any{"available": true},
					},
				})
			case "hook":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"hook": map[string]any{"available": true},
					},
				})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/automation/schedules")
	waitForConsoleTestID(t, ctx, "automation-schedules-unavailable")
	body := consoleBodyText(t, ctx)
	if strings.Contains(body, "Load failed") {
		t.Fatalf("body should not show generic load failure for 503 scheduling service:\n%s", body)
	}
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserGovernanceOperationsTreats503AsUnavailable(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/governance/health":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "governance delivery controller is not configured"})
		case "/operator/governance/deliveries/stats":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "governance delivery controller is not configured"})
		case "/operator/governance/deliveries":
			writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "governance delivery controller is not configured"})
		case "/operator/governance/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":   "gov-event-1",
						"type": "governance.dead_letter",
						"time": "2026-03-30T10:00:00Z",
						"attrs": map[string]any{
							"adapter_name":    "slack-alerts",
							"delivery_status": "dead_letter",
						},
					},
				},
			})
		case "/operator/controlplane/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"issues": []any{},
				"auth":   map[string]any{"configured": true, "ready": true},
				"authz": map[string]any{
					"kind":           "open",
					"mode":           "toc",
					"default_effect": "allow",
					"bindings": []any{
						map[string]any{"name": "all-callers", "kind": "policy"},
					},
					"resources": []any{"*"},
					"actions":   []any{"read", "write"},
				},
			})
		case "/operator/approvals/providers":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/governance/adapters":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/audit/sinks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/policy/engines":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"policy": map[string]any{"layers": []any{}}})
		case "/operator/config/credentials":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/operator/authz":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"summary": map[string]any{
					"kind":           "open",
					"mode":           "toc",
					"default_effect": "allow",
					"bindings":       []any{},
					"resources":      []any{"*"},
					"actions":        []any{"read", "write"},
				},
				"decision": map[string]any{
					"allowed": true,
					"source":  "open",
				},
				"identity": nil,
			})
		case "/operator/audit/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/governance")
	waitForConsoleTestIDAttr(t, ctx, "governance-operations-error", "data-unavailable", "true")
	waitForConsoleBodyText(t, ctx, "governance.dead_letter")
	assertConsoleNoToast(t, ctx)
}

func TestConsoleBrowserGovernanceOperationsSkipsDeliveryEndpointsWhenControllerUnavailable(t *testing.T) {
	var mu sync.Mutex
	deliveryCalls := 0

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/governance/health", "/operator/governance/deliveries/stats", "/operator/governance/deliveries":
			mu.Lock()
			deliveryCalls++
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusInternalServerError, map[string]any{"error": "should not request governance deliveries"})
		case "/operator/governance/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":   "gov-event-1",
						"type": "governance.dead_letter",
						"time": "2026-03-30T10:00:00Z",
						"attrs": map[string]any{
							"adapter_name":    "slack-alerts",
							"delivery_status": "dead_letter",
						},
					},
				},
			})
		case "/operator/controlplane/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"issues": []any{},
				"auth":   map[string]any{"configured": true, "ready": true},
				"authz": map[string]any{
					"kind":           "open",
					"mode":           "toc",
					"default_effect": "allow",
					"bindings":       []any{},
					"resources":      []any{"*"},
					"actions":        []any{"read", "write"},
				},
				"effective_config": map[string]any{
					"id":                       "ecs-test",
					"governance_adapter_names": []any{},
				},
			})
		case "/operator/approvals/providers":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/governance/adapters":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/audit/sinks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/policy/engines":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"policy": map[string]any{"layers": []any{}}})
		case "/operator/config/credentials":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		case "/operator/authz":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"summary": map[string]any{
					"kind":           "open",
					"mode":           "toc",
					"default_effect": "allow",
					"bindings":       []any{},
					"resources":      []any{"*"},
					"actions":        []any{"read", "write"},
				},
				"decision": map[string]any{
					"allowed": true,
					"source":  "open",
				},
				"identity": nil,
			})
		case "/operator/audit/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/governance")
	waitForConsoleTestIDAttr(t, ctx, "governance-operations-error", "data-unavailable", "true")
	waitForConsoleBodyText(t, ctx, "governance.dead_letter")
	assertConsoleNoToast(t, ctx)

	mu.Lock()
	calls := deliveryCalls
	mu.Unlock()
	if calls != 0 {
		t.Fatalf("governance delivery endpoints called %d times, want 0", calls)
	}
}

func TestConsoleBrowserAutomationSchedulesEnableCronFlow(t *testing.T) {
	var mu sync.Mutex
	cronEnabled := false
	lastCronConfig := consoleCronConfigPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/automation/templates":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/agents":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/automation/items":
			switch r.URL.Query().Get("kinds") {
			case "cron,wakeup":
				mu.Lock()
				enabled := cronEnabled
				mu.Unlock()
				if !enabled {
					writeConsoleBrowserJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduling unavailable"})
					return
				}
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"cron":   map[string]any{"available": true},
						"wakeup": map[string]any{"available": false},
					},
				})
			case "watch":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"watch": map[string]any{"available": true},
					},
				})
			case "hook":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"hook": map[string]any{"available": true},
					},
				})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
		case "/operator/config/cron":
			if r.Method != http.MethodPut {
				http.NotFound(w, r)
				return
			}
			var payload consoleCronConfigPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cron config payload: %v", err)
			}
			mu.Lock()
			lastCronConfig = payload
			cronEnabled = payload.Enabled
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/automation/schedules")
	waitForConsoleBodyText(t, ctx, "Scheduling Not Enabled")
	clickConsoleButtonByText(t, ctx, "Enable Cron Service")
	waitForConsoleToastText(t, ctx, "Cron service enabled")
	waitForConsoleBodyText(t, ctx, "No scheduled jobs or triggers")
	waitForConsoleBodyTextMissing(t, ctx, "Scheduling Not Enabled")

	mu.Lock()
	defer mu.Unlock()
	if !lastCronConfig.Enabled {
		t.Fatalf("cron enable payload = %+v, want enabled=true", lastCronConfig)
	}
}

func TestConsoleBrowserAutomationSchedulesCreateCronFlow(t *testing.T) {
	var mu sync.Mutex
	var createdPayload consoleCronCreatePayload
	cronJobs := []map[string]any{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/automation/templates":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/agents":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/automation/items":
			switch r.URL.Query().Get("kinds") {
			case "cron,wakeup":
				mu.Lock()
				items := append([]map[string]any(nil), cronJobs...)
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": items,
					"services": map[string]any{
						"cron":   map[string]any{"available": true},
						"wakeup": map[string]any{"available": false},
					},
				})
			case "watch":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"watch": map[string]any{"available": true},
					},
				})
			case "hook":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"hook": map[string]any{"available": true},
					},
				})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
		case "/operator/cron/jobs":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleCronCreatePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cron create payload: %v", err)
			}
			job := map[string]any{
				"id":      "cron-daily-digest",
				"kind":    "cron",
				"name":    payload.Name,
				"enabled": payload.Enabled,
				"schedule": map[string]any{
					"kind":       payload.Schedule.Kind,
					"expression": payload.Schedule.Expression,
				},
				"payload": map[string]any{
					"content": payload.Payload.Content,
				},
			}
			mu.Lock()
			createdPayload = payload
			cronJobs = []map[string]any{job}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"job": job})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/automation/schedules")
	waitForConsoleBodyText(t, ctx, "No scheduled jobs or triggers")
	clickConsoleButtonByText(t, ctx, "Create Job")
	waitForConsoleBodyText(t, ctx, "Prompt")
	setConsoleFieldValueByLabel(t, ctx, "Name", "daily-digest")
	setConsoleFieldValueByLabel(t, ctx, "Cron Expression", "0 9 * * *")
	setConsoleFieldValueByLabel(t, ctx, "Prompt", "digest prompt")
	clickConsoleButtonByTextLast(t, ctx, "Create Job")
	waitForConsoleToastText(t, ctx, "Job created")
	waitForConsoleBodyText(t, ctx, "daily-digest")

	mu.Lock()
	defer mu.Unlock()
	if createdPayload.Name != "daily-digest" {
		t.Fatalf("created cron payload name = %q, want %q", createdPayload.Name, "daily-digest")
	}
	if createdPayload.Schedule.Kind != "cron" || createdPayload.Schedule.Expression != "0 9 * * *" {
		t.Fatalf("created cron schedule = %+v, want kind=cron expression=0 9 * * *", createdPayload.Schedule)
	}
	if createdPayload.Payload.Content != "digest prompt" {
		t.Fatalf("created cron content = %q, want %q", createdPayload.Payload.Content, "digest prompt")
	}
}

func TestConsoleBrowserAutomationHooksAdvancedEditFromTemplateHasNoPageErrors(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/automation/templates":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":          "hook-approval-callback",
						"kind":        "hook",
						"name":        "Approval callback webhook",
						"headline":    "Resolve approval tickets from downstream systems.",
						"summary":     "Send approval resolution events to a downstream system when a ticket is approved or denied.",
						"outcome":     "Approval callback webhook ready.",
						"setup_hints": []string{"Point this hook at your callback endpoint."},
						"trigger":     "run.completed",
						"url":         "https://example.com/hooks/approval",
						"phase":       "post",
						"retry_count": 3,
						"timeout_sec": 30,
						"enabled":     true,
					},
				},
			})
		case "/operator/agents":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/hooks/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"trigger":        "run.completed",
						"description":    "Run completed",
						"supports_async": true,
						"can_block":      false,
						"category":       "run",
					},
				},
			})
		case "/operator/automation/items":
			switch r.URL.Query().Get("kinds") {
			case "cron,wakeup":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"cron":   map[string]any{"available": true},
						"wakeup": map[string]any{"available": true},
					},
				})
			case "watch":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"watch": map[string]any{"available": true},
					},
				})
			case "hook":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items": []any{},
					"services": map[string]any{
						"hook": map[string]any{"available": true},
					},
				})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/automation/hooks")
	waitForConsoleBodyText(t, ctx, "Official Starter Workflows")
	clickConsoleElementBySelector(t, ctx, ".hc-automation-template-strip .hc-page-section-head")
	waitForConsoleBodyText(t, ctx, "Approval callback webhook")
	clickConsoleButtonByText(t, ctx, "Use template")
	waitForConsoleBodyText(t, ctx, "Required inputs")
	clickConsoleButtonByText(t, ctx, "Advanced edit")
	waitForConsoleBodyText(t, ctx, "Headers")
	waitForConsoleBodyText(t, ctx, "Create")
	assertConsoleNoPageErrors(t, ctx)
}

func TestConsoleBrowserApprovalsResolveFlowPersistsScope(t *testing.T) {
	var mu sync.Mutex
	var lastResolve consoleApprovalResolvePayload
	pending := []map[string]any{
		{
			"id":          "ticket-1",
			"tool_name":   "deploy-prod-shell",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"cmd":"deploy --prod"}`,
			"reasons":     []string{"Production deployment requires approval"},
		},
	}
	approved := []map[string]any{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/approvals":
			status := r.URL.Query().Get("status")
			mu.Lock()
			switch status {
			case "pending":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), pending...)})
			case "approved":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), approved...)})
			case "denied":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
			mu.Unlock()
		case "/operator/approvals/ticket-1/resolve":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleApprovalResolvePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode resolve approval payload: %v", err)
			}
			mu.Lock()
			lastResolve = payload
			pending = []map[string]any{}
			approved = []map[string]any{
				{
					"id":          "ticket-1",
					"tool_name":   "deploy-prod-shell",
					"resolution":  payload.Status,
					"scope":       payload.Scope,
					"resolved_by": payload.By,
					"resolved_at": time.Date(2026, 3, 27, 10, 1, 0, 0, time.UTC).Format(time.RFC3339),
					"session_key": "webchat",
				},
			}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/approvals")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	clickConsoleButtonByText(t, ctx, "Session")
	clickConsoleButtonByText(t, ctx, "Approve")
	waitForConsoleToastText(t, ctx, "Approved")
	waitForConsoleCount(t, ctx, `.hc-approval-card`, 0)
	waitForConsoleBodyText(t, ctx, "No pending approvals")
	clickConsoleButtonByText(t, ctx, "Resolved")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	waitForConsoleBodyText(t, ctx, "session")

	mu.Lock()
	defer mu.Unlock()
	if lastResolve.Scope != "session" || lastResolve.Status != "approved" || lastResolve.By != "operator" {
		t.Fatalf("resolve payload = %+v, want scope=session status=approved by=operator", lastResolve)
	}
}

func TestConsoleBrowserApprovalsRefreshesFromDottedSSEIntoApprovalView(t *testing.T) {
	var mu sync.Mutex
	pending := []map[string]any{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/approvals":
			status := r.URL.Query().Get("status")
			mu.Lock()
			switch status {
			case "pending":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), pending...)})
			case "approved", "denied":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
			mu.Unlock()
		case "/runtime/events/stream":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer does not support flushing")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)

			mu.Lock()
			pending = []map[string]any{
				{
					"id":          "ticket-sse-approval-1",
					"run_id":      "run-sse-approval-1",
					"session_key": "webchat",
					"created_at":  time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
					"tool_calls": []map[string]any{
						{"id": "call-1", "name": "deploy-prod-shell", "input": map[string]any{"cmd": "deploy --prod"}},
					},
					"governance": map[string]any{
						"tool_names": []string{"deploy-prod-shell"},
						"summary":    "release readiness blocked prod deploy",
						"policy": map[string]any{
							"summary": "release readiness blocked prod deploy",
							"reasons": []string{"Release readiness blocked high-risk execution"},
						},
					},
					"resource_scope_summary": "automation=prod-release",
				},
			}
			mu.Unlock()

			event := map[string]any{
				"id":         "evt-approval-dotted-1",
				"type":       "approval.requested",
				"session_id": "webchat",
				"run_id":     "run-sse-approval-1",
				"attrs": map[string]any{
					"approval_id":    "ticket-sse-approval-1",
					"tool_names":     []string{"deploy-prod-shell"},
					"policy_summary": "release readiness blocked prod deploy",
				},
			}
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal approval event: %v", err)
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/approvals")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	waitForConsoleBodyText(t, ctx, "Release readiness blocked high-risk execution")
	waitForConsoleBodyText(t, ctx, "automation=prod-release")
	waitForConsoleBodyText(t, ctx, "deploy --prod")
	assertConsoleNoPageErrors(t, ctx)
}

func TestConsoleBrowserSettingsSecuritySaveRoundTrip(t *testing.T) {
	var mu sync.Mutex
	securityConfig := map[string]any{
		"exec_approval": map[string]any{
			"mode": "deny",
		},
		"network": map[string]any{
			"allow_private": false,
			"allow_local":   false,
			"deny_hosts":    []string{},
		},
		"filesystem": map[string]any{
			"allowed_roots": []string{},
			"deny_patterns": []string{},
			"skip_dirs":     []string{},
		},
		"patterns": []any{},
		"sandbox":  false,
	}
	toolsConfig := map[string]any{
		"layer2":    map[string]any{},
		"dangerous": []string{},
	}
	lastSecurityPayload := consoleSecurityPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers":     []any{},
				"channels":      []any{},
				"provider_apis": []any{},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{{"name": "browser", "health": "healthy"}},
				"channels":     []map[string]any{{"name": "slack", "status": "connected"}},
			})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/config/security":
			if r.Method == http.MethodPut {
				var payload consoleSecurityPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode security payload: %v", err)
				}
				mu.Lock()
				lastSecurityPayload = payload
				securityConfig["exec_approval"] = map[string]any{
					"mode":             payload.ExecApproval.Mode,
					"approval_timeout": payload.ExecApproval.ApprovalTimeout,
					"grace_period":     payload.ExecApproval.GracePeriod,
				}
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			mu.Lock()
			writeConsoleBrowserJSON(w, http.StatusOK, securityConfig)
			mu.Unlock()
		case "/operator/config/tools":
			mu.Lock()
			writeConsoleBrowserJSON(w, http.StatusOK, toolsConfig)
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/security")
	waitForConsoleBodyText(t, ctx, "Exec Approval Mode")
	selectConsoleFieldValueByLabel(t, ctx, "Mode", "approve")
	setConsoleFieldValueByLabel(t, ctx, "Approval Timeout", "120")
	setConsoleFieldValueByLabel(t, ctx, "Grace Period", "15")
	clickConsoleButtonByText(t, ctx, "Save Security Config")
	waitForConsoleToastText(t, ctx, "Security settings saved")
	waitForConsoleFieldValueByLabel(t, ctx, "Approval Timeout", "120")
	waitForConsoleFieldValueByLabel(t, ctx, "Grace Period", "15")

	mu.Lock()
	defer mu.Unlock()
	if lastSecurityPayload.ExecApproval.Mode != "approve" {
		t.Fatalf("security payload mode = %q, want %q", lastSecurityPayload.ExecApproval.Mode, "approve")
	}
	if lastSecurityPayload.ExecApproval.ApprovalTimeout != 120 || lastSecurityPayload.ExecApproval.GracePeriod != 15 {
		t.Fatalf("security payload approval = %+v, want timeout=120 grace=15", lastSecurityPayload.ExecApproval)
	}
}

func TestConsoleBrowserSettingsChannelsCreateFlow(t *testing.T) {
	var mu sync.Mutex
	channels := []map[string]any{}
	lastCreate := consoleChannelCreatePayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers": []any{},
				"channels": []map[string]any{
					{
						"id":           "slack",
						"display_name": "Slack",
						"operator_fields": []map[string]any{
							{"id": "token", "label": "Bot Token", "type": "password", "required": true},
							{"id": "default_channel", "label": "Default Channel", "type": "string", "required": false},
						},
					},
				},
				"provider_apis": []any{},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{{"name": "browser", "health": "healthy"}},
				"channels":     []any{},
			})
		case "/operator/channels":
			switch r.Method {
			case http.MethodGet:
				mu.Lock()
				items := append([]map[string]any(nil), channels...)
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
			case http.MethodPost:
				var payload consoleChannelCreatePayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode channel create payload: %v", err)
				}
				item := map[string]any{
					"name":    payload.Name,
					"enabled": payload.Enabled,
					"source":  "api",
					"config":  payload.Config,
				}
				mu.Lock()
				lastCreate = payload
				channels = []map[string]any{item}
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, item)
			default:
				http.NotFound(w, r)
			}
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/matrix":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/channels/thread-bindings":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/channels")
	waitForConsoleBodyText(t, ctx, "Channel Configuration")
	waitForConsoleBodyText(t, ctx, "No data")
	clickConsoleButtonByText(t, ctx, "Add Channel")
	selectConsoleFieldValueByLabel(t, ctx, "Channel Type", "slack")
	setConsoleFieldValueByLabel(t, ctx, "Channel Name", "slack-bot")
	clickConsoleButtonByText(t, ctx, "Save")
	waitForConsoleToastText(t, ctx, "Settings saved")
	waitForConsoleBodyText(t, ctx, "slack-bot")
	waitForConsoleBodyText(t, ctx, "slack")

	mu.Lock()
	defer mu.Unlock()
	if lastCreate.Name != "slack-bot" || !lastCreate.Enabled {
		t.Fatalf("channel create payload = %+v, want name=slack-bot enabled=true", lastCreate)
	}
	if got := stringValue(lastCreate.Config["type"]); got != "slack" {
		t.Fatalf("channel config type = %q, want %q", got, "slack")
	}
}

func TestConsoleBrowserSettingsPluginsShowsRuntimeCapabilityPacks(t *testing.T) {
	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers":     []any{},
				"channels":      []any{},
				"provider_apis": []any{},
			})
		case "/operator/plugins":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"name":             "demo-pack",
						"version":          "1.2.3",
						"description":      "Demo plugin pack",
						"dir":              "/tmp/demo-pack",
						"component_counts": map[string]any{"tools": 2},
					},
				},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"modules": []map[string]any{
					{
						"id":          "builtin:operator-support-pack",
						"name":        "operator-support-pack",
						"source":      "builtin",
						"delivery":    "bundled",
						"description": "Operator support surfaces",
						"health":      map[string]any{"status": "ready", "summary": "support runtime healthy"},
						"contributions": map[string]any{
							"total_count":   1,
							"tool_count":    1,
							"tool_names":    []string{"support.status"},
							"channel_count": 0,
						},
					},
					{
						"id":           "plugin:demo-pack",
						"name":         "demo-pack",
						"version":      "1.2.3",
						"source":       "plugin",
						"delivery":     "manifest",
						"description":  "Demo plugin pack",
						"dependencies": []string{"builtin:operator-support-pack"},
						"health":       map[string]any{"status": "ready", "summary": "plugin loaded"},
						"contributions": map[string]any{
							"total_count":   3,
							"tool_count":    2,
							"tool_names":    []string{"demo.echo", "demo.reply"},
							"channel_count": 1,
							"channel_names": []string{"demo-webhook"},
						},
					},
				},
				"capabilities": []map[string]any{
					{"name": "browser", "health": "healthy"},
					{"name": "desktop", "health": "healthy"},
				},
				"channels": []map[string]any{
					{"name": "slack", "status": "connected"},
				},
			})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/plugins")
	waitForConsoleBodyText(t, ctx, "Installed Plugins")
	waitForConsoleBodyText(t, ctx, "Runtime Capability Packs")
	waitForConsoleBodyText(t, ctx, "operator-support-pack")
	waitForConsoleBodyText(t, ctx, "builtin:operator-support-pack")
	waitForConsoleBodyText(t, ctx, "demo-pack")
	waitForConsoleBodyText(t, ctx, "1 builtin")
	waitForConsoleBodyText(t, ctx, "1 plugin")
	waitForConsoleBodyText(t, ctx, "manifest")
	waitForConsoleBodyText(t, ctx, "2 tools")
	waitForConsoleBodyText(t, ctx, "1 channel")
	waitForConsoleBodyText(t, ctx, "demo.echo, demo.reply, demo-webhook")
	assertConsoleNoPageErrors(t, ctx)
}

func TestConsoleBrowserKnowledgeMemoryCreateAndDeleteFlow(t *testing.T) {
	var mu sync.Mutex
	memItems := []map[string]any{}
	notebook := map[string]any{
		"path":       "/tmp/notebook.md",
		"content":    "",
		"updated_at": "",
	}
	lastCreate := consoleMemoryRecordPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/memory":
			mu.Lock()
			items := append([]map[string]any(nil), memItems...)
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
		case "/runtime/memory/notebook":
			mu.Lock()
			payload := map[string]any{
				"path":       notebook["path"],
				"content":    notebook["content"],
				"updated_at": notebook["updated_at"],
			}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, payload)
		case "/runtime/memory/records":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleMemoryRecordPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode memory create payload: %v", err)
			}
			key := fmt.Sprintf("%s:%s:%s", payload.Namespace, payload.ScopeKey, payload.Field)
			item := map[string]any{
				"key":            key,
				"namespace":      payload.Namespace,
				"scope_key":      payload.ScopeKey,
				"field":          payload.Field,
				"label":          payload.Label,
				"value":          payload.Value,
				"source":         payload.Source,
				"confidence":     payload.Confidence,
				"updated_at":     "2026-03-27T10:00:00Z",
				"evidence_count": 0,
			}
			mu.Lock()
			lastCreate = payload
			memItems = []map[string]any{item}
			notebook["content"] = "- Preferred Language: Go"
			notebook["updated_at"] = "2026-03-27T10:00:00Z"
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, item)
		case "/runtime/memory/profile:user:favorite_language":
			if r.Method != http.MethodDelete {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			memItems = []map[string]any{}
			notebook["content"] = ""
			notebook["updated_at"] = "2026-03-27T10:01:00Z"
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusNoContent, nil)
		case "/operator/knowledge/sources":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}, "supported_kinds": []any{}})
		case "/operator/artifacts":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/knowledge")
	waitForConsoleBodyText(t, ctx, "No memory entries found.")
	clickConsoleButtonByText(t, ctx, "Add Entry")
	waitForConsoleBodyText(t, ctx, "Memory Type")
	waitForConsoleBodyText(t, ctx, "Applies To")
	waitForConsoleBodyTextMissing(t, ctx, "Namespace")
	setConsoleFieldValueByLabel(t, ctx, "Field", "favorite_language")
	setConsoleFieldValueByLabel(t, ctx, "Name", "Preferred Language")
	setConsoleFieldValueByLabel(t, ctx, "Value", "Go")
	clickConsoleButtonByText(t, ctx, "Save")
	waitForConsoleToastText(t, ctx, "Entry saved")
	waitForConsoleCount(t, ctx, ".hc-list-card", 1)
	waitForConsoleBodyText(t, ctx, "Go")
	clickConsoleButtonByText(t, ctx, "Delete")
	waitForConsoleToastText(t, ctx, "Entry deleted")
	waitForConsoleBodyText(t, ctx, "No memory entries found.")

	mu.Lock()
	defer mu.Unlock()
	if lastCreate.Namespace != "profile" || lastCreate.ScopeKey != "user" || lastCreate.Field != "favorite_language" {
		t.Fatalf("memory create payload = %+v, want namespace=profile scope_key=user field=favorite_language", lastCreate)
	}
	if lastCreate.Label != "Preferred Language" || lastCreate.Value != "Go" {
		t.Fatalf("memory create payload = %+v, want label=Preferred Language value=Go", lastCreate)
	}
}

func TestConsoleBrowserKnowledgeSourcesCreateSyncAndDeleteFlow(t *testing.T) {
	var mu sync.Mutex
	sources := []map[string]any{}
	lastCreate := consoleSourcePayload{}
	supportedKinds := []map[string]any{
		{
			"value":       "local_dir",
			"label":       "Local Directory",
			"description": "Index a maintained folder without moving the source of truth.",
			"fields": []map[string]any{
				{
					"id":       "path",
					"key":      "path",
					"scope":    "root",
					"label":    "Root Path",
					"type":     "string",
					"required": true,
				},
			},
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/memory":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/memory/notebook":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"path": "/tmp/notebook.md", "content": "", "updated_at": ""})
		case "/operator/artifacts":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/knowledge/sources":
			switch r.Method {
			case http.MethodGet:
				mu.Lock()
				items := append([]map[string]any(nil), sources...)
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
					"items":           items,
					"supported_kinds": supportedKinds,
				})
			case http.MethodPost:
				var payload consoleSourcePayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode source create payload: %v", err)
				}
				item := map[string]any{
					"id":           "source-eng-docs",
					"name":         payload.Name,
					"kind":         payload.Kind,
					"enabled":      payload.Enabled,
					"path":         payload.Path,
					"config":       payload.Config,
					"urls":         payload.URLs,
					"status":       "blocked",
					"stats":        map[string]any{"documents": 0, "chunks": 0, "bytes": 0},
					"last_sync_at": "",
				}
				mu.Lock()
				lastCreate = payload
				sources = []map[string]any{item}
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"item": item})
			default:
				http.NotFound(w, r)
			}
		case "/operator/knowledge/sources/source-eng-docs/sync":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			if len(sources) > 0 {
				sources[0]["status"] = "ready"
				sources[0]["last_sync_at"] = "2026-03-27T11:00:00Z"
				sources[0]["stats"] = map[string]any{"documents": 2, "chunks": 5, "bytes": 2048}
			}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/knowledge/sources/source-eng-docs":
			if r.Method != http.MethodDelete {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			sources = []map[string]any{}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusNoContent, nil)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/knowledge")
	clickConsoleButtonByText(t, ctx, "Sources")
	waitForConsoleBodyText(t, ctx, "No knowledge sources yet.")
	clickConsoleButtonByText(t, ctx, "Add Source")
	setConsoleFieldValueByLabel(t, ctx, "Name", "Engineering Docs")
	setConsoleFieldValueByLabel(t, ctx, "Root Path", "/workspace/docs")
	clickConsoleButtonByText(t, ctx, "Create & Sync")
	waitForConsoleToastText(t, ctx, "Source created")
	waitForConsoleToastText(t, ctx, "Source synced")
	waitForConsoleBodyText(t, ctx, "Engineering Docs")
	waitForConsoleBodyText(t, ctx, "2 docs")
	waitForConsoleBodyText(t, ctx, "5 chunks")
	clickConsoleButtonByText(t, ctx, "Delete")
	waitForConsoleToastText(t, ctx, "Source deleted")
	waitForConsoleBodyText(t, ctx, "No knowledge sources yet.")

	mu.Lock()
	defer mu.Unlock()
	if lastCreate.Name != "Engineering Docs" || lastCreate.Kind != "local_dir" || !lastCreate.Enabled {
		t.Fatalf("source create payload = %+v, want name=Engineering Docs kind=local_dir enabled=true", lastCreate)
	}
	if lastCreate.Path != "/workspace/docs" {
		t.Fatalf("source create path = %q, want %q", lastCreate.Path, "/workspace/docs")
	}
}

func TestConsoleBrowserGovernanceTabsSwitchAndRedriveFlow(t *testing.T) {
	var mu sync.Mutex
	redriveCount := 0
	deliveryStatus := "dead_letter"
	deliverySummary := "Slack alert delivery failed"
	deliveryDeliveredAt := ""
	deliveryCanRedrive := true

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/governance/health":
			mu.Lock()
			status := deliveryStatus
			summary := deliverySummary
			canRedrive := deliveryCanRedrive
			deliveredAt := deliveryDeliveredAt
			mu.Unlock()
			payload := map[string]any{
				"status":              "critical",
				"summary":             "1 dead letter pending review",
				"dead_letter_count":   1,
				"redrivable_count":    1,
				"stale_pending_count": 1,
				"oldest_pending_at":   "2026-03-27T09:58:00Z",
				"pending_count":       1,
				"delivered_count":     0,
				"adapters_impacted":   []string{"slack-alerts"},
			}
			if status == "delivered" {
				payload["status"] = "ok"
				payload["summary"] = "All deliveries healthy"
				payload["dead_letter_count"] = 0
				payload["redrivable_count"] = 0
				payload["stale_pending_count"] = 0
				payload["pending_count"] = 0
				payload["delivered_count"] = 1
				payload["oldest_pending_at"] = deliveredAt
				if !canRedrive {
					payload["adapters_impacted"] = []string{}
				}
			}
			_ = summary
			writeConsoleBrowserJSON(w, http.StatusOK, payload)
		case "/operator/governance/deliveries/stats":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"total": 1})
		case "/operator/governance/deliveries":
			if r.Method == http.MethodPost {
				mu.Lock()
				redriveCount++
				deliveryStatus = "delivered"
				deliverySummary = "Slack alert delivered"
				deliveryDeliveredAt = "2026-03-27T10:05:00Z"
				deliveryCanRedrive = false
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"updated": 1})
				return
			}
			mu.Lock()
			status := deliveryStatus
			summary := deliverySummary
			deliveredAt := deliveryDeliveredAt
			canRedrive := deliveryCanRedrive
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":           "delivery-1",
						"status":       status,
						"adapter_name": "slack-alerts",
						"attempts":     2,
						"max_attempts": 5,
						"updated_at":   "2026-03-27T10:00:00Z",
						"delivered_at": deliveredAt,
						"can_redrive":  canRedrive,
						"record": map[string]any{
							"summary":                      summary,
							"kind":                         "dead_letter",
							"event_type":                   "governance.dead_letter",
							"event_id":                     "event-1",
							"run_id":                       "run-1",
							"session_id":                   "webchat",
							"severity":                     "high",
							"security_category":            "delivery",
							"effective_config_snapshot_id": "cfg-1",
							"tool_names":                   []string{"notify"},
							"scope": map[string]any{
								"automation_id": "automation-webchat",
							},
						},
						"last_error": map[bool]string{
							true:  "",
							false: "Webhook returned 500",
						}[status == "delivered"],
					},
				},
			})
		case "/operator/governance/deliveries/delivery-1":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"id":           "delivery-1",
				"status":       "dead_letter",
				"adapter_name": "slack-alerts",
				"attempts":     2,
				"max_attempts": 5,
				"updated_at":   "2026-03-27T10:00:00Z",
				"can_redrive":  true,
				"record": map[string]any{
					"summary":                      "Slack alert delivery failed",
					"kind":                         "dead_letter",
					"event_type":                   "governance.dead_letter",
					"event_id":                     "event-1",
					"run_id":                       "run-1",
					"session_id":                   "webchat",
					"severity":                     "high",
					"security_category":            "delivery",
					"effective_config_snapshot_id": "cfg-1",
					"tool_names":                   []string{"notify"},
					"scope": map[string]any{
						"automation_id": "automation-webchat",
					},
				},
				"last_error": "Webhook returned 500",
			})
		case "/operator/governance/deliveries/delivery-1/redrive":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			redriveCount++
			deliveryStatus = "delivered"
			deliverySummary = "Slack alert delivered"
			deliveryDeliveredAt = "2026-03-27T10:05:00Z"
			deliveryCanRedrive = false
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"updated": 1})
		case "/operator/governance/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":   "evt-1",
						"type": "governance.dead_letter",
						"time": "2026-03-27T10:00:00Z",
						"attrs": map[string]any{
							"adapter_name":    "slack-alerts",
							"delivery_status": "dead_letter",
						},
					},
				},
			})
		case "/operator/controlplane/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"ok":     false,
				"issues": []string{"approval callback secret missing"},
				"auth": map[string]any{
					"configured": true,
					"ready":      false,
				},
				"approvals": map[string]any{
					"store_available": true,
				},
				"effective_config": map[string]any{
					"id":              "cfg-1",
					"edition":         "consumer",
					"runtime_profile": "default",
					"default_model":   "gpt-4.1",
					"store_backend":   "sqlite",
					"generated_at":    "2026-03-27T10:00:00Z",
					"capabilities": map[string]any{
						"builtins_enabled": true,
						"search_enabled":   true,
					},
					"policy_pack_ids": []string{"base"},
					"layers": []map[string]any{
						{"name": "default", "kind": "builtin", "source": "runtime"},
					},
					"approval": map[string]any{
						"exec_mode":                  "approve",
						"skill_install_policy":       "approve",
						"default_grant_scope":        "session",
						"max_grant_scope":            "workspace",
						"require_approval_for_write": true,
						"deny_destructive":           true,
					},
				},
				"policy": map[string]any{
					"kind": "layered",
					"layers": []map[string]any{
						{
							"layer_order":                1,
							"name":                       "runtime",
							"type":                       "builtin",
							"require_approval_for_write": true,
							"deny_destructive":           true,
							"security_audit_wired":       true,
							"grant_store_wired":          true,
						},
					},
				},
				"authz": map[string]any{
					"kind":           "rbac",
					"mode":           "overlay",
					"default_effect": "deny",
					"bindings": []map[string]any{
						{
							"name":        "operator",
							"kind":        "role",
							"description": "runs (read, write)",
							"resources":   []string{"runs"},
							"actions":     []string{"read", "write"},
						},
					},
					"resources": []string{"runs"},
					"actions":   []string{"read", "write"},
				},
			})
		case "/operator/approvals/providers":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"name":           "slack-approvals",
						"type":           "slack",
						"enabled":        true,
						"registered":     true,
						"submit_enabled": true,
						"update_enabled": true,
						"sync_enabled":   false,
						"metadata":       map[string]any{"workspace": "ops"},
						"callback_auth":  map[string]any{"protected": true, "mode": "hmac", "header_name": "X-Signature", "max_age": 60000000000},
					},
				},
			})
		case "/operator/governance/adapters":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"name":             "slack-alerts",
						"type":             "webhook",
						"enabled":          true,
						"registered":       true,
						"include_snapshot": true,
						"kinds":            []string{"dead_letter", "audit"},
						"metadata":         map[string]any{"workspace": "ops"},
					},
				},
			})
		case "/operator/audit/sinks":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"name": "audit-webhook", "type": "webhook", "enabled": true, "registered": true, "target": "https://audit.example.com"},
				},
			})
		case "/operator/policy/engines":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"policy": map[string]any{
					"kind": "layered",
					"layers": []map[string]any{
						{
							"layer_order":                1,
							"name":                       "runtime",
							"type":                       "builtin",
							"require_approval_for_write": true,
							"deny_destructive":           true,
							"security_audit_wired":       true,
							"grant_store_wired":          true,
						},
					},
				},
			})
		case "/operator/config/credentials":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"path": "secret://slack/token", "kind": "env", "locator": "SLACK_TOKEN"},
				},
				"count":   1,
				"by_kind": map[string]any{"env": 1},
			})
		case "/operator/authz":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"summary": map[string]any{
					"kind":           "rbac",
					"mode":           "overlay",
					"default_effect": "deny",
					"bindings": []map[string]any{
						{
							"name":        "operator",
							"kind":        "role",
							"description": "runs (read, write)",
							"resources":   []string{"runs"},
							"actions":     []string{"read", "write"},
						},
					},
					"resources": []string{"runs"},
					"actions":   []string{"read", "write"},
				},
				"decision": map[string]any{
					"allowed": true,
					"source":  "rbac",
					"metadata": map[string]any{
						"resolved_role": "operator",
					},
				},
				"identity": map[string]any{
					"subject":  "user:test",
					"provider": "local",
					"scopes":   []string{"workspace"},
				},
			})
		case "/operator/audit/events":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{
						"id":         "audit-1",
						"type":       "governance.dead_letter",
						"severity":   "high",
						"time":       "2026-03-27T10:00:00Z",
						"run_id":     "run-1",
						"session_id": "webchat",
						"attrs": map[string]any{
							"family":       "governance",
							"summary":      "Slack alert delivery failed",
							"adapter_name": "slack-alerts",
							"approval_id":  "ticket-1",
						},
						"governance": map[string]any{
							"tool_names":                   []string{"notify"},
							"effective_config_snapshot_id": "cfg-1",
							"scope": map[string]any{
								"automation_id": "automation-webchat",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/governance")
	waitForConsoleBodyText(t, ctx, "Operator Overview")
	waitForConsoleBodyText(t, ctx, "Slack alert delivery failed")
	clickConsoleButtonByText(t, ctx, "Retry")
	waitForConsoleToastText(t, ctx, "Redriven 1 delivery")
	waitForConsoleBodyText(t, ctx, "All deliveries healthy")
	clickConsoleButtonByText(t, ctx, "Config & Integrations")
	waitForConsoleBodyText(t, ctx, "approval callback secret missing")
	waitForConsoleBodyText(t, ctx, "slack-approvals")
	clickConsoleButtonByText(t, ctx, "Audit Explorer")
	waitForConsoleBodyText(t, ctx, "Slack alert delivery failed")
	waitForConsoleBodyText(t, ctx, "ticket-1")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if redriveCount != 1 {
		t.Fatalf("redrive count = %d, want 1", redriveCount)
	}
}

func TestConsoleBrowserSettingsModelsCreateAndValidateFlow(t *testing.T) {
	var mu sync.Mutex
	models := []map[string]any{}
	lastCreate := consoleModelPayload{}
	lastValidate := consoleModelPayload{}
	lastTest := consoleModelPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{}})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers": []map[string]any{
					{
						"id":             "openai",
						"display_name":   "OpenAI",
						"api":            "openai-completions",
						"base_url":       "https://api.openai.com/v1",
						"default_models": []string{"gpt-4.1"},
						"env_vars":       []string{"OPENAI_API_KEY"},
					},
				},
				"channels": []any{},
				"provider_apis": []map[string]any{
					{
						"id": "openai-completions",
						"fields": []map[string]any{
							{"id": "base_url", "label": "Base URL", "type": "url"},
							{"id": "api_key", "label": "API Key", "type": "password", "required": true},
							{"id": "headers", "label": "Extra Headers", "type": "string_map", "advanced": true},
							{"id": "default_model", "label": "Default Model", "type": "text"},
						},
						"capability_matrix": map[string]any{"supports_tools": true},
					},
				},
			})
		case "/operator/extensions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{{"name": "browser", "health": "healthy"}},
				"channels":     []any{},
			})
		case "/operator/channels/health":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/capabilities":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/models":
			switch r.Method {
			case http.MethodGet:
				mu.Lock()
				providers := append([]map[string]any(nil), models...)
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"providers": providers})
			case http.MethodPost:
				var payload consoleModelPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode model create payload: %v", err)
				}
				item := map[string]any{
					"name":          payload.Name,
					"api":           payload.API,
					"base_url":      payload.BaseURL,
					"default_model": payload.DefaultModel,
					"has_key":       payload.APIKey != "",
					"mutable":       true,
				}
				mu.Lock()
				lastCreate = payload
				models = []map[string]any{item}
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusOK, item)
			default:
				http.NotFound(w, r)
			}
		case "/operator/models/validate":
			var payload consoleModelPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode model validate payload: %v", err)
			}
			mu.Lock()
			lastValidate = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"valid": true, "message": "Provider reachable"})
		case "/operator/models/test-chat":
			var payload consoleModelPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode model test payload: %v", err)
			}
			mu.Lock()
			lastTest = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": "pong", "latency_ms": 42, "tokens": 12})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/settings/models")
	waitForConsoleBodyText(t, ctx, "Model Configuration")
	clickConsoleButtonByText(t, ctx, "Add Provider")
	setConsoleFieldValueByLabel(t, ctx, "Name", "openai-prod")
	setConsoleFieldValueByLabel(t, ctx, "API Key", "sk-live")
	clickConsoleButtonByText(t, ctx, "Validate Provider")
	waitForConsoleBodyText(t, ctx, "Provider reachable")
	clickConsoleButtonByText(t, ctx, "Test Message")
	waitForConsoleBodyText(t, ctx, "pong")
	clickConsoleButtonByText(t, ctx, "Save")
	waitForConsoleToastText(t, ctx, "Settings saved")
	waitForConsoleBodyText(t, ctx, "openai-prod")
	waitForConsoleBodyText(t, ctx, "Key configured")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastCreate.Name != "openai-prod" || lastCreate.APIKey != "sk-live" || lastCreate.API != "openai-completions" {
		t.Fatalf("model create payload = %+v, want name=openai-prod api_key=sk-live api=openai-completions", lastCreate)
	}
	if lastValidate.APIKey != "sk-live" || lastTest.Message != "Hello" {
		t.Fatalf("validate/test payloads = %+v / %+v, want api_key=sk-live and message=Hello", lastValidate, lastTest)
	}
}

func TestConsoleBrowserSetupWizardValidateFlow(t *testing.T) {
	var mu sync.Mutex
	lastValidate := consoleModelPayload{}
	lastTest := consoleModelPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers": []map[string]any{
					{
						"id":             "openai",
						"display_name":   "OpenAI",
						"description":    "Ship fast with the default OpenAI stack.",
						"api":            "openai-completions",
						"base_url":       "https://api.openai.com/v1",
						"default_models": []string{"gpt-4.1"},
						"env_vars":       []string{"OPENAI_API_KEY"},
						"capability_matrix": map[string]any{
							"supports_tools":     true,
							"supports_streaming": true,
						},
					},
				},
				"provider_apis": []map[string]any{
					{
						"id": "openai-completions",
						"fields": []map[string]any{
							{"id": "base_url", "label": "Base URL", "type": "url"},
							{"id": "api_key", "label": "API Key", "type": "password", "required": true},
							{"id": "headers", "label": "Extra Headers", "type": "string_map", "advanced": true, "description": "Optional headers"},
							{"id": "default_model", "label": "Default Model", "type": "text"},
						},
					},
				},
			})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured":         false,
				"providers":          []any{},
				"detected_providers": []string{"openai"},
			})
		case "/operator/models/validate":
			var payload consoleModelPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode setup validate payload: %v", err)
			}
			mu.Lock()
			lastValidate = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"valid": true, "models": []string{"gpt-4.1", "gpt-4o"}})
		case "/operator/models/test-chat":
			var payload consoleModelPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode setup test payload: %v", err)
			}
			mu.Lock()
			lastTest = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": "Hello from OpenAI", "latency_ms": 33, "tokens": 9})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/setup")
	waitForConsoleBodyText(t, ctx, "Choose Your AI Provider")
	waitForConsoleBodyText(t, ctx, "OpenAI")
	clickConsoleButtonByText(t, ctx, "Next")
	waitForConsoleBodyText(t, ctx, "API Key")
	setConsoleFieldValueByLabel(t, ctx, "API Key", "sk-openai")
	clickConsoleButtonByText(t, ctx, "Validate Key")
	waitForConsoleToastText(t, ctx, "Provider validated successfully.")
	waitForConsoleBodyText(t, ctx, "Provider is valid. 2 models available.")
	waitForConsoleFieldValueByLabel(t, ctx, "Default Model", "gpt-4.1")
	clickConsoleButtonByText(t, ctx, "Send Test Message")
	waitForConsoleBodyText(t, ctx, "Hello from OpenAI")
	clickConsoleButtonByText(t, ctx, "Next")
	waitForConsoleBodyText(t, ctx, "You're All Set!")
	waitForConsoleBodyText(t, ctx, "gpt-4.1")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastValidate.Provider != "openai" || lastValidate.APIKey != "sk-openai" {
		t.Fatalf("setup validate payload = %+v, want provider=openai api_key=sk-openai", lastValidate)
	}
	if lastTest.Message != "Hello! Please respond with a brief greeting." {
		t.Fatalf("setup test payload = %+v, want greeting message", lastTest)
	}
}

func TestConsoleBrowserSetupWizardFinishPersistsProviderAndRedirectsToAssistant(t *testing.T) {
	var mu sync.Mutex
	lastCreate := consoleModelPayload{}
	lastAgentConfig := consoleAgentConfigPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/catalog":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"providers": []map[string]any{
					{
						"id":             "openai",
						"display_name":   "OpenAI",
						"description":    "Ship fast with the default OpenAI stack.",
						"api":            "openai-completions",
						"base_url":       "https://api.openai.com/v1",
						"default_models": []string{"gpt-4.1"},
						"env_vars":       []string{"OPENAI_API_KEY"},
						"capability_matrix": map[string]any{
							"supports_tools":     true,
							"supports_streaming": true,
						},
					},
				},
				"provider_apis": []map[string]any{
					{
						"id": "openai-completions",
						"fields": []map[string]any{
							{"id": "base_url", "label": "Base URL", "type": "url"},
							{"id": "api_key", "label": "API Key", "type": "password", "required": true},
							{"id": "default_model", "label": "Default Model", "type": "text"},
						},
					},
				},
			})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured":         false,
				"providers":          []any{},
				"detected_providers": []string{"openai"},
			})
		case "/operator/models/validate":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"valid": true, "models": []string{"gpt-4.1", "gpt-4o"}})
		case "/operator/models/test-chat":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": "Hello from OpenAI", "latency_ms": 33, "tokens": 9})
		case "/operator/models":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleModelPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode setup finish create payload: %v", err)
			}
			mu.Lock()
			lastCreate = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/operator/config/agent":
			if r.Method != http.MethodPut {
				http.NotFound(w, r)
				return
			}
			var payload consoleAgentConfigPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode setup finish agent payload: %v", err)
			}
			mu.Lock()
			lastAgentConfig = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/setup")
	waitForConsoleBodyText(t, ctx, "Choose Your AI Provider")
	clickConsoleButtonByText(t, ctx, "Next")
	waitForConsoleBodyText(t, ctx, "API Key")
	setConsoleFieldValueByLabel(t, ctx, "API Key", "sk-openai")
	clickConsoleButtonByText(t, ctx, "Validate Key")
	waitForConsoleBodyText(t, ctx, "Provider is valid. 2 models available.")
	clickConsoleButtonByText(t, ctx, "Send Test Message")
	waitForConsoleBodyText(t, ctx, "Hello from OpenAI")
	clickConsoleButtonByText(t, ctx, "Next")
	waitForConsoleBodyText(t, ctx, "You're All Set!")
	clickConsoleButtonByText(t, ctx, "Start Chatting")
	waitForConsoleHash(t, ctx, "#/assistant")
	waitForConsoleBodyText(t, ctx, "Task Workspace")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastCreate.Name != "openai" || lastCreate.Provider != "openai" || lastCreate.API != "openai-completions" {
		t.Fatalf("setup finish model payload = %+v, want name=openai provider=openai api=openai-completions", lastCreate)
	}
	if lastCreate.APIKey != "sk-openai" || lastCreate.DefaultModel != "gpt-4.1" || lastCreate.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("setup finish model payload = %+v, want api_key=sk-openai default_model=gpt-4.1 base_url=https://api.openai.com/v1", lastCreate)
	}
	if lastAgentConfig.DefaultModel != "openai/gpt-4.1" {
		t.Fatalf("setup finish agent payload = %+v, want default_model=openai/gpt-4.1", lastAgentConfig)
	}
}

func TestConsoleBrowserAssistantFirstMessageFlow(t *testing.T) {
	var mu sync.Mutex
	lastInteract := consoleInteractPayload{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/interact":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleInteractPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode interact payload: %v", err)
			}
			mu.Lock()
			lastInteract = payload
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"message": "Hello from HopClaw",
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/assistant")
	waitForConsoleBodyText(t, ctx, "Task Workspace")
	setConsoleFieldValueBySelector(t, ctx, "#hc-chat-input", "Hello from browser smoke")
	clickConsoleElementBySelector(t, ctx, ".hc-send-btn")
	waitForConsoleBodyText(t, ctx, "Hello from browser smoke")
	waitForConsoleBodyText(t, ctx, "Hello from HopClaw")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastInteract.SessionKey != "webchat" || lastInteract.Content != "Hello from browser smoke" {
		t.Fatalf("interact payload = %+v, want session_key=webchat content=Hello from browser smoke", lastInteract)
	}
}

func TestConsoleBrowserAssistantTaskAcceptRefreshesWorkspace(t *testing.T) {
	var mu sync.Mutex
	lastInteract := consoleInteractPayload{}
	submitted := false

	runRecord := map[string]any{
		"id":          "run-1234567890",
		"session_id":  "sess-1",
		"status":      "running",
		"created_at":  "2026-03-29T08:00:00Z",
		"started_at":  "2026-03-29T08:00:01Z",
		"updated_at":  "2026-03-29T08:00:02Z",
		"scope_label": "session",
	}
	sessionRecord := map[string]any{
		"id":         "sess-1",
		"key":        "webchat",
		"updated_at": "2026-03-29T08:00:02Z",
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/runtime/sessions":
			mu.Lock()
			items := []any{}
			if submitted {
				items = []any{sessionRecord}
			}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
		case "/runtime/interact":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleInteractPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task accept interact payload: %v", err)
			}
			mu.Lock()
			lastInteract = payload
			submitted = true
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"decision": map[string]any{
					"reply_act": "task_accept",
				},
				"run": runRecord,
			})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{runRecord}})
		case "/runtime/approvals":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/operator/artifacts":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		case "/runtime/runs/run-1234567890/completion":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"status": "running"})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/assistant")
	waitForConsoleBodyText(t, ctx, "No run data for this thread yet. Start a conversation to see status, plan, outputs, and verification here.")
	setConsoleFieldValueBySelector(t, ctx, "#hc-chat-input", "Launch a task run")
	clickConsoleElementBySelector(t, ctx, ".hc-send-btn")
	waitForConsoleBodyText(t, ctx, "Open run")
	waitForConsoleBodyText(t, ctx, "Run history")
	waitForConsoleBodyTextMissing(t, ctx, "No run data for this thread yet. Start a conversation to see status, plan, outputs, and verification here.")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if lastInteract.SessionKey != "webchat" || lastInteract.Content != "Launch a task run" {
		t.Fatalf("task accept interact payload = %+v, want session_key=webchat content=Launch a task run", lastInteract)
	}
}

func TestConsoleBrowserRunsRouteShowsExpandedRunDetail(t *testing.T) {
	runRecord := map[string]any{
		"id":                   "run-governed-123",
		"session_id":           "sess-1",
		"session_key":          "webchat",
		"status":               "completed",
		"verification_status":  "passed",
		"verification_summary": "All checks passed",
		"model":                "openai/gpt-4.1",
		"created_at":           "2026-03-29T08:00:00Z",
		"started_at":           "2026-03-29T08:00:01Z",
		"finished_at":          "2026-03-29T08:01:05Z",
		"tool_rounds":          2,
		"total_tokens":         3210,
	}
	runDetail := map[string]any{
		"id":          "run-governed-123",
		"session_id":  "sess-1",
		"session_key": "webchat",
		"status":      "completed",
		"created_at":  "2026-03-29T08:00:00Z",
		"started_at":  "2026-03-29T08:00:01Z",
		"finished_at": "2026-03-29T08:01:05Z",
		"governance": map[string]any{
			"summary": "Allowed after policy evaluation",
			"scope": map[string]any{
				"automation_id": "automation-governed",
			},
			"approval": map[string]any{
				"id":     "approval-1",
				"kind":   "human",
				"status": "approved",
				"external": []map[string]any{
					{"provider": "feishu", "external_id": "ext-1", "status": "approved", "url": "https://example.com/approval/ext-1"},
				},
			},
			"tool_names": []string{"browser.navigate", "browser.click"},
			"policy": map[string]any{
				"action":        "allow",
				"summary":       "Execution allowed",
				"policy_source": "operator/default",
				"reasons":       []string{"Session scope validated"},
				"reason_codes":  []string{"scope_ok"},
				"policy_layers": []string{"builtin", "operator"},
				"audit_labels":  []string{"prod-safe"},
			},
		},
		"governance_trace": map[string]any{
			"updated_at":                   "2026-03-29T08:00:03Z",
			"effective_config_snapshot_id": "snap-1",
			"decision": map[string]any{
				"action":        "allow",
				"summary":       "Execution allowed",
				"policy_source": "operator/default",
				"reasons":       []string{"Workspace scope validated"},
				"reason_codes":  []string{"scope_ok"},
				"policy_layers": []string{"builtin", "operator"},
				"audit_labels":  []string{"prod-safe"},
			},
		},
		"task_contract": map[string]any{
			"goal":                     "Capture governed browser evidence",
			"target_summary":           "Collect evidence from the target page",
			"job_type":                 "browser_task",
			"confidence":               0.92,
			"requires_external_effect": true,
			"requires_approval":        true,
			"suggested_domains":        []string{"browser", "evidence"},
			"expected_deliverables": []map[string]any{
				{"kind": "screenshot", "summary": "Annotated browser screenshot", "required": true},
			},
			"acceptance_criteria": []map[string]any{
				{"id": "evidence", "summary": "Provide screenshot evidence", "required": true, "deliverable_kinds": []string{"screenshot"}},
			},
		},
		"tool_calls": []map[string]any{
			{"name": "browser.navigate", "arguments": map[string]any{"url": "https://example.com"}},
		},
	}
	completion := map[string]any{
		"outcome": "completed",
		"result": map[string]any{
			"summary": "Collected browser evidence successfully",
		},
		"verification": map[string]any{
			"status":  "passed",
			"summary": "All checks passed",
			"checks": []map[string]any{
				{"name": "artifact-exists", "status": "passed", "summary": "Screenshot persisted"},
			},
		},
		"bundle": map[string]any{
			"outcome": "completed",
			"summary": "Evidence bundle ready",
			"deliverables": []map[string]any{
				{"name": "screenshot.png", "kind": "image", "content_type": "image/png", "size_bytes": 2048, "preview_text": "Primary screenshot"},
			},
			"suggested_actions": []map[string]any{
				{"kind": "review_result", "label": "Review result"},
			},
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"configured": true,
				"providers":  []string{"openai"},
			})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []any{
					map[string]any{"id": "sess-1", "key": "webchat", "updated_at": "2026-03-29T08:01:05Z"},
				},
			})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{runRecord}})
		case "/runtime/runs/run-governed-123":
			writeConsoleBrowserJSON(w, http.StatusOK, runDetail)
		case "/runtime/runs/run-governed-123/completion":
			writeConsoleBrowserJSON(w, http.StatusOK, completion)
		case "/operator/artifacts":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
				"items": []any{
					map[string]any{
						"id":           "artifact-1",
						"name":         "screenshot.png",
						"kind":         "image",
						"content_type": "image/png",
						"size":         2048,
						"preview_text": "Primary screenshot",
						"created_at":   "2026-03-29T08:01:04Z",
					},
				},
			})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/runs/run-governed-123")
	waitForConsoleHash(t, ctx, "#/runs/run-governed-123")
	waitForConsoleBodyText(t, ctx, "Governance")
	waitForConsoleBodyText(t, ctx, "Capture governed browser evidence")
	waitForConsoleBodyText(t, ctx, "Evidence bundle ready")
	waitForConsoleBodyText(t, ctx, "screenshot.png")
	waitForConsoleBodyText(t, ctx, "artifact-exists")
	assertConsoleNoPageErrors(t, ctx)
}

func TestConsoleBrowserRunsCancelFlow(t *testing.T) {
	var mu sync.Mutex
	cancelledRunID := ""
	runs := []map[string]any{
		{
			"id":          "run-live-1",
			"session_id":  "sess-1",
			"session_key": "webchat",
			"status":      "running",
			"model":       "openai/gpt-4.1",
			"created_at":  "2026-03-29T08:00:00Z",
			"started_at":  "2026-03-29T08:00:01Z",
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"id": "sess-1", "key": "webchat", "updated_at": "2026-03-29T08:00:01Z"},
			}})
		case "/runtime/runs":
			if r.Method == http.MethodPost {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			items := append([]map[string]any(nil), runs...)
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
		case "/runtime/runs/run-live-1/cancel":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			cancelledRunID = "run-live-1"
			runs[0]["status"] = "cancelled"
			runs[0]["finished_at"] = "2026-03-29T08:00:10Z"
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/runs")
	waitForConsoleBodyText(t, ctx, "run-live-1")
	clickConsoleButtonByText(t, ctx, "Cancel")
	waitForConsoleToastText(t, ctx, "Run cancelled")
	waitForConsoleBodyText(t, ctx, "Cancelled")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if cancelledRunID != "run-live-1" {
		t.Fatalf("cancelled run id = %q, want run-live-1", cancelledRunID)
	}
}

func TestConsoleBrowserRunsRetryFlow(t *testing.T) {
	var mu sync.Mutex
	retryPayload := consoleInteractPayload{}
	runs := []map[string]any{
		{
			"id":                  "run-failed-1",
			"session_id":          "sess-1",
			"session_key":         "webchat",
			"status":              "failed",
			"verification_status": "failed",
			"model":               "openai/gpt-4.1",
			"created_at":          "2026-03-29T08:00:00Z",
			"started_at":          "2026-03-29T08:00:01Z",
			"finished_at":         "2026-03-29T08:00:04Z",
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"id": "sess-1", "key": "webchat", "updated_at": "2026-03-29T08:00:04Z"},
			}})
		case "/runtime/interact":
			if r.Method == http.MethodPost {
				var payload consoleInteractPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode retry payload: %v", err)
				}
				mu.Lock()
				retryPayload = payload
				runs = []map[string]any{
					{
						"id":          "run-retry-2",
						"session_id":  "sess-1",
						"session_key": "webchat",
						"status":      "running",
						"model":       "openai/gpt-4.1",
						"created_at":  "2026-03-29T08:00:05Z",
						"started_at":  "2026-03-29T08:00:05Z",
					},
					runs[0],
				}
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusAccepted, map[string]any{
					"decision": map[string]any{"reply_act": "task_accept"},
					"run": map[string]any{
						"id":         "run-retry-2",
						"session_id": "sess-1",
						"status":     "running",
					},
				})
				return
			}
			http.NotFound(w, r)
			return
		case "/runtime/runs":
			mu.Lock()
			items := append([]map[string]any(nil), runs...)
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/runs")
	waitForConsoleBodyText(t, ctx, "run-failed-1")
	clickConsoleButtonByText(t, ctx, "Retry")
	waitForConsoleToastText(t, ctx, "New run started")
	waitForConsoleBodyText(t, ctx, "run-retry-2")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if retryPayload.SessionKey != "webchat" || retryPayload.ParentRunID != "run-failed-1" {
		t.Fatalf("retry payload = %+v, want session_key=webchat parent_run_id=run-failed-1", retryPayload)
	}
	if retryPayload.StructuredCommand == nil || retryPayload.StructuredCommand.Kind != "retry" || retryPayload.StructuredCommand.RunID != "run-failed-1" {
		t.Fatalf("retry payload structured command = %+v", retryPayload.StructuredCommand)
	}
}

func TestConsoleBrowserApprovalsCancelFlow(t *testing.T) {
	var mu sync.Mutex
	cancelledTicketID := ""
	pending := []map[string]any{
		{
			"id":          "ticket-cancel-1",
			"tool_name":   "deploy-prod-shell",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"cmd":"deploy --prod"}`,
			"reasons":     []string{"Production deployment requires approval"},
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/approvals":
			status := r.URL.Query().Get("status")
			mu.Lock()
			switch status {
			case "pending":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), pending...)})
			case "approved", "denied":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
			mu.Unlock()
		case "/operator/approvals/ticket-cancel-1/cancel":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			cancelledTicketID = "ticket-cancel-1"
			pending = []map[string]any{}
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/approvals")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	clickConsoleButtonByText(t, ctx, "Cancel")
	waitForConsoleToastText(t, ctx, "Cancelled")
	waitForConsoleBodyText(t, ctx, "No pending approvals")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if cancelledTicketID != "ticket-cancel-1" {
		t.Fatalf("cancelled ticket id = %q, want ticket-cancel-1", cancelledTicketID)
	}
}

func TestConsoleBrowserApprovalsBatchApproveFlow(t *testing.T) {
	var mu sync.Mutex
	resolveCalls := []consoleApprovalResolveRequest{}
	pending := []map[string]any{
		{
			"id":          "ticket-batch-a",
			"tool_name":   "deploy-prod-shell",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"cmd":"deploy --prod"}`,
			"reasons":     []string{"Production deployment requires approval"},
		},
		{
			"id":          "ticket-batch-b",
			"tool_name":   "push-config",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 1, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"target":"prod"}`,
			"reasons":     []string{"Configuration change requires approval"},
		},
	}
	approved := []map[string]any{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/approvals":
			status := r.URL.Query().Get("status")
			mu.Lock()
			switch status {
			case "pending":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), pending...)})
			case "approved":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), approved...)})
			case "denied":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
			mu.Unlock()
		case "/operator/approvals/ticket-batch-a/resolve", "/operator/approvals/ticket-batch-b/resolve":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleApprovalResolvePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch approve payload: %v", err)
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/operator/approvals/"), "/resolve")
			mu.Lock()
			resolveCalls = append(resolveCalls, consoleApprovalResolveRequest{ID: id, Payload: payload})
			nextPending := make([]map[string]any, 0, len(pending))
			for _, item := range pending {
				if stringValue(item["id"]) == id {
					approved = append(approved, map[string]any{
						"id":          id,
						"tool_name":   item["tool_name"],
						"resolution":  payload.Status,
						"scope":       payload.Scope,
						"resolved_by": payload.By,
						"session_key": item["session_key"],
						"resolved_at": time.Date(2026, 3, 27, 10, 2, 0, 0, time.UTC).Format(time.RFC3339),
					})
					continue
				}
				nextPending = append(nextPending, item)
			}
			pending = nextPending
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/approvals")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approvals input[type="checkbox"]`, 0)
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approval-card input[type="checkbox"]`, 0)
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approval-card input[type="checkbox"]`, 1)
	waitForConsoleBodyText(t, ctx, "2 selected")
	clickConsoleButtonByText(t, ctx, "Approve (2)")
	waitForConsoleToastText(t, ctx, "2 approved")
	waitForConsoleBodyText(t, ctx, "No pending approvals")
	clickConsoleButtonByText(t, ctx, "Resolved")
	waitForConsoleBodyText(t, ctx, "deploy-prod-shell")
	waitForConsoleBodyText(t, ctx, "push-config")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(resolveCalls) != 2 {
		t.Fatalf("batch approve resolve calls = %d, want 2", len(resolveCalls))
	}
	for _, call := range resolveCalls {
		if call.Payload.Status != "approved" || call.Payload.By != "operator" {
			t.Fatalf("batch approve resolve call = %+v, want approved by operator", call)
		}
	}
}

func TestConsoleBrowserApprovalsBatchDenyFlow(t *testing.T) {
	var mu sync.Mutex
	resolveCalls := []consoleApprovalResolveRequest{}
	pending := []map[string]any{
		{
			"id":          "ticket-deny-a",
			"tool_name":   "delete-bucket",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"bucket":"prod-logs"}`,
			"reasons":     []string{"Destructive change requires approval"},
		},
		{
			"id":          "ticket-deny-b",
			"tool_name":   "rotate-keys",
			"session_key": "webchat",
			"created_at":  time.Date(2026, 3, 27, 10, 1, 0, 0, time.UTC).Format(time.RFC3339),
			"arguments":   `{"scope":"prod"}`,
			"reasons":     []string{"Security-sensitive change requires approval"},
		},
	}
	denied := []map[string]any{}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/operator/approvals":
			status := r.URL.Query().Get("status")
			mu.Lock()
			switch status {
			case "pending":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), pending...)})
			case "approved":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			case "denied":
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": append([]map[string]any(nil), denied...)})
			default:
				writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			}
			mu.Unlock()
		case "/operator/approvals/ticket-deny-a/resolve", "/operator/approvals/ticket-deny-b/resolve":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var payload consoleApprovalResolvePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch deny payload: %v", err)
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/operator/approvals/"), "/resolve")
			mu.Lock()
			resolveCalls = append(resolveCalls, consoleApprovalResolveRequest{ID: id, Payload: payload})
			nextPending := make([]map[string]any, 0, len(pending))
			for _, item := range pending {
				if stringValue(item["id"]) == id {
					denied = append(denied, map[string]any{
						"id":          id,
						"tool_name":   item["tool_name"],
						"resolution":  payload.Status,
						"scope":       payload.Scope,
						"resolved_by": payload.By,
						"session_key": item["session_key"],
						"resolved_at": time.Date(2026, 3, 27, 10, 2, 0, 0, time.UTC).Format(time.RFC3339),
					})
					continue
				}
				nextPending = append(nextPending, item)
			}
			pending = nextPending
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/approvals")
	waitForConsoleBodyText(t, ctx, "delete-bucket")
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approvals input[type="checkbox"]`, 0)
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approval-card input[type="checkbox"]`, 0)
	clickConsoleElementBySelectorAt(t, ctx, `.hc-approval-card input[type="checkbox"]`, 1)
	waitForConsoleBodyText(t, ctx, "2 selected")
	clickConsoleButtonByText(t, ctx, "Deny (2)")
	waitForConsoleToastText(t, ctx, "2 denied")
	waitForConsoleBodyText(t, ctx, "No pending approvals")
	clickConsoleButtonByText(t, ctx, "Resolved")
	waitForConsoleBodyText(t, ctx, "delete-bucket")
	waitForConsoleBodyText(t, ctx, "rotate-keys")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(resolveCalls) != 2 {
		t.Fatalf("batch deny resolve calls = %d, want 2", len(resolveCalls))
	}
	for _, call := range resolveCalls {
		if call.Payload.Status != "denied" || call.Payload.By != "operator" {
			t.Fatalf("batch deny resolve call = %+v, want denied by operator", call)
		}
	}
}

func TestConsoleBrowserRunsBatchRetryFlow(t *testing.T) {
	var mu sync.Mutex
	retryCalls := []consoleInteractPayload{}
	runs := []map[string]any{
		{
			"id":                  "run-batch-failed-a",
			"session_id":          "sess-1",
			"session_key":         "webchat",
			"status":              "failed",
			"verification_status": "failed",
			"model":               "openai/gpt-4.1",
			"created_at":          "2026-03-29T08:00:00Z",
			"started_at":          "2026-03-29T08:00:01Z",
			"finished_at":         "2026-03-29T08:00:04Z",
		},
		{
			"id":                  "run-batch-failed-b",
			"session_id":          "sess-2",
			"session_key":         "ops-room",
			"status":              "failed",
			"verification_status": "failed",
			"model":               "openai/gpt-4.1",
			"created_at":          "2026-03-29T08:01:00Z",
			"started_at":          "2026-03-29T08:01:01Z",
			"finished_at":         "2026-03-29T08:01:04Z",
		},
		{
			"id":          "run-completed-c",
			"session_id":  "sess-3",
			"session_key": "design-room",
			"status":      "completed",
			"model":       "openai/gpt-4.1",
			"created_at":  "2026-03-29T08:02:00Z",
			"started_at":  "2026-03-29T08:02:01Z",
			"finished_at": "2026-03-29T08:02:10Z",
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"id": "sess-1", "key": "webchat", "updated_at": "2026-03-29T08:00:04Z"},
				map[string]any{"id": "sess-2", "key": "ops-room", "updated_at": "2026-03-29T08:01:04Z"},
				map[string]any{"id": "sess-3", "key": "design-room", "updated_at": "2026-03-29T08:02:10Z"},
			}})
		case "/runtime/interact":
			if r.Method == http.MethodPost {
				var payload consoleInteractPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode batch retry payload: %v", err)
				}
				mu.Lock()
				retryCalls = append(retryCalls, payload)
				mu.Unlock()
				writeConsoleBrowserJSON(w, http.StatusAccepted, map[string]any{
					"decision": map[string]any{"reply_act": "task_accept"},
					"run": map[string]any{
						"id":         "run-new-" + payload.ParentRunID,
						"session_id": "sess-new",
						"status":     "running",
					},
				})
				return
			}
			http.NotFound(w, r)
			return
		case "/runtime/runs":
			mu.Lock()
			items := append([]map[string]any(nil), runs...)
			mu.Unlock()
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": items})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/runs")
	waitForConsoleBodyText(t, ctx, "ops-room")
	clickConsoleElementBySelectorAt(t, ctx, `.hc-runs-table-wrap thead input[type="checkbox"]`, 0)
	waitForConsoleBodyText(t, ctx, "3 selected")
	clickConsoleButtonByText(t, ctx, "Retry selected")
	waitForConsoleToastText(t, ctx, "2 runs retried")
	assertConsoleNoPageErrors(t, ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(retryCalls) != 2 {
		t.Fatalf("batch retry calls = %d, want 2", len(retryCalls))
	}
	got := map[string]string{}
	gotCommand := map[string]consoleStructuredCommandPayload{}
	for _, call := range retryCalls {
		got[call.SessionKey] = call.ParentRunID
		if call.StructuredCommand != nil {
			gotCommand[call.SessionKey] = *call.StructuredCommand
		}
	}
	if got["webchat"] != "run-batch-failed-a" || got["ops-room"] != "run-batch-failed-b" {
		t.Fatalf("batch retry payloads = %+v, want run ids for webchat and ops-room", retryCalls)
	}
	if gotCommand["webchat"].Kind != "retry" || gotCommand["webchat"].RunID != "run-batch-failed-a" {
		t.Fatalf("webchat retry command = %+v", gotCommand["webchat"])
	}
	if gotCommand["ops-room"].Kind != "retry" || gotCommand["ops-room"].RunID != "run-batch-failed-b" {
		t.Fatalf("ops-room retry command = %+v", gotCommand["ops-room"])
	}
}

func TestConsoleBrowserRunsExportAllFlow(t *testing.T) {
	runs := []map[string]any{
		{
			"id":          "run-export-a",
			"session_id":  "sess-1",
			"session_key": "webchat",
			"status":      "completed",
			"model":       "openai/gpt-4.1",
			"created_at":  "2026-03-29T08:00:00Z",
			"started_at":  "2026-03-29T08:00:01Z",
			"finished_at": "2026-03-29T08:00:09Z",
		},
		{
			"id":          "run-export-b",
			"session_id":  "sess-2",
			"session_key": "ops-room",
			"status":      "failed",
			"model":       "openai/gpt-4.1",
			"created_at":  "2026-03-29T08:01:00Z",
			"started_at":  "2026-03-29T08:01:01Z",
			"finished_at": "2026-03-29T08:01:04Z",
		},
	}

	server := newConsoleBrowserSmokeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "uptime": "1m"})
		case "/operator/setup/status":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"configured": true, "providers": []string{"openai"}})
		case "/runtime/sessions":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": []any{
				map[string]any{"id": "sess-1", "key": "webchat", "updated_at": "2026-03-29T08:00:09Z"},
				map[string]any{"id": "sess-2", "key": "ops-room", "updated_at": "2026-03-29T08:01:04Z"},
			}})
		case "/runtime/runs":
			writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{"items": runs})
		case "/runtime/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	ctx, cancel := newConsoleBrowserContext(t)
	defer cancel()

	openConsoleBrowserPage(t, ctx, server.URL+"/dashboard/#/runs")
	waitForConsoleBodyText(t, ctx, "ops-room")
	armConsoleDownloadCapture(t, ctx)
	clickConsoleButtonByText(t, ctx, "Export")
	downloaded := waitForConsoleDownload(t, ctx, "runs-export.json")

	var payload []map[string]any
	if err := json.Unmarshal([]byte(downloaded), &payload); err != nil {
		t.Fatalf("decode downloaded runs export: %v\npayload=%s", err, downloaded)
	}
	if len(payload) != 2 {
		t.Fatalf("downloaded runs export len = %d, want 2; payload=%s", len(payload), downloaded)
	}
	assertConsoleNoPageErrors(t, ctx)
}

func newConsoleBrowserSmokeServer(t *testing.T, apiHandler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	if testing.CoverMode() != "" {
		t.Skip("skipping console browser smoke tests under coverage")
	}

	gw := newTestGatewayFull(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /dashboard/api/config", func(w http.ResponseWriter, r *http.Request) {
		writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
			"session_key": "webchat",
			"lang":        "en",
			"locale":      "en",
		})
	})
	mux.HandleFunc("GET /dashboard/api/i18n", func(w http.ResponseWriter, r *http.Request) {
		writeConsoleBrowserJSON(w, http.StatusOK, map[string]any{
			"lang":     "en",
			"locale":   "en",
			"messages": map[string]any{"common.yes": "Yes"},
		})
	})
	mux.HandleFunc("GET /dashboard/sse", apiHandler)
	mux.HandleFunc("GET /webchat/sse", apiHandler)
	mux.Handle("GET /dashboard/", http.StripPrefix("/dashboard/", gw.consoleUIHandler()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/operator/") || strings.HasPrefix(r.URL.Path, "/runtime/") {
			apiHandler(w, r)
			return
		}
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

func writeConsoleBrowserJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func newConsoleBrowserContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	browserPath := detectConsoleBrowserPath()
	if browserPath == "" {
		t.Skip("skipping console browser smoke tests: no Chrome/Chromium binary found")
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("mute-audio", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	timeoutCtx, timeoutCancel := context.WithTimeout(browserCtx, 25*time.Second)

	cancel := func() {
		timeoutCancel()
		browserCancel()
		allocCancel()
	}
	return timeoutCtx, cancel
}

func detectConsoleBrowserPath() string {
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func openConsoleBrowserPage(t *testing.T, ctx context.Context, url string) {
	t.Helper()

	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
				try {
					localStorage.setItem('hc_lang', 'en');
					localStorage.setItem('hc_auth_token', 'test-token');
					localStorage.setItem('hc_session_key', 'webchat');
					window.__hcErrors = [];
					window.addEventListener('error', (event) => {
						try {
							window.__hcErrors.push(String((event.error && event.error.stack) || event.message || 'error'));
						} catch (_) {}
					});
					window.addEventListener('unhandledrejection', (event) => {
						try {
							window.__hcErrors.push(String((event.reason && event.reason.stack) || event.reason || 'unhandledrejection'));
						} catch (_) {}
					});
					window.confirm = () => true;
					window.Notification = {
						permission: 'denied',
						requestPermission: () => Promise.resolve('denied')
					};
				} catch (_) {}
			`).Do(ctx)
			return err
		}),
		chromedp.EmulateViewport(1440, 1100),
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open console page %q: %v", url, err)
	}
}

func waitForConsoleBodyText(t *testing.T, ctx context.Context, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body := consoleBodyText(t, ctx)
		if strings.Contains(body, want) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console body did not contain %q within timeout.\nBody:\n%s\nPage errors:\n%s", want, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func waitForConsoleBodyTextMissing(t *testing.T, ctx context.Context, unwanted string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body := consoleBodyText(t, ctx)
		if !strings.Contains(body, unwanted) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console body still contained %q within timeout.\nBody:\n%s", unwanted, consoleBodyText(t, ctx))
}

func waitForConsoleTestID(t *testing.T, ctx context.Context, testID string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if consoleHasTestID(t, ctx, testID) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console testid %q did not appear within timeout.\nBody:\n%s\nPage errors:\n%s", testID, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func consoleHasTestID(t *testing.T, ctx context.Context, testID string) bool {
	t.Helper()

	var found bool
	script := fmt.Sprintf(`(() => {
		const testID = %s;
		return Array.from(document.querySelectorAll('[data-testid]')).some((el) => String(el.getAttribute('data-testid') || '') === testID);
	})()`, mustConsoleJSString(testID))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &found)); err != nil {
		t.Fatalf("lookup testid %q: %v", testID, err)
	}
	return found
}

func waitForConsoleTestIDAttr(t *testing.T, ctx context.Context, testID string, attr string, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := consoleTestIDAttr(t, ctx, testID, attr); got == want {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("testid %q attr %q did not reach %q within timeout; got %q\nBody:\n%s\nPage errors:\n%s", testID, attr, want, consoleTestIDAttr(t, ctx, testID, attr), consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func consoleTestIDAttr(t *testing.T, ctx context.Context, testID string, attr string) string {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const testID = %s;
		const attr = %s;
		const node = Array.from(document.querySelectorAll('[data-testid]')).find((el) => String(el.getAttribute('data-testid') || '') === testID);
		return node ? String(node.getAttribute(attr) || '') : '';
	})()`, mustConsoleJSString(testID), mustConsoleJSString(attr))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("read testid attr %q[%q]: %v", testID, attr, err)
	}
	return result
}

func clickConsoleElementByTestID(t *testing.T, ctx context.Context, testID string) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const testID = %s;
		const node = Array.from(document.querySelectorAll('[data-testid]')).find((el) => String(el.getAttribute('data-testid') || '') === testID);
		if (!node) return 'element not found: ' + testID;
		node.click();
		return '';
	})()`, mustConsoleJSString(testID))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("click testid %q: %v", testID, err)
	}
	if result != "" {
		t.Fatalf("click testid %q: %s\nBody:\n%s\nPage errors:\n%s", testID, result, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
	}
}

func waitForConsoleStoreLang(t *testing.T, ctx context.Context, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := consoleStoreLang(t, ctx); got == want {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console store lang did not reach %q within timeout; got %q\nBody:\n%s\nPage errors:\n%s", want, consoleStoreLang(t, ctx), consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func consoleStoreLang(t *testing.T, ctx context.Context) string {
	t.Helper()

	var lang string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window._hcStore ? String(window._hcStore.lang || "") : ""`, &lang)); err != nil {
		t.Fatalf("read store lang: %v", err)
	}
	return lang
}

func waitForConsoleHash(t *testing.T, ctx context.Context, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := consoleLocationHash(t, ctx); got == want {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console location hash did not reach %q within timeout; got %q\nBody:\n%s\nPage errors:\n%s", want, consoleLocationHash(t, ctx), consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func consoleBodyText(t *testing.T, ctx context.Context) string {
	t.Helper()

	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &body)); err != nil {
		t.Fatalf("read console body: %v", err)
	}
	return body
}

func consoleLocationHash(t *testing.T, ctx context.Context) string {
	t.Helper()

	var hash string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location ? window.location.hash : ""`, &hash)); err != nil {
		t.Fatalf("read console location hash: %v", err)
	}
	return hash
}

func clickConsoleButtonByText(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	clickConsoleButtonByTextMode(t, ctx, text, false)
}

func clickConsoleButtonByTextLast(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	clickConsoleButtonByTextMode(t, ctx, text, true)
}

func clickConsoleButtonByTextMode(t *testing.T, ctx context.Context, text string, last bool) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const target = %s;
		const pickLast = %t;
		const norm = (value) => String(value || '').replace(/\s+/g, ' ').trim();
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return false;
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		};
		const candidates = Array.from(document.querySelectorAll('button,a')).filter(visible);
		const exact = candidates.filter((el) => norm(el.innerText || el.textContent) === target);
		const partial = candidates.filter((el) => norm(el.innerText || el.textContent).includes(target));
		const matches = exact.length ? exact : partial;
		const node = matches.length ? matches[pickLast ? matches.length - 1 : 0] : null;
		if (!node) return 'button not found: ' + target;
		node.click();
		return '';
	})()`, mustConsoleJSString(text), last)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("click button %q: %v", text, err)
	}
	if result != "" {
		t.Fatalf("click button %q: %s\nBody:\n%s", text, result, consoleBodyText(t, ctx))
	}
}

func setConsoleFieldValueByLabel(t *testing.T, ctx context.Context, label string, value string) {
	t.Helper()
	setConsoleFieldByLabel(t, ctx, label, value, false)
}

func setConsoleFieldValueBySelector(t *testing.T, ctx context.Context, selector string, value string) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const selector = %s;
		const value = %s;
		const field = document.querySelector(selector);
		if (!field) return 'field not found: ' + selector;
		field.focus();
		field.value = value;
		field.dispatchEvent(new Event('input', { bubbles: true }));
		field.dispatchEvent(new Event('change', { bubbles: true }));
		return '';
	})()`, mustConsoleJSString(selector), mustConsoleJSString(value))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("set field by selector %q: %v", selector, err)
	}
	if result != "" {
		t.Fatalf("set field by selector %q: %s\nBody:\n%s\nPage errors:\n%s", selector, result, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
	}
}

func selectConsoleFieldValueByLabel(t *testing.T, ctx context.Context, label string, value string) {
	t.Helper()
	setConsoleFieldByLabel(t, ctx, label, value, true)
}

func clickConsoleElementBySelector(t *testing.T, ctx context.Context, selector string) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const selector = %s;
		const node = document.querySelector(selector);
		if (!node) return 'element not found: ' + selector;
		node.click();
		return '';
	})()`, mustConsoleJSString(selector))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("click selector %q: %v", selector, err)
	}
	if result != "" {
		t.Fatalf("click selector %q: %s\nBody:\n%s\nPage errors:\n%s", selector, result, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
	}
}

func clickConsoleElementBySelectorAt(t *testing.T, ctx context.Context, selector string, index int) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const selector = %s;
		const index = %d;
		const nodes = Array.from(document.querySelectorAll(selector));
		const node = nodes[index];
		if (!node) return 'element not found at index ' + index + ': ' + selector;
		node.click();
		return '';
	})()`, mustConsoleJSString(selector), index)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("click selector %q at %d: %v", selector, index, err)
	}
	if result != "" {
		t.Fatalf("click selector %q at %d: %s\nBody:\n%s\nPage errors:\n%s", selector, index, result, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
	}
}

func armConsoleDownloadCapture(t *testing.T, ctx context.Context) {
	t.Helper()

	var result string
	script := `(() => {
		try {
			if (window.__hcDownloadsArmed) return '';
			window.__hcDownloadsArmed = true;
			window.__hcDownloads = [];
			window.__hcDownloadBlobURLs = new Map();
			let seq = 0;
			const origCreate = URL.createObjectURL ? URL.createObjectURL.bind(URL) : null;
			const origRevoke = URL.revokeObjectURL ? URL.revokeObjectURL.bind(URL) : null;
			URL.createObjectURL = (blob) => {
				const id = 'blob:hc-download-' + (++seq);
				window.__hcDownloadBlobURLs.set(id, blob);
				return id;
			};
			URL.revokeObjectURL = (url) => {
				try { window.__hcDownloadBlobURLs.delete(url); } catch (_) {}
				if (origRevoke) {
					try { origRevoke(url); } catch (_) {}
				}
			};
			const origClick = HTMLAnchorElement.prototype.click;
			HTMLAnchorElement.prototype.click = function(...args) {
				try {
					const href = String(this.href || '');
					const name = String(this.download || '');
					if (window.__hcDownloadBlobURLs && window.__hcDownloadBlobURLs.has(href)) {
						const blob = window.__hcDownloadBlobURLs.get(href);
						Promise.resolve(blob && typeof blob.text === 'function' ? blob.text() : '')
							.then((text) => {
								window.__hcDownloads.push({ name, href, text: String(text || '') });
							})
							.catch(() => {
								window.__hcDownloads.push({ name, href, text: '' });
							});
						return;
					}
				} catch (_) {}
				if (origClick) return origClick.apply(this, args);
			};
			return '';
		} catch (err) {
			return String(err && err.message || err);
		}
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("arm download capture: %v", err)
	}
	if result != "" {
		t.Fatalf("arm download capture: %s", result)
	}
}

func waitForConsoleDownload(t *testing.T, ctx context.Context, filename string) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if text := consoleDownloadedFileText(t, ctx, filename); text != "" {
			return text
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console download %q was not captured within timeout", filename)
	return ""
}

func consoleDownloadedFileText(t *testing.T, ctx context.Context, filename string) string {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const name = %s;
		const items = Array.isArray(window.__hcDownloads) ? window.__hcDownloads : [];
		const match = items.find((item) => item && item.name === name);
		return match ? String(match.text || '') : '';
	})()`, mustConsoleJSString(filename))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("read downloaded file %q: %v", filename, err)
	}
	return result
}

func setConsoleFieldByLabel(t *testing.T, ctx context.Context, label string, value string, selectOnly bool) {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const labelText = %s;
		const value = %s;
		const selectOnly = %t;
		const norm = (text) => String(text || '').replace(/\s+/g, ' ').trim();
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return false;
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		};
		const labels = Array.from(document.querySelectorAll('label'));
		const visibleLabels = labels.filter(visible);
		const pick = (items) => items.find((el) => norm(el.innerText || el.textContent) === labelText)
			|| items.find((el) => norm(el.innerText || el.textContent).includes(labelText));
		const matched = pick(visibleLabels) || pick(labels);
		if (!matched) return 'label not found: ' + labelText;
		let field = matched.control || null;
		let scope = matched.parentElement;
		while (!field && scope) {
			field = scope.querySelector(selectOnly ? 'select' : 'input, textarea, select');
			scope = field ? null : scope.parentElement;
		}
		if (!field) return 'field not found for label: ' + labelText;
		if (selectOnly && field.tagName !== 'SELECT') return 'field is not a select for label: ' + labelText;
		field.focus();
		field.value = value;
		field.dispatchEvent(new Event('input', { bubbles: true }));
		field.dispatchEvent(new Event('change', { bubbles: true }));
		return '';
	})()`, mustConsoleJSString(label), mustConsoleJSString(value), selectOnly)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("set field %q: %v", label, err)
	}
	if result != "" {
		t.Fatalf("set field %q: %s\nBody:\n%s\nPage errors:\n%s", label, result, consoleBodyText(t, ctx), consolePageErrors(t, ctx))
	}
}

func waitForConsoleFieldValueByLabel(t *testing.T, ctx context.Context, label string, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := consoleFieldValueByLabel(t, ctx, label); got == want {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("field %q did not reach value %q within timeout; got %q", label, want, consoleFieldValueByLabel(t, ctx, label))
}

func consoleFieldValueByLabel(t *testing.T, ctx context.Context, label string) string {
	t.Helper()

	var result string
	script := fmt.Sprintf(`(() => {
		const labelText = %s;
		const norm = (text) => String(text || '').replace(/\s+/g, ' ').trim();
		const visible = (el) => {
			if (!el) return false;
			const style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return false;
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		};
		const labels = Array.from(document.querySelectorAll('label'));
		const visibleLabels = labels.filter(visible);
		const pick = (items) => items.find((el) => norm(el.innerText || el.textContent) === labelText)
			|| items.find((el) => norm(el.innerText || el.textContent).includes(labelText));
		const matched = pick(visibleLabels) || pick(labels);
		if (!matched) return '';
		let field = matched.control || null;
		let scope = matched.parentElement;
		while (!field && scope) {
			field = scope.querySelector('input, textarea, select');
			scope = field ? null : scope.parentElement;
		}
		return field ? String(field.value || '') : '';
	})()`, mustConsoleJSString(label))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		t.Fatalf("read field %q: %v", label, err)
	}
	return result
}

func waitForConsoleToastText(t *testing.T, ctx context.Context, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(consoleToastText(t, ctx), want) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("console toast did not contain %q within timeout.\nToasts:\n%s\nBody:\n%s", want, consoleToastText(t, ctx), consoleBodyText(t, ctx))
}

func consoleToastText(t *testing.T, ctx context.Context) string {
	t.Helper()

	var text string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll(".hc-toast-msg")).map((el) => el.innerText || el.textContent || "").join("\n")`, &text)); err != nil {
		t.Fatalf("read toast text: %v", err)
	}
	return text
}

func waitForConsoleCount(t *testing.T, ctx context.Context, selector string, want int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := consoleCount(t, ctx, selector); got == want {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("selector %q count did not reach %d within timeout; got %d\nBody:\n%s\nPage errors:\n%s", selector, want, consoleCount(t, ctx, selector), consoleBodyText(t, ctx), consolePageErrors(t, ctx))
}

func consoleCount(t *testing.T, ctx context.Context, selector string) int {
	t.Helper()

	var count int
	script := fmt.Sprintf(`document.querySelectorAll(%s).length`, mustConsoleJSString(selector))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &count)); err != nil {
		t.Fatalf("read selector count %q: %v", selector, err)
	}
	return count
}

func assertConsoleNoToast(t *testing.T, ctx context.Context) {
	t.Helper()

	var count int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll(".hc-toast").length`, &count)); err != nil {
		t.Fatalf("read toast count: %v", err)
	}
	if count != 0 {
		t.Fatalf("toast count = %d, want 0", count)
	}
}

func assertConsoleNoPageErrors(t *testing.T, ctx context.Context) {
	t.Helper()

	if errors := consolePageErrors(t, ctx); strings.TrimSpace(errors) != "" {
		t.Fatalf("page errors present:\n%s\nBody:\n%s", errors, consoleBodyText(t, ctx))
	}
}

func consolePageErrors(t *testing.T, ctx context.Context) string {
	t.Helper()

	var errors string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.isArray(window.__hcErrors) ? window.__hcErrors.join("\n") : ""`, &errors)); err != nil {
		t.Fatalf("read page errors: %v", err)
	}
	return errors
}

func mustConsoleJSString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

type consoleCronConfigPayload struct {
	Enabled bool `json:"enabled"`
}

type consoleModelPayload struct {
	Name            string            `json:"name"`
	Provider        string            `json:"provider"`
	CatalogProvider string            `json:"catalog_provider"`
	API             string            `json:"api"`
	APIKey          string            `json:"api_key"`
	BaseURL         string            `json:"base_url"`
	DefaultModel    string            `json:"default_model"`
	Message         string            `json:"message"`
	Headers         map[string]string `json:"headers"`
}

type consoleAgentConfigPayload struct {
	DefaultModel string `json:"default_model"`
}

type consoleInteractPayload struct {
	SessionKey        string                           `json:"session_key"`
	Content           string                           `json:"content"`
	ParentRunID       string                           `json:"parent_run_id,omitempty"`
	StructuredCommand *consoleStructuredCommandPayload `json:"structured_command,omitempty"`
}

type consoleStructuredCommandPayload struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id,omitempty"`
}

type consoleApprovalResolveRequest struct {
	ID      string
	Payload consoleApprovalResolvePayload
}

type consoleCronCreatePayload struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Session  string `json:"session_key"`
	Model    string `json:"model"`
	Schedule struct {
		Kind       string `json:"kind"`
		Expression string `json:"expression"`
	} `json:"schedule"`
	Payload struct {
		Content string `json:"content"`
	} `json:"payload"`
}

type consoleApprovalResolvePayload struct {
	Status string `json:"status"`
	Scope  string `json:"scope"`
	By     string `json:"by"`
}

type consoleSecurityPayload struct {
	ExecApproval struct {
		Mode            string `json:"mode"`
		ApprovalTimeout int    `json:"approval_timeout"`
		GracePeriod     int    `json:"grace_period"`
	} `json:"exec_approval"`
}

type consoleChannelCreatePayload struct {
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
}

type consoleMemoryRecordPayload struct {
	Namespace  string  `json:"namespace"`
	ScopeKey   string  `json:"scope_key"`
	Field      string  `json:"field"`
	Label      string  `json:"label"`
	Value      string  `json:"value"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

type consoleSourcePayload struct {
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	Enabled      bool           `json:"enabled"`
	Path         string         `json:"path"`
	URLs         []string       `json:"urls"`
	Config       map[string]any `json:"config"`
	IncludeGlobs []string       `json:"include_globs"`
	ExcludeGlobs []string       `json:"exclude_globs"`
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
