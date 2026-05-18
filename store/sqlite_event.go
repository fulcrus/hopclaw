package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
)

// ---------------------------------------------------------------------------
// SQLiteEventSink
// ---------------------------------------------------------------------------

// SQLiteEventSink implements eventbus.Sink for persistent event storage
// and provides Replay/ReplaySince methods for loading historical events.
type SQLiteEventSink struct {
	db     *sql.DB
	nextID atomic.Uint64
}

func NewSQLiteEventSink(db *sql.DB) *SQLiteEventSink {
	s := &SQLiteEventSink{db: db}
	s.nextID.Store(recoverMaxIDCounter(db, "events", "evt-"))
	return s
}

// Handle persists a single event. It implements eventbus.Sink.
func (s *SQLiteEventSink) Handle(ctx context.Context, event eventbus.Event) error {
	if event.ID == "" {
		event.ID = fmt.Sprintf("evt-%06d", s.nextID.Add(1))
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	attrsJSON, err := marshalJSONValue(event.Attrs)
	if err != nil {
		return fmt.Errorf("marshal event attrs: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO events (id, type, run_id, session_id, timestamp, attrs)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID, string(event.Type),
		event.RunID, event.SessionID,
		formatTime(event.Time),
		attrsJSON,
	)
	return err
}

// Replay loads all persisted events in insertion order.
// New callers should prefer ReplayContext so replay can be cancelled.
func (s *SQLiteEventSink) Replay() ([]eventbus.Event, error) {
	return s.ReplayContext(context.Background())
}

// ReplayContext loads all persisted events in insertion order with caller
// cancellation support.
func (s *SQLiteEventSink) ReplayContext(ctx context.Context) ([]eventbus.Event, error) {
	return s.queryEventsContext(ctx, `SELECT id, type, run_id, session_id, timestamp, attrs FROM events ORDER BY id`)
}

// ReplaySince loads events after the given cursor ID, up to limit.
// New callers should prefer ReplaySinceContext so replay can be cancelled.
func (s *SQLiteEventSink) ReplaySince(sinceID string, limit int) ([]eventbus.Event, error) {
	return s.ReplaySinceContext(context.Background(), sinceID, limit)
}

// ReplaySinceContext loads events after the given cursor ID, up to limit,
// with caller cancellation support.
func (s *SQLiteEventSink) ReplaySinceContext(ctx context.Context, sinceID string, limit int) ([]eventbus.Event, error) {
	q := `SELECT id, type, run_id, session_id, timestamp, attrs FROM events`
	var args []any
	if sinceID != "" {
		q += ` WHERE id > ?`
		args = append(args, sinceID)
	}
	q += ` ORDER BY id`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	return s.queryEventsContext(ctx, q, args...)
}

func (s *SQLiteEventSink) PruneEvents(ctx context.Context, before time.Time) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE timestamp < ?`, formatTime(before))
	if err != nil {
		return 0, fmt.Errorf("prune events: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune events rows affected: %w", err)
	}
	return int(rows), nil
}

func (s *SQLiteEventSink) queryEventsContext(ctx context.Context, query string, args ...any) ([]eventbus.Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []eventbus.Event
	for rows.Next() {
		var (
			id, typ, runID, sessionID string
			timestampStr, attrsJSON   string
		)
		if err := rows.Scan(&id, &typ, &runID, &sessionID, &timestampStr, &attrsJSON); err != nil {
			return nil, err
		}
		attrs, err := decodeJSONMapField(attrsJSON, "sqlite events", id, "attrs")
		if err != nil {
			return nil, err
		}
		timestamp, err := parseTime(timestampStr, "sqlite events", id, "timestamp")
		if err != nil {
			return nil, err
		}
		out = append(out, eventbus.Event{
			ID:        id,
			Type:      eventbus.EventType(typ),
			RunID:     runID,
			SessionID: sessionID,
			Time:      timestamp,
			Attrs:     attrs,
		})
	}
	return out, rows.Err()
}
