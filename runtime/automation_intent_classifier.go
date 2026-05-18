package runtime

import (
	"context"

	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

type AutomationIntentClassifyRequest struct {
	SessionKey     string                           `json:"session_key"`
	Message        string                           `json:"message"`
	Model          string                           `json:"model,omitempty"`
	SessionState   InteractionSessionState          `json:"session_state,omitempty"`
	RecentMessages []contextengine.Message          `json:"recent_messages,omitempty"`
	Inventory      []automationintent.InventoryItem `json:"inventory,omitempty"`
}

type AutomationIntentClassifier interface {
	Analyze(ctx context.Context, req AutomationIntentClassifyRequest) (automationintent.Plan, error)
}

type TriageAutomationIntentClassifier struct {
	engine *triage.ModelTriage
}

func NewTriageAutomationIntentClassifier(engine *triage.ModelTriage) *TriageAutomationIntentClassifier {
	if engine == nil {
		return nil
	}
	return &TriageAutomationIntentClassifier{engine: engine}
}

func (c *TriageAutomationIntentClassifier) Analyze(ctx context.Context, req AutomationIntentClassifyRequest) (automationintent.Plan, error) {
	if c == nil || c.engine == nil {
		return automationintent.Plan{}, context.Canceled
	}
	plan, err := c.engine.AnalyzeAutomationIntent(ctx, triage.AutomationIntentRequest{
		Model:          req.Model,
		Message:        req.Message,
		SessionState:   req.SessionState.String(),
		RecentMessages: triageRecentMessagesFromContext(req.RecentMessages),
		Inventory:      req.Inventory,
	})
	if err != nil {
		return automationintent.Plan{}, err
	}
	return plan.Normalized(), nil
}
