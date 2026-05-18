package runtime

import (
	"context"

	"github.com/fulcrus/hopclaw/durablefact"
)

// ListMemoryContextViews returns durable context views from the configured
// memory store. The boolean result reports whether the current memory store
// supports DurableFact-backed typed views.
func (s *Service) ListMemoryContextViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.ContextView, bool, error) {
	if s == nil || s.memory == nil {
		return nil, false, nil
	}
	provider, ok := s.memory.(durablefact.ContextViewReader)
	if !ok {
		return nil, false, nil
	}
	views, err := provider.ListContextViews(ctx, filter)
	return views, true, err
}

// ListMemoryOperatorViews returns operator-facing durable views from the
// configured memory store. The boolean result reports whether the current
// memory store supports DurableFact-backed typed views.
func (s *Service) ListMemoryOperatorViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, bool, error) {
	if s == nil || s.memory == nil {
		return nil, false, nil
	}
	provider, ok := s.memory.(durablefact.OperatorViewReader)
	if !ok {
		return nil, false, nil
	}
	views, err := provider.ListOperatorViews(ctx, filter)
	return views, true, err
}
