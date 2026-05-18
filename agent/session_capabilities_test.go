package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

type baseSessionStore struct{}

func (baseSessionStore) GetOrCreate(context.Context, string, string, ...string) (*Session, error) {
	return nil, nil
}

func (baseSessionStore) AppendUserMessage(context.Context, string, IncomingMessage) error {
	return nil
}

func (baseSessionStore) LoadForExecution(context.Context, string) (*Session, func(), error) {
	return nil, func() {}, nil
}

func (baseSessionStore) Save(context.Context, *Session) error {
	return nil
}

type listerSessionStore struct {
	baseSessionStore
	listFn func(context.Context) ([]*Session, error)
}

func (s listerSessionStore) List(ctx context.Context) ([]*Session, error) {
	return s.listFn(ctx)
}

type dualReaderSessionStore struct {
	baseSessionStore
	getCalled       bool
	getScopedCalled bool
}

func (s *dualReaderSessionStore) Get(context.Context, string) (*Session, error) {
	s.getCalled = true
	return &Session{ID: "plain"}, nil
}

func (s *dualReaderSessionStore) GetScoped(context.Context, string, ScopeFilter) (*Session, error) {
	s.getScopedCalled = true
	return &Session{ID: "scoped"}, nil
}

type keyReaderSessionStore struct {
	baseSessionStore
	getByKeyFn func(context.Context, string) (*Session, error)
}

func (s keyReaderSessionStore) GetByKey(ctx context.Context, sessionKey string) (*Session, error) {
	return s.getByKeyFn(ctx, sessionKey)
}

type scopedKeyReaderSessionStore struct {
	baseSessionStore
	getByKeyScopedFn func(context.Context, string, ScopeFilter) (*Session, error)
}

func (s scopedKeyReaderSessionStore) GetByKeyScoped(ctx context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
	return s.getByKeyScopedFn(ctx, sessionKey, scope)
}

type scopedKeyMetadataReaderSessionStore struct {
	baseSessionStore
	getByKeyMetadataScopedFn func(context.Context, string, ScopeFilter) (*Session, error)
}

func (s scopedKeyMetadataReaderSessionStore) GetByKeyMetadataScoped(ctx context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
	return s.getByKeyMetadataScopedFn(ctx, sessionKey, scope)
}

type scopedPrunerSessionStore struct {
	baseSessionStore
	pruneScopedFn func(context.Context, time.Time, []string) (int, error)
}

func (s scopedPrunerSessionStore) PruneSessionsExcept(ctx context.Context, before time.Time, excludeSessionIDs []string) (int, error) {
	return s.pruneScopedFn(ctx, before, excludeSessionIDs)
}

func TestListSessionsFiltersScopeAndLimit(t *testing.T) {
	t.Parallel()

	store := listerSessionStore{
		listFn: func(context.Context) ([]*Session, error) {
			return []*Session{
				{ID: "sess-1", Scope: domainscope.Ref{AutomationID: "auto-a"}},
				{ID: "sess-2", Scope: domainscope.Ref{AutomationID: "auto-b"}},
				{ID: "sess-3", Scope: domainscope.Ref{AutomationID: "auto-a"}},
			}, nil
		},
	}

	sessions, err := ListSessions(context.Background(), store, SessionListFilter{
		Scope: ScopeFilter{},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess-1" {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestLoadSessionPrefersScopedReader(t *testing.T) {
	t.Parallel()

	store := &dualReaderSessionStore{}
	session, err := LoadSession(context.Background(), store, "sess-1", ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if session == nil || session.ID != "scoped" {
		t.Fatalf("session = %#v", session)
	}
	if !store.getScopedCalled {
		t.Fatal("expected scoped reader to be used")
	}
	if store.getCalled {
		t.Fatal("expected unscoped reader to remain unused when scoped reader exists")
	}
}

func TestLoadSessionByKeyAppliesScopeToUnscopedReader(t *testing.T) {
	t.Parallel()

	store := keyReaderSessionStore{
		getByKeyFn: func(context.Context, string) (*Session, error) {
			return &Session{
				ID:    "sess-1",
				Key:   "slack:thread-1",
				Scope: domainscope.Ref{AutomationID: "auto-b"},
			}, nil
		},
	}

	session, err := LoadSessionByKey(context.Background(), store, "slack:thread-1", ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionByKey() error = %v", err)
	}
	if session == nil || session.ID != "sess-1" {
		t.Fatalf("session = %#v", session)
	}
}

func TestLoadSessionMetadataByKeyPrefersScopedMetadataReader(t *testing.T) {
	t.Parallel()

	store := scopedKeyMetadataReaderSessionStore{
		getByKeyMetadataScopedFn: func(_ context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
			if sessionKey != "slack:thread-2" {
				t.Fatalf("sessionKey = %q", sessionKey)
			}
			if !scope.IsZero() {
				t.Fatalf("scope = %#v", scope)
			}
			return &Session{ID: "sess-meta", Key: sessionKey, MessageCount: 7}, nil
		},
	}

	session, err := LoadSessionMetadataByKey(context.Background(), store, "slack:thread-2", ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionMetadataByKey() error = %v", err)
	}
	if session == nil || session.ID != "sess-meta" || session.TotalMessageCount() != 7 {
		t.Fatalf("session = %#v", session)
	}
}

func TestSessionKeyReaderCapabilityWrapsScopedReader(t *testing.T) {
	t.Parallel()

	store := scopedKeyReaderSessionStore{
		getByKeyScopedFn: func(_ context.Context, sessionKey string, scope ScopeFilter) (*Session, error) {
			if sessionKey != "discord:thread-1" {
				t.Fatalf("sessionKey = %q", sessionKey)
			}
			if !scope.IsZero() {
				t.Fatalf("scope = %#v, want zero scope", scope)
			}
			return &Session{ID: "sess-1", Key: sessionKey}, nil
		},
	}

	reader := SessionKeyReaderCapability(store)
	if reader == nil {
		t.Fatal("SessionKeyReaderCapability() = nil")
	}
	session, err := reader.GetByKey(context.Background(), "discord:thread-1")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if session == nil || session.ID != "sess-1" {
		t.Fatalf("session = %#v", session)
	}
}

func TestSessionQueryCapabilityUsesGroupedContract(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	ctx := context.Background()
	session, err := store.GetOrCreate(ctx, "slack:thread-3", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := store.AppendUserMessage(ctx, session.ID, IncomingMessage{
		Content: "hello",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	query := SessionQueryCapability(store)
	if query == nil {
		t.Fatal("SessionQueryCapability() = nil")
	}
	meta, err := query.GetMetadataScoped(ctx, session.ID, ScopeFilter{})
	if err != nil {
		t.Fatalf("GetMetadataScoped() error = %v", err)
	}
	if meta == nil || meta.ID != session.ID || meta.TotalMessageCount() != 1 {
		t.Fatalf("metadata = %#v", meta)
	}
	byKey, err := query.GetByKeyMetadataScoped(ctx, "slack:thread-3", ScopeFilter{})
	if err != nil {
		t.Fatalf("GetByKeyMetadataScoped() error = %v", err)
	}
	if byKey == nil || byKey.ID != session.ID {
		t.Fatalf("byKey = %#v", byKey)
	}
	msgs, err := query.RecentMessages(ctx, session.ID, 1)
	if err != nil {
		t.Fatalf("RecentMessages() error = %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("messages = %#v", msgs)
	}
}

func TestPruneStoredSessionsUsesScopedPruner(t *testing.T) {
	t.Parallel()

	var captured []string
	store := scopedPrunerSessionStore{
		pruneScopedFn: func(_ context.Context, _ time.Time, excludeSessionIDs []string) (int, error) {
			captured = append([]string(nil), excludeSessionIDs...)
			return 3, nil
		},
	}

	count, err := PruneStoredSessions(context.Background(), store, time.Now().UTC(), []string{"sess-1", "sess-2"})
	if err != nil {
		t.Fatalf("PruneStoredSessions() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if !reflect.DeepEqual(captured, []string{"sess-1", "sess-2"}) {
		t.Fatalf("captured exclude IDs = %#v", captured)
	}
}

func TestDeleteStoredSessionUsesMaintenanceContract(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	ctx := context.Background()
	session, err := store.GetOrCreate(ctx, "discord:thread-7", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	if err := DeleteStoredSession(ctx, store, session.ID); err != nil {
		t.Fatalf("DeleteStoredSession() error = %v", err)
	}
	if _, err := LoadSession(ctx, store, session.ID, ScopeFilter{}); err == nil {
		t.Fatal("expected deleted session to be gone")
	}
	if _, err := LoadSessionByKey(ctx, store, "discord:thread-7", ScopeFilter{}); err == nil {
		t.Fatal("expected deleted session key lookup to fail")
	}
}

func TestListSessionsReturnsUnsupportedSentinel(t *testing.T) {
	t.Parallel()

	_, err := ListSessions(context.Background(), baseSessionStore{}, SessionListFilter{})
	if !errors.Is(err, ErrSessionListUnsupported) {
		t.Fatalf("ListSessions() error = %v, want ErrSessionListUnsupported", err)
	}
}
