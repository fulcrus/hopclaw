package runtime

import (
	"context"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

type TriageInteractionClassifier struct {
	engine *triage.ModelTriage
}

func NewTriageInteractionClassifier(engine *triage.ModelTriage) *TriageInteractionClassifier {
	if engine == nil {
		return nil
	}
	return &TriageInteractionClassifier{engine: engine}
}

func (c *TriageInteractionClassifier) Classify(ctx context.Context, req InteractionClassifyRequest) (InteractionDecision, error) {
	if c == nil || c.engine == nil {
		return InteractionDecision{}, context.Canceled
	}
	decision, err := c.engine.ClassifyInteraction(ctx, triage.InteractionRequest{
		Model:           req.Model,
		Message:         req.Message,
		SessionState:    req.SessionState.String(),
		ActiveRun:       triageRunStateFromRun(req.ActiveRun),
		PendingApproval: triageApprovalStateFromTicket(req.PendingApproval),
		WaitingInput:    req.WaitingInput,
		WaitingApproval: req.WaitingApproval,
		RecentMessages:  triageRecentMessagesFromContext(req.RecentMessages),
	})
	if err != nil {
		return InteractionDecision{}, err
	}
	return InteractionDecision{
		SpeechAct:   IncomingSpeechAct(decision.SpeechAct),
		TargetScope: TargetScope(decision.TargetScope),
		ReplyAct:    ReplyAct(decision.ReplyAct),
		Reason:      decision.Reason,
		Confidence:  decision.Confidence,
	}, nil
}

func triageRunStateFromRun(run *agent.Run) *triage.IngressRunState {
	if run == nil {
		return nil
	}
	return &triage.IngressRunState{
		ID:         run.ID,
		Status:     string(run.Status),
		Phase:      string(run.Phase),
		ToolRounds: run.ToolRounds,
	}
}

func triageApprovalStateFromTicket(ticket *approval.Ticket) *triage.IngressApprovalState {
	if ticket == nil {
		return nil
	}
	return &triage.IngressApprovalState{
		ID:   ticket.ID,
		Kind: string(ticket.Kind),
	}
}

func triageRecentMessagesFromContext(messages []contextengine.Message) []triage.IngressMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]triage.IngressMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, triage.IngressMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return out
}
