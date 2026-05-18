package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	sourceIndexFileName = "sources.json"
	chunksDirName       = "chunks"
)

type FileStore struct {
	root    string
	mu      sync.RWMutex
	sources map[string]Source
}

func NewFileStore(root string) (*FileStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("knowledge store root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, chunksDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create knowledge store root: %w", err)
	}
	store := &FileStore{
		root:    root,
		sources: make(map[string]Source),
	}
	if err := store.loadSourcesLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) ListSources(_ context.Context) ([]Source, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Source, 0, len(s.sources))
	for _, item := range s.sources {
		out = append(out, cloneSource(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *FileStore) GetSource(_ context.Context, id string) (*Source, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.sources[strings.TrimSpace(id)]
	if !ok {
		return nil, nil
	}
	cloned := cloneSource(item)
	return &cloned, nil
}

func (s *FileStore) UpsertSource(_ context.Context, source Source) (Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source.ID = strings.TrimSpace(source.ID)
	if source.ID == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	source.Name = strings.TrimSpace(source.Name)
	if source.Name == "" {
		return Source{}, fmt.Errorf("source name is required")
	}
	source.Path = strings.TrimSpace(source.Path)
	source.URLs = uniqueStrings(source.URLs)
	source.IncludeGlobs = uniqueStrings(source.IncludeGlobs)
	source.ExcludeGlobs = uniqueStrings(source.ExcludeGlobs)
	if source.CreatedAt.IsZero() {
		if existing, ok := s.sources[source.ID]; ok && !existing.CreatedAt.IsZero() {
			source.CreatedAt = existing.CreatedAt
		}
	}
	s.sources[source.ID] = cloneSource(source)
	if err := s.persistSourcesLocked(); err != nil {
		return Source{}, err
	}
	return cloneSource(source), nil
}

func (s *FileStore) DeleteSource(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	delete(s.sources, id)
	if err := s.persistSourcesLocked(); err != nil {
		return err
	}
	if err := os.Remove(s.chunksPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

func (s *FileStore) ReplaceChunks(_ context.Context, sourceID string, chunks []Chunk, source Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("source id is required")
	}
	body, err := json.MarshalIndent(chunks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chunks: %w", err)
	}
	if err := atomicWriteFile(s.chunksPath(sourceID), append(body, '\n'), 0o644); err != nil {
		return err
	}
	s.sources[sourceID] = cloneSource(source)
	return s.persistSourcesLocked()
}

func (s *FileStore) ListChunks(_ context.Context, sourceID string) ([]Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadChunksLocked(strings.TrimSpace(sourceID))
}

func (s *FileStore) ListAllChunks(_ context.Context) ([]Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Chunk, 0, 64)
	for id := range s.sources {
		items, err := s.loadChunksLocked(id)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func (s *FileStore) loadSourcesLocked() error {
	body, err := os.ReadFile(filepath.Join(s.root, sourceIndexFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read sources index: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	var items []Source
	if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("decode sources index: %w", err)
	}
	for _, item := range items {
		s.sources[item.ID] = cloneSource(item)
	}
	return nil
}

func (s *FileStore) persistSourcesLocked() error {
	items := make([]Source, 0, len(s.sources))
	for _, item := range s.sources {
		items = append(items, cloneSource(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	body, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sources index: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.root, sourceIndexFileName), append(body, '\n'), 0o644)
}

func (s *FileStore) loadChunksLocked(sourceID string) ([]Chunk, error) {
	body, err := os.ReadFile(s.chunksPath(sourceID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chunks for %s: %w", sourceID, err)
	}
	if len(body) == 0 {
		return nil, nil
	}
	var items []Chunk
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode chunks for %s: %w", sourceID, err)
	}
	return items, nil
}

func (s *FileStore) chunksPath(sourceID string) string {
	return filepath.Join(s.root, chunksDirName, sourceID+".json")
}

func atomicWriteFile(path string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "knowledge-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

func cloneSource(source Source) Source {
	source.URLs = append([]string(nil), source.URLs...)
	source.Config = cloneConfigMap(source.Config)
	source.IncludeGlobs = append([]string(nil), source.IncludeGlobs...)
	source.ExcludeGlobs = append([]string(nil), source.ExcludeGlobs...)
	return source
}

func cloneConfigMap(config map[string]any) map[string]any {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]any, len(config))
	for key, value := range config {
		out[key] = cloneConfigValue(value)
	}
	return out
}

func cloneConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneConfigMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneConfigValue(item))
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
