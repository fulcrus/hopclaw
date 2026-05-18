package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// handleWebChat
// ---------------------------------------------------------------------------

func TestDashboardServesHTML(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	// Static SPA is served at /dashboard/ (trailing slash).
	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: status = %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="shell-lang-toggle"`) {
		t.Fatal("dashboard html missing shell lang toggle testid")
	}
}

func TestDashboardDefaultSessionKey(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	// The SPA is now a static file; session key is managed in JS, not server-side template.
	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `js/app.js`) {
		t.Fatal("dashboard html missing app entry point")
	}
}

func TestWebChatConfigAPI(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/api/config?session=custom-sess", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("webchat config: status = %d", rec.Code)
	}
	var payload webChatConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if payload.SessionKey != "custom-sess" {
		t.Fatalf("SessionKey = %q, want custom-sess", payload.SessionKey)
	}
}

func TestWebChatConfigAPIToken(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/api/config", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("webchat config: status = %d", rec.Code)
	}
	var payload webChatConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if payload.SessionKey == "" {
		t.Fatal("SessionKey should not be empty")
	}
}

func TestWebChatConfigAPINormalizesLocale(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/api/config?lang=zh-CN", "")
	rec := captureResponse(t, handler, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webchat config: status = %d", rec.Code)
	}

	var payload webChatConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if payload.Lang != "zh" {
		t.Fatalf("Lang = %q, want zh", payload.Lang)
	}
	if payload.Locale != "zh-CN" {
		t.Fatalf("Locale = %q, want zh-CN", payload.Locale)
	}
}

func TestWebChatCatalogAPI(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/api/i18n?lang=zh-CN", "")
	rec := captureResponse(t, handler, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webchat catalog: status = %d", rec.Code)
	}

	var payload webChatCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if payload.Lang != "zh" || payload.Locale != "zh-CN" {
		t.Fatalf("unexpected locale payload: %#v", payload)
	}
	if strings.TrimSpace(payload.Messages["common.yes"]) == "" {
		t.Fatalf("expected common.yes in catalog payload: %#v", payload.Messages)
	}
}

func TestDashboardDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	// No auth header — should still serve dashboard (static SPA).
	req := makeUnauthRequest(t, http.MethodGet, "/dashboard/", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard without auth: status = %d", rec.Code)
	}
}
