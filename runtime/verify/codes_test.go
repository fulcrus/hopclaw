package verify

import (
	"reflect"
	"strings"
	"testing"
)

func TestAllIssueCodesStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"contract_missing_info_unresolved",
		"contract_external_effect_missing",
		"contract_deliverable_missing",
		"contract_acceptance_missing",
		"plan_coverage_gap",
		"run_failed",
		"missing_result",
		"tool_execution_failed",
		"browser_evidence_missing",
		"desktop_evidence_missing",
		"spreadsheet_evidence_missing",
		"document_evidence_missing",
		"presentation_evidence_missing",
		"email_not_configured",
		"email_action_failed",
		"email_evidence_missing",
		"watch_notification_missing",
	}
	got := AllIssueCodes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllIssueCodes() = %#v, want %#v", got, want)
	}

	seen := make(map[string]struct{}, len(got))
	for _, code := range got {
		if _, ok := seen[code]; ok {
			t.Fatalf("duplicate verification code %q in %#v", code, got)
		}
		seen[code] = struct{}{}
		if code != strings.ToLower(code) || strings.Contains(code, "-") {
			t.Fatalf("verification code %q is not canonical snake_case", code)
		}
	}
}
