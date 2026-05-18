package pairing

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	codeLength = 6
	codeExpiry = 10 * time.Minute
	codeMin    = 100000 // smallest 6-digit number
	codeMax    = 999999 // largest 6-digit number
)

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager handles DM pairing lifecycle: initiation, verification, and revocation.
type Manager struct {
	store Store
	mu    sync.Mutex // guards code generation
}

// NewManager creates a Manager backed by the given Store.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// InitiatePairing generates a new verification code for the given DM user.
// If a pending record already exists, it is replaced with a fresh code.
func (m *Manager) InitiatePairing(channel, userID, displayName string) (string, error) {
	channel = strings.TrimSpace(channel)
	userID = strings.TrimSpace(userID)
	if channel == "" {
		return "", fmt.Errorf("channel is required")
	}
	if userID == "" {
		return "", fmt.Errorf("user id is required")
	}

	m.mu.Lock()
	code, err := generateCode()
	m.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("generate verification code: %w", err)
	}

	now := time.Now().UTC()
	rec := &PairingRecord{
		ID:            storeKey(channel, userID),
		Channel:       channel,
		UserID:        userID,
		DisplayName:   strings.TrimSpace(displayName),
		Status:        StatusPending,
		Code:          code,
		CodeExpiresAt: now.Add(codeExpiry),
		CreatedAt:     now,
	}

	if err := m.store.Save(rec); err != nil {
		return "", fmt.Errorf("save pairing record: %w", err)
	}
	return code, nil
}

// VerifyCode validates a verification code and marks the record as verified.
func (m *Manager) VerifyCode(code string) (*PairingRecord, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("verification code is required")
	}

	rec, err := m.store.GetByCode(code)
	if err != nil {
		return nil, fmt.Errorf("invalid verification code")
	}
	if rec.Status != StatusPending {
		return nil, fmt.Errorf("pairing record is not pending")
	}
	if time.Now().UTC().After(rec.CodeExpiresAt) {
		return nil, fmt.Errorf("verification code has expired")
	}

	now := time.Now().UTC()
	rec.Status = StatusVerified
	rec.Code = ""
	rec.CodeExpiresAt = time.Time{}
	rec.VerifiedAt = now

	if err := m.store.Save(rec); err != nil {
		return nil, fmt.Errorf("save verified record: %w", err)
	}
	return rec, nil
}

// IsVerified returns true if the given user has a verified pairing.
func (m *Manager) IsVerified(channel, userID string) bool {
	rec, err := m.store.Get(strings.TrimSpace(channel), strings.TrimSpace(userID))
	if err != nil {
		return false
	}
	return rec.Status == StatusVerified
}

// Revoke marks a verified pairing as revoked.
func (m *Manager) Revoke(channel, userID string) error {
	channel = strings.TrimSpace(channel)
	userID = strings.TrimSpace(userID)

	rec, err := m.store.Get(channel, userID)
	if err != nil {
		return fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}

	rec.Status = StatusRevoked
	rec.Code = ""
	rec.CodeExpiresAt = time.Time{}
	if err := m.store.Save(rec); err != nil {
		return fmt.Errorf("save revoked record: %w", err)
	}
	return nil
}

// List returns all pairing records.
func (m *Manager) List() ([]PairingRecord, error) {
	return m.store.List()
}

// Store returns the underlying Store.
func (m *Manager) Store() Store {
	return m.store
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateCode produces a cryptographically random 6-digit code.
func generateCode() (string, error) {
	// Range: [0, codeMax - codeMin + 1)
	span := big.NewInt(int64(codeMax - codeMin + 1))
	n, err := rand.Int(rand.Reader, span)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", codeLength, n.Int64()+int64(codeMin)), nil
}
