package hooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Store interface
// ---------------------------------------------------------------------------

// Store manages hook persistence.
type Store interface {
	List(ctx context.Context) ([]*Hook, error)
	Get(ctx context.Context, hookID string) (*Hook, error)
	Add(ctx context.Context, hook Hook) (*Hook, error)
	Update(ctx context.Context, hook Hook) (*Hook, error)
	Remove(ctx context.Context, hookID string) error
}

// ---------------------------------------------------------------------------
// InMemoryStore
// ---------------------------------------------------------------------------

// InMemoryStore is a thread-safe in-memory Store implementation.
type InMemoryStore struct {
	mu     sync.RWMutex // guards hooks
	nextID atomic.Uint64
	hooks  map[string]*Hook
}

// NewInMemoryStore returns a ready-to-use InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		hooks: make(map[string]*Hook),
	}
}

func (s *InMemoryStore) List(_ context.Context) ([]*Hook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Hook, 0, len(s.hooks))
	for _, h := range s.hooks {
		cp := *h
		out = append(out, &cp)
	}
	return out, nil
}

func (s *InMemoryStore) Get(_ context.Context, hookID string) (*Hook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h, ok := s.hooks[hookID]
	if !ok {
		return nil, fmt.Errorf("hook %s: not found", hookID)
	}
	cp := *h
	return &cp, nil
}

func (s *InMemoryStore) Add(_ context.Context, hook Hook) (*Hook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hook.ID = fmt.Sprintf("hook-%06d", s.nextID.Add(1))
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = time.Now().UTC()
	}
	if hook.Priority == 0 {
		hook.Priority = defaultHookPriority
	}
	if hook.Phase == "" {
		hook.Phase = HookPhasePost
	}
	cp := hook
	s.hooks[cp.ID] = &cp
	ret := cp
	return &ret, nil
}

func (s *InMemoryStore) Update(_ context.Context, hook Hook) (*Hook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.hooks[hook.ID]; !ok {
		return nil, fmt.Errorf("hook %s: not found", hook.ID)
	}
	cp := hook
	s.hooks[cp.ID] = &cp
	ret := cp
	return &ret, nil
}

func (s *InMemoryStore) Remove(_ context.Context, hookID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.hooks[hookID]; !ok {
		return fmt.Errorf("hook %s: not found", hookID)
	}
	delete(s.hooks, hookID)
	return nil
}

// ListByTrigger returns hooks matching the given trigger and phase, sorted by
// priority ascending. Only enabled hooks are returned.
func (s *InMemoryStore) ListByTrigger(trigger TriggerEvent, phase HookPhase) []Hook {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if phase == "" {
		phase = HookPhasePost
	}

	var matched []Hook
	for _, h := range s.hooks {
		if !h.Enabled || h.Trigger != trigger {
			continue
		}
		if h.EffectivePhase() != phase {
			continue
		}
		matched = append(matched, *h)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].EffectivePriority() < matched[j].EffectivePriority()
	})
	return matched
}
