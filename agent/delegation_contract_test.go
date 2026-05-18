package agent

import (
	"strings"
	"testing"
)

func TestBuildDelegationContractForPlannedDevelopmentTask(t *testing.T) {
	t.Parallel()

	contract := buildDelegationContract("并行分析仓库并修复失败测试", ExecutionModePlanned, nil, &TaskContract{
		Goal:             "修复失败测试",
		JobType:          taskContractJobDevelopment,
		SuggestedDomains: []string{string(DomainFS), string(DomainExec)},
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSummary, Required: true},
		},
		AcceptanceCriteria: []TaskContractAcceptance{
			{ID: taskAcceptanceVisibleResult, Summary: "Visible result", Required: true},
		},
	})
	if contract == nil {
		t.Fatal("buildDelegationContract() returned nil")
	}
	if contract.SideEffectClass != "local_write" {
		t.Fatalf("SideEffectClass = %q, want local_write", contract.SideEffectClass)
	}
	if contract.MaxTurns < delegationDefaultMaxTurns {
		t.Fatalf("MaxTurns = %d, want at least %d", contract.MaxTurns, delegationDefaultMaxTurns)
	}
	if contract.MaxBudgetTokens < delegationDefaultBudgetTokens {
		t.Fatalf("MaxBudgetTokens = %d, want at least %d", contract.MaxBudgetTokens, delegationDefaultBudgetTokens)
	}
	for _, want := range []string{string(DomainFS), string(DomainExec), string(DomainText), string(DomainGit)} {
		if !containsDelegationString(contract.AllowedDomains, want) {
			t.Fatalf("AllowedDomains = %#v, want %q", contract.AllowedDomains, want)
		}
	}
	if contract.VerificationPlanRef == "" || !strings.Contains(contract.VerificationPlanRef, taskAcceptanceVisibleResult) {
		t.Fatalf("VerificationPlanRef = %q, want acceptance criteria reference", contract.VerificationPlanRef)
	}
}

func TestBuildDelegationContractSkipsWhenRequiredInfoMissing(t *testing.T) {
	t.Parallel()

	contract := buildDelegationContract("并行发出报告", ExecutionModePlanned, nil, &TaskContract{
		Goal:    "发送报告",
		JobType: taskContractJobDelivery,
		MissingInfo: []TaskContractMissingInfo{
			{ID: taskMissingInfoDeliveryTarget, Summary: "Need recipient", Required: true},
		},
	})
	if contract != nil {
		t.Fatalf("buildDelegationContract() = %#v, want nil while required info is missing", contract)
	}
}

func TestBuildDelegationContractDoesNotInferDelegationFromKeywordsAlone(t *testing.T) {
	t.Parallel()

	contract := buildDelegationContract("please delegate this in parallel", ExecutionModeDirect, nil, &TaskContract{
		Goal:    "Summarize the document",
		JobType: taskContractJobGeneral,
	})
	if contract != nil {
		t.Fatalf("buildDelegationContract() = %#v, want nil when only keyword hints imply delegation", contract)
	}
}

func TestBuildDelegationContractPromptIncludesAllowedChildTools(t *testing.T) {
	t.Parallel()

	prompt := buildDelegationContractPrompt(&Run{
		Delegation: &DelegationContract{
			Goal:                "Split repo inspection",
			AllowedDomains:      []string{string(DomainFS), string(DomainText)},
			SideEffectClass:     "local_write",
			MaxTurns:            4,
			MaxBudgetTokens:     4000,
			VerificationPlanRef: "task_contract:visible_result",
		},
	}, []ToolDefinition{
		{Name: "agent.spawn", SideEffectClass: "local_write"},
		{Name: "fs.read", SideEffectClass: "read"},
		{Name: "fs.write", SideEffectClass: "local_write"},
		{Name: "text.extract", SideEffectClass: "read"},
		{Name: "net.fetch", SideEffectClass: "read"},
	})

	if !strings.Contains(prompt, "<delegation_contract>") {
		t.Fatalf("prompt = %q, want delegation block", prompt)
	}
	if !strings.Contains(prompt, "allowed child tools:") {
		t.Fatalf("prompt = %q, want allowed child tools line", prompt)
	}
	for _, want := range []string{"fs.read", "fs.write", "text.extract"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt = %q, want %q", prompt, want)
		}
	}
	if strings.Contains(prompt, "net.fetch") {
		t.Fatalf("prompt = %q, unexpected out-of-contract tool", prompt)
	}
}

func TestFilterToolDefinitionsForRunHidesAgentToolsWithoutDelegation(t *testing.T) {
	t.Parallel()

	defs := []ToolDefinition{
		{Name: "agent.spawn"},
		{Name: "fs.read"},
	}

	filtered := filterToolDefinitionsForRun(defs, &Run{})
	if len(filtered) != 1 || filtered[0].Name != "fs.read" {
		t.Fatalf("filtered without delegation = %#v", filtered)
	}

	filtered = filterToolDefinitionsForRun(defs, &Run{
		Delegation: &DelegationContract{AllowedDomains: []string{string(DomainFS)}},
	})
	if len(filtered) != 2 {
		t.Fatalf("filtered with delegation = %#v, want both tools", filtered)
	}
}

func containsDelegationString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
