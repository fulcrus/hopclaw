package gateway

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Bearer token auth provider
// ---------------------------------------------------------------------------

const (
	headerHopClawToken = "X-HopClaw-Token"
	// headerOpenClawToken is the legacy authentication header for backward
	// compatibility with OpenClaw deployments. New integrations should use
	// X-HopClaw-Token or Authorization.
	// Deprecated: will be removed in a future major version.
	headerOpenClawToken = "X-OpenClaw-Token"
	headerAuthorization = "Authorization"
	bearerPrefix        = "bearer "
)

// BearerTokenProvider authenticates requests by comparing a bearer token
// from standard headers against a known secret. Constant-time comparison
// is used to prevent timing attacks.
type BearerTokenProvider struct {
	token       string   // expected token
	headerNames []string // headers to check (in order)
}

// NewBearerTokenProvider returns a provider that accepts tokens via
// X-HopClaw-Token, X-OpenClaw-Token, and Authorization: Bearer headers.
func NewBearerTokenProvider(token string) *BearerTokenProvider {
	return &BearerTokenProvider{
		token: strings.TrimSpace(token),
		headerNames: []string{
			headerHopClawToken,
			headerOpenClawToken,
			headerAuthorization,
		},
	}
}

// Name returns "bearer".
func (p *BearerTokenProvider) Name() string { return "bearer" }

// Authenticate checks the known header names for a matching bearer token.
// Returns (nil, nil) when no bearer credentials are present, or when the
// Authorization header contains a value that does not match (it may be a
// JWT intended for another provider in the chain).
// Returns (*AuthIdentity, nil) on match.
// Returns (nil, error) when a dedicated header (X-HopClaw-Token,
// X-OpenClaw-Token) is present but the token does not match.
func (p *BearerTokenProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	if p.token == "" {
		return nil, nil
	}

	for _, name := range p.headerNames {
		raw := strings.TrimSpace(r.Header.Get(name))
		if raw == "" {
			continue
		}

		candidate := raw
		isSharedHeader := strings.EqualFold(name, headerAuthorization)
		if isSharedHeader {
			if !strings.HasPrefix(strings.ToLower(raw), bearerPrefix) {
				continue
			}
			candidate = strings.TrimSpace(raw[len(bearerPrefix):])
		}

		if constantTimeEqual(candidate, p.token) {
			return &AuthIdentity{
				Subject:  "bearer-token",
				Provider: p.Name(),
			}, nil
		}

		// For dedicated headers a mismatch is a hard error. For the shared
		// Authorization header we return (nil, nil) so other providers in
		// the chain can attempt validation (e.g. JWT).
		if !isSharedHeader {
			return nil, fmt.Errorf("invalid bearer token")
		}
	}

	return nil, nil
}

// constantTimeEqual compares two strings in constant time.
func constantTimeEqual(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
