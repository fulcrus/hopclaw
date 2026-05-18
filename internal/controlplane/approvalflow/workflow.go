package approvalflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("approval_flow")

type Provider interface {
	Name() string
	SubmitApproval(ctx context.Context, req SubmitRequest) (*Submission, error)
	UpdateApproval(ctx context.Context, req UpdateRequest) error
}

type SyncProvider interface {
	SyncPendingApprovals(ctx context.Context, req SyncRequest) ([]SyncResult, error)
}

type Runtime interface {
	GetApproval(ctx context.Context, id string) (*approval.Ticket, error)
	GetApprovalByExternal(ctx context.Context, provider, externalID string) (*approval.Ticket, error)
	ListApprovals(ctx context.Context, status approval.Status) ([]*approval.Ticket, error)
	ResolveApproval(ctx context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error)
	UpsertApprovalExternalRef(ctx context.Context, id string, ref approval.ExternalReference) (*approval.Ticket, error)
	EffectiveConfigSnapshot() *controlsnapshot.EffectiveConfigSnapshot
}

type Submission struct {
	ExternalID string         `json:"external_id,omitempty"`
	URL        string         `json:"url,omitempty"`
	Status     string         `json:"status,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type SubmitRequest struct {
	Provider string                                   `json:"provider"`
	Event    eventbus.Event                           `json:"event"`
	Ticket   approval.Ticket                          `json:"ticket"`
	Snapshot *controlsnapshot.EffectiveConfigSnapshot `json:"snapshot,omitempty"`
}

type UpdateRequest struct {
	Provider  string                                   `json:"provider"`
	Event     eventbus.Event                           `json:"event"`
	Ticket    approval.Ticket                          `json:"ticket"`
	Snapshot  *controlsnapshot.EffectiveConfigSnapshot `json:"snapshot,omitempty"`
	Remaining time.Duration                            `json:"remaining,omitempty"`
}

type SyncRequest struct {
	Provider string                                   `json:"provider"`
	Pending  []approval.Ticket                        `json:"pending"`
	Snapshot *controlsnapshot.EffectiveConfigSnapshot `json:"snapshot,omitempty"`
}

type SyncResult struct {
	TicketID       string              `json:"ticket_id,omitempty"`
	Resolution     approval.Resolution `json:"resolution"`
	ExternalID     string              `json:"external_id,omitempty"`
	URL            string              `json:"url,omitempty"`
	ExternalStatus string              `json:"external_status,omitempty"`
	Metadata       map[string]any      `json:"metadata,omitempty"`
}

type ResolveCallbackRequest = controlplane.ApprovalResolveCallbackRequest

type Dispatcher struct {
	runtime   Runtime
	providers []Provider
}

func NewDispatcher(runtime Runtime, providers ...Provider) *Dispatcher {
	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider == nil || strings.TrimSpace(provider.Name()) == "" {
			continue
		}
		filtered = append(filtered, provider)
	}
	return &Dispatcher{runtime: runtime, providers: filtered}
}

func (d *Dispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if d == nil || d.runtime == nil || len(d.providers) == 0 {
		return nil
	}
	approvalPayload, _ := event.ApprovalPayload()
	approvalID := strings.TrimSpace(approvalPayload.ApprovalID)
	if approvalID == "" {
		return nil
	}
	ticket, err := d.runtime.GetApproval(ctx, approvalID)
	if err != nil || ticket == nil {
		return nil
	}
	snapshot := matchingSnapshot(d.runtime, ticket)
	switch event.Type {
	case eventbus.EventApprovalRequested:
		for _, provider := range d.providers {
			submission, err := provider.SubmitApproval(ctx, SubmitRequest{
				Provider: provider.Name(),
				Event:    cloneEvent(event),
				Ticket:   *cloneTicket(ticket),
				Snapshot: snapshot,
			})
			if err != nil {
				log.Warn("approval provider submit failed", "provider", provider.Name(), "approval_id", ticket.ID, "error", err)
				continue
			}
			if ref, ok := submissionExternalRef(provider.Name(), submission); ok {
				if _, err := d.runtime.UpsertApprovalExternalRef(ctx, ticket.ID, ref); err != nil {
					log.Warn("approval provider external ref persist failed", "provider", provider.Name(), "approval_id", ticket.ID, "error", err)
				}
			}
		}
	case eventbus.EventApprovalResolved, eventbus.EventApprovalTimedOut, eventbus.EventApprovalGraceWarning:
		remaining := time.Duration(approvalPayload.RemainingMs) * time.Millisecond
		for _, provider := range d.providers {
			if err := provider.UpdateApproval(ctx, UpdateRequest{
				Provider:  provider.Name(),
				Event:     cloneEvent(event),
				Ticket:    *cloneTicket(ticket),
				Snapshot:  snapshot,
				Remaining: remaining,
			}); err != nil {
				log.Warn("approval provider update failed", "provider", provider.Name(), "approval_id", ticket.ID, "event_type", string(event.Type), "error", err)
			}
		}
	}
	return nil
}

func (d *Dispatcher) SyncPendingApprovals(ctx context.Context) error {
	if d == nil || d.runtime == nil || len(d.providers) == 0 {
		return nil
	}
	pending, err := d.runtime.ListApprovals(ctx, approval.StatusPending)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	snapshot := d.runtime.EffectiveConfigSnapshot()
	for _, provider := range d.providers {
		syncer, ok := provider.(SyncProvider)
		if !ok {
			continue
		}
		results, err := syncer.SyncPendingApprovals(ctx, SyncRequest{
			Provider: provider.Name(),
			Pending:  cloneTickets(pending),
			Snapshot: cloneSnapshot(snapshot),
		})
		if err != nil {
			log.Warn("approval provider sync failed", "provider", provider.Name(), "error", err)
			continue
		}
		for _, result := range results {
			ticketID := strings.TrimSpace(result.TicketID)
			if ticketID == "" {
				continue
			}
			if ref, ok := syncResultExternalRef(provider.Name(), result); ok {
				if _, err := d.runtime.UpsertApprovalExternalRef(ctx, ticketID, ref); err != nil {
					log.Warn("approval provider sync external ref persist failed", "provider", provider.Name(), "ticket_id", ticketID, "error", err)
				}
			}
			resolution, err := normalizeSyncResolution(provider.Name(), result.Resolution)
			if err != nil {
				log.Warn("approval provider sync result invalid", "provider", provider.Name(), "ticket_id", ticketID, "error", err)
				continue
			}
			if _, err := d.runtime.ResolveApproval(ctx, ticketID, resolution); err != nil && !errors.Is(err, approval.ErrAlreadyResolved) {
				log.Warn("approval provider sync resolve failed", "provider", provider.Name(), "ticket_id", ticketID, "error", err)
			}
		}
	}
	return nil
}

func normalizeSyncResolution(provider string, resolution approval.Resolution) (approval.Resolution, error) {
	switch resolution.Status {
	case approval.StatusApproved, approval.StatusDenied, approval.StatusCancelled:
	default:
		return approval.Resolution{}, fmt.Errorf("unsupported sync resolution %q", resolution.Status)
	}
	if strings.TrimSpace(resolution.ResolvedBy) == "" {
		resolution.ResolvedBy = "approval_sync:" + strings.TrimSpace(provider)
	}
	return resolution, nil
}

func matchingSnapshot(runtime Runtime, ticket *approval.Ticket) *controlsnapshot.EffectiveConfigSnapshot {
	if runtime == nil {
		return nil
	}
	snapshot := runtime.EffectiveConfigSnapshot()
	if snapshot == nil {
		return nil
	}
	if ticket == nil || len(ticket.Metadata) == 0 {
		return snapshot.Clone()
	}
	want := strings.TrimSpace(normalize.String(ticket.Metadata["effective_config_snapshot_id"]))
	if want == "" || want == strings.TrimSpace(snapshot.ID) {
		return snapshot.Clone()
	}
	return nil
}

func cloneTickets(items []*approval.Ticket) []approval.Ticket {
	if len(items) == 0 {
		return nil
	}
	out := make([]approval.Ticket, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, *cloneTicket(item))
	}
	return out
}

func cloneTicket(in *approval.Ticket) *approval.Ticket {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.ToolCalls) > 0 {
		out.ToolCalls = make([]approval.ToolCall, len(in.ToolCalls))
		for i, call := range in.ToolCalls {
			out.ToolCalls[i] = approval.ToolCall{
				ID:   call.ID,
				Name: call.Name,
			}
			if len(call.Input) > 0 {
				out.ToolCalls[i].Input = make(map[string]any, len(call.Input))
				for key, value := range call.Input {
					out.ToolCalls[i].Input[key] = value
				}
			}
		}
	}
	if len(in.Reasons) > 0 {
		out.Reasons = append([]string(nil), in.Reasons...)
	}
	if len(in.Metadata) > 0 {
		out.Metadata = make(map[string]any, len(in.Metadata))
		for key, value := range in.Metadata {
			out.Metadata[key] = value
		}
	}
	if len(in.External) > 0 {
		out.External = approval.CloneExternalReferences(in.External)
	}
	return &out
}

func cloneSnapshot(snapshot *controlsnapshot.EffectiveConfigSnapshot) *controlsnapshot.EffectiveConfigSnapshot {
	if snapshot == nil {
		return nil
	}
	return snapshot.Clone()
}

func cloneEvent(event eventbus.Event) eventbus.Event {
	out := event
	if len(event.Attrs) > 0 {
		out.Attrs = make(map[string]any, len(event.Attrs))
		for key, value := range event.Attrs {
			out.Attrs[key] = value
		}
	}
	return out
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case string:
		var out int64
		_, _ = fmt.Sscan(strings.TrimSpace(typed), &out)
		return out
	default:
		return 0
	}
}

func submissionExternalRef(provider string, submission *Submission) (approval.ExternalReference, bool) {
	if submission == nil {
		return approval.ExternalReference{}, false
	}
	ref := approval.ExternalReference{
		Provider:   strings.TrimSpace(provider),
		ExternalID: strings.TrimSpace(submission.ExternalID),
		URL:        strings.TrimSpace(submission.URL),
		Status:     strings.TrimSpace(submission.Status),
		SyncedAt:   time.Now().UTC(),
	}
	if len(submission.Metadata) > 0 {
		ref.Metadata = supportmaps.Clone(submission.Metadata)
	}
	if ref.Provider == "" {
		return approval.ExternalReference{}, false
	}
	if ref.Status == "" && (ref.ExternalID != "" || ref.URL != "" || len(ref.Metadata) > 0) {
		ref.Status = "submitted"
	}
	if ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && len(ref.Metadata) == 0 {
		return approval.ExternalReference{}, false
	}
	return ref, true
}

func syncResultExternalRef(provider string, result SyncResult) (approval.ExternalReference, bool) {
	ref := approval.ExternalReference{
		Provider:   strings.TrimSpace(provider),
		ExternalID: strings.TrimSpace(result.ExternalID),
		URL:        strings.TrimSpace(result.URL),
		Status:     strings.TrimSpace(result.ExternalStatus),
		SyncedAt:   time.Now().UTC(),
	}
	if len(result.Metadata) > 0 {
		ref.Metadata = supportmaps.Clone(result.Metadata)
	}
	if ref.Provider == "" {
		return approval.ExternalReference{}, false
	}
	if ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && len(ref.Metadata) == 0 {
		return approval.ExternalReference{}, false
	}
	return ref, true
}
