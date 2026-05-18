package pairing

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status represents the state of a DM pairing record.
type Status string

const (
	StatusPending  Status = "pending"
	StatusVerified Status = "verified"
	StatusRevoked  Status = "revoked"
)

// ---------------------------------------------------------------------------
// PairingRecord
// ---------------------------------------------------------------------------

// PairingRecord tracks the pairing state for a single DM user.
type PairingRecord struct {
	ID            string    `json:"id"`
	Channel       string    `json:"channel"` // "slack", "telegram", etc.
	UserID        string    `json:"user_id"` // channel-specific user ID
	DisplayName   string    `json:"display_name,omitempty"`
	Status        Status    `json:"status"`
	Code          string    `json:"code,omitempty"` // verification code (cleared after verify)
	CodeExpiresAt time.Time `json:"code_expires_at,omitempty"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Store interface
// ---------------------------------------------------------------------------

// Store persists pairing records.
type Store interface {
	Get(channel, userID string) (*PairingRecord, error)
	GetByCode(code string) (*PairingRecord, error)
	Save(rec *PairingRecord) error
	List() ([]PairingRecord, error)
	Delete(channel, userID string) error
}

// ---------------------------------------------------------------------------
// InMemoryStore
// ---------------------------------------------------------------------------

// InMemoryStore is a thread-safe in-memory implementation of Store.
type InMemoryStore struct {
	mu      sync.RWMutex
	records map[string]*PairingRecord // key: "channel:userID"
}

// NewInMemoryStore creates a new empty InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		records: make(map[string]*PairingRecord),
	}
}

func storeKey(channel, userID string) string {
	return channel + ":" + userID
}

func (s *InMemoryStore) Get(channel, userID string) (*PairingRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[storeKey(channel, userID)]
	if !ok {
		return nil, fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	cp := *rec
	return &cp, nil
}

func (s *InMemoryStore) GetByCode(code string) (*PairingRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rec := range s.records {
		if rec.Code == code {
			cp := *rec
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("pairing record not found for code")
}

func (s *InMemoryStore) Save(rec *PairingRecord) error {
	if rec == nil {
		return fmt.Errorf("pairing record is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *rec
	s.records[storeKey(rec.Channel, rec.UserID)] = &cp
	return nil
}

func (s *InMemoryStore) List() ([]PairingRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PairingRecord, 0, len(s.records))
	for _, rec := range s.records {
		result = append(result, *rec)
	}
	return result, nil
}

func (s *InMemoryStore) Delete(channel, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storeKey(channel, userID)
	if _, ok := s.records[key]; !ok {
		return fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	delete(s.records, key)
	return nil
}
