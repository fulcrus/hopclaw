package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// scannerInitBuf is the initial buffer size for the JSONL line scanner.
	scannerInitBuf = 64 * 1024
	// scannerMaxBuf is the maximum buffer size for a single JSONL line (1 MB).
	scannerMaxBuf = 1024 * 1024
)

// JSONLStore is a file-backed implementation of Store that persists usage
// records as newline-delimited JSON (one Record per line). Writes are
// serialized with a mutex and flushed after each append. Reads scan the
// full file and apply filters in memory.
type JSONLStore struct {
	mu   sync.Mutex // guards file writes
	file *os.File
	enc  *json.Encoder
	path string
}

// NewJSONLStore creates or opens a JSONL usage store at the given path.
func NewJSONLStore(path string) (*JSONLStore, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open usage store file: %w", err)
	}
	enc := json.NewEncoder(f)
	return &JSONLStore{
		file: f,
		enc:  enc,
		path: path,
	}, nil
}

// Record appends a usage record as a JSON line, assigning an ID and
// timestamp if missing.
func (s *JSONLStore) Record(_ context.Context, rec Record) error {
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

	if err := s.enc.Encode(rec); err != nil {
		return fmt.Errorf("failed to write usage record: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("failed to flush usage store: %w", err)
	}
	return nil
}

// Query returns records matching the given filter by scanning the JSONL file.
func (s *JSONLStore) Query(_ context.Context, filter QueryFilter) ([]Record, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	var results []Record
	for _, rec := range records {
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
func (s *JSONLStore) Summarize(_ context.Context, filter QueryFilter) (*Summary, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	return summarizeRecords(records, filter), nil
}

// SessionSummary returns a comprehensive cost breakdown for a single session.
func (s *JSONLStore) SessionSummary(_ context.Context, sessionID string) (*SessionCostSummary, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	return sessionSummaryRecords(records, sessionID), nil
}

// DailySummary groups records by UTC date, returning a sorted slice.
func (s *JSONLStore) DailySummary(_ context.Context, filter QueryFilter) ([]DailyUsage, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	return dailySummaryRecords(records, filter), nil
}

// ProviderSummary groups records by Provider field.
func (s *JSONLStore) ProviderSummary(_ context.Context, filter QueryFilter) (map[string]*ProviderUsage, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	return providerSummaryRecords(records, filter), nil
}

// Close flushes and closes the underlying file.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("failed to flush usage store on close: %w", err)
	}
	return s.file.Close()
}

// readAll opens the file for reading and scans all records. The write file
// handle is not used for reading to avoid seek/position conflicts.
func (s *JSONLStore) readAll() ([]Record, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to open usage store for reading: %w", err)
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerInitBuf), scannerMaxBuf)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("failed to decode usage record: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan usage store: %w", err)
	}
	return records, nil
}
