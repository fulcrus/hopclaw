package channels

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

func (b *Bridge) tryAutoApproveSkillInstall(ctx context.Context, event eventbus.Event) bool {
	if b == nil || b.runtime == nil || b.status == nil || event.RunID == "" || event.SessionID == "" {
		return false
	}
	session, err := agent.LoadSession(ctx, b.sessions, event.SessionID, agent.ScopeFilter{})
	if err != nil || !SessionAutoApproveSession(session) {
		return false
	}
	ticket, err := b.approvalForEvent(ctx, event)
	if err != nil || ticket == nil || ticket.Status != approval.StatusPending {
		return false
	}
	if _, err := b.runtime.ResolveApproval(ctx, ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "channel_auto",
		Note:       "auto-approved skill install for session",
	}); err != nil {
		log.Error("bridge: auto-approve skill install", "channel", b.cfg.ChannelName, "error", err, "approval_id", ticket.ID)
		return false
	}
	b.status.NotifyAutoApproved(ctx, event.RunID)
	return true
}

func (b *Bridge) approvalForEvent(ctx context.Context, event eventbus.Event) (*approval.Ticket, error) {
	if payload, ok := event.ApprovalPayload(); ok && strings.TrimSpace(payload.ApprovalID) != "" {
		return b.runtime.GetApproval(ctx, payload.ApprovalID)
	}
	run, err := b.runtime.GetRun(ctx, event.RunID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.ApprovalID) == "" {
		return nil, fmt.Errorf("approval id is required")
	}
	return b.runtime.GetApproval(ctx, run.ApprovalID)
}
