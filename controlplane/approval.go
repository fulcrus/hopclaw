package controlplane

import (
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
)

type ApprovalResolveCallbackRequest struct {
	Provider       string         `json:"provider,omitempty"`
	TicketID       string         `json:"ticket_id,omitempty"`
	ExternalID     string         `json:"external_id,omitempty"`
	ExternalURL    string         `json:"external_url,omitempty"`
	ExternalStatus string         `json:"external_status,omitempty"`
	ExternalMeta   map[string]any `json:"external_metadata,omitempty"`
	Status         string         `json:"status,omitempty"`
	Decision       string         `json:"decision,omitempty"`
	Scope          string         `json:"scope,omitempty"`
	Note           string         `json:"note,omitempty"`
	ResolvedBy     string         `json:"resolved_by,omitempty"`
}

func (r ApprovalResolveCallbackRequest) Resolution() (string, approval.Resolution, error) {
	ticketID := strings.TrimSpace(r.TicketID)
	if ticketID == "" {
		return "", approval.Resolution{}, fmt.Errorf("ticket_id is required")
	}
	resolution, err := r.NormalizedResolution()
	if err != nil {
		return "", approval.Resolution{}, err
	}
	return ticketID, resolution, nil
}

func (r ApprovalResolveCallbackRequest) NormalizedResolution() (approval.Resolution, error) {
	status := approval.Status(strings.TrimSpace(r.Status))
	if status == "" {
		switch strings.ToLower(strings.TrimSpace(r.Decision)) {
		case "approve", "approved":
			status = approval.StatusApproved
		case "deny", "denied":
			status = approval.StatusDenied
		case "cancel", "cancelled":
			status = approval.StatusCancelled
		}
	}
	switch status {
	case approval.StatusApproved, approval.StatusDenied, approval.StatusCancelled:
	default:
		return approval.Resolution{}, fmt.Errorf("status must be approved, denied, or cancelled")
	}
	resolvedBy := strings.TrimSpace(r.ResolvedBy)
	if resolvedBy == "" {
		if provider := strings.TrimSpace(r.Provider); provider != "" {
			resolvedBy = "provider:" + provider
		} else {
			resolvedBy = "provider_callback"
		}
	}
	return approval.Resolution{
		Status:     status,
		ResolvedBy: resolvedBy,
		Note:       strings.TrimSpace(r.Note),
		Scope:      approval.Scope(strings.TrimSpace(r.Scope)),
	}, nil
}

func (r ApprovalResolveCallbackRequest) ExternalReference() (approval.ExternalReference, bool) {
	provider := strings.TrimSpace(r.Provider)
	if provider == "" {
		return approval.ExternalReference{}, false
	}
	ref := approval.ExternalReference{
		Provider:   provider,
		ExternalID: strings.TrimSpace(r.ExternalID),
		URL:        strings.TrimSpace(r.ExternalURL),
		Status:     strings.TrimSpace(r.ExternalStatus),
		SyncedAt:   time.Now().UTC(),
	}
	if ref.Status == "" {
		ref.Status = strings.TrimSpace(r.Status)
	}
	if ref.Status == "" {
		ref.Status = strings.TrimSpace(r.Decision)
	}
	if len(r.ExternalMeta) > 0 {
		ref.Metadata = cloneMap(r.ExternalMeta)
	}
	if ref.ExternalID == "" && ref.URL == "" && ref.Status == "" && len(ref.Metadata) == 0 {
		return approval.ExternalReference{}, false
	}
	return ref, true
}

func (r ApprovalResolveCallbackRequest) Target() (ticketID string, provider string, externalID string) {
	return strings.TrimSpace(r.TicketID), strings.TrimSpace(r.Provider), strings.TrimSpace(r.ExternalID)
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
