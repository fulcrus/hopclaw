package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

const interactionIngressTimeout = 5 * time.Second

type InteractionIngressClassifyRequest struct {
	SessionKey      string                           `json:"session_key"`
	Message         string                           `json:"message"`
	Model           string                           `json:"model,omitempty"`
	SessionState    InteractionSessionState          `json:"session_state,omitempty"`
	ActiveRun       *agent.Run                       `json:"active_run,omitempty"`
	PendingApproval *approval.Ticket                 `json:"pending_approval,omitempty"`
	WaitingInput    bool                             `json:"waiting_input"`
	WaitingApproval bool                             `json:"waiting_approval"`
	RecentMessages  []contextengine.Message          `json:"recent_messages,omitempty"`
	Inventory       []automationintent.InventoryItem `json:"inventory,omitempty"`
}

type InteractionIngressClassification struct {
	Decision       InteractionDecision   `json:"decision"`
	AutomationPlan automationintent.Plan `json:"automation_plan,omitempty"`
	SemanticSignal *agent.SemanticSignal `json:"semantic_signal,omitempty"`
}

type InteractionIngressClassifier interface {
	Analyze(ctx context.Context, req InteractionIngressClassifyRequest) (InteractionIngressClassification, error)
}

type TriageInteractionIngressClassifier struct {
	engine *triage.ModelTriage
}

func NewTriageInteractionIngressClassifier(engine *triage.ModelTriage) *TriageInteractionIngressClassifier {
	if engine == nil {
		return nil
	}
	return &TriageInteractionIngressClassifier{engine: engine}
}

func (c *TriageInteractionIngressClassifier) Analyze(ctx context.Context, req InteractionIngressClassifyRequest) (InteractionIngressClassification, error) {
	if c == nil || c.engine == nil {
		return InteractionIngressClassification{}, context.Canceled
	}
	classification, err := c.engine.AnalyzeInteractionIngress(ctx, triage.InteractionIngressRequest{
		Model:           req.Model,
		Message:         req.Message,
		SessionState:    req.SessionState.String(),
		ActiveRun:       triageRunStateFromRun(req.ActiveRun),
		PendingApproval: triageApprovalStateFromTicket(req.PendingApproval),
		WaitingInput:    req.WaitingInput,
		WaitingApproval: req.WaitingApproval,
		RecentMessages:  triageRecentMessagesFromContext(req.RecentMessages),
		Inventory:       append([]automationintent.InventoryItem(nil), req.Inventory...),
	})
	if err != nil {
		return InteractionIngressClassification{}, err
	}
	return InteractionIngressClassification{
		Decision: InteractionDecision{
			SpeechAct:   IncomingSpeechAct(classification.SpeechAct),
			TargetScope: TargetScope(classification.TargetScope),
			ReplyAct:    ReplyAct(classification.ReplyAct),
			Reason:      classification.Reason,
			Confidence:  classification.Confidence,
		},
		AutomationPlan: classification.AutomationPlan.Normalized(),
		SemanticSignal: interactionIngressSemanticSignal(req.Message, classification),
	}, nil
}

func (s *Service) classifyIngress(ctx context.Context, req InteractionRequest, message string, snap InteractionContextSnapshot, sessionKey, model string) (InteractionIngressClassification, error) {
	if s == nil || s.ingressClassifier == nil {
		return InteractionIngressClassification{}, context.Canceled
	}

	classifyReq := InteractionIngressClassifyRequest{
		SessionKey:      sessionKey,
		Message:         message,
		Model:           model,
		SessionState:    snap.SessionState,
		WaitingInput:    snap.WaitingInput,
		WaitingApproval: snap.WaitingApproval,
	}
	if snap.ActiveRunID != "" {
		if run, err := s.runs.Get(ctx, snap.ActiveRunID); err == nil {
			classifyReq.ActiveRun = run
		}
	}
	if snap.PendingTicketID != "" && s.approvals != nil {
		if ticket, err := s.approvals.Get(ctx, snap.PendingTicketID); err == nil {
			classifyReq.PendingApproval = ticket
		}
	}
	if snap.SessionID != "" {
		classifyReq.RecentMessages = s.loadRecentMessages(ctx, snap.SessionID, interactRecentMessageLimit)
	}
	if !snap.HasActiveRun && !snap.WaitingApproval && !snap.WaitingInput {
		if inventory, err := s.loadAutomationIntentInventory(ctx, sessionKey, model, req); err == nil && len(inventory) > 0 {
			classifyReq.Inventory = inventory
		}
	}

	classifyCtx, cancel := context.WithTimeout(ctx, interactionIngressTimeout)
	defer cancel()

	classification, err := s.ingressClassifier.Analyze(classifyCtx, classifyReq)
	if err != nil {
		return InteractionIngressClassification{}, err
	}
	classification.AutomationPlan = classification.AutomationPlan.Normalized()
	if classification.Decision.SpeechAct == "" || classification.Decision.ReplyAct == "" {
		if !automationPlanNeedsHandling(classification.AutomationPlan) {
			return InteractionIngressClassification{}, fmt.Errorf("interaction ingress returned an empty decision")
		}
	}
	return classification, nil
}

func automationPlanNeedsHandling(plan automationintent.Plan) bool {
	plan = plan.Normalized()
	return plan.Actionable() || plan.NeedConfirmation || len(plan.MissingInfo) > 0
}

func interactionIngressSemanticSignal(message string, decision triage.InteractionIngressDecision) *agent.SemanticSignal {
	signal := &agent.SemanticSignal{
		Message:             strings.TrimSpace(message),
		RequiresCurrentInfo: decision.RequiresCurrentInfo,
		Reason:              strings.TrimSpace(decision.Reason),
		Confidence:          decision.Confidence,
		GeneratedAt:         time.Now().UTC(),
	}
	signal.Language = agent.LanguageProfile{
		MainSemanticPath: true,
	}
	if decision.Language != nil {
		signal.Language.Family = strings.TrimSpace(decision.Language.Family)
		signal.Language.Script = strings.TrimSpace(decision.Language.Script)
		signal.Language.Confidence = decision.Language.Confidence
	}
	return signal
}
