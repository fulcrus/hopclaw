package verify

import "testing"

func TestEvaluateCompletedRunWithoutEvidenceWarns(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:  "run-no-evidence",
		Status: "completed",
	})

	if result.Status != StatusWarning {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusWarning)
	}
	if result.Checks[0].Severity != SeverityWarning {
		t.Fatalf("result.Checks[0].Severity = %q, want %q", result.Checks[0].Severity, SeverityWarning)
	}
	if result.RequiredWarnings != 1 {
		t.Fatalf("result.RequiredWarnings = %d, want 1", result.RequiredWarnings)
	}
	if result.Summary != "verification finished with 1 required warning" {
		t.Fatalf("result.Summary = %q", result.Summary)
	}
	if len(result.Checks) == 0 || result.Checks[0].Name != "run.outcome" || result.Checks[0].Status != StatusWarning {
		t.Fatalf("unexpected checks: %#v", result.Checks)
	}
}

func TestEvaluateContractExternalEffectMissingFails(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:     "run-contract-fail",
		Status:    "completed",
		Output:    "Reported success to the user",
		ToolNames: []string{"browser.navigate"},
		Contract: &Contract{
			JobType:                "delivery",
			RequiresExternalEffect: true,
			ExpectedDeliverables: []ContractDeliverable{
				{Kind: contractDeliverableBrowserEvidence, Required: true},
			},
		},
	})

	if result.Status != StatusWarning {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusWarning)
	}
	if result.RequiredWarnings != 1 {
		t.Fatalf("result.RequiredWarnings = %d, want 1", result.RequiredWarnings)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name != "task.contract" {
			continue
		}
		found = true
		if check.Status != StatusFailed {
			t.Fatalf("contract check status = %q, want %q", check.Status, StatusFailed)
		}
		if check.Severity != SeverityWarning {
			t.Fatalf("contract check severity = %q, want %q", check.Severity, SeverityWarning)
		}
		if len(check.Issues) == 0 || check.Issues[0].Code != "contract_external_effect_missing" {
			t.Fatalf("contract issues = %#v", check.Issues)
		}
	}
	if !found {
		t.Fatalf("task.contract check missing: %#v", result.Checks)
	}
}

func TestEvaluateEmailNotConfiguredFails(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:       "run-email-not-configured",
		Status:      "completed",
		ToolNames:   []string{"email.search"},
		ToolOutputs: []string{`{"status":"not_configured","message":"email tool is not configured"}`},
	})

	if result.Status != StatusWarning {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusWarning)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name != "email.result" {
			continue
		}
		found = true
		if check.Status != StatusFailed {
			t.Fatalf("email check status = %q, want %q", check.Status, StatusFailed)
		}
		if check.Severity != SeverityWarning {
			t.Fatalf("email check severity = %q, want %q", check.Severity, SeverityWarning)
		}
		if len(check.Issues) == 0 || check.Issues[0].Code != "email_not_configured" {
			t.Fatalf("email issues = %#v", check.Issues)
		}
	}
	if !found {
		t.Fatalf("email.result check missing: %#v", result.Checks)
	}
}

func TestEvaluateBlockingVerifierPreventsDelivery(t *testing.T) {
	t.Parallel()

	result := EvaluateWithOptions(Input{
		RunID:       "run-email-blocking",
		Status:      "completed",
		ToolNames:   []string{"email.search"},
		ToolOutputs: []string{`{"status":"not_configured","message":"email tool is not configured"}`},
	}, WithPolicy(Policy{
		VerifierSeverities: map[string]IssueSeverity{
			"email.result": SeverityBlocking,
		},
	}))

	if result.Status != StatusFailed {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusFailed)
	}
	if result.BlockingFailures != 1 {
		t.Fatalf("result.BlockingFailures = %d, want 1", result.BlockingFailures)
	}
	if !result.ShouldBlockDelivery() {
		t.Fatal("expected blocking verification to prevent delivery")
	}
}

func TestEvaluateBrowserExternalEffectEvidencePassesContract(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:     "run-browser-external-effect",
		Status:    "completed",
		Output:    "Submitted the form successfully.",
		ToolNames: []string{"browser.click", "browser.eval"},
		ToolOutputs: []string{
			`{"selector":"button[type=submit]","clicked":true}`,
			`{"url":"https://example.com/confirmation","html":"<h1>Done</h1>","result":{"submitted":true}}`,
		},
		Contract: &Contract{
			JobType:                "delivery",
			RequiresExternalEffect: true,
			ExpectedDeliverables: []ContractDeliverable{
				{Kind: contractDeliverableBrowserEvidence, Required: true},
			},
			AcceptanceCriteria: []ContractAcceptance{
				{ID: contractAcceptanceExternalEffect, Required: true},
			},
		},
	})

	if result.Status != StatusPassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusPassed)
	}
	for _, check := range result.Checks {
		if check.Name == "task.contract" && check.Status != StatusPassed {
			t.Fatalf("contract check status = %q, want %q; issues=%#v", check.Status, StatusPassed, check.Issues)
		}
	}
}

func TestEvaluateDeploymentStructuredEvidencePassesContract(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:  "run-deployment-structured-evidence",
		Status: "completed",
		Output: "Service updated and reachable.",
		Deliverables: []Deliverable{
			{
				Kind:     contractDeliverableDeployment,
				URI:      "https://deploy.example.com/releases/2026-04-10",
				ToolName: "deploy.apply",
			},
		},
		ToolNames: []string{"deploy.apply"},
		ToolOutputs: []string{
			`{"service":"payments-api","endpoint":"https://payments.example.com/healthz","url":"https://payments.example.com"}`,
		},
		Contract: &Contract{
			JobType:                "deployment",
			RequiresExternalEffect: true,
			ExpectedDeliverables: []ContractDeliverable{
				{Kind: contractDeliverableDeployment, Required: true},
			},
			AcceptanceCriteria: []ContractAcceptance{
				{ID: contractAcceptanceExternalEffect, Required: true},
			},
		},
	})

	if result.Status != StatusPassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusPassed)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name != "task.contract" {
			continue
		}
		found = true
		if check.Status != StatusPassed {
			t.Fatalf("contract check status = %q, want %q; issues=%#v", check.Status, StatusPassed, check.Issues)
		}
	}
	if !found {
		t.Fatalf("task.contract check missing: %#v", result.Checks)
	}
}

func TestEvaluateDeploymentPlainTextWithoutStructuredEvidenceFailsContract(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:  "run-deployment-plain-text-only",
		Status: "completed",
		Output: "deployed successfully, 发布成功",
		Contract: &Contract{
			JobType:                "deployment",
			RequiresExternalEffect: true,
			ExpectedDeliverables: []ContractDeliverable{
				{Kind: contractDeliverableDeployment, Required: true},
			},
			AcceptanceCriteria: []ContractAcceptance{
				{ID: contractAcceptanceExternalEffect, Required: true},
			},
		},
	})

	if result.Status != StatusWarning {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusWarning)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name != "task.contract" {
			continue
		}
		found = true
		if check.Status != StatusFailed {
			t.Fatalf("contract check status = %q, want %q", check.Status, StatusFailed)
		}
		if len(check.Issues) == 0 || check.Issues[0].Code != IssueCodeContractExternalEffectMissing {
			t.Fatalf("contract issues = %#v", check.Issues)
		}
	}
	if !found {
		t.Fatalf("task.contract check missing: %#v", result.Checks)
	}
}

func TestEvaluatePlanCoverageWarningsAddsAdvisoryWarning(t *testing.T) {
	t.Parallel()

	result := Evaluate(Input{
		RunID:                "run-plan-coverage",
		Status:               "completed",
		Output:               "Shared the summary.",
		PlanCoverageWarnings: []string{`deliverable "spreadsheet" is not clearly covered by any plan task`},
	})

	if result.Status != StatusWarning {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusWarning)
	}
	found := false
	for _, check := range result.Checks {
		if check.Name != "plan.coverage" {
			continue
		}
		found = true
		if check.Requirement != RequirementAdvisory {
			t.Fatalf("check.Requirement = %q, want %q", check.Requirement, RequirementAdvisory)
		}
		if check.Status != StatusWarning {
			t.Fatalf("check.Status = %q, want %q", check.Status, StatusWarning)
		}
		if len(check.Issues) != 1 || check.Issues[0].Code != IssueCodePlanCoverageGap {
			t.Fatalf("check.Issues = %#v", check.Issues)
		}
	}
	if !found {
		t.Fatalf("plan.coverage check missing: %#v", result.Checks)
	}
	if result.AdvisoryWarnings != 1 {
		t.Fatalf("result.AdvisoryWarnings = %d, want 1", result.AdvisoryWarnings)
	}
}
