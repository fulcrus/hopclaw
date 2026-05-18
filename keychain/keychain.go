package keychain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/logging"

	"github.com/zalando/go-keyring"
)

var log = logging.WithSubsystem("keychain")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultService is the keychain service name used by HopClaw.
	defaultService = "hopclaw"

	// keychainPrefix triggers a keychain lookup in ResolveSecret.
	keychainPrefix = "keychain:"

	// envPrefix triggers an environment variable lookup in ResolveSecret.
	envPrefix = "env:"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrNotFound is returned when a key does not exist in the store.
var ErrNotFound = errors.New("secret not found")

// ---------------------------------------------------------------------------
// Store interface
// ---------------------------------------------------------------------------

// Store provides secure credential storage.
type Store interface {
	Get(service, key string) (string, error)
	Set(service, key, value string) error
	Delete(service, key string) error
}

// ---------------------------------------------------------------------------
// Native keychain store (go-keyring)
// ---------------------------------------------------------------------------

// nativeStore uses the platform-native keychain via go-keyring.
type nativeStore struct{}

func (s *nativeStore) Get(service, key string) (string, error) {
	val, err := keyring.Get(service, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
		}
		return "", fmt.Errorf("keychain get %s/%s: %w", service, key, err)
	}
	return val, nil
}

func (s *nativeStore) Set(service, key, value string) error {
	if err := keyring.Set(service, key, value); err != nil {
		return fmt.Errorf("keychain set %s/%s: %w", service, key, err)
	}
	return nil
}

func (s *nativeStore) Delete(service, key string) error {
	if err := keyring.Delete(service, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
		}
		return fmt.Errorf("keychain delete %s/%s: %w", service, key, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Package-level store
// ---------------------------------------------------------------------------

// store is the package-level keychain store. It defaults to the native
// platform keychain but can be overridden for testing.
var store Store = &nativeStore{}

// SetStore replaces the package-level keychain store. This is intended
// for testing only.
func SetStore(s Store) {
	store = s
}

// CurrentStore returns the package-level keychain store.
// It is primarily intended for tests that need to restore the previous store.
func CurrentStore() Store {
	return store
}

// DefaultService returns the default keychain service name.
func DefaultService() string {
	return defaultService
}

// ---------------------------------------------------------------------------
// Public operations
// ---------------------------------------------------------------------------

// SaveSecret stores a secret in the keychain under the default service.
func SaveSecret(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("keychain: key must not be empty")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("keychain: value must not be empty")
	}
	return activeStore().Set(defaultService, key, value)
}

// GetSecret retrieves a secret from the keychain under the default service.
func GetSecret(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("keychain: key must not be empty")
	}
	return activeStore().Get(defaultService, key)
}

// DeleteSecret removes a secret from the keychain under the default service.
func DeleteSecret(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("keychain: key must not be empty")
	}
	return activeStore().Delete(defaultService, key)
}

// ---------------------------------------------------------------------------
// Config integration helper
// ---------------------------------------------------------------------------

// ResolveField resolves a single config field value. If the value starts
// with "keychain:", the remainder is looked up in the keychain. On failure
// (keychain unavailable, key missing) the original string is returned and
// the error is logged — this ensures config loading never breaks.
func ResolveField(value string) string {
	resolved, err := ResolveSecret(value)
	if err != nil {
		log.Warn("resolve secret failed, using original value", "key", value, "error", err)
		return value
	}
	return resolved
}
