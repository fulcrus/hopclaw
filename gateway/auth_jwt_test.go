package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// JWT edge cases beyond what auth_test.go covers
// ---------------------------------------------------------------------------

func TestJWTProviderMalformedBase64Header(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// Build a token with invalid base64 in the header part.
	token := "!!!invalid-base64!!!.eyJzdWIiOiJ1c2VyLTEifQ.signature"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for malformed base64 header")
	}
}

func TestJWTProviderMalformedJSONHeader(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// Valid base64 but not valid JSON.
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	claimsB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-1"}`))
	token := notJSON + "." + claimsB64 + ".fakesig"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for malformed JSON header")
	}
	if !strings.Contains(err.Error(), "header") {
		t.Fatalf("error = %v, want mention of header", err)
	}
}

func TestJWTProviderAlgorithmMismatch(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// Build a token claiming RS256 but provider expects HS256.
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claimsJSON, _ := json.Marshal(map[string]any{
		"sub": "user-1",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	token := headerB64 + "." + claimsB64 + "." + sigB64

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for algorithm mismatch")
	}
	if !strings.Contains(err.Error(), "algorithm") {
		t.Fatalf("error = %v, want mention of algorithm", err)
	}
}

func TestJWTProviderEmptyBearerToken(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for empty bearer, got %+v", id)
	}
}

func TestJWTProviderNonBearerScheme(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil identity for Basic scheme, got %+v", id)
	}
}

func TestJWTProviderClockSkewEdge(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	const skew = 5 * time.Second
	p, err := NewJWTProvider(JWTConfig{
		Secret:    secret,
		ClockSkew: skew,
	})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// Token expired 3 seconds ago, within 5s skew — should still be valid.
	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-skew",
		"exp": float64(time.Now().Add(-3 * time.Second).Unix()),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("within clock skew: Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected identity within clock skew")
	}
}

func TestJWTProviderNoExpClaim(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	p, err := NewJWTProvider(JWTConfig{Secret: secret})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}

	// Token without exp — should be valid (exp=0 is not enforced).
	token := buildTestJWT(t, secret, map[string]any{
		"sub": "user-no-exp",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("no exp: Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("expected identity for token without exp")
	}
}

func TestJWTProviderMissingSecretForHS256(t *testing.T) {
	t.Parallel()

	_, err := NewJWTProvider(JWTConfig{
		Algorithm: "HS256",
		Secret:    "",
	})
	if err == nil {
		t.Fatal("expected error for missing HS256 secret")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v, want mention of secret", err)
	}
}

func TestJWTProviderDefaultAlgorithmIsHS256(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "my-secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}
	if p.algorithm != "HS256" {
		t.Fatalf("algorithm = %q, want HS256", p.algorithm)
	}
}

func TestJWTProviderDefaultClockSkew(t *testing.T) {
	t.Parallel()

	p, err := NewJWTProvider(JWTConfig{Secret: "my-secret"})
	if err != nil {
		t.Fatalf("NewJWTProvider() error = %v", err)
	}
	if p.clockSkew != jwtDefaultClockSkew {
		t.Fatalf("clockSkew = %v, want %v", p.clockSkew, jwtDefaultClockSkew)
	}
}

// ---------------------------------------------------------------------------
// jwtAud UnmarshalJSON edge cases
// ---------------------------------------------------------------------------

func TestJWTAudUnmarshalString(t *testing.T) {
	t.Parallel()

	var aud jwtAud
	if err := json.Unmarshal([]byte(`"api"`), &aud); err != nil {
		t.Fatalf("Unmarshal string: %v", err)
	}
	if len(aud) != 1 || aud[0] != "api" {
		t.Fatalf("aud = %v, want [api]", aud)
	}
}

func TestJWTAudUnmarshalArray(t *testing.T) {
	t.Parallel()

	var aud jwtAud
	if err := json.Unmarshal([]byte(`["web","api"]`), &aud); err != nil {
		t.Fatalf("Unmarshal array: %v", err)
	}
	if len(aud) != 2 || aud[0] != "web" || aud[1] != "api" {
		t.Fatalf("aud = %v, want [web api]", aud)
	}
}

func TestJWTAudUnmarshalInvalid(t *testing.T) {
	t.Parallel()

	var aud jwtAud
	err := json.Unmarshal([]byte(`123`), &aud)
	if err == nil {
		t.Fatal("expected error for invalid aud type")
	}
}

// ---------------------------------------------------------------------------
// base64URLDecode
// ---------------------------------------------------------------------------

func TestBase64URLDecodeNoPadding(t *testing.T) {
	t.Parallel()

	original := "hello"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(original))
	decoded, err := base64URLDecode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if string(decoded) != original {
		t.Fatalf("decoded = %q, want %q", string(decoded), original)
	}
}

func TestBase64URLDecodeWithPadding(t *testing.T) {
	t.Parallel()

	// "ab" encodes to "YWI" (3 chars, needs 1 = padding).
	decoded, err := base64URLDecode("YWI")
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if string(decoded) != "ab" {
		t.Fatalf("decoded = %q, want ab", string(decoded))
	}
}

func TestBase64URLDecodeEmpty(t *testing.T) {
	t.Parallel()

	decoded, err := base64URLDecode("")
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %v, want empty", decoded)
	}
}
