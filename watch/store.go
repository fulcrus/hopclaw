package watch

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

type Store struct {
	mu      sync.RWMutex
	path    string
	watches []Watch
}

func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.watches = make([]Watch, 0)
			return s, nil
		}
		return nil, fmt.Errorf("watch store: read %s: %w", path, err)
	}
	var sf StoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("watch store: parse %s: %w", path, err)
	}
	if sf.Watches == nil {
		sf.Watches = make([]Watch, 0)
	}
	s.watches = sf.Watches
	return s, nil
}

func (s *Store) Save() error {
	s.mu.RLock()
	sf := StoreFile{
		Version: storeFileVersion,
		Watches: s.watches,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("watch store: marshal: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return fmt.Errorf("watch store: create dir %s: %w", dir, err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, storeFileMode); err != nil {
		return fmt.Errorf("watch store: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("watch store: rename: %w", err)
	}
	return nil
}

func (s *Store) List() []Watch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Watch, len(s.watches))
	copy(out, s.watches)
	return out
}

func (s *Store) Get(id string) (*Watch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.watches {
		if item.ID == id {
			cp := item
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("watch %s: %w", id, ErrNotFound)
}

func (s *Store) Add(item Watch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.watches {
		if existing.ID == item.ID {
			return fmt.Errorf("watch %s: %w", item.ID, ErrDuplicateID)
		}
	}
	s.watches = append(s.watches, item)
	return nil
}

func (s *Store) Update(id string, fn func(*Watch)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.watches {
		if s.watches[i].ID == id {
			fn(&s.watches[i])
			return nil
		}
	}
	return fmt.Errorf("watch %s: %w", id, ErrNotFound)
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.watches {
		if item.ID == id {
			s.watches = append(s.watches[:i], s.watches[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("watch %s: %w", id, ErrNotFound)
}

func (s *Store) Path() string {
	return s.path
}
