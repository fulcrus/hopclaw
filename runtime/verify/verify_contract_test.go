package verify

import "testing"

func TestEvaluateContractUsesPolicyMappingAndDefaultVerifiers(t *testing.T) {
	t.Parallel()

	result := EvaluateWithOptions(Input{
		RunID:                "run-verify-contract",
		Status:               "completed",
		Output:               "Sent the update and captured the browser result.",
		ToolNames:            []string{"browser.navigate", "email.send"},
		ToolOutputs:          []string{`{"success":true,"message_id":"msg-1","url":"https://example.com/status"}`},
		PlanCoverageWarnings: []string{"watch alert fallback is not explicitly covered"},
		Deliverables: []Deliverable{
			{
				Kind:        contractDeliverableBrowserEvidence,
				ToolName:    "browser.navigate",
				URI:         "/tmp/browser-proof.png",
				ContentType: "image/png",
			},
		},
	}, WithPolicy(PolicyFromStrings(map[string]string{
		"plan.coverage": "blocking",
	})))

	checksByName := make(map[string]Check, len(result.Checks))
	for _, check := range result.Checks {
		checksByName[check.Name] = check
	}

	for _, name := range []string{"run.outcome", "plan.coverage", "browser.result", "email.result"} {
		if _, ok := checksByName[name]; !ok {
			t.Fatalf("missing verifier %q in %#v", name, result.Checks)
		}
	}
	if got := checksByName["plan.coverage"].Severity; got != SeverityBlocking {
		t.Fatalf("plan.coverage severity = %q, want %q", got, SeverityBlocking)
	}
	if got := checksByName["browser.result"].Status; got != StatusPassed {
		t.Fatalf("browser.result status = %q, want %q", got, StatusPassed)
	}
	if got := checksByName["email.result"].Status; got != StatusPassed {
		t.Fatalf("email.result status = %q, want %q", got, StatusPassed)
	}
}
