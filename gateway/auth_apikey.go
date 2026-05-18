package gateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// API Key auth provider
// ---------------------------------------------------------------------------

const (
	headerAPIKey     = "X-API-Key"
	queryParamAPIKey = "api_key"
)

// APIKeyEntry describes a single API key and its associated metadata.
type APIKeyEntry struct {
	Key     string   `json:"key" yaml:"key"`
	Name    string   `json:"name" yaml:"name"`
	Scopes  []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Enabled bool     `json:"enabled" yaml:"enabled"`
}

// APIKeyProvider authenticates requests using API keys sent via the
// X-API-Key header or api_key query parameter. Keys are stored as
// SHA-256 hashes — the raw key is never retained in memory.
type APIKeyProvider struct {
	mu   sync.RWMutex
	keys map[string]*apiKeyRecord // hex(SHA-256(key)) -> record
}

// apiKeyRecord is the internal storage type for an API key entry.
type apiKeyRecord struct {
	hash    string // hex-encoded SHA-256 of the key
	name    string
	scopes  []string
	enabled bool
}

// NewAPIKeyProvider creates a provider pre-loaded with the given keys.
func NewAPIKeyProvider(keys []APIKeyEntry) *APIKeyProvider {
	p := &APIKeyProvider{
		keys: make(map[string]*apiKeyRecord, len(keys)),
	}
	for _, k := range keys {
		p.addEntry(k)
	}
	return p
}

// Name returns "apikey".
func (p *APIKeyProvider) Name() string { return "apikey" }

// AddKey adds (or replaces) an API key at runtime. Thread-safe.
func (p *APIKeyProvider) AddKey(entry APIKeyEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addEntry(entry)
}

// RemoveKey removes an API key by its raw value. Thread-safe.
func (p *APIKeyProvider) RemoveKey(key string) {
	h := hashAPIKey(key)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.keys, h)
}

// Authenticate checks the X-API-Key header and api_key query parameter.
// Returns (nil, nil) when no API key is present.
// Returns (*AuthIdentity, nil) when a valid, enabled key is found.
// Returns (nil, error) when a key is present but invalid or disabled.
func (p *APIKeyProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	candidate := strings.TrimSpace(r.Header.Get(headerAPIKey))
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get(queryParamAPIKey))
	}
	if candidate == "" {
		return nil, nil
	}

	record, ok := p.lookup(candidate)
	if !ok {
		return nil, fmt.Errorf("invalid api key")
	}
	if !record.enabled {
		return nil, fmt.Errorf("api key is disabled")
	}

	return &AuthIdentity{
		Subject:  record.name,
		Provider: p.Name(),
		Scopes:   record.scopes,
		Metadata: map[string]string{"key_name": record.name},
	}, nil
}

// lookup finds a record by comparing the SHA-256 hash in constant time.
func (p *APIKeyProvider) lookup(candidate string) (*apiKeyRecord, bool) {
	candidateHash := hashAPIKey(candidate)
	p.mu.RLock()
	defer p.mu.RUnlock()
	for storedHash, record := range p.keys {
		if subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) == 1 {
			return record, true
		}
	}
	return nil, false
}

func (p *APIKeyProvider) addEntry(entry APIKeyEntry) {
	h := hashAPIKey(entry.Key)
	p.keys[h] = &apiKeyRecord{
		hash:    h,
		name:    entry.Name,
		scopes:  entry.Scopes,
		enabled: entry.Enabled,
	}
}

// hashAPIKey returns the hex-encoded SHA-256 digest of the raw key.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
