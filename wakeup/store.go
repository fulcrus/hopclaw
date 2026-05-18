package wakeup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	storeFileVersion = 1
	storeFileMode    = 0o600
	storeDirMode     = 0o700
)

type StoreFile struct {
	Version  int       `json:"version"`
	Triggers []Trigger `json:"triggers"`
}

type Store struct {
	mu       sync.RWMutex
	path     string
	triggers []Trigger
}

func NewStore(path string) *Store {
	return &Store{
		path:     path,
		triggers: make([]Trigger, 0),
	}
}

func Load(path string) (*Store, error) {
	s := NewStore(path)
	if path == "" {
		return s, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("wakeup store: read %s: %w", path, err)
	}

	var sf StoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("wakeup store: parse %s: %w", path, err)
	}
	if sf.Triggers == nil {
		sf.Triggers = make([]Trigger, 0)
	}
	s.triggers = sf.Triggers
	return s, nil
}

func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}

	s.mu.RLock()
	sf := StoreFile{
		Version:  storeFileVersion,
		Triggers: s.triggers,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("wakeup store: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return fmt.Errorf("wakeup store: create dir %s: %w", dir, err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, storeFileMode); err != nil {
		return fmt.Errorf("wakeup store: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("wakeup store: rename: %w", err)
	}
	return nil
}

func (s *Store) List() []Trigger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Trigger, len(s.triggers))
	copy(out, s.triggers)
	return out
}

func (s *Store) Get(id string) (*Trigger, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, trigger := range s.triggers {
		if trigger.ID == id {
			cp := trigger
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("wakeup: trigger %s: %w", id, ErrNotFound)
}

func (s *Store) Add(trigger Trigger) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.triggers {
		if existing.ID == trigger.ID {
			return fmt.Errorf("wakeup: trigger %s: %w", trigger.ID, ErrDuplicateID)
		}
	}
	s.triggers = append(s.triggers, trigger)
	return nil
}

func (s *Store) Update(id string, fn func(*Trigger)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.triggers {
		if s.triggers[i].ID == id {
			fn(&s.triggers[i])
			return nil
		}
	}
	return fmt.Errorf("wakeup: trigger %s: %w", id, ErrNotFound)
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, trigger := range s.triggers {
		if trigger.ID == id {
			s.triggers = append(s.triggers[:i], s.triggers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("wakeup: trigger %s: %w", id, ErrNotFound)
}
