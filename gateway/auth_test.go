package gateway

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AuthChain tests
// ---------------------------------------------------------------------------

func TestAuthChainNoProviders(t *testing.T) {
	t.Parallel()

	chain := NewAuthChain()
	handler := chain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no providers: status = %d, want 200", rec.Code)
	}
}

func TestAuthChainFirstProviderWins(t *testing.T) {
	t.Parallel()

	chain := NewAuthChain(
		NewBearerTokenProvider("token-a"),
		NewBearerTokenProvider("token-b"),
	)
	handler := chain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := AuthIdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first provider match: status = %d, want 200", rec.Code)
	}
}

func TestAuthChainRejectsWhenAllFail(t *testing.T) {
	t.Parallel()

	chain := NewAuthChain(
		NewBearerTokenProvider("correct-token"),
	)
	handler := chain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: status = %d, want 401", rec.Code)
	}
}

func TestAuthChainOptionalPassesThrough(t *testing.T) {
	t.Parallel()

	chain := NewAuthChain(
		NewBearerTokenProvider("token"),
	)
	chain.optional = true
	handler := chain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("optional chain: status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// BearerTokenProvider tests
// ---------------------------------------------------------------------------

func TestBearerTokenProviderAuthorizationHeader(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("my-secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil || id.Provider != "bearer" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestBearerTokenProviderXHopClawToken(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("my-secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-HopClaw-Token", "my-secret")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
}

func TestBearerTokenProviderXOpenClawToken(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("my-secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-OpenClaw-Token", "my-secret")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
}

func TestBearerTokenProviderRejectsWrongDedicatedHeader(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("correct")

	// Dedicated headers return hard error on mismatch.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-HopClaw-Token", "wrong")
	id, err := p.Authenticate(req)
	if err == nil {
		t.Fatalf("expected error for wrong X-HopClaw-Token, got identity = %+v", id)
	}
}

func TestBearerTokenProviderSoftPassOnAuthorizationMismatch(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("correct")

	// Authorization header mismatch returns (nil, nil) so other providers
	// in the chain (e.g. JWT) can attempt validation.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-but-maybe-jwt")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected nil error for Authorization mismatch, got %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for Authorization mismatch, got %+v", id)
	}
}

func TestBearerTokenProviderNoCredentials(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for no credentials, got %+v", id)
	}
}

func TestBearerTokenProviderEmptyToken(t *testing.T) {
	t.Parallel()

	p := NewBearerTokenProvider("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer something")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for empty provider token, got %+v", id)
	}
}

// ---------------------------------------------------------------------------
// Auth-session tests
// ---------------------------------------------------------------------------

func TestAuthSessionProviderCookieAuthenticatesAndTouchesLastSeen(t *testing.T) {
	t.Parallel()

	cfg := AuthSessionConfig{CookieName: "hc_session"}
	store := NewMemoryAuthSessionStore(cfg)
	t.Cleanup(store.Close)

	session := store.Create(&AuthIdentity{
		Subject:  "user-42",
		Provider: sessionProviderName,
	})
	beforeTouch := session.LastSeenAt.Add(-time.Minute)
	session.LastSeenAt = beforeTouch

	provider := NewAuthSessionProvider(store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(cfg.CookieName), Value: session.ID})

	id, err := provider.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil || id.Subject != "user-42" {
		t.Fatalf("identity = %#v", id)
	}
	if !session.LastSeenAt.After(beforeTouch) {
		t.Fatalf("LastSeenAt = %s, want after %s", session.LastSeenAt, beforeTouch)
	}
}

func TestAuthSessionCSRFMiddlewareRejectsMissingTokenAndAllowsHeader(t *testing.T) {
	t.Parallel()

	cfg := AuthSessionConfig{CookieName: "hc_session"}
	store := NewMemoryAuthSessionStore(cfg)
	t.Cleanup(store.Close)

	session := store.Create(&AuthIdentity{
		Subject:  "csrf-user",
		Provider: sessionProviderName,
	})

	hits := 0
	handler := AuthSessionCSRFMiddleware(store, authSessionCookieName(cfg.CookieName))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/operator/mutate", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(cfg.CookieName), Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	if hits != 0 {
		t.Fatalf("hits = %d, want 0", hits)
	}

	req = httptest.NewRequest(http.MethodPost, "/operator/mutate", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(cfg.CookieName), Value: session.ID})
	req.Header.Set(csrfHeaderName, session.CSRFToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST with csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
}

// ---------------------------------------------------------------------------
// JWTProvider tests
// ---------------------------------------------------------------------------

func TestJWTProviderValidToken(t *testing.T) {
	t.Parallel()

	secret := "test-jwt-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret:   secret,
		Issuer:   "hopclaw",
		Audience: "api",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub":    "user-42",
		"iss":    "hopclaw",
		"aud":    "api",
		"scopes": []string{"read", "write"},
		"exp":    float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Subject != "user-42" {
		t.Fatalf("subject = %q, want user-42", id.Subject)
	}
	if id.Provider != "jwt" {
		t.Fatalf("provider = %q, want jwt", id.Provider)
	}
	if len(id.Scopes) != 2 || id.Scopes[0] != "read" || id.Scopes[1] != "write" {
		t.Fatalf("scopes = %v", id.Scopes)
	}
}

func TestJWTProviderExpiredToken(t *testing.T) {
	t.Parallel()

	secret := "test-jwt-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret:    secret,
		ClockSkew: time.Second,
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-1",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error = %v, want 'expired'", err)
	}
}

func TestJWTProviderWrongSignature(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "correct-secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, "wrong-secret", map[string]any{
		"sub": "user-1",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong signature")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want 'signature'", err)
	}
}

func TestJWTProviderWrongIssuer(t *testing.T) {
	t.Parallel()

	secret := "shared-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret: secret,
		Issuer: "expected-issuer",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-1",
		"iss": "wrong-issuer",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWTProviderWrongAudience(t *testing.T) {
	t.Parallel()

	secret := "shared-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret:   secret,
		Audience: "expected-aud",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-1",
		"aud": "wrong-aud",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestJWTProviderNotBeforeInFuture(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret:    secret,
		ClockSkew: time.Second,
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-1",
		"nbf": float64(time.Now().Add(time.Hour).Unix()),
		"exp": float64(time.Now().Add(2 * time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for nbf in future")
	}
	if !strings.Contains(err.Error(), "not yet valid") {
		t.Fatalf("error = %v, want 'not yet valid'", err)
	}
}

func TestJWTProviderNoAuthHeader(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity, got %+v", id)
	}
}

func TestJWTProviderSkipsNonJWTBearer(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// A simple bearer token (no dots) should be skipped by JWT provider.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer simple-token-no-dots")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for non-JWT token, got %+v", id)
	}
}

func TestJWTProviderUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := NewJWTProvider(JWTConfig{
		Secret:    "secret",
		Algorithm: "ES384",
	})
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v, want 'unsupported'", err)
	}
}

func TestJWTProviderAudienceArray(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	p, err := NewJWTProvider(JWTConfig{
		Secret:   secret,
		Audience: "api",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-1",
		"aud": []string{"web", "api"},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected identity for matching aud array")
	}
}

// ---------------------------------------------------------------------------
// APIKeyProvider tests
// ---------------------------------------------------------------------------

func TestAPIKeyProviderHeader(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider([]APIKeyEntry{
		{Key: "key-abc-123", Name: "test-key", Scopes: []string{"read"}, Enabled: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "key-abc-123")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Subject != "test-key" {
		t.Fatalf("subject = %q, want test-key", id.Subject)
	}
	if id.Provider != "apikey" {
		t.Fatalf("provider = %q, want apikey", id.Provider)
	}
}

func TestAPIKeyProviderQueryParam(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider([]APIKeyEntry{
		{Key: "key-xyz", Name: "query-key", Enabled: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/?api_key=key-xyz", nil)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
}

func TestAPIKeyProviderInvalidKey(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider([]APIKeyEntry{
		{Key: "valid-key", Name: "my-key", Enabled: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestAPIKeyProviderDisabledKey(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider([]APIKeyEntry{
		{Key: "disabled-key", Name: "my-key", Enabled: false},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "disabled-key")
	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for disabled key")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %v, want 'disabled'", err)
	}
}

func TestAPIKeyProviderNoCredentials(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider([]APIKeyEntry{
		{Key: "some-key", Name: "my-key", Enabled: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity, got %+v", id)
	}
}

func TestAPIKeyProviderAddAndRemove(t *testing.T) {
	t.Parallel()

	p := NewAPIKeyProvider(nil)

	// Initially empty — should pass through.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "dynamic-key")
	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}

	// Add key at runtime.
	p.AddKey(APIKeyEntry{Key: "dynamic-key", Name: "dyn", Enabled: true})
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() after AddKey error = %v", err)
	}
	if id == nil {
		t.Fatal("expected identity after AddKey")
	}

	// Remove key.
	p.RemoveKey("dynamic-key")
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error after RemoveKey")
	}
}

// ---------------------------------------------------------------------------
// AuthIdentity context round-trip
// ---------------------------------------------------------------------------

func TestAuthIdentityContextRoundTrip(t *testing.T) {
	t.Parallel()

	chain := NewAuthChain(NewBearerTokenProvider("secret"))
	var captured *AuthIdentity
	handler := chain.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = AuthIdentityFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if captured == nil {
		t.Fatal("expected identity in context")
	}
	if captured.Provider != "bearer" {
		t.Fatalf("provider = %q, want bearer", captured.Provider)
	}
}

// ---------------------------------------------------------------------------
// Multi-provider chain integration
// ---------------------------------------------------------------------------

func TestAuthChainMultiProviders(t *testing.T) {
	t.Parallel()

	secret := "jwt-secret"
	jwtProvider, err := NewJWTProvider(JWTConfig{Secret: secret})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	chain := NewAuthChain(
		NewBearerTokenProvider("bearer-token"),
		jwtProvider,
		NewAPIKeyProvider([]APIKeyEntry{
			{Key: "api-key-1", Name: "key1", Enabled: true},
		}),
	)

	handler := chain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := AuthIdentityFromContext(r.Context())
		w.Header().Set("X-Auth-Provider", id.Provider)
		w.WriteHeader(http.StatusOK)
	}))

	// Test bearer token.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-HopClaw-Token", "bearer-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Auth-Provider") != "bearer" {
		t.Fatalf("bearer auth: status=%d provider=%s", rec.Code, rec.Header().Get("X-Auth-Provider"))
	}

	// Test JWT.
	token := buildTestJWT(t, secret, map[string]any{
		"sub": "jwt-user",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Auth-Provider") != "jwt" {
		t.Fatalf("jwt auth: status=%d provider=%s", rec.Code, rec.Header().Get("X-Auth-Provider"))
	}

	// Test API key.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "api-key-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Auth-Provider") != "apikey" {
		t.Fatalf("apikey auth: status=%d provider=%s", rec.Code, rec.Header().Get("X-Auth-Provider"))
	}

	// No credentials — should be rejected.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status=%d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// RS256 JWTProvider tests
// ---------------------------------------------------------------------------

func TestJWTProviderRS256ValidToken(t *testing.T) {
	t.Parallel()

	privKey, pubKeyPEM := generateTestRSAKey(t)

	p, err := NewJWTProvider(JWTConfig{
		PublicKey: pubKeyPEM,
		Issuer:    "hopclaw",
		Audience:  "api",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestRS256JWT(t, privKey, map[string]any{
		"sub":    "rs256-user",
		"iss":    "hopclaw",
		"aud":    "api",
		"scopes": []string{"admin"},
		"exp":    float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}
	if id.Subject != "rs256-user" {
		t.Fatalf("subject = %q, want rs256-user", id.Subject)
	}
	if id.Provider != "jwt" {
		t.Fatalf("provider = %q, want jwt", id.Provider)
	}
	if len(id.Scopes) != 1 || id.Scopes[0] != "admin" {
		t.Fatalf("scopes = %v, want [admin]", id.Scopes)
	}
}

func TestJWTProviderRS256WrongKey(t *testing.T) {
	t.Parallel()

	// Sign with one key, verify with another.
	signingKey, _ := generateTestRSAKey(t)
	_, wrongPubPEM := generateTestRSAKey(t)

	p, err := NewJWTProvider(JWTConfig{
		PublicKey: wrongPubPEM,
		Algorithm: "RS256",
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestRS256JWT(t, signingKey, map[string]any{
		"sub": "user-1",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong RS256 key")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want 'signature'", err)
	}
}

func TestJWTProviderRS256ExpiredToken(t *testing.T) {
	t.Parallel()

	privKey, pubKeyPEM := generateTestRSAKey(t)

	p, err := NewJWTProvider(JWTConfig{
		PublicKey: pubKeyPEM,
		ClockSkew: time.Second,
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	token := buildTestRS256JWT(t, privKey, map[string]any{
		"sub": "user-1",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for expired RS256 token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error = %v, want 'expired'", err)
	}
}

func TestJWTProviderRS256AutoDetectAlgorithm(t *testing.T) {
	t.Parallel()

	_, pubKeyPEM := generateTestRSAKey(t)

	// When PublicKey is set and Algorithm is empty, RS256 should be auto-detected.
	p, err := NewJWTProvider(JWTConfig{
		PublicKey: pubKeyPEM,
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}
	if p.algorithm != "RS256" {
		t.Fatalf("algorithm = %q, want RS256", p.algorithm)
	}
}

func TestJWTProviderRS256MissingPublicKey(t *testing.T) {
	t.Parallel()

	_, err := NewJWTProvider(JWTConfig{
		Algorithm: "RS256",
	})
	if err == nil {
		t.Fatal("expected error for missing public key")
	}
	if !strings.Contains(err.Error(), "public_key") {
		t.Fatalf("error = %v, want 'public_key'", err)
	}
}

func TestJWTProviderRS256InvalidPEM(t *testing.T) {
	t.Parallel()

	_, err := NewJWTProvider(JWTConfig{
		Algorithm: "RS256",
		PublicKey: "not-a-valid-pem",
	})
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildTestJWT creates a signed HS256 JWT for testing.
func buildTestJWT(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s.%s", headerB64, claimsB64, sigB64)
}

// testRSAKeyBits is the RSA key size used in tests. Kept small for speed.
const testRSAKeyBits = 2048

// generateTestRSAKey creates a fresh RSA key pair and returns the private key
// plus the PEM-encoded public key string.
func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, testRSAKeyBits)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	})
	return privKey, string(pubPEM)
}

// buildTestRS256JWT creates a signed RS256 JWT for testing.
func buildTestRS256JWT(t *testing.T, privKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign RS256: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s.%s", headerB64, claimsB64, sigB64)
}
