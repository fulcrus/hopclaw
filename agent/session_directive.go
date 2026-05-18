package agent

import (
	"context"
	"sync"
	"time"
)

type SessionDirectiveKind string

const (
	SessionDirectiveSteer SessionDirectiveKind = "steer_current_run"
)

type SessionDirective struct {
	Kind      SessionDirectiveKind
	Content   string
	CreatedAt time.Time
}

type SessionDirectiveStore interface {
	Push(ctx context.Context, sessionID string, directive SessionDirective) error
	Drain(ctx context.Context, sessionID string, kind SessionDirectiveKind) ([]SessionDirective, error)
}

type InMemorySessionDirectiveStore struct {
	mu    sync.Mutex
	items map[string][]SessionDirective
}

func NewInMemorySessionDirectiveStore() *InMemorySessionDirectiveStore {
	return &InMemorySessionDirectiveStore{
		items: make(map[string][]SessionDirective),
	}
}

func (s *InMemorySessionDirectiveStore) Push(_ context.Context, sessionID string, directive SessionDirective) error {
	if s == nil || sessionID == "" {
		return nil
	}
	if directive.CreatedAt.IsZero() {
		directive.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[sessionID] = append(s.items[sessionID], directive)
	return nil
}

func (s *InMemorySessionDirectiveStore) Drain(_ context.Context, sessionID string, kind SessionDirectiveKind) ([]SessionDirective, error) {
	if s == nil || sessionID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.items[sessionID]
	if len(items) == 0 {
		return nil, nil
	}
	kept := make([]SessionDirective, 0, len(items))
	drained := make([]SessionDirective, 0, len(items))
	for _, item := range items {
		if kind != "" && item.Kind != kind {
			kept = append(kept, item)
			continue
		}
		drained = append(drained, item)
	}
	if len(kept) == 0 {
		delete(s.items, sessionID)
	} else {
		s.items[sessionID] = kept
	}
	return drained, nil
}
