package jsonrepair

import "testing"

func TestDecodeJSONObjectCandidateRepairsTrailingCommaAndFence(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"mode\":\"planned\",}\n```"
	var out struct {
		Mode string `json:"mode"`
	}
	if err := DecodeJSONObjectCandidate(raw, &out); err != nil {
		t.Fatalf("DecodeJSONObjectCandidate() error = %v", err)
	}
	if out.Mode != "planned" {
		t.Fatalf("Mode = %q, want planned", out.Mode)
	}
}

func TestRepairBalancesMissingBrace(t *testing.T) {
	t.Parallel()

	got := Repair("{\"a\":1")
	if got != "{\"a\":1}" {
		t.Fatalf("Repair() = %q", got)
	}
}
