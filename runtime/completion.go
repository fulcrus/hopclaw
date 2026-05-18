package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

const runCompletionReceiptLimit = 64

// DeliveryReceipt is a canonical delivery attempt/result record attached to a run completion.
type DeliveryReceipt struct {
	Kind           string             `json:"kind,omitempty"`
	AdapterName    string             `json:"adapter_name,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	Status         string             `json:"status,omitempty"`
	Summary        string             `json:"summary,omitempty"`
	Error          string             `json:"error,omitempty"`
	EventID        string             `json:"event_id,omitempty"`
	EventType      eventbus.EventType `json:"event_type,omitempty"`
	Attempts       int                `json:"attempts,omitempty"`
	MaxAttempts    int                `json:"max_attempts,omitempty"`
	LastAttemptAt  time.Time          `json:"last_attempt_at,omitempty"`
	NextAttemptAt  time.Time          `json:"next_attempt_at,omitempty"`
	DeliveredAt    time.Time          `json:"delivered_at,omitempty"`
	UpdatedAt      time.Time          `json:"updated_at,omitempty"`
}

// RunCompletion is the canonical product-completion read model for a run.
type RunCompletion struct {
	RunID        string                    `json:"run_id"`
	SessionID    string                    `json:"session_id,omitempty"`
	Status       agent.RunStatus           `json:"status"`
	Outcome      RunOutcome                `json:"outcome,omitempty"`
	Governance   *GovernanceReceipt        `json:"governance,omitempty"`
	Result       *RunResult                `json:"result,omitempty"`
	Verification *verifyrt.RunVerification `json:"verification,omitempty"`
	Delivery     *DeliveryPlan             `json:"delivery,omitempty"`
	Bundle       *ResultBundle             `json:"bundle,omitempty"`
	Receipts     []DeliveryReceipt         `json:"receipts,omitempty"`
	UpdatedAt    string                    `json:"updated_at,omitempty"`
}

type runCompletionState struct {
	run          *agent.Run
	session      *agent.Session
	result       *RunResult
	verification *verifyrt.RunVerification
}

func (s *Service) GetRunCompletion(ctx context.Context, id string) (*RunCompletion, error) {
	state, err := s.buildRunCompletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	return state.view(), nil
}

func (s *Service) buildRunCompletionState(ctx context.Context, id string) (*runCompletionState, error) {
	base, err := s.getRunResultBase(ctx, id)
	if err != nil {
		return nil, err
	}
	verification, err := s.getRunVerification(ctx, base.run, base.result, base.session)
	if err != nil {
		return nil, err
	}
	applyRunOutcome(base.result, base.run, verification)
	governance, err := s.buildGovernanceReceipt(ctx, base.run)
	if err != nil {
		return nil, err
	}
	base.result.Governance = governance
	receipts, err := s.buildDeliveryReceipts(ctx, base.run)
	if err != nil {
		return nil, err
	}
	base.result.Receipts = receipts
	base.result.Delivery = buildDeliveryEnvelope(base.result, base.run, base.session, verification)
	base.result.Bundle = buildResultBundle(base.run, base.result, verification)
	base.result.Canonical = buildAutomationResult(base.run, base.result, verification)
	return &runCompletionState{
		run:          base.run,
		session:      base.session,
		result:       base.result,
		verification: verification,
	}, nil
}

func (s *runCompletionState) view() *RunCompletion {
	if s == nil || s.result == nil {
		return nil
	}
	return &RunCompletion{
		RunID:        s.result.RunID,
		SessionID:    s.result.SessionID,
		Status:       s.result.Status,
		Outcome:      s.result.Outcome,
		Governance:   s.result.Governance,
		Result:       s.result,
		Verification: s.verification,
		Delivery:     s.result.Delivery,
		Bundle:       s.result.Bundle,
		Receipts:     cloneDeliveryReceipts(s.result.Receipts),
		UpdatedAt:    bundleUpdatedAt(s.run),
	}
}

func (s *Service) buildDeliveryReceipts(ctx context.Context, run *agent.Run) ([]DeliveryReceipt, error) {
	if s == nil || run == nil || strings.TrimSpace(run.ID) == "" {
		return nil, nil
	}
	items, err := s.ListGovernanceDeliveries(ctx, GovernanceDeliveryFilter{
		RunID: run.ID,
		Limit: runCompletionReceiptLimit,
	})
	if err != nil {
		if errors.Is(err, ErrGovernanceDeliveryControllerNil) {
			return nil, nil
		}
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]DeliveryReceipt, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, deliveryReceiptFromGovernanceView(item))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func deliveryReceiptFromGovernanceView(item *GovernanceDeliveryView) DeliveryReceipt {
	if item == nil {
		return DeliveryReceipt{}
	}
	return DeliveryReceipt{
		Kind:           strings.TrimSpace(string(item.Record.Kind)),
		AdapterName:    strings.TrimSpace(item.AdapterName),
		IdempotencyKey: strings.TrimSpace(item.IdempotencyKey),
		Status:         strings.TrimSpace(string(item.Status)),
		Summary:        normalize.FirstNonEmpty(strings.TrimSpace(item.Record.Summary), strings.TrimSpace(string(item.Record.EventType)), strings.TrimSpace(item.LastError)),
		Error:          strings.TrimSpace(item.LastError),
		EventID:        strings.TrimSpace(item.Record.EventID),
		EventType:      item.Record.EventType,
		Attempts:       item.Attempts,
		MaxAttempts:    item.MaxAttempts,
		LastAttemptAt:  item.LastAttemptAt.UTC(),
		NextAttemptAt:  item.NextAttemptAt.UTC(),
		DeliveredAt:    item.DeliveredAt.UTC(),
		UpdatedAt:      item.UpdatedAt.UTC(),
	}
}

func cloneDeliveryReceipts(items []DeliveryReceipt) []DeliveryReceipt {
	if len(items) == 0 {
		return nil
	}
	out := make([]DeliveryReceipt, len(items))
	copy(out, items)
	return out
}
