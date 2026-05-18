package keychain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// oauthKeyPrefix is the keychain key prefix for OAuth tokens.
	oauthKeyPrefix = "hopclaw.oauth."

	// refreshWindow is the duration before expiry at which a token is
	// considered in need of refresh.
	refreshWindow = 5 * time.Minute
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Token represents an OAuth2 token with metadata.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	Provider     string    `json:"provider"`
}

// RefreshFunc obtains a new token using the provided refresh token.
type RefreshFunc func(ctx context.Context, refreshToken string) (*Token, error)

// TokenManager handles OAuth2 token storage, refresh, and retrieval.
// Tokens are stored in the platform keychain with automatic refresh.
type TokenManager struct {
	mu     sync.Mutex
	store  Store
	tokens map[string]*Token
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewTokenManager creates a TokenManager backed by the package-level
// keychain store.
func NewTokenManager() *TokenManager {
	return &TokenManager{
		store:  store,
		tokens: make(map[string]*Token),
	}
}

// NewTokenManagerWithStore creates a TokenManager backed by the given store.
// This is intended for testing.
func NewTokenManagerWithStore(s Store) *TokenManager {
	return &TokenManager{
		store:  s,
		tokens: make(map[string]*Token),
	}
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Store persists a token in the keychain for the given provider.
func (tm *TokenManager) Store(provider string, token Token) error {
	if provider == "" {
		return fmt.Errorf("oauth: provider must not be empty")
	}
	token.Provider = provider

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("oauth: marshal token for %s: %w", provider, err)
	}

	key := oauthKeyPrefix + provider
	if err := tm.store.Set(defaultService, key, string(data)); err != nil {
		return fmt.Errorf("oauth: store token for %s: %w", provider, err)
	}

	tm.mu.Lock()
	tm.tokens[provider] = &token
	tm.mu.Unlock()

	return nil
}

// Load retrieves a token from the keychain for the given provider.
func (tm *TokenManager) Load(provider string) (*Token, error) {
	if provider == "" {
		return nil, fmt.Errorf("oauth: provider must not be empty")
	}

	// Check in-memory cache first.
	tm.mu.Lock()
	if tok, ok := tm.tokens[provider]; ok {
		tm.mu.Unlock()
		return tok, nil
	}
	tm.mu.Unlock()

	key := oauthKeyPrefix + provider
	raw, err := tm.store.Get(defaultService, key)
	if err != nil {
		return nil, fmt.Errorf("oauth: load token for %s: %w", provider, err)
	}

	var token Token
	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return nil, fmt.Errorf("oauth: unmarshal token for %s: %w", provider, err)
	}

	tm.mu.Lock()
	tm.tokens[provider] = &token
	tm.mu.Unlock()

	return &token, nil
}

// Delete removes a token from both the keychain and in-memory cache.
func (tm *TokenManager) Delete(provider string) error {
	if provider == "" {
		return fmt.Errorf("oauth: provider must not be empty")
	}

	key := oauthKeyPrefix + provider
	if err := tm.store.Delete(defaultService, key); err != nil {
		return fmt.Errorf("oauth: delete token for %s: %w", provider, err)
	}

	tm.mu.Lock()
	delete(tm.tokens, provider)
	tm.mu.Unlock()

	return nil
}

// List returns the names of all providers with cached tokens, sorted
// alphabetically.
func (tm *TokenManager) List() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	names := make([]string, 0, len(tm.tokens))
	for name := range tm.tokens {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsExpired reports whether the token for the given provider has expired.
// Returns true if the provider is not found or the token has no expiry.
func (tm *TokenManager) IsExpired(provider string) bool {
	tm.mu.Lock()
	tok, ok := tm.tokens[provider]
	tm.mu.Unlock()

	if !ok {
		return true
	}
	if tok.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(tok.ExpiresAt)
}

// NeedsRefresh reports whether the token for the given provider expires
// within the refresh window (5 minutes). Returns true if the provider
// is not found.
func (tm *TokenManager) NeedsRefresh(provider string) bool {
	tm.mu.Lock()
	tok, ok := tm.tokens[provider]
	tm.mu.Unlock()

	if !ok {
		return true
	}
	if tok.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(refreshWindow).After(tok.ExpiresAt)
}

// Refresh refreshes the token for the given provider if it needs refreshing.
// The refreshFn is called with the current refresh token and must return a
// new token. The new token is stored in the keychain automatically.
func (tm *TokenManager) Refresh(ctx context.Context, provider string, refreshFn RefreshFunc) (*Token, error) {
	if provider == "" {
		return nil, fmt.Errorf("oauth: provider must not be empty")
	}
	if refreshFn == nil {
		return nil, fmt.Errorf("oauth: refresh function must not be nil")
	}

	if !tm.NeedsRefresh(provider) {
		tm.mu.Lock()
		tok := tm.tokens[provider]
		tm.mu.Unlock()
		return tok, nil
	}

	tm.mu.Lock()
	tok, ok := tm.tokens[provider]
	tm.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("oauth: no token cached for provider %s", provider)
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("oauth: no refresh token available for provider %s", provider)
	}

	newToken, err := refreshFn(ctx, tok.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("oauth: refresh token for %s: %w", provider, err)
	}

	if err := tm.Store(provider, *newToken); err != nil {
		return nil, err
	}

	return newToken, nil
}
