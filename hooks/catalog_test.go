package hooks

import "testing"

func TestLookupEventSpecKnownTrigger(t *testing.T) {
	t.Parallel()

	spec, ok := LookupEventSpec(TriggerBeforeToolCall)
	if !ok {
		t.Fatal("expected spec for before.tool_call")
	}
	if !spec.CanBlock {
		t.Fatal("expected before.tool_call to be blocking")
	}
	if spec.SupportsAsync {
		t.Fatal("expected before.tool_call to reject async execution")
	}
	if len(spec.AllowedPhases) != 1 || spec.AllowedPhases[0] != HookPhasePre {
		t.Fatalf("unexpected allowed phases: %#v", spec.AllowedPhases)
	}
}

func TestValidateHookDefinitionRejectsUnsupportedPhase(t *testing.T) {
	t.Parallel()

	err := ValidateHookDefinition(Hook{
		Name:    "bad-phase",
		Trigger: TriggerRunCompleted,
		Kind:    KindHTTP,
		URL:     "https://example.com/hook",
		Phase:   HookPhasePre,
	})
	if err == nil {
		t.Fatal("expected invalid phase error")
	}
}

func TestValidateHookDefinitionRejectsAsyncPre(t *testing.T) {
	t.Parallel()

	err := ValidateHookDefinition(Hook{
		Name:    "bad-async",
		Trigger: TriggerBeforeToolCall,
		Kind:    KindCommand,
		Command: "echo test",
		Phase:   HookPhasePre,
		Async:   true,
	})
	if err == nil {
		t.Fatal("expected async pre validation error")
	}
}

func TestLookupEventSpecGovernanceTrigger(t *testing.T) {
	t.Parallel()

	spec, ok := LookupEventSpec(TriggerGovernanceDeliveryDeadLettered)
	if !ok {
		t.Fatal("expected spec for governance.delivery.dead_lettered")
	}
	if spec.Category != EventCategoryGovernance {
		t.Fatalf("spec.Category = %q, want %q", spec.Category, EventCategoryGovernance)
	}
	if spec.CanBlock {
		t.Fatal("expected governance delivery hooks to be non-blocking")
	}
	if !spec.SupportsAsync {
		t.Fatal("expected governance delivery hooks to support async execution")
	}
	if len(spec.AllowedPhases) != 1 || spec.AllowedPhases[0] != HookPhasePost {
		t.Fatalf("unexpected allowed phases: %#v", spec.AllowedPhases)
	}
}

func TestLookupEventSpecApprovalTrigger(t *testing.T) {
	t.Parallel()

	spec, ok := LookupEventSpec(TriggerApprovalResolved)
	if !ok {
		t.Fatal("expected spec for approval.resolved")
	}
	if spec.Category != EventCategoryApproval {
		t.Fatalf("spec.Category = %q, want %q", spec.Category, EventCategoryApproval)
	}
	if spec.CanBlock {
		t.Fatal("expected approval hooks to be non-blocking")
	}
	if !spec.SupportsAsync {
		t.Fatal("expected approval hooks to support async execution")
	}
	if len(spec.AllowedPhases) != 1 || spec.AllowedPhases[0] != HookPhasePost {
		t.Fatalf("unexpected allowed phases: %#v", spec.AllowedPhases)
	}
}
