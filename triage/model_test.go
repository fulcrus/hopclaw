package triage

import (
	"context"
	"testing"
	"time"
)

func TestParseJSONExtractsFromCodeFence(t *testing.T) {
	t.Parallel()

	decision, err := parseJSON[RunDecision]("preface\n```json\n{\"execution_mode\":\"planned\",\"needs_reference\":true,\"requires_current_info\":true}\n```")
	if err != nil {
		t.Fatalf("parseJSON() error = %v", err)
	}
	if decision.ExecutionMode != "planned" {
		t.Fatalf("decision.ExecutionMode = %q, want planned", decision.ExecutionMode)
	}
	if !decision.NeedsReference {
		t.Fatal("decision.NeedsReference = false, want true")
	}
	if !decision.RequiresCurrentInfo {
		t.Fatal("decision.RequiresCurrentInfo = false, want true")
	}
}

func TestAnalyzeRunUsesDefaultModel(t *testing.T) {
	t.Parallel()

	capturedModel := ""
	m := NewModelTriage(func(_ context.Context, req ChatRequest) (string, error) {
		capturedModel = req.Model
		return `{"execution_mode":"direct","confidence":0.9}`, nil
	}, 0).WithDefaultModel("triage-default")

	if m.timeout != 3*time.Second {
		t.Fatalf("m.timeout = %v, want %v", m.timeout, 3*time.Second)
	}

	decision, err := m.AnalyzeRun(context.Background(), RunRequest{
		Model:   "caller-model",
		Message: "do the thing",
	})
	if err != nil {
		t.Fatalf("AnalyzeRun() error = %v", err)
	}
	if capturedModel != "triage-default" {
		t.Fatalf("capturedModel = %q, want triage-default", capturedModel)
	}
	if decision.ExecutionMode != "direct" {
		t.Fatalf("decision.ExecutionMode = %q, want direct", decision.ExecutionMode)
	}
}
