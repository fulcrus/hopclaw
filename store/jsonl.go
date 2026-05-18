package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/support/ints"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

const (
	jsonlSessionLockRetryInterval = 5 * time.Millisecond
	jsonlCompactThresholdBytes    = 512 * 1024
)

type jsonlAppendConfig struct {
	CompactThresholdBytes int64
}

var defaultJSONLAppendConfig = jsonlAppendConfig{
	CompactThresholdBytes: jsonlCompactThresholdBytes,
}

type JSONLLayout struct {
	Root string
}

type JSONLStoreOptions struct {
	StartupLimit int
}

func (l JSONLLayout) SessionsDir() string {
	return filepath.Join(l.Root, "sessions")
}

func (l JSONLLayout) SessionPath(id string) string {
	return filepath.Join(l.SessionsDir(), id+".jsonl")
}

func (l JSONLLayout) RunsDir() string {
	return filepath.Join(l.Root, "runs")
}

func (l JSONLLayout) RunPath(id string) string {
	return filepath.Join(l.RunsDir(), id+".jsonl")
}

func (l JSONLLayout) ApprovalsDir() string {
	return filepath.Join(l.Root, "approvals")
}

func (l JSONLLayout) ApprovalPath(id string) string {
	return filepath.Join(l.ApprovalsDir(), id+".jsonl")
}

type JSONLSessionStore struct {
	layout JSONLLayout
	opts   JSONLStoreOptions

	mu     sync.RWMutex
	nextID atomic.Uint64
	byID   map[string]*agent.Session
	byKey  map[string]string
	locks  map[string]*sync.Mutex
}

var (
	_ agent.SessionQueryStore       = (*JSONLSessionStore)(nil)
	_ agent.SessionMaintenanceStore = (*JSONLSessionStore)(nil)
)

func NewJSONLSessionStore(root string) (*JSONLSessionStore, error) {
	return NewJSONLSessionStoreWithOptions(root, JSONLStoreOptions{})
}

func NewJSONLSessionStoreWithOptions(root string, opts JSONLStoreOptions) (*JSONLSessionStore, error) {
	store := &JSONLSessionStore{
		layout: JSONLLayout{Root: root},
		opts:   opts,
		byID:   make(map[string]*agent.Session),
		byKey:  make(map[string]string),
		locks:  make(map[string]*sync.Mutex),
	}
	if err := ensureDir(store.layout.SessionsDir()); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONLSessionStore) GetOrCreate(_ context.Context, sessionKey string, defaultModel string, sessionID ...string) (*agent.Session, error) {
	s.mu.Lock()
	if id, ok := s.byKey[sessionKey]; ok {
		session, err := cloneAgentSession(s.byID[id])
		s.mu.Unlock()
		return session, err
	}

	id := ""
	if len(sessionID) > 0 && sessionID[0] != "" {
		id = sessionID[0]
	}
	if id == "" {
		id = fmt.Sprintf("sess-%06d", s.nextID.Add(1))
	}
	now := time.Now().UTC()
	session := &agent.Session{
		ID:        id,
		Key:       sessionKey,
		Model:     defaultModel,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
		Session: contextengine.Session{
			ID: id,
		},
	}
	s.byID[id] = session
	s.byKey[sessionKey] = id
	s.locks[id] = &sync.Mutex{}
	s.mu.Unlock()

	if err := appendJSONL(s.layout.SessionPath(id), session); err != nil {
		return nil, err
	}
	return cloneAgentSession(session)
}

func (s *JSONLSessionStore) AppendUserMessage(_ context.Context, sessionID string, msg agent.IncomingMessage) error {
	lock, session, err := s.lookup(sessionID)
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   msg.Content,
		CreatedAt: now,
		Metadata:  supportmaps.Clone(msg.Metadata),
	})
	session.MessageCount = len(session.Messages)
	session.Metadata = agent.MergeSessionMetadata(session.Metadata, msg)
	session.Scope = agent.MergeScopeRef(session.Scope, msg)
	if msg.Model != "" {
		session.Model = msg.Model
	}
	session.Revision++
	session.UpdatedAt = now
	return appendJSONL(s.layout.SessionPath(session.ID), session)
}

func (s *JSONLSessionStore) LoadForExecution(ctx context.Context, sessionID string) (*agent.Session, func(), error) {
	lock, session, err := s.lookup(sessionID)
	if err != nil {
		return nil, nil, err
	}
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
		time.Sleep(jsonlSessionLockRetryInterval)
	}
	return session, lock.Unlock, nil
}

func (s *JSONLSessionStore) Save(_ context.Context, session *agent.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	_, current, err := s.lookup(session.ID)
	if err != nil {
		return err
	}

	cloned, err := cloneAgentSession(session)
	if err != nil {
		return err
	}
	cloned.Revision = current.Revision + 1
	cloned.UpdatedAt = time.Now().UTC()
	cloned.MessageCount = cloned.TotalMessageCount()
	s.mu.Lock()
	*current = *cloned
	s.mu.Unlock()
	session.Revision = cloned.Revision
	session.UpdatedAt = cloned.UpdatedAt
	session.MessageCount = cloned.MessageCount
	return appendJSONL(s.layout.SessionPath(session.ID), current)
}

func (s *JSONLSessionStore) List(ctx context.Context) ([]*agent.Session, error) {
	return s.ListScoped(ctx, agent.SessionListFilter{})
}

func (s *JSONLSessionStore) ListScoped(ctx context.Context, filter agent.SessionListFilter) ([]*agent.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*agent.Session, 0, len(s.byID))
	for _, sess := range s.byID {
		if !filter.Scope.Matches(sess.Scope) {
			continue
		}
		out = append(out, cloneAgentSessionMetadata(sess))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *JSONLSessionStore) Get(ctx context.Context, sessionID string) (*agent.Session, error) {
	return s.GetScoped(ctx, sessionID, agent.ScopeFilter{})
}

func (s *JSONLSessionStore) GetMetadata(ctx context.Context, sessionID string) (*agent.Session, error) {
	return s.GetMetadataScoped(ctx, sessionID, agent.ScopeFilter{})
}

func (s *JSONLSessionStore) GetScoped(ctx context.Context, sessionID string, scope agent.ScopeFilter) (*agent.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return cloneAgentSession(sess)
}

func (s *JSONLSessionStore) GetMetadataScoped(ctx context.Context, sessionID string, scope agent.ScopeFilter) (*agent.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return cloneAgentSessionMetadata(sess), nil
}

func (s *JSONLSessionStore) GetByKey(ctx context.Context, sessionKey string) (*agent.Session, error) {
	return s.GetByKeyScoped(ctx, sessionKey, agent.ScopeFilter{})
}

func (s *JSONLSessionStore) GetByKeyMetadata(ctx context.Context, sessionKey string) (*agent.Session, error) {
	return s.GetByKeyMetadataScoped(ctx, sessionKey, agent.ScopeFilter{})
}

func (s *JSONLSessionStore) GetByKeyScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionID, ok := s.byKey[sessionKey]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return cloneAgentSession(sess)
}

func (s *JSONLSessionStore) GetByKeyMetadataScoped(ctx context.Context, sessionKey string, scope agent.ScopeFilter) (*agent.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionID, ok := s.byKey[sessionKey]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return cloneAgentSessionMetadata(sess), nil
}

func (s *JSONLSessionStore) RecentMessages(ctx context.Context, sessionID string, limit int) ([]contextengine.Message, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	start := len(sess.Messages) - limit
	if start < 0 {
		start = 0
	}
	return cloneMessagesWindow(sess.Messages[start:]), nil
}

// DeleteSession removes a session from the in-memory index and deletes its JSONL file.
func (s *JSONLSessionStore) DeleteSession(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.byID[sessionID]
	if !ok {
		return fmt.Errorf("session %s: not found", sessionID)
	}

	// Remove from indexes.
	if sess.Key != "" {
		delete(s.byKey, sess.Key)
	}
	delete(s.byID, sessionID)

	// Remove file.
	fp := s.layout.SessionPath(sessionID)
	if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove session file: %w", err)
	}
	return nil
}

func (s *JSONLSessionStore) load() error {
	entries, err := os.ReadDir(s.layout.SessionsDir())
	if err != nil {
		return err
	}
	type loadedSession struct {
		session *agent.Session
		path    string
	}
	loaded := make([]loadedSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		var session agent.Session
		path := filepath.Join(s.layout.SessionsDir(), entry.Name())
		if err := loadLatestJSONL(path, &session); err != nil {
			return err
		}
		loaded = append(loaded, loadedSession{session: &session, path: path})
	}
	if limit := s.opts.StartupLimit; limit > 0 && len(loaded) > limit {
		sort.Slice(loaded, func(i, j int) bool {
			return loaded[i].session.UpdatedAt.After(loaded[j].session.UpdatedAt)
		})
		for _, item := range loaded[limit:] {
			if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("truncate session jsonl %s: %w", item.path, err)
			}
		}
		loaded = loaded[:limit]
	}
	var maxID uint64
	for _, item := range loaded {
		session := item.session
		session.MessageCount = session.TotalMessageCount()
		s.byID[session.ID] = session
		s.byKey[session.Key] = session.ID
		s.locks[session.ID] = &sync.Mutex{}
		maxID = ints.Max64(maxID, parseSequence(session.ID, "sess-"))
	}
	s.nextID.Store(maxID)
	return nil
}

func (s *JSONLSessionStore) lookup(sessionID string) (*sync.Mutex, *agent.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lock, ok := s.locks[sessionID]
	if !ok {
		return nil, nil, fmt.Errorf("session %s not found", sessionID)
	}
	session, ok := s.byID[sessionID]
	if !ok {
		return nil, nil, fmt.Errorf("session %s not found", sessionID)
	}
	return lock, session, nil
}

func (s *JSONLSessionStore) PruneSessions(ctx context.Context, before time.Time) (int, error) {
	return s.PruneSessionsExcept(ctx, before, nil)
}

func (s *JSONLSessionStore) PruneSessionsExcept(ctx context.Context, before time.Time, excludeSessionIDs []string) (int, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
	if before.IsZero() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	excluded := make(map[string]struct{}, len(excludeSessionIDs))
	for _, id := range excludeSessionIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			excluded[trimmed] = struct{}{}
		}
	}
	deleted := 0
	for id, sess := range s.byID {
		if _, keep := excluded[id]; keep {
			continue
		}
		if sess == nil || sess.UpdatedAt.IsZero() || !sess.UpdatedAt.Before(before) {
			continue
		}
		delete(s.byID, id)
		delete(s.byKey, sess.Key)
		delete(s.locks, id)
		if err := os.Remove(s.layout.SessionPath(id)); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("remove session jsonl %s: %w", id, err)
		}
		deleted++
	}
	return deleted, nil
}

type JSONLRunStore struct {
	layout JSONLLayout
	opts   JSONLStoreOptions

	mu        sync.RWMutex
	nextID    atomic.Uint64
	byID      map[string]*agent.Run
	byEventID map[string]string
}

func NewJSONLRunStore(root string) (*JSONLRunStore, error) {
	return NewJSONLRunStoreWithOptions(root, JSONLStoreOptions{})
}

func NewJSONLRunStoreWithOptions(root string, opts JSONLStoreOptions) (*JSONLRunStore, error) {
	store := &JSONLRunStore{
		layout:    JSONLLayout{Root: root},
		opts:      opts,
		byID:      make(map[string]*agent.Run),
		byEventID: make(map[string]string),
	}
	if err := ensureDir(store.layout.RunsDir()); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONLRunStore) Seen(_ context.Context, externalEventID string, within time.Duration) bool {
	if externalEventID == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	runID, ok := s.byEventID[externalEventID]
	if !ok {
		return false
	}
	run := s.byID[runID]
	if run == nil {
		return false
	}
	if within <= 0 {
		return true
	}
	return time.Since(run.UpdatedAt) <= within
}

func (s *JSONLRunStore) FindByExternalEvent(_ context.Context, externalEventID string) (*agent.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runID, ok := s.byEventID[externalEventID]
	if !ok {
		return nil, fmt.Errorf("run for event %q not found", externalEventID)
	}
	return cloneAgentRun(s.byID[runID])
}

func (s *JSONLRunStore) Get(ctx context.Context, runID string) (*agent.Run, error) {
	return s.GetScoped(ctx, runID, agent.ScopeFilter{})
}

func (s *JSONLRunStore) GetScoped(ctx context.Context, runID string, scope agent.ScopeFilter) (*agent.Run, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.byID[runID]
	if !ok {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	if !scope.Matches(run.Scope) {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	return cloneAgentRun(run)
}

func (s *JSONLRunStore) Create(_ context.Context, sessionID string, msg agent.IncomingMessage, cfg agent.AgentConfig) (*agent.Run, error) {
	now := time.Now().UTC()
	run := &agent.Run{
		ID:                  fmt.Sprintf("run-%06d", s.nextID.Add(1)),
		SessionID:           sessionID,
		Scope:               agent.ScopeRefFromIncomingMessage(msg),
		InputEventID:        msg.ExternalEventID,
		Status:              agent.RunQueued,
		QueueMode:           cfg.QueueMode,
		Phase:               agent.PhasePreparing,
		ExecutionMode:       agent.ExecutionModeDirect,
		Model:               defaultString(msg.Model, cfg.DefaultModel),
		LastSessionRevision: 0,
		UpdatedAt:           now,
	}

	if err := appendJSONL(s.layout.RunPath(run.ID), run); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.byID[run.ID] = run
	if msg.ExternalEventID != "" {
		s.byEventID[msg.ExternalEventID] = run.ID
	}
	s.mu.Unlock()

	return cloneAgentRun(run)
}

func (s *JSONLRunStore) List(_ context.Context, filter agent.RunListFilter) ([]*agent.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*agent.Run
	for _, run := range s.byID {
		if filter.SessionID != "" && run.SessionID != filter.SessionID {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		if !filter.Scope.Matches(run.Scope) {
			continue
		}
		c, err := cloneAgentRun(run)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *JSONLRunStore) Update(_ context.Context, run *agent.Run) error {
	if run == nil {
		return fmt.Errorf("run is required")
	}

	cloned, err := cloneAgentRun(run)
	if err != nil {
		return err
	}
	cloned.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	if _, ok := s.byID[run.ID]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("run %s not found", run.ID)
	}
	s.byID[run.ID] = cloned
	if cloned.InputEventID != "" {
		s.byEventID[cloned.InputEventID] = cloned.ID
	}
	s.mu.Unlock()

	*run = *cloned
	return appendJSONL(s.layout.RunPath(run.ID), cloned)
}

func (s *JSONLRunStore) ClaimQueuedRun(_ context.Context, runID string) (*agent.Run, bool, error) {
	s.mu.Lock()
	current, ok := s.byID[runID]
	if !ok {
		s.mu.Unlock()
		return nil, false, fmt.Errorf("run %s not found", runID)
	}
	if current.Status != agent.RunQueued {
		cloned, err := cloneAgentRun(current)
		s.mu.Unlock()
		return cloned, false, err
	}
	cloned, err := cloneAgentRun(current)
	if err != nil {
		s.mu.Unlock()
		return nil, false, err
	}
	cloned.Status = agent.RunRunning
	if cloned.StartedAt.IsZero() {
		cloned.StartedAt = time.Now().UTC()
	}
	cloned.UpdatedAt = time.Now().UTC()
	s.byID[runID] = cloned
	s.mu.Unlock()

	if err := appendJSONL(s.layout.RunPath(runID), cloned); err != nil {
		return nil, false, err
	}
	out, err := cloneAgentRun(cloned)
	return out, true, err
}

func (s *JSONLRunStore) load() error {
	entries, err := os.ReadDir(s.layout.RunsDir())
	if err != nil {
		return err
	}
	type loadedRun struct {
		run  *agent.Run
		path string
	}
	loaded := make([]loadedRun, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		var run agent.Run
		path := filepath.Join(s.layout.RunsDir(), entry.Name())
		if err := loadLatestJSONL(path, &run); err != nil {
			return err
		}
		loaded = append(loaded, loadedRun{run: &run, path: path})
	}
	if limit := s.opts.StartupLimit; limit > 0 && len(loaded) > limit {
		sort.Slice(loaded, func(i, j int) bool {
			return loaded[i].run.UpdatedAt.After(loaded[j].run.UpdatedAt)
		})
		for _, item := range loaded[limit:] {
			if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("truncate run jsonl %s: %w", item.path, err)
			}
		}
		loaded = loaded[:limit]
	}
	var maxID uint64
	for _, item := range loaded {
		run := item.run
		s.byID[run.ID] = run
		if run.InputEventID != "" {
			s.byEventID[run.InputEventID] = run.ID
		}
		maxID = ints.Max64(maxID, parseSequence(run.ID, "run-"))
	}
	s.nextID.Store(maxID)
	return nil
}

func (s *JSONLRunStore) PruneRuns(_ context.Context, before time.Time) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for id, run := range s.byID {
		if run == nil || !jsonlTerminalRunStatus(run.Status) || run.FinishedAt.IsZero() || !run.FinishedAt.Before(before) {
			continue
		}
		delete(s.byID, id)
		if strings.TrimSpace(run.InputEventID) != "" {
			delete(s.byEventID, run.InputEventID)
		}
		if err := os.Remove(s.layout.RunPath(id)); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("remove run jsonl %s: %w", id, err)
		}
		deleted++
	}
	return deleted, nil
}

func jsonlTerminalRunStatus(status agent.RunStatus) bool {
	switch status {
	case agent.RunCompleted, agent.RunFailed, agent.RunCancelled:
		return true
	default:
		return false
	}
}

type JSONLApprovalStore struct {
	layout JSONLLayout

	mu      sync.RWMutex
	nextID  atomic.Uint64
	byID    map[string]*approval.Ticket
	byRunID map[string]string
	byExtID map[string]string
}

func NewJSONLApprovalStore(root string) (*JSONLApprovalStore, error) {
	store := &JSONLApprovalStore{
		layout:  JSONLLayout{Root: root},
		byID:    make(map[string]*approval.Ticket),
		byRunID: make(map[string]string),
		byExtID: make(map[string]string),
	}
	if err := ensureDir(store.layout.ApprovalsDir()); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONLApprovalStore) Create(_ context.Context, ticket approval.Ticket) (*approval.Ticket, error) {
	s.mu.Lock()
	if ticket.RunID == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("run id is required")
	}
	if existingID, ok := s.byRunID[ticket.RunID]; ok {
		if existing, exists := s.byID[existingID]; exists && existing.Status == approval.StatusPending {
			s.mu.Unlock()
			cloned, err := cloneTicket(existing)
			if err != nil {
				return nil, err
			}
			return cloned, nil
		}
	}
	ticket.ID = fmt.Sprintf("appr-%06d", s.nextID.Add(1))
	ticket.Status = approval.StatusPending
	ticket.CreatedAt = time.Now().UTC()
	cloned, err := cloneTicket(&ticket)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.byID[cloned.ID] = cloned
	s.byRunID[cloned.RunID] = cloned.ID
	for _, ref := range cloned.External {
		if key := approval.ExternalReferenceLookupKey(ref.Provider, ref.ExternalID); key != "" {
			s.byExtID[key] = cloned.ID
		}
	}
	s.mu.Unlock()

	if err := appendJSONL(s.layout.ApprovalPath(cloned.ID), cloned); err != nil {
		return nil, err
	}
	return cloneTicket(cloned)
}

func (s *JSONLApprovalStore) Get(_ context.Context, ticketID string) (*approval.Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ticket, ok := s.byID[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}
	return cloneTicket(ticket)
}

func (s *JSONLApprovalStore) GetByRun(_ context.Context, runID string) (*approval.Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ticketID, ok := s.byRunID[runID]
	if !ok {
		return nil, fmt.Errorf("ticket for run %s not found", runID)
	}
	return cloneTicket(s.byID[ticketID])
}

func (s *JSONLApprovalStore) GetByExternal(_ context.Context, provider, externalID string) (*approval.Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ticketID, ok := s.byExtID[approval.ExternalReferenceLookupKey(provider, externalID)]
	if !ok {
		return nil, fmt.Errorf("ticket for external approval %s/%s: %w", strings.TrimSpace(provider), strings.TrimSpace(externalID), approval.ErrNotFound)
	}
	return cloneTicket(s.byID[ticketID])
}

func (s *JSONLApprovalStore) List(_ context.Context, filter approval.ListFilter) ([]*approval.Ticket, error) {
	filter = filter.Normalize()
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*approval.Ticket, 0, len(s.byID))
	for _, ticket := range s.byID {
		if filter.Status != "" && ticket.Status != filter.Status {
			continue
		}
		cloned, err := cloneTicket(ticket)
		if err != nil {
			return nil, err
		}
		items = append(items, cloned)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if filter.Offset > 0 {
		if filter.Offset >= len(items) {
			return nil, nil
		}
		items = items[filter.Offset:]
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *JSONLApprovalStore) Resolve(_ context.Context, ticketID string, resolution approval.Resolution) (*approval.Ticket, error) {
	s.mu.Lock()
	ticket, ok := s.byID[ticketID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}
	if ticket.Status != approval.StatusPending {
		s.mu.Unlock()
		return nil, fmt.Errorf("ticket %s already resolved", ticketID)
	}
	switch resolution.Status {
	case approval.StatusApproved, approval.StatusDenied, approval.StatusCancelled:
	default:
		s.mu.Unlock()
		return nil, fmt.Errorf("invalid approval resolution %q", resolution.Status)
	}
	ticket.Status = resolution.Status
	ticket.ResolvedAt = time.Now().UTC()
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	ticket.Scope = resolution.Scope
	cloned, err := cloneTicket(ticket)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := appendJSONL(s.layout.ApprovalPath(ticketID), cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func (s *JSONLApprovalStore) UpsertExternalRef(_ context.Context, ticketID string, ref approval.ExternalReference) (*approval.Ticket, error) {
	s.mu.Lock()
	ticket, ok := s.byID[ticketID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("ticket %s: %w", ticketID, approval.ErrNotFound)
	}
	previous := externalReferenceByProvider(ticket.External, ref.Provider)
	nextRefs, merged, err := approval.UpsertExternalReferences(ticket.External, ref)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if previous != nil && strings.TrimSpace(previous.ExternalID) != "" {
		delete(s.byExtID, approval.ExternalReferenceLookupKey(previous.Provider, previous.ExternalID))
	}
	if strings.TrimSpace(merged.ExternalID) != "" {
		s.byExtID[approval.ExternalReferenceLookupKey(merged.Provider, merged.ExternalID)] = ticketID
	}
	ticket.External = nextRefs
	cloned, err := cloneTicket(ticket)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := appendJSONL(s.layout.ApprovalPath(ticketID), cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func (s *JSONLApprovalStore) load() error {
	entries, err := os.ReadDir(s.layout.ApprovalsDir())
	if err != nil {
		return err
	}
	var maxID uint64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		var ticket approval.Ticket
		if err := loadLatestJSONL(filepath.Join(s.layout.ApprovalsDir(), entry.Name()), &ticket); err != nil {
			return err
		}
		s.byID[ticket.ID] = &ticket
		s.byRunID[ticket.RunID] = ticket.ID
		for _, ref := range ticket.External {
			if key := approval.ExternalReferenceLookupKey(ref.Provider, ref.ExternalID); key != "" {
				s.byExtID[key] = ticket.ID
			}
		}
		maxID = ints.Max64(maxID, parseSequence(ticket.ID, "appr-"))
	}
	s.nextID.Store(maxID)
	return nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func appendJSONL(path string, value any) error {
	return appendJSONLWithConfig(path, value, defaultJSONLAppendConfig)
}

func appendJSONLWithConfig(path string, value any, cfg jsonlAppendConfig) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if cfg.CompactThresholdBytes <= 0 {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < cfg.CompactThresholdBytes {
		return nil
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return rewriteJSONLSnapshot(path, data)
}

func rewriteJSONLSnapshot(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func loadLatestJSONL(path string, target any) error {
	line, err := readLastJSONLine(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(line, target)
}

func readLastJSONLine(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var last []byte
	for {
		line, err := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			last = append(last[:0], line...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	if len(last) == 0 {
		return nil, fmt.Errorf("jsonl file %s is empty", path)
	}
	return last, nil
}

func parseSequence(id, prefix string) uint64 {
	if !strings.HasPrefix(id, prefix) {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(id, prefix), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func cloneAgentSession(in *agent.Session) (*agent.Session, error) {
	return cloneValue[agent.Session](in)
}

func cloneAgentSessionMetadata(in *agent.Session) *agent.Session {
	if in == nil {
		return nil
	}
	out := *in
	out.MessageCount = in.TotalMessageCount()
	out.Messages = nil
	out.Metadata = supportmaps.Clone(in.Metadata)
	if snapshot, err := cloneValue(&in.SkillSnapshot); err == nil && snapshot != nil {
		out.SkillSnapshot = *snapshot
	}
	return &out
}

func cloneAgentRun(in *agent.Run) (*agent.Run, error) {
	return cloneValue[agent.Run](in)
}

func cloneTicket(in *approval.Ticket) (*approval.Ticket, error) {
	return cloneValue[approval.Ticket](in)
}

func cloneValue[T any](in *T) (*T, error) {
	if in == nil {
		return nil, nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func cloneMessagesWindow(in []contextengine.Message) []contextengine.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]contextengine.Message, len(in))
	for i, msg := range in {
		out[i] = msg
		out[i].Metadata = supportmaps.Clone(msg.Metadata)
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = append([]contextengine.ToolCallRef(nil), msg.ToolCalls...)
		}
		if len(msg.ContentBlocks) > 0 {
			out[i].ContentBlocks = append([]contextengine.ContentBlock(nil), msg.ContentBlocks...)
		}
	}
	return out
}

func externalReferenceByProvider(refs []approval.ExternalReference, provider string) *approval.ExternalReference {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	for _, ref := range refs {
		if strings.ToLower(strings.TrimSpace(ref.Provider)) != provider {
			continue
		}
		cloned := ref
		if len(ref.Metadata) > 0 {
			cloned.Metadata = supportmaps.Clone(ref.Metadata)
		}
		return &cloned
	}
	return nil
}

func defaultString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
