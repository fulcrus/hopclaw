package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	memoryNotebookTitle           = "# HopClaw Memory\n\n_Projection only. DurableFact is the source of truth._\n\n"
	memoryNotebookEmptyState      = "_No stored memory yet._\n"
	memoryNotebookValueMaxRunes   = 160
	memoryNotebookTimestampLayout = time.RFC3339
)

// MirroredMemoryStore wraps a MemoryStore and keeps a human-readable markdown
// notebook projection in sync so operators and users can inspect persistent
// memory without querying internal APIs directly.
type MirroredMemoryStore struct {
	inner        MemoryStore
	notebookPath string
	mu           sync.Mutex
}

func NewMirroredMemoryStore(inner MemoryStore, notebookPath string) (*MirroredMemoryStore, error) {
	trimmedPath := strings.TrimSpace(notebookPath)
	if inner == nil {
		return nil, fmt.Errorf("mirrored memory store requires inner store")
	}
	if trimmedPath == "" {
		return nil, fmt.Errorf("mirrored memory store requires notebook path")
	}
	return &MirroredMemoryStore{
		inner:        inner,
		notebookPath: trimmedPath,
	}, nil
}

func (s *MirroredMemoryStore) StoreType() string {
	if provider, ok := s.inner.(MemoryStoreMetadataProvider); ok {
		return "mirrored/" + provider.StoreType()
	}
	return "mirrored"
}

func (s *MirroredMemoryStore) Reindex(ctx context.Context, force bool) (int, error) {
	indexer, ok := s.inner.(MemoryIndexer)
	if !ok {
		entries, err := s.List(ctx)
		if err != nil {
			return 0, err
		}
		return len(entries), nil
	}
	return indexer.Reindex(ctx, force)
}

func (s *MirroredMemoryStore) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncLocked(ctx)
}

func (s *MirroredMemoryStore) Get(ctx context.Context, key string) (*MemoryEntry, error) {
	return s.inner.Get(ctx, key)
}

func (s *MirroredMemoryStore) Set(ctx context.Context, key, value string) error {
	if err := s.inner.Set(ctx, key, value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLockedBestEffort(ctx)
	return nil
}

func (s *MirroredMemoryStore) Delete(ctx context.Context, key string) error {
	if err := s.inner.Delete(ctx, key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLockedBestEffort(ctx)
	return nil
}

func (s *MirroredMemoryStore) Search(ctx context.Context, query string) ([]MemoryEntry, error) {
	return s.inner.Search(ctx, query)
}

func (s *MirroredMemoryStore) SemanticSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error) {
	return s.inner.SemanticSearch(ctx, query, limit)
}

func (s *MirroredMemoryStore) SemanticSearchMMR(ctx context.Context, query string, limit int, lambda float64) ([]MemoryEntry, error) {
	return s.inner.SemanticSearchMMR(ctx, query, limit, lambda)
}

func (s *MirroredMemoryStore) HasEmbedding() bool {
	if provider, ok := s.inner.(interface{ HasEmbedding() bool }); ok {
		return provider.HasEmbedding()
	}
	return false
}

func (s *MirroredMemoryStore) EmbeddingClient() EmbeddingClient {
	if provider, ok := s.inner.(interface{ EmbeddingClient() EmbeddingClient }); ok {
		return provider.EmbeddingClient()
	}
	return nil
}

func (s *MirroredMemoryStore) VectorStats() (int, int) {
	if provider, ok := s.inner.(interface{ VectorStats() (int, int) }); ok {
		return provider.VectorStats()
	}
	return 0, 0
}

func (s *MirroredMemoryStore) ListContextViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.ContextView, error) {
	provider, ok := s.inner.(durablefact.ContextViewReader)
	if !ok {
		return nil, fmt.Errorf("durable context views not supported")
	}
	return provider.ListContextViews(ctx, filter)
}

func (s *MirroredMemoryStore) ListOperatorViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, error) {
	provider, ok := s.inner.(durablefact.OperatorViewReader)
	if !ok {
		return nil, fmt.Errorf("durable operator views not supported")
	}
	return provider.ListOperatorViews(ctx, filter)
}

func (s *MirroredMemoryStore) List(ctx context.Context) ([]MemoryEntry, error) {
	return s.inner.List(ctx)
}

func (s *MirroredMemoryStore) ListFiltered(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error) {
	if governed, ok := s.inner.(ManagedMemoryStore); ok {
		return governed.ListFiltered(ctx, filter)
	}
	entries, err := s.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	entries, err = normalizeMemoryEntries(entries)
	if err != nil {
		return nil, err
	}
	filtered := make([]MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if filter.ManagedOnly && !entry.Managed {
			continue
		}
		if strings.TrimSpace(filter.Namespace) != "" && entry.Namespace != strings.TrimSpace(filter.Namespace) {
			continue
		}
		if strings.TrimSpace(filter.ScopeKey) != "" && entry.ScopeKey != strings.TrimSpace(filter.ScopeKey) {
			continue
		}
		if strings.TrimSpace(filter.Query) != "" && !memoryEntryMatchesQuery(entry, strings.TrimSpace(filter.Query)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func (s *MirroredMemoryStore) UpsertRecord(ctx context.Context, record MemoryRecord) (*MemoryEntry, error) {
	if governed, ok := s.inner.(ManagedMemoryStore); ok {
		entry, err := governed.UpsertRecord(ctx, record)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.syncLockedBestEffort(ctx)
		return entry, nil
	}
	if err := s.Set(ctx, record.Key, record.Value); err != nil {
		return nil, err
	}
	return s.Get(ctx, record.Key)
}

func (s *MirroredMemoryStore) TouchMemoryVerification(ctx context.Context, key string, passed bool) (*MemoryEntry, error) {
	verifier, ok := s.inner.(MemoryVerificationStore)
	if !ok {
		return nil, fmt.Errorf("memory verification updates not supported")
	}
	entry, err := verifier.TouchMemoryVerification(ctx, key, passed)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncLockedBestEffort(ctx)
	return entry, nil
}

func (s *MirroredMemoryStore) AgentUpsert(ctx context.Context, record MemoryRecord) (*MemoryEntry, MemoryMutationResult, error) {
	upserter, ok := s.inner.(AgentUpserter)
	if !ok {
		entry, err := s.UpsertRecord(ctx, record)
		return entry, MemoryMutationResult{Action: MutationApplied}, err
	}
	entry, result, err := upserter.AgentUpsert(ctx, record)
	if err != nil {
		return nil, result, err
	}
	if result.Action == MutationApplied {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.syncLockedBestEffort(ctx)
	}
	return entry, result, nil
}

func (s *MirroredMemoryStore) NotebookSnapshot(ctx context.Context) (*MemoryNotebookSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.notebookPath); err != nil {
		if os.IsNotExist(err) {
			if syncErr := s.syncLocked(ctx); syncErr != nil {
				return nil, syncErr
			}
		} else {
			return nil, fmt.Errorf("stat memory notebook: %w", err)
		}
	}
	body, err := os.ReadFile(s.notebookPath)
	if err != nil {
		return nil, fmt.Errorf("read memory notebook: %w", err)
	}
	info, err := os.Stat(s.notebookPath)
	if err != nil {
		return nil, fmt.Errorf("stat memory notebook: %w", err)
	}
	return &MemoryNotebookSnapshot{
		Path:      s.notebookPath,
		Content:   string(body),
		UpdatedAt: info.ModTime().UTC(),
	}, nil
}

func (s *MirroredMemoryStore) syncLocked(ctx context.Context) error {
	entries, err := s.inner.List(ctx)
	if err != nil {
		return fmt.Errorf("list mirrored memory entries: %w", err)
	}
	entries, err = normalizeMemoryEntries(entries)
	if err != nil {
		return fmt.Errorf("normalize mirrored memory entries: %w", err)
	}
	content := renderMemoryNotebook(entries)
	if err := os.MkdirAll(filepath.Dir(s.notebookPath), 0o755); err != nil {
		return fmt.Errorf("create memory notebook dir: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(s.notebookPath), "memory-notebook-*.tmp")
	if err != nil {
		return fmt.Errorf("create memory notebook tmp: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write memory notebook tmp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close memory notebook tmp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod memory notebook tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.notebookPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace memory notebook: %w", err)
	}
	return nil
}

func (s *MirroredMemoryStore) syncLockedBestEffort(ctx context.Context) {
	if err := s.syncLocked(ctx); err != nil {
		log.Warn("memory notebook sync failed after store mutation", "path", s.notebookPath, "error", err)
	}
}

func renderMemoryNotebook(entries []MemoryEntry) string {
	var b strings.Builder
	b.WriteString(memoryNotebookTitle)
	if len(entries) == 0 {
		b.WriteString(memoryNotebookEmptyState)
		return b.String()
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Namespace != entries[j].Namespace {
			return entries[i].Namespace < entries[j].Namespace
		}
		if entries[i].ScopeKey != entries[j].ScopeKey {
			return entries[i].ScopeKey < entries[j].ScopeKey
		}
		return entries[i].Key < entries[j].Key
	})
	currentNamespace := ""
	for _, entry := range entries {
		namespace := normalize.FirstNonEmpty(strings.TrimSpace(entry.Namespace), memoryNamespaceGeneral)
		if namespace != currentNamespace {
			if currentNamespace != "" {
				b.WriteString("\n")
			}
			currentNamespace = namespace
			b.WriteString("## ")
			b.WriteString(titleCaseWords(namespace))
			b.WriteString("\n\n")
			b.WriteString("| Label | Scope | Value | Updated At |\n")
			b.WriteString("| --- | --- | --- | --- |\n")
		}
		b.WriteString("| ")
		b.WriteString(escapeMarkdownTable(normalize.FirstNonEmpty(entry.Label, entry.Key)))
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTable(normalize.FirstNonEmpty(entry.ScopeKey, memoryDefaultScopeKey)))
		b.WriteString(" | ")
		b.WriteString(escapeMarkdownTable(truncateRunes(singleLine(entry.Value), memoryNotebookValueMaxRunes)))
		b.WriteString(" | ")
		b.WriteString(entry.UpdatedAt.UTC().Format(memoryNotebookTimestampLayout))
		b.WriteString(" |\n")
	}
	return b.String()
}

func singleLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func escapeMarkdownTable(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	if value == "" {
		return " "
	}
	return value
}

func titleCaseWords(value string) string {
	parts := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
