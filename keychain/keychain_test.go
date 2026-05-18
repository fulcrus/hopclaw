package keychain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockStore struct {
	mu      sync.Mutex // guards secrets
	secrets map[string]map[string]string
}

func newMockStore() *mockStore {
	return &mockStore{
		secrets: make(map[string]map[string]string),
	}
}

func (m *mockStore) Get(service, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	svc, ok := m.secrets[service]
	if !ok {
		return "", fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	val, ok := svc[key]
	if !ok {
		return "", fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	return val, nil
}

func (m *mockStore) Set(service, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.secrets[service]; !ok {
		m.secrets[service] = make(map[string]string)
	}
	m.secrets[service][key] = value
	return nil
}

func (m *mockStore) Delete(service, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	svc, ok := m.secrets[service]
	if !ok {
		return fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	if _, ok := svc[key]; !ok {
		return fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	delete(svc, key)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupMock replaces the package-level store with a fresh mock and restores
// the original on cleanup. Tests using setupMock must NOT use t.Parallel
// because they mutate package-level state.
func setupMock(t *testing.T) *mockStore {
	t.Helper()
	mock := newMockStore()
	original := store
	SetStore(mock)
	t.Cleanup(func() { store = original })
	return mock
}

// ---------------------------------------------------------------------------
// ResolveSecret tests
// ---------------------------------------------------------------------------

func TestResolveSecretKeychain(t *testing.T) {
	mock := setupMock(t)

	// Seed the store.
	if err := mock.Set(defaultService, "openai-api-key", "sk-test-123"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := ResolveSecret("keychain:openai-api-key")
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if got != "sk-test-123" {
		t.Fatalf("ResolveSecret() = %q, want %q", got, "sk-test-123")
	}
}

func TestResolveSecretKeychainNotFound(t *testing.T) {
	setupMock(t)

	_, err := ResolveSecret("keychain:nonexistent")
	if err == nil {
		t.Fatal("ResolveSecret() expected error for missing key")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveSecret() error = %v, want %v", err, ErrNotFound)
	}
}

func TestResolveSecretKeychainEmptyKey(t *testing.T) {
	setupMock(t)

	_, err := ResolveSecret("keychain:")
	if err == nil {
		t.Fatal("ResolveSecret() expected error for empty keychain key")
	}
}

func TestResolveSecretEnv(t *testing.T) {
	t.Setenv("TEST_HOPCLAW_KEY", "env-value-456")

	got, err := ResolveSecret("env:TEST_HOPCLAW_KEY")
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if got != "env-value-456" {
		t.Fatalf("ResolveSecret() = %q, want %q", got, "env-value-456")
	}
}

func TestResolveSecretEnvNotSet(t *testing.T) {
	_, err := ResolveSecret("env:HOPCLAW_DEFINITELY_NOT_SET_12345")
	if err == nil {
		t.Fatal("ResolveSecret() expected error for unset env var")
	}
}

func TestResolveSecretEnvEmptyName(t *testing.T) {
	_, err := ResolveSecret("env:")
	if err == nil {
		t.Fatal("ResolveSecret() expected error for empty env var name")
	}
}

func TestResolveSecretLiteral(t *testing.T) {
	got, err := ResolveSecret("sk-literal-value")
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if got != "sk-literal-value" {
		t.Fatalf("ResolveSecret() = %q, want %q", got, "sk-literal-value")
	}
}

func TestResolveSecretEmptyLiteral(t *testing.T) {
	got, err := ResolveSecret("")
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if got != "" {
		t.Fatalf("ResolveSecret() = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// SaveSecret / GetSecret / DeleteSecret lifecycle
// ---------------------------------------------------------------------------

func TestSetGetDeleteLifecycle(t *testing.T) {
	setupMock(t)

	// Set a secret.
	if err := SaveSecret("my-key", "my-value"); err != nil {
		t.Fatalf("SaveSecret() error = %v", err)
	}

	// Get it back.
	got, err := GetSecret("my-key")
	if err != nil {
		t.Fatalf("GetSecret() error = %v", err)
	}
	if got != "my-value" {
		t.Fatalf("GetSecret() = %q, want %q", got, "my-value")
	}

	// Overwrite.
	if err := SaveSecret("my-key", "updated-value"); err != nil {
		t.Fatalf("SaveSecret() overwrite error = %v", err)
	}
	got, err = GetSecret("my-key")
	if err != nil {
		t.Fatalf("GetSecret() after overwrite error = %v", err)
	}
	if got != "updated-value" {
		t.Fatalf("GetSecret() = %q, want %q", got, "updated-value")
	}

	// Delete.
	if err := DeleteSecret("my-key"); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}

	// Confirm deleted.
	_, err = GetSecret("my-key")
	if err == nil {
		t.Fatal("GetSecret() expected error after delete")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret() error = %v, want %v", err, ErrNotFound)
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestGetSecretEmptyKey(t *testing.T) {
	setupMock(t)

	_, err := GetSecret("")
	if err == nil {
		t.Fatal("GetSecret() expected error for empty key")
	}
}

func TestGetSecretWhitespaceKey(t *testing.T) {
	setupMock(t)

	_, err := GetSecret("   ")
	if err == nil {
		t.Fatal("GetSecret() expected error for whitespace key")
	}
}

func TestSaveSecretEmptyKey(t *testing.T) {
	setupMock(t)

	err := SaveSecret("", "value")
	if err == nil {
		t.Fatal("SaveSecret() expected error for empty key")
	}
}

func TestSaveSecretEmptyValue(t *testing.T) {
	setupMock(t)

	err := SaveSecret("key", "")
	if err == nil {
		t.Fatal("SaveSecret() expected error for empty value")
	}
}

func TestDeleteSecretEmptyKey(t *testing.T) {
	setupMock(t)

	err := DeleteSecret("")
	if err == nil {
		t.Fatal("DeleteSecret() expected error for empty key")
	}
}

func TestDeleteSecretNotFound(t *testing.T) {
	setupMock(t)

	err := DeleteSecret("nonexistent")
	if err == nil {
		t.Fatal("DeleteSecret() expected error for nonexistent key")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSecret() error = %v, want %v", err, ErrNotFound)
	}
}

func TestFileStoreEnvOverrideLifecycle(t *testing.T) {
	original := CurrentStore()
	defer SetStore(original)

	SetStore(newMockStore())
	path := filepath.Join(t.TempDir(), "keychain.json")
	t.Setenv(fileStorePathEnv, path)

	if err := SaveSecret("openai-api-key", "sk-file-123"); err != nil {
		t.Fatalf("SaveSecret() error = %v", err)
	}
	got, err := GetSecret("openai-api-key")
	if err != nil {
		t.Fatalf("GetSecret() error = %v", err)
	}
	if got != "sk-file-123" {
		t.Fatalf("GetSecret() = %q, want %q", got, "sk-file-123")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if len(data) == 0 {
		t.Fatal("expected file-backed keychain data to be written")
	}

	if err := DeleteSecret("openai-api-key"); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	if _, err := GetSecret("openai-api-key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret() error = %v, want %v", err, ErrNotFound)
	}
}

// ---------------------------------------------------------------------------
// ResolveField (non-breaking config helper)
// ---------------------------------------------------------------------------

func TestResolveFieldKeychainFallback(t *testing.T) {
	setupMock(t)

	// When keychain lookup fails, ResolveField returns the original value.
	got := ResolveField("keychain:missing-key")
	if got != "keychain:missing-key" {
		t.Fatalf("ResolveField() = %q, want original value", got)
	}
}

func TestResolveFieldLiteral(t *testing.T) {
	got := ResolveField("sk-literal")
	if got != "sk-literal" {
		t.Fatalf("ResolveField() = %q, want %q", got, "sk-literal")
	}
}

func TestResolveFieldEmpty(t *testing.T) {
	got := ResolveField("")
	if got != "" {
		t.Fatalf("ResolveField() = %q, want empty", got)
	}
}

func TestResolveFieldKeychainSuccess(t *testing.T) {
	mock := setupMock(t)

	if err := mock.Set(defaultService, "test-key", "resolved-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got := ResolveField("keychain:test-key")
	if got != "resolved-value" {
		t.Fatalf("ResolveField() = %q, want %q", got, "resolved-value")
	}
}

func TestResolveFieldEnvSuccess(t *testing.T) {
	t.Setenv("TEST_RESOLVE_FIELD_ENV", "env-resolved")

	got := ResolveField("env:TEST_RESOLVE_FIELD_ENV")
	if got != "env-resolved" {
		t.Fatalf("ResolveField() = %q, want %q", got, "env-resolved")
	}
}

func TestResolveFieldEnvFallback(t *testing.T) {
	got := ResolveField("env:TOTALLY_MISSING_VAR_999")
	if got != "env:TOTALLY_MISSING_VAR_999" {
		t.Fatalf("ResolveField() = %q, want original value", got)
	}
}
