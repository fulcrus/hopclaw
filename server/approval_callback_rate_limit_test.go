package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/controlplane"
)

func TestApprovalCallbackRateLimitRejectsBurst(t *testing.T) {
	t.Parallel()

	handler := New(newRuntimeService(t, runtimeFixture{}), Config{
		ApprovalCallbacks: map[string]controlplane.ApprovalCallbackAuthPolicy{
			"jira": {HeaderName: "X-HopClaw-Approval-Token", Token: "jira-secret"},
		},
		ApprovalCallbackRateLimit: ApprovalCallbackRateLimitConfig{
			RequestsPerSecond: 0.001,
			BurstSize:         1,
		},
	}).Handler()

	body, err := json.Marshal(controlplane.ApprovalResolveCallbackRequest{
		Provider: "jira",
		TicketID: "appr-1",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	first := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	first.RemoteAddr = "203.0.113.10:1234"
	first.Header.Set("Content-Type", "application/json")
	first.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	if firstRec.Code == http.StatusTooManyRequests {
		t.Fatalf("first callback unexpectedly rate limited: %s", firstRec.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/runtime/approvals/callbacks/resolve", bytes.NewReader(body))
	second.RemoteAddr = "203.0.113.10:1234"
	second.Header.Set("Content-Type", "application/json")
	second.Header.Set("X-HopClaw-Approval-Token", "jira-secret")
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second callback status = %d body=%s", secondRec.Code, secondRec.Body.String())
	}
}
