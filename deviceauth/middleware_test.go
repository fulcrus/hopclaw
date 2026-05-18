package deviceauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupMiddleware creates a store, pairing manager, and middleware with a
// registered trusted device and stored token, ready for testing.
func setupMiddleware(t *testing.T) (*Middleware, *Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	pm := NewPairingManager(store)
	mw := NewMiddleware(store, pm)

	deviceID := GenerateDeviceID()
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}

	// Register and trust the device.
	dev := &DeviceIdentity{DeviceID: deviceID, Trusted: true}
	if err := store.RegisterDevice(dev); err != nil {
		t.Fatal(err)
	}

	// Store the token.
	dt := &DeviceToken{
		Token:    tok,
		DeviceID: deviceID,
		Role:     RoleOperator,
		IssuedAt: time.Now().UTC(),
	}
	if err := store.SetToken(dt); err != nil {
		t.Fatal(err)
	}

	return mw, store, deviceID, tok
}

// makeAuthHeader builds a valid V2 auth payload header value.
func makeAuthHeader(deviceID, token string) string {
	p := AuthPayload{
		DeviceID:   deviceID,
		ClientID:   "test-client",
		ClientMode: "interactive",
		Role:       RoleOperator,
		SignedAtMs: time.Now().UnixMilli(),
		Token:      token,
		Nonce:      GenerateDeviceID(), // use random string as nonce
	}
	return EncodePayloadV2(p)
}

// ---------------------------------------------------------------------------
// Authenticate: valid auth passes
// ---------------------------------------------------------------------------

func TestMiddlewareAuthenticate(t *testing.T) {
	mw, _, deviceID, tok := setupMiddleware(t)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		dc, ok := DeviceFromContext(r.Context())
		if !ok {
			t.Fatal("expected DeviceContext in context")
		}
		if dc.DeviceID != deviceID {
			t.Fatalf("device_id = %q, want %q", dc.DeviceID, deviceID)
		}
		if dc.Role != RoleOperator {
			t.Fatalf("role = %q, want %q", dc.Role, RoleOperator)
		}
		if !dc.Trusted {
			t.Fatal("expected trusted")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Authenticate(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(headerDeviceAuth, makeAuthHeader(deviceID, tok))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Authenticate: reject invalid payload
// ---------------------------------------------------------------------------

func TestMiddlewareRejectInvalid(t *testing.T) {
	mw, _, _, _ := setupMiddleware(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	cases := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"malformed payload", "not-a-valid-payload"},
		{"bad version", "v1|a|b|c|d|e|123|tok|nonce"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := mw.Authenticate(inner)
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			if tc.header != "" {
				req.Header.Set(headerDeviceAuth, tc.header)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Authenticate: reject untrusted device
// ---------------------------------------------------------------------------

func TestMiddlewareRejectUntrusted(t *testing.T) {
	mw, store, deviceID, tok := setupMiddleware(t)

	// Revoke trust.
	if err := store.RevokeDevice(deviceID); err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called for untrusted device")
	})

	handler := mw.Authenticate(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(headerDeviceAuth, makeAuthHeader(deviceID, tok))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// Optional: unauthenticated passes through
// ---------------------------------------------------------------------------

func TestMiddlewareOptional(t *testing.T) {
	mw, _, _, _ := setupMiddleware(t)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		dc, ok := DeviceFromContext(r.Context())
		if ok && dc != nil {
			t.Fatal("expected no DeviceContext for unauthenticated request")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Optional(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// No auth header.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestMiddlewareOptionalWithAuth verifies that Optional attaches context when
// valid auth is present.
func TestMiddlewareOptionalWithAuth(t *testing.T) {
	mw, _, deviceID, tok := setupMiddleware(t)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		dc, ok := DeviceFromContext(r.Context())
		if !ok {
			t.Fatal("expected DeviceContext when auth is present")
		}
		if dc.DeviceID != deviceID {
			t.Fatalf("device_id = %q, want %q", dc.DeviceID, deviceID)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Optional(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(headerDeviceAuth, makeAuthHeader(deviceID, tok))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
}

// ---------------------------------------------------------------------------
// Loopback detection
// ---------------------------------------------------------------------------

func TestMiddlewareLoopback(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"ipv4 loopback", "127.0.0.1:9090", true},
		{"ipv6 loopback", "[::1]:9090", true},
		{"localhost", "localhost:9090", true},
		{"external ip", "192.168.1.100:9090", false},
		{"bare ipv4 loopback", "127.0.0.1", true},
		{"bare external", "10.0.0.1", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			got := IsLoopback(req)
			if got != tc.want {
				t.Fatalf("IsLoopback(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DeviceFromContext: no context set
// ---------------------------------------------------------------------------

func TestDeviceFromContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	dc, ok := DeviceFromContext(req.Context())
	if ok {
		t.Fatal("expected ok=false for empty context")
	}
	if dc != nil {
		t.Fatal("expected nil DeviceContext for empty context")
	}
}

// ---------------------------------------------------------------------------
// Token mismatch
// ---------------------------------------------------------------------------

func TestMiddlewareRejectTokenMismatch(t *testing.T) {
	mw, _, deviceID, _ := setupMiddleware(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called for token mismatch")
	})

	handler := mw.Authenticate(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(headerDeviceAuth, makeAuthHeader(deviceID, "hc_wrong_token_value_that_does_not_match_stored"))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// Token expired
// ---------------------------------------------------------------------------

func TestMiddlewareRejectExpiredToken(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	pm := NewPairingManager(store)
	mw := NewMiddleware(store, pm)

	deviceID := GenerateDeviceID()
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}

	dev := &DeviceIdentity{DeviceID: deviceID, Trusted: true}
	if err := store.RegisterDevice(dev); err != nil {
		t.Fatal(err)
	}

	// Store token with expiry in the past.
	dt := &DeviceToken{
		Token:     tok,
		DeviceID:  deviceID,
		Role:      RoleOperator,
		IssuedAt:  time.Now().Add(-2 * time.Hour).UTC(),
		ExpiresAt: time.Now().Add(-time.Hour).UTC(),
	}
	if err := store.SetToken(dt); err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called for expired token")
	})

	handler := mw.Authenticate(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set(headerDeviceAuth, makeAuthHeader(deviceID, tok))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}
