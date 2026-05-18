package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/skill"
)

const sessionLockRetryInterval = 5 * time.Millisecond

// MediaStore stores user-provided media blobs and returns an addressable URI.
type MediaStore interface {
	Put(ctx context.Context, kind, contentType string, body []byte) (uri string, err error)
}

type InMemorySessionStore struct {
	mu            sync.RWMutex
	nextID        atomic.Uint64
	nextEpisodeID atomic.Uint64
	byID          map[string]*Session
	byKey         map[string]string
	locks         map[string]*sync.Mutex
	episodes      map[string][]contextengine.EpisodeSummary
	activeEpisode map[string]string
	mediaStore    MediaStore
}

var (
	_ SessionQueryStore           = (*InMemorySessionStore)(nil)
	_ SessionMaintenanceStore     = (*InMemorySessionStore)(nil)
	_ SessionEpisodeManager       = (*InMemorySessionStore)(nil)
	_ contextengine.EpisodeWriter = (*InMemorySessionStore)(nil)
	_ contextengine.EpisodeReader = (*InMemorySessionStore)(nil)
)

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		byID:          make(map[string]*Session),
		byKey:         make(map[string]string),
		locks:         make(map[string]*sync.Mutex),
		episodes:      make(map[string][]contextengine.EpisodeSummary),
		activeEpisode: make(map[string]string),
	}
}

// SetMediaStore installs the optional media store used to externalize image payloads.
func (s *InMemorySessionStore) SetMediaStore(media MediaStore) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mediaStore = media
}

func (s *InMemorySessionStore) GetOrCreate(_ context.Context, sessionKey string, defaultModel string, sessionID ...string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byKey[sessionKey]; ok {
		return cloneSession(s.byID[id]), nil
	}

	id := normalize.FirstNonEmpty(sessionID...)
	if id == "" {
		id = fmt.Sprintf("sess-%06d", s.nextID.Add(1))
	}
	now := time.Now().UTC()
	session := &Session{
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
	return cloneSession(session), nil
}

func (s *InMemorySessionStore) AppendUserMessage(_ context.Context, sessionID string, msg IncomingMessage) error {
	lock, session, err := s.lookup(sessionID)
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	userMsg := contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   msg.Content,
		CreatedAt: now,
		Metadata:  cloneMap(msg.Metadata),
	}
	if blocks := incomingContentBlocks(msg, s.mediaStore); len(blocks) > 0 {
		userMsg.ContentBlocks = blocks
	}
	session.Messages = append(session.Messages, userMsg)
	session.MessageCount = len(session.Messages)
	session.Metadata = MergeSessionMetadata(session.Metadata, msg)
	session.Scope = MergeScopeRef(session.Scope, msg)
	if msg.Model != "" {
		session.Model = msg.Model
	}
	session.Revision++
	session.UpdatedAt = now
	return nil
}

func incomingContentBlocks(msg IncomingMessage, media MediaStore) []contextengine.ContentBlock {
	blocks := cloneContentBlocks(msg.ContentBlocks)
	if len(msg.Images) > 0 {
		text := ""
		if len(blocks) == 0 {
			text = msg.Content
		}
		blocks = append(blocks, buildImageContentBlocks(text, msg.Images, media)...)
	}
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func buildImageContentBlocks(text string, images []string, media MediaStore) []contextengine.ContentBlock {
	blocks := make([]contextengine.ContentBlock, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, contextengine.ContentBlock{
			Type: contextengine.ContentBlockText,
			Text: text,
		})
	}
	for _, img := range images {
		mediaType, data := parseImageData(img)
		if data == "" {
			continue
		}
		if media != nil {
			raw, err := base64.StdEncoding.DecodeString(data)
			if err == nil {
				uri, err := media.Put(context.Background(), "user_image", mediaType, raw)
				if err == nil && strings.TrimSpace(uri) != "" {
					blocks = append(blocks, contextengine.ContentBlock{
						Type:      contextengine.ContentBlockImage,
						MediaType: mediaType,
						MediaRef:  uri,
					})
					continue
				}
			}
		}
		blocks = append(blocks, contextengine.ContentBlock{
			Type:      contextengine.ContentBlockImage,
			MediaType: mediaType,
			Data:      data,
		})
	}
	return blocks
}

// parseImageData extracts media type and base64 data from a data URI or raw base64.
func parseImageData(input string) (mediaType, data string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if strings.HasPrefix(input, "data:") {
		parts := strings.SplitN(input, ",", 2)
		if len(parts) != 2 {
			return "", ""
		}
		header := strings.TrimPrefix(parts[0], "data:")
		mediaType = strings.TrimSuffix(header, ";base64")
		if strings.TrimSpace(mediaType) == "" {
			mediaType = "image/jpeg"
		}
		return mediaType, parts[1]
	}
	return "image/jpeg", input
}

func (s *InMemorySessionStore) LoadForExecution(ctx context.Context, sessionID string) (*Session, func(), error) {
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
		time.Sleep(sessionLockRetryInterval)
	}
	// Return a clone so callers never hold a raw pointer into byID.
	// Modifications accumulate on the clone; Save() writes them back
	// atomically. This eliminates the data race where a concurrent
	// reader (Get/List) could observe a half-modified session.
	return cloneSession(session), lock.Unlock, nil
}

func (s *InMemorySessionStore) Save(_ context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}

	// The caller is expected to hold the per-session lock (from
	// LoadForExecution). We only need the global map lock to protect
	// the byID map structure during replacement.
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.byID[session.ID]
	if !ok {
		return fmt.Errorf("session %s not found", session.ID)
	}
	cloned := cloneSession(session)
	cloned.Revision = current.Revision + 1
	*current = *cloned
	session.Revision = cloned.Revision
	session.MessageCount = cloned.MessageCount
	return nil
}

func (s *InMemorySessionStore) List(_ context.Context) ([]*Session, error) {
	return s.ListScoped(context.Background(), SessionListFilter{})
}

func (s *InMemorySessionStore) ListScoped(_ context.Context, filter SessionListFilter) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Session, 0, len(s.byID))
	for _, sess := range s.byID {
		if !filter.Scope.Matches(sess.Scope) {
			continue
		}
		result = append(result, cloneSessionMetadata(sess))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (s *InMemorySessionStore) Get(_ context.Context, sessionID string) (*Session, error) {
	return s.GetScoped(context.Background(), sessionID, ScopeFilter{})
}

func (s *InMemorySessionStore) GetMetadata(_ context.Context, sessionID string) (*Session, error) {
	return s.GetMetadataScoped(context.Background(), sessionID, ScopeFilter{})
}

func (s *InMemorySessionStore) GetScoped(_ context.Context, sessionID string, scope ScopeFilter) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return cloneSession(sess), nil
}

func (s *InMemorySessionStore) GetMetadataScoped(_ context.Context, sessionID string, scope ScopeFilter) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return cloneSessionMetadata(sess), nil
}

func (s *InMemorySessionStore) GetByKey(_ context.Context, sessionKey string) (*Session, error) {
	return s.GetByKeyScoped(context.Background(), sessionKey, ScopeFilter{})
}

func (s *InMemorySessionStore) GetByKeyMetadata(_ context.Context, sessionKey string) (*Session, error) {
	return s.GetByKeyMetadataScoped(context.Background(), sessionKey, ScopeFilter{})
}

func (s *InMemorySessionStore) GetByKeyScoped(_ context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byKey[sessionKey]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	sess, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return cloneSession(sess), nil
}

func (s *InMemorySessionStore) GetByKeyMetadataScoped(_ context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byKey[sessionKey]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	sess, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	if !scope.Matches(sess.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return cloneSessionMetadata(sess), nil
}

func (s *InMemorySessionStore) RecentMessages(_ context.Context, sessionID string, limit int) ([]contextengine.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if limit <= 0 {
		return nil, nil
	}
	start := len(sess.Messages) - limit
	if start < 0 {
		start = 0
	}
	return cloneMessages(sess.Messages[start:]), nil
}

func (s *InMemorySessionStore) DeleteSession(_ context.Context, sessionID string) error {
	lock, _, err := s.lookup(sessionID)
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byID[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	delete(s.byID, sessionID)
	delete(s.locks, sessionID)
	delete(s.episodes, sessionID)
	delete(s.activeEpisode, sessionID)
	if session.Key != "" {
		delete(s.byKey, session.Key)
	}
	return nil
}

func (s *InMemorySessionStore) EnsureActiveEpisode(_ context.Context, sessionID string, reason string) (string, error) {
	lock, _, err := s.lookup(sessionID)
	if err != nil {
		return "", err
	}
	lock.Lock()
	defer lock.Unlock()

	s.mu.RLock()
	episodeID := strings.TrimSpace(s.activeEpisode[sessionID])
	s.mu.RUnlock()
	if episodeID != "" {
		return episodeID, nil
	}
	return s.createEpisodeLocked(sessionID, reason)
}

func (s *InMemorySessionStore) StartNewEpisode(_ context.Context, sessionID string, reason string) (string, error) {
	lock, session, err := s.lookup(sessionID)
	if err != nil {
		return "", err
	}
	lock.Lock()
	defer lock.Unlock()

	s.mu.RLock()
	current := strings.TrimSpace(s.activeEpisode[sessionID])
	s.mu.RUnlock()
	if current != "" {
		s.mu.Lock()
		if err := s.sealEpisodeLocked(current, int64(len(session.Messages))); err != nil {
			s.mu.Unlock()
			return "", err
		}
		s.mu.Unlock()
	}
	return s.createEpisodeLocked(sessionID, reason)
}

func (s *InMemorySessionStore) CreateEpisode(_ context.Context, sessionID string, reason string) (string, error) {
	lock, _, err := s.lookup(sessionID)
	if err != nil {
		return "", err
	}
	lock.Lock()
	defer lock.Unlock()
	return s.createEpisodeLocked(sessionID, reason)
}

func (s *InMemorySessionStore) SealEpisode(_ context.Context, episodeID string, seqEnd int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sealEpisodeLocked(strings.TrimSpace(episodeID), seqEnd)
}

func (s *InMemorySessionStore) ActiveEpisode(_ context.Context, sessionID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.byID[sessionID]; !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	return strings.TrimSpace(s.activeEpisode[sessionID]), nil
}

func (s *InMemorySessionStore) ListEpisodes(_ context.Context, sessionID string) ([]contextengine.EpisodeSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.byID[sessionID]; !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return append([]contextengine.EpisodeSummary(nil), s.episodes[sessionID]...), nil
}

func (s *InMemorySessionStore) createEpisodeLocked(sessionID string, reason string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[sessionID]; !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	if current := strings.TrimSpace(s.activeEpisode[sessionID]); current != "" {
		return "", fmt.Errorf("session %s already has active episode %s", sessionID, current)
	}
	now := time.Now().UTC()
	episodeID := fmt.Sprintf("ep-%06d", s.nextEpisodeID.Add(1))
	s.episodes[sessionID] = append(s.episodes[sessionID], contextengine.EpisodeSummary{
		ID:        episodeID,
		SessionID: sessionID,
		SeqNum:    len(s.episodes[sessionID]) + 1,
		Status:    "active",
		StartedAt: now,
	})
	s.activeEpisode[sessionID] = episodeID
	_ = reason
	return episodeID, nil
}

func (s *InMemorySessionStore) sealEpisodeLocked(episodeID string, seqEnd int64) error {
	if episodeID == "" {
		return fmt.Errorf("episode id is required")
	}
	for sessionID, episodes := range s.episodes {
		for idx := range episodes {
			if episodes[idx].ID != episodeID {
				continue
			}
			episodes[idx].Status = "sealed"
			episodes[idx].SealedAt = time.Now().UTC()
			if seqEnd > 0 && episodes[idx].MessageCount == 0 {
				episodes[idx].MessageCount = int(seqEnd)
			}
			s.episodes[sessionID] = episodes
			if s.activeEpisode[sessionID] == episodeID {
				delete(s.activeEpisode, sessionID)
			}
			return nil
		}
	}
	return fmt.Errorf("episode %s not found", episodeID)
}

func (s *InMemorySessionStore) lookup(sessionID string) (*sync.Mutex, *Session, error) {
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

type InMemoryRunStore struct {
	mu        sync.RWMutex
	nextID    atomic.Uint64
	byID      map[string]*Run
	byEventID map[string]string
}

func NewInMemoryRunStore() *InMemoryRunStore {
	return &InMemoryRunStore{
		byID:      make(map[string]*Run),
		byEventID: make(map[string]string),
	}
}

func (s *InMemoryRunStore) Seen(_ context.Context, externalEventID string, within time.Duration) bool {
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

func (s *InMemoryRunStore) FindByExternalEvent(_ context.Context, externalEventID string) (*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runID, ok := s.byEventID[externalEventID]
	if !ok {
		return nil, fmt.Errorf("run for event %q not found", externalEventID)
	}
	run, ok := s.byID[runID]
	if !ok {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	return cloneRun(run), nil
}

func (s *InMemoryRunStore) Get(_ context.Context, runID string) (*Run, error) {
	return s.GetScoped(context.Background(), runID, ScopeFilter{})
}

func (s *InMemoryRunStore) GetScoped(_ context.Context, runID string, scope ScopeFilter) (*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.byID[runID]
	if !ok {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	if !scope.Matches(run.Scope) {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	return cloneRun(run), nil
}

func (s *InMemoryRunStore) Create(_ context.Context, sessionID string, msg IncomingMessage, cfg AgentConfig) (*Run, error) {
	now := time.Now().UTC()
	run := &Run{
		ID:                  fmt.Sprintf("run-%06d", s.nextID.Add(1)),
		SessionID:           sessionID,
		Scope:               ScopeRefFromIncomingMessage(msg),
		ParentRunID:         strings.TrimSpace(msg.ParentRunID),
		InputEventID:        msg.ExternalEventID,
		Status:              RunQueued,
		QueueMode:           cfg.QueueMode,
		Phase:               PhasePreparing,
		ExecutionMode:       ExecutionModeDirect,
		Model:               defaultString(msg.Model, cfg.DefaultModel),
		LastSessionRevision: 0,
		UpdatedAt:           now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[run.ID] = cloneRun(run)
	if msg.ExternalEventID != "" {
		s.byEventID[msg.ExternalEventID] = run.ID
	}
	return run, nil
}

func (s *InMemoryRunStore) List(_ context.Context, filter RunListFilter) ([]*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*Run
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
		out = append(out, cloneRun(run))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *InMemoryRunStore) Update(_ context.Context, run *Run) error {
	if run == nil {
		return fmt.Errorf("run is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[run.ID]; !ok {
		return fmt.Errorf("run %s not found", run.ID)
	}
	run.UpdatedAt = time.Now().UTC()
	s.byID[run.ID] = cloneRun(run)
	if run.InputEventID != "" {
		s.byEventID[run.InputEventID] = run.ID
	}
	return nil
}

func (s *InMemoryRunStore) ClaimQueuedRun(_ context.Context, runID string) (*Run, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.byID[runID]
	if !ok {
		return nil, false, fmt.Errorf("run %s not found", runID)
	}
	if current.Status != RunQueued {
		return cloneRun(current), false, nil
	}
	claimed := cloneRun(current)
	claimed.Status = RunRunning
	if claimed.StartedAt.IsZero() {
		claimed.StartedAt = time.Now().UTC()
	}
	claimed.UpdatedAt = time.Now().UTC()
	s.byID[runID] = cloneRun(claimed)
	return claimed, true, nil
}

func (s *InMemorySessionStore) PruneSessions(_ context.Context, before time.Time) (int, error) {
	return s.PruneSessionsExcept(context.Background(), before, nil)
}

func (s *InMemorySessionStore) PruneSessionsExcept(_ context.Context, before time.Time, excludeSessionIDs []string) (int, error) {
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
		deleted++
	}
	return deleted, nil
}

func (s *InMemoryRunStore) PruneRuns(_ context.Context, before time.Time) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for id, run := range s.byID {
		if run == nil || !isTerminalRunStatus(run.Status) || run.FinishedAt.IsZero() || !run.FinishedAt.Before(before) {
			continue
		}
		delete(s.byID, id)
		if strings.TrimSpace(run.InputEventID) != "" {
			delete(s.byEventID, run.InputEventID)
		}
		deleted++
	}
	return deleted, nil
}

func isTerminalRunStatus(status RunStatus) bool {
	switch status {
	case RunCompleted, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

type InMemoryCoordinator struct {
	mu     sync.Mutex
	queues map[string]*sessionQueue
}

type sessionQueue struct {
	active string
	queued []string
}

func NewInMemoryCoordinator() *InMemoryCoordinator {
	return &InMemoryCoordinator{
		queues: make(map[string]*sessionQueue),
	}
}

func (c *InMemoryCoordinator) EnqueueSessionRun(_ context.Context, sessionID, runID string, mode QueueMode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.queue(sessionID)
	switch mode {
	case QueueReject:
		if q.active != "" || len(q.queued) > 0 {
			return ErrRunRejected
		}
	case QueueInterrupt:
		q.queued = []string{runID}
		return nil
	case QueueCoalesce:
		if len(q.queued) > 0 {
			q.queued[len(q.queued)-1] = runID
			return nil
		}
		if q.active != "" {
			q.queued = append(q.queued, runID)
			return nil
		}
	}
	q.queued = append(q.queued, runID)
	return nil
}

func (c *InMemoryCoordinator) NextQueuedRun(_ context.Context, sessionID string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.queue(sessionID)
	if len(q.queued) == 0 {
		return "", false, nil
	}
	return q.queued[0], true, nil
}

func (c *InMemoryCoordinator) StartRun(_ context.Context, sessionID, runID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.queue(sessionID)
	if q.active == runID {
		return nil
	}
	if q.active != "" && q.active != runID {
		return fmt.Errorf("session %s already has active run %s", sessionID, q.active)
	}
	if len(q.queued) == 0 {
		q.active = runID
		return nil
	}
	if q.queued[0] != runID {
		// Allow approved/resumed runs to preempt queued work for the same session.
		q.active = runID
		return nil
	}
	q.active = runID
	q.queued = q.queued[1:]
	return nil
}

func (c *InMemoryCoordinator) FinishRun(_ context.Context, sessionID, runID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.queue(sessionID)
	if q.active == runID {
		q.active = ""
	}
	if len(q.queued) > 0 {
		filtered := q.queued[:0]
		for _, queuedRunID := range q.queued {
			if queuedRunID == runID {
				continue
			}
			filtered = append(filtered, queuedRunID)
		}
		q.queued = filtered
	}
	return nil
}

func (c *InMemoryCoordinator) queue(sessionID string) *sessionQueue {
	q, ok := c.queues[sessionID]
	if !ok {
		q = &sessionQueue{}
		c.queues[sessionID] = q
	}
	return q
}

func cloneSession(in *Session) *Session {
	if in == nil {
		return nil
	}
	out := *in
	out.MessageCount = in.TotalMessageCount()
	out.Messages = cloneMessages(in.Messages)
	out.LoadedMessageSeqs = append([]int64(nil), in.LoadedMessageSeqs...)
	out.PinnedFacts = clonePinnedFacts(in.PinnedFacts)
	out.Metadata = cloneMap(in.Metadata)
	out.SkillSnapshot = cloneSkillSnapshot(in.SkillSnapshot)
	return &out
}

func cloneSessionMetadata(in *Session) *Session {
	if in == nil {
		return nil
	}
	out := *in
	out.MessageCount = in.TotalMessageCount()
	out.Messages = nil
	out.LoadedMessageSeqs = nil
	out.PinnedFacts = clonePinnedFacts(in.PinnedFacts)
	out.Metadata = cloneMap(in.Metadata)
	out.SkillSnapshot = cloneSkillSnapshot(in.SkillSnapshot)
	return &out
}

func cloneRun(in *Run) *Run {
	if in == nil {
		return nil
	}
	out := *in
	out.Plan = clonePlan(in.Plan)
	out.ExecutionGraph = cloneExecutionGraph(in.ExecutionGraph)
	out.WorkflowState = cloneWorkflowState(in.WorkflowState)
	out.PendingTools = cloneToolCalls(in.PendingTools)
	out.SemanticSignal = cloneSemanticSignal(in.SemanticSignal)
	out.Preflight = cloneRunPreflightReport(in.Preflight)
	out.Triage = cloneRunTriageTrace(in.Triage)
	out.TaskContract = cloneTaskContract(in.TaskContract)
	out.Delegation = cloneDelegationContract(in.Delegation)
	out.Governance = cloneGovernanceEvaluation(in.Governance)
	if in.EffectiveProfile != nil {
		out.EffectiveProfile = cloneEffectiveAgentProfile(in.EffectiveProfile)
	}
	return &out
}

func cloneMessages(in []contextengine.Message) []contextengine.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]contextengine.Message, len(in))
	for i, msg := range in {
		out[i] = msg
		out[i].Metadata = cloneMap(msg.Metadata)
		out[i].ToolCalls = cloneToolCallRefs(msg.ToolCalls)
		out[i].ContentBlocks = cloneContentBlocks(msg.ContentBlocks)
	}
	return out
}

func cloneToolCallRefs(in []contextengine.ToolCallRef) []contextengine.ToolCallRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]contextengine.ToolCallRef, len(in))
	copy(out, in)
	return out
}

func cloneContentBlocks(in []contextengine.ContentBlock) []contextengine.ContentBlock {
	if len(in) == 0 {
		return nil
	}
	out := make([]contextengine.ContentBlock, len(in))
	copy(out, in)
	return out
}

func clonePinnedFacts(in []contextengine.PinnedFact) []contextengine.PinnedFact {
	if len(in) == 0 {
		return nil
	}
	out := make([]contextengine.PinnedFact, len(in))
	copy(out, in)
	for i := range out {
		out[i].Metadata = cloneMap(out[i].Metadata)
	}
	return out
}

func cloneSkillSnapshot(in skill.SessionSkillSnapshot) skill.SessionSkillSnapshot {
	out := in
	if len(in.Skills) > 0 {
		out.Skills = make(map[string]skill.BoundSkill, len(in.Skills))
		for name, bound := range in.Skills {
			out.Skills[name] = cloneBoundSkill(bound)
		}
	}
	if len(in.Ordered) > 0 {
		out.Ordered = make([]skill.BoundSkill, len(in.Ordered))
		for i, bound := range in.Ordered {
			out.Ordered[i] = cloneBoundSkill(bound)
		}
	}
	out.PromptCatalog = append([]skill.PromptCatalogEntry(nil), in.PromptCatalog...)
	out.Blocked = append([]skill.BlockedSkill(nil), in.Blocked...)
	return out
}

func cloneBoundSkill(in skill.BoundSkill) skill.BoundSkill {
	out := in
	out.Package = cloneSkillPackage(in.Package)
	out.Eligibility = cloneEligibilityResult(in.Eligibility)
	return out
}

func cloneSkillPackage(in *skill.SkillPackage) *skill.SkillPackage {
	if in == nil {
		return nil
	}
	out := *in
	out.ToolManifests = cloneToolManifests(in.ToolManifests)
	out.Issues = append([]skill.SkillIssue(nil), in.Issues...)
	out.Raw = cloneExternalSkillSpec(in.Raw)
	return &out
}

func cloneEligibilityResult(in skill.EligibilityResult) skill.EligibilityResult {
	out := in
	out.InjectedEnv = cloneStrings(in.InjectedEnv)
	out.Reasons = cloneStrings(in.Reasons)
	if len(in.Checks) > 0 {
		out.Checks = make([]skill.DependencyCheck, len(in.Checks))
		for i, check := range in.Checks {
			out.Checks[i] = check
			out.Checks[i].Candidates = cloneStrings(check.Candidates)
		}
	}
	return out
}

func cloneToolManifests(in []skill.ToolManifest) []skill.ToolManifest {
	if len(in) == 0 {
		return nil
	}
	out := make([]skill.ToolManifest, len(in))
	for i, manifest := range in {
		out[i] = manifest
		out[i].Aliases = cloneStrings(manifest.Aliases)
		out[i].InputSchema = skill.JSONSchema(supportmaps.Clone(manifest.InputSchema))
		out[i].OutputSchema = skill.JSONSchema(supportmaps.Clone(manifest.OutputSchema))
	}
	return out
}

func cloneExternalSkillSpec(in skill.ExternalSkillSpec) skill.ExternalSkillSpec {
	out := in
	out.Frontmatter = supportmaps.Clone(in.Frontmatter)
	out.RawMetadata = supportmaps.Clone(in.RawMetadata)
	out.OpenClaw = cloneOpenClawMetadata(in.OpenClaw)
	out.SupportingFiles = append([]skill.SkillFile(nil), in.SupportingFiles...)
	if in.Companion != nil {
		companion := *in.Companion
		companion.Tool = cloneToolManifestSpec(in.Companion.Tool)
		out.Companion = &companion
	}
	if in.Bundle != nil {
		bundle := *in.Bundle
		bundle.Tags = cloneStrings(in.Bundle.Tags)
		bundle.Requires = cloneRequiresSpec(in.Bundle.Requires)
		bundle.Install = skill.BundleInstallSpec{Steps: cloneInstallSpecs(in.Bundle.Install.Steps)}
		if in.Bundle.Runtime.Executable != nil {
			exec := *in.Bundle.Runtime.Executable
			bundle.Runtime.Executable = &exec
		}
		if in.Bundle.Runtime.Sidecar != nil {
			sidecar := *in.Bundle.Runtime.Sidecar
			bundle.Runtime.Sidecar = &sidecar
		}
		if in.Bundle.Prompt != nil {
			prompt := *in.Bundle.Prompt
			bundle.Prompt = &prompt
		}
		if len(in.Bundle.Tools) > 0 {
			bundle.Tools = make([]skill.ToolManifestSpec, len(in.Bundle.Tools))
			for i, spec := range in.Bundle.Tools {
				bundle.Tools[i] = cloneToolManifestSpec(spec)
			}
		}
		bundle.OpenClaw = cloneOpenClawMetadata(in.Bundle.OpenClaw)
		out.Bundle = &bundle
	}
	return out
}

func cloneToolManifestSpec(in skill.ToolManifestSpec) skill.ToolManifestSpec {
	out := in
	out.Aliases = cloneStrings(in.Aliases)
	out.InputSchema = skill.JSONSchema(supportmaps.Clone(in.InputSchema))
	out.OutputSchema = skill.JSONSchema(supportmaps.Clone(in.OutputSchema))
	return out
}

func cloneOpenClawMetadata(in skill.OpenClawMetadata) skill.OpenClawMetadata {
	out := in
	out.OS = cloneStrings(in.OS)
	out.Requires = cloneRequiresSpec(in.Requires)
	out.Install = cloneInstallSpecs(in.Install)
	return out
}

func cloneRequiresSpec(in skill.RequiresSpec) skill.RequiresSpec {
	out := in
	out.Bins = cloneStrings(in.Bins)
	out.AnyBins = cloneStrings(in.AnyBins)
	out.Env = cloneStrings(in.Env)
	out.Config = cloneStrings(in.Config)
	return out
}

func cloneInstallSpecs(in []skill.InstallSpec) []skill.InstallSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]skill.InstallSpec, len(in))
	for i, spec := range in {
		out[i] = spec
		out[i].OS = cloneStrings(spec.OS)
		out[i].Bins = cloneStrings(spec.Bins)
		out[i].Args = cloneStrings(spec.Args)
		if spec.Env != nil {
			out[i].Env = make(map[string]string, len(spec.Env))
			for key, value := range spec.Env {
				out[i].Env[key] = value
			}
		}
	}
	return out
}

func defaultString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
