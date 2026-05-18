package approval

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors for the approval package.
var (
	ErrAlreadyResolved = errors.New("approval already resolved")
	ErrNotFound        = errors.New("approval not found")
	ErrInvalidScope    = errors.New("invalid approval scope")
	ErrScopePolicy     = errors.New("approval scope violates policy")
)

// Status reports the lifecycle state of an approval ticket.
type Status string

// Kind identifies the workflow that produced an approval ticket.
type Kind string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusDenied    Status = "denied"
	StatusCancelled Status = "cancelled"

	KindToolCalls    Kind = "tool_calls"
	KindSkillInstall Kind = "skill_install"
)

// ToolCall records one tool invocation attached to an approval request.
type ToolCall struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Input         map[string]any `json:"input,omitempty"`
	ResourceScope ResourceScope  `json:"resource_scope,omitempty"`
}

// ExternalReference links a ticket to an approval tracked in an external
// system.
type ExternalReference struct {
	Provider   string         `json:"provider,omitempty"`
	ExternalID string         `json:"external_id,omitempty"`
	URL        string         `json:"url,omitempty"`
	Status     string         `json:"status,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	SyncedAt   time.Time      `json:"synced_at,omitempty"`
}

// Ticket stores the approval request, its scope, and its eventual resolution.
type Ticket struct {
	ID         string              `json:"id"`
	RunID      string              `json:"run_id"`
	SessionID  string              `json:"session_id"`
	Kind       Kind                `json:"kind,omitempty"`
	Status     Status              `json:"status"`
	ToolCalls  []ToolCall          `json:"tool_calls"`
	Reasons    []string            `json:"reasons,omitempty"`
	Metadata   map[string]any      `json:"metadata,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	ResolvedAt time.Time           `json:"resolved_at,omitempty"`
	ResolvedBy string              `json:"resolved_by,omitempty"`
	Note       string              `json:"note,omitempty"`
	Scope      Scope               `json:"scope,omitempty"`
	External   []ExternalReference `json:"external,omitempty"`
}

// Resolution describes the final decision applied to a pending ticket.
type Resolution struct {
	Status     Status
	ResolvedBy string
	Note       string
	Scope      Scope
}

// ListFilter narrows approval list operations by status and pagination.
type ListFilter struct {
	Status Status
	Limit  int
	Offset int
}

// Normalize clamps negative paging values to zero.
func (f ListFilter) Normalize() ListFilter {
	if f.Limit < 0 {
		f.Limit = 0
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f
}

// Store persists approval tickets and their external references.
type Store interface {
	Create(ctx context.Context, ticket Ticket) (*Ticket, error)
	Get(ctx context.Context, ticketID string) (*Ticket, error)
	GetByRun(ctx context.Context, runID string) (*Ticket, error)
	GetByExternal(ctx context.Context, provider, externalID string) (*Ticket, error)
	List(ctx context.Context, filter ListFilter) ([]*Ticket, error)
	Resolve(ctx context.Context, ticketID string, resolution Resolution) (*Ticket, error)
	UpsertExternalRef(ctx context.Context, ticketID string, ref ExternalReference) (*Ticket, error)
}

// InMemoryStore keeps approval tickets in process memory for tests and
// lightweight deployments.
type InMemoryStore struct {
	mu      sync.RWMutex
	nextID  atomic.Uint64
	byID    map[string]*Ticket
	byRunID map[string]string
	byExtID map[string]string
}

// NewInMemoryStore returns an empty in-memory approval store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		byID:    make(map[string]*Ticket),
		byRunID: make(map[string]string),
		byExtID: make(map[string]string),
	}
}

// Create inserts a pending ticket and reuses the existing pending ticket for
// the same run when concurrent callers race.
func (s *InMemoryStore) Create(_ context.Context, ticket Ticket) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ticket.RunID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	if existingID, ok := s.byRunID[ticket.RunID]; ok {
		existing, exists := s.byID[existingID]
		if exists && existing.Status == StatusPending {
			// Return the existing pending ticket instead of erroring,
			// so concurrent evaluations for the same run converge.
			return cloneTicketValue(existing), nil
		}
	}
	ticket.ID = fmt.Sprintf("appr-%06d", s.nextID.Add(1))
	if ticket.Kind == "" {
		ticket.Kind = KindToolCalls
	}
	ticket.Status = StatusPending
	ticket.CreatedAt = time.Now().UTC()
	if len(ticket.External) > 0 {
		ticket.External = cloneExternalReferences(ticket.External)
	}
	copied := cloneTicket(ticket)
	s.byID[copied.ID] = copied
	s.byRunID[copied.RunID] = copied.ID
	for _, ref := range copied.External {
		if key := ExternalReferenceLookupKey(ref.Provider, ref.ExternalID); key != "" {
			s.byExtID[key] = copied.ID
		}
	}
	return cloneTicketValue(copied), nil
}

// Get returns a ticket by ID or a not-found error when it does not exist.
func (s *InMemoryStore) Get(_ context.Context, ticketID string) (*Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}
	return cloneTicketValue(ticket), nil
}

// GetByRun returns the ticket associated with a run or a not-found error when
// none exists.
func (s *InMemoryStore) GetByRun(_ context.Context, runID string) (*Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ticketID, ok := s.byRunID[runID]
	if !ok {
		return nil, fmt.Errorf("ticket for run %s not found", runID)
	}
	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}
	return cloneTicketValue(ticket), nil
}

// GetByExternal returns a ticket indexed by external provider and ID. Missing
// entries wrap `ErrNotFound`.
func (s *InMemoryStore) GetByExternal(_ context.Context, provider, externalID string) (*Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ticketID, ok := s.byExtID[ExternalReferenceLookupKey(provider, externalID)]
	if !ok {
		return nil, fmt.Errorf("ticket for external approval %s/%s: %w", strings.TrimSpace(provider), strings.TrimSpace(externalID), ErrNotFound)
	}
	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, ErrNotFound)
	}
	return cloneTicketValue(ticket), nil
}

// List returns cloned tickets ordered by creation time and applies normalized
// paging after status filtering.
func (s *InMemoryStore) List(_ context.Context, filter ListFilter) ([]*Ticket, error) {
	filter = filter.Normalize()
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Ticket, 0, len(s.byID))
	for _, ticket := range s.byID {
		if filter.Status != "" && ticket.Status != filter.Status {
			continue
		}
		out = append(out, cloneTicketValue(ticket))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			return nil, nil
		}
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// Resolve applies a final approval decision to a pending ticket. Missing
// tickets wrap `ErrNotFound`, and non-pending tickets wrap `ErrAlreadyResolved`.
func (s *InMemoryStore) Resolve(_ context.Context, ticketID string, resolution Resolution) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, ErrNotFound)
	}
	if ticket.Status != StatusPending {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, ErrAlreadyResolved)
	}
	switch resolution.Status {
	case StatusApproved, StatusDenied, StatusCancelled:
	default:
		return nil, fmt.Errorf("invalid approval resolution %q", resolution.Status)
	}
	resolution, err := NormalizeResolution(ticket, resolution)
	if err != nil {
		return nil, err
	}

	ticket.Status = resolution.Status
	ticket.ResolvedAt = time.Now().UTC()
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	ticket.Scope = resolution.Scope
	return cloneTicketValue(ticket), nil
}

// UpsertExternalRef adds or updates one external reference on a ticket. Missing
// tickets wrap `ErrNotFound`.
func (s *InMemoryStore) UpsertExternalRef(_ context.Context, ticketID string, ref ExternalReference) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, ErrNotFound)
	}
	previous := externalReferenceByProvider(ticket.External, ref.Provider)
	nextRefs, merged, err := UpsertExternalReferences(ticket.External, ref)
	if err != nil {
		return nil, err
	}
	if previous != nil && strings.TrimSpace(previous.ExternalID) != "" {
		delete(s.byExtID, ExternalReferenceLookupKey(previous.Provider, previous.ExternalID))
	}
	if strings.TrimSpace(merged.ExternalID) != "" {
		s.byExtID[ExternalReferenceLookupKey(merged.Provider, merged.ExternalID)] = ticketID
	}
	ticket.External = nextRefs
	return cloneTicketValue(ticket), nil
}

func cloneTicketValue(in *Ticket) *Ticket {
	return cloneTicket(*in)
}

func cloneTicket(in Ticket) *Ticket {
	out := in
	if in.ToolCalls != nil {
		out.ToolCalls = make([]ToolCall, len(in.ToolCalls))
		for i, call := range in.ToolCalls {
			out.ToolCalls[i] = ToolCall{
				ID:            call.ID,
				Name:          call.Name,
				ResourceScope: call.ResourceScope.Normalized(),
			}
			if call.Input != nil {
				out.ToolCalls[i].Input = make(map[string]any, len(call.Input))
				for k, v := range call.Input {
					out.ToolCalls[i].Input[k] = v
				}
			}
		}
	}
	if in.Reasons != nil {
		out.Reasons = append([]string(nil), in.Reasons...)
	}
	if in.Metadata != nil {
		out.Metadata = cloneAnyMap(in.Metadata)
	}
	if len(in.External) > 0 {
		out.External = cloneExternalReferences(in.External)
	}
	return &out
}

// CloneExternalReferences returns a normalized deep copy of external reference
// metadata.
func CloneExternalReferences(in []ExternalReference) []ExternalReference {
	return cloneExternalReferences(in)
}

func cloneExternalReferences(in []ExternalReference) []ExternalReference {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExternalReference, len(in))
	for i, ref := range in {
		out[i] = normalizeExternalReference(ref)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAnyValue(v)
	}
	return out
}

func cloneAnySlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneAnyValue(v)
	}
	return out
}

func cloneAnyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []ToolCall:
		out := make([]ToolCall, len(typed))
		for i, call := range typed {
			out[i] = ToolCall{ID: call.ID, Name: call.Name, ResourceScope: call.ResourceScope.Normalized()}
			if call.Input != nil {
				out[i].Input = cloneAnyMap(call.Input)
			}
		}
		return out
	case []ExternalReference:
		return cloneExternalReferences(typed)
	default:
		return v
	}
}

// UpsertExternalReferences merges one provider reference into an existing list
// and returns the updated list plus the merged reference.
func UpsertExternalReferences(existing []ExternalReference, update ExternalReference) ([]ExternalReference, ExternalReference, error) {
	update = normalizeExternalReference(update)
	if update.Provider == "" {
		return nil, ExternalReference{}, fmt.Errorf("external provider is required")
	}
	out := cloneExternalReferences(existing)
	index := -1
	for i, ref := range out {
		if strings.EqualFold(strings.TrimSpace(ref.Provider), update.Provider) {
			index = i
			break
		}
	}
	if index >= 0 {
		merged := mergeExternalReference(out[index], update)
		out[index] = merged
		sortExternalReferences(out)
		return out, merged, nil
	}
	merged := mergeExternalReference(ExternalReference{}, update)
	out = append(out, merged)
	sortExternalReferences(out)
	return out, merged, nil
}

// ExternalReferenceLookupKey builds the normalized map key for a provider and
// external approval ID.
func ExternalReferenceLookupKey(provider, externalID string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return ""
	}
	return provider + "\x00" + externalID
}

func externalReferenceByProvider(refs []ExternalReference, provider string) *ExternalReference {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	for _, ref := range refs {
		if strings.ToLower(strings.TrimSpace(ref.Provider)) == provider {
			cloned := normalizeExternalReference(ref)
			return &cloned
		}
	}
	return nil
}

func mergeExternalReference(current ExternalReference, update ExternalReference) ExternalReference {
	out := normalizeExternalReference(current)
	if update.Provider != "" {
		out.Provider = update.Provider
	}
	if update.ExternalID != "" {
		out.ExternalID = update.ExternalID
	}
	if update.URL != "" {
		out.URL = update.URL
	}
	if update.Status != "" {
		out.Status = update.Status
	}
	if len(update.Metadata) > 0 {
		if out.Metadata == nil {
			out.Metadata = make(map[string]any, len(update.Metadata))
		}
		for key, value := range update.Metadata {
			out.Metadata[key] = cloneAnyValue(value)
		}
	}
	if !update.SyncedAt.IsZero() {
		out.SyncedAt = update.SyncedAt.UTC()
	} else if out.SyncedAt.IsZero() {
		out.SyncedAt = time.Now().UTC()
	}
	return out
}

func normalizeExternalReference(ref ExternalReference) ExternalReference {
	out := ref
	out.Provider = strings.TrimSpace(out.Provider)
	out.ExternalID = strings.TrimSpace(out.ExternalID)
	out.URL = strings.TrimSpace(out.URL)
	out.Status = strings.TrimSpace(out.Status)
	if out.Metadata != nil {
		out.Metadata = cloneAnyMap(out.Metadata)
	}
	if !out.SyncedAt.IsZero() {
		out.SyncedAt = out.SyncedAt.UTC()
	}
	return out
}

func sortExternalReferences(items []ExternalReference) {
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(items[i].Provider))
		right := strings.ToLower(strings.TrimSpace(items[j].Provider))
		if left == right {
			return strings.TrimSpace(items[i].ExternalID) < strings.TrimSpace(items[j].ExternalID)
		}
		return left < right
	})
}
