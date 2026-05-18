package agent

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

func (a *AgentComponent) buildTaskContract(ctx context.Context, msg IncomingMessage, session *Session, mode ExecutionMode, preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal) *TaskContract {
	if a == nil || a.taskContract == nil {
		return buildHeuristicTaskContract(msg, session, mode, preflight, triage, signal)
	}

	base := buildTaskContractStructuredBaseWithSignal(msg, session, preflight, triage, signal)
	if base == nil {
		return nil
	}
	if preflight != nil && preflight.Blocking && signal != nil && signal.PreflightAnalyzerReady {
		return buildHeuristicTaskContract(msg, session, mode, preflight, triage, signal)
	}

	req := TaskContractAnalysisRequest{
		Model:            defaultString(msg.Model, a.config.DefaultModel),
		Message:          strings.TrimSpace(msg.Content),
		ExecutionMode:    mode,
		SuggestedDomains: append([]string(nil), base.SuggestedDomains...),
		SemanticSignal:   cloneSemanticSignal(signal),
	}
	if session != nil {
		req.SessionSummary = sessionReferenceSummary(session)
	}
	if preflight != nil {
		req.PreflightState = string(preflight.State)
	}

	if analysis, err := a.taskContract.Analyze(ctx, req); err == nil && taskContractAnalysisHasSemanticContent(analysis) {
		applyTaskContractAnalysisToSemanticSignal(signal, analysis)
		return finalizeTaskContract(mergeTaskContractAnalysis(base, msg, mode, preflight, analysis), msg.Metadata)
	}
	return buildHeuristicTaskContract(msg, session, mode, preflight, triage, signal)
}

func buildTaskContractStructuredBaseWithSignal(msg IncomingMessage, session *Session, preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal) *TaskContract {
	message := strings.TrimSpace(msg.Content)
	goal := message
	if goal == "" && session != nil {
		goal = strings.TrimSpace(session.Summary)
	}
	if goal == "" {
		return nil
	}

	domains := structuredTaskContractDomainsWithSignal(preflight, triage, signal)
	if signal != nil && signal.BrowserContextOnly {
		domains = removeSemanticDomains(domains, DomainWatch, DomainCron)
	}
	targetSummary := extractTaskContractTarget(message)

	confidence := taskContractBaseConfidence
	if len(domains) > 0 {
		confidence += taskContractDomainConfidenceBonus
	}
	if targetSummary != "" {
		confidence += taskContractTargetConfidenceBonus
	}
	if triage != nil && triage.Confidence > confidence {
		confidence = triage.Confidence
	}
	if confidence > taskContractMaxConfidence {
		confidence = taskContractMaxConfidence
	}

	return &TaskContract{
		Goal:             goal,
		JobType:          taskContractJobGeneral,
		TargetSummary:    targetSummary,
		SuggestedDomains: append([]string(nil), domains...),
		Confidence:       confidence,
		Source:           taskContractSourceHeuristic,
		GeneratedAt:      time.Now().UTC(),
	}
}

func structuredTaskContractDomainsWithSignal(preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal) []string {
	switch {
	case signal != nil && len(signal.SuggestedDomains) > 0:
		return normalizeSemanticDomains(signal.SuggestedDomains)
	case preflight != nil && len(preflight.SuggestedDomains) > 0:
		return normalizeSemanticDomains(preflight.SuggestedDomains)
	case triage != nil && len(triage.SuggestedDomains) > 0:
		return normalizeSemanticDomains(triage.SuggestedDomains)
	default:
		return nil
	}
}

func taskContractAnalysisHasSemanticContent(analysis TaskContractAnalysis) bool {
	return analysis.JobType != "" ||
		analysis.TargetSummary != "" ||
		len(analysis.SuggestedDomains) > 0 ||
		len(analysis.CapabilityHints) > 0 ||
		analysis.DeliverableKinds != nil ||
		analysis.MissingInfoIDs != nil ||
		analysis.BrowserContextOnly ||
		analysis.RequiresExternalEffect != nil ||
		analysis.RequiresApproval != nil
}

func mergeTaskContractAnalysis(base *TaskContract, msg IncomingMessage, mode ExecutionMode, preflight *RunPreflightReport, analysis TaskContractAnalysis) *TaskContract {
	if base == nil {
		return nil
	}
	message := strings.TrimSpace(msg.Content)
	domains := append([]string(nil), base.SuggestedDomains...)
	if len(analysis.SuggestedDomains) > 0 {
		domains = append([]string(nil), analysis.SuggestedDomains...)
	}
	domains = normalizeSemanticDomains(domains)
	analysis = sanitizeTaskContractAnalysisForMessage(message, domains, analysis)
	if len(analysis.SuggestedDomains) > 0 {
		domains = append([]string(nil), analysis.SuggestedDomains...)
	}
	domains = sanitizeSuggestedDomainsForMessage(message, domains)
	if analysis.BrowserContextOnly {
		domains = removeSemanticDomains(domains, DomainWatch, DomainCron)
	}

	jobType := normalize.FirstNonEmpty(normalizeTaskContractJobType(analysis.JobType), base.JobType)
	if jobType == "" {
		jobType = taskContractJobGeneral
	}
	targetSummary := normalize.FirstNonEmpty(strings.TrimSpace(analysis.TargetSummary), base.TargetSummary)

	deliverables := inferTaskContractDeliverables(message, mode, domains, jobType)
	if analysis.DeliverableKinds != nil {
		deliverables = buildTaskContractDeliverablesFromKinds(analysis.DeliverableKinds)
	}
	capabilityHints := normalizeCapabilityHints(analysis.CapabilityHints)
	if len(capabilityHints) == 0 {
		capabilityHints = normalizeCapabilityHints(base.CapabilityHints)
	}
	if len(capabilityHints) == 0 {
		capabilityHints = inferTaskContractCapabilityHints(jobType, domains, deliverables)
	}

	missing := inferTaskContractMissingInfo(message, mode, domains, preflight, jobType, deliverables)
	if analysis.MissingInfoSpecified {
		missingIDs := normalizeTaskContractMissingInfoIDs(analysis.MissingInfoIDs)
		if preflightHasCheck(preflight, "reference_gap") && !containsTaskContractString(missingIDs, taskMissingInfoSourceTarget) {
			missingIDs = append(missingIDs, taskMissingInfoSourceTarget)
		}
		missing = buildTaskContractMissingInfoFromIDs(missingIDs, domains, preflight)
	}

	requiresExternalEffect := taskContractRequiresExternalEffect(message, jobType, domains, deliverables)
	if analysis.RequiresExternalEffect != nil {
		requiresExternalEffect = *analysis.RequiresExternalEffect
	}
	if jobType == taskContractJobDelivery || jobType == taskContractJobDeployment || jobType == taskContractJobMonitor ||
		taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery, taskDeliverableWatchAlert, taskDeliverableDeployment) {
		requiresExternalEffect = true
	}

	requiresApproval := taskContractRequiresApproval(message, jobType, domains, deliverables)
	if analysis.RequiresApproval != nil {
		requiresApproval = *analysis.RequiresApproval
	}
	if preflightHasCheck(preflight, "expected_confirmation") ||
		jobType == taskContractJobDelivery ||
		jobType == taskContractJobDeployment ||
		taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery, taskDeliverableDeployment) {
		requiresApproval = true
	}

	confidence := base.Confidence
	if analysis.Confidence > confidence {
		confidence = analysis.Confidence
	}

	return &TaskContract{
		Goal:                   base.Goal,
		JobType:                jobType,
		TargetSummary:          targetSummary,
		SuggestedDomains:       domains,
		CapabilityHints:        capabilityHints,
		ExpectedDeliverables:   deliverables,
		AcceptanceCriteria:     inferTaskContractAcceptance(deliverables, missing, requiresExternalEffect, requiresApproval),
		MissingInfo:            missing,
		RequiresExternalEffect: requiresExternalEffect,
		RequiresApproval:       requiresApproval,
		Confidence:             confidence,
		Source:                 taskContractSourceModel,
		GeneratedAt:            base.GeneratedAt,
	}
}

func containsTaskContractString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func sanitizeTaskContractAnalysisForMessage(message string, domains []string, analysis TaskContractAnalysis) TaskContractAnalysis {
	if analysis.BrowserContextOnly {
		analysis.SuggestedDomains = removeSemanticDomains(analysis.SuggestedDomains, DomainWatch, DomainCron)
		switch normalizeTaskContractJobType(analysis.JobType) {
		case taskContractJobMonitor, taskContractJobAutomation:
			analysis.JobType = ""
		}
		if len(analysis.DeliverableKinds) > 0 {
			filtered := make([]string, 0, len(analysis.DeliverableKinds))
			for _, kind := range normalizeTaskContractDeliverableKinds(analysis.DeliverableKinds) {
				if strings.TrimSpace(kind) == taskDeliverableWatchAlert {
					continue
				}
				filtered = append(filtered, kind)
			}
			analysis.DeliverableKinds = filtered
		}
		if analysis.MissingInfoSpecified {
			filtered := make([]string, 0, len(analysis.MissingInfoIDs))
			for _, id := range normalizeTaskContractMissingInfoIDs(analysis.MissingInfoIDs) {
				if strings.TrimSpace(id) == taskMissingInfoSchedule {
					continue
				}
				filtered = append(filtered, id)
			}
			analysis.MissingInfoIDs = filtered
		}
		analysis.RequiresExternalEffect = nil
		analysis.RequiresApproval = nil
		return analysis
	}
	if !shouldSuppressWatchDomainsForBrowserContext(message, domains) {
		return analysis
	}

	if len(analysis.SuggestedDomains) > 0 {
		filtered := make([]string, 0, len(analysis.SuggestedDomains))
		for _, domain := range normalizeSemanticDomains(analysis.SuggestedDomains) {
			switch ToolDomain(domain) {
			case DomainWatch, DomainCron:
				continue
			default:
				filtered = append(filtered, domain)
			}
		}
		analysis.SuggestedDomains = filtered
	}

	switch normalizeTaskContractJobType(analysis.JobType) {
	case taskContractJobMonitor, taskContractJobAutomation:
		analysis.JobType = ""
	}

	if len(analysis.DeliverableKinds) > 0 {
		filtered := make([]string, 0, len(analysis.DeliverableKinds))
		for _, kind := range normalizeTaskContractDeliverableKinds(analysis.DeliverableKinds) {
			if strings.TrimSpace(kind) == taskDeliverableWatchAlert {
				continue
			}
			filtered = append(filtered, kind)
		}
		analysis.DeliverableKinds = filtered
	}

	if analysis.MissingInfoSpecified {
		filtered := make([]string, 0, len(analysis.MissingInfoIDs))
		for _, id := range normalizeTaskContractMissingInfoIDs(analysis.MissingInfoIDs) {
			if strings.TrimSpace(id) == taskMissingInfoSchedule {
				continue
			}
			filtered = append(filtered, id)
		}
		analysis.MissingInfoIDs = filtered
	}

	analysis.RequiresExternalEffect = nil
	analysis.RequiresApproval = nil
	return analysis
}
