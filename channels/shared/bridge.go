package shared

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type BridgeRuntime interface {
	channels.InteractableRuntime
	Submit(ctx context.Context, req runtimesvc.SubmitRequest) (*agent.Run, error)
	GetRun(ctx context.Context, id string) (*agent.Run, error)
	GetApproval(ctx context.Context, id string) (*approval.Ticket, error)
	FindPendingApproval(ctx context.Context, sessionID string) (*approval.Ticket, error)
	ResolveApproval(ctx context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error)
	CancelRun(ctx context.Context, runID string) (*agent.Run, error)
	GetArtifact(ctx context.Context, id string) (*artifact.Blob, error)
}

type AutoApproveNotifier interface {
	NotifyAutoApproved(ctx context.Context, runID string) bool
}

func StartBridgeLoops(
	ctx context.Context,
	adapter channels.Adapter,
	bus *eventbus.InMemoryBus,
	onInbound func(context.Context, channels.InboundMessage),
	onOutbound func(context.Context, *eventbus.Subscription),
) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)

	if inbound := adapter.SubscribeEvents(); inbound != nil && onInbound != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-inbound:
					if !ok {
						return
					}
					onInbound(ctx, msg)
				}
			}
		}()
	}
	if bus != nil && onOutbound != nil {
		sub := bus.SubscribeChannel(128)
		go onOutbound(ctx, sub)
	}
	return cancel
}

func ApprovalForEvent(ctx context.Context, runtime BridgeRuntime, event eventbus.Event) (*approval.Ticket, error) {
	if approvalID := approvalIDFromAttrs(event.Attrs); approvalID != "" {
		return runtime.GetApproval(ctx, approvalID)
	}
	run, err := runtime.GetRun(ctx, event.RunID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.ApprovalID) == "" {
		return nil, fmt.Errorf("approval id is required")
	}
	return runtime.GetApproval(ctx, run.ApprovalID)
}

func TryAutoApproveSkillInstall(
	ctx context.Context,
	runtime BridgeRuntime,
	sessions agent.SessionStore,
	status AutoApproveNotifier,
	event eventbus.Event,
) (bool, error) {
	if runtime == nil || sessions == nil || status == nil || event.RunID == "" || event.SessionID == "" {
		return false, nil
	}
	session, err := agent.LoadSession(ctx, sessions, event.SessionID, agent.ScopeFilter{})
	if err != nil || !channels.SessionAutoApproveSession(session) {
		return false, nil
	}
	ticket, err := ApprovalForEvent(ctx, runtime, event)
	if err != nil || ticket == nil || ticket.Status != approval.StatusPending {
		return false, nil
	}
	if _, err := runtime.ResolveApproval(ctx, ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "channel_auto",
		Note:       "auto-approved skill install for session",
	}); err != nil {
		return false, err
	}
	_ = status.NotifyAutoApproved(ctx, event.RunID)
	return true, nil
}

func approvalIDFromAttrs(attrs map[string]any) string {
	for _, key := range []string{meta.KeyApprovalID, "approval_id"} {
		if approvalID, _ := attrs[key].(string); strings.TrimSpace(approvalID) != "" {
			return strings.TrimSpace(approvalID)
		}
	}
	return ""
}
