package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

var (
	_ contextengine.StateWriter = (*SQLiteSessionStore)(nil)
	_ contextengine.StateReader = (*SQLiteSessionStore)(nil)
)

func (s *SQLiteSessionStore) UpsertState(ctx context.Context, sessionID string, entries []contextengine.StateEntry) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session %s not found", sessionID)
		}
		return err
	}

	for _, entry := range entries {
		normalized, err := normalizeStateEntry(entry)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO session_state (
				session_id, key, category, value, status,
				source_episode, source_segment, confidence,
				created_at, updated_at, expires_at, superseded_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id, key) DO UPDATE SET
				category = excluded.category,
				value = excluded.value,
				status = excluded.status,
				source_episode = excluded.source_episode,
				source_segment = excluded.source_segment,
				confidence = excluded.confidence,
				updated_at = excluded.updated_at,
				expires_at = excluded.expires_at,
				superseded_by = excluded.superseded_by`,
			sessionID,
			normalized.Key,
			normalized.Category,
			normalized.Value,
			normalized.Status,
			nullIfBlank(normalized.SourceEpisode),
			nullIfBlank(normalized.SourceSegment),
			normalized.Confidence,
			formatTime(normalized.CreatedAt),
			formatTime(normalized.UpdatedAt),
			nullIfBlank(formatTime(normalized.ExpiresAt)),
			nullIfBlank(normalized.SupersededBy),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteSessionStore) ActiveStates(ctx context.Context, sessionID string) ([]contextengine.StateEntry, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}

	now := formatTime(time.Now().UTC())
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, category, value, status, source_episode, source_segment,
		        confidence, created_at, updated_at, expires_at, superseded_by
		   FROM session_state
		  WHERE session_id = ?
		    AND status = 'active'
		    AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)
		  ORDER BY CASE category
		               WHEN 'decision' THEN 0
		               WHEN 'constraint' THEN 1
		               WHEN 'todo' THEN 2
		               ELSE 3
		           END,
		           key`,
		sessionID, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []contextengine.StateEntry
	for rows.Next() {
		entry, err := scanStateEntry(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return states, nil
}

func normalizeStateEntry(entry contextengine.StateEntry) (contextengine.StateEntry, error) {
	normalized := entry
	normalized.Key = strings.TrimSpace(normalized.Key)
	if normalized.Key == "" {
		return contextengine.StateEntry{}, fmt.Errorf("state key is required")
	}
	normalized.Category = strings.TrimSpace(normalized.Category)
	if normalized.Category == "" {
		return contextengine.StateEntry{}, fmt.Errorf("state category is required")
	}
	normalized.Value = strings.TrimSpace(normalized.Value)
	if normalized.Value == "" {
		return contextengine.StateEntry{}, fmt.Errorf("state value is required")
	}
	normalized.Status = strings.TrimSpace(normalized.Status)
	if normalized.Status == "" {
		normalized.Status = "active"
	}
	if normalized.Confidence <= 0 {
		normalized.Confidence = 1.0
	}
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = normalized.CreatedAt
	}
	if normalized.UpdatedAt.Before(normalized.CreatedAt) {
		normalized.UpdatedAt = normalized.CreatedAt
	}
	normalized.SourceEpisode = strings.TrimSpace(normalized.SourceEpisode)
	normalized.SourceSegment = strings.TrimSpace(normalized.SourceSegment)
	normalized.SupersededBy = strings.TrimSpace(normalized.SupersededBy)
	return normalized, nil
}

type stateScanner interface {
	Scan(dest ...any) error
}

func scanStateEntry(row stateScanner) (contextengine.StateEntry, error) {
	var (
		entry                                                 contextengine.StateEntry
		sourceEpisode, sourceSegment, expiresAt, supersededBy sql.NullString
		createdAtRaw, updatedAtRaw                            string
	)
	if err := row.Scan(
		&entry.Key,
		&entry.Category,
		&entry.Value,
		&entry.Status,
		&sourceEpisode,
		&sourceSegment,
		&entry.Confidence,
		&createdAtRaw,
		&updatedAtRaw,
		&expiresAt,
		&supersededBy,
	); err != nil {
		return contextengine.StateEntry{}, err
	}

	var err error
	if entry.CreatedAt, err = parseTime(createdAtRaw, "sqlite state", entry.Key, "created_at"); err != nil {
		return contextengine.StateEntry{}, err
	}
	if entry.UpdatedAt, err = parseTime(updatedAtRaw, "sqlite state", entry.Key, "updated_at"); err != nil {
		return contextengine.StateEntry{}, err
	}
	if entry.ExpiresAt, err = parseTime(expiresAt.String, "sqlite state", entry.Key, "expires_at"); err != nil {
		return contextengine.StateEntry{}, err
	}
	entry.SourceEpisode = strings.TrimSpace(sourceEpisode.String)
	entry.SourceSegment = strings.TrimSpace(sourceSegment.String)
	entry.SupersededBy = strings.TrimSpace(supersededBy.String)
	return entry, nil
}
