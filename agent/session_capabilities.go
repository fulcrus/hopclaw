package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

type scopedSessionListerAdapter struct {
	scoped ScopedSessionLister
}

func (a scopedSessionListerAdapter) List(ctx context.Context) ([]*Session, error) {
	return a.scoped.ListScoped(ctx, SessionListFilter{})
}

type scopedSessionReaderAdapter struct {
	scoped ScopedSessionReader
}

func (a scopedSessionReaderAdapter) Get(ctx context.Context, sessionID string) (*Session, error) {
	return a.scoped.GetScoped(ctx, sessionID, ScopeFilter{})
}

type scopedSessionMetadataReaderAdapter struct {
	scoped ScopedSessionMetadataReader
}

func (a scopedSessionMetadataReaderAdapter) GetMetadata(ctx context.Context, sessionID string) (*Session, error) {
	return a.scoped.GetMetadataScoped(ctx, sessionID, ScopeFilter{})
}

type scopedSessionKeyReaderAdapter struct {
	scoped ScopedSessionKeyReader
}

func (a scopedSessionKeyReaderAdapter) GetByKey(ctx context.Context, sessionKey string) (*Session, error) {
	return a.scoped.GetByKeyScoped(ctx, sessionKey, ScopeFilter{})
}

type scopedSessionKeyMetadataReaderAdapter struct {
	scoped ScopedSessionKeyMetadataReader
}

func (a scopedSessionKeyMetadataReaderAdapter) GetByKeyMetadata(ctx context.Context, sessionKey string) (*Session, error) {
	return a.scoped.GetByKeyMetadataScoped(ctx, sessionKey, ScopeFilter{})
}

type scopedSessionPrunerAdapter struct {
	scoped ScopedSessionPruner
}

func (a scopedSessionPrunerAdapter) PruneSessions(ctx context.Context, before time.Time) (int, error) {
	return a.scoped.PruneSessionsExcept(ctx, before, nil)
}

func SessionQueryCapability(store SessionStore) SessionQueryStore {
	if store == nil {
		return nil
	}
	query, _ := store.(SessionQueryStore)
	return query
}

func SessionListerCapability(store SessionStore) SessionLister {
	if store == nil {
		return nil
	}
	if lister, ok := store.(SessionLister); ok {
		return lister
	}
	if scoped, ok := store.(ScopedSessionLister); ok {
		return scopedSessionListerAdapter{scoped: scoped}
	}
	return nil
}

func SessionReaderCapability(store SessionStore) SessionReader {
	if store == nil {
		return nil
	}
	if reader, ok := store.(SessionReader); ok {
		return reader
	}
	if scoped, ok := store.(ScopedSessionReader); ok {
		return scopedSessionReaderAdapter{scoped: scoped}
	}
	return nil
}

func SessionMetadataReaderCapability(store SessionStore) SessionMetadataReader {
	if store == nil {
		return nil
	}
	if reader, ok := store.(SessionMetadataReader); ok {
		return reader
	}
	if scoped, ok := store.(ScopedSessionMetadataReader); ok {
		return scopedSessionMetadataReaderAdapter{scoped: scoped}
	}
	return nil
}

func SessionKeyReaderCapability(store SessionStore) SessionKeyReader {
	if store == nil {
		return nil
	}
	if reader, ok := store.(SessionKeyReader); ok {
		return reader
	}
	if scoped, ok := store.(ScopedSessionKeyReader); ok {
		return scopedSessionKeyReaderAdapter{scoped: scoped}
	}
	return nil
}

func SessionKeyMetadataReaderCapability(store SessionStore) SessionKeyMetadataReader {
	if store == nil {
		return nil
	}
	if reader, ok := store.(SessionKeyMetadataReader); ok {
		return reader
	}
	if scoped, ok := store.(ScopedSessionKeyMetadataReader); ok {
		return scopedSessionKeyMetadataReaderAdapter{scoped: scoped}
	}
	return nil
}

func SessionRecentMessageReaderCapability(store SessionStore) SessionRecentMessageReader {
	if store == nil {
		return nil
	}
	reader, _ := store.(SessionRecentMessageReader)
	return reader
}

func SessionMaintenanceCapability(store SessionStore) SessionMaintenanceStore {
	if store == nil {
		return nil
	}
	maintenance, _ := store.(SessionMaintenanceStore)
	return maintenance
}

func SessionDeleterCapability(store SessionStore) SessionDeleter {
	if store == nil {
		return nil
	}
	deleter, _ := store.(SessionDeleter)
	return deleter
}

func SessionPrunerCapability(store SessionStore) SessionPruner {
	if store == nil {
		return nil
	}
	if pruner, ok := store.(SessionPruner); ok {
		return pruner
	}
	if scoped, ok := store.(ScopedSessionPruner); ok {
		return scopedSessionPrunerAdapter{scoped: scoped}
	}
	return nil
}

func ListSessions(ctx context.Context, store SessionStore, filter SessionListFilter) ([]*Session, error) {
	if query := SessionQueryCapability(store); query != nil {
		sessions, err := query.ListScoped(ctx, filter)
		if err != nil {
			return nil, err
		}
		return filterListedSessions(sessions, filter), nil
	}
	if scoped, ok := store.(ScopedSessionLister); ok {
		sessions, err := scoped.ListScoped(ctx, filter)
		if err != nil {
			return nil, err
		}
		return filterListedSessions(sessions, filter), nil
	}
	lister := SessionListerCapability(store)
	if lister == nil {
		return nil, ErrSessionListUnsupported
	}
	sessions, err := lister.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterListedSessions(sessions, filter), nil
}

func LoadSession(ctx context.Context, store SessionStore, sessionID string, scope ScopeFilter) (*Session, error) {
	if query := SessionQueryCapability(store); query != nil {
		return query.GetScoped(ctx, sessionID, scope)
	}
	if scoped, ok := store.(ScopedSessionReader); ok {
		return scoped.GetScoped(ctx, sessionID, scope)
	}
	reader := SessionReaderCapability(store)
	if reader == nil {
		return nil, ErrSessionReadUnsupported
	}
	session, err := reader.Get(ctx, sessionID)
	if err != nil || session == nil {
		return session, err
	}
	if !scope.IsZero() && !scope.Matches(session.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return session, nil
}

func LoadSessionMetadata(ctx context.Context, store SessionStore, sessionID string, scope ScopeFilter) (*Session, error) {
	if query := SessionQueryCapability(store); query != nil {
		return query.GetMetadataScoped(ctx, sessionID, scope)
	}
	if scoped, ok := store.(ScopedSessionMetadataReader); ok {
		return scoped.GetMetadataScoped(ctx, sessionID, scope)
	}
	reader := SessionMetadataReaderCapability(store)
	if reader == nil {
		return LoadSession(ctx, store, sessionID, scope)
	}
	session, err := reader.GetMetadata(ctx, sessionID)
	if err != nil || session == nil {
		return session, err
	}
	if !scope.IsZero() && !scope.Matches(session.Scope) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return session, nil
}

func LoadSessionByKey(ctx context.Context, store SessionStore, sessionKey string, scope ScopeFilter) (*Session, error) {
	if query := SessionQueryCapability(store); query != nil {
		return query.GetByKeyScoped(ctx, sessionKey, scope)
	}
	if scoped, ok := store.(ScopedSessionKeyReader); ok {
		return scoped.GetByKeyScoped(ctx, sessionKey, scope)
	}
	reader := SessionKeyReaderCapability(store)
	if reader == nil {
		return nil, ErrSessionKeyLookupUnsupported
	}
	session, err := reader.GetByKey(ctx, sessionKey)
	if err != nil || session == nil {
		return session, err
	}
	if !scope.IsZero() && !scope.Matches(session.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return session, nil
}

func LoadSessionMetadataByKey(ctx context.Context, store SessionStore, sessionKey string, scope ScopeFilter) (*Session, error) {
	if query := SessionQueryCapability(store); query != nil {
		return query.GetByKeyMetadataScoped(ctx, sessionKey, scope)
	}
	if scoped, ok := store.(ScopedSessionKeyMetadataReader); ok {
		return scoped.GetByKeyMetadataScoped(ctx, sessionKey, scope)
	}
	reader := SessionKeyMetadataReaderCapability(store)
	if reader == nil {
		return LoadSessionByKey(ctx, store, sessionKey, scope)
	}
	session, err := reader.GetByKeyMetadata(ctx, sessionKey)
	if err != nil || session == nil {
		return session, err
	}
	if !scope.IsZero() && !scope.Matches(session.Scope) {
		return nil, fmt.Errorf("session %q not found", sessionKey)
	}
	return session, nil
}

func LoadRecentMessages(ctx context.Context, store SessionStore, sessionID string, limit int) ([]contextengine.Message, error) {
	if query := SessionQueryCapability(store); query != nil {
		return query.RecentMessages(ctx, sessionID, limit)
	}
	if recentReader := SessionRecentMessageReaderCapability(store); recentReader != nil {
		messages, err := recentReader.RecentMessages(ctx, sessionID, limit)
		if err == nil && len(messages) > 0 {
			return messages, nil
		}
	}
	session, err := LoadSession(ctx, store, sessionID, ScopeFilter{})
	if err != nil || session == nil {
		return nil, err
	}
	return session.Messages, nil
}

func DeleteStoredSession(ctx context.Context, store SessionStore, sessionID string) error {
	if maintenance := SessionMaintenanceCapability(store); maintenance != nil {
		return maintenance.DeleteSession(ctx, sessionID)
	}
	deleter := SessionDeleterCapability(store)
	if deleter == nil {
		return ErrSessionDeleteUnsupported
	}
	return deleter.DeleteSession(ctx, sessionID)
}

func PruneStoredSessions(ctx context.Context, store SessionStore, before time.Time, excludeSessionIDs []string) (int, error) {
	if maintenance := SessionMaintenanceCapability(store); maintenance != nil {
		return maintenance.PruneSessionsExcept(ctx, before, excludeSessionIDs)
	}
	if scoped, ok := store.(ScopedSessionPruner); ok {
		return scoped.PruneSessionsExcept(ctx, before, excludeSessionIDs)
	}
	pruner := SessionPrunerCapability(store)
	if pruner == nil {
		return 0, ErrSessionPruneUnsupported
	}
	return pruner.PruneSessions(ctx, before)
}

func filterListedSessions(sessions []*Session, filter SessionListFilter) []*Session {
	if len(sessions) == 0 {
		return sessions
	}
	filtered := sessions[:0]
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if !filter.Scope.IsZero() && !filter.Scope.Matches(session.Scope) {
			continue
		}
		filtered = append(filtered, session)
		if filter.Limit > 0 && len(filtered) >= filter.Limit {
			break
		}
	}
	return filtered
}
