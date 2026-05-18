package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyProviderRejectsUntrustedSource(t *testing.T) {
	t.Parallel()

	provider := NewTrustedProxyProvider("", "")
	req := httptest.NewRequest("GET", "/operator/status", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-User", "alice")

	identity, err := provider.Authenticate(req)
	if err == nil {
		t.Fatal("expected untrusted source to be rejected")
	}
	if identity != nil {
		t.Fatalf("identity = %#v, want nil", identity)
	}
}

func TestTrustedProxyProviderAcceptsConfiguredTrustedSource(t *testing.T) {
	t.Parallel()

	provider, err := NewTrustedProxyProviderWithTrustedCIDRs("X-Forwarded-User", "X-Forwarded-Email", []string{"203.0.113.0/24"})
	if err != nil {
		t.Fatalf("NewTrustedProxyProviderWithTrustedCIDRs() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/operator/status", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-User", "alice")
	req.Header.Set("X-Forwarded-Email", "alice@example.com")

	identity, err := provider.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity == nil || identity.Subject != "alice" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.Metadata["email"] != "alice@example.com" {
		t.Fatalf("email metadata = %#v", identity.Metadata)
	}
}
