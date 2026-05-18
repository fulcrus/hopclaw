package gateway

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// SecurityHeaders
// ---------------------------------------------------------------------------

func TestSecurityHeadersAdded(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "0",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for hdr, expected := range want {
		got := rec.Header().Get(hdr)
		if got != expected {
			t.Fatalf("%s = %q, want %q", hdr, got, expected)
		}
	}

	// CSP should be set.
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header not set")
	}
}

func TestSecurityHeadersHSTSOnTLS(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{} // simulate TLS connection
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Fatal("Strict-Transport-Security not set on TLS request")
	}
	if !strings.Contains(hsts, "max-age=") {
		t.Fatalf("HSTS header missing max-age: %q", hsts)
	}
}

func TestSecurityHeadersNoHSTSOnPlainHTTP(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeaders(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("Strict-Transport-Security should not be set on plain HTTP")
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

func TestCORSPreflightAllowedOrigin(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cors := CORS(CORSConfig{AllowedOrigins: []string{"https://example.com"}})
	handler := cors(inner)

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("Allow-Origin = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("Allow-Methods not set on OPTIONS")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("Allow-Headers not set on OPTIONS")
	}
	if rec.Header().Get("Access-Control-Max-Age") == "" {
		t.Fatal("Max-Age not set on OPTIONS")
	}
}

func TestCORSDisallowedOriginNoHeader(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cors := CORS(CORSConfig{AllowedOrigins: []string{"https://allowed.com"}})
	handler := cors(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("Allow-Origin should be empty for disallowed origin, got %q",
			rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSWildcardAllowsAnyOrigin(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cors := CORS(CORSConfig{AllowedOrigins: []string{"*"}})
	handler := cors(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://any-origin.io")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://any-origin.io" {
		t.Fatalf("Allow-Origin = %q, want https://any-origin.io",
			rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSNoOriginHeaderNoAllowOrigin(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cors := CORS(CORSConfig{AllowedOrigins: []string{"*"}})
	handler := cors(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Without Origin request header, no Allow-Origin should be echoed.
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected Allow-Origin = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSDefaultMethodsAndHeaders(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Zero-value config — defaults should be applied.
	cors := CORS(CORSConfig{AllowedOrigins: []string{"*"}})
	handler := cors(inner)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://test.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
		t.Fatalf("default methods missing expected verbs: %q", methods)
	}
	headers := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Authorization") {
		t.Fatalf("default headers missing Authorization: %q", headers)
	}
	if !strings.Contains(headers, "X-OpenClaw-Token") {
		t.Fatalf("default headers missing X-OpenClaw-Token: %q", headers)
	}
}

func TestWebSocketOriginAllowedAllowsSameHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://hopclaw.example/operator/ws", nil)
	req.Host = "hopclaw.example"
	req.Header.Set("Origin", "https://hopclaw.example")

	if !websocketOriginAllowed(req, nil) {
		t.Fatal("same-host websocket origin should be allowed")
	}
}

func TestWebSocketOriginAllowedRejectsCrossHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://hopclaw.example/operator/ws", nil)
	req.Host = "hopclaw.example"
	req.Header.Set("Origin", "https://evil.example")

	if websocketOriginAllowed(req, nil) {
		t.Fatal("cross-host websocket origin should be rejected")
	}
}

func TestWebSocketOriginAllowedHonorsExplicitAllowlist(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://hopclaw.example/operator/ws", nil)
	req.Host = "hopclaw.example"
	req.Header.Set("Origin", "https://console.example")

	if !websocketOriginAllowed(req, []string{"https://console.example"}) {
		t.Fatal("explicitly allowed websocket origin should be accepted")
	}
}

// ---------------------------------------------------------------------------
// RequestID
// ---------------------------------------------------------------------------

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("X-Request-ID header not set")
	}
	if !strings.HasPrefix(id, "req-") {
		t.Fatalf("X-Request-ID = %q, want prefix req-", id)
	}
}

func TestRequestIDAlsoSetsTraceID(t *testing.T) {
	var traceID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID = logging.TraceIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-fixed")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if traceID != "req-fixed" {
		t.Fatalf("traceID = %q, want %q", traceID, "req-fixed")
	}
}

func TestMetricsMiddlewareStatusCapture(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	rec := httptest.NewRecorder()
	MetricsMiddleware(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

func TestRequestIDPreservedWhenPresent(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "custom-id-123" {
		t.Fatalf("X-Request-ID = %q, want custom-id-123", rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDUniqueness(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(inner)

	ids := make(map[string]bool)
	const iterations = 50
	for range iterations {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-ID")
		if ids[id] {
			t.Fatalf("duplicate request ID: %s", id)
		}
		ids[id] = true
	}
}

// ---------------------------------------------------------------------------
// Rate Limiter
// ---------------------------------------------------------------------------

func TestRateLimitAllowsBurstRequests(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	const burst = 5
	limiter := RateLimit(RateLimitConfig{RequestsPerSecond: 1, BurstSize: burst})
	handler := limiter(inner)

	for i := range burst {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}

func TestRateLimitRejectsAfterBurst(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	const burst = 3
	limiter := RateLimit(RateLimitConfig{RequestsPerSecond: 0.001, BurstSize: burst})
	handler := limiter(inner)

	// Drain the burst.
	for range burst {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Next request should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after burst: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimitReturnsJSONError(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	limiter := RateLimit(RateLimitConfig{RequestsPerSecond: 0.001, BurstSize: 1})
	handler := limiter(inner)

	// Drain the single token.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request should return a JSON error.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	payload := decodeGatewayErrorPayload(t, rec)
	if payload.Code != string(apiresponse.ErrorCodeRateLimited) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeRateLimited)
	}
	if strings.TrimSpace(payload.Error) == "" {
		t.Fatal("rate limit error payload should include a message")
	}
}

func TestRateLimitDifferentIPsAreIndependent(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	limiter := RateLimit(RateLimitConfig{RequestsPerSecond: 0.001, BurstSize: 1})
	handler := limiter(inner)

	// Drain IP A's bucket.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.4:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// IP B should still be allowed.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("different IP: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitExtractsXForwardedFor(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	limiter := RateLimit(RateLimitConfig{RequestsPerSecond: 0.001, BurstSize: 1})
	handler := limiter(inner)

	// First request from 1.2.3.4 via X-Forwarded-For.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.99:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.99")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first XFF request: status = %d", rec.Code)
	}

	// Second request from same IP should be limited.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.100:5678"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second XFF request: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimitDefaultConfig(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Zero-value config should use defaults and not panic.
	limiter := RateLimit(RateLimitConfig{})
	handler := limiter(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.6:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("default config: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// ReadinessHandler
// ---------------------------------------------------------------------------

func TestReadinessHandlerBootingReturns503(t *testing.T) {
	t.Parallel()

	rs := NewReadinessState()
	handler := ReadinessHandler(rs)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("booting: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp readinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.State != readinessBooting {
		t.Fatalf("state = %q, want %q", resp.State, readinessBooting)
	}
}

func TestReadinessHandlerReadyReturns200(t *testing.T) {
	t.Parallel()

	rs := NewReadinessState()
	rs.SetReady()
	handler := ReadinessHandler(rs)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ready: status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp readinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.State != readinessReady {
		t.Fatalf("state = %q, want %q", resp.State, readinessReady)
	}
}

func TestReadinessHandlerUnhealthyReturns503(t *testing.T) {
	t.Parallel()

	rs := NewReadinessState()
	rs.SetReady()
	rs.SetUnhealthy()
	handler := ReadinessHandler(rs)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
