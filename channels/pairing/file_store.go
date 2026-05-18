package pairing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore persists pairing records to a JSON file.
type FileStore struct {
	path string

	mu      sync.RWMutex
	loaded  bool
	records map[string]*PairingRecord
}

func NewFileStore(path string) *FileStore {
	return &FileStore{
		path:    path,
		records: make(map[string]*PairingRecord),
	}
}

func (s *FileStore) Get(channel, userID string) (*PairingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	rec, ok := s.records[storeKey(channel, userID)]
	if !ok {
		return nil, fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	cp := *rec
	return &cp, nil
}

func (s *FileStore) GetByCode(code string) (*PairingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	for _, rec := range s.records {
		if rec.Code == code {
			cp := *rec
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("pairing record not found for code")
}

func (s *FileStore) Save(rec *PairingRecord) error {
	if rec == nil {
		return fmt.Errorf("pairing record is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	cp := *rec
	s.records[storeKey(rec.Channel, rec.UserID)] = &cp
	return s.persistLocked()
}

func (s *FileStore) List() ([]PairingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	result := make([]PairingRecord, 0, len(s.records))
	for _, rec := range s.records {
		result = append(result, *rec)
	}
	return result, nil
}

func (s *FileStore) Delete(channel, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	key := storeKey(channel, userID)
	if _, ok := s.records[key]; !ok {
		return fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	delete(s.records, key)
	return s.persistLocked()
}

func (s *FileStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	if s.path == "" {
		s.loaded = true
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.loaded = true
			return nil
		}
		return fmt.Errorf("read pairing store %s: %w", s.path, err)
	}
	var records map[string]*PairingRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("decode pairing store %s: %w", s.path, err)
	}
	if records == nil {
		records = make(map[string]*PairingRecord)
	}
	s.records = records
	s.loaded = true
	return nil
}

func (s *FileStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
