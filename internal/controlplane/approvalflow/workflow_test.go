package approvalflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
)

func TestDispatcherSubmitAndUpdate(t *testing.T) {
	t.Parallel()

	runtime := &stubRuntime{
		tickets: map[string]*approval.Ticket{
			"appr-1": {
				ID:        "appr-1",
				RunID:     "run-1",
				SessionID: "sess-1",
				Kind:      approval.KindToolCalls,
				Status:    approval.StatusPending,
				Metadata: map[string]any{
					"effective_config_snapshot_id": "ecs-1",
				},
			},
		},
		snapshot: &controlsnapshot.EffectiveConfigSnapshot{ID: "ecs-1"},
	}
	provider := &observedProvider{}
	dispatcher := NewDispatcher(runtime, provider)

	if err := dispatcher.Handle(context.Background(), eventbus.Event{
		Type: eventbus.EventApprovalRequested,
		Attrs: map[string]any{
			"approval_id": "appr-1",
		},
	}); err != nil {
		t.Fatalf("Handle(requested) error = %v", err)
	}
	if err := dispatcher.Handle(context.Background(), eventbus.Event{
		Type: eventbus.EventApprovalGraceWarning,
		Attrs: map[string]any{
			"approval_id":  "appr-1",
			"remaining_ms": int64(45000),
		},
	}); err != nil {
		t.Fatalf("Handle(grace) error = %v", err)
	}

	if provider.submitCount != 1 {
		t.Fatalf("submitCount = %d, want 1", provider.submitCount)
	}
	if provider.updateCount != 1 {
		t.Fatalf("updateCount = %d, want 1", provider.updateCount)
	}
	if provider.lastSubmit.Snapshot == nil || provider.lastSubmit.Snapshot.ID != "ecs-1" {
		t.Fatalf("Submit snapshot = %#v", provider.lastSubmit.Snapshot)
	}
	if provider.lastUpdate.Remaining != 45*time.Second {
		t.Fatalf("Update remaining = %v", provider.lastUpdate.Remaining)
	}
	if got := runtime.tickets["appr-1"].External; len(got) != 1 || got[0].Provider != "observed" || got[0].ExternalID != "ext-1" {
		t.Fatalf("ticket external = %#v", got)
	}
}

func TestDispatcherSyncPendingApprovals(t *testing.T) {
	t.Parallel()

	runtime := &stubRuntime{
		tickets: map[string]*approval.Ticket{
			"appr-2": {
				ID:        "appr-2",
				RunID:     "run-2",
				SessionID: "sess-2",
				Status:    approval.StatusPending,
			},
		},
	}
	provider := &syncProvider{
		results: []SyncResult{{
			TicketID: "appr-2",
			Resolution: approval.Resolution{
				Status: approval.StatusApproved,
			},
			ExternalID:     "sync-ext-1",
			ExternalStatus: "approved_remote",
		}},
	}
	dispatcher := NewDispatcher(runtime, provider)

	if err := dispatcher.SyncPendingApprovals(context.Background()); err != nil {
		t.Fatalf("SyncPendingApprovals() error = %v", err)
	}
	if len(runtime.resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(runtime.resolved))
	}
	if runtime.resolved[0].Status != approval.StatusApproved {
		t.Fatalf("resolved status = %q", runtime.resolved[0].Status)
	}
	if runtime.resolved[0].ResolvedBy != "approval_sync:sync" {
		t.Fatalf("resolved by = %q", runtime.resolved[0].ResolvedBy)
	}
	if got := runtime.tickets["appr-2"].External; len(got) != 1 || got[0].ExternalID != "sync-ext-1" || got[0].Status != "approved_remote" {
		t.Fatalf("ticket external = %#v", got)
	}
}

func TestResolveCallbackRequestResolution(t *testing.T) {
	t.Parallel()

	ticketID, resolution, err := (ResolveCallbackRequest{
		Provider: "jira",
		TicketID: "appr-3",
		Decision: "approve",
		Scope:    "session",
	}).Resolution()
	if err != nil {
		t.Fatalf("Resolution() error = %v", err)
	}
	if ticketID != "appr-3" {
		t.Fatalf("ticketID = %q", ticketID)
	}
	if resolution.Status != approval.StatusApproved {
		t.Fatalf("status = %q", resolution.Status)
	}
	if resolution.ResolvedBy != "provider:jira" {
		t.Fatalf("resolved_by = %q", resolution.ResolvedBy)
	}
}

type stubRuntime struct {
	mu       sync.Mutex
	tickets  map[string]*approval.Ticket
	snapshot *controlsnapshot.EffectiveConfigSnapshot
	resolved []approval.Resolution
}

func (s *stubRuntime) GetApproval(_ context.Context, id string) (*approval.Ticket, error) {
	if ticket, ok := s.tickets[id]; ok {
		out := *ticket
		return &out, nil
	}
	return nil, errors.New("not found")
}

func (s *stubRuntime) ListApprovals(_ context.Context, status approval.Status) ([]*approval.Ticket, error) {
	out := make([]*approval.Ticket, 0, len(s.tickets))
	for _, ticket := range s.tickets {
		if status != "" && ticket.Status != status {
			continue
		}
		clone := *ticket
		out = append(out, &clone)
	}
	return out, nil
}

func (s *stubRuntime) GetApprovalByExternal(_ context.Context, provider, externalID string) (*approval.Ticket, error) {
	for _, ticket := range s.tickets {
		for _, ref := range ticket.External {
			if ref.Provider == provider && ref.ExternalID == externalID {
				out := *ticket
				out.External = approval.CloneExternalReferences(ticket.External)
				return &out, nil
			}
		}
	}
	return nil, errors.New("not found")
}

func (s *stubRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved = append(s.resolved, resolution)
	if ticket, ok := s.tickets[id]; ok {
		ticket.Status = resolution.Status
		ticket.ResolvedBy = resolution.ResolvedBy
		out := *ticket
		return &out, nil
	}
	return nil, errors.New("not found")
}

func (s *stubRuntime) UpsertApprovalExternalRef(_ context.Context, id string, ref approval.ExternalReference) (*approval.Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[id]
	if !ok {
		return nil, errors.New("not found")
	}
	nextRefs, _, err := approval.UpsertExternalReferences(ticket.External, ref)
	if err != nil {
		return nil, err
	}
	ticket.External = nextRefs
	out := *ticket
	out.External = approval.CloneExternalReferences(ticket.External)
	return &out, nil
}

func (s *stubRuntime) EffectiveConfigSnapshot() *controlsnapshot.EffectiveConfigSnapshot {
	if s.snapshot == nil {
		return nil
	}
	return s.snapshot.Clone()
}

type observedProvider struct {
	submitCount int
	updateCount int
	lastSubmit  SubmitRequest
	lastUpdate  UpdateRequest
}

func (p *observedProvider) Name() string { return "observed" }

func (p *observedProvider) SubmitApproval(_ context.Context, req SubmitRequest) (*Submission, error) {
	p.submitCount++
	p.lastSubmit = req
	return &Submission{ExternalID: "ext-1"}, nil
}

func (p *observedProvider) UpdateApproval(_ context.Context, req UpdateRequest) error {
	p.updateCount++
	p.lastUpdate = req
	return nil
}

type syncProvider struct {
	results []SyncResult
}

func (p *syncProvider) Name() string { return "sync" }

func (p *syncProvider) SubmitApproval(context.Context, SubmitRequest) (*Submission, error) {
	return nil, nil
}

func (p *syncProvider) UpdateApproval(context.Context, UpdateRequest) error {
	return nil
}

func (p *syncProvider) SyncPendingApprovals(context.Context, SyncRequest) ([]SyncResult, error) {
	return append([]SyncResult(nil), p.results...), nil
}
