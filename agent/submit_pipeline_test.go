package agent

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/triage"
)

func TestApplyClarificationRoundLimitRelaxesAfterMaxRounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runs := NewInMemoryRunStore()
	component := &AgentComponent{
		runs:   runs,
		config: AgentConfig{MaxClarificationRounds: 3},
	}

	original := mustCreateSupersededClarificationRun(t, ctx, runs, "")
	first := mustCreateSupersededClarificationRun(t, ctx, runs, original.ID)
	second := mustCreateSupersededClarificationRun(t, ctx, runs, first.ID)
	current, err := runs.Create(ctx, "sess-clarify", IncomingMessage{
		SessionKey:  "sess-clarify",
		ParentRunID: second.ID,
	}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create(current) error = %v", err)
	}
	current.Preflight = blockingClarificationPreflight()

	got := component.applyClarificationRoundLimit(ctx, current, IncomingMessage{
		Metadata: map[string]any{
			MetadataKeyClarificationSourceRunID: original.ID,
		},
	})
	if got == nil {
		t.Fatal("expected preflight report")
	}
	if got.Blocking {
		t.Fatalf("got.Blocking = %v, want false", got.Blocking)
	}
	if got.State != RunPreflightAutoPreparing {
		t.Fatalf("got.State = %q, want %q", got.State, RunPreflightAutoPreparing)
	}
	if got.Summary != "I'll proceed with what I have. Some details may be incomplete." {
		t.Fatalf("got.Summary = %q", got.Summary)
	}
	if len(got.ClarificationSlots) != 0 {
		t.Fatalf("got.ClarificationSlots = %#v, want empty", got.ClarificationSlots)
	}
	if !preflightChecksContain(got.Checks, "clarification_limit_reached") {
		t.Fatalf("got.Checks = %#v, want clarification_limit_reached", got.Checks)
	}
}

func TestApplyClarificationRoundLimitKeepsBlockingBeforeMaxRounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runs := NewInMemoryRunStore()
	component := &AgentComponent{
		runs:   runs,
		config: AgentConfig{MaxClarificationRounds: 3},
	}

	original := mustCreateSupersededClarificationRun(t, ctx, runs, "")
	first := mustCreateSupersededClarificationRun(t, ctx, runs, original.ID)
	current, err := runs.Create(ctx, "sess-clarify", IncomingMessage{
		SessionKey:  "sess-clarify",
		ParentRunID: first.ID,
	}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create(current) error = %v", err)
	}
	current.Preflight = blockingClarificationPreflight()

	got := component.applyClarificationRoundLimit(ctx, current, IncomingMessage{
		Metadata: map[string]any{
			MetadataKeyClarificationSourceRunID: original.ID,
		},
	})
	if got == nil {
		t.Fatal("expected preflight report")
	}
	if !got.Blocking {
		t.Fatalf("got.Blocking = %v, want true", got.Blocking)
	}
	if got.State != RunPreflightNeedsConfirmation {
		t.Fatalf("got.State = %q, want %q", got.State, RunPreflightNeedsConfirmation)
	}
	if len(got.ClarificationSlots) != 1 {
		t.Fatalf("got.ClarificationSlots = %#v, want one slot", got.ClarificationSlots)
	}
	if preflightChecksContain(got.Checks, "clarification_limit_reached") {
		t.Fatalf("got.Checks = %#v, did not expect clarification_limit_reached", got.Checks)
	}
}

func TestIdleEpisodeBoundaryReasonUsesCapabilityDrivenLocalInteractiveTimeout(t *testing.T) {
	t.Parallel()

	session := &Session{
		Key: "cli-idle",
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:      contextengine.RoleUser,
				Content:   "first",
				CreatedAt: time.Now().UTC().Add(-3 * time.Hour),
				Metadata: map[string]any{
					meta.KeyChannelCapabilities: map[string]any{
						"interactive": true,
					},
					meta.KeyChatType: meta.ChatTypeDirect.String(),
				},
			}},
		},
	}

	got := idleEpisodeBoundaryReason(session, IncomingMessage{
		Content: "continue",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive": true,
			},
			meta.KeyChatType: meta.ChatTypeDirect.String(),
		},
	})
	if got != "idle_timeout" {
		t.Fatalf("idleEpisodeBoundaryReason() = %q, want %q", got, "idle_timeout")
	}
}

func TestExplicitEpisodeBoundaryReasonUsesStructuredMetadata(t *testing.T) {
	t.Parallel()

	got := explicitEpisodeBoundaryReason(map[string]any{
		MetadataKeyEpisodeBoundaryReason: "explicit_request",
	})
	if got != "explicit_request" {
		t.Fatalf("explicitEpisodeBoundaryReason() = %q, want %q", got, "explicit_request")
	}
}

func TestSubmitEpisodeBoundaryRotatesOnProfileSwitch(t *testing.T) {
	t.Parallel()

	session := &Session{
		Key: "shared-chat",
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:      contextengine.RoleUser,
				Content:   "continue helping in the same chat",
				CreatedAt: time.Now().UTC().Add(-5 * time.Minute),
				Metadata: map[string]any{
					MetadataKeyAgentProfileName:  "writer",
					MetadataKeyAgentProfileModel: "gpt-4.1",
				},
			}},
		},
	}
	current := &EffectiveAgentProfile{
		Name:  "reviewer",
		Model: "gpt-4.1",
	}

	reason, rotate := submitEpisodeBoundary(session, IncomingMessage{
		Content: "continue helping in the same chat",
		Metadata: map[string]any{
			MetadataKeyAgentProfileName:  "reviewer",
			MetadataKeyAgentProfileModel: "gpt-4.1",
		},
	}, current)
	if !rotate {
		t.Fatal("submitEpisodeBoundary() rotate = false, want true")
	}
	if reason != "agent_profile_switch" {
		t.Fatalf("submitEpisodeBoundary() reason = %q, want %q", reason, "agent_profile_switch")
	}
}

func TestSubmitEpisodeBoundaryKeepsClarificationFollowupsInSameEpisode(t *testing.T) {
	t.Parallel()

	session := &Session{
		Key: "cli-idle",
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:      contextengine.RoleUser,
				Content:   "first request",
				CreatedAt: time.Now().UTC().Add(-3 * time.Hour),
				Metadata: map[string]any{
					meta.KeyChannelCapabilities: map[string]any{
						"interactive": true,
					},
					meta.KeyChatType: meta.ChatTypeDirect.String(),
				},
			}},
		},
	}

	reason, rotate := submitEpisodeBoundary(session, IncomingMessage{
		Content: "here is the missing target",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive": true,
			},
			meta.KeyChatType:                    meta.ChatTypeDirect.String(),
			MetadataKeyClarificationSourceRunID: "run-clarify",
		},
	}, nil)
	if rotate {
		t.Fatal("submitEpisodeBoundary() rotate = true, want false for clarification follow-up")
	}
	if reason != "default" {
		t.Fatalf("submitEpisodeBoundary() reason = %q, want %q", reason, "default")
	}
}

func TestPrepareSubmitEpisodeRejectsMissingState(t *testing.T) {
	t.Parallel()

	component := &AgentComponent{}
	if err := component.prepareSubmitEpisode(context.Background(), nil); err == nil {
		t.Fatal("prepareSubmitEpisode() error = nil, want missing state failure")
	}
}

func TestPrepareSubmitEpisodeRejectsMissingSession(t *testing.T) {
	t.Parallel()

	component := &AgentComponent{}
	if err := component.prepareSubmitEpisode(context.Background(), &submitPipelineState{}); err == nil {
		t.Fatal("prepareSubmitEpisode() error = nil, want missing session failure")
	}
}

func TestAnalyzeSubmitRunAllowsNilAgent(t *testing.T) {
	t.Parallel()

	var component *AgentComponent
	component.analyzeSubmitRun(context.Background(), &submitPipelineState{
		session: &Session{},
		run:     &Run{},
	})
}

func TestSubmitStoresSanitizedSemanticSignalDiagnostics(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:       "direct",
			SuggestedDomains:    []string{"browser"},
			RequiresCurrentInfo: true,
			Reason:              "fresh_page_state",
			Confidence:          0.82,
		},
	}
	preflight := &staticPreflightAnalyzer{
		analysis: PreflightAnalysis{
			SuggestedDomains:         []string{"browser"},
			DomainsSpecified:         true,
			Reason:                   "browser_ready",
			Confidence:               0.84,
			DetectedDomains:          []string{"browser"},
			DetectedDomainsSpecified: true,
		},
	}
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:              "report",
			TargetSummary:        "docs/tmp/resumen.md",
			SuggestedDomains:     []string{"browser", "fs"},
			CapabilityHints:      []string{"browser.navigate", "fs.write"},
			DeliverableKinds:     []string{"browser_evidence", "document"},
			MissingInfoSpecified: true,
			MissingInfoIDs:       []string{},
			Confidence:           0.93,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithRunTriage(triager).
		WithPreflightAnalyzer(preflight).
		WithTaskContractAnalyzer(taskContract)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-submit-semantic-diagnostics",
		ExternalEventID: "evt-submit-semantic-diagnostics",
		Content:         "Resume https://example.com en docs/tmp/resumen.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.SemanticSignal == nil {
		t.Fatal("expected semantic signal diagnostics on run")
	}
	if run.SemanticSignal.Message != "" || run.SemanticSignal.SessionSummary != "" {
		t.Fatalf("run.SemanticSignal should be sanitized, got %#v", run.SemanticSignal)
	}
	if run.SemanticSignal.Language.Family != "es" {
		t.Fatalf("run.SemanticSignal.Language.Family = %q, want es", run.SemanticSignal.Language.Family)
	}
	if run.SemanticSignal.ExecutionMode != ExecutionModeDirect {
		t.Fatalf("run.SemanticSignal.ExecutionMode = %q, want %q", run.SemanticSignal.ExecutionMode, ExecutionModeDirect)
	}
	if !run.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("run.SemanticSignal.RequiresCurrentInfo = %v, want true", run.SemanticSignal.RequiresCurrentInfo)
	}
	if !run.SemanticSignal.TriageReady || !run.SemanticSignal.TaskContractReady {
		t.Fatalf("run.SemanticSignal readiness = %#v, want triage/task contract ready", run.SemanticSignal)
	}
	if run.SemanticSignal.JobType != "report" {
		t.Fatalf("run.SemanticSignal.JobType = %q, want report", run.SemanticSignal.JobType)
	}
	if run.SemanticSignal.TargetSummary != "docs/tmp/resumen.md" {
		t.Fatalf("run.SemanticSignal.TargetSummary = %q, want docs/tmp/resumen.md", run.SemanticSignal.TargetSummary)
	}
	if len(run.SemanticSignal.SuggestedDomains) != 2 || run.SemanticSignal.SuggestedDomains[0] != "browser" || run.SemanticSignal.SuggestedDomains[1] != "fs" {
		t.Fatalf("run.SemanticSignal.SuggestedDomains = %#v, want [browser fs]", run.SemanticSignal.SuggestedDomains)
	}

	stored, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if stored.SemanticSignal == nil || stored.SemanticSignal.Message != "" || stored.SemanticSignal.SessionSummary != "" {
		t.Fatalf("stored.SemanticSignal = %#v, want sanitized persisted diagnostics", stored.SemanticSignal)
	}
}

func TestSubmitWorkflowInitializesWorkflowState(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeWorkflow}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-submit-workflow",
		ExternalEventID: "evt-submit-workflow",
		Content:         "continue this workflow",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeWorkflow {
		t.Fatalf("run.ExecutionMode = %q, want %q", run.ExecutionMode, ExecutionModeWorkflow)
	}
	if run.WorkflowState == nil {
		t.Fatal("run.WorkflowState = nil, want initialized state")
	}
	if run.WorkflowState.OriginalRunID != run.ID {
		t.Fatalf("run.WorkflowState.OriginalRunID = %q, want %q", run.WorkflowState.OriginalRunID, run.ID)
	}
	if run.WorkflowState.ContinuationIndex != 0 {
		t.Fatalf("run.WorkflowState.ContinuationIndex = %d, want 0", run.WorkflowState.ContinuationIndex)
	}
	if run.WorkflowState.MaxContinuations != DefaultMaxContinuations {
		t.Fatalf("run.WorkflowState.MaxContinuations = %d, want %d", run.WorkflowState.MaxContinuations, DefaultMaxContinuations)
	}
	if run.WorkflowState.MaxTotalRounds != DefaultMaxTotalRounds {
		t.Fatalf("run.WorkflowState.MaxTotalRounds = %d, want %d", run.WorkflowState.MaxTotalRounds, DefaultMaxTotalRounds)
	}
	if run.WorkflowState.Budget == nil {
		t.Fatal("run.WorkflowState.Budget = nil, want initialized budget")
	}
	if run.WorkflowState.Budget.Mode != WorkflowBudgetModeNormal {
		t.Fatalf("run.WorkflowState.Budget.Mode = %q, want %q", run.WorkflowState.Budget.Mode, WorkflowBudgetModeNormal)
	}
	if run.WorkflowState.Budget.Policy.HardContinuations != DefaultMaxContinuations {
		t.Fatalf(
			"run.WorkflowState.Budget.Policy.HardContinuations = %d, want %d",
			run.WorkflowState.Budget.Policy.HardContinuations,
			DefaultMaxContinuations,
		)
	}
	if run.WorkflowState.Budget.Policy.HardTotalRounds != DefaultMaxTotalRounds {
		t.Fatalf(
			"run.WorkflowState.Budget.Policy.HardTotalRounds = %d, want %d",
			run.WorkflowState.Budget.Policy.HardTotalRounds,
			DefaultMaxTotalRounds,
		)
	}
	if run.WorkflowState.Budget.Usage.StartedAt.IsZero() {
		t.Fatal("run.WorkflowState.Budget.Usage.StartedAt = zero, want initialized timestamp")
	}
}

func TestFinalizeSubmitPreflightPreservesDetectedDomains(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeDirect,
		Preflight: buildRunPreflightWithAnalysis("draft the email reply", PreflightAnalysis{
			SuggestedDomains: []string{"email"},
			DetectedDomains:  []string{"email"},
		}),
		TaskContract: &TaskContract{
			SuggestedDomains: []string{"email"},
		},
	}

	report := finalizeSubmitPreflight(nil, IncomingMessage{
		Content: "draft the email reply",
	}, &Session{}, run)
	if report == nil {
		t.Fatal("expected preflight report")
	}
	if !containsTestString(report.DetectedDomains, "email") {
		t.Fatalf("report.DetectedDomains = %#v, want email", report.DetectedDomains)
	}
	if !containsTestString(report.SuggestedDomains, "email") {
		t.Fatalf("report.SuggestedDomains = %#v, want email", report.SuggestedDomains)
	}
}

func mustCreateSupersededClarificationRun(t *testing.T, ctx context.Context, runs *InMemoryRunStore, parentRunID string) *Run {
	t.Helper()

	run, err := runs.Create(ctx, "sess-clarify", IncomingMessage{
		SessionKey:  "sess-clarify",
		ParentRunID: parentRunID,
	}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunCancelled
	run.Error = RunReasonClarificationSuperseded
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}
	return run
}

func blockingClarificationPreflight() *RunPreflightReport {
	return &RunPreflightReport{
		State:        RunPreflightNeedsConfirmation,
		Summary:      "Need one more detail before continuing.",
		Prompt:       "Please share the exact target.",
		Question:     "What exact file or target should I use?",
		ReplyHints:   []string{"/tmp/demo.txt"},
		ContinueHint: "Reply with the missing detail and I will continue.",
		Blocking:     true,
		Checks: []RunPreflightCheck{{
			ID:       taskMissingInfoSourceTarget,
			Title:    "Missing target",
			State:    RunPreflightNeedsConfirmation,
			Blocking: true,
		}},
		ClarificationSlots: []RunClarificationSlot{{
			ID:       taskMissingInfoSourceTarget,
			Required: true,
		}},
		GeneratedAt: time.Now().UTC(),
	}
}
