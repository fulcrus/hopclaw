package wire

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultMaxEntries    = 1000
	defaultMaxBodyBytes  = 64 * 1024 // 64 KiB per captured body
	defaultRetentionTime = 1 * time.Hour
)

// ---------------------------------------------------------------------------
// Direction
// ---------------------------------------------------------------------------

// Direction indicates whether the entry is a request or response.
type Direction string

const (
	DirectionRequest  Direction = "request"
	DirectionResponse Direction = "response"
)

// ---------------------------------------------------------------------------
// Entry
// ---------------------------------------------------------------------------

// Entry represents a single captured protocol exchange.
type Entry struct {
	ID         string            `json:"id"`
	RecordedAt time.Time         `json:"recorded_at"`
	Direction  Direction         `json:"direction"`
	Provider   string            `json:"provider"` // e.g. "openai", "anthropic"
	Method     string            `json:"method"`   // HTTP method
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers,omitempty"`  // selected headers (no auth)
	Body       string            `json:"body,omitempty"`     // truncated body
	Status     int               `json:"status,omitempty"`   // response status code
	Duration   time.Duration     `json:"duration,omitempty"` // round-trip time
	Error      string            `json:"error,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	RunID      string            `json:"run_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// Stats contains summary statistics about captured wire entries.
type Stats struct {
	TotalEntries  int            `json:"total_entries"`
	RequestCount  int            `json:"request_count"`
	ResponseCount int            `json:"response_count"`
	ErrorCount    int            `json:"error_count"`
	AvgDuration   time.Duration  `json:"avg_duration"`
	ByProvider    map[string]int `json:"by_provider"`
	OldestEntry   time.Time      `json:"oldest_entry,omitempty"`
	NewestEntry   time.Time      `json:"newest_entry,omitempty"`
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config controls wire logging behavior.
type Config struct {
	Enabled       bool          `json:"enabled" yaml:"enabled"`
	MaxEntries    int           `json:"max_entries,omitempty" yaml:"max_entries"`
	MaxBodyBytes  int           `json:"max_body_bytes,omitempty" yaml:"max_body_bytes"`
	RetentionTime time.Duration `json:"retention_time,omitempty" yaml:"retention_time"`
	RedactHeaders []string      `json:"redact_headers,omitempty" yaml:"redact_headers"` // headers to redact
	Providers     []string      `json:"providers,omitempty" yaml:"providers"`           // filter by provider; empty = all
}

// ---------------------------------------------------------------------------
// QueryFilter
// ---------------------------------------------------------------------------

// QueryFilter filters wire log entries.
type QueryFilter struct {
	Provider  string
	Direction Direction
	SessionID string
	RunID     string
	Since     time.Time
	Limit     int
}

// ---------------------------------------------------------------------------
// Logger
// ---------------------------------------------------------------------------

// Logger captures wire protocol exchanges in a circular buffer.
type Logger struct {
	mu       sync.RWMutex // guards entries, enabled
	entries  []Entry
	head     int // write position in the circular buffer
	count    int // number of valid entries (up to config.MaxEntries)
	enabled  bool
	config   Config
	sequence atomic.Int64
}

// NewLogger creates a wire Logger with the given configuration.
// Zero-value fields in cfg are replaced with sensible defaults.
func NewLogger(cfg Config) *Logger {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = defaultMaxEntries
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.RetentionTime <= 0 {
		cfg.RetentionTime = defaultRetentionTime
	}
	return &Logger{
		entries: make([]Entry, cfg.MaxEntries),
		enabled: cfg.Enabled,
		config:  cfg,
	}
}

// ---------------------------------------------------------------------------
// Core logging
// ---------------------------------------------------------------------------

// Log adds an entry to the circular buffer. It is safe for concurrent use.
// If the logger is disabled or the entry's provider is filtered out, the call
// is a no-op.
func (l *Logger) Log(entry Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.enabled {
		return
	}
	if !l.providerAllowed(entry.Provider) {
		return
	}

	if entry.ID == "" {
		entry.ID = fmt.Sprintf("wire-%06d", l.sequence.Add(1))
	}
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	entry.Headers = SanitizeHeaders(entry.Headers, l.config.RedactHeaders)

	l.entries[l.head] = entry
	l.head = (l.head + 1) % l.config.MaxEntries
	if l.count < l.config.MaxEntries {
		l.count++
	}
}

// LogRequest logs an outgoing request and returns the generated entry ID.
func (l *Logger) LogRequest(ctx context.Context, provider, method, url string, headers map[string]string, body io.Reader) string {
	id := fmt.Sprintf("wire-%06d", l.sequence.Add(1))

	var sessionID, runID string
	if v, ok := ctx.Value(contextKeySessionID).(string); ok {
		sessionID = v
	}
	if v, ok := ctx.Value(contextKeyRunID).(string); ok {
		runID = v
	}

	entry := Entry{
		ID:         id,
		RecordedAt: time.Now().UTC(),
		Direction:  DirectionRequest,
		Provider:   provider,
		Method:     method,
		URL:        url,
		Headers:    headers,
		Body:       TruncateBody(body, l.config.MaxBodyBytes),
		SessionID:  sessionID,
		RunID:      runID,
	}
	l.Log(entry)
	return id
}

// LogResponse logs the response that corresponds to a prior LogRequest call.
func (l *Logger) LogResponse(requestID string, status int, headers map[string]string, body io.Reader, duration time.Duration, err error) {
	entry := Entry{
		RecordedAt: time.Now().UTC(),
		Direction:  DirectionResponse,
		Status:     status,
		Headers:    headers,
		Body:       TruncateBody(body, l.config.MaxBodyBytes),
		Duration:   duration,
	}
	if err != nil {
		entry.Error = err.Error()
	}

	// Copy provider/session/run from the matching request if we can find it.
	l.mu.RLock()
	for i := 0; i < l.count; i++ {
		idx := (l.head - 1 - i + l.config.MaxEntries) % l.config.MaxEntries
		if l.entries[idx].ID == requestID {
			entry.Provider = l.entries[idx].Provider
			entry.SessionID = l.entries[idx].SessionID
			entry.RunID = l.entries[idx].RunID
			entry.URL = l.entries[idx].URL
			entry.Method = l.entries[idx].Method
			break
		}
	}
	l.mu.RUnlock()

	l.Log(entry)
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

// Query returns entries matching the given filter. Entries are returned in
// chronological order (oldest first).
func (l *Logger) Query(filter QueryFilter) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var results []Entry
	for i := 0; i < l.count; i++ {
		idx := (l.head - l.count + i + l.config.MaxEntries) % l.config.MaxEntries
		e := l.entries[idx]
		if !l.matchesFilter(e, filter) {
			continue
		}
		results = append(results, cloneEntry(e))
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}
	return results
}

// Recent returns the n most recent entries in chronological order.
func (l *Logger) Recent(n int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n <= 0 || n > l.count {
		n = l.count
	}
	out := make([]Entry, n)
	for i := 0; i < n; i++ {
		idx := (l.head - n + i + l.config.MaxEntries) % l.config.MaxEntries
		out[i] = cloneEntry(l.entries[idx])
	}
	return out
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Clear removes all entries from the buffer.
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make([]Entry, l.config.MaxEntries)
	l.head = 0
	l.count = 0
}

// Enable turns on wire logging.
func (l *Logger) Enable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = true
}

// Disable turns off wire logging.
func (l *Logger) Disable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = false
}

// IsEnabled reports whether wire logging is currently enabled.
func (l *Logger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// Stats returns summary statistics about the current buffer contents.
func (l *Logger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	s := Stats{
		ByProvider: make(map[string]int),
	}
	if l.count == 0 {
		return s
	}

	var totalDuration time.Duration
	var durationCount int

	for i := 0; i < l.count; i++ {
		idx := (l.head - l.count + i + l.config.MaxEntries) % l.config.MaxEntries
		e := l.entries[idx]
		s.TotalEntries++

		switch e.Direction {
		case DirectionRequest:
			s.RequestCount++
		case DirectionResponse:
			s.ResponseCount++
		}
		if e.Error != "" {
			s.ErrorCount++
		}
		if e.Duration > 0 {
			totalDuration += e.Duration
			durationCount++
		}
		if e.Provider != "" {
			s.ByProvider[e.Provider]++
		}
		if i == 0 {
			s.OldestEntry = e.RecordedAt
			s.NewestEntry = e.RecordedAt
		} else {
			if e.RecordedAt.Before(s.OldestEntry) {
				s.OldestEntry = e.RecordedAt
			}
			if e.RecordedAt.After(s.NewestEntry) {
				s.NewestEntry = e.RecordedAt
			}
		}
	}

	if durationCount > 0 {
		s.AvgDuration = totalDuration / time.Duration(durationCount)
	}
	return s
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (l *Logger) providerAllowed(provider string) bool {
	if len(l.config.Providers) == 0 {
		return true
	}
	for _, p := range l.config.Providers {
		if strings.EqualFold(p, provider) {
			return true
		}
	}
	return false
}

func (l *Logger) matchesFilter(e Entry, f QueryFilter) bool {
	if f.Provider != "" && !strings.EqualFold(e.Provider, f.Provider) {
		return false
	}
	if f.Direction != "" && e.Direction != f.Direction {
		return false
	}
	if f.SessionID != "" && e.SessionID != f.SessionID {
		return false
	}
	if f.RunID != "" && e.RunID != f.RunID {
		return false
	}
	if !f.Since.IsZero() && e.RecordedAt.Before(f.Since) {
		return false
	}
	return true
}

func cloneEntry(e Entry) Entry {
	out := e
	if e.Headers != nil {
		out.Headers = make(map[string]string, len(e.Headers))
		for k, v := range e.Headers {
			out.Headers[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

type contextKey int

const (
	contextKeySessionID contextKey = iota
	contextKeyRunID
)

// WithSessionID returns a context carrying the given session ID.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeySessionID, id)
}

// WithRunID returns a context carrying the given run ID.
func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyRunID, id)
}
