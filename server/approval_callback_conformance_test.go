package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/policy"
	rt "github.com/fulcrus/hopclaw/runtime"
)

func TestApprovalCallbackConformanceResolvesByTicketIDWithTokenAuth(t *testing.T) {
	t.Parallel()

	handler, _ := newApprovalCallbackConformanceHandler(t, controlplane.ApprovalCallbackAuthPolicy{
		HeaderName: "X-HopClaw-Approval-Token",
		Token:      "jira-secret",
	})
	ticket := waitForPendingApprovalCallbackTicket(t, handler, "approval-callback-conformance-ticket")

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: ticket.ID,
		Status:   "approved",
		Scope:    "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.ID != ticket.ID {
		t.Fatalf("view.ID = %q, want %q", view.ID, ticket.ID)
	}
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q, want %q", view.Status, approval.StatusApproved)
	}
	if view.ResolvedBy != "provider:jira" {
		t.Fatalf("view.ResolvedBy = %q, want provider:jira", view.ResolvedBy)
	}
	if len(view.External) != 1 || view.External[0].Provider != "jira" || view.External[0].Status != "approved" {
		t.Fatalf("view.External = %#v", view.External)
	}
}

func TestApprovalCallbackConformanceResolvesByExternalIDWithTokenAuth(t *testing.T) {
	t.Parallel()

	handler, store := newApprovalCallbackConformanceHandler(t, controlplane.ApprovalCallbackAuthPolicy{
		HeaderName: "X-HopClaw-Approval-Token",
		Token:      "jira-secret",
	})
	ticket := waitForPendingApprovalCallbackTicket(t, handler, "approval-callback-conformance-external")
	if _, err := store.UpsertExternalRef(context.Background(), ticket.ID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "jira-123",
		Status:     "pending_remote",
		SyncedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider:   "jira",
		ExternalID: "jira-123",
		Status:     "approved",
		Scope:      "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.ID != ticket.ID {
		t.Fatalf("view.ID = %q, want %q", view.ID, ticket.ID)
	}
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q, want %q", view.Status, approval.StatusApproved)
	}
}

func TestApprovalCallbackConformanceSupportsHMACAuth(t *testing.T) {
	t.Parallel()

	handler, _ := newApprovalCallbackConformanceHandler(t, controlplane.ApprovalCallbackAuthPolicy{
		Mode:            "hmac",
		Secret:          "jira-hmac-secret",
		SignatureHeader: "X-HopClaw-Signature",
		TimestampHeader: "X-HopClaw-Timestamp",
		MaxAge:          5 * time.Minute,
	})
	ticket := waitForPendingApprovalCallbackTicket(t, handler, "approval-callback-conformance-hmac")

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: ticket.ID,
		Status:   "approved",
		Scope:    "session",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := "sha256=" + computeApprovalCallbackHMAC("jira-hmac-secret", timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HopClaw-Timestamp", timestamp)
	req.Header.Set("X-HopClaw-Signature", signature)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var view rt.ApprovalView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if view.Status != approval.StatusApproved {
		t.Fatalf("view.Status = %q, want %q", view.Status, approval.StatusApproved)
	}
}

func TestApprovalCallbackConformanceRejectsMissingProviderToken(t *testing.T) {
	t.Parallel()

	handler, _ := newApprovalCallbackConformanceHandler(t, controlplane.ApprovalCallbackAuthPolicy{
		HeaderName: "X-HopClaw-Approval-Token",
		Token:      "jira-secret",
	})

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: "appr-1",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST callback status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func newApprovalCallbackConformanceHandler(t *testing.T, auth controlplane.ApprovalCallbackAuthPolicy) (http.Handler, approval.Store) {
	t.Helper()

	store := approval.NewInMemoryStore()
	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-approval-callback-conformance",
					Name: "fs.write",
				}},
			}},
		},
		tools: serverToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-approval-callback-conformance",
				Content:    "written",
			}},
		},
		policy: policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		}),
		approvals: store,
		bus:       eventbus.NewInMemoryBus(),
		skills:    newSkillService(t, "fs.write", "local_write"),
	})
	handler := New(svc, Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": auth,
		},
	}).Handler()
	return handler, store
}

func waitForPendingApprovalCallbackTicket(t *testing.T, handler http.Handler, sessionKey string) *rt.ApprovalView {
	t.Helper()

	run := postRun(t, handler, map[string]any{
		"session_key": sessionKey,
		"content":     "write file",
	}, http.StatusAccepted)
	waitForRunStatus(t, handler, run.ID, agent.RunWaitingApproval)

	tickets := listApprovals(t, handler, "pending")
	if len(tickets) != 1 {
		t.Fatalf("len(tickets) = %d, want 1", len(tickets))
	}
	return tickets[0]
}
