package approval

import (
	"context"
	"time"

	"github.com/fulcrus/hopclaw/internal/metrics"
)

type MetricsStore struct {
	Inner Store
}

func (m *MetricsStore) Create(ctx context.Context, ticket Ticket) (*Ticket, error) {
	next, err := m.Inner.Create(ctx, ticket)
	if err == nil {
		metrics.ApprovalsPending.Inc()
	}
	return next, err
}

func (m *MetricsStore) Get(ctx context.Context, ticketID string) (*Ticket, error) {
	return m.Inner.Get(ctx, ticketID)
}

func (m *MetricsStore) GetByRun(ctx context.Context, runID string) (*Ticket, error) {
	return m.Inner.GetByRun(ctx, runID)
}

func (m *MetricsStore) GetByExternal(ctx context.Context, provider, externalID string) (*Ticket, error) {
	return m.Inner.GetByExternal(ctx, provider, externalID)
}

func (m *MetricsStore) List(ctx context.Context, filter ListFilter) ([]*Ticket, error) {
	return m.Inner.List(ctx, filter)
}

func (m *MetricsStore) Resolve(ctx context.Context, ticketID string, resolution Resolution) (*Ticket, error) {
	next, err := m.Inner.Resolve(ctx, ticketID, resolution)
	if err == nil {
		metrics.ApprovalsPending.Dec()
		if next != nil && !next.CreatedAt.IsZero() {
			metrics.ApprovalWaitDuration.Observe(time.Since(next.CreatedAt).Seconds())
		}
	}
	return next, err
}

func (m *MetricsStore) UpsertExternalRef(ctx context.Context, ticketID string, ref ExternalReference) (*Ticket, error) {
	return m.Inner.UpsertExternalRef(ctx, ticketID, ref)
}
