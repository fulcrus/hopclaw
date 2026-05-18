package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

type stubInteractionIngressClassifier struct {
	classification InteractionIngressClassification
	err            error
	calls          int
	lastRequest    InteractionIngressClassifyRequest
}

func (s *stubInteractionIngressClassifier) Analyze(_ context.Context, req InteractionIngressClassifyRequest) (InteractionIngressClassification, error) {
	s.calls++
	s.lastRequest = req
	if s.err != nil {
		return InteractionIngressClassification{}, s.err
	}
	return s.classification, nil
}

type countingInteractionClassifier struct {
	decision InteractionDecision
	err      error
	calls    int
}

func (c *countingInteractionClassifier) Classify(context.Context, InteractionClassifyRequest) (InteractionDecision, error) {
	c.calls++
	if c.err != nil {
		return InteractionDecision{}, c.err
	}
	return c.decision, nil
}

type countingAutomationClassifier struct {
	plan  automationintent.Plan
	err   error
	calls int
}

func (c *countingAutomationClassifier) Analyze(context.Context, AutomationIntentClassifyRequest) (automationintent.Plan, error) {
	c.calls++
	if c.err != nil {
		return automationintent.Plan{}, c.err
	}
	return c.plan, nil
}

type recordingRunTriage struct {
	decision triage.RunDecision
	lastReq  triage.RunRequest
}

func (r *recordingRunTriage) AnalyzeRun(_ context.Context, req triage.RunRequest) (triage.RunDecision, error) {
	r.lastReq = req
	return r.decision, nil
}

func TestInteractUsesUnifiedIngressForChatReply(t *testing.T) {
	t.Parallel()

	model := &recordingInteractionModelClient{
		response: testModelResponse("I am HopClaw."),
	}
	unified := &stubInteractionIngressClassifier{
		classification: InteractionIngressClassification{
			Decision: InteractionDecision{
				SpeechAct:   SpeechActMetaQuestion,
				TargetScope: TargetScopeNone,
				ReplyAct:    ReplyActChatReply,
				Confidence:  0.98,
				Reason:      "meta_question",
			},
		},
	}
	legacyInteraction := &countingInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.99,
		},
	}
	legacyAutomation := &countingAutomationClassifier{
		plan: automationintent.Plan{
			Action:     automationintent.ActionQuery,
			Confidence: 0.99,
			Query: automationintent.Query{
				Metric: automationintent.QueryMetricSummary,
			},
		},
	}

	svc := newInteractiveServiceWithModel(model).
		WithIngressClassifier(unified).
		WithClassifier(legacyInteraction).
		WithAutomationClassifier(legacyAutomation)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "unified-ingress-chat",
		Content:    "who are you?",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if result.ReplyMessage != "I am HopClaw." {
		t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, "I am HopClaw.")
	}
	if unified.calls != 1 {
		t.Fatalf("unified calls = %d, want 1", unified.calls)
	}
	if legacyInteraction.calls != 0 {
		t.Fatalf("legacy interaction calls = %d, want 0", legacyInteraction.calls)
	}
	if legacyAutomation.calls != 0 {
		t.Fatalf("legacy automation calls = %d, want 0", legacyAutomation.calls)
	}
}

func TestInteractUsesUnifiedIngressForAutomationIntent(t *testing.T) {
	t.Parallel()

	exec := &automationIntentStubExecutor{
		items: []automationIntentStubItem{{
			ID:                       "cron-news",
			Kind:                     "cron",
			Name:                     "Daily news",
			Enabled:                  true,
			Schedule:                 "0 8 * * *",
			NotificationTodayCount:   2,
			NotificationFailureCount: 1,
			NotificationTodayDate:    time.Now().UTC().Format("2006-01-02"),
		}},
	}
	unified := &stubInteractionIngressClassifier{
		classification: InteractionIngressClassification{
			Decision: InteractionDecision{
				SpeechAct:   SpeechActCommand,
				TargetScope: TargetScopeSession,
				ReplyAct:    ReplyActChatReply,
				Confidence:  0.93,
				Reason:      "automation_intent_query",
			},
			AutomationPlan: automationintent.Plan{
				Action:     automationintent.ActionQuery,
				Confidence: 0.93,
				Query: automationintent.Query{
					Metric: automationintent.QueryMetricNotificationToday,
				},
			},
		},
	}
	legacyInteraction := &countingInteractionClassifier{}
	legacyAutomation := &countingAutomationClassifier{}

	svc := newAutomationIntentService(t, exec).
		WithIngressClassifier(unified).
		WithClassifier(legacyInteraction).
		WithAutomationClassifier(legacyAutomation)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "unified-ingress-automation",
		Content:    "今天一共给我发了多少通知消息了？",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if !strings.Contains(result.ReplyMessage, "今天已发送 2 条通知") {
		t.Fatalf("ReplyMessage = %q", result.ReplyMessage)
	}
	if unified.calls != 1 {
		t.Fatalf("unified calls = %d, want 1", unified.calls)
	}
	if legacyInteraction.calls != 0 {
		t.Fatalf("legacy interaction calls = %d, want 0", legacyInteraction.calls)
	}
	if legacyAutomation.calls != 0 {
		t.Fatalf("legacy automation calls = %d, want 0", legacyAutomation.calls)
	}
}

func TestInteractFallsBackToNoRunConversationWhenUnifiedClassifierFails(t *testing.T) {
	t.Parallel()

	model := &recordingInteractionModelClient{
		response: testModelResponse("fallback conversation reply"),
	}
	unified := &stubInteractionIngressClassifier{err: errors.New("unified ingress failed")}
	legacyInteraction := &countingInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActMetaQuestion,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.92,
			Reason:      "legacy_meta_question",
		},
	}

	svc := newInteractiveServiceWithModel(model).
		WithIngressClassifier(unified).
		WithClassifier(legacyInteraction)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "unified-ingress-fallback",
		Content:    "who are you?",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if result.ReplyMessage != "fallback conversation reply" {
		t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, "fallback conversation reply")
	}
	if unified.calls != 1 {
		t.Fatalf("unified calls = %d, want 1", unified.calls)
	}
	if legacyInteraction.calls != 0 {
		t.Fatalf("legacy interaction calls = %d, want 0", legacyInteraction.calls)
	}
}

func TestInteractUsesEffectiveContentForUnifiedIngress(t *testing.T) {
	t.Parallel()

	model := &recordingInteractionModelClient{
		response: testModelResponse("I can read block text."),
	}
	unified := &stubInteractionIngressClassifier{
		classification: InteractionIngressClassification{
			Decision: InteractionDecision{
				SpeechAct:   SpeechActMetaQuestion,
				TargetScope: TargetScopeNone,
				ReplyAct:    ReplyActChatReply,
				Confidence:  0.94,
				Reason:      "meta_question_blocks",
			},
		},
	}

	svc := newInteractiveServiceWithModel(model).
		WithIngressClassifier(unified)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "unified-ingress-blocks",
		ContentBlocks: []contextengine.ContentBlock{{
			Type: contextengine.ContentBlockText,
			Text: "who are you?",
		}},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if unified.lastRequest.Message != "who are you?" {
		t.Fatalf("unified last message = %q, want %q", unified.lastRequest.Message, "who are you?")
	}
	if result.ReplyMessage != "I can read block text." {
		t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, "I can read block text.")
	}
}

func TestTriageInteractionIngressClassifierBuildsSemanticSignal(t *testing.T) {
	t.Parallel()

	engine := triage.NewModelTriage(func(_ context.Context, _ triage.ChatRequest) (string, error) {
		return `{
			"speech_act":"new_task",
			"target_scope":"new_run",
			"reply_act":"task_accept",
			"requires_current_info":true,
			"language":{"family":"es","script":"Latn","confidence":0.91},
			"reason":"fresh_research",
			"confidence":0.91,
			"automation_plan":{"action":"none"}
		}`, nil
	}, 0)
	classifier := NewTriageInteractionIngressClassifier(engine)

	classification, err := classifier.Analyze(context.Background(), InteractionIngressClassifyRequest{
		Message: "Resume esta pagina y verifica si hay cambios recientes",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if classification.SemanticSignal == nil {
		t.Fatal("expected semantic signal")
	}
	if classification.SemanticSignal.Language.Family != "es" {
		t.Fatalf("SemanticSignal.Language.Family = %q, want es", classification.SemanticSignal.Language.Family)
	}
	if classification.SemanticSignal.Language.Script != "Latn" {
		t.Fatalf("SemanticSignal.Language.Script = %q, want Latn", classification.SemanticSignal.Language.Script)
	}
	if !classification.SemanticSignal.Language.MainSemanticPath {
		t.Fatalf("SemanticSignal.Language.MainSemanticPath = %v, want true", classification.SemanticSignal.Language.MainSemanticPath)
	}
	if !classification.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("SemanticSignal.RequiresCurrentInfo = %v, want true", classification.SemanticSignal.RequiresCurrentInfo)
	}
}

func TestInteractPassesUnifiedIngressSemanticSignalToSubmit(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	triager := &recordingRunTriage{
		decision: triage.RunDecision{
			ExecutionMode: "direct",
		},
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
		WithRunTriage(triager).
		WithPreflightAnalyzer(testPreflightAnalyzer{})
	svc := NewService(component, sessions, runs, approvals, nil, nil)

	unified := &stubInteractionIngressClassifier{
		classification: InteractionIngressClassification{
			Decision: InteractionDecision{
				SpeechAct:   SpeechActNewTask,
				TargetScope: TargetScopeNewRun,
				ReplyAct:    ReplyActTaskAccept,
				Confidence:  0.93,
				Reason:      "fresh_research",
			},
			SemanticSignal: &agent.SemanticSignal{
				Language: agent.LanguageProfile{
					Family:           "es",
					Script:           "Latn",
					MainSemanticPath: true,
					Confidence:       0.93,
				},
				RequiresCurrentInfo: true,
				Reason:              "fresh_research",
				Confidence:          0.93,
			},
		},
	}
	svc.WithIngressClassifier(unified)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "unified-ingress-submit-seed",
		Content:    "Resume esta pagina y verifica si hay cambios recientes",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.SubmitRequest == nil {
		t.Fatal("expected submit request")
	}
	if result.SubmitRequest.SemanticSignal == nil {
		t.Fatal("expected semantic signal on submit request")
	}
	if result.SubmitRequest.SemanticSignal.Language.Family != "es" {
		t.Fatalf("SubmitRequest.SemanticSignal.Language.Family = %q, want es", result.SubmitRequest.SemanticSignal.Language.Family)
	}
	if !result.SubmitRequest.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("SubmitRequest.SemanticSignal.RequiresCurrentInfo = %v, want true", result.SubmitRequest.SemanticSignal.RequiresCurrentInfo)
	}
	if triager.lastReq.LanguageHint != "es" {
		t.Fatalf("triager.lastReq.LanguageHint = %q, want es", triager.lastReq.LanguageHint)
	}
	if triager.lastReq.SemanticSignal == nil {
		t.Fatal("expected semantic signal on run triage request")
	}
	if !triager.lastReq.SemanticSignal.RequiresCurrentInfo {
		t.Fatalf("triager.lastReq.SemanticSignal.RequiresCurrentInfo = %v, want true", triager.lastReq.SemanticSignal.RequiresCurrentInfo)
	}
}

func testModelResponse(content string) *agent.ModelResponse {
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: content},
	}
}
