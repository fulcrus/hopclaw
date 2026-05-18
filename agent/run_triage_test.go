package agent

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/triage"
)

type staticRunTriage struct {
	decision triage.RunDecision
	err      error
	calls    int
	lastReq  triage.RunRequest
}

func (s *staticRunTriage) AnalyzeRun(_ context.Context, req triage.RunRequest) (triage.RunDecision, error) {
	s.lastReq = req
	s.calls++
	if s.err != nil {
		return triage.RunDecision{}, s.err
	}
	return s.decision, nil
}

func cachedRunTriageRequestForSubmit(session *Session, content string) triage.RunRequest {
	return submitRunTriageRequest(
		IncomingMessage{Content: content},
		session,
		"test-model",
		true,
		newSemanticSignal(content, session),
	)
}

func TestSubmitPassesLanguageProfileToRunTriage(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "planned",
			SuggestedDomains: []string{"browser"},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-language-profile",
		ExternalEventID: "evt-run-triage-language-profile",
		Content:         "Abre https://example.com y guarda un resumen en docs/tmp/resumen.md",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if triager.lastReq.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.LanguageHint = %q, want es", triager.lastReq.LanguageHint)
	}
	// MainSemanticPath is always true (no keyword fallback).
	if !triager.lastReq.MainSemanticPath {
		t.Fatalf("triager.lastReq.MainSemanticPath = %v, want true", triager.lastReq.MainSemanticPath)
	}
	if triager.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on run triage request")
	}
	if triager.lastReq.SemanticSignal.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.SemanticSignal.LanguageHint = %q, want es", triager.lastReq.SemanticSignal.LanguageHint)
	}
	if !triager.lastReq.SemanticSignal.MainSemanticPath {
		t.Fatalf("triager.lastReq.SemanticSignal.MainSemanticPath = %v, want true", triager.lastReq.SemanticSignal.MainSemanticPath)
	}
	if triager.lastReq.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("triager.lastReq.SemanticSignal.RequiresCurrentInfo = %v, want false", triager.lastReq.SemanticSignal.RequiresCurrentInfo)
	}
}

func TestNewSemanticSignalDoesNotSetCurrentInfo(t *testing.T) {
	t.Parallel()

	// Keyword-based RequiresCurrentInfo detection is removed;
	// newSemanticSignal never sets it — the model decides.
	signal := newSemanticSignal("What's the latest Go version today?", nil)
	if signal == nil {
		t.Fatal("expected semantic signal")
	}
	if signal.RequiresCurrentInfo {
		t.Fatalf("signal.RequiresCurrentInfo = true, want false (keyword heuristic removed)")
	}
}

func TestRunTriageSignatureIncludesSemanticHints(t *testing.T) {
	t.Parallel()

	base := triage.RunRequest{
		Model:            "test-model",
		Message:          "Abre la pagina y resume el contenido",
		SessionSummary:   "current page context",
		PlannerAvailable: true,
		LanguageHint:     "other",
		MainSemanticPath: true,
		SemanticSignal: &triage.RunSemanticSignal{
			LanguageHint:     "other",
			MainSemanticPath: true,
		},
	}

	withoutLanguage := base
	withoutLanguage.LanguageHint = ""
	if runTriageSignature(base) == runTriageSignature(withoutLanguage) {
		t.Fatal("expected run triage signature to change when language_hint changes")
	}

	withoutMainSemantic := base
	withoutMainSemantic.MainSemanticPath = false
	if runTriageSignature(base) == runTriageSignature(withoutMainSemantic) {
		t.Fatal("expected run triage signature to change when main_semantic_path changes")
	}

	withoutCurrentInfoHint := base
	withoutCurrentInfoHint.SemanticSignal = &triage.RunSemanticSignal{
		LanguageHint:        "other",
		MainSemanticPath:    true,
		RequiresCurrentInfo: true,
	}
	if runTriageSignature(base) == runTriageSignature(withoutCurrentInfoHint) {
		t.Fatal("expected run triage signature to change when semantic_signal freshness hint changes")
	}
}

func TestSubmitDoesNotSetCurrentInfoHintOnSemanticSignal(t *testing.T) {
	t.Parallel()

	// Keyword-based RequiresCurrentInfo detection is removed.
	// The semantic signal should not set RequiresCurrentInfo from keywords.
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode: "direct",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-current-info-signal",
		ExternalEventID: "evt-run-triage-current-info-signal",
		Content:         "What's the latest Go version today?",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if triager.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on run triage request")
	}
	if triager.lastReq.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("triager.lastReq.SemanticSignal.RequiresCurrentInfo = %v, want false (keyword heuristic removed)", triager.lastReq.SemanticSignal.RequiresCurrentInfo)
	}
}

func TestSubmitUsesProvidedSemanticSignalSeed(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode: "direct",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	seed := &SemanticSignal{
		Language: LanguageProfile{
			Family:           "es",
			Script:           "Latn",
			MainSemanticPath: true,
			Confidence:       0.93,
		},
		RequiresCurrentInfo: true,
		Reason:              "ingress_seed",
		Confidence:          0.93,
	}

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-seed",
		ExternalEventID: "evt-run-triage-seed",
		Content:         "Resume esta pagina en docs/tmp/resumen.md",
		SemanticSignal:  seed,
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if triager.lastReq.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.LanguageHint = %q, want es", triager.lastReq.LanguageHint)
	}
	if triager.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on run triage request")
	}
	if triager.lastReq.SemanticSignal.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.SemanticSignal.LanguageHint = %q, want es", triager.lastReq.SemanticSignal.LanguageHint)
	}
	if !triager.lastReq.SemanticSignal.MainSemanticPath {
		t.Fatalf("triager.lastReq.SemanticSignal.MainSemanticPath = %v, want true", triager.lastReq.SemanticSignal.MainSemanticPath)
	}
	if !triager.lastReq.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("triager.lastReq.SemanticSignal.RequiresCurrentInfo = %v, want true", triager.lastReq.SemanticSignal.RequiresCurrentInfo)
	}
	if seed.GeneratedAt != (time.Time{}) {
		t.Fatalf("seed.GeneratedAt = %v, want caller-owned seed to remain untouched", seed.GeneratedAt)
	}
}

func TestSubmitUsesRunTriageOnceForModeAndPreflight(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "watch",
			NeedsReference:   true,
			SuggestedDomains: []string{"watch", "browser"},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage",
		ExternalEventID: "evt-run-triage",
		Content:         "持续监控这个页面变化",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
	if run.ExecutionMode != ExecutionModeWatch {
		t.Fatalf("run.ExecutionMode = %q, want watch", run.ExecutionMode)
	}
	if run.Preflight == nil || !run.Preflight.Blocking {
		t.Fatalf("run.Preflight = %#v, want blocking preflight", run.Preflight)
	}
	if run.Triage == nil || run.Triage.Source != "model" {
		t.Fatalf("run.Triage = %#v", run.Triage)
	}
	if run.Triage.Mode != ExecutionModeWatch {
		t.Fatalf("run.Triage.Mode = %q, want watch", run.Triage.Mode)
	}
	if !run.Triage.NeedsReference {
		t.Fatal("expected triage trace to retain needs_reference")
	}
}

func TestSubmitCurrentInfoRequirementComesFromModelDecisionOnly(t *testing.T) {
	t.Parallel()

	// Keyword-based RequiresCurrentInfo is removed.
	// With domains ["search","web"] (not "news"/"watch") and no model signal,
	// RequiresCurrentInfo should be false.
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "direct",
			SuggestedDomains: []string{"search", "web"},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-current-info",
		ExternalEventID: "evt-run-triage-current-info",
		Content:         "What's the latest Go version today?",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Triage == nil {
		t.Fatal("expected triage trace")
	}
	if run.Triage.RequiresCurrentInfo {
		t.Fatalf("run.Triage.RequiresCurrentInfo = true, want false (keyword heuristic removed, domains are search/web not news/watch)")
	}
}

func TestSubmitUsesModelCurrentInfoSignalEvenWithoutFreshnessKeywords(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:       "direct",
			RequiresCurrentInfo: true,
			SuggestedDomains:    []string{"search"},
			Confidence:          0.91,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-model-current-info",
		ExternalEventID: "evt-run-triage-model-current-info",
		Content:         "Compare the two release channels and summarize the risk.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Triage == nil || !run.Triage.RequiresCurrentInfo {
		t.Fatalf("run.Triage = %#v, want requires_current_info=true", run.Triage)
	}
}

func TestSubmitReusesCachedRunTriageFromSession(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	session, err := sessions.GetOrCreate(context.Background(), "chat-run-triage-cache", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessions.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Metadata = map[string]any{
		sessionRunTriageCacheKey: cachedRunTriage{
			Signature: runTriageSignature(cachedRunTriageRequestForSubmit(locked, "持续监控这个页面变化")),
			Mode:      ExecutionModeWatch,
			Preflight: &RunPreflightReport{
				State:       RunPreflightNeedsConfirmation,
				Blocking:    true,
				Question:    "请提供具体链接。",
				GeneratedAt: time.Now().UTC(),
			},
			Trace: &RunTriageTrace{
				Source:            "model",
				Mode:              ExecutionModeWatch,
				NeedsReference:    true,
				NeedsConfirmation: false,
				Reason:            "cached decision",
				GeneratedAt:       time.Now().UTC(),
			},
			SavedAt: time.Now().UTC(),
		},
	}
	if err := sessions.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-cache",
		ExternalEventID: "evt-run-triage-cache",
		Content:         "持续监控这个页面变化",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if triager.calls != 0 {
		t.Fatalf("triager.calls = %d, want 0", triager.calls)
	}
	if run.ExecutionMode != ExecutionModeWatch {
		t.Fatalf("run.ExecutionMode = %q", run.ExecutionMode)
	}
	if run.Triage == nil || !run.Triage.CacheHit || run.Triage.Source != "session_cache" {
		t.Fatalf("run.Triage = %#v", run.Triage)
	}
	if run.Triage.Mode != ExecutionModeWatch {
		t.Fatalf("run.Triage.Mode = %q, want watch", run.Triage.Mode)
	}
	if !run.Triage.NeedsReference {
		t.Fatal("expected cached triage trace to retain needs_reference")
	}
}

func TestSubmitStoresRunTriageCacheForLaterReuse(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "planned",
			SuggestedDomains: []string{"browser", "spreadsheet"},
			Reason:           "multi_step",
			Confidence:       0.88,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	for i := 0; i < 2; i++ {
		_, err := component.Submit(context.Background(), IncomingMessage{
			SessionKey:      "chat-run-triage-reuse",
			ExternalEventID: "evt-run-triage-reuse-" + string(rune('a'+i)),
			Content:         "帮我抓数据整理成表格",
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
}

func TestSubmitReusesCachedRunTriageWhenCurrentInfoComesFromModelDecision(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:       "direct",
			RequiresCurrentInfo: true,
			SuggestedDomains:    []string{"search"},
			Reason:              "fresh_model_signal",
			Confidence:          0.92,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	for i := 0; i < 2; i++ {
		run, err := component.Submit(context.Background(), IncomingMessage{
			SessionKey:      "chat-run-triage-current-info-cache",
			ExternalEventID: "evt-run-triage-current-info-cache-" + string(rune('a'+i)),
			Content:         "Compare the two release channels and summarize the risk.",
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		if run.Triage == nil || !run.Triage.RequiresCurrentInfo {
			t.Fatalf("run.Triage = %#v, want requires_current_info=true", run.Triage)
		}
		if i == 1 && (!run.Triage.CacheHit || run.Triage.Source != "session_cache") {
			t.Fatalf("second run triage = %#v, want session_cache hit", run.Triage)
		}
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
}

func TestSubmitCachedRunTriagePreservesRawTriageModeBeforeTaskContractRefinement(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	triager := &staticRunTriage{
		decision: triage.RunDecision{
			ExecutionMode:    "direct",
			SuggestedDomains: []string{"document"},
			Reason:           "single_step_answer",
			Confidence:       0.78,
		},
	}
	taskContract := &countingTaskContractAnalyzer{
		analysis: TaskContractAnalysis{
			JobType:                taskContractJobDelivery,
			SuggestedDomains:       []string{"email", "document"},
			DeliverableKinds:       []string{taskDeliverableMessageDelivery, taskDeliverableDocument},
			MissingInfoIDs:         []string{},
			MissingInfoSpecified:   true,
			RequiresExternalEffect: boolPtr(true),
			RequiresApproval:       boolPtr(true),
			Confidence:             0.93,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager).
		WithTaskContractAnalyzer(taskContract)

	for i := 0; i < 2; i++ {
		run, err := component.Submit(context.Background(), IncomingMessage{
			SessionKey:      "chat-run-triage-cache-raw-mode",
			ExternalEventID: "evt-run-triage-cache-raw-mode-" + string(rune('a'+i)),
			Content:         "Send the prepared report to ceo@example.com",
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		if run.ExecutionMode != ExecutionModeWorkflow {
			t.Fatalf("run.ExecutionMode = %q, want workflow", run.ExecutionMode)
		}
		if run.Triage == nil {
			t.Fatal("expected triage trace")
		}
		if run.Triage.Mode != ExecutionModeDirect {
			t.Fatalf("run.Triage.Mode = %q, want raw triage mode direct", run.Triage.Mode)
		}
		if i == 1 && (!run.Triage.CacheHit || run.Triage.Source != "session_cache") {
			t.Fatalf("second run triage = %#v, want session_cache hit", run.Triage)
		}
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
}

func TestSubmitSkipsExpiredRunTriageCache(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	session, err := sessions.GetOrCreate(context.Background(), "chat-run-triage-expired", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessions.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Metadata = map[string]any{
		sessionRunTriageCacheKey: cachedRunTriage{
			Signature: runTriageSignature(cachedRunTriageRequestForSubmit(locked, "帮我抓数据整理成表格")),
			Mode:      ExecutionModePlanned,
			SavedAt:   time.Now().Add(-runTriageCacheTTL - time.Minute).UTC(),
		},
	}
	if err := sessions.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	triager := &staticRunTriage{
		decision: triage.RunDecision{ExecutionMode: "planned", Reason: "fresh"},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, NewInMemoryRunStore(), NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithRunTriage(triager)

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-run-triage-expired",
		ExternalEventID: "evt-run-triage-expired",
		Content:         "帮我抓数据整理成表格",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if triager.calls != 1 {
		t.Fatalf("triager.calls = %d, want 1", triager.calls)
	}
}

func TestLoadCachedRunTriageParsesMapMetadata(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	req := triage.RunRequest{
		Model:            "test-model",
		Message:          "持续监控这个页面变化",
		PlannerAvailable: true,
	}
	session := &Session{
		Metadata: map[string]any{
			sessionRunTriageCacheKey: map[string]any{
				"signature": runTriageSignature(req),
				"mode":      string(ExecutionModeWatch),
				"saved_at":  now.Format(time.RFC3339Nano),
				"preflight": map[string]any{
					"state":             string(RunPreflightNeedsConfirmation),
					"summary":           "Need a URL",
					"blocking":          true,
					"suggested_domains": []any{"watch", "browser"},
					"reply_hints":       []any{"发送网页链接"},
					"checks": []any{
						map[string]any{
							"id":       "source_target",
							"title":    "Provide URL",
							"state":    string(RunPreflightNeedsConfirmation),
							"blocking": true,
						},
					},
					"clarification_slots": []any{
						map[string]any{
							"id":       "source_target",
							"label":    "网页链接",
							"required": true,
							"hints":    []any{"https://example.com"},
						},
					},
					"generated_at": now.Format(time.RFC3339Nano),
				},
				"trace": map[string]any{
					"source":                "model",
					"mode":                  string(ExecutionModeWatch),
					"needs_reference":       true,
					"needs_confirmation":    false,
					"requires_current_info": true,
					"reason":                "cached",
					"confidence":            0.9,
					"suggested_domains":     []any{"watch", "browser"},
					"generated_at":          now.Format(time.RFC3339Nano),
				},
			},
		},
	}

	cached, ok := loadCachedRunTriage(session, req)
	if !ok {
		t.Fatal("loadCachedRunTriage() = false, want true")
	}
	if cached.Mode != ExecutionModeWatch {
		t.Fatalf("cached.Mode = %q", cached.Mode)
	}
	if cached.Preflight == nil || cached.Preflight.Checks[0].ID != "source_target" {
		t.Fatalf("cached.Preflight = %#v", cached.Preflight)
	}
	if cached.Trace == nil || cached.Trace.Reason != "cached" {
		t.Fatalf("cached.Trace = %#v", cached.Trace)
	}
	if cached.Trace.Mode != ExecutionModeWatch {
		t.Fatalf("cached.Trace.Mode = %q, want watch", cached.Trace.Mode)
	}
	if !cached.Trace.NeedsReference {
		t.Fatal("expected cached trace to include needs_reference")
	}
	if !cached.Trace.RequiresCurrentInfo {
		t.Fatal("expected cached trace to include requires_current_info")
	}

	cached.Preflight.SuggestedDomains[0] = "desktop"
	cached.Trace.SuggestedDomains[0] = "desktop"

	raw := session.Metadata[sessionRunTriageCacheKey].(map[string]any)
	preflight := raw["preflight"].(map[string]any)
	trace := raw["trace"].(map[string]any)
	if got := preflight["suggested_domains"].([]any)[0]; got != "watch" {
		t.Fatalf("raw preflight suggested_domains[0] = %#v", got)
	}
	if got := trace["suggested_domains"].([]any)[0]; got != "watch" {
		t.Fatalf("raw trace suggested_domains[0] = %#v", got)
	}
}

func TestBuildRunTriageTraceFromPreflightFallback(t *testing.T) {
	t.Parallel()

	report := &RunPreflightReport{
		State:            RunPreflightNeedsConfirmation,
		Blocking:         true,
		SuggestedDomains: []string{"browser", "search"},
		Checks: []RunPreflightCheck{
			{ID: "reference_gap", State: RunPreflightNeedsConfirmation, Blocking: true},
			{ID: "expected_confirmation", State: RunPreflightNeedsConfirmation},
		},
	}
	trace := buildRunTriageTrace("fallback", ExecutionModePlanned, "show me today's headlines", triageAnalysisFromPreflight(report, "fallback", 0))
	if trace == nil {
		t.Fatal("expected triage trace")
	}
	if trace.Mode != ExecutionModePlanned {
		t.Fatalf("trace.Mode = %q, want planned", trace.Mode)
	}
	if !trace.NeedsReference {
		t.Fatal("expected needs_reference from fallback preflight")
	}
	if !trace.NeedsConfirmation {
		t.Fatal("expected needs_confirmation from fallback preflight")
	}
	if len(trace.SuggestedDomains) != 2 || trace.SuggestedDomains[0] != "browser" {
		t.Fatalf("trace.SuggestedDomains = %#v", trace.SuggestedDomains)
	}
	// Keyword-based RequiresCurrentInfo detection is removed.
	// Domains ["browser","search"] are not "news"/"watch", so RequiresCurrentInfo=false.
	if trace.RequiresCurrentInfo {
		t.Fatalf("trace.RequiresCurrentInfo = true, want false (keyword heuristic removed)")
	}
}

func TestLoadCachedRunTriageRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	req := triage.RunRequest{
		Model:            "test-model",
		Message:          "持续监控这个页面变化",
		PlannerAvailable: true,
	}
	session := &Session{
		Metadata: map[string]any{
			sessionRunTriageCacheKey: map[string]any{
				"signature": runTriageSignature(req),
			},
		},
	}

	if cached, ok := loadCachedRunTriage(session, req); ok || cached != nil {
		t.Fatalf("loadCachedRunTriage() = %#v, %v; want nil, false", cached, ok)
	}
}
