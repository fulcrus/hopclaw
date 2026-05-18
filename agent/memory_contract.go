package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/internal/support/ints"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	memoryNamespaceProfile   = "profile"
	memoryNamespaceWorkspace = "workspace"
	memoryNamespaceProject   = "project"
	memoryNamespaceTask      = "task"
	memoryNamespaceGeneral   = "general"

	memoryDefaultScopeKey      = "default"
	memoryDefaultProfileScope  = "user"
	memoryEnvelopePrefix       = "hc_memory_v1:"
	memoryDefaultSourceManual  = "manual"
	memoryDefaultSourceRuntime = "runtime"
	memoryPreviousValueLimit   = 8
	memoryEvidenceLimit        = 12
)

type MemoryFilter struct {
	Query       string `json:"query,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ScopeKey    string `json:"scope_key,omitempty"`
	ManagedOnly bool   `json:"managed_only,omitempty"`
}

type MemoryRecord struct {
	Key                   string                 `json:"key,omitempty"`
	FactClass             durablefact.FactClass  `json:"fact_class,omitempty"`
	Namespace             string                 `json:"namespace,omitempty"`
	ScopeKey              string                 `json:"scope_key,omitempty"`
	Field                 string                 `json:"field,omitempty"`
	Label                 string                 `json:"label,omitempty"`
	Value                 string                 `json:"value,omitempty"`
	Managed               bool                   `json:"managed,omitempty"`
	Source                string                 `json:"source,omitempty"`
	Score                 float64                `json:"score,omitempty"`
	State                 MemoryState            `json:"state,omitempty"`
	SupersededBy          string                 `json:"superseded_by,omitempty"`
	SessionKey            string                 `json:"session_key,omitempty"`
	ProjectID             string                 `json:"project_id,omitempty"`
	MediaRefs             []string               `json:"media_refs,omitempty"`
	UsedCount             int                    `json:"used_count,omitempty"`
	LastUsedAt            time.Time              `json:"last_used_at,omitempty"`
	CorrectionCount       int                    `json:"correction_count,omitempty"`
	VerificationPassCount int                    `json:"verification_pass_count,omitempty"`
	VerificationFailCount int                    `json:"verification_fail_count,omitempty"`
	Tags                  []string               `json:"tags,omitempty"`
	PreviousValues        []string               `json:"previous_values,omitempty"`
	Evidence              []MemoryRecordEvidence `json:"evidence,omitempty"`
	CreatedAt             time.Time              `json:"created_at,omitempty"`
	UpdatedAt             time.Time              `json:"updated_at,omitempty"`
}

type MemoryRecordEvidence struct {
	Source     string    `json:"source,omitempty"`
	Ref        string    `json:"ref,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Value      string    `json:"value,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

type MemoryNotebookSnapshot struct {
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// MutationAction describes the outcome of an agent memory mutation attempt.
type MutationAction string

const (
	MutationApplied MutationAction = "applied"
	MutationBlocked MutationAction = "blocked"
)

// MemoryMutationResult describes what happened when Agent tried to write memory.
type MemoryMutationResult struct {
	Action        MutationAction `json:"action"`
	Reason        string         `json:"reason,omitempty"`
	Suggest       string         `json:"suggest,omitempty"`
	ExistingValue string         `json:"existing_value,omitempty"`
	NewValue      string         `json:"new_value,omitempty"`
}

type AgentUpserter interface {
	AgentUpsert(ctx context.Context, record MemoryRecord) (*MemoryEntry, MemoryMutationResult, error)
}

type ManagedMemoryStore interface {
	UpsertRecord(ctx context.Context, record MemoryRecord) (*MemoryEntry, error)
	ListFiltered(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error)
}

type MemoryVerificationStore interface {
	TouchMemoryVerification(ctx context.Context, key string, passed bool) (*MemoryEntry, error)
}

type MemoryNotebookProvider interface {
	NotebookSnapshot(ctx context.Context) (*MemoryNotebookSnapshot, error)
}

type MemoryIndexer interface {
	Reindex(ctx context.Context, force bool) (int, error)
}

type MemoryStoreMetadataProvider interface {
	StoreType() string
}

type GovernedMemoryStore struct {
	inner MemoryStore
}

func NewGovernedMemoryStore(inner MemoryStore) *GovernedMemoryStore {
	if inner == nil {
		return nil
	}
	return &GovernedMemoryStore{inner: inner}
}

func (s *GovernedMemoryStore) Get(ctx context.Context, key string) (*MemoryEntry, error) {
	entry, err := s.inner.Get(ctx, strings.TrimSpace(key))
	if err != nil || entry == nil {
		return entry, err
	}
	normalized, _, normErr := decodeMemoryEntry(*entry)
	if normErr != nil {
		return nil, normErr
	}
	return &normalized, nil
}

func (s *GovernedMemoryStore) Set(ctx context.Context, key, value string) error {
	_, err := s.UpsertRecord(ctx, MemoryRecord{
		Key:     strings.TrimSpace(key),
		Value:   value,
		Managed: true,
		Source:  MemorySourceUser,
	})
	return err
}

func (s *GovernedMemoryStore) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, strings.TrimSpace(key))
}

func (s *GovernedMemoryStore) Search(ctx context.Context, query string) ([]MemoryEntry, error) {
	results, err := s.inner.Search(ctx, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	return normalizeMemoryEntries(results)
}

func (s *GovernedMemoryStore) SemanticSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error) {
	results, err := s.inner.SemanticSearch(ctx, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	return normalizeMemoryEntries(results)
}

func (s *GovernedMemoryStore) SemanticSearchMMR(ctx context.Context, query string, limit int, lambda float64) ([]MemoryEntry, error) {
	results, err := s.inner.SemanticSearchMMR(ctx, strings.TrimSpace(query), limit, lambda)
	if err != nil {
		return nil, err
	}
	return normalizeMemoryEntries(results)
}

func (s *GovernedMemoryStore) HasEmbedding() bool {
	if provider, ok := s.inner.(interface{ HasEmbedding() bool }); ok {
		return provider.HasEmbedding()
	}
	return false
}

func (s *GovernedMemoryStore) StoreType() string {
	if provider, ok := s.inner.(MemoryStoreMetadataProvider); ok {
		return "governed/" + provider.StoreType()
	}
	return "governed"
}

func (s *GovernedMemoryStore) Reindex(ctx context.Context, force bool) (int, error) {
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

func (s *GovernedMemoryStore) EmbeddingClient() EmbeddingClient {
	if provider, ok := s.inner.(interface{ EmbeddingClient() EmbeddingClient }); ok {
		return provider.EmbeddingClient()
	}
	return nil
}

func (s *GovernedMemoryStore) VectorStats() (int, int) {
	if provider, ok := s.inner.(interface{ VectorStats() (int, int) }); ok {
		return provider.VectorStats()
	}
	return 0, 0
}

func (s *GovernedMemoryStore) ListContextViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.ContextView, error) {
	provider, ok := s.inner.(durablefact.ContextViewReader)
	if !ok {
		return nil, fmt.Errorf("durable context views not supported")
	}
	return provider.ListContextViews(ctx, filter)
}

func (s *GovernedMemoryStore) ListOperatorViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, error) {
	provider, ok := s.inner.(durablefact.OperatorViewReader)
	if !ok {
		return nil, fmt.Errorf("durable operator views not supported")
	}
	return provider.ListOperatorViews(ctx, filter)
}

func (s *GovernedMemoryStore) List(ctx context.Context) ([]MemoryEntry, error) {
	results, err := s.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	return normalizeMemoryEntries(results)
}

func (s *GovernedMemoryStore) ListFiltered(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error) {
	if managed, ok := s.inner.(ManagedMemoryStore); ok {
		return managed.ListFiltered(ctx, filter)
	}
	filter.Namespace = normalizeMemoryNamespace(filter.Namespace)
	filter.ScopeKey = strings.TrimSpace(filter.ScopeKey)
	filter.Query = strings.TrimSpace(filter.Query)
	results, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	if filter.Namespace == "" && filter.ScopeKey == "" && filter.Query == "" && !filter.ManagedOnly {
		return results, nil
	}
	filtered := make([]MemoryEntry, 0, len(results))
	for _, entry := range results {
		if filter.ManagedOnly && !entry.Managed {
			continue
		}
		if filter.Namespace != "" && entry.Namespace != filter.Namespace {
			continue
		}
		if filter.ScopeKey != "" && entry.ScopeKey != filter.ScopeKey {
			continue
		}
		if filter.Query != "" && !memoryEntryMatchesQuery(entry, filter.Query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func (s *GovernedMemoryStore) UpsertRecord(ctx context.Context, record MemoryRecord) (*MemoryEntry, error) {
	if managed, ok := s.inner.(ManagedMemoryStore); ok {
		return managed.UpsertRecord(ctx, record)
	}
	normalized := normalizeMemoryRecord(record)
	rawExisting, err := s.inner.Get(ctx, normalized.Key)
	if err != nil {
		return nil, err
	}
	if rawExisting != nil {
		_, decodedRecord, decodeErr := decodeMemoryEntry(*rawExisting)
		if decodeErr != nil {
			return nil, decodeErr
		}
		existingRecord := normalized.recordFromEntry(*rawExisting)
		if decodedRecord != nil {
			existingRecord = *decodedRecord
		}
		normalized = mergeMemoryRecord(existingRecord, normalized)
	}
	if err := s.inner.Set(ctx, normalized.Key, encodeMemoryRecord(normalized)); err != nil {
		return nil, err
	}
	return s.Get(ctx, normalized.Key)
}

func (s *GovernedMemoryStore) TouchMemoryVerification(ctx context.Context, key string, passed bool) (*MemoryEntry, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("memory record key is required")
	}
	if verifier, ok := s.inner.(MemoryVerificationStore); ok {
		return verifier.TouchMemoryVerification(ctx, key, passed)
	}

	rawExisting, err := s.inner.Get(ctx, key)
	if err != nil || rawExisting == nil {
		return rawExisting, err
	}
	normalized, decodedRecord, decodeErr := decodeMemoryEntry(*rawExisting)
	if decodeErr != nil {
		return nil, decodeErr
	}

	record := MemoryRecord{}.recordFromEntry(normalized)
	if decodedRecord != nil {
		record = *decodedRecord
	}

	entry := record.toEntry(normalized.Score)
	TouchMemoryVerification(&entry, passed)
	record.UsedCount = entry.UsedCount
	record.LastUsedAt = entry.LastUsedAt
	record.VerificationPassCount = entry.VerificationPassCount
	record.VerificationFailCount = entry.VerificationFailCount
	record.Score = entry.Score
	record.State = entry.State
	record.SupersededBy = entry.SupersededBy
	record = normalizeMemoryRecord(record)
	if err := s.inner.Set(ctx, key, encodeMemoryRecord(record)); err != nil {
		return nil, err
	}
	return s.Get(ctx, key)
}

// AgentUpsert is the entry point for agent-initiated memory writes.
// It enforces that agent writes cannot overwrite user memories.
func (s *GovernedMemoryStore) AgentUpsert(ctx context.Context, record MemoryRecord) (*MemoryEntry, MemoryMutationResult, error) {
	record.Source = MemorySourceAgent
	normalized := normalizeMemoryRecord(record)

	existing, err := s.Get(ctx, normalized.Key)
	if err != nil {
		return nil, MemoryMutationResult{}, err
	}

	normalized.Score = ComputeScore(normalized.toEntry(normalized.Score))

	if existing != nil {
		if existing.Source == MemorySourceUser {
			return nil, MemoryMutationResult{
				Action:  MutationBlocked,
				Reason:  "cannot overwrite user memory",
				Suggest: normalized.Value,
			}, nil
		}
	}

	entry, err := s.UpsertRecord(ctx, normalized)
	return entry, MemoryMutationResult{Action: MutationApplied}, err
}

// UserCorrect handles user corrections: old memory is superseded (not deleted), new one is active.
func (s *GovernedMemoryStore) UserCorrect(ctx context.Context, key, newValue string) (*MemoryEntry, error) {
	key = strings.TrimSpace(key)
	newValue = strings.TrimSpace(newValue)

	existing, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	record := MemoryRecord{
		Key:    key,
		Value:  newValue,
		Source: MemorySourceUser,
		State:  MemoryActive,
	}

	if existing != nil && strings.TrimSpace(existing.Value) != newValue {
		record.PreviousValues = append(append([]string(nil), existing.PreviousValues...), existing.Value)
		record.CorrectionCount = existing.CorrectionCount + 1
	}

	return s.UpsertRecord(ctx, record)
}

func normalizeMemoryEntries(entries []MemoryEntry) ([]MemoryEntry, error) {
	out := make([]MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		normalized, _, err := decodeMemoryEntry(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].ScopeKey != out[j].ScopeKey {
			return out[i].ScopeKey < out[j].ScopeKey
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func decodeMemoryEntry(entry MemoryEntry) (MemoryEntry, *MemoryRecord, error) {
	raw := strings.TrimSpace(entry.Value)
	if !strings.HasPrefix(raw, memoryEnvelopePrefix) {
		if entry.FactClass != "" || entry.Namespace != "" || entry.Field != "" || entry.Managed || entry.Source != "" || len(entry.Tags) > 0 || len(entry.PreviousValues) > 0 || entry.State != "" || entry.SupersededBy != "" || len(entry.MediaRefs) > 0 || entry.UsedCount > 0 || !entry.LastUsedAt.IsZero() || entry.CorrectionCount > 0 || entry.VerificationPassCount > 0 || entry.VerificationFailCount > 0 || entry.SessionKey != "" || entry.ProjectID != "" {
			record := normalizeMemoryRecord(MemoryRecord{
				Key:                   entry.Key,
				FactClass:             entry.FactClass,
				Namespace:             entry.Namespace,
				ScopeKey:              entry.ScopeKey,
				Field:                 entry.Field,
				Label:                 entry.Label,
				Value:                 entry.Value,
				Managed:               entry.Managed,
				Source:                entry.Source,
				Score:                 entry.Score,
				State:                 entry.State,
				SupersededBy:          entry.SupersededBy,
				SessionKey:            entry.SessionKey,
				ProjectID:             entry.ProjectID,
				MediaRefs:             append([]string(nil), entry.MediaRefs...),
				UsedCount:             entry.UsedCount,
				LastUsedAt:            entry.LastUsedAt,
				CorrectionCount:       entry.CorrectionCount,
				VerificationPassCount: entry.VerificationPassCount,
				VerificationFailCount: entry.VerificationFailCount,
				Tags:                  append([]string(nil), entry.Tags...),
				PreviousValues:        append([]string(nil), entry.PreviousValues...),
				CreatedAt:             entry.CreatedAt,
				UpdatedAt:             entry.UpdatedAt,
			})
			return record.toEntry(entry.Score), &record, nil
		}
		record := inferMemoryRecordFromRaw(entry.Key, entry.Value, entry.CreatedAt, entry.UpdatedAt)
		return record.toEntry(entry.Score), &record, nil
	}
	payload := strings.TrimPrefix(raw, memoryEnvelopePrefix)
	var record MemoryRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return MemoryEntry{}, nil, fmt.Errorf("decode memory record %q: %w", entry.Key, err)
	}
	record.Key = normalize.FirstNonEmpty(strings.TrimSpace(record.Key), strings.TrimSpace(entry.Key))
	record.CreatedAt = chooseNonZeroTime(record.CreatedAt, entry.CreatedAt)
	record.UpdatedAt = chooseNonZeroTime(record.UpdatedAt, entry.UpdatedAt)
	normalized := normalizeMemoryRecord(record)
	return normalized.toEntry(entry.Score), &normalized, nil
}

func encodeMemoryRecord(record MemoryRecord) string {
	normalized := normalizeMemoryRecord(record)
	body, err := json.Marshal(normalized)
	if err != nil {
		return memoryEnvelopePrefix + `{"key":"` + escapeJSONString(normalized.Key) + `","value":"` + escapeJSONString(normalized.Value) + `"}`
	}
	return memoryEnvelopePrefix + string(body)
}

func normalizeMemoryRecord(record MemoryRecord) MemoryRecord {
	now := time.Now().UTC()
	out := record
	out.Key = strings.TrimSpace(out.Key)
	out.Namespace = normalizeMemoryNamespace(out.Namespace)
	out.ScopeKey = normalizeMemoryScope(out.Namespace, out.ScopeKey)
	out.Field = normalizeMemoryField(out.Field)
	if out.Key == "" {
		out.Key = buildMemoryKey(out.Namespace, out.ScopeKey, out.Field, out.Label)
	}
	if out.Field == "" {
		out.Field = inferMemoryFieldFromKey(out.Key)
	}
	out.Label = strings.TrimSpace(out.Label)
	if out.Label == "" {
		out.Label = inferMemoryLabel(out.Field, out.Key)
	}
	out.Value = strings.TrimSpace(out.Value)
	out.Managed = true
	out.FactClass = normalizeMemoryFactClass(out.FactClass)
	out.Source = strings.TrimSpace(out.Source)
	if out.Source == "" {
		out.Source = memoryDefaultSourceManual
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if out.Score > 1 {
		out.Score = 1
	}
	if out.Source == MemorySourceUser {
		out.Score = 0
	}
	out.State = MemoryState(strings.TrimSpace(string(out.State)))
	if out.State == "" {
		out.State = MemoryActive
	}
	out.SupersededBy = strings.TrimSpace(out.SupersededBy)
	out.SessionKey = strings.TrimSpace(out.SessionKey)
	out.ProjectID = strings.TrimSpace(out.ProjectID)
	out.MediaRefs = uniqueSortedStrings(out.MediaRefs)
	if out.FactClass == "" {
		class, _ := classifyMemoryRecord(out)
		out.FactClass = class
	}
	out.Tags = uniqueSortedStrings(out.Tags)
	out.PreviousValues = trimUniqueValues(out.PreviousValues, memoryPreviousValueLimit)
	out.Evidence = normalizeMemoryEvidence(out.Evidence)
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = now
	}
	return out
}

func mergeMemoryRecord(existing, update MemoryRecord) MemoryRecord {
	out := existing
	if update.FactClass != "" {
		out.FactClass = normalizeMemoryFactClass(update.FactClass)
	}
	out.Namespace = update.Namespace
	out.ScopeKey = update.ScopeKey
	out.Field = update.Field
	out.Label = normalize.FirstNonEmpty(update.Label, existing.Label)
	if strings.TrimSpace(update.Value) != "" && strings.TrimSpace(update.Value) != strings.TrimSpace(existing.Value) {
		out.PreviousValues = append([]string{existing.Value}, existing.PreviousValues...)
		out.PreviousValues = trimUniqueValues(out.PreviousValues, memoryPreviousValueLimit)
		out.Value = update.Value
	}
	if out.Value == "" {
		out.Value = update.Value
	}
	out.Source = normalize.FirstNonEmpty(update.Source, existing.Source)
	if update.Source == MemorySourceUser {
		out.Score = 0
	} else if update.Score > 0 {
		out.Score = update.Score
	}
	if update.State != "" {
		out.State = update.State
	}
	if update.SupersededBy != "" || update.State == MemorySuperseded {
		out.SupersededBy = update.SupersededBy
	}
	out.SessionKey = normalize.FirstNonEmpty(strings.TrimSpace(update.SessionKey), strings.TrimSpace(existing.SessionKey))
	out.ProjectID = normalize.FirstNonEmpty(strings.TrimSpace(update.ProjectID), strings.TrimSpace(existing.ProjectID))
	out.MediaRefs = uniqueSortedStrings(append(existing.MediaRefs, update.MediaRefs...))
	if update.UsedCount > 0 {
		out.UsedCount = update.UsedCount
	}
	out.LastUsedAt = chooseNonZeroTime(update.LastUsedAt, existing.LastUsedAt)
	if update.CorrectionCount > 0 {
		out.CorrectionCount = update.CorrectionCount
	}
	if update.VerificationPassCount > 0 {
		out.VerificationPassCount = update.VerificationPassCount
	}
	if update.VerificationFailCount > 0 {
		out.VerificationFailCount = update.VerificationFailCount
	}
	out.Tags = uniqueSortedStrings(append(existing.Tags, update.Tags...))
	out.Evidence = normalizeMemoryEvidence(append(existing.Evidence, update.Evidence...))
	out.Managed = true
	out.Key = normalize.FirstNonEmpty(update.Key, existing.Key)
	out.CreatedAt = chooseNonZeroTime(existing.CreatedAt, update.CreatedAt)
	out.UpdatedAt = chooseNonZeroTime(update.UpdatedAt, time.Now().UTC())
	return normalizeMemoryRecord(out)
}

func inferMemoryRecordFromRaw(key, value string, createdAt, updatedAt time.Time) MemoryRecord {
	namespace, scopeKey, field := inferMemoryCoordinateFromKey(key)
	return normalizeMemoryRecord(MemoryRecord{
		Key:       strings.TrimSpace(key),
		Namespace: namespace,
		ScopeKey:  scopeKey,
		Field:     field,
		Value:     value,
		Managed:   true,
		Source:    memoryDefaultSourceManual,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})
}

func (r MemoryRecord) toEntry(score float64) MemoryEntry {
	entryScore := r.Score
	if score > 0 {
		entryScore = score
	}
	return MemoryEntry{
		Key:                   r.Key,
		Value:                 r.Value,
		SessionKey:            r.SessionKey,
		ProjectID:             r.ProjectID,
		FactClass:             r.FactClass,
		Namespace:             r.Namespace,
		ScopeKey:              r.ScopeKey,
		Field:                 r.Field,
		Label:                 r.Label,
		Managed:               r.Managed,
		Source:                r.Source,
		Tags:                  append([]string(nil), r.Tags...),
		PreviousValues:        append([]string(nil), r.PreviousValues...),
		EvidenceCount:         len(r.Evidence),
		Score:                 entryScore,
		State:                 r.State,
		SupersededBy:          r.SupersededBy,
		MediaRefs:             append([]string(nil), r.MediaRefs...),
		UsedCount:             r.UsedCount,
		LastUsedAt:            r.LastUsedAt,
		CorrectionCount:       r.CorrectionCount,
		VerificationPassCount: r.VerificationPassCount,
		VerificationFailCount: r.VerificationFailCount,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
}

func (r MemoryRecord) recordFromEntry(entry MemoryEntry) MemoryRecord {
	return MemoryRecord{
		Key:                   entry.Key,
		SessionKey:            entry.SessionKey,
		ProjectID:             entry.ProjectID,
		FactClass:             entry.FactClass,
		Namespace:             entry.Namespace,
		ScopeKey:              entry.ScopeKey,
		Field:                 entry.Field,
		Label:                 entry.Label,
		Value:                 entry.Value,
		Managed:               entry.Managed,
		Source:                entry.Source,
		Score:                 entry.Score,
		State:                 entry.State,
		SupersededBy:          entry.SupersededBy,
		MediaRefs:             append([]string(nil), entry.MediaRefs...),
		UsedCount:             entry.UsedCount,
		LastUsedAt:            entry.LastUsedAt,
		CorrectionCount:       entry.CorrectionCount,
		VerificationPassCount: entry.VerificationPassCount,
		VerificationFailCount: entry.VerificationFailCount,
		Tags:                  append([]string(nil), entry.Tags...),
		PreviousValues:        append([]string(nil), entry.PreviousValues...),
		CreatedAt:             entry.CreatedAt,
		UpdatedAt:             entry.UpdatedAt,
	}
}

func normalizeMemoryNamespace(namespace string) string {
	switch strings.ToLower(strings.TrimSpace(namespace)) {
	case memoryNamespaceProfile:
		return memoryNamespaceProfile
	case memoryNamespaceWorkspace:
		return memoryNamespaceWorkspace
	case memoryNamespaceProject:
		return memoryNamespaceProject
	case memoryNamespaceTask:
		return memoryNamespaceTask
	case "":
		return memoryNamespaceGeneral
	default:
		return strings.ToLower(strings.TrimSpace(namespace))
	}
}

func normalizeMemoryScope(namespace, scopeKey string) string {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey != "" {
		return scopeKey
	}
	if normalizeMemoryNamespace(namespace) == memoryNamespaceProfile {
		return memoryDefaultProfileScope
	}
	return memoryDefaultScopeKey
}

func normalizeMemoryField(field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return "value"
	}
	field = strings.ToLower(field)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range field {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if valid {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	return out
}

func buildMemoryKey(namespace, scopeKey, field, label string) string {
	parts := []string{
		normalizeMemoryNamespace(namespace),
		normalizeMemoryField(scopeKey),
		normalizeMemoryField(normalize.FirstNonEmpty(field, label, "value")),
	}
	return strings.Join(parts, ".")
}

func inferMemoryCoordinateFromKey(key string) (string, string, string) {
	parts := strings.Split(strings.TrimSpace(key), ".")
	if len(parts) >= 3 {
		return normalizeMemoryNamespace(parts[0]), parts[1], normalizeMemoryField(strings.Join(parts[2:], "_"))
	}
	if len(parts) == 2 {
		return normalizeMemoryNamespace(parts[0]), memoryDefaultScopeKey, normalizeMemoryField(parts[1])
	}
	if len(parts) == 1 {
		return memoryNamespaceGeneral, memoryDefaultScopeKey, normalizeMemoryField(parts[0])
	}
	return memoryNamespaceGeneral, memoryDefaultScopeKey, "value"
}

func inferMemoryFieldFromKey(key string) string {
	_, _, field := inferMemoryCoordinateFromKey(key)
	return field
}

func inferMemoryLabel(field, key string) string {
	base := strings.TrimSpace(field)
	if base == "" {
		base = strings.TrimSpace(key)
	}
	if base == "" {
		return "Memory"
	}
	words := strings.Split(strings.ReplaceAll(base, "_", " "), " ")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func normalizeMemoryEvidence(in []MemoryRecordEvidence) []MemoryRecordEvidence {
	if len(in) == 0 {
		return nil
	}
	out := make([]MemoryRecordEvidence, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item.Source = strings.TrimSpace(item.Source)
		item.Ref = strings.TrimSpace(item.Ref)
		item.Summary = strings.TrimSpace(item.Summary)
		item.Value = strings.TrimSpace(item.Value)
		if item.ObservedAt.IsZero() {
			item.ObservedAt = time.Now().UTC()
		}
		key := strings.Join([]string{item.Source, item.Ref, item.Summary, item.Value}, "|")
		if key == "|||" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if len(out) >= memoryEvidenceLimit {
			break
		}
	}
	return out
}

func normalizeMemoryFactClass(class durablefact.FactClass) durablefact.FactClass {
	switch durablefact.FactClass(strings.TrimSpace(string(class))) {
	case durablefact.FactClassPreference:
		return durablefact.FactClassPreference
	case durablefact.FactClassAgreement:
		return durablefact.FactClassAgreement
	case durablefact.FactClassBusinessRule:
		return durablefact.FactClassBusinessRule
	case durablefact.FactClassSystemConfig:
		return durablefact.FactClassSystemConfig
	case durablefact.FactClassImportedNote:
		return durablefact.FactClassImportedNote
	default:
		return ""
	}
}

func classifyMemoryRecord(record MemoryRecord) (durablefact.FactClass, bool) {
	field := normalizeMemoryField(record.Field)
	namespace := normalizeMemoryNamespace(record.Namespace)
	hasTag := func(target string) bool {
		target = strings.ToLower(strings.TrimSpace(target))
		for _, tag := range record.Tags {
			if strings.ToLower(strings.TrimSpace(tag)) == target {
				return true
			}
		}
		return false
	}
	switch {
	case hasTag("agreement"):
		return durablefact.FactClassAgreement, false
	case hasTag("business_rule"), hasTag("rule"), field == "policy", field == "rule", field == "guardrail":
		return durablefact.FactClassBusinessRule, false
	case hasTag("preference"),
		namespace == memoryNamespaceProfile && (field == "reply_language" || field == "response_style" || field == "tone" || field == "format"):
		return durablefact.FactClassPreference, false
	case namespace == memoryNamespaceProfile:
		return durablefact.FactClassImportedNote, false
	case namespace == memoryNamespaceWorkspace, namespace == memoryNamespaceProject, namespace == memoryNamespaceTask:
		return durablefact.FactClassImportedNote, false
	default:
		return durablefact.FactClassImportedNote, true
	}
}

func trimUniqueValues(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	out := make([]string, 0, ints.Min(len(values), limit))
	seen := make(map[string]struct{}, len(values))
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
		if len(out) >= limit {
			break
		}
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
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

func memoryEntryMatchesQuery(entry MemoryEntry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	for _, candidate := range []string{
		entry.Key,
		entry.Value,
		string(entry.FactClass),
		entry.Namespace,
		entry.ScopeKey,
		entry.Field,
		entry.Label,
		entry.Source,
		strings.Join(entry.Tags, " "),
		strings.Join(entry.PreviousValues, " "),
	} {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

func chooseNonZeroTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	return fallback
}

func escapeJSONString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(value)
}
