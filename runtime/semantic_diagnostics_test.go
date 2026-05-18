package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	"github.com/fulcrus/hopclaw/triage"
)

type semanticDiagnosticsIngressClassifier struct{}

func (semanticDiagnosticsIngressClassifier) Analyze(context.Context, InteractionIngressClassifyRequest) (InteractionIngressClassification, error) {
	return InteractionIngressClassification{}, nil
}

type semanticDiagnosticsInteractionClassifier struct{}

func (semanticDiagnosticsInteractionClassifier) Classify(context.Context, InteractionClassifyRequest) (InteractionDecision, error) {
	return InteractionDecision{}, nil
}

type semanticDiagnosticsAutomationClassifier struct{}

func (semanticDiagnosticsAutomationClassifier) Analyze(context.Context, AutomationIntentClassifyRequest) (automationintent.Plan, error) {
	return automationintent.Plan{}, nil
}

type semanticDiagnosticsRunTriage struct{}

func (semanticDiagnosticsRunTriage) AnalyzeRun(context.Context, triage.RunRequest) (triage.RunDecision, error) {
	return triage.RunDecision{}, nil
}

type semanticDiagnosticsPreflight struct{}

func (semanticDiagnosticsPreflight) Analyze(context.Context, agent.PreflightAnalysisRequest) (agent.PreflightAnalysis, error) {
	return agent.PreflightAnalysis{}, nil
}

type semanticDiagnosticsTaskContract struct{}

func (semanticDiagnosticsTaskContract) Analyze(context.Context, agent.TaskContractAnalysisRequest) (agent.TaskContractAnalysis, error) {
	return agent.TaskContractAnalysis{}, nil
}

type semanticDiagnosticsSessionStore struct{ agent.SessionStore }

type semanticDiagnosticsRunStore struct{ agent.RunStore }

func TestServiceSemanticIngressSummaryPrefersUnifiedIngress(t *testing.T) {
	t.Parallel()

	component := (&agent.AgentComponent{}).
		WithRunTriage(semanticDiagnosticsRunTriage{}).
		WithPreflightAnalyzer(semanticDiagnosticsPreflight{}).
		WithTaskContractAnalyzer(semanticDiagnosticsTaskContract{})
	svc := NewService(component, nil, nil, nil, nil, nil).
		WithIngressClassifier(semanticDiagnosticsIngressClassifier{}).
		WithClassifier(semanticDiagnosticsInteractionClassifier{}).
		WithAutomationClassifier(semanticDiagnosticsAutomationClassifier{})

	got := svc.SemanticIngressSummary()
	if got.MainPath != "interaction_ingress" {
		t.Fatalf("MainPath = %q, want interaction_ingress", got.MainPath)
	}
	if !got.InteractionIngressConfigured || !got.LegacyInteractionClassifierConfigured || !got.LegacyAutomationClassifierConfigured {
		t.Fatalf("summary = %+v, want all ingress classifiers configured", got)
	}
	if !got.SharedSignalEnabled || !got.LanguageProfileEnabled {
		t.Fatalf("summary = %+v, want shared signal and language profile enabled", got)
	}
	if !got.RunTriageConfigured || !got.PreflightAnalyzerConfigured || !got.TaskContractConfigured {
		t.Fatalf("summary = %+v, want triage/preflight/task-contract configured", got)
	}
	if len(got.MainPathLanguageFamilies) == 0 || got.MainPathLanguageFamilies[len(got.MainPathLanguageFamilies)-1] != "zh" {
		t.Fatalf("MainPathLanguageFamilies = %#v, want stable language family list", got.MainPathLanguageFamilies)
	}
}

func TestServiceSemanticIngressSummaryFallsBackToLegacyClassifier(t *testing.T) {
	t.Parallel()

	svc := NewService((&agent.AgentComponent{}).WithRunTriage(semanticDiagnosticsRunTriage{}), nil, nil, nil, nil, nil).
		WithClassifier(semanticDiagnosticsInteractionClassifier{})

	got := svc.SemanticIngressSummary()
	if got.MainPath != "legacy_interaction_classifier" {
		t.Fatalf("MainPath = %q, want legacy_interaction_classifier", got.MainPath)
	}
	if got.InteractionIngressConfigured {
		t.Fatalf("InteractionIngressConfigured = %v, want false", got.InteractionIngressConfigured)
	}
	if !got.LegacyInteractionClassifierConfigured {
		t.Fatalf("LegacyInteractionClassifierConfigured = %v, want true", got.LegacyInteractionClassifierConfigured)
	}
}

func TestServiceSemanticIngressSummaryDefaultsToDeterministicOnly(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, nil, nil, nil, nil, nil)
	got := svc.SemanticIngressSummary()
	if got.MainPath != "deterministic_only" {
		t.Fatalf("MainPath = %q, want deterministic_only", got.MainPath)
	}
	if got.SharedSignalEnabled || got.RunTriageConfigured || got.PreflightAnalyzerConfigured || got.TaskContractConfigured {
		t.Fatalf("summary = %+v, want zero analyzer summary", got)
	}
	if len(got.MainPathLanguageFamilies) != 0 {
		t.Fatalf("MainPathLanguageFamilies = %#v, want empty", got.MainPathLanguageFamilies)
	}
}

var (
	_ InteractionIngressClassifier = semanticDiagnosticsIngressClassifier{}
	_ InteractionClassifier        = semanticDiagnosticsInteractionClassifier{}
	_ AutomationIntentClassifier   = semanticDiagnosticsAutomationClassifier{}
)
