package resultmodel

import "testing"

func TestAutomationResultNormalizedMarksVerificationFailureAsError(t *testing.T) {
	t.Parallel()

	result := AutomationResult{
		Status:  AutomationStatusTriggered,
		Summary: "chart generated",
		Verification: &ResultVerification{
			Status:  VerificationStatus("failed"),
			Summary: "artifact chart is missing",
		},
	}

	normalized := result.Normalized()
	if normalized.Status != AutomationStatusError {
		t.Fatalf("Status = %q, want %q", normalized.Status, AutomationStatusError)
	}
	if normalized.Error == nil {
		t.Fatal("expected verification failure to populate Error")
	}
	if normalized.Error.Message != "artifact chart is missing" {
		t.Fatalf("Error.Message = %q, want %q", normalized.Error.Message, "artifact chart is missing")
	}
}

func TestAutomationResultNormalizedKeepsWarningsNonFatal(t *testing.T) {
	t.Parallel()

	result := AutomationResult{
		Status:  AutomationStatusTriggered,
		Summary: "chart generated",
		Verification: &ResultVerification{
			Status:  VerificationStatus("warning"),
			Summary: "one advisory warning",
		},
	}

	normalized := result.Normalized()
	if normalized.Status != AutomationStatusTriggered {
		t.Fatalf("Status = %q, want %q", normalized.Status, AutomationStatusTriggered)
	}
	if normalized.Error != nil {
		t.Fatalf("Error = %#v, want nil", normalized.Error)
	}
}
