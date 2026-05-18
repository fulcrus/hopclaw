package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
)

var ErrApprovalSyncerNil = errors.New("approval syncer is not configured")

type ApprovalSyncer interface {
	SyncPendingApprovals(ctx context.Context) error
}

func (s *Service) WithApprovalSyncer(syncer ApprovalSyncer) *Service {
	s.approvalSync = syncer
	return s
}

func (s *Service) SyncPendingApprovals(ctx context.Context) error {
	if s == nil || s.approvalSync == nil {
		return ErrApprovalSyncerNil
	}
	return s.approvalSync.SyncPendingApprovals(ctx)
}

func (s *Service) ResolveApprovalCallback(ctx context.Context, req controlplane.ApprovalResolveCallbackRequest) (*ApprovalView, error) {
	resolution, err := req.NormalizedResolution()
	if err != nil {
		return nil, err
	}
	ticketID, err := s.resolveCallbackTarget(ctx, req)
	if err != nil {
		return nil, err
	}
	if ref, ok := req.ExternalReference(); ok {
		if _, err := s.UpsertApprovalExternalRef(ctx, ticketID, ref); err != nil {
			return nil, err
		}
	}
	ticket, err := s.ResolveApproval(ctx, ticketID, resolution)
	if err != nil {
		if errors.Is(err, approval.ErrAlreadyResolved) {
			current, getErr := s.GetApproval(ctx, ticketID)
			if getErr == nil {
				return s.approvalView(ctx, current)
			}
		}
		return nil, err
	}
	return s.approvalView(ctx, ticket)
}

func (s *Service) resolveCallbackTarget(ctx context.Context, req controlplane.ApprovalResolveCallbackRequest) (string, error) {
	if ticketID, _, _ := req.Target(); strings.TrimSpace(ticketID) != "" {
		return strings.TrimSpace(ticketID), nil
	}
	_, provider, externalID := req.Target()
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(externalID) == "" {
		return "", fmt.Errorf("ticket_id or provider+external_id is required")
	}
	ticket, err := s.GetApprovalByExternal(ctx, provider, externalID)
	if err != nil {
		return "", err
	}
	if ticket == nil || strings.TrimSpace(ticket.ID) == "" {
		return "", fmt.Errorf("approval callback target not found")
	}
	return strings.TrimSpace(ticket.ID), nil
}
