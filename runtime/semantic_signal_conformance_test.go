package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/triage"
)

type semanticSignalConformanceRunTriage struct {
	order    *[]string
	calls    int
	lastReq  triage.RunRequest
	decision triage.RunDecision
}

func (a *semanticSignalConformanceRunTriage) AnalyzeRun(_ context.Context, req triage.RunRequest) (triage.RunDecision, error) {
	a.calls++
	a.lastReq = req
	if a.order != nil {
		*a.order = append(*a.order, "triage")
	}
	return a.decision, nil
}

type semanticSignalConformancePreflight struct {
	order    *[]string
	calls    int
	lastReq  agent.PreflightAnalysisRequest
	analysis agent.PreflightAnalysis
}

func (a *semanticSignalConformancePreflight) Analyze(_ context.Context, req agent.PreflightAnalysisRequest) (agent.PreflightAnalysis, error) {
	a.calls++
	a.lastReq = req
	if a.order != nil {
		*a.order = append(*a.order, "preflight")
	}
	return a.analysis, nil
}

type semanticSignalConformanceTaskContract struct {
	order    *[]string
	calls    int
	lastReq  agent.TaskContractAnalysisRequest
	analysis agent.TaskContractAnalysis
}

func (a *semanticSignalConformanceTaskContract) Analyze(_ context.Context, req agent.TaskContractAnalysisRequest) (agent.TaskContractAnalysis, error) {
	a.calls++
	a.lastReq = req
	if a.order != nil {
		*a.order = append(*a.order, "task_contract")
	}
	return a.analysis, nil
}

func newSemanticSignalConformanceService(
	triager agent.RunTriageAnalyzer,
	preflight agent.PreflightAnalyzer,
	taskContract agent.TaskContractAnalyzer,
) *Service {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil)
	if triager != nil {
		component = component.WithRunTriage(triager)
	}
	if preflight != nil {
		component = component.WithPreflightAnalyzer(preflight)
	}
	if taskContract != nil {
		component = component.WithTaskContractAnalyzer(taskContract)
	}
	return NewService(component, sessions, runs, approvals, bus, artifacts)
}

func TestSubmitSemanticSignalConformanceKeepsAnalyzerStagesIndependent(t *testing.T) {
	t.Parallel()

	order := []string{}
	triager := &semanticSignalConformanceRunTriage{
		order: &order,
		decision: triage.RunDecision{
			ExecutionMode:    "direct",
			SuggestedDomains: []string{"browser"},
			Confidence:       0.84,
		},
	}
	preflight := &semanticSignalConformancePreflight{
		order: &order,
		analysis: agent.PreflightAnalysis{
			SuggestedDomains: []string{"browser"},
			DomainsSpecified: true,
			Reason:           "browser_ready",
			Confidence:       0.81,
		},
	}
	taskContract := &semanticSignalConformanceTaskContract{
		order: &order,
		analysis: agent.TaskContractAnalysis{
			JobType:              "report",
			SuggestedDomains:     []string{"browser", "document"},
			DeliverableKinds:     []string{"browser_evidence", "document"},
			MissingInfoSpecified: true,
			MissingInfoIDs:       []string{},
			Confidence:           0.93,
		},
	}
	svc := newSemanticSignalConformanceService(triager, preflight, taskContract)

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "semantic-signal-conformance-stages",
		Content:    "Resume https://example.com en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if got := strings.Join(order, ","); got != "triage,preflight,task_contract" {
		t.Fatalf("analyzer order = %q, want triage,preflight,task_contract", got)
	}
	if triager.calls != 1 || preflight.calls != 1 || taskContract.calls != 1 {
		t.Fatalf("calls = triage:%d preflight:%d task_contract:%d, want 1/1/1", triager.calls, preflight.calls, taskContract.calls)
	}
	if triager.lastReq.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.LanguageHint = %q, want es", triager.lastReq.LanguageHint)
	}
	if preflight.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on preflight request")
	}
	if !preflight.lastReq.SemanticSignal.TriageReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TriageReady = %v, want true", preflight.lastReq.SemanticSignal.TriageReady)
	}
	if preflight.lastReq.SemanticSignal.TaskContractReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TaskContractReady = %v, want false", preflight.lastReq.SemanticSignal.TaskContractReady)
	}
	if preflight.lastReq.SemanticSignal.Language.Family != "es" {
		t.Fatalf("preflight.lastReq.SemanticSignal.Language.Family = %q, want es", preflight.lastReq.SemanticSignal.Language.Family)
	}
	if taskContract.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on task-contract request")
	}
	if !taskContract.lastReq.SemanticSignal.TriageReady {
		t.Fatalf("taskContract.lastReq.SemanticSignal.TriageReady = %v, want true", taskContract.lastReq.SemanticSignal.TriageReady)
	}
	if taskContract.lastReq.SemanticSignal.TaskContractReady {
		t.Fatalf("taskContract.lastReq.SemanticSignal.TaskContractReady = %v, want false", taskContract.lastReq.SemanticSignal.TaskContractReady)
	}
	if taskContract.lastReq.SemanticSignal.Language.Family != "es" {
		t.Fatalf("taskContract.lastReq.SemanticSignal.Language.Family = %q, want es", taskContract.lastReq.SemanticSignal.Language.Family)
	}
	if len(taskContract.lastReq.SuggestedDomains) != 1 || taskContract.lastReq.SuggestedDomains[0] != "browser" {
		t.Fatalf("taskContract.lastReq.SuggestedDomains = %#v, want [browser]", taskContract.lastReq.SuggestedDomains)
	}
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}
	if run.Preflight == nil || run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v, want non-blocking preflight", run.Preflight)
	}
	if run.TaskContract == nil {
		t.Fatal("expected task contract")
	}
	if run.TaskContract.JobType != "report" {
		t.Fatalf("run.TaskContract.JobType = %q, want report", run.TaskContract.JobType)
	}
}

func TestSubmitSemanticSignalConformanceBlockingPreflightStopsTaskContract(t *testing.T) {
	t.Parallel()

	order := []string{}
	triager := &semanticSignalConformanceRunTriage{
		order: &order,
		decision: triage.RunDecision{
			ExecutionMode: "direct",
			Confidence:    0.73,
		},
	}
	preflight := &semanticSignalConformancePreflight{
		order: &order,
		analysis: agent.PreflightAnalysis{
			NeedsReference:   true,
			SuggestedDomains: []string{"browser"},
			DomainsSpecified: true,
			Reason:           "need_source_target",
			Confidence:       0.9,
		},
	}
	taskContract := &semanticSignalConformanceTaskContract{
		order: &order,
		analysis: agent.TaskContractAnalysis{
			JobType:          "report",
			SuggestedDomains: []string{"browser", "document"},
			DeliverableKinds: []string{"browser_evidence", "document"},
			Confidence:       0.92,
		},
	}
	svc := newSemanticSignalConformanceService(triager, preflight, taskContract)

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "semantic-signal-conformance-blocking",
		Content:    "Resume esta página.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if got := strings.Join(order, ","); got != "triage,preflight" {
		t.Fatalf("analyzer order = %q, want triage,preflight", got)
	}
	if triager.calls != 1 || preflight.calls != 1 {
		t.Fatalf("calls = triage:%d preflight:%d, want 1/1", triager.calls, preflight.calls)
	}
	if taskContract.calls != 0 {
		t.Fatalf("taskContract.calls = %d, want 0 when preflight blocks", taskContract.calls)
	}
	if preflight.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on preflight request")
	}
	if !preflight.lastReq.SemanticSignal.TriageReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TriageReady = %v, want true", preflight.lastReq.SemanticSignal.TriageReady)
	}
	if preflight.lastReq.SemanticSignal.TaskContractReady {
		t.Fatalf("preflight.lastReq.SemanticSignal.TaskContractReady = %v, want false", preflight.lastReq.SemanticSignal.TaskContractReady)
	}
	if preflight.lastReq.SemanticSignal.Language.Family != "es" {
		t.Fatalf("preflight.lastReq.SemanticSignal.Language.Family = %q, want es", preflight.lastReq.SemanticSignal.Language.Family)
	}
	if run.Status != agent.RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || !run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v, want blocking preflight", run.Preflight)
	}
}
