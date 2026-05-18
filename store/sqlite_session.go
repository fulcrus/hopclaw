package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/skill"
)

const (
	sqliteSessionLockRetryInterval  = 5 * time.Millisecond
	selectSessionFields             = `id, key, model, revision, summary, summary_at, scope, metadata, pinned_facts, skill_snapshot, created_at, updated_at, execution_watermark`
	transcriptEventUserAppended     = "user_message_appended"
	transcriptEventMessagesAppended = "messages_appended"
	transcriptEventSessionUpdated   = "session_updated"
)

// ---------------------------------------------------------------------------
// SQLiteSessionStore
// ---------------------------------------------------------------------------

// SQLiteSessionStore implements agent.SessionStore, agent.SessionLister,
// agent.SessionReader, and agent.SessionKeyReader backed by SQLite.
type SQLiteSessionStore struct {
	db     *sql.DB
	nextID atomic.Uint64
	locks  sync.Map // sessionID -> *sync.Mutex
}

var (
	_ agent.SessionQueryStore       = (*SQLiteSessionStore)(nil)
	_ agent.SessionMaintenanceStore = (*SQLiteSessionStore)(nil)
	_ agent.ExecutionSnapshotStore  = (*SQLiteSessionStore)(nil)
)

// NewSQLiteSessionStore creates a session store backed by the given database.
// It recovers the ID counter from existing data.
func NewSQLiteSessionStore(db *sql.DB) *SQLiteSessionStore {
	s := &SQLiteSessionStore{db: db}
	s.nextID.Store(recoverMaxIDCounter(db, "sessions", "sess-"))
	return s
}

func (s *SQLiteSessionStore) sessionLock(id string) *sync.Mutex {
	val, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// ---------------------------------------------------------------------------
// SessionStore interface
// ---------------------------------------------------------------------------

func (s *SQLiteSessionStore) GetOrCreate(ctx context.Context, sessionKey, defaultModel string, sessionID ...string) (*agent.Session, error) {
	// Try to read existing session first.
	row := s.db.QueryRowContext(ctx,
		`SELECT `+selectSessionFields+` FROM sessions WHERE key = ?`, sessionKey)

	sess, err := scanSession(row)
	if err == nil {
		msgs, loadErr := s.loadMessages(ctx, sess.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		sess.Messages = msgs
		sess.MessageCount = len(msgs)
		return sess, nil
	}

	// Create new session. Use caller-specified ID if provided.
	id := ""
	if len(sessionID) > 0 && sessionID[0] != "" {
		id = sessionID[0]
	}
	if id == "" {
		id = fmt.Sprintf("sess-%06d", s.nextID.Add(1))
	}
	now := time.Now().UTC()
	nowStr := formatTime(now)
	scopeJSON, err := marshalJSONValue(domainscope.Ref{})
	if err != nil {
		return nil, fmt.Errorf("marshal session scope: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, key, model, revision, scope, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, sessionKey, defaultModel, 1, scopeJSON, nowStr, nowStr,
	); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &agent.Session{
		ID:        id,
		Key:       sessionKey,
		Model:     defaultModel,
		Revision:  1,
		Scope:     domainscope.Ref{},
		CreatedAt: now,
		UpdatedAt: now,
		Session:   contextengine.Session{ID: id},
	}, nil
}

func (s *SQLiteSessionStore) AppendUserMessage(ctx context.Context, sessionID string, msg agent.IncomingMessage) error {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanSessionWithCount(tx.QueryRowContext(ctx,
		`SELECT `+selectSessionFields+`,
		 (SELECT COUNT(*) FROM messages WHERE messages.session_id = sessions.id) AS message_count
		 FROM sessions WHERE id = ?`,
		sessionID,
	))
	if err != nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if err := insertMessageWithExecer(ctx, tx, sessionID, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   msg.Content,
		CreatedAt: now,
		Metadata:  msg.Metadata,
	}); err != nil {
		return err
	}

	current.Metadata = agent.MergeSessionMetadata(current.Metadata, msg)
	current.Scope = agent.MergeScopeRef(current.Scope, msg)
	if msg.Model != "" {
		current.Model = msg.Model
	}
	nextRevision := current.Revision + 1
	scopeJSON, err := marshalJSONValue(current.Scope)
	if err != nil {
		return fmt.Errorf("marshal session scope: %w", err)
	}
	metadataJSON, err := marshalJSONValue(current.Metadata)
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	q := `UPDATE sessions SET model = ?, revision = ?, updated_at = ?, scope = ?, metadata = ? WHERE id = ?`
	args := []any{
		current.Model,
		nextRevision,
		formatTime(now),
		scopeJSON,
		metadataJSON,
		sessionID,
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return err
	}
	if err := insertTranscriptEvent(ctx, tx, sessionID, nextRevision, transcriptEventUserAppended, 1, now, map[string]any{
		"roles": []string{string(contextengine.RoleUser)},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteSessionStore) LoadForExecution(ctx context.Context, sessionID string) (*agent.Session, func(), error) {
	snapshot, release, err := s.LoadExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return snapshot.Session, release, nil
}

func (s *SQLiteSessionStore) LoadExecutionSnapshot(ctx context.Context, sessionID string) (*agent.ExecutionSnapshot, func(), error) {
	lock := s.sessionLock(sessionID)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if lock.TryLock() {
			if err := ctx.Err(); err != nil {
				lock.Unlock()
				return nil, nil, err
			}
			break
		}
		time.Sleep(sqliteSessionLockRetryInterval)
	}

	snapshot, err := s.loadExecutionSnapshot(ctx, sessionID)
	if err != nil {
		lock.Unlock()
		return nil, nil, err
	}
	return snapshot, lock.Unlock, nil
}

func (s *SQLiteSessionStore) Save(ctx context.Context, session *agent.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update session metadata.
	nextRevision := session.Revision + 1
	scopeJSON, err := marshalJSONValue(session.Scope)
	if err != nil {
		return fmt.Errorf("marshal session scope: %w", err)
	}
	metadataJSON, err := marshalJSONValue(session.Metadata)
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	pinnedFactsJSON, err := marshalJSONSliceValue(session.PinnedFacts)
	if err != nil {
		return fmt.Errorf("marshal session pinned facts: %w", err)
	}
	skillSnapshotJSON, err := marshalJSONValue(session.SkillSnapshot)
	if err != nil {
		return fmt.Errorf("marshal session skill snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET model = ?, revision = ?, summary = ?, summary_at = ?,
		 scope = ?, metadata = ?, pinned_facts = ?, skill_snapshot = ?, updated_at = ?, execution_watermark = ?
		 WHERE id = ?`,
		session.Model,
		nextRevision,
		session.Summary,
		formatTime(session.SummaryAt),
		scopeJSON,
		metadataJSON,
		pinnedFactsJSON,
		skillSnapshotJSON,
		formatTime(session.UpdatedAt),
		session.ExecutionWatermark,
		session.ID,
	); err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	var persistedCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, session.ID).Scan(&persistedCount); err != nil {
		return fmt.Errorf("count persisted messages: %w", err)
	}

	persisted, err := loadStoredMessagesFromQuerier(ctx, tx, session.ID, session.ExecutionWatermark)
	if err != nil {
		return fmt.Errorf("load persisted messages: %w", err)
	}
	loadedPersistedCount := len(persisted)
	if len(session.LoadedMessageSeqs) > 0 {
		loadedPersistedCount = len(session.LoadedMessageSeqs)
		if loadedPersistedCount > len(persisted) {
			return fmt.Errorf("update session %s: loaded transcript exceeds persisted tail", session.ID)
		}
	}
	if len(session.Messages) < loadedPersistedCount {
		return fmt.Errorf("update session %s: transcript truncation is not supported", session.ID)
	}
	for idx := 0; idx < loadedPersistedCount; idx++ {
		persistedMsg := persisted[idx]
		if len(session.LoadedMessageSeqs) > 0 && persistedMsg.Seq != session.LoadedMessageSeqs[idx] {
			return fmt.Errorf("update session %s: loaded message seq mismatch at message %d", session.ID, idx)
		}
		equal, err := storedMessagesEqual(persistedMsg.Message, session.Messages[idx])
		if err != nil {
			return fmt.Errorf("update session %s: compare transcript message %d: %w", session.ID, idx, err)
		}
		if !equal {
			return fmt.Errorf("update session %s: append-only transcript mismatch at message %d", session.ID, idx)
		}
	}
	nextSeq := session.ExecutionWatermark
	if len(persisted) > 0 {
		nextSeq = persisted[len(persisted)-1].Seq
	}
	loadedSeqs := make([]int64, 0, len(session.Messages))
	if len(session.LoadedMessageSeqs) > 0 {
		loadedSeqs = append(loadedSeqs, session.LoadedMessageSeqs[:loadedPersistedCount]...)
	} else {
		for _, persistedMsg := range persisted[:loadedPersistedCount] {
			loadedSeqs = append(loadedSeqs, persistedMsg.Seq)
		}
	}
	for _, msg := range session.Messages[loadedPersistedCount:] {
		if err := insertMessageWithExecer(ctx, tx, session.ID, msg); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		nextSeq++
		loadedSeqs = append(loadedSeqs, nextSeq)
	}
	eventType := transcriptEventSessionUpdated
	appended := session.Messages[loadedPersistedCount:]
	if len(appended) > 0 {
		eventType = transcriptEventMessagesAppended
	}
	if err := insertTranscriptEvent(ctx, tx, session.ID, nextRevision, eventType, len(appended), session.UpdatedAt, map[string]any{
		"roles": rolesForMessages(appended),
	}); err != nil {
		return fmt.Errorf("insert transcript event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	session.Revision = nextRevision
	session.MessageCount = persistedCount + len(appended)
	session.LoadedMessageSeqs = loadedSeqs
	return nil
}

// ---------------------------------------------------------------------------
// SessionLister / SessionReader / SessionKeyReader
// ---------------------------------------------------------------------------

func (s *SQLiteSessionStore) List(ctx context.Context) ([]*agent.Session, error) {
	return s.ListScoped(ctx, agent.SessionListFilter{})
}

func (s *SQLiteSessionStore) ListScoped(ctx context.Context, filter agent.SessionListFilter) ([]*agent.Session, error) {
	q := `SELECT ` + selectSessionFields + `,
			 (SELECT COUNT(*) FROM messages WHERE messages.session_id = sessions.id) AS message_count
			 FROM sessions WHERE 1=1`
	var args []any
	q += ` ORDER BY updated_at DESC`
	if filter.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*agent.Session
	for rows.Next() {
		sess, err := scanSessionWithCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteSessionStore) Get(ctx context.Context, sessionID string) (*agent.Session, error) {
	return s.GetScoped(ctx, sessionID, agent.ScopeFilter{})
}

func (s *SQLiteSessionStore) GetMetadata(ctx context.Context, sessionID string) (*agent.Session, error) {
	return s.GetMetadataScoped(ctx, sessionID, agent.ScopeFilter{})
}

func (s *SQLiteSessionStore) GetScoped(ctx context.Context, sessionID string, scope agent.ScopeFilter) (*agent.Session, error) {
	return s.loadSession(ctx, sessionID, scope)
}

func (s *SQLiteSessionStore) GetMetadataScoped(ctx context.Context, sessionID string, scope agent.ScopeFilter) (*agent.Session, error) {
	return s.loadSessionMetadata(ctx, sessionID, scope)
}

func (s *SQLiteSessionStore) GetByKey(ctx context.Context, sessionKey string) (*agent.Session, error) {
	return s.GetByKeyScoped(ctx, sessionKey, agent.ScopeFilter{})
}

func (s *SQLiteSessionStore) GetByKeyMetadata(ctx context.Context, sessionKey string) (*agent.Session, error) {
	return s.GetByKeyMetadataScoped(ctx, sessionKey, agent.ScopeFilter{})
}

func (s *SQLiteSessionStore) GetByKeyScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	sess, err := s.loadSessionMetadataByKey(ctx, sessionKey, scope)
	if err != nil {
		return nil, err
	}
	msgs, err := s.loadMessages(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	sess.Messages = msgs
	sess.MessageCount = len(msgs)
	return sess, nil
}

func (s *SQLiteSessionStore) GetByKeyMetadataScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	return s.loadSessionMetadataByKey(ctx, sessionKey, scope)
}

func (s *SQLiteSessionStore) RecentMessages(ctx context.Context, sessionID string, limit int) ([]contextengine.Message, error) {
	return s.loadRecentMessages(ctx, sessionID, limit)
}

// DeleteSession removes a session and all its messages.
func (s *SQLiteSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	mu := s.sessionLock(sessionID)
	mu.Lock()
	defer mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Check session exists.
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		return fmt.Errorf("session %s: not found", sessionID)
	}

	// Delete messages first, then session.
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *SQLiteSessionStore) loadSession(ctx context.Context, id string, scope ...agent.ScopeFilter) (*agent.Session, error) {
	sess, err := s.loadSessionMetadata(ctx, id, scope...)
	if err != nil {
		return nil, err
	}
	msgs, err := s.loadMessages(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	sess.Messages = msgs
	sess.MessageCount = len(msgs)
	return sess, nil
}

func (s *SQLiteSessionStore) loadExecutionSnapshot(ctx context.Context, id string) (*agent.ExecutionSnapshot, error) {
	sess, err := s.loadSessionMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	stored, err := loadStoredMessagesFromQuerier(ctx, s.db, sess.ID, sess.ExecutionWatermark)
	if err != nil {
		return nil, err
	}
	sess.Messages = make([]contextengine.Message, 0, len(stored))
	sess.LoadedMessageSeqs = make([]int64, 0, len(stored))
	for _, storedMsg := range stored {
		sess.Messages = append(sess.Messages, storedMsg.Message)
		sess.LoadedMessageSeqs = append(sess.LoadedMessageSeqs, storedMsg.Seq)
	}
	return &agent.ExecutionSnapshot{Session: sess}, nil
}

func (s *SQLiteSessionStore) loadSessionMetadata(ctx context.Context, id string, scope ...agent.ScopeFilter) (*agent.Session, error) {
	q := `SELECT ` + selectSessionFields + `,
			 (SELECT COUNT(*) FROM messages WHERE messages.session_id = sessions.id) AS message_count
			 FROM sessions WHERE id = ?`
	args := []any{id}
	row := s.db.QueryRowContext(ctx, q, args...)
	sess, err := scanSessionWithCount(row)
	if err != nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return sess, nil
}

func (s *SQLiteSessionStore) loadSessionMetadataByKey(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	q := `SELECT ` + selectSessionFields + `,
			 (SELECT COUNT(*) FROM messages WHERE messages.session_id = sessions.id) AS message_count
			 FROM sessions WHERE key = ?`
	args := []any{sessionKey}
	row := s.db.QueryRowContext(ctx, q, args...)
	sess, err := scanSessionWithCount(row)
	if err != nil {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return sess, nil
}

func (s *SQLiteSessionStore) PruneSessions(ctx context.Context, before time.Time) (int, error) {
	return s.PruneSessionsExcept(ctx, before, nil)
}

func (s *SQLiteSessionStore) PruneSessionsExcept(ctx context.Context, before time.Time, excludeSessionIDs []string) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	q := `DELETE FROM sessions
		WHERE updated_at != ''
		  AND updated_at < ?
		  AND NOT EXISTS (
			SELECT 1 FROM runs
			 WHERE runs.session_id = sessions.id
			   AND runs.status NOT IN (?, ?, ?)
		  )`
	args := []any{
		formatTime(before),
		string(agent.RunCompleted), string(agent.RunFailed), string(agent.RunCancelled),
	}
	if len(excludeSessionIDs) > 0 {
		placeholders := make([]string, 0, len(excludeSessionIDs))
		for _, id := range excludeSessionIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				placeholders = append(placeholders, "?")
				args = append(args, trimmed)
			}
		}
		if len(placeholders) > 0 {
			q += ` AND id NOT IN (` + strings.Join(placeholders, ",") + `)`
		}
	}
	result, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune sessions rows affected: %w", err)
	}
	return int(rows), nil
}

func (s *SQLiteSessionStore) loadMessages(ctx context.Context, sessionID string) ([]contextengine.Message, error) {
	return loadMessagesFromQuerier(ctx, s.db, sessionID)
}

type storedMessage struct {
	Seq     int64
	Message contextengine.Message
}

func loadMessagesFromQuerier(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, sessionID string) ([]contextengine.Message, error) {
	stored, err := loadStoredMessagesFromQuerier(ctx, querier, sessionID, 0)
	if err != nil {
		return nil, err
	}
	msgs := make([]contextengine.Message, 0, len(stored))
	for _, storedMsg := range stored {
		msgs = append(msgs, storedMsg.Message)
	}
	return msgs, nil
}

func loadStoredMessagesFromQuerier(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, sessionID string, afterSeq int64) ([]storedMessage, error) {
	query := `SELECT seq, role, content, content_blocks, name, tool_call_id, tool_calls, metadata, created_at
		 FROM messages WHERE session_id = ?`
	args := []any{sessionID}
	if afterSeq > 0 {
		query += ` AND seq > ?`
		args = append(args, afterSeq)
	}
	query += ` ORDER BY seq, id`

	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []storedMessage
	for rows.Next() {
		var (
			seq                              int64
			role, content, contentBlocksJSON string
			name, toolCallID                 string
			toolCallsJSON, metadataJSON      string
			createdAtStr                     string
		)
		if err := rows.Scan(&seq, &role, &content, &contentBlocksJSON, &name, &toolCallID, &toolCallsJSON, &metadataJSON, &createdAtStr); err != nil {
			return nil, err
		}
		msg := contextengine.Message{
			Role:       contextengine.MessageRole(role),
			Content:    content,
			Name:       name,
			ToolCallID: toolCallID,
		}
		createdAt, err := parseTime(createdAtStr, "sqlite messages", sessionID, "created_at")
		if err != nil {
			return nil, err
		}
		msg.CreatedAt = createdAt
		metadata, err := decodeJSONMapField(metadataJSON, "sqlite messages", sessionID, "metadata")
		if err != nil {
			return nil, err
		}
		msg.Metadata = metadata
		if contentBlocksJSON != "" && contentBlocksJSON != "[]" {
			var blocks []contextengine.ContentBlock
			if err := decodeJSONField(contentBlocksJSON, "sqlite messages", sessionID, "content_blocks", &blocks); err != nil {
				return nil, err
			}
			msg.ContentBlocks = blocks
		}
		if toolCallsJSON != "" && toolCallsJSON != "[]" {
			var refs []contextengine.ToolCallRef
			if err := decodeJSONField(toolCallsJSON, "sqlite messages", sessionID, "tool_calls", &refs); err != nil {
				return nil, err
			}
			msg.ToolCalls = refs
		}
		msgs = append(msgs, storedMessage{Seq: seq, Message: msg})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func storedMessagesEqual(a, b contextengine.Message) (bool, error) {
	if a.Role != b.Role ||
		a.Content != b.Content ||
		a.Name != b.Name ||
		a.ToolCallID != b.ToolCallID ||
		!timesEqual(a.CreatedAt, b.CreatedAt) {
		return false, nil
	}
	aContentBlocksJSON, err := marshalJSONSliceValue(a.ContentBlocks)
	if err != nil {
		return false, err
	}
	bContentBlocksJSON, err := marshalJSONSliceValue(b.ContentBlocks)
	if err != nil {
		return false, err
	}
	if aContentBlocksJSON != bContentBlocksJSON {
		return false, nil
	}
	aToolCallsJSON, err := marshalJSONSliceValue(a.ToolCalls)
	if err != nil {
		return false, err
	}
	bToolCallsJSON, err := marshalJSONSliceValue(b.ToolCalls)
	if err != nil {
		return false, err
	}
	if aToolCallsJSON != bToolCallsJSON {
		return false, nil
	}
	aMetadataJSON, err := marshalJSONValue(a.Metadata)
	if err != nil {
		return false, err
	}
	bMetadataJSON, err := marshalJSONValue(b.Metadata)
	if err != nil {
		return false, err
	}
	if aMetadataJSON != bMetadataJSON {
		return false, nil
	}
	return true, nil
}

func timesEqual(a, b time.Time) bool {
	switch {
	case a.IsZero() || b.IsZero():
		return a.IsZero() == b.IsZero()
	default:
		return a.UTC().Equal(b.UTC())
	}
}

func (s *SQLiteSessionStore) loadRecentMessages(ctx context.Context, sessionID string, limit int) ([]contextengine.Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content
		 FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reversed []contextengine.Message
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		reversed = append(reversed, contextengine.Message{
			Role:    contextengine.MessageRole(role),
			Content: content,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func insertMessageWithExecer(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, sessionID string, msg contextengine.Message) error {
	contentBlocksJSON, err := marshalJSONSliceValue(msg.ContentBlocks)
	if err != nil {
		return fmt.Errorf("marshal message content blocks: %w", err)
	}
	toolCallsJSON, err := marshalJSONSliceValue(msg.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal message tool calls: %w", err)
	}
	metadataJSON, err := marshalJSONValue(msg.Metadata)
	if err != nil {
		return fmt.Errorf("marshal message metadata: %w", err)
	}
	_, err = execer.ExecContext(ctx,
		`INSERT INTO messages (session_id, seq, role, content, content_blocks, name, tool_call_id, tool_calls, metadata, created_at)
		 VALUES (
			?,
			COALESCE((SELECT MAX(seq) FROM messages WHERE session_id = ?), 0) + 1,
			?, ?, ?, ?, ?, ?, ?, ?
		)`,
		sessionID,
		sessionID,
		string(msg.Role),
		msg.Content,
		contentBlocksJSON,
		msg.Name,
		msg.ToolCallID,
		toolCallsJSON,
		metadataJSON,
		formatTime(msg.CreatedAt),
	)
	return err
}

func insertTranscriptEvent(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, sessionID string, revision int64, eventType string, messageCountDelta int, createdAt time.Time, metadata map[string]any) error {
	metadataJSON, err := marshalJSONValue(metadata)
	if err != nil {
		return fmt.Errorf("marshal transcript event metadata: %w", err)
	}
	_, err = execer.ExecContext(ctx,
		`INSERT INTO transcript_events (session_id, session_revision, event_type, message_count_delta, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID,
		revision,
		eventType,
		messageCountDelta,
		metadataJSON,
		formatTime(createdAt),
	)
	return err
}

func rolesForMessages(messages []contextengine.Message) []string {
	if len(messages) == 0 {
		return nil
	}
	roles := make([]string, 0, len(messages))
	for _, msg := range messages {
		if role := strings.TrimSpace(string(msg.Role)); role != "" {
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return nil
	}
	return roles
}

// ---------------------------------------------------------------------------
// Row scanners
// ---------------------------------------------------------------------------

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (*agent.Session, error) {
	var (
		id, key, model             string
		revision                   int64
		summary, summaryAtStr      string
		scopeJSON                  string
		metadataJSON               string
		pinnedFactsJSON, skillJSON string
		createdAtStr, updatedAtStr string
		executionWatermark         int64
	)
	if err := row.Scan(&id, &key, &model, &revision, &summary, &summaryAtStr, &scopeJSON, &metadataJSON, &pinnedFactsJSON, &skillJSON, &createdAtStr, &updatedAtStr, &executionWatermark); err != nil {
		return nil, err
	}

	var snap skill.SessionSkillSnapshot
	if strings.TrimSpace(skillJSON) != "" && strings.TrimSpace(skillJSON) != "{}" {
		if err := decodeJSONField(skillJSON, "sqlite sessions", id, "skill_snapshot", &snap); err != nil {
			return nil, err
		}
	}

	metadata, err := decodeJSONMapField(metadataJSON, "sqlite sessions", id, "metadata")
	if err != nil {
		return nil, err
	}
	var pinnedFacts []contextengine.PinnedFact
	if strings.TrimSpace(pinnedFactsJSON) != "" && strings.TrimSpace(pinnedFactsJSON) != "[]" {
		if err := decodeJSONField(pinnedFactsJSON, "sqlite sessions", id, "pinned_facts", &pinnedFacts); err != nil {
			return nil, err
		}
	}
	var scopeRef domainscope.Ref
	if strings.TrimSpace(scopeJSON) != "" && strings.TrimSpace(scopeJSON) != "{}" {
		if err := decodeJSONField(scopeJSON, "sqlite sessions", id, "scope", &scopeRef); err != nil {
			return nil, err
		}
	} else {
		scopeRef = agent.ScopeRefFromMetadata(metadata)
	}
	createdAt, err := parseTime(createdAtStr, "sqlite sessions", id, "created_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updatedAtStr, "sqlite sessions", id, "updated_at")
	if err != nil {
		return nil, err
	}
	summaryAt, err := parseTime(summaryAtStr, "sqlite sessions", id, "summary_at")
	if err != nil {
		return nil, err
	}

	return &agent.Session{
		ID:        id,
		Key:       key,
		Model:     model,
		Revision:  revision,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Scope:     scopeRef,
		Metadata:  metadata,
		Session: contextengine.Session{
			ID:                 id,
			Summary:            summary,
			SummaryAt:          summaryAt,
			PinnedFacts:        pinnedFacts,
			ExecutionWatermark: executionWatermark,
			SkillSnapshot:      snap,
		},
	}, nil
}

func scanSessionWithCount(row sessionScanner) (*agent.Session, error) {
	var (
		id, key, model             string
		revision                   int64
		summary, summaryAtStr      string
		scopeJSON                  string
		metadataJSON               string
		pinnedFactsJSON, skillJSON string
		createdAtStr, updatedAtStr string
		executionWatermark         int64
		messageCount               int
	)
	if err := row.Scan(&id, &key, &model, &revision, &summary, &summaryAtStr, &scopeJSON, &metadataJSON, &pinnedFactsJSON, &skillJSON, &createdAtStr, &updatedAtStr, &executionWatermark, &messageCount); err != nil {
		return nil, err
	}
	sess, err := scanSession(scannedSessionRow{
		id:                 id,
		key:                key,
		model:              model,
		revision:           revision,
		summary:            summary,
		summaryAtStr:       summaryAtStr,
		scopeJSON:          scopeJSON,
		metadataJSON:       metadataJSON,
		pinnedFactsJSON:    pinnedFactsJSON,
		skillJSON:          skillJSON,
		createdAtStr:       createdAtStr,
		updatedAtStr:       updatedAtStr,
		executionWatermark: executionWatermark,
	})
	if err != nil {
		return nil, err
	}
	sess.MessageCount = messageCount
	return sess, nil
}

type scannedSessionRow struct {
	id, key, model             string
	revision                   int64
	summary, summaryAtStr      string
	scopeJSON                  string
	metadataJSON               string
	pinnedFactsJSON, skillJSON string
	createdAtStr, updatedAtStr string
	executionWatermark         int64
}

func (r scannedSessionRow) Scan(dest ...any) error {
	if len(dest) != 13 {
		return fmt.Errorf("scan session row: unexpected destination count %d", len(dest))
	}
	values := []any{
		r.id,
		r.key,
		r.model,
		r.revision,
		r.summary,
		r.summaryAtStr,
		r.scopeJSON,
		r.metadataJSON,
		r.pinnedFactsJSON,
		r.skillJSON,
		r.createdAtStr,
		r.updatedAtStr,
		r.executionWatermark,
	}
	for i, value := range values {
		switch dst := dest[i].(type) {
		case *string:
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("scan session row: destination %d expects string", i)
			}
			*dst = text
		case *int64:
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("scan session row: destination %d expects int64", i)
			}
			*dst = number
		default:
			return fmt.Errorf("scan session row: unsupported destination %T", dest[i])
		}
	}
	return nil
}
