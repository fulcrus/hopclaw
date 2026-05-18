package agent

import (
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/policy"
)

func TestBuildRunHarnessSpecCapturesRecoveryRoutingAndApprovalHints(t *testing.T) {
	t.Parallel()

	run := &Run{
		Preflight: &RunPreflightReport{
			Checks:           []RunPreflightCheck{{ID: "expected_confirmation"}},
			SuggestedDomains: []string{string(DomainEmail)},
		},
		TaskContract: &TaskContract{
			RequiresApproval:       true,
			RequiresExternalEffect: true,
			SuggestedDomains:       []string{string(DomainEmail)},
			CapabilityHints:        []string{"email.search"},
		},
	}
	spec := buildRunHarnessSpec(run, nil, "find the recent invoice correspondence", []ToolDefinition{{Name: "skill.ensure"}})

	if spec.Recovery.TransparentIntent == nil || spec.Recovery.TransparentIntent.Key != "email" {
		t.Fatalf("transparent recovery intent = %#v, want email", spec.Recovery.TransparentIntent)
	}
	if !spec.Model.RequireThinking {
		t.Fatal("expected harness to require thinking model")
	}
	if !spec.Approval.NeedsConfirmation {
		t.Fatal("expected harness to require confirmation hint")
	}
	if !spec.Approval.RequiresApproval {
		t.Fatal("expected harness to carry task-contract approval requirement")
	}
	if !spec.Approval.RequiresExternalSide {
		t.Fatal("expected harness to carry external side effect requirement")
	}
	if spec.Recovery.ExtraAttempts != 0 {
		t.Fatalf("extra recovery attempts = %d, want 0 for direct run", spec.Recovery.ExtraAttempts)
	}
}

func TestBuildRunHarnessSpecBudgetCapsExtraRounds(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeWorkflow,
		Preflight: &RunPreflightReport{
			SuggestedDomains: []string{string(DomainNews), string(DomainSearch)},
		},
	}
	task := &planpkg.Task{
		Kind:                 planpkg.TaskExecute,
		Goal:                 "collect the latest updates, compare the top three items, then save a summary to the workspace",
		RequiredCapabilities: []string{"browser", "desktop"},
	}

	spec := buildRunHarnessSpec(run, task, task.Goal, nil)
	if spec.Budget.ExtraToolRounds != 8 {
		t.Fatalf("extra tool rounds = %d, want capped 8", spec.Budget.ExtraToolRounds)
	}
	if spec.Recovery.ExtraAttempts != 2 {
		t.Fatalf("extra recovery attempts = %d, want 2 for workflow run", spec.Recovery.ExtraAttempts)
	}
}

func TestBuildRunHarnessSpecBudgetUsesStructuredSignalsWithoutPromptKeywords(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeDirect,
		Preflight: &RunPreflightReport{
			SuggestedDomains: []string{string(DomainBrowser)},
		},
		TaskContract: &TaskContract{
			JobType:          taskContractJobResearch,
			SuggestedDomains: []string{string(DomainBrowser), string(DomainFS)},
			ExpectedDeliverables: []TaskContractDeliverable{{
				Kind: taskDeliverableBrowserEvidence,
			}},
		},
	}

	spec := buildRunHarnessSpec(run, nil, "collect evidence and save the result", nil)
	if spec.Budget.ExtraToolRounds != 6 {
		t.Fatalf("extra tool rounds = %d, want 6 from structured browser + multi-step + research signals", spec.Budget.ExtraToolRounds)
	}
}

func TestBuildRunHarnessSpecBudgetDoesNotUsePromptHeuristicsWithoutStructuredSignals(t *testing.T) {
	t.Parallel()

	spec := buildRunHarnessSpec(nil, nil, "focus the frontmost window and then take a screenshot", nil)
	if spec.Budget.ExtraToolRounds != 0 {
		t.Fatalf("extra tool rounds = %d, want 0 without structured signals", spec.Budget.ExtraToolRounds)
	}
}

func TestBuildRunHarnessSpecRecordsMissingDomainsForEmptyActivatedTier3Domain(t *testing.T) {
	t.Parallel()

	run := &Run{
		TaskContract: &TaskContract{
			Goal:             "Schedule the leadership sync for next week.",
			SuggestedDomains: []string{string(DomainCalendar)},
		},
	}

	spec := buildRunHarnessSpec(run, nil, "Schedule the leadership sync for next week.", []ToolDefinition{
		{Name: "skill.ensure", SideEffectClass: "read"},
		{Name: "fs.read", SideEffectClass: "read"},
	})
	if len(spec.Recovery.MissingDomains) != 1 || spec.Recovery.MissingDomains[0] != "calendar" {
		t.Fatalf("missing domains = %#v, want [calendar]", spec.Recovery.MissingDomains)
	}
	if spec.Recovery.TransparentIntent == nil || spec.Recovery.TransparentIntent.Key != "missing-domain-calendar" {
		t.Fatalf("transparent recovery intent = %#v, want missing-domain-calendar", spec.Recovery.TransparentIntent)
	}
}

func TestBuildApprovalDetailsIncludesHarnessMetadata(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID: "run-approval-harness",
		Preflight: &RunPreflightReport{
			Checks:           []RunPreflightCheck{{ID: "expected_confirmation"}},
			SuggestedDomains: []string{string(DomainEmail)},
			Blocking:         true,
			GeneratedAt:      time.Now().UTC(),
		},
		TaskContract: &TaskContract{
			RequiresApproval:       true,
			RequiresExternalEffect: true,
			SuggestedDomains:       []string{string(DomainEmail)},
			CapabilityHints:        []string{"email.search"},
		},
	}
	session := &Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:      contextengine.RoleUser,
				Content:   "find the recent invoice correspondence",
				Metadata:  messageMetadataForRun(nil, run.ID),
				CreatedAt: time.Now().UTC(),
			}},
		},
	}
	_, metadata := buildApprovalDetails(run, session, []ToolCall{{Name: "email.search"}}, policy.Decision{
		Action:  policy.ActionRequireApproval,
		Summary: "approval required",
	})

	if got := metadata["harness_transparent_recovery_intent"]; got != "email" {
		t.Fatalf("harness_transparent_recovery_intent = %#v, want email", got)
	}
	if got := metadata["harness_require_thinking"]; got != true {
		t.Fatalf("harness_require_thinking = %#v, want true", got)
	}
	if got := metadata["harness_needs_confirmation"]; got != true {
		t.Fatalf("harness_needs_confirmation = %#v, want true", got)
	}
	if got := metadata["harness_requires_external_side_effect"]; got != true {
		t.Fatalf("harness_requires_external_side_effect = %#v, want true", got)
	}
	if got := metadata["harness_domains"]; got == nil {
		t.Fatal("expected harness_domains metadata to be present")
	}
}
