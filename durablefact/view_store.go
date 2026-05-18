package durablefact

import "context"

type ContextViewReader interface {
	ListContextViews(context.Context, Filter) ([]ContextView, error)
}

type ConfigViewReader interface {
	ListConfigViews(context.Context, Filter) ([]ConfigView, error)
}

type OperatorViewReader interface {
	ListOperatorViews(context.Context, Filter) ([]OperatorView, error)
}

func (s *SQLiteStore) ListContextViews(ctx context.Context, filter Filter) ([]ContextView, error) {
	facts, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]ContextView, 0, len(facts))
	for _, fact := range facts {
		view, ok := ToContextView(fact)
		if !ok {
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SQLiteStore) ListConfigViews(ctx context.Context, filter Filter) ([]ConfigView, error) {
	facts, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]ConfigView, 0, len(facts))
	for _, fact := range facts {
		view, ok := ToConfigView(fact)
		if !ok {
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SQLiteStore) ListOperatorViews(ctx context.Context, filter Filter) ([]OperatorView, error) {
	facts, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]OperatorView, 0, len(facts))
	for _, fact := range facts {
		views = append(views, ToOperatorView(fact))
	}
	return views, nil
}
