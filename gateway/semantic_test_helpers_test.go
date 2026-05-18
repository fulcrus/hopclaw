package gateway

import (
	"context"

	"github.com/fulcrus/hopclaw/agent"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/triage"
)

type semanticDiagnosticsIngressClassifier struct{}

func (semanticDiagnosticsIngressClassifier) Analyze(context.Context, runtimepkg.InteractionIngressClassifyRequest) (runtimepkg.InteractionIngressClassification, error) {
	return runtimepkg.InteractionIngressClassification{}, nil
}

type semanticDiagnosticsInteractionClassifier struct{}

func (semanticDiagnosticsInteractionClassifier) Classify(context.Context, runtimepkg.InteractionClassifyRequest) (runtimepkg.InteractionDecision, error) {
	return runtimepkg.InteractionDecision{}, nil
}

type semanticDiagnosticsAutomationClassifier struct{}

func (semanticDiagnosticsAutomationClassifier) Analyze(context.Context, runtimepkg.AutomationIntentClassifyRequest) (automationintent.Plan, error) {
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
