package governance

import (
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

func TestMetadataAttrsSkipsNilValuesAndNormalizesLegacyToolNames(t *testing.T) {
	t.Parallel()

	attrs := MetadataAttrs(map[string]any{
		"effective_config_snapshot_id": nil,
		"policy_action":                nil,
		"policy_source":                " policy.test/base ",
		"policy_summary":               nil,
		"tool_names":                   []any{" fs.write ", nil, "fs.write", " "},
		"scope": domainscope.Ref{
			AutomationID: "auto-1",
		},
	})

	if got, ok := attrs["effective_config_snapshot_id"]; ok {
		t.Fatalf("effective_config_snapshot_id = %#v, want omitted", got)
	}
	if got, ok := attrs["policy_action"]; ok {
		t.Fatalf("policy_action = %#v, want omitted", got)
	}
	if got := attrs["policy_source"]; got != "policy.test/base" {
		t.Fatalf("policy_source = %#v", got)
	}
	if got := attrs["policy_tool_names"]; !reflect.DeepEqual(got, []string{"fs.write"}) {
		t.Fatalf("policy_tool_names = %#v", got)
	}
	if got := attrs["scope"]; got != (domainscope.Ref{AutomationID: "auto-1"}) {
		t.Fatalf("scope = %#v", got)
	}
}

func TestEventContextFromEventAppliesApprovalDefaultsConsistently(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		eventType  eventbus.EventType
		wantStatus approval.Status
	}{
		{name: "approval_requested", eventType: eventbus.EventApprovalRequested, wantStatus: approval.StatusPending},
		{name: "run_waiting_approval", eventType: eventbus.EventRunWaitingApproval, wantStatus: approval.StatusPending},
		{name: "approval_timed_out", eventType: eventbus.EventApprovalTimedOut, wantStatus: approval.StatusCancelled},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			context := EventContextFromEvent(tc.eventType, map[string]any{
				"approval_kind": "tool_calls",
			})
			if context.Approval == nil {
				t.Fatal("expected approval context")
			}
			if context.Approval.Kind != approval.KindToolCalls {
				t.Fatalf("approval.Kind = %q", context.Approval.Kind)
			}
			if context.Approval.Status != tc.wantStatus {
				t.Fatalf("approval.Status = %q, want %q", context.Approval.Status, tc.wantStatus)
			}
		})
	}
}
