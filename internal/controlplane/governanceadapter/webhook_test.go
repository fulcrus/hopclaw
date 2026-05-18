package governanceadapter

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

	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/internal/controlplane/webhookclient"
)

func TestWebhookAdapterPostsGovernanceRecord(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if got := strings.TrimSpace(r.Header.Get(webhookAdapterHeader)); got != "audit-hub" {
			t.Fatalf("adapter header = %q", got)
		}
		if got := strings.TrimSpace(r.Header.Get(webhookKindHeader)); got != string(KindSecurityEvent) {
			t.Fatalf("kind header = %q", got)
		}
		if got := strings.TrimSpace(r.Header.Get(webhookEventHeader)); got != string(eventbus.EventSecurityRiskDetected) {
			t.Fatalf("event header = %q", got)
		}
		timestamp := strings.TrimSpace(r.Header.Get(webhookclient.TimestampHeader))
		signature := strings.TrimSpace(r.Header.Get(webhookclient.SignatureHeader))
		if timestamp == "" || signature == "" {
			t.Fatalf("missing signature headers ts=%q sig=%q", timestamp, signature)
		}
		wantSig := "sha256=" + webhookclient.ComputeHMAC("gov-secret", timestamp, body)
		if signature != wantSig {
			t.Fatalf("signature = %q, want %q", signature, wantSig)
		}
		var payload WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Adapter != "audit-hub" {
			t.Fatalf("payload.Adapter = %q", payload.Adapter)
		}
		if payload.Record.Kind != KindSecurityEvent || payload.Record.EventType != eventbus.EventSecurityRiskDetected {
			t.Fatalf("payload.Record = %#v", payload.Record)
		}
		if payload.Record.Snapshot == nil || payload.Record.Snapshot.ID != "ecs-gov-1" {
			t.Fatalf("payload.Record.Snapshot = %#v", payload.Record.Snapshot)
		}
		if payload.Metadata["tenant_mode"] != "b2b" {
			t.Fatalf("payload.Metadata = %#v", payload.Metadata)
		}
		mu.Lock()
		seen = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter, err := NewWebhookAdapter(WebhookAdapterConfig{
		Name:            "audit-hub",
		URL:             server.URL,
		Secret:          "gov-secret",
		Timeout:         5 * time.Second,
		IncludeSnapshot: true,
		Kinds:           []Kind{KindSecurityEvent},
		Metadata: map[string]any{
			"tenant_mode": "b2b",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookAdapter() error = %v", err)
	}

	if err := adapter.HandleGovernanceRecord(context.Background(), Record{
		Kind:      KindSecurityEvent,
		EventType: eventbus.EventSecurityRiskDetected,
		RunID:     "run-1",
		Snapshot: &controlsnapshot.EffectiveConfigSnapshot{
			ID: "ecs-gov-1",
		},
	}); err != nil {
		t.Fatalf("HandleGovernanceRecord() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("expected webhook adapter request")
	}
}

func TestWebhookAdapterFiltersKindsAndCanOmitSnapshot(t *testing.T) {
	t.Parallel()

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		var payload WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Record.Snapshot != nil {
			t.Fatalf("payload.Record.Snapshot = %#v, want nil", payload.Record.Snapshot)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter, err := NewWebhookAdapter(WebhookAdapterConfig{
		Name:            "audit-hub",
		URL:             server.URL,
		Timeout:         5 * time.Second,
		IncludeSnapshot: false,
		Kinds:           []Kind{KindApprovalResolved},
	})
	if err != nil {
		t.Fatalf("NewWebhookAdapter() error = %v", err)
	}

	if err := adapter.HandleGovernanceRecord(context.Background(), Record{
		Kind:      KindSecurityEvent,
		EventType: eventbus.EventSecurityRiskDetected,
	}); err != nil {
		t.Fatalf("HandleGovernanceRecord(security) error = %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0 before allowed kind", requestCount)
	}
	if err := adapter.HandleGovernanceRecord(context.Background(), Record{
		Kind:      KindApprovalResolved,
		EventType: eventbus.EventApprovalResolved,
		Snapshot: &controlsnapshot.EffectiveConfigSnapshot{
			ID: "ecs-gov-2",
		},
	}); err != nil {
		t.Fatalf("HandleGovernanceRecord(approval) error = %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}
}
