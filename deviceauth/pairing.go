package deviceauth

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Pairing constants
// ---------------------------------------------------------------------------

const (
	pairingCodeLength   = 6
	pairingCodeExpiry   = 10 * time.Minute
	pairingMaxPending   = 100
	pairingReapInterval = time.Minute

	// pairingCodeMax is the exclusive upper bound for a 6-digit code.
	pairingCodeMax = 1_000_000
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrPairingMaxPending indicates the maximum number of pending pairings
	// has been reached.
	ErrPairingMaxPending = fmt.Errorf("maximum pending pairings reached")

	// ErrPairingExpired indicates the pairing code has expired.
	ErrPairingExpired = fmt.Errorf("pairing code expired")

	// ErrPairingNotPending indicates the pairing is not in pending state.
	ErrPairingNotPending = fmt.Errorf("pairing is not pending")
)

// ---------------------------------------------------------------------------
// PairingManager
// ---------------------------------------------------------------------------

// PairingManager manages device pairing flows using short-lived verification
// codes.
type PairingManager struct {
	mu      sync.Mutex                // guards records and byCode
	records map[string]*PairingRecord // "channel:deviceID" -> record
	byCode  map[string]string         // code -> key
	store   *Store
	done    chan struct{}
}

// NewPairingManager creates a new pairing manager backed by the given store.
func NewPairingManager(store *Store) *PairingManager {
	return &PairingManager{
		records: make(map[string]*PairingRecord),
		byCode:  make(map[string]string),
		store:   store,
		done:    make(chan struct{}),
	}
}

// Start begins the background goroutine that reaps expired pairing records.
func (m *PairingManager) Start() {
	go func() {
		ticker := time.NewTicker(pairingReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.reapExpired()
			case <-m.done:
				return
			}
		}
	}()
}

// Stop signals the reap goroutine to exit.
func (m *PairingManager) Stop() {
	close(m.done)
}

// ---------------------------------------------------------------------------
// Pairing lifecycle
// ---------------------------------------------------------------------------

// InitiatePairing creates a new pairing record with a 6-digit verification
// code. If a pending pairing already exists for the same channel and device,
// it is replaced.
func (m *PairingManager) InitiatePairing(channel, deviceID string) (*PairingRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := pairingKey(channel, deviceID)

	// If an existing pending record is being replaced, clean up its code index.
	if existing, ok := m.records[key]; ok {
		delete(m.byCode, existing.Code)
	} else if m.pendingCountLocked() >= pairingMaxPending {
		return nil, ErrPairingMaxPending
	}

	code, err := m.generateCode()
	if err != nil {
		return nil, fmt.Errorf("generate pairing code: %w", err)
	}

	now := time.Now().UTC()
	rec := &PairingRecord{
		DeviceID:  deviceID,
		Channel:   channel,
		Code:      code,
		Status:    PairingPending,
		CreatedAt: now,
		ExpiresAt: now.Add(pairingCodeExpiry),
	}

	m.records[key] = rec
	m.byCode[code] = key
	return rec, nil
}

// VerifyCode verifies a 6-digit pairing code. On success the device is
// registered as trusted in the store and the record status is set to verified.
func (m *PairingManager) VerifyCode(code string) (*PairingRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.byCode[code]
	if !ok {
		return nil, fmt.Errorf("pairing code %q: %w", code, ErrNotFound)
	}

	rec, ok := m.records[key]
	if !ok {
		// Defensive: code index out of sync.
		delete(m.byCode, code)
		return nil, fmt.Errorf("pairing record for code %q: %w", code, ErrNotFound)
	}

	if rec.Status != PairingPending {
		return nil, ErrPairingNotPending
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		rec.Status = PairingExpired
		delete(m.byCode, code)
		return nil, ErrPairingExpired
	}

	rec.Status = PairingVerified
	rec.VerifiedAt = time.Now().UTC()
	delete(m.byCode, code)

	// Register the device as trusted in the store.
	device := &DeviceIdentity{
		DeviceID:  rec.DeviceID,
		Trusted:   true,
		CreatedAt: rec.VerifiedAt,
	}
	if err := m.store.RegisterDevice(device); err != nil {
		return nil, fmt.Errorf("register paired device: %w", err)
	}

	return rec, nil
}

// RevokePairing revokes a pairing for the given channel and device.
func (m *PairingManager) RevokePairing(channel, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := pairingKey(channel, deviceID)
	rec, ok := m.records[key]
	if !ok {
		return fmt.Errorf("pairing %q: %w", key, ErrNotFound)
	}

	delete(m.byCode, rec.Code)
	rec.Status = PairingRevoked
	return nil
}

// IsVerified reports whether a pairing for the given channel and device has
// been verified.
func (m *PairingManager) IsVerified(channel, deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.records[pairingKey(channel, deviceID)]
	return ok && rec.Status == PairingVerified
}

// GetByCode returns the pairing record for a verification code.
func (m *PairingManager) GetByCode(code string) (*PairingRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.byCode[code]
	if !ok {
		return nil, false
	}
	rec, ok := m.records[key]
	return rec, ok
}

// ListPairings returns a snapshot of all pairing records.
func (m *PairingManager) ListPairings() []*PairingRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*PairingRecord, 0, len(m.records))
	for _, rec := range m.records {
		result = append(result, rec)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		if result[i].Channel != result[j].Channel {
			return result[i].Channel < result[j].Channel
		}
		return result[i].DeviceID < result[j].DeviceID
	})
	return result
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

// reapExpired removes expired pending records.
func (m *PairingManager) reapExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for key, rec := range m.records {
		if rec.Status == PairingPending && now.After(rec.ExpiresAt) {
			rec.Status = PairingExpired
			delete(m.byCode, rec.Code)
			delete(m.records, key)
		}
	}
}

// generateCode produces a cryptographically random 6-digit code, retrying if
// the code collides with an existing one. Must be called with m.mu held.
func (m *PairingManager) generateCode() (string, error) {
	const maxAttempts = 10
	for range maxAttempts {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(pairingCodeMax)))
		if err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		code := fmt.Sprintf("%0*d", pairingCodeLength, n.Int64())
		if _, exists := m.byCode[code]; !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique code after %d attempts", maxAttempts)
}

// pendingCountLocked returns the number of pending pairings. Must be called
// with m.mu held.
func (m *PairingManager) pendingCountLocked() int {
	count := 0
	for _, rec := range m.records {
		if rec.Status == PairingPending {
			count++
		}
	}
	return count
}

func pairingKey(channel, deviceID string) string {
	return channel + ":" + deviceID
}
