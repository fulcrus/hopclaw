package controlplane

import (
	"testing"

	"github.com/fulcrus/hopclaw/approval"
)

func TestApprovalResolveCallbackRequestNormalizesResolution(t *testing.T) {
	t.Parallel()

	req := ApprovalResolveCallbackRequest{
		Provider: "jira",
		Decision: "approved",
		Scope:    "session",
		Note:     " shipped ",
	}

	resolution, err := req.NormalizedResolution()
	if err != nil {
		t.Fatalf("NormalizedResolution() error = %v", err)
	}
	if resolution.Status != approval.StatusApproved {
		t.Fatalf("Status = %q, want approved", resolution.Status)
	}
	if resolution.ResolvedBy != "provider:jira" {
		t.Fatalf("ResolvedBy = %q, want provider:jira", resolution.ResolvedBy)
	}
	if resolution.Scope != approval.ScopeSession {
		t.Fatalf("Scope = %q, want session", resolution.Scope)
	}
	if resolution.Note != "shipped" {
		t.Fatalf("Note = %q, want shipped", resolution.Note)
	}
}

func TestApprovalResolveCallbackRequestExternalReference(t *testing.T) {
	t.Parallel()

	req := ApprovalResolveCallbackRequest{
		Provider:       "jira",
		ExternalID:     "ext-1",
		ExternalURL:    "https://example.com/ext-1",
		ExternalStatus: "approved_remote",
		ExternalMeta: map[string]any{
			"source": "callback",
		},
	}

	ref, ok := req.ExternalReference()
	if !ok {
		t.Fatal("ExternalReference() ok = false, want true")
	}
	if ref.Provider != "jira" || ref.ExternalID != "ext-1" || ref.Status != "approved_remote" {
		t.Fatalf("ref = %#v", ref)
	}
	if got := ref.Metadata["source"]; got != "callback" {
		t.Fatalf("Metadata[source] = %#v, want callback", got)
	}
}
