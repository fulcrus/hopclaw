package usage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// maxRecords is the maximum number of usage records kept in memory.
// When exceeded, the oldest records are evicted.
const maxRecords = 10000

// Store persists and queries usage records.
type Store interface {
	Record(ctx context.Context, rec Record) error
	Query(ctx context.Context, filter QueryFilter) ([]Record, error)
	Summarize(ctx context.Context, filter QueryFilter) (*Summary, error)
	SessionSummary(ctx context.Context, sessionID string) (*SessionCostSummary, error)
	DailySummary(ctx context.Context, filter QueryFilter) ([]DailyUsage, error)
	ProviderSummary(ctx context.Context, filter QueryFilter) (map[string]*ProviderUsage, error)
}

// InMemoryStore is a mutex-protected in-memory implementation of Store.
type InMemoryStore struct {
	mu      sync.RWMutex // guards records
	records []Record
}

// NewInMemoryStore returns a new empty InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{}
}

// Record appends a usage record, assigning an ID and timestamp if missing.
// When the store exceeds maxRecords, the oldest records are evicted.
func (s *InMemoryStore) Record(_ context.Context, rec Record) error {
	if rec.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("failed to generate usage record id: %w", err)
		}
		rec.ID = id
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = append(s.records, rec)
	if len(s.records) > maxRecords {
		drop := len(s.records) - maxRecords
		s.records = append([]Record(nil), s.records[drop:]...)
	}
	return nil
}

// Query returns records matching the given filter.
func (s *InMemoryStore) Query(_ context.Context, filter QueryFilter) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Record
	for _, rec := range s.records {
		if !matchesFilter(rec, filter) {
			continue
		}
		results = append(results, rec)
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}
	return results, nil
}

// Summarize aggregates usage across records matching the given filter.
func (s *InMemoryStore) Summarize(_ context.Context, filter QueryFilter) (*Summary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return summarizeRecords(s.records, filter), nil
}

// SessionSummary returns a comprehensive cost breakdown for a single session.
func (s *InMemoryStore) SessionSummary(_ context.Context, sessionID string) (*SessionCostSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionSummaryRecords(s.records, sessionID), nil
}

// dailyDateFormat is the layout used for DailyUsage.Date.
const dailyDateFormat = "2006-01-02"

// DailySummary groups records by UTC date, returning a sorted slice.
func (s *InMemoryStore) DailySummary(_ context.Context, filter QueryFilter) ([]DailyUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return dailySummaryRecords(s.records, filter), nil
}

// ProviderSummary groups records by Provider field.
func (s *InMemoryStore) ProviderSummary(_ context.Context, filter QueryFilter) (map[string]*ProviderUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return providerSummaryRecords(s.records, filter), nil
}

// matchesFilter returns true if the record satisfies all non-zero filter fields.
func matchesFilter(rec Record, f QueryFilter) bool {
	if f.SessionID != "" && rec.SessionID != f.SessionID {
		return false
	}
	if f.RunID != "" && rec.RunID != f.RunID {
		return false
	}
	if f.WorkflowID != "" && rec.WorkflowID != f.WorkflowID {
		return false
	}
	if f.Model != "" && rec.Model != f.Model {
		return false
	}
	if f.RecordType != "" && normalizedRecordType(rec) != f.RecordType {
		return false
	}
	if !f.Since.IsZero() && rec.CreatedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && rec.CreatedAt.After(f.Until) {
		return false
	}
	return true
}

func normalizedRecordType(rec Record) RecordType {
	if rec.RecordType == "" {
		return RecordTypeModelCall
	}
	return rec.RecordType
}

// ---------------------------------------------------------------------------
// Shared aggregation helpers (used by InMemoryStore and JSONLStore)
// ---------------------------------------------------------------------------

// summarizeRecords aggregates usage across records matching the given filter.
func summarizeRecords(records []Record, filter QueryFilter) *Summary {
	summary := &Summary{
		ByModel: make(map[string]ModelUsage),
	}
	for _, rec := range records {
		if !matchesFilter(rec, filter) {
			continue
		}
		summary.TotalPromptTokens += rec.PromptTokens
		summary.TotalCompletionTokens += rec.CompletionTokens
		summary.TotalTokens += rec.TotalTokens
		summary.TotalCostEstimate += rec.CostEstimate
		summary.RecordCount++

		mu := summary.ByModel[rec.Model]
		mu.PromptTokens += rec.PromptTokens
		mu.CompletionTokens += rec.CompletionTokens
		mu.TotalTokens += rec.TotalTokens
		mu.CostEstimate += rec.CostEstimate
		mu.CallCount++
		summary.ByModel[rec.Model] = mu
	}
	return summary
}

// sessionSummaryRecords computes a session cost breakdown from a record slice.
func sessionSummaryRecords(records []Record, sessionID string) *SessionCostSummary {
	result := &SessionCostSummary{
		SessionID: sessionID,
		ByModel:   make(map[string]ModelUsage),
		ByTool:    make(map[string]ToolUsage),
	}

	for _, rec := range records {
		if rec.SessionID != sessionID {
			continue
		}

		result.TotalDuration += rec.Duration

		// Track first/last call timestamps.
		if result.FirstCallAt.IsZero() || rec.CreatedAt.Before(result.FirstCallAt) {
			result.FirstCallAt = rec.CreatedAt
		}
		if rec.CreatedAt.After(result.LastCallAt) {
			result.LastCallAt = rec.CreatedAt
		}

		switch rec.RecordType {
		case RecordTypeModelCall:
			result.ModelCallCount++
			result.TotalCost += rec.CostEstimate
			result.TotalTokens += rec.TotalTokens

			mu := result.ByModel[rec.Model]
			mu.PromptTokens += rec.PromptTokens
			mu.CompletionTokens += rec.CompletionTokens
			mu.TotalTokens += rec.TotalTokens
			mu.CostEstimate += rec.CostEstimate
			mu.CallCount++
			result.ByModel[rec.Model] = mu

		case RecordTypeToolExecution:
			result.ToolExecutionCount++

			tu := result.ByTool[rec.ToolName]
			tu.CallCount++
			tu.TotalDuration += rec.Duration
			if tu.CallCount > 0 {
				tu.AvgDuration = tu.TotalDuration / time.Duration(tu.CallCount)
			}
			result.ByTool[rec.ToolName] = tu

		default:
			// Legacy records without a RecordType are treated as model calls.
			result.ModelCallCount++
			result.TotalCost += rec.CostEstimate
			result.TotalTokens += rec.TotalTokens

			mu := result.ByModel[rec.Model]
			mu.PromptTokens += rec.PromptTokens
			mu.CompletionTokens += rec.CompletionTokens
			mu.TotalTokens += rec.TotalTokens
			mu.CostEstimate += rec.CostEstimate
			mu.CallCount++
			result.ByModel[rec.Model] = mu
		}
	}
	return result
}

// dailySummaryRecords groups records by UTC date, returning a sorted slice.
func dailySummaryRecords(records []Record, filter QueryFilter) []DailyUsage {
	byDate := make(map[string]*DailyUsage)
	var dateOrder []string

	for _, rec := range records {
		if !matchesFilter(rec, filter) {
			continue
		}
		date := rec.CreatedAt.UTC().Format(dailyDateFormat)
		du, ok := byDate[date]
		if !ok {
			du = &DailyUsage{
				Date:    date,
				ByModel: make(map[string]ModelUsage),
			}
			byDate[date] = du
			dateOrder = append(dateOrder, date)
		}
		du.PromptTokens += rec.PromptTokens
		du.CompletionTokens += rec.CompletionTokens
		du.TotalTokens += rec.TotalTokens
		du.CostEstimate += rec.CostEstimate
		du.CallCount++

		mu := du.ByModel[rec.Model]
		mu.PromptTokens += rec.PromptTokens
		mu.CompletionTokens += rec.CompletionTokens
		mu.TotalTokens += rec.TotalTokens
		mu.CostEstimate += rec.CostEstimate
		mu.CallCount++
		du.ByModel[rec.Model] = mu
	}

	// Sort by date ascending. dateOrder is already in insertion order, but
	// records may not be strictly chronological, so we sort explicitly.
	sort.Strings(dateOrder)

	results := make([]DailyUsage, 0, len(dateOrder))
	for _, d := range dateOrder {
		results = append(results, *byDate[d])
	}
	return results
}

// providerSummaryRecords groups records by Provider field.
func providerSummaryRecords(records []Record, filter QueryFilter) map[string]*ProviderUsage {
	result := make(map[string]*ProviderUsage)
	for _, rec := range records {
		if !matchesFilter(rec, filter) {
			continue
		}
		provider := rec.Provider
		if provider == "" {
			provider = "unknown"
		}
		pu, ok := result[provider]
		if !ok {
			pu = &ProviderUsage{
				Provider: provider,
				ByModel:  make(map[string]ModelUsage),
			}
			result[provider] = pu
		}
		pu.PromptTokens += rec.PromptTokens
		pu.CompletionTokens += rec.CompletionTokens
		pu.TotalTokens += rec.TotalTokens
		pu.CostEstimate += rec.CostEstimate
		pu.CallCount++

		mu := pu.ByModel[rec.Model]
		mu.PromptTokens += rec.PromptTokens
		mu.CompletionTokens += rec.CompletionTokens
		mu.TotalTokens += rec.TotalTokens
		mu.CostEstimate += rec.CostEstimate
		mu.CallCount++
		pu.ByModel[rec.Model] = mu
	}
	return result
}

// newID generates a random hex ID for usage records.
func newID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
