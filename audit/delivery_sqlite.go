package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
)

type SQLiteDeliveryStore struct {
	db     *sql.DB
	nextID atomic.Uint64
}

func NewSQLiteDeliveryStore(db *sql.DB) (*SQLiteDeliveryStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite audit delivery db is required")
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS audit_delivery_outbox (
    id              TEXT PRIMARY KEY,
    sink_name       TEXT NOT NULL,
    event_id        TEXT NOT NULL DEFAULT '',
    event_type      TEXT NOT NULL DEFAULT '',
    run_id          TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    record          TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL DEFAULT '',
    last_attempt_at TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    delivered_at    TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_delivery_sink_event
    ON audit_delivery_outbox (sink_name, event_id)
    WHERE sink_name != '' AND event_id != '';
CREATE INDEX IF NOT EXISTS idx_audit_delivery_status_due
    ON audit_delivery_outbox (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_audit_delivery_sink
    ON audit_delivery_outbox (sink_name);
CREATE INDEX IF NOT EXISTS idx_audit_delivery_run
    ON audit_delivery_outbox (run_id);
CREATE INDEX IF NOT EXISTS idx_audit_delivery_session
    ON audit_delivery_outbox (session_id);
`); err != nil {
		return nil, fmt.Errorf("init audit delivery outbox table: %w", err)
	}
	store := &SQLiteDeliveryStore{db: db}
	var maxID uint64
	if err := db.QueryRow(`SELECT COALESCE(MAX(CAST(SUBSTR(id, 6) AS INTEGER)), 0) FROM audit_delivery_outbox WHERE id LIKE 'adel-%'`).Scan(&maxID); err == nil {
		store.nextID.Store(maxID)
	}
	return store, nil
}

func (s *SQLiteDeliveryStore) Enqueue(ctx context.Context, entry DeliveryEntry) (*DeliveryEntry, error) {
	recordJSON, err := json.Marshal(cloneEvent(entry.Event))
	if err != nil {
		return nil, fmt.Errorf("marshal audit delivery record: %w", err)
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.NextAttemptAt.IsZero() {
		entry.NextAttemptAt = entry.CreatedAt
	}
	if entry.Status == "" {
		entry.Status = DeliveryStatusPending
	}
	id := fmt.Sprintf("adel-%06d", s.nextID.Add(1))
	result, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO audit_delivery_outbox
    (id, sink_name, event_id, event_type, run_id, session_id, record, status, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		strings.TrimSpace(entry.SinkName),
		strings.TrimSpace(entry.EventID),
		strings.TrimSpace(string(entry.EventType)),
		strings.TrimSpace(entry.RunID),
		strings.TrimSpace(entry.SessionID),
		string(recordJSON),
		string(entry.Status),
		entry.Attempts,
		entry.MaxAttempts,
		strings.TrimSpace(entry.LastError),
		formatAuditDeliveryTime(entry.NextAttemptAt),
		formatAuditDeliveryTime(entry.LastAttemptAt),
		formatAuditDeliveryTime(entry.CreatedAt),
		formatAuditDeliveryTime(entry.UpdatedAt),
		formatAuditDeliveryTime(entry.DeliveredAt),
	)
	if err != nil {
		return nil, fmt.Errorf("enqueue audit delivery: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return s.getBySinkEvent(ctx, entry.SinkName, entry.EventID)
	}
	return s.Get(ctx, id)
}

func (s *SQLiteDeliveryStore) Due(ctx context.Context, now time.Time, limit int) ([]*DeliveryEntry, error) {
	if limit <= 0 {
		limit = 32
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, sink_name, event_id, event_type, run_id, session_id, record, status, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at
FROM audit_delivery_outbox
WHERE status = ? AND (next_attempt_at = '' OR next_attempt_at <= ?)
ORDER BY next_attempt_at ASC, created_at ASC
LIMIT ?`,
		string(DeliveryStatusPending),
		formatAuditDeliveryTime(now),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query due audit deliveries: %w", err)
	}
	defer rows.Close()
	return scanAuditDeliveryRows(rows)
}

func (s *SQLiteDeliveryStore) Save(ctx context.Context, entry *DeliveryEntry) error {
	if entry == nil {
		return ErrDeliveryNotFound
	}
	recordJSON, err := json.Marshal(cloneEvent(entry.Event))
	if err != nil {
		return fmt.Errorf("marshal audit delivery record: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE audit_delivery_outbox
SET sink_name = ?, event_id = ?, event_type = ?, run_id = ?, session_id = ?, record = ?, status = ?, attempts = ?, max_attempts = ?, last_error = ?, next_attempt_at = ?, last_attempt_at = ?, created_at = ?, updated_at = ?, delivered_at = ?
WHERE id = ?`,
		strings.TrimSpace(entry.SinkName),
		strings.TrimSpace(entry.EventID),
		strings.TrimSpace(string(entry.EventType)),
		strings.TrimSpace(entry.RunID),
		strings.TrimSpace(entry.SessionID),
		string(recordJSON),
		string(entry.Status),
		entry.Attempts,
		entry.MaxAttempts,
		strings.TrimSpace(entry.LastError),
		formatAuditDeliveryTime(entry.NextAttemptAt),
		formatAuditDeliveryTime(entry.LastAttemptAt),
		formatAuditDeliveryTime(entry.CreatedAt),
		formatAuditDeliveryTime(entry.UpdatedAt),
		formatAuditDeliveryTime(entry.DeliveredAt),
		strings.TrimSpace(entry.ID),
	)
	if err != nil {
		return fmt.Errorf("save audit delivery: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (s *SQLiteDeliveryStore) Get(ctx context.Context, id string) (*DeliveryEntry, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, sink_name, event_id, event_type, run_id, session_id, record, status, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at
FROM audit_delivery_outbox
WHERE id = ?`,
		strings.TrimSpace(id),
	)
	entry, err := scanAuditDeliveryRow(row)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *SQLiteDeliveryStore) List(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error) {
	query := `SELECT id, sink_name, event_id, event_type, run_id, session_id, record, status, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at FROM audit_delivery_outbox WHERE 1=1`
	var args []any
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, string(filter.Status))
	}
	if name := strings.TrimSpace(filter.SinkName); name != "" {
		query += ` AND sink_name = ?`
		args = append(args, name)
	}
	if runID := strings.TrimSpace(filter.RunID); runID != "" {
		query += ` AND run_id = ?`
		args = append(args, runID)
	}
	if sessionID := strings.TrimSpace(filter.SessionID); sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if filter.EventType != "" {
		query += ` AND event_type = ?`
		args = append(args, string(filter.EventType))
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit deliveries: %w", err)
	}
	defer rows.Close()
	items, err := scanAuditDeliveryRows(rows)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.Query) == "" {
		return items, nil
	}
	filtered := make([]*DeliveryEntry, 0, len(items))
	baseFilter := DeliveryListFilter{Query: filter.Query}
	for _, item := range items {
		if matchesDeliveryFilter(item, baseFilter) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *SQLiteDeliveryStore) Stats(ctx context.Context, filter DeliveryListFilter) (DeliveryStats, error) {
	filter.Limit = 0
	items, err := s.List(ctx, filter)
	if err != nil {
		return DeliveryStats{}, err
	}
	return summarizeDeliveries(items), nil
}

func (s *SQLiteDeliveryStore) Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error) {
	var updated []*DeliveryEntry
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		entry, err := s.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrDeliveryNotFound) {
				continue
			}
			return nil, err
		}
		if entry.Status == DeliveryStatusDelivered {
			continue
		}
		entry.Status = DeliveryStatusPending
		entry.NextAttemptAt = time.Now().UTC()
		entry.UpdatedAt = entry.NextAttemptAt
		entry.DeliveredAt = time.Time{}
		if opts.ResetAttempts {
			entry.Attempts = 0
		}
		if opts.ClearError {
			entry.LastError = ""
		}
		if err := s.Save(ctx, entry); err != nil {
			return nil, err
		}
		updated = append(updated, entry)
	}
	return updated, nil
}

func (s *SQLiteDeliveryStore) getBySinkEvent(ctx context.Context, sinkName, eventID string) (*DeliveryEntry, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, sink_name, event_id, event_type, run_id, session_id, record, status, attempts, max_attempts, last_error, next_attempt_at, last_attempt_at, created_at, updated_at, delivered_at
FROM audit_delivery_outbox
WHERE sink_name = ? AND event_id = ?
LIMIT 1`,
		strings.TrimSpace(sinkName),
		strings.TrimSpace(eventID),
	)
	return scanAuditDeliveryRow(row)
}

type auditDeliveryRowScanner interface {
	Scan(dest ...any) error
}

func scanAuditDeliveryRows(rows *sql.Rows) ([]*DeliveryEntry, error) {
	items := []*DeliveryEntry{}
	for rows.Next() {
		entry, err := scanAuditDeliveryRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit deliveries: %w", err)
	}
	return items, nil
}

func scanAuditDeliveryRow(scanner auditDeliveryRowScanner) (*DeliveryEntry, error) {
	var (
		id, sinkName, eventID, eventType  string
		runID, sessionID, recordJSON      string
		status, lastError                 string
		nextAttemptAt, lastAttemptAt      string
		createdAt, updatedAt, deliveredAt string
		attempts, maxAttempts             int
	)
	if err := scanner.Scan(
		&id,
		&sinkName,
		&eventID,
		&eventType,
		&runID,
		&sessionID,
		&recordJSON,
		&status,
		&attempts,
		&maxAttempts,
		&lastError,
		&nextAttemptAt,
		&lastAttemptAt,
		&createdAt,
		&updatedAt,
		&deliveredAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeliveryNotFound
		}
		return nil, fmt.Errorf("scan audit delivery: %w", err)
	}
	var event eventbus.Event
	if err := json.Unmarshal([]byte(recordJSON), &event); err != nil {
		return nil, fmt.Errorf("decode audit delivery record: %w", err)
	}
	entry := &DeliveryEntry{
		ID:            strings.TrimSpace(id),
		SinkName:      strings.TrimSpace(sinkName),
		EventID:       strings.TrimSpace(eventID),
		EventType:     eventbus.EventType(strings.TrimSpace(eventType)),
		RunID:         strings.TrimSpace(runID),
		SessionID:     strings.TrimSpace(sessionID),
		Event:         cloneEvent(event),
		Status:        DeliveryStatus(strings.TrimSpace(status)),
		Attempts:      attempts,
		MaxAttempts:   maxAttempts,
		LastError:     strings.TrimSpace(lastError),
		NextAttemptAt: parseAuditDeliveryTime(nextAttemptAt),
		LastAttemptAt: parseAuditDeliveryTime(lastAttemptAt),
		CreatedAt:     parseAuditDeliveryTime(createdAt),
		UpdatedAt:     parseAuditDeliveryTime(updatedAt),
		DeliveredAt:   parseAuditDeliveryTime(deliveredAt),
	}
	return entry, nil
}

func formatAuditDeliveryTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseAuditDeliveryTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
