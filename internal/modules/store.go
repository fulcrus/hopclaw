package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
)

type StoreSnapshot struct {
	Version string
	Catalog Catalog
}

type Store struct {
	mu       sync.RWMutex
	snapshot StoreSnapshot
}

func NewStore(catalog Catalog) *Store {
	return &Store{
		snapshot: newStoreSnapshot(catalog),
	}
}

func (s *Store) Swap(catalog Catalog) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = newStoreSnapshot(catalog)
}

func (s *Store) SwapWith(fn func(Catalog) Catalog) {
	if s == nil || fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = newStoreSnapshot(fn(cloneCatalog(s.snapshot.Catalog)))
}

func (s *Store) Snapshot() Catalog {
	return s.SnapshotState().Catalog
}

func (s *Store) SnapshotState() StoreSnapshot {
	if s == nil {
		return StoreSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneStoreSnapshot(s.snapshot)
}

func (s *Store) Version() string {
	return strings.TrimSpace(s.SnapshotState().Version)
}

func (s *Store) ProjectionVersion() string {
	return s.Version()
}

func (s *Store) Len() int {
	return s.Snapshot().Len()
}

func (s *Store) Modules() []StaticModule {
	return s.Snapshot().Modules()
}

func (s *Store) Manifests() []Manifest {
	return s.Snapshot().Manifests()
}

func (s *Store) Contributions() Contributions {
	return s.Snapshot().Contributions()
}

func (s *Store) Find(id string) (StaticModule, bool) {
	return s.Snapshot().Find(id)
}

func cloneCatalog(catalog Catalog) Catalog {
	items := catalog.Modules()
	if len(items) == 0 {
		return Catalog{}
	}
	return BuildCatalog(items)
}

func cloneStoreSnapshot(snapshot StoreSnapshot) StoreSnapshot {
	return StoreSnapshot{
		Version: strings.TrimSpace(snapshot.Version),
		Catalog: cloneCatalog(snapshot.Catalog),
	}
}

func newStoreSnapshot(catalog Catalog) StoreSnapshot {
	cloned := cloneCatalog(catalog)
	return StoreSnapshot{
		Version: catalogVersion(cloned),
		Catalog: cloned,
	}
}

func catalogVersion(catalog Catalog) string {
	payload, err := json.Marshal(catalog.Modules())
	if err != nil {
		return "mod-unknown"
	}
	sum := sha256.Sum256(payload)
	return "mod-" + hex.EncodeToString(sum[:8])
}
