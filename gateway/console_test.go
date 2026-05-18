package gateway

import (
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// handleConsoleRedirect
// ---------------------------------------------------------------------------

func TestConsoleRedirectsToDashboard(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET /: status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if loc != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", loc)
	}
}

func TestConsoleRedirectPreservesQueryParams(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	req := makeUnauthRequest(t, http.MethodGet, "/?token=abc&foo=bar", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET /?...: status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if loc != "/dashboard/?token=abc&foo=bar" {
		t.Fatalf("Location = %q, want /dashboard/?token=abc&foo=bar", loc)
	}
}

func TestConsoleRedirectNoTrailingSlash(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	// The root handler is registered with /{$}, so only exact "/" matches.
	req := makeUnauthRequest(t, http.MethodGet, "/", "")
	rec := captureResponse(t, handler, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("redirect: status = %d, want %d", rec.Code, http.StatusFound)
	}
}
