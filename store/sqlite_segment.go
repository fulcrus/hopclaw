package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

var (
	_ contextengine.SegmentWriter   = (*SQLiteSessionStore)(nil)
	_ contextengine.SegmentReader   = (*SQLiteSessionStore)(nil)
	_ contextengine.SegmentSearcher = (*SQLiteSessionStore)(nil)
)

func (s *SQLiteSessionStore) InsertSegment(ctx context.Context, seg contextengine.SummarySegment) error {
	normalized, err := normalizeSummarySegment(seg)
	if err != nil {
		return err
	}
	decisionsJSON, err := marshalJSONSliceValue(normalized.Decisions)
	if err != nil {
		return fmt.Errorf("marshal segment decisions: %w", err)
	}
	todosJSON, err := marshalJSONSliceValue(normalized.TODOs)
	if err != nil {
		return fmt.Errorf("marshal segment todos: %w", err)
	}
	constraintsJSON, err := marshalJSONSliceValue(normalized.Constraints)
	if err != nil {
		return fmt.Errorf("marshal segment constraints: %w", err)
	}
	entitiesJSON, err := marshalJSONSliceValue(normalized.Entities)
	if err != nil {
		return fmt.Errorf("marshal segment entities: %w", err)
	}
	artifactRefsJSON, err := marshalJSONSliceValue(normalized.ArtifactRefs)
	if err != nil {
		return fmt.Errorf("marshal segment artifact refs: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM session_episodes WHERE id = ? AND session_id = ?`,
		normalized.EpisodeID, normalized.SessionID,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("episode %s not found", normalized.EpisodeID)
		}
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO summary_segments (
			id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
			summary_text, decisions_json, todos_json, constraints_json, entities_json,
			artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ID,
		normalized.SessionID,
		normalized.EpisodeID,
		normalized.Level,
		normalized.SeqStart,
		normalized.SeqEnd,
		formatTime(normalized.TSStart),
		formatTime(normalized.TSEnd),
		normalized.SummaryText,
		decisionsJSON,
		todosJSON,
		constraintsJSON,
		entitiesJSON,
		artifactRefsJSON,
		encodeVector(normalized.Embedding),
		normalized.Keywords,
		normalized.QualityScore,
		nullIfBlank(normalized.ParentSegmentID),
		formatTime(normalized.CreatedAt),
	); err != nil {
		return err
	}

	if normalized.Level == 1 {
		delta := normalized.SeqEnd - normalized.SeqStart + 1
		if delta < 0 {
			delta = 0
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE session_episodes
			 SET msg_seq_start = CASE
			         WHEN msg_seq_start IS NULL OR msg_seq_start > ? THEN ?
			         ELSE msg_seq_start
			     END,
			     msg_seq_end = CASE
			         WHEN msg_seq_end IS NULL OR msg_seq_end < ? THEN ?
			         ELSE msg_seq_end
			     END,
			     message_count = COALESCE(message_count, 0) + ?
			 WHERE id = ?`,
			normalized.SeqStart, normalized.SeqStart,
			normalized.SeqEnd, normalized.SeqEnd,
			delta,
			normalized.EpisodeID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteSessionStore) RecentSegments(ctx context.Context, sessionID string, level int, limit int) ([]contextengine.SummarySegment, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if level <= 0 {
		level = 1
	}
	if limit <= 0 {
		limit = 1
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
		        summary_text, decisions_json, todos_json, constraints_json, entities_json,
		        artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
		   FROM summary_segments
		  WHERE session_id = ? AND level = ?
		  ORDER BY seq_start DESC, created_at DESC
		  LIMIT ?`,
		sessionID, level, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []contextengine.SummarySegment
	for rows.Next() {
		segment, err := scanSummarySegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (s *SQLiteSessionStore) SegmentsByEpisode(ctx context.Context, episodeID string) ([]contextengine.SummarySegment, error) {
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, fmt.Errorf("episode id is required")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
		        summary_text, decisions_json, todos_json, constraints_json, entities_json,
		        artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
		   FROM summary_segments
		  WHERE episode_id = ?
		  ORDER BY level, seq_start, created_at`,
		episodeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []contextengine.SummarySegment
	for rows.Next() {
		segment, err := scanSummarySegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (s *SQLiteSessionStore) UnparentedL1Segments(ctx context.Context, episodeID string, limit int) ([]contextengine.SummarySegment, error) {
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, fmt.Errorf("episode id is required")
	}
	query := `SELECT id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
	        summary_text, decisions_json, todos_json, constraints_json, entities_json,
	        artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
	   FROM summary_segments
	  WHERE episode_id = ? AND level = 1 AND (parent_segment_id IS NULL OR parent_segment_id = '')
	  ORDER BY seq_start, created_at`
	args := []any{episodeID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []contextengine.SummarySegment
	for rows.Next() {
		segment, err := scanSummarySegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (s *SQLiteSessionStore) UpdateParentSegmentID(ctx context.Context, segmentID string, parentSegmentID string) error {
	segmentID = strings.TrimSpace(segmentID)
	if segmentID == "" {
		return fmt.Errorf("segment id is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE summary_segments
		    SET parent_segment_id = ?
		  WHERE id = ?`,
		nullIfBlank(parentSegmentID), segmentID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("segment %s not found", segmentID)
	}
	return nil
}

func (s *SQLiteSessionStore) SearchSegments(ctx context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]contextengine.SummarySegment, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 1
	}

	segments, err := s.loadSessionSegments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, nil
	}
	if len(queryEmbedding) > 0 {
		return rankSegmentsByEmbedding(segments, queryEmbedding, limit), nil
	}
	return rankSegmentsByKeywords(segments, queryText, limit), nil
}

func normalizeSummarySegment(seg contextengine.SummarySegment) (contextengine.SummarySegment, error) {
	normalized := seg
	normalized.SessionID = strings.TrimSpace(normalized.SessionID)
	if normalized.SessionID == "" {
		return contextengine.SummarySegment{}, fmt.Errorf("session id is required")
	}
	normalized.EpisodeID = strings.TrimSpace(normalized.EpisodeID)
	if normalized.EpisodeID == "" {
		return contextengine.SummarySegment{}, fmt.Errorf("episode id is required")
	}
	if normalized.Level <= 0 {
		normalized.Level = 1
	}
	if normalized.SeqStart <= 0 {
		return contextengine.SummarySegment{}, fmt.Errorf("seq_start must be positive")
	}
	if normalized.SeqEnd < normalized.SeqStart {
		return contextengine.SummarySegment{}, fmt.Errorf("seq_end must be >= seq_start")
	}
	if normalized.ID == "" {
		id, err := newSQLiteRandomID("seg")
		if err != nil {
			return contextengine.SummarySegment{}, err
		}
		normalized.ID = id
	}
	normalized.SummaryText = strings.TrimSpace(normalized.SummaryText)
	if normalized.SummaryText == "" {
		return contextengine.SummarySegment{}, fmt.Errorf("summary text is required")
	}
	now := time.Now().UTC()
	if normalized.TSStart.IsZero() {
		normalized.TSStart = now
	}
	if normalized.TSEnd.IsZero() {
		normalized.TSEnd = normalized.TSStart
	}
	if normalized.TSEnd.Before(normalized.TSStart) {
		normalized.TSEnd = normalized.TSStart
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.ParentSegmentID = strings.TrimSpace(normalized.ParentSegmentID)
	normalized.Keywords = strings.TrimSpace(normalized.Keywords)
	return normalized, nil
}

func nullIfBlank(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

type summarySegmentScanner interface {
	Scan(dest ...any) error
}

func scanSummarySegment(row summarySegmentScanner) (contextengine.SummarySegment, error) {
	var (
		segment                                   contextengine.SummarySegment
		tsStartRaw, tsEndRaw, createdAtRaw        string
		parentSegmentID                           sql.NullString
		decisionsJSON, todosJSON, constraintsJSON string
		entitiesJSON, artifactRefsJSON            string
		embeddingRaw                              []byte
	)
	if err := row.Scan(
		&segment.ID,
		&segment.SessionID,
		&segment.EpisodeID,
		&segment.Level,
		&segment.SeqStart,
		&segment.SeqEnd,
		&tsStartRaw,
		&tsEndRaw,
		&segment.SummaryText,
		&decisionsJSON,
		&todosJSON,
		&constraintsJSON,
		&entitiesJSON,
		&artifactRefsJSON,
		&embeddingRaw,
		&segment.Keywords,
		&segment.QualityScore,
		&parentSegmentID,
		&createdAtRaw,
	); err != nil {
		return contextengine.SummarySegment{}, err
	}

	tsStart, err := parseTime(tsStartRaw, "sqlite segments", segment.ID, "ts_start")
	if err != nil {
		return contextengine.SummarySegment{}, err
	}
	tsEnd, err := parseTime(tsEndRaw, "sqlite segments", segment.ID, "ts_end")
	if err != nil {
		return contextengine.SummarySegment{}, err
	}
	createdAt, err := parseTime(createdAtRaw, "sqlite segments", segment.ID, "created_at")
	if err != nil {
		return contextengine.SummarySegment{}, err
	}
	segment.TSStart = tsStart
	segment.TSEnd = tsEnd
	segment.CreatedAt = createdAt
	segment.ParentSegmentID = strings.TrimSpace(parentSegmentID.String)

	if segment.Decisions, err = decodeJSONStringSliceField(decisionsJSON, "sqlite segments", segment.ID, "decisions_json"); err != nil {
		return contextengine.SummarySegment{}, err
	}
	if segment.TODOs, err = decodeJSONStringSliceField(todosJSON, "sqlite segments", segment.ID, "todos_json"); err != nil {
		return contextengine.SummarySegment{}, err
	}
	if segment.Constraints, err = decodeJSONStringSliceField(constraintsJSON, "sqlite segments", segment.ID, "constraints_json"); err != nil {
		return contextengine.SummarySegment{}, err
	}
	if segment.Entities, err = decodeJSONStringSliceField(entitiesJSON, "sqlite segments", segment.ID, "entities_json"); err != nil {
		return contextengine.SummarySegment{}, err
	}
	if segment.ArtifactRefs, err = decodeJSONStringSliceField(artifactRefsJSON, "sqlite segments", segment.ID, "artifact_refs"); err != nil {
		return contextengine.SummarySegment{}, err
	}
	segment.Embedding = decodeVector(embeddingRaw)

	return segment, nil
}

func (s *SQLiteSessionStore) loadSessionSegments(ctx context.Context, sessionID string) ([]contextengine.SummarySegment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
		        summary_text, decisions_json, todos_json, constraints_json, entities_json,
		        artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
		   FROM summary_segments
		  WHERE session_id = ?`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []contextengine.SummarySegment
	for rows.Next() {
		segment, err := scanSummarySegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

const vectorBytesPerFloat32 = 4

func encodeVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*vectorBytesPerFloat32)
	for i, value := range v {
		binary.LittleEndian.PutUint32(buf[i*vectorBytesPerFloat32:], math.Float32bits(value))
	}
	return buf
}

func decodeVector(data []byte) []float32 {
	if len(data) == 0 || len(data)%vectorBytesPerFloat32 != 0 {
		return nil
	}
	vector := make([]float32, len(data)/vectorBytesPerFloat32)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*vectorBytesPerFloat32:]))
	}
	return vector
}

func rankSegmentsByEmbedding(segments []contextengine.SummarySegment, query []float32, limit int) []contextengine.SummarySegment {
	type scoredSegment struct {
		segment contextengine.SummarySegment
		score   float64
	}

	scored := make([]scoredSegment, 0, len(segments))
	for _, segment := range segments {
		if len(segment.Embedding) == 0 {
			continue
		}
		scored = append(scored, scoredSegment{
			segment: segment,
			score:   cosineSimilarity(segment.Embedding, query),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].segment.QualityScore != scored[j].segment.QualityScore {
			return scored[i].segment.QualityScore > scored[j].segment.QualityScore
		}
		if scored[i].segment.SeqEnd != scored[j].segment.SeqEnd {
			return scored[i].segment.SeqEnd > scored[j].segment.SeqEnd
		}
		return scored[i].segment.CreatedAt.After(scored[j].segment.CreatedAt)
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]contextengine.SummarySegment, 0, len(scored))
	for _, item := range scored {
		results = append(results, item.segment)
	}
	return results
}

func rankSegmentsByKeywords(segments []contextengine.SummarySegment, queryText string, limit int) []contextengine.SummarySegment {
	tokens := keywordMatchTokens(queryText)
	if len(tokens) == 0 {
		return nil
	}

	type scoredSegment struct {
		segment contextengine.SummarySegment
		score   int
	}

	scored := make([]scoredSegment, 0, len(segments))
	for _, segment := range segments {
		score := keywordMatchScore(segment.Keywords, tokens)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSegment{segment: segment, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].segment.QualityScore != scored[j].segment.QualityScore {
			return scored[i].segment.QualityScore > scored[j].segment.QualityScore
		}
		if scored[i].segment.SeqEnd != scored[j].segment.SeqEnd {
			return scored[i].segment.SeqEnd > scored[j].segment.SeqEnd
		}
		return scored[i].segment.CreatedAt.After(scored[j].segment.CreatedAt)
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]contextengine.SummarySegment, 0, len(scored))
	for _, item := range scored {
		results = append(results, item.segment)
	}
	return results
}

func keywordMatchTokens(queryText string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(queryText)), func(r rune) bool {
		return !(r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z'))
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		tokens = append(tokens, field)
	}
	return tokens
}

func keywordMatchScore(keywords string, tokens []string) int {
	if strings.TrimSpace(keywords) == "" || len(tokens) == 0 {
		return 0
	}
	score := 0
	normalized := strings.ToLower(keywords)
	for _, token := range tokens {
		if strings.Contains(normalized, token) {
			score++
		}
	}
	return score
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
