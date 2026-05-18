package governanceadapter

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
)

func TestProjectApprovalRequestedIncludesApprovalKind(t *testing.T) {
	t.Parallel()

	record, ok := Project(eventbus.Event{
		ID:        "evt-1",
		Type:      eventbus.EventApprovalRequested,
		RunID:     "run-1",
		SessionID: "sess-1",
		Time:      time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC),
		Attrs: map[string]any{
			"approval_id":   "appr-1",
			"approval_kind": "tool_calls",
			"status":        "pending",
			"approval_external_refs": []map[string]any{{
				"provider":    "jira",
				"external_id": "JIRA-123",
				"status":      "pending_remote",
				"synced_at":   "2026-03-19T10:00:00Z",
			}},
			"policy_action":                 "require_approval",
			"policy_source":                 "policy.overlay/test",
			"policy_summary":                "approval required",
			"policy_layers":                 []string{"base", "overlay"},
			"policy_tool_names":             []string{"fs.write"},
			"policy_approval_default_scope": "once",
			"policy_approval_max_scope":     "session",
			"scope": map[string]any{
				"automation_id": "auto-1",
			},
			"effective_config_snapshot_id": "ecs-1",
		},
	})
	if !ok {
		t.Fatal("Project() reported unsupported event")
	}
	if record.Kind != KindApprovalRequested {
		t.Fatalf("Kind = %q", record.Kind)
	}
	if record.Governance.Approval == nil || record.Governance.Approval.Kind != "tool_calls" {
		t.Fatalf("Approval = %#v", record.Governance.Approval)
	}
	if len(record.Governance.Approval.External) != 1 || record.Governance.Approval.External[0].Provider != "jira" {
		t.Fatalf("Approval.External = %#v", record.Governance.Approval.External)
	}
	if !strings.Contains(record.Governance.Summary, "providers=jira") {
		t.Fatalf("Summary = %q", record.Governance.Summary)
	}
	if record.Governance.Policy == nil || !reflect.DeepEqual(record.Governance.Policy.PolicyLayers, []string{"base", "overlay"}) {
		t.Fatalf("Policy = %#v", record.Governance.Policy)
	}
	if record.Governance.Scope.AutomationID != "auto-1" {
		t.Fatalf("Scope = %#v", record.Governance.Scope)
	}
}

func TestDispatcherFailOpenAndSnapshotEnrichment(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var records []Record
	dispatcher := NewDispatcher(
		AdapterFunc(func(context.Context, Record) error {
			return errors.New("boom")
		}),
		AdapterFunc(func(_ context.Context, record Record) error {
			mu.Lock()
			defer mu.Unlock()
			records = append(records, record)
			return nil
		}),
	).WithSnapshotResolver(staticSnapshotResolver{
		snapshot: &controlsnapshot.EffectiveConfigSnapshot{
			ID:              "ecs-2",
			PolicyProfileID: "business-production",
			PolicyPackIDs:   []string{"base-core", "business-default", "production-default"},
		},
	})

	err := dispatcher.Handle(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Attrs: map[string]any{
			"severity":                     "high",
			"summary":                      "risky command detected",
			"effective_config_snapshot_id": "ecs-2",
			"scope": map[string]any{
				"automation_id": "auto-2",
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil fail-open", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Snapshot == nil || records[0].Snapshot.PolicyProfileID != "business-production" {
		t.Fatalf("Snapshot = %#v", records[0].Snapshot)
	}
	if !reflect.DeepEqual(records[0].Snapshot.PolicyPackIDs, []string{"base-core", "business-default", "production-default"}) {
		t.Fatalf("Snapshot.PolicyPackIDs = %#v", records[0].Snapshot.PolicyPackIDs)
	}
}

type staticSnapshotResolver struct {
	snapshot *controlsnapshot.EffectiveConfigSnapshot
}

func (r staticSnapshotResolver) EffectiveConfigSnapshot() *controlsnapshot.EffectiveConfigSnapshot {
	if r.snapshot == nil {
		return nil
	}
	return r.snapshot.Clone()
}
