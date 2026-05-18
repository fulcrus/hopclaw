package approvalflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/controlplane/webhookclient"
)

func TestWebhookProviderSubmitUpdateAndSync(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var submitSeen, updateSeen, syncSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAllString(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := strings.TrimSpace(r.Header.Get(webhookProviderHeader)); got != "jira" {
			t.Fatalf("provider header = %q", got)
		}
		timestamp := strings.TrimSpace(r.Header.Get(webhookTimestampHeader))
		signature := strings.TrimSpace(r.Header.Get(webhookSignatureHeader))
		if timestamp == "" || signature == "" {
			t.Fatalf("missing signature headers: ts=%q sig=%q", timestamp, signature)
		}
		wantSig := "sha256=" + webhookclient.ComputeHMAC("outbound-secret", timestamp, []byte(body))
		if signature != wantSig {
			t.Fatalf("signature = %q, want %q", signature, wantSig)
		}
		switch r.Header.Get(webhookOperationHeader) {
		case "submit":
			mu.Lock()
			submitSeen = true
			mu.Unlock()
			var req SubmitRequest
			if err := json.Unmarshal([]byte(body), &req); err != nil {
				t.Fatalf("submit unmarshal: %v", err)
			}
			if req.Provider != "jira" || req.Ticket.ID != "appr-1" {
				t.Fatalf("submit req = %#v", req)
			}
			_ = json.NewEncoder(w).Encode(Submission{
				ExternalID: "jira-100",
				URL:        "https://jira.example/approvals/100",
				Status:     "submitted",
			})
		case "update":
			mu.Lock()
			updateSeen = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case "sync":
			mu.Lock()
			syncSeen = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []SyncResult{{
					TicketID: "appr-1",
					Resolution: approval.Resolution{
						Status: approval.StatusApproved,
					},
					ExternalID:     "jira-100",
					ExternalStatus: "approved_remote",
				}},
			})
		default:
			t.Fatalf("unexpected op %q", r.Header.Get(webhookOperationHeader))
		}
	}))
	defer server.Close()

	provider, err := NewWebhookProvider(WebhookProviderConfig{
		Name:      "jira",
		SubmitURL: server.URL,
		UpdateURL: server.URL,
		SyncURL:   server.URL,
		Secret:    "outbound-secret",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}

	submission, err := provider.SubmitApproval(context.Background(), SubmitRequest{
		Provider: "jira",
		Event:    eventbus.Event{Type: eventbus.EventApprovalRequested},
		Ticket: approval.Ticket{
			ID: "appr-1",
		},
	})
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if submission == nil || submission.ExternalID != "jira-100" {
		t.Fatalf("submission = %#v", submission)
	}
	if err := provider.UpdateApproval(context.Background(), UpdateRequest{
		Provider: "jira",
		Event:    eventbus.Event{Type: eventbus.EventApprovalResolved},
		Ticket: approval.Ticket{
			ID: "appr-1",
		},
	}); err != nil {
		t.Fatalf("UpdateApproval() error = %v", err)
	}
	results, err := provider.SyncPendingApprovals(context.Background(), SyncRequest{
		Provider: "jira",
		Pending:  []approval.Ticket{{ID: "appr-1"}},
	})
	if err != nil {
		t.Fatalf("SyncPendingApprovals() error = %v", err)
	}
	if len(results) != 1 || results[0].ExternalStatus != "approved_remote" {
		t.Fatalf("results = %#v", results)
	}

	mu.Lock()
	defer mu.Unlock()
	if !submitSeen || !updateSeen || !syncSeen {
		t.Fatalf("seen submit=%v update=%v sync=%v", submitSeen, updateSeen, syncSeen)
	}
}

func TestNewWebhookProviderRequiresEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := NewWebhookProvider(WebhookProviderConfig{Name: "jira"}); err == nil {
		t.Fatal("expected error when webhook provider has no endpoints")
	}
}

func ioReadAllString(r *http.Request) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
