package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("cron")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	storeFileVersion = 1
	storeFileMode    = 0o600
	storeDirMode     = 0o700
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store provides thread-safe CRUD operations on cron jobs backed by a JSON file.
type Store struct {
	mu   sync.RWMutex // guards jobs and path
	path string
	jobs []Job
}

// Load reads jobs from path, creating an empty store file if it does not exist.
func Load(path string) (*Store, error) {
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.jobs = make([]Job, 0)
			return s, nil
		}
		return nil, fmt.Errorf("cron store: read %s: %w", path, err)
	}

	var sf StoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("cron store: parse %s: %w", path, err)
	}
	if sf.Jobs == nil {
		sf.Jobs = make([]Job, 0)
	}
	s.jobs = sf.Jobs
	return s, nil
}

// Save atomically writes the current job list to disk (write-to-temp then rename).
func (s *Store) Save() error {
	s.mu.RLock()
	sf := StoreFile{
		Version: storeFileVersion,
		Jobs:    s.jobs,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("cron store: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return fmt.Errorf("cron store: create dir %s: %w", dir, err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, storeFileMode); err != nil {
		return fmt.Errorf("cron store: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		// Fallback for systems where rename fails across volumes.
		logging.DebugIfErr(os.Remove(tmp), "remove temp cron store failed")
		return fmt.Errorf("cron store: rename: %w", err)
	}
	return nil
}

// List returns a copy of all jobs.
func (s *Store) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Job, len(s.jobs))
	copy(out, s.jobs)
	return out
}

// Get returns a pointer to a copy of the job with the given ID.
func (s *Store) Get(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.ID == id {
			cp := j
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("cron job %s: %w", id, ErrNotFound)
}

// Add inserts a new job. Returns ErrDuplicateID if the ID already exists.
func (s *Store) Add(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == job.ID {
			return fmt.Errorf("cron job %s: %w", job.ID, ErrDuplicateID)
		}
	}
	s.jobs = append(s.jobs, job)
	return nil
}

// Update applies fn to the job with the given ID in place.
func (s *Store) Update(id string, fn func(*Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			fn(&s.jobs[i])
			return nil
		}
	}
	return fmt.Errorf("cron job %s: %w", id, ErrNotFound)
}

// Remove deletes the job with the given ID.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, j := range s.jobs {
		if j.ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("cron job %s: %w", id, ErrNotFound)
}

// Path returns the file path the store reads from and writes to.
func (s *Store) Path() string {
	return s.path
}
