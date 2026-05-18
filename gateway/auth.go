package gateway

import (
	"context"
	"encoding/json"
	"net/http"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Auth provider interface
// ---------------------------------------------------------------------------

// AuthProvider validates an HTTP request and returns the authenticated identity.
type AuthProvider interface {
	// Authenticate validates the request and returns the authenticated identity.
	// Returns nil, nil when the provider does not recognize the credentials
	// (e.g. no relevant header present), allowing the chain to try the next
	// provider. Returns nil, error when credentials were present but invalid.
	Authenticate(r *http.Request) (*AuthIdentity, error)
	// Name returns the provider name (e.g., "bearer", "jwt", "apikey").
	Name() string
}

// AuthIdentity represents the authenticated caller.
type AuthIdentity struct {
	Subject  string            `json:"subject"`
	Provider string            `json:"provider"`
	Scopes   []string          `json:"scopes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

const (
	authMetadataKeyRole   = "role"
	authMetadataKeyGroups = "groups"
)

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type authContextKey struct{}

// AuthIdentityFromContext extracts the AuthIdentity stored by the auth
// middleware. Returns nil when the request was not authenticated.
func AuthIdentityFromContext(ctx context.Context) *AuthIdentity {
	id, _ := ctx.Value(authContextKey{}).(*AuthIdentity)
	return id
}

func contextWithAuthIdentity(ctx context.Context, id *AuthIdentity) context.Context {
	return context.WithValue(ctx, authContextKey{}, id)
}

// ---------------------------------------------------------------------------
// Auth chain
// ---------------------------------------------------------------------------

// AuthChain tries multiple AuthProviders in order. The first successful
// authentication wins. If all providers return nil (no credentials
// recognized) and Optional is false, the request is rejected with 401.
type AuthChain struct {
	providers []AuthProvider
	optional  bool // if true, unauthenticated requests pass through
}

// NewAuthChain creates a chain that tries each provider in order.
func NewAuthChain(providers ...AuthProvider) *AuthChain {
	return &AuthChain{providers: providers}
}

// Authenticate validates a single request against the configured providers.
// It returns nil, nil when no provider recognized the request.
func (c *AuthChain) Authenticate(r *http.Request) (*AuthIdentity, error) {
	if c == nil || len(c.providers) == 0 {
		return nil, nil
	}
	for _, p := range c.providers {
		identity, err := p.Authenticate(r)
		if err != nil {
			return nil, err
		}
		if identity != nil {
			return identity, nil
		}
	}
	return nil, nil
}

// Middleware returns an http.Handler middleware that authenticates requests
// using the configured provider chain.
func (c *AuthChain) Middleware(next http.Handler) http.Handler {
	// No providers configured — pass everything through.
	if len(c.providers) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := c.Authenticate(r)
		if err != nil {
			writeAuthError(r.Context(), w, err.Error())
			return
		}
		if identity != nil {
			ctx := contextWithAuthIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if c.optional {
			next.ServeHTTP(w, r)
			return
		}
		writeAuthError(r.Context(), w, "missing or invalid auth credentials")
	})
}

func writeAuthError(ctx context.Context, w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="hopclaw-operator"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	logging.LogIfErr(ctx, json.NewEncoder(w).Encode(errorResponse{
		Code:  string(apiresponse.ErrorCodeUnauthenticated),
		Error: msg,
	}), "write auth error response failed")
}
