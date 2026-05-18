package keychain

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Store / Load round-trip
// ---------------------------------------------------------------------------

func TestTokenManagerStoreLoad(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	token := Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"read", "write"},
		Provider:     "github",
	}

	if err := tm.Store("github", token); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	got, err := tm.Load("github")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.AccessToken != "access-123" {
		t.Fatalf("Load().AccessToken = %q, want %q", got.AccessToken, "access-123")
	}
	if got.RefreshToken != "refresh-456" {
		t.Fatalf("Load().RefreshToken = %q, want %q", got.RefreshToken, "refresh-456")
	}
	if got.TokenType != "Bearer" {
		t.Fatalf("Load().TokenType = %q, want %q", got.TokenType, "Bearer")
	}
	if got.Provider != "github" {
		t.Fatalf("Load().Provider = %q, want %q", got.Provider, "github")
	}
	if len(got.Scopes) != 2 {
		t.Fatalf("Load().Scopes len = %d, want 2", len(got.Scopes))
	}
}

// TestTokenManagerLoadFromKeychain verifies that Load reads from the
// keychain store when the token is not in the in-memory cache.
func TestTokenManagerLoadFromKeychain(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	token := Token{
		AccessToken: "access-789",
		TokenType:   "Bearer",
		Provider:    "google",
	}

	// Store via TokenManager to persist in keychain.
	if err := tm.Store("google", token); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Create a fresh TokenManager with the same store but empty cache.
	tm2 := NewTokenManagerWithStore(mock)

	got, err := tm2.Load("google")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.AccessToken != "access-789" {
		t.Fatalf("Load().AccessToken = %q, want %q", got.AccessToken, "access-789")
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestTokenManagerDelete(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	token := Token{
		AccessToken: "access-delete",
		TokenType:   "Bearer",
		Provider:    "slack",
	}

	if err := tm.Store("slack", token); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	if err := tm.Delete("slack"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err := tm.Load("slack")
	if err == nil {
		t.Fatal("Load() expected error after Delete")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want %v", err, ErrNotFound)
	}
}

func TestTokenManagerDeleteNotFound(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	err := tm.Delete("nonexistent")
	if err == nil {
		t.Fatal("Delete() expected error for nonexistent provider")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestTokenManagerList(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Empty initially.
	if got := tm.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}

	_ = tm.Store("github", Token{AccessToken: "a", TokenType: "Bearer"})
	_ = tm.Store("google", Token{AccessToken: "b", TokenType: "Bearer"})
	_ = tm.Store("slack", Token{AccessToken: "c", TokenType: "Bearer"})

	got := tm.List()
	if len(got) != 3 {
		t.Fatalf("List() len = %d, want 3", len(got))
	}
	// Should be sorted.
	if got[0] != "github" || got[1] != "google" || got[2] != "slack" {
		t.Fatalf("List() = %v, want [github google slack]", got)
	}
}

// ---------------------------------------------------------------------------
// IsExpired
// ---------------------------------------------------------------------------

func TestTokenManagerIsExpired(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Unknown provider is considered expired.
	if !tm.IsExpired("unknown") {
		t.Fatal("IsExpired() = false for unknown provider, want true")
	}

	// Token with future expiry.
	_ = tm.Store("fresh", Token{
		AccessToken: "a",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if tm.IsExpired("fresh") {
		t.Fatal("IsExpired() = true for fresh token, want false")
	}

	// Token with past expiry.
	_ = tm.Store("stale", Token{
		AccessToken: "b",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})
	if !tm.IsExpired("stale") {
		t.Fatal("IsExpired() = false for stale token, want true")
	}

	// Token with zero expiry (never expires).
	_ = tm.Store("forever", Token{
		AccessToken: "c",
		TokenType:   "Bearer",
	})
	if tm.IsExpired("forever") {
		t.Fatal("IsExpired() = true for token with no expiry, want false")
	}
}

// ---------------------------------------------------------------------------
// NeedsRefresh
// ---------------------------------------------------------------------------

func TestTokenManagerNeedsRefresh(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Unknown provider needs refresh.
	if !tm.NeedsRefresh("unknown") {
		t.Fatal("NeedsRefresh() = false for unknown provider, want true")
	}

	// Token expiring well in the future.
	_ = tm.Store("healthy", Token{
		AccessToken: "a",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if tm.NeedsRefresh("healthy") {
		t.Fatal("NeedsRefresh() = true for healthy token, want false")
	}

	// Token expiring within 5 minutes.
	_ = tm.Store("soon", Token{
		AccessToken: "b",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(3 * time.Minute),
	})
	if !tm.NeedsRefresh("soon") {
		t.Fatal("NeedsRefresh() = false for token expiring in 3m, want true")
	}

	// Token with zero expiry (never needs refresh).
	_ = tm.Store("permanent", Token{
		AccessToken: "c",
		TokenType:   "Bearer",
	})
	if tm.NeedsRefresh("permanent") {
		t.Fatal("NeedsRefresh() = true for token with no expiry, want false")
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestTokenManagerRefresh(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Store a token that needs refresh (expiring in 2 minutes).
	_ = tm.Store("github", Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(2 * time.Minute),
		Provider:     "github",
	})

	refreshCalled := false
	refreshFn := func(ctx context.Context, refreshToken string) (*Token, error) {
		refreshCalled = true
		if refreshToken != "old-refresh" {
			t.Fatalf("refreshFn got refreshToken = %q, want %q", refreshToken, "old-refresh")
		}
		return &Token{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			TokenType:    "Bearer",
			ExpiresAt:    time.Now().Add(time.Hour),
			Provider:     "github",
		}, nil
	}

	got, err := tm.Refresh(context.Background(), "github", refreshFn)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !refreshCalled {
		t.Fatal("Refresh() did not call refreshFn")
	}
	if got.AccessToken != "new-access" {
		t.Fatalf("Refresh().AccessToken = %q, want %q", got.AccessToken, "new-access")
	}

	// Verify the new token is persisted.
	loaded, err := tm.Load("github")
	if err != nil {
		t.Fatalf("Load() after Refresh() error = %v", err)
	}
	if loaded.AccessToken != "new-access" {
		t.Fatalf("Load().AccessToken after Refresh() = %q, want %q", loaded.AccessToken, "new-access")
	}
}

func TestTokenManagerRefreshSkipsWhenFresh(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Store a token with plenty of time left.
	_ = tm.Store("github", Token{
		AccessToken:  "still-good",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Provider:     "github",
	})

	refreshCalled := false
	refreshFn := func(ctx context.Context, refreshToken string) (*Token, error) {
		refreshCalled = true
		return nil, errors.New("should not be called")
	}

	got, err := tm.Refresh(context.Background(), "github", refreshFn)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshCalled {
		t.Fatal("Refresh() called refreshFn when token was still fresh")
	}
	if got.AccessToken != "still-good" {
		t.Fatalf("Refresh().AccessToken = %q, want %q", got.AccessToken, "still-good")
	}
}

func TestTokenManagerRefreshErrorNoToken(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	refreshFn := func(ctx context.Context, refreshToken string) (*Token, error) {
		return nil, errors.New("should not be called")
	}

	_, err := tm.Refresh(context.Background(), "missing", refreshFn)
	if err == nil {
		t.Fatal("Refresh() expected error for missing provider")
	}
}

func TestTokenManagerRefreshErrorNoRefreshToken(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	// Store a token that needs refresh but has no refresh token.
	_ = tm.Store("github", Token{
		AccessToken: "old-access",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(2 * time.Minute),
		Provider:    "github",
	})

	refreshFn := func(ctx context.Context, refreshToken string) (*Token, error) {
		return nil, errors.New("should not be called")
	}

	_, err := tm.Refresh(context.Background(), "github", refreshFn)
	if err == nil {
		t.Fatal("Refresh() expected error when no refresh token available")
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestTokenManagerStoreEmptyProvider(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	err := tm.Store("", Token{AccessToken: "a"})
	if err == nil {
		t.Fatal("Store() expected error for empty provider")
	}
}

func TestTokenManagerLoadEmptyProvider(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	_, err := tm.Load("")
	if err == nil {
		t.Fatal("Load() expected error for empty provider")
	}
}

func TestTokenManagerDeleteEmptyProvider(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	err := tm.Delete("")
	if err == nil {
		t.Fatal("Delete() expected error for empty provider")
	}
}

func TestTokenManagerRefreshNilFunc(t *testing.T) {
	mock := newMockStore()
	tm := NewTokenManagerWithStore(mock)

	_, err := tm.Refresh(context.Background(), "github", nil)
	if err == nil {
		t.Fatal("Refresh() expected error for nil refreshFn")
	}
}
