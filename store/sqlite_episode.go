package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

var (
	_ agent.SessionEpisodeManager = (*SQLiteSessionStore)(nil)
	_ contextengine.EpisodeWriter = (*SQLiteSessionStore)(nil)
	_ contextengine.EpisodeReader = (*SQLiteSessionStore)(nil)
)

func (s *SQLiteSessionStore) EnsureActiveEpisode(ctx context.Context, sessionID string, reason string) (string, error) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	if episodeID, err := s.ActiveEpisode(ctx, sessionID); err != nil {
		return "", err
	} else if strings.TrimSpace(episodeID) != "" {
		return episodeID, nil
	}
	return s.CreateEpisode(ctx, sessionID, defaultEpisodeReason(reason))
}

func (s *SQLiteSessionStore) StartNewEpisode(ctx context.Context, sessionID string, reason string) (string, error) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	current, err := s.ActiveEpisode(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != "" {
		seqEnd, err := s.maxMessageSeq(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if err := s.SealEpisode(ctx, current, seqEnd); err != nil {
			return "", err
		}
	}
	return s.CreateEpisode(ctx, sessionID, defaultEpisodeReason(reason))
}

func (s *SQLiteSessionStore) CreateEpisode(ctx context.Context, sessionID string, reason string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("session %s not found", sessionID)
		}
		return "", err
	}

	var activeID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM session_episodes
		 WHERE session_id = ? AND status = 'active'
		 ORDER BY seq_num DESC LIMIT 1`,
		sessionID,
	).Scan(&activeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if strings.TrimSpace(activeID) != "" {
		return "", fmt.Errorf("session %s already has active episode %s", sessionID, activeID)
	}

	var nextSeq int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq_num), 0) + 1 FROM session_episodes WHERE session_id = ?`,
		sessionID,
	).Scan(&nextSeq); err != nil {
		return "", err
	}

	episodeID, err := newSQLiteRandomID("ep")
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO session_episodes (
			id, session_id, seq_num, status, started_at, sealed_at,
			msg_seq_start, msg_seq_end, message_count, trigger_reason, metadata
		) VALUES (?, ?, ?, 'active', ?, NULL, NULL, NULL, 0, ?, '{}')`,
		episodeID, sessionID, nextSeq, formatTime(now), strings.TrimSpace(reason),
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return episodeID, nil
}

func (s *SQLiteSessionStore) SealEpisode(ctx context.Context, episodeID string, seqEnd int64) error {
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return fmt.Errorf("episode id is required")
	}

	now := time.Now().UTC()
	query := `UPDATE session_episodes
		SET status = 'sealed',
		    sealed_at = ?`
	args := []any{formatTime(now)}
	if seqEnd > 0 {
		query += `,
		    msg_seq_end = CASE
		        WHEN msg_seq_end IS NULL OR msg_seq_end < ? THEN ?
		        ELSE msg_seq_end
		    END`
		args = append(args, seqEnd, seqEnd)
	}
	query += ` WHERE id = ?`
	args = append(args, episodeID)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("episode %s not found", episodeID)
	}
	if err := contextengine.GenerateL3EpisodeOverview(ctx, s, s, episodeID); err != nil {
		slog.Warn("failed to generate l3 episode overview", "episode_id", episodeID, "error", err)
	}
	return nil
}

func (s *SQLiteSessionStore) ActiveEpisode(ctx context.Context, sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	var episodeID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM session_episodes
		 WHERE session_id = ? AND status = 'active'
		 ORDER BY seq_num DESC LIMIT 1`,
		sessionID,
	).Scan(&episodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return episodeID, nil
}

func (s *SQLiteSessionStore) ListEpisodes(ctx context.Context, sessionID string) ([]contextengine.EpisodeSummary, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq_num, status, started_at, sealed_at, message_count
		 FROM session_episodes
		 WHERE session_id = ?
		 ORDER BY seq_num`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []contextengine.EpisodeSummary
	for rows.Next() {
		episode, err := scanEpisodeSummary(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return episodes, nil
}

type episodeScanner interface {
	Scan(dest ...any) error
}

func scanEpisodeSummary(row episodeScanner) (contextengine.EpisodeSummary, error) {
	var (
		episode      contextengine.EpisodeSummary
		startedAtRaw string
		sealedAtRaw  sql.NullString
	)
	if err := row.Scan(
		&episode.ID,
		&episode.SessionID,
		&episode.SeqNum,
		&episode.Status,
		&startedAtRaw,
		&sealedAtRaw,
		&episode.MessageCount,
	); err != nil {
		return contextengine.EpisodeSummary{}, err
	}

	startedAt, err := parseTime(startedAtRaw, "sqlite episodes", episode.ID, "started_at")
	if err != nil {
		return contextengine.EpisodeSummary{}, err
	}
	sealedAt, err := parseTime(sealedAtRaw.String, "sqlite episodes", episode.ID, "sealed_at")
	if err != nil {
		return contextengine.EpisodeSummary{}, err
	}
	episode.StartedAt = startedAt
	episode.SealedAt = sealedAt
	return episode, nil
}

func (s *SQLiteSessionStore) maxMessageSeq(ctx context.Context, sessionID string) (int64, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, fmt.Errorf("session id is required")
	}
	var seq sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM messages WHERE session_id = ?`,
		sessionID,
	).Scan(&seq); err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

func defaultEpisodeReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "default"
	}
	return strings.TrimSpace(reason)
}
