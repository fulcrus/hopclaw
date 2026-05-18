package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
)

type ReleaseExecutionGateMode string

const (
	ReleaseExecutionGateModeDisabled        ReleaseExecutionGateMode = ""
	ReleaseExecutionGateModeAdvisory        ReleaseExecutionGateMode = "advisory"
	ReleaseExecutionGateModeRequireApproval ReleaseExecutionGateMode = "require_approval"
	ReleaseExecutionGateModeBlock           ReleaseExecutionGateMode = "block"
)

type releaseGateContextKeyType struct{}

var releaseGateBypassContextKey = releaseGateContextKeyType{}

const (
	releaseGatePolicySource      = "runtime.release_readiness_gate"
	releaseGatePolicyLayer       = "quality_release_readiness"
	releaseGateMetadataKey       = "release_gate"
	releaseGateMetadataModeKey   = "release_gate_mode"
	releaseGateMetadataReadyKey  = "release_gate_ready"
	releaseGateMetadataTimeKey   = "release_gate_generated_at"
	releaseGateMetadataBlockerID = "release_gate_blocker_ids"
)

type ReleaseExecutionGatePolicy struct {
	Mode       ReleaseExecutionGateMode  `json:"mode,omitempty"`
	Thresholds ReleaseReadinessThresholds `json:"thresholds,omitempty"`
}

type releaseExecutionGateDecision struct {
	mode    ReleaseExecutionGateMode
	report  *ReleaseReadinessReport
	summary string
	reasons []string
}

func DefaultReleaseExecutionGatePolicy() ReleaseExecutionGatePolicy {
	return ReleaseExecutionGatePolicy{
		Mode:       ReleaseExecutionGateModeRequireApproval,
		Thresholds: DefaultReleaseReadinessThresholds(),
	}
}

func normalizeReleaseExecutionGatePolicy(policy ReleaseExecutionGatePolicy) ReleaseExecutionGatePolicy {
	switch ReleaseExecutionGateMode(strings.ToLower(strings.TrimSpace(string(policy.Mode)))) {
	case ReleaseExecutionGateModeAdvisory:
		policy.Mode = ReleaseExecutionGateModeAdvisory
	case ReleaseExecutionGateModeRequireApproval:
		policy.Mode = ReleaseExecutionGateModeRequireApproval
	case ReleaseExecutionGateModeBlock:
		policy.Mode = ReleaseExecutionGateModeBlock
	default:
		policy.Mode = ReleaseExecutionGateModeDisabled
	}
	if policy.Mode != ReleaseExecutionGateModeDisabled && policy.Thresholds == (ReleaseReadinessThresholds{}) {
		policy.Thresholds = DefaultReleaseReadinessThresholds()
	}
	return policy
}

func releaseGateDispatchContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, releaseGateBypassContextKey, true)
}

func skipReleaseExecutionGate(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value := ctx.Value(releaseGateBypassContextKey)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		ok, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return ok
	default:
		return false
	}
}

func (s *Service) applyReleaseExecutionGate(ctx context.Context, run *agent.Run) (bool, error) {
	if s == nil || run == nil || skipReleaseExecutionGate(ctx) {
		return false, nil
	}
	policy := normalizeReleaseExecutionGatePolicy(s.releaseGatePolicy)
	if policy.Mode == ReleaseExecutionGateModeDisabled || policy.Mode == ReleaseExecutionGateModeAdvisory {
		return false, nil
	}
	if !releaseExecutionGateApplies(run) {
		return false, nil
	}
	decision, err := s.evaluateReleaseExecutionGate(ctx, run, policy)
	if err != nil || decision == nil {
		return false, err
	}
	switch decision.mode {
	case ReleaseExecutionGateModeRequireApproval:
		if err := s.holdRunForReleaseExecutionApproval(ctx, run, decision); err != nil {
			return false, err
		}
		return true, nil
	case ReleaseExecutionGateModeBlock:
		return false, fmt.Errorf("%s", decision.summary)
	default:
		return false, nil
	}
}

func releaseExecutionGateApplies(run *agent.Run) bool {
	if run == nil || run.Status != agent.RunQueued || !run.StartedAt.IsZero() || run.TaskContract == nil {
		return false
	}
	return run.TaskContract.RequiresExternalEffect || run.TaskContract.RequiresApproval
}

func (s *Service) evaluateReleaseExecutionGate(ctx context.Context, run *agent.Run, policy ReleaseExecutionGatePolicy) (*releaseExecutionGateDecision, error) {
	if run == nil {
		return nil, nil
	}
	req := ReleaseReadinessRequest{
		Thresholds: policy.Thresholds,
	}
	if automationID := strings.TrimSpace(run.Scope.AutomationID); automationID != "" {
		req.SummaryRequest.Scope = agent.ScopeFilter{AutomationIDs: []string{automationID}}
	} else if sessionID := strings.TrimSpace(run.SessionID); sessionID != "" {
		req.SummaryRequest.SessionID = sessionID
	}
	report, err := s.GetReleaseReadiness(ctx, req)
	if err != nil {
		return nil, err
	}
	if report == nil || report.Ready {
		return nil, nil
	}
	return &releaseExecutionGateDecision{
		mode:    policy.Mode,
		report:  report,
		summary: releaseGateSummary(report),
		reasons: releaseGateReasons(report),
	}, nil
}

func releaseGateSummary(report *ReleaseReadinessReport) string {
	if report == nil || len(report.Blockers) == 0 {
		return "release readiness blocked high-risk execution"
	}
	if len(report.Blockers) == 1 {
		return "release readiness blocked high-risk execution: " + strings.TrimSpace(report.Blockers[0].Summary)
	}
	return fmt.Sprintf("release readiness blocked high-risk execution: %d blockers", len(report.Blockers))
}

func releaseGateReasons(report *ReleaseReadinessReport) []string {
	if report == nil || len(report.Blockers) == 0 {
		return []string{"release readiness blocked high-risk execution"}
	}
	out := make([]string, 0, len(report.Blockers))
	for _, blocker := range report.Blockers {
		if summary := strings.TrimSpace(blocker.Summary); summary != "" {
			out = append(out, summary)
		}
	}
	if len(out) == 0 {
		out = append(out, "release readiness blocked high-risk execution")
	}
	return normalize.DedupeStrings(out)
}

func releaseGateBlockerIDs(report *ReleaseReadinessReport) []string {
	if report == nil || len(report.Blockers) == 0 {
		return nil
	}
	out := make([]string, 0, len(report.Blockers))
	for _, blocker := range report.Blockers {
		if id := strings.TrimSpace(blocker.ID); id != "" {
			out = append(out, id)
		}
	}
	return normalize.DedupeStrings(out)
}

func (s *Service) holdRunForReleaseExecutionApproval(ctx context.Context, run *agent.Run, decision *releaseExecutionGateDecision) error {
	if s == nil || run == nil || decision == nil {
		return nil
	}
	if s.approvals == nil {
		return agent.ErrApprovalStoreNil
	}
	if current, err := s.approvals.GetByRun(ctx, run.ID); err == nil && current != nil && current.Status == approval.StatusPending && releaseGateApprovalTicket(current) {
		return s.applyExistingReleaseGateHold(ctx, run, current, decision)
	}

	metadata := releaseGateApprovalMetadata(run, decision)
	ticket, err := s.approvals.Create(ctx, approval.Ticket{
		RunID:     run.ID,
		SessionID: run.SessionID,
		Kind:      approval.KindToolCalls,
		Reasons:   append([]string(nil), decision.reasons...),
		Metadata:  metadata,
	})
	if err != nil {
		return err
	}
	return s.applyExistingReleaseGateHold(ctx, run, ticket, decision)
}

func (s *Service) applyExistingReleaseGateHold(ctx context.Context, run *agent.Run, ticket *approval.Ticket, decision *releaseExecutionGateDecision) error {
	if run == nil || ticket == nil {
		return nil
	}
	run.Status = agent.RunWaitingApproval
	run.Phase = agent.PhaseWaitingApproval
	run.ApprovalID = strings.TrimSpace(ticket.ID)
	run.PendingTools = nil
	run.Error = strings.TrimSpace(decision.summary)
	run.Governance = releaseGateGovernance(run, decision)
	if err := s.runs.Update(ctx, run); err != nil {
		return err
	}
	if err := s.publish(ctx, eventbus.NewApprovalRequestedEvent(
		run.ID,
		run.SessionID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:    ticket.ID,
			ApprovalKind:  string(ticket.Kind),
			Status:        string(ticket.Status),
			Reasons:       append([]string(nil), ticket.Reasons...),
			PolicySource:  releaseGatePolicySource,
			PolicySummary: decision.summary,
		},
		agent.BuildRunEventAttrs(run),
	)); err != nil {
		logging.DebugIfErr(err, "publish release gate approval requested failed")
	}
	if err := s.publish(ctx, eventbus.NewRunWaitingApprovalEvent(
		run.ID,
		run.SessionID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:    ticket.ID,
			ApprovalKind:  string(ticket.Kind),
			Status:        string(ticket.Status),
			Reasons:       append([]string(nil), ticket.Reasons...),
			PolicySource:  releaseGatePolicySource,
			PolicySummary: decision.summary,
		},
		agent.BuildRunEventAttrs(run),
	)); err != nil {
		logging.DebugIfErr(err, "publish release gate run waiting approval failed")
	}
	return nil
}

func releaseGateGovernance(run *agent.Run, decision *releaseExecutionGateDecision) *domaingov.Evaluation {
	evaluation := domaingov.Evaluation{
		Decision: domaingov.Decision{
			Action:       domaingov.DecisionRequireApproval,
			Reasons:      append([]string(nil), decision.reasons...),
			ReasonCodes:  releaseGateBlockerIDs(decision.report),
			PolicySource: releaseGatePolicySource,
			Summary:      decision.summary,
			PolicyLayers: []string{releaseGatePolicyLayer},
			AuditLabels:  []string{releaseGatePolicyLayer},
			ApprovalPolicy: &domaingov.ApprovalPolicy{
				DefaultScope: approval.ScopeOnce,
				MaxScope:     approval.ScopeOnce,
			},
		},
		UpdatedAt: time.Now().UTC(),
	}
	if run != nil && run.Governance != nil {
		evaluation.EffectiveConfigSnapshotID = strings.TrimSpace(run.Governance.EffectiveConfigSnapshotID)
	}
	normalized := evaluation.Normalized()
	return &normalized
}

func releaseGateApprovalMetadata(run *agent.Run, decision *releaseExecutionGateDecision) map[string]any {
	metadata := map[string]any{
		"policy_action":                 string(domaingov.DecisionRequireApproval),
		"policy_source":                 releaseGatePolicySource,
		"policy_summary":                decision.summary,
		"policy_reasons":                append([]string(nil), decision.reasons...),
		"policy_reason_codes":           releaseGateBlockerIDs(decision.report),
		"policy_layers":                 []string{releaseGatePolicyLayer},
		"policy_audit_labels":           []string{releaseGatePolicyLayer},
		"policy_approval_default_scope": string(approval.ScopeOnce),
		"policy_approval_max_scope":     string(approval.ScopeOnce),
		releaseGateMetadataKey:          true,
		releaseGateMetadataModeKey:      string(decision.mode),
		releaseGateMetadataReadyKey:     false,
		releaseGateMetadataBlockerID:    releaseGateBlockerIDs(decision.report),
	}
	if run != nil {
		if scope := publicScopeRef(run.Scope); !scope.IsZero() {
			metadata["scope"] = scope
		}
		if run.Governance != nil && strings.TrimSpace(run.Governance.EffectiveConfigSnapshotID) != "" {
			metadata["effective_config_snapshot_id"] = run.Governance.EffectiveConfigSnapshotID
		}
	}
	if decision.report != nil && strings.TrimSpace(decision.report.GeneratedAt) != "" {
		metadata[releaseGateMetadataTimeKey] = decision.report.GeneratedAt
	}
	return metadata
}

func releaseGateApprovalTicket(ticket *approval.Ticket) bool {
	if ticket == nil || ticket.Metadata == nil {
		return false
	}
	value, ok := ticket.Metadata[releaseGateMetadataKey]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	default:
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(typed)), "true")
	}
}
