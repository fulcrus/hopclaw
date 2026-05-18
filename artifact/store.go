package artifact

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

type Blob struct {
	ID          string         `json:"id"`
	URI         string         `json:"uri"`
	Kind        string         `json:"kind"`
	ContentType string         `json:"content_type"`
	Size        int64          `json:"size"`
	CreatedAt   time.Time      `json:"created_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type PutRequest struct {
	Kind        string
	ContentType string
	Body        []byte
	Metadata    map[string]any
}

type Store interface {
	Put(ctx context.Context, req PutRequest) (*Blob, error)
	Get(ctx context.Context, id string) (*Blob, error)
	Read(ctx context.Context, id string) ([]byte, string, error)
	List(ctx context.Context, filter ListFilter) ([]*Blob, error)
	Delete(ctx context.Context, id string) error
}

type InMemoryStore struct {
	mu    sync.RWMutex
	blobs map[string]*Blob
	body  map[string][]byte
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		blobs: make(map[string]*Blob),
		body:  make(map[string][]byte),
	}
}

func (s *InMemoryStore) Put(_ context.Context, req PutRequest) (*Blob, error) {
	if _, err := MarshalMetadataJSON(req.Metadata); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	blob := &Blob{
		ID:          id,
		URI:         URI(id),
		Kind:        defaultString(req.Kind, "tool_output"),
		ContentType: defaultString(req.ContentType, "text/plain; charset=utf-8"),
		Size:        int64(len(req.Body)),
		CreatedAt:   time.Now().UTC(),
		Metadata:    supportmaps.Clone(req.Metadata),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[id] = blob
	s.body[id] = append([]byte(nil), req.Body...)
	return cloneBlob(blob), nil
}

func (s *InMemoryStore) Get(_ context.Context, id string) (*Blob, error) {
	id = ParseID(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	blob, ok := s.blobs[id]
	if !ok {
		return nil, fmt.Errorf("artifact %q not found", id)
	}
	return cloneBlob(blob), nil
}

func (s *InMemoryStore) Read(_ context.Context, id string) ([]byte, string, error) {
	id = ParseID(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	blob, ok := s.blobs[id]
	if !ok {
		return nil, "", fmt.Errorf("artifact %q not found", id)
	}
	body := s.body[id]
	return append([]byte(nil), body...), blob.ContentType, nil
}

func (s *InMemoryStore) Delete(_ context.Context, id string) error {
	id = ParseID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blobs[id]; !ok {
		return fmt.Errorf("artifact %q not found", id)
	}
	delete(s.blobs, id)
	delete(s.body, id)
	return nil
}

type FileStore struct {
	root string
}

func NewFileStore(root string) (*FileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	store := &FileStore{root: root}
	if err := store.cleanupIncompleteArtifacts(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) Put(_ context.Context, req PutRequest) (*Blob, error) {
	if _, err := MarshalMetadataJSON(req.Metadata); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	blob := &Blob{
		ID:          id,
		URI:         URI(id),
		Kind:        defaultString(req.Kind, "tool_output"),
		ContentType: defaultString(req.ContentType, "text/plain; charset=utf-8"),
		Size:        int64(len(req.Body)),
		CreatedAt:   time.Now().UTC(),
		Metadata:    supportmaps.Clone(req.Metadata),
	}
	metaPath, bodyPath := s.paths(id)
	tempMetaPath, tempBodyPath := s.tempPaths(id)
	data, err := json.Marshal(blob)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tempBodyPath, req.Body, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(tempMetaPath, data, 0o644); err != nil {
		_ = os.Remove(tempBodyPath)
		return nil, err
	}
	if err := os.Rename(tempBodyPath, bodyPath); err != nil {
		_ = os.Remove(tempBodyPath)
		_ = os.Remove(tempMetaPath)
		return nil, err
	}
	if err := os.Rename(tempMetaPath, metaPath); err != nil {
		_ = os.Remove(tempMetaPath)
		_ = os.Remove(bodyPath)
		return nil, err
	}
	return cloneBlob(blob), nil
}

func (s *FileStore) Get(_ context.Context, id string) (*Blob, error) {
	id = ParseID(id)
	metaPath, _ := s.paths(id)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("artifact %q not found", id)
		}
		return nil, err
	}
	var blob Blob
	if err := json.Unmarshal(data, &blob); err != nil {
		return nil, err
	}
	return cloneBlob(&blob), nil
}

func (s *FileStore) Read(ctx context.Context, id string) ([]byte, string, error) {
	id = ParseID(id)
	blob, err := s.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	_, bodyPath := s.paths(id)
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("artifact %q not found", id)
		}
		return nil, "", err
	}
	return body, blob.ContentType, nil
}

func (s *FileStore) Delete(_ context.Context, id string) error {
	id = ParseID(id)
	metaPath, bodyPath := s.paths(id)
	metaErr := os.Remove(metaPath)
	bodyErr := os.Remove(bodyPath)
	if metaErr != nil && !os.IsNotExist(metaErr) {
		return metaErr
	}
	if bodyErr != nil && !os.IsNotExist(bodyErr) {
		return bodyErr
	}
	if os.IsNotExist(metaErr) && os.IsNotExist(bodyErr) {
		return fmt.Errorf("artifact %q not found", id)
	}
	return nil
}

func (s *FileStore) paths(id string) (string, string) {
	base := filepath.Join(s.root, id)
	return base + ".json", base + ".bin"
}

func (s *FileStore) tempPaths(id string) (string, string) {
	base := filepath.Join(s.root, id)
	return base + ".json.tmp", base + ".bin.tmp"
}

func (s *FileStore) cleanupIncompleteArtifacts() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	metas := make(map[string]struct{})
	bodies := make(map[string]struct{})
	tempMetas := make(map[string]struct{})
	tempBodies := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".json"):
			metas[strings.TrimSuffix(name, ".json")] = struct{}{}
		case strings.HasSuffix(name, ".bin"):
			bodies[strings.TrimSuffix(name, ".bin")] = struct{}{}
		case strings.HasSuffix(name, ".json.tmp"):
			tempMetas[strings.TrimSuffix(name, ".json.tmp")] = struct{}{}
		case strings.HasSuffix(name, ".bin.tmp"):
			tempBodies[strings.TrimSuffix(name, ".bin.tmp")] = struct{}{}
		}
	}
	for id := range metas {
		metaPath, bodyPath := s.paths(id)
		tempMetaPath, tempBodyPath := s.tempPaths(id)
		if _, ok := bodies[id]; ok {
			_ = os.Remove(tempMetaPath)
			_ = os.Remove(tempBodyPath)
			continue
		}
		if _, ok := tempBodies[id]; ok {
			if err := os.Rename(tempBodyPath, bodyPath); err == nil {
				_ = os.Remove(tempMetaPath)
				bodies[id] = struct{}{}
				continue
			}
		}
		_ = os.Remove(tempMetaPath)
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range bodies {
		metaPath, bodyPath := s.paths(id)
		tempMetaPath, _ := s.tempPaths(id)
		if _, ok := metas[id]; ok {
			_ = os.Remove(tempMetaPath)
			continue
		}
		if _, ok := tempMetas[id]; ok {
			if err := os.Rename(tempMetaPath, metaPath); err == nil {
				metas[id] = struct{}{}
				continue
			}
		}
		if err := os.Remove(bodyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range metas {
		if _, ok := bodies[id]; ok {
			continue
		}
		metaPath, _ := s.paths(id)
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range bodies {
		if _, ok := metas[id]; ok {
			continue
		}
		_, bodyPath := s.paths(id)
		if err := os.Remove(bodyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range tempMetas {
		if _, ok := metas[id]; ok {
			continue
		}
		if _, ok := bodies[id]; ok {
			continue
		}
		tempMetaPath, _ := s.tempPaths(id)
		if err := os.Remove(tempMetaPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range tempBodies {
		if _, ok := metas[id]; ok {
			continue
		}
		if _, ok := bodies[id]; ok {
			continue
		}
		_, tempBodyPath := s.tempPaths(id)
		if err := os.Remove(tempBodyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func URI(id string) string {
	id = ParseID(id)
	return "artifact://local/" + id
}

func ParseID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "artifact://local/")
	return value
}

func MarshalMetadataJSON(metadata map[string]any) (string, error) {
	if metadata == nil {
		return "{}", nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("artifact metadata must be JSON serializable: %w", err)
	}
	return string(data), nil
}

func cloneBlob(in *Blob) *Blob {
	if in == nil {
		return nil
	}
	return &Blob{
		ID:          in.ID,
		URI:         in.URI,
		Kind:        in.Kind,
		ContentType: in.ContentType,
		Size:        in.Size,
		CreatedAt:   in.CreatedAt,
		Metadata:    supportmaps.Clone(in.Metadata),
	}
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func newID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
