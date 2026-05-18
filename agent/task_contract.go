package agent

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/semanticschema"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	taskContractSourceHeuristic = "heuristic"
	taskContractSourceModel     = "model_semantic"

	taskContractJobGeneral     = "general"
	taskContractJobReport      = "report"
	taskContractJobResearch    = "research"
	taskContractJobMonitor     = "monitor"
	taskContractJobDelivery    = "delivery"
	taskContractJobDeployment  = "deployment"
	taskContractJobDevelopment = "development"
	taskContractJobAutomation  = "automation"

	taskDeliverableSummary         = "summary"
	taskDeliverableDocument        = "document"
	taskDeliverableSpreadsheet     = "spreadsheet"
	taskDeliverablePresentation    = "presentation"
	taskDeliverableBrowserEvidence = "browser_evidence"
	taskDeliverableDesktopEvidence = "desktop_evidence"
	taskDeliverableMessageDelivery = "message_delivery"
	taskDeliverableWatchAlert      = "watch_alert"
	taskDeliverableDeployment      = "deployment"

	taskAcceptanceVisibleResult  = "visible_result"
	taskAcceptanceDeliverables   = "deliverables_ready"
	taskAcceptanceExternalEffect = "external_effect_verified"
	taskAcceptanceApproval       = "approval_before_side_effect"

	taskMissingInfoSourceTarget    = "source_target"
	taskMissingInfoDeliveryTarget  = "delivery_target"
	taskMissingInfoSchedule        = "schedule"
	taskMissingInfoDeploymentScope = "deployment_target"
)

const (
	taskContractBaseConfidence             = 0.45
	taskContractDomainConfidenceBonus      = 0.10
	taskContractTargetConfidenceBonus      = 0.10
	taskContractDeliverableConfidenceBonus = 0.05
	taskContractMaxConfidence              = 0.95
)

var (
	taskContractURLPattern                = regexp.MustCompile(`https?://[^\s]+`)
	taskContractArtifactPattern           = regexp.MustCompile(`artifact://[^\s]+`)
	taskContractEmailPattern              = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	taskContractPathPattern               = regexp.MustCompile(`(?:[A-Za-z]:\\[\w.\-\\]+|(?:\.\.?/|/|[A-Za-z0-9._-]+/)[\w.\-~/]+)`)
	taskContractChannelPattern            = regexp.MustCompile(`(?:^|[\s(])#[^\s#]{2,}`)
	taskContractMentionPattern            = regexp.MustCompile(`(?:^|[\s(])@[^\s@]{2,}`)
	taskContractTimePattern               = regexp.MustCompile(`\b(?:[01]?\d|2[0-3]):[0-5]\d\b`)
	taskContractISODatePattern            = regexp.MustCompile(`\b\d{4}-\d{1,2}-\d{1,2}(?:[ t](?:[01]?\d|2[0-3]):[0-5]\d)?\b`)
	taskContractSlashDatePattern          = regexp.MustCompile(`\b\d{1,2}/\d{1,2}(?:/\d{2,4})?(?:\s+(?:[01]?\d|2[0-3]):[0-5]\d)?\b`)
	taskContractCronPattern               = regexp.MustCompile(`(?m)(?:^|\s)(?:\*|[0-5]?\d)(?:/\d+)?\s+(?:\*|[01]?\d|2[0-3])(?:/\d+)?\s+(?:\*|[1-9]|[12]\d|3[01])(?:/\d+)?\s+(?:\*|[1-9]|1[0-2])(?:/\d+)?\s+(?:\*|[0-7])(?:/\d+)?(?:\s|$)`)
	taskContractStructuredSchedulePattern = regexp.MustCompile(`(?i)(?:--every\s+\S+|--at\s+\S+|--expression\s+\S+|(?:every|interval|at|expression)\s*=\s*\S+|"(?:every|interval|at|expression)"\s*:\s*"[^"]+")`)
	taskContractHostPattern               = regexp.MustCompile(`\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}\b`)
	taskContractIPPattern                 = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	taskContractCountedOutputPattern      = regexp.MustCompile(`\b\d+\s*(?:line|lines|sentence|sentences|bullet|bullets|step|steps|item|items|point|points|paragraph|paragraphs)\b|\d+\s*(?:条|点|句|段|页|步)`)
)

func buildTaskContract(msg IncomingMessage, session *Session, mode ExecutionMode, preflight *RunPreflightReport, triage *RunTriageTrace) *TaskContract {
	return buildHeuristicTaskContract(msg, session, mode, preflight, triage, nil)
}

func buildHeuristicTaskContract(msg IncomingMessage, session *Session, mode ExecutionMode, preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal) *TaskContract {
	return finalizeTaskContract(buildTaskContractBaseWithSignal(msg, session, mode, preflight, triage, signal), msg.Metadata)
}

func finalizeTaskContract(contract *TaskContract, metadata map[string]any) *TaskContract {
	if contract == nil {
		return nil
	}
	applyTaskContractClarifications(contract, metadata)
	return contract
}

func buildTaskContractBaseWithSignal(msg IncomingMessage, session *Session, mode ExecutionMode, preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal) *TaskContract {
	message := strings.TrimSpace(msg.Content)
	goal := message
	if goal == "" && session != nil {
		goal = strings.TrimSpace(session.Summary)
	}
	if goal == "" {
		return nil
	}

	domains := taskContractDomainsWithSignal(preflight, triage, signal, message)
	domains = sanitizeSuggestedDomainsForMessage(message, domains)
	if signal != nil && signal.BrowserContextOnly {
		domains = removeSemanticDomains(domains, DomainWatch, DomainCron)
	}
	jobType := inferTaskContractJobType(message, mode, domains)
	targetSummary := extractTaskContractTarget(message)
	deliverables := inferTaskContractDeliverables(message, mode, domains, jobType)
	missing := inferTaskContractMissingInfo(message, mode, domains, preflight, jobType, deliverables)
	requiresExternalEffect := taskContractRequiresExternalEffect(message, jobType, domains, deliverables)
	requiresApproval := taskContractRequiresApproval(message, jobType, domains, deliverables) || preflightHasCheck(preflight, "expected_confirmation")
	acceptance := inferTaskContractAcceptance(deliverables, missing, requiresExternalEffect, requiresApproval)

	confidence := taskContractBaseConfidence
	if len(domains) > 0 {
		confidence += taskContractDomainConfidenceBonus
	}
	if targetSummary != "" {
		confidence += taskContractTargetConfidenceBonus
	}
	if len(deliverables) > 0 {
		confidence += taskContractDeliverableConfidenceBonus
	}
	if triage != nil && triage.Confidence > confidence {
		confidence = triage.Confidence
	}
	if confidence > taskContractMaxConfidence {
		confidence = taskContractMaxConfidence
	}

	contract := &TaskContract{
		Goal:                   goal,
		JobType:                jobType,
		TargetSummary:          targetSummary,
		SuggestedDomains:       append([]string(nil), domains...),
		CapabilityHints:        inferTaskContractCapabilityHints(jobType, domains, deliverables),
		ExpectedDeliverables:   deliverables,
		AcceptanceCriteria:     acceptance,
		MissingInfo:            missing,
		RequiresExternalEffect: requiresExternalEffect,
		RequiresApproval:       requiresApproval,
		Confidence:             confidence,
		Source:                 taskContractSourceHeuristic,
		GeneratedAt:            time.Now().UTC(),
	}
	return contract
}

func taskContractDomainsWithSignal(preflight *RunPreflightReport, triage *RunTriageTrace, signal *SemanticSignal, message string) []string {
	switch {
	case signal != nil && len(signal.SuggestedDomains) > 0:
		return normalizeSemanticDomains(signal.SuggestedDomains)
	case preflight != nil && len(preflight.SuggestedDomains) > 0:
		return normalizeSemanticDomains(preflight.SuggestedDomains)
	case triage != nil && len(triage.SuggestedDomains) > 0:
		return normalizeSemanticDomains(triage.SuggestedDomains)
	default:
		return domainsToStrings(detectStructuredEvidence(message))
	}
}

func inferTaskContractJobType(message string, mode ExecutionMode, domains []string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case taskContractLooksLikeMonitorJob(message, lower, mode, domains):
		return taskContractJobMonitor
	case taskContractLooksLikeDeploymentJob(message, lower, mode):
		return taskContractJobDeployment
	case taskContractLooksLikeAutomationJob(message, lower, mode, domains):
		return taskContractJobAutomation
	case taskContractLooksLikeDeliveryJob(message, mode, domains):
		return taskContractJobDelivery
	case taskContractLooksLikeReportJob(message, lower, domains):
		return taskContractJobReport
	case taskContractLooksLikeResearchJob(message, lower, mode, domains):
		return taskContractJobResearch
	case taskContractLooksLikeDevelopmentJob(lower, domains):
		return taskContractJobDevelopment
	}
	return taskContractJobGeneral
}

func taskContractLooksLikeMonitorJob(message, lower string, mode ExecutionMode, domains []string) bool {
	if shouldSuppressWatchDomainsForBrowserContext(message, domains) {
		return false
	}
	if mode == ExecutionModeWatch || hasSemanticDomain(domains, DomainWatch) {
		return true
	}
	return fallbackTaskContractMentionsMonitorIntent(lower)
}

func shouldSuppressWatchDomainsForBrowserContext(message string, domains []string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if !hasSemanticDomain(domains, DomainBrowser) &&
		!messageHasBrowserReference(message) &&
		!fallbackTaskContractMentionsBrowserReference(lower) {
		return false
	}
	if messageRequestsScheduledExecution(message) || messageHasSchedule(message) {
		return false
	}
	if fallbackTaskContractMentionsBrowserWatchIntent(lower) {
		return false
	}
	return fallbackTaskContractMentionsStayOnPageIntent(lower)
}

func taskContractLooksLikeDeploymentJob(message, lower string, mode ExecutionMode) bool {
	if taskExplicitlyRequestsDeployment(message) {
		return true
	}
	if mode != ExecutionModeWorkflow || !messageHasDeploymentTarget(message) {
		return false
	}
	return messageMentionsDeployableArtifact(lower) &&
		(fallbackTaskContractMentionsExplicitDeployment(lower) ||
			fallbackTaskContractMentionsPublishIntent(lower) ||
			fallbackTaskContractMentionsDeploymentWorkflow(lower))
}

func taskContractLooksLikeAutomationJob(message, lower string, mode ExecutionMode, domains []string) bool {
	if taskContractLooksLikeMonitorJob(message, lower, mode, domains) {
		return false
	}
	if mode == ExecutionModeWorkflow && hasSemanticDomain(domains, DomainCron) {
		return true
	}
	if hasSemanticDomain(domains, DomainCron, DomainCalendar) && messageRequestsScheduledExecution(message) {
		return true
	}
	if mode == ExecutionModeWorkflow && messageRequestsScheduledExecution(message) {
		return true
	}
	return fallbackTaskContractMentionsAutomationIntent(lower)
}

func taskContractLooksLikeDeliveryJob(message string, mode ExecutionMode, domains []string) bool {
	if taskExplicitlyRequestsMessageDelivery(message) {
		return true
	}
	if mode != ExecutionModeWorkflow {
		return false
	}
	if !messageHasExternalDeliveryTarget(message) {
		return false
	}
	if hasSemanticDomain(domains, DomainBrowser) && taskExplicitlyRequestsExternalSubmission(message, domains) {
		return false
	}
	return hasSemanticDomain(domains, DomainEmail, DomainChannel, DomainCalendar)
}

func taskContractLooksLikeReportJob(message, lower string, domains []string) bool {
	if !taskContractRequestsOfficeOutput(message, lower) {
		return false
	}
	if hasSemanticDomain(domains, DomainDocument, DomainSheet, DomainPresentation) {
		return true
	}
	return fallbackTaskContractMentionsStructuredWriteup(lower) ||
		fallbackTaskContractMentionsSpreadsheetDeliverable(lower) ||
		fallbackTaskContractMentionsDocumentDeliverable(lower) ||
		fallbackTaskContractMentionsPresentationDeliverable(lower)
}

func taskContractLooksLikeResearchJob(message, lower string, mode ExecutionMode, domains []string) bool {
	if taskExplicitlyRequestsDeployment(message) ||
		taskExplicitlyRequestsMessageDelivery(message) ||
		taskExplicitlyRequestsExternalSubmission(message, domains) ||
		taskContractLooksLikeDevelopmentJob(lower, domains) ||
		messageRequestsStructuredWriteup(message, lower) {
		return false
	}
	if messageRequestsInvestigation(lower) {
		return true
	}
	return mode == ExecutionModePlanned &&
		!hasSemanticDomain(domains, DomainDocument, DomainSheet, DomainPresentation, DomainEmail, DomainChannel, DomainCalendar, DomainWatch, DomainCron) &&
		hasSemanticDomain(domains, DomainBrowser, DomainFS, DomainNet, DomainSearch, DomainGit)
}

func taskContractLooksLikeDevelopmentJob(lower string, domains []string) bool {
	if fallbackTaskContractMentionsDevelopmentIntent(lower) {
		return true
	}
	return hasSemanticDomain(domains, DomainGit) &&
		hasSemanticDomain(domains, DomainFS) &&
		hasSemanticDomain(domains, DomainExec)
}

func taskContractRequestsOfficeOutput(message, lower string) bool {
	if messageRequestsStructuredWriteup(message, lower) {
		return true
	}
	if !messageRequestsArtifactOutput(lower) {
		return false
	}
	if target := extractTaskContractTarget(message); messageTargetLooksLikeOfficeArtifact(target) {
		return true
	}
	return fallbackTaskContractMentionsSpreadsheetDeliverable(lower) ||
		fallbackTaskContractMentionsDocumentDeliverable(lower) ||
		fallbackTaskContractMentionsPresentationDeliverable(lower)
}

func messageTargetLooksLikeOfficeArtifact(target string) bool {
	switch strings.TrimPrefix(pathExt(strings.ToLower(strings.TrimSpace(target))), ".") {
	case "md", "doc", "docx", "odt", "rtf", "csv", "tsv", "xlsx", "xls", "ods", "ppt", "pptx", "key":
		return true
	default:
		return false
	}
}

func inferTaskContractDeliverables(message string, mode ExecutionMode, domains []string, jobType string) []TaskContractDeliverable {
	out := make([]TaskContractDeliverable, 0, 4)
	deliveryIntent := taskExplicitlyRequestsMessageDelivery(message)
	lower := strings.ToLower(strings.TrimSpace(message))
	officeOutputRequested := taskContractRequestsOfficeOutput(message, lower)
	appendDeliverable := func(kind, summary string, required bool) {
		for _, existing := range out {
			if existing.Kind == kind {
				return
			}
		}
		out = append(out, TaskContractDeliverable{
			Kind:     kind,
			Summary:  strings.TrimSpace(summary),
			Required: required,
		})
	}

	appendDeliverable(taskDeliverableSummary, "Provide a concise final result that the user can review quickly.", true)

	for _, domain := range domains {
		switch ToolDomain(domain) {
		case DomainSheet:
			if officeOutputRequested || strings.TrimSpace(jobType) == taskContractJobReport {
				appendDeliverable(taskDeliverableSpreadsheet, "Produce a spreadsheet update, export, or table-like deliverable.", true)
			}
		case DomainDocument:
			if officeOutputRequested || strings.TrimSpace(jobType) == taskContractJobReport {
				appendDeliverable(taskDeliverableDocument, "Produce a document, notes file, or document-style output.", true)
			}
		case DomainPresentation:
			if officeOutputRequested || strings.TrimSpace(jobType) == taskContractJobReport {
				appendDeliverable(taskDeliverablePresentation, "Produce slide output or presentation evidence.", true)
			}
		case DomainBrowser:
			appendDeliverable(taskDeliverableBrowserEvidence, "Leave browser evidence such as a page snapshot, screenshot, or extracted data.", false)
		case DomainDesktop:
			appendDeliverable(taskDeliverableDesktopEvidence, "Leave desktop evidence such as a UI tree snapshot or screenshot.", false)
		case DomainEmail, DomainChannel:
			if deliveryIntent {
				appendDeliverable(taskDeliverableMessageDelivery, "Leave delivery evidence for the sent message, reply, or notification.", true)
			}
		case DomainWatch:
			appendDeliverable(taskDeliverableWatchAlert, "Produce an alert, notification payload, or changed-state summary.", true)
		}
	}

	switch jobType {
	case taskContractJobDelivery:
		appendDeliverable(taskDeliverableMessageDelivery, "Leave delivery evidence for the external action.", true)
	case taskContractJobMonitor, taskContractJobAutomation:
		appendDeliverable(taskDeliverableWatchAlert, "Produce a reusable alert or automation outcome.", true)
	case taskContractJobDeployment:
		appendDeliverable(taskDeliverableDeployment, "Produce deployment evidence such as a target environment, endpoint, or release artifact.", true)
	}

	if officeOutputRequested {
		switch {
		case fallbackTaskContractMentionsSpreadsheetDeliverable(lower):
			appendDeliverable(taskDeliverableSpreadsheet, "Produce a spreadsheet file, export, or sheet update.", true)
		case fallbackTaskContractMentionsDocumentDeliverable(lower):
			appendDeliverable(taskDeliverableDocument, "Produce a document or report artifact.", true)
		case fallbackTaskContractMentionsPresentationDeliverable(lower):
			appendDeliverable(taskDeliverablePresentation, "Produce slide output or presentation evidence.", true)
		}
	}

	return out
}

func inferTaskContractMissingInfo(message string, mode ExecutionMode, domains []string, preflight *RunPreflightReport, jobType string, deliverables []TaskContractDeliverable) []TaskContractMissingInfo {
	out := make([]TaskContractMissingInfo, 0, 4)
	appendMissing := func(item TaskContractMissingInfo) {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			return
		}
		for _, existing := range out {
			if existing.ID == item.ID {
				return
			}
		}
		out = append(out, item)
	}

	if preflightHasCheck(preflight, "reference_gap") || taskContractNeedsWorkspaceReference(message, domains) {
		for _, item := range buildTaskContractMissingInfoFromIDs([]string{taskMissingInfoSourceTarget}, domains, preflight) {
			appendMissing(item)
		}
	}

	if taskNeedsDeliveryTarget(message, jobType, domains, deliverables) && !messageHasDeliveryTarget(message) {
		appendMissing(TaskContractMissingInfo{
			ID:          taskMissingInfoDeliveryTarget,
			Label:       taskMissingInfoLabel(taskMissingInfoDeliveryTarget),
			Summary:     "The task needs an explicit recipient, channel, webhook, or delivery destination.",
			Question:    "Where should I send or post the result?",
			InputMode:   taskMissingInfoInputMode(taskMissingInfoDeliveryTarget),
			Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeliveryTarget),
			Required:    true,
			Hints: []string{
				"发给 ceo@example.com",
				"发送到 Slack #ops",
				"回到当前会话即可",
			},
		})
	}

	if taskNeedsSchedule(message, mode, jobType, domains, deliverables) && !messageHasSchedule(message) {
		appendMissing(TaskContractMissingInfo{
			ID:          taskMissingInfoSchedule,
			Label:       taskMissingInfoLabel(taskMissingInfoSchedule),
			Summary:     "The task needs a concrete start time or repeat cadence.",
			Question:    "When should this run, and how often should it repeat?",
			InputMode:   taskMissingInfoInputMode(taskMissingInfoSchedule),
			Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSchedule),
			Required:    true,
			Hints: []string{
				"每天上午 9 点",
				"每周一 08:30",
				"从下周开始，每小时一次",
			},
		})
	}

	if jobType == taskContractJobDeployment && !messageHasDeploymentTarget(message) {
		appendMissing(TaskContractMissingInfo{
			ID:          taskMissingInfoDeploymentScope,
			Label:       taskMissingInfoLabel(taskMissingInfoDeploymentScope),
			Summary:     "The deployment request needs an environment, service, URL, or server target.",
			Question:    "Which environment, service, URL, or server should I deploy to?",
			InputMode:   taskMissingInfoInputMode(taskMissingInfoDeploymentScope),
			Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeploymentScope),
			Required:    true,
			Hints: []string{
				"部署到 staging",
				"发布到 https://app.example.com",
				"推到生产集群 web-api",
			},
		})
	}

	return out
}

func taskContractNeedsWorkspaceReference(message string, domains []string) bool {
	if messageNeedsWorkspaceReference(message, domains) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if containsConcreteReference(message) || messageHasLocalPathReference(message) {
		return false
	}
	if messageLooksLikeWorkspaceExploratoryRead(lower) {
		return false
	}
	return fallbackTaskContractMentionsAmbiguousWorkspaceTarget(lower) &&
		fallbackTaskContractMentionsWorkspaceChange(lower)
}

func normalizeTaskContractJobType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !semanticschema.IsTaskContractJobType(value) {
		return ""
	}
	return value
}

func normalizeTaskContractDeliverableKinds(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if !semanticschema.IsTaskContractDeliverableKind(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeTaskContractMissingInfoIDs(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if !semanticschema.IsTaskContractMissingInfoID(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func taskContractDeliverableTemplate(kind string) (TaskContractDeliverable, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case taskDeliverableSummary:
		return TaskContractDeliverable{
			Kind:     taskDeliverableSummary,
			Summary:  "Provide a concise final result that the user can review quickly.",
			Required: true,
		}, true
	case taskDeliverableDocument:
		return TaskContractDeliverable{
			Kind:     taskDeliverableDocument,
			Summary:  "Produce a document, notes file, or document-style output.",
			Required: true,
		}, true
	case taskDeliverableSpreadsheet:
		return TaskContractDeliverable{
			Kind:     taskDeliverableSpreadsheet,
			Summary:  "Produce a spreadsheet update, export, or table-like deliverable.",
			Required: true,
		}, true
	case taskDeliverablePresentation:
		return TaskContractDeliverable{
			Kind:     taskDeliverablePresentation,
			Summary:  "Produce slide output or presentation evidence.",
			Required: true,
		}, true
	case taskDeliverableBrowserEvidence:
		return TaskContractDeliverable{
			Kind:     taskDeliverableBrowserEvidence,
			Summary:  "Leave browser evidence such as a page snapshot, screenshot, or extracted data.",
			Required: false,
		}, true
	case taskDeliverableDesktopEvidence:
		return TaskContractDeliverable{
			Kind:     taskDeliverableDesktopEvidence,
			Summary:  "Leave desktop evidence such as a UI tree snapshot or screenshot.",
			Required: false,
		}, true
	case taskDeliverableMessageDelivery:
		return TaskContractDeliverable{
			Kind:     taskDeliverableMessageDelivery,
			Summary:  "Leave delivery evidence for the sent message, reply, or notification.",
			Required: true,
		}, true
	case taskDeliverableWatchAlert:
		return TaskContractDeliverable{
			Kind:     taskDeliverableWatchAlert,
			Summary:  "Produce an alert, notification payload, or changed-state summary.",
			Required: true,
		}, true
	case taskDeliverableDeployment:
		return TaskContractDeliverable{
			Kind:     taskDeliverableDeployment,
			Summary:  "Produce deployment evidence such as a target environment, endpoint, or release artifact.",
			Required: true,
		}, true
	default:
		return TaskContractDeliverable{}, false
	}
}

func mergeTaskContractDeliverableKinds(base []TaskContractDeliverable, kinds []string) []TaskContractDeliverable {
	if kinds == nil {
		return base
	}
	out := cloneTaskContractDeliverables(base)
	seen := make(map[string]struct{}, len(out))
	for _, item := range out {
		seen[item.Kind] = struct{}{}
	}
	for _, kind := range normalizeTaskContractDeliverableKinds(kinds) {
		if _, ok := seen[kind]; ok {
			continue
		}
		item, ok := taskContractDeliverableTemplate(kind)
		if !ok {
			continue
		}
		out = append(out, item)
		seen[kind] = struct{}{}
	}
	return out
}

func buildTaskContractDeliverablesFromKinds(kinds []string) []TaskContractDeliverable {
	if kinds == nil {
		return nil
	}
	return mergeTaskContractDeliverableKinds(nil, kinds)
}

func normalizeCapabilityHints(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := normalizeCapabilityHint(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeCapabilityHint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "browser", "desktop", "fs", "exec", "net", "search", "web", "text", "git":
		return ""
	case "email", "email.search", "email.read", "email.list", "email.download_attachment":
		return "email.search"
	case "email.send":
		return "email.send"
	case "rss", "news", "rss.fetch", "news.fetch", "search.news":
		return "search.news"
	case "news.digest":
		return "news.digest"
	case "weather", "weather.fetch", "search.web":
		return "search.web"
	case "translate", "translation", "translate.run":
		return "translate.run"
	case "calculator", "math", "calculator.eval":
		return "calculator.eval"
	default:
		if strings.Contains(value, ".") {
			return value
		}
		return ""
	}
}

func inferTaskContractCapabilityHints(jobType string, domains []string, deliverables []TaskContractDeliverable) []string {
	hints := make([]string, 0, 2)
	if hasSemanticDomain(domains, DomainNews) {
		if strings.TrimSpace(jobType) == taskContractJobReport ||
			taskContractDeliverablesContain(deliverables, taskDeliverableSpreadsheet, taskDeliverableDocument, taskDeliverablePresentation) {
			hints = append(hints, "news.digest")
		} else {
			hints = append(hints, "search.news")
		}
	}
	if hasSemanticDomain(domains, DomainEmail) {
		if strings.TrimSpace(jobType) == taskContractJobDelivery || taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery) {
			hints = append(hints, "email.send")
		} else {
			hints = append(hints, "email.search")
		}
	}
	return normalizeCapabilityHints(hints)
}

func buildTaskContractMissingInfoFromIDs(ids []string, domains []string, preflight *RunPreflightReport) []TaskContractMissingInfo {
	if ids == nil {
		return nil
	}
	normalized := normalizeTaskContractMissingInfoIDs(ids)
	out := make([]TaskContractMissingInfo, 0, len(normalized))
	for _, id := range normalized {
		switch id {
		case taskMissingInfoSourceTarget:
			out = append(out, TaskContractMissingInfo{
				ID:          taskMissingInfoSourceTarget,
				Label:       taskMissingInfoLabel(taskMissingInfoSourceTarget),
				Summary:     "The task needs a concrete file, URL, screenshot, repository, or source target.",
				Question:    preflightReferenceQuestion(domains),
				InputMode:   taskMissingInfoInputMode(taskMissingInfoSourceTarget),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSourceTarget),
				Required:    true,
				Hints:       preflightReferenceHints(domains),
			})
		case taskMissingInfoDeliveryTarget:
			out = append(out, TaskContractMissingInfo{
				ID:          taskMissingInfoDeliveryTarget,
				Label:       taskMissingInfoLabel(taskMissingInfoDeliveryTarget),
				Summary:     "The task needs an explicit recipient, channel, webhook, or delivery destination.",
				Question:    "Where should I send or post the result?",
				InputMode:   taskMissingInfoInputMode(taskMissingInfoDeliveryTarget),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeliveryTarget),
				Required:    true,
				Hints: []string{
					"发给 ceo@example.com",
					"发送到 Slack #ops",
					"回到当前会话即可",
				},
			})
		case taskMissingInfoSchedule:
			out = append(out, TaskContractMissingInfo{
				ID:          taskMissingInfoSchedule,
				Label:       taskMissingInfoLabel(taskMissingInfoSchedule),
				Summary:     "The task needs a concrete start time or repeat cadence.",
				Question:    "When should this run, and how often should it repeat?",
				InputMode:   taskMissingInfoInputMode(taskMissingInfoSchedule),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSchedule),
				Required:    true,
				Hints: []string{
					"每天上午 9 点",
					"每周一 08:30",
					"从下周开始，每小时一次",
				},
			})
		case taskMissingInfoDeploymentScope:
			out = append(out, TaskContractMissingInfo{
				ID:          taskMissingInfoDeploymentScope,
				Label:       taskMissingInfoLabel(taskMissingInfoDeploymentScope),
				Summary:     "The deployment request needs an environment, service, URL, or server target.",
				Question:    "Which environment, service, URL, or server should I deploy to?",
				InputMode:   taskMissingInfoInputMode(taskMissingInfoDeploymentScope),
				Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeploymentScope),
				Required:    true,
				Hints: []string{
					"部署到 staging",
					"发布到 https://app.example.com",
					"推到生产集群 web-api",
				},
			})
		}
	}
	if len(out) == 0 && preflightHasCheck(preflight, "reference_gap") {
		return buildTaskContractMissingInfoFromIDs([]string{taskMissingInfoSourceTarget}, domains, nil)
	}
	return out
}

func taskContractDeliverablesContain(items []TaskContractDeliverable, kinds ...string) bool {
	for _, item := range items {
		for _, kind := range kinds {
			if item.Kind == strings.TrimSpace(kind) {
				return true
			}
		}
	}
	return false
}

func taskExplicitlyRequestsDeployment(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if taskIntentCancelsOrStops(lower) {
		return false
	}
	if fallbackTaskContractMentionsExplicitDeployment(lower) {
		return true
	}
	if !messageHasDeploymentTarget(message) || !messageMentionsDeployableArtifact(lower) {
		return false
	}
	return fallbackTaskContractMentionsPublishIntent(lower) ||
		fallbackTaskContractMentionsDeploymentWorkflow(lower)
}

func messageMentionsDeployableArtifact(lower string) bool {
	return fallbackTaskContractMentionsDeployableArtifact(lower)
}

func messageRequestsStructuredWriteup(message, lower string) bool {
	if taskContractCountedOutputPattern.MatchString(lower) &&
		fallbackTaskContractMentionsCountedWriteup(lower) {
		return true
	}
	if fallbackTaskContractMentionsTransformWriteup(lower) &&
		(fallbackTaskContractMentionsStructuredWriteup(lower) ||
			fallbackTaskContractMentionsSpreadsheetDeliverable(lower) ||
			fallbackTaskContractMentionsDocumentDeliverable(lower) ||
			fallbackTaskContractMentionsPresentationDeliverable(lower) ||
			messageRequestsArtifactOutput(lower) ||
			messageTargetLooksLikeOfficeArtifact(extractTaskContractTarget(message))) {
		return true
	}
	target := extractTaskContractTarget(message)
	if target == "" || !messageRequestsArtifactOutput(lower) {
		return false
	}
	switch strings.TrimPrefix(pathExt(strings.ToLower(target)), ".") {
	case "md", "doc", "docx", "odt", "rtf", "csv", "tsv", "xlsx", "xls", "ods", "ppt", "pptx", "key":
		return true
	default:
		return false
	}
}

func messageRequestsArtifactOutput(lower string) bool {
	return fallbackTaskContractMentionsArtifactOutput(lower)
}

func messageRequestsInvestigation(lower string) bool {
	return fallbackTaskContractMentionsInvestigation(lower)
}

func inferTaskContractAcceptance(deliverables []TaskContractDeliverable, missing []TaskContractMissingInfo, requiresExternalEffect, requiresApproval bool) []TaskContractAcceptance {
	out := []TaskContractAcceptance{{
		ID:       taskAcceptanceVisibleResult,
		Summary:  "Produce a user-visible result or summary, not just internal steps.",
		Required: true,
	}}
	if len(deliverables) > 0 {
		kinds := make([]string, 0, len(deliverables))
		for _, deliverable := range deliverables {
			if deliverable.Required {
				kinds = append(kinds, deliverable.Kind)
			}
		}
		if len(kinds) > 0 {
			out = append(out, TaskContractAcceptance{
				ID:               taskAcceptanceDeliverables,
				Summary:          "Leave the expected deliverables or evidence for the requested work.",
				Required:         true,
				DeliverableKinds: normalize.DedupeStrings(kinds),
			})
		}
	}
	if requiresExternalEffect {
		out = append(out, TaskContractAcceptance{
			ID:       taskAcceptanceExternalEffect,
			Summary:  "Do not claim completion unless the external side effect leaves delivery or execution evidence.",
			Required: true,
			EvidenceHints: []string{
				"delivery receipt",
				"notification output",
				"deployment target evidence",
			},
		})
	}
	if requiresApproval {
		out = append(out, TaskContractAcceptance{
			ID:       taskAcceptanceApproval,
			Summary:  "Approval must be respected before external or high-risk side effects.",
			Required: true,
			EvidenceHints: []string{
				"approval granted",
				"tool execution after approval",
			},
		})
	}
	if hasRequiredTaskContractMissingInfo(missing) {
		out = append(out, TaskContractAcceptance{
			ID:       "missing_info_resolved",
			Summary:  "Do not finish the run while required task inputs are still unresolved.",
			Required: true,
		})
	}
	return out
}

func taskContractRequiresExternalEffect(message, jobType string, domains []string, deliverables []TaskContractDeliverable) bool {
	if jobType == taskContractJobDelivery || jobType == taskContractJobDeployment || jobType == taskContractJobMonitor {
		return true
	}
	if taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery, taskDeliverableWatchAlert, taskDeliverableDeployment) {
		return true
	}
	if hasSemanticDomain(domains, DomainWatch, DomainCron) {
		return true
	}
	return taskExplicitlyRequestsMessageDelivery(message) ||
		taskExplicitlyRequestsExternalSubmission(message, domains) ||
		taskExplicitlyRequestsDeployment(message)
}

// taskContractRequiresApproval returns true when the task workflow shape
// needs human approval — delivery to other humans/systems, or deployment.
// Browser action risk is NOT judged here; per-tool approval is handled by
// the policy engine at execution time via ToolDefinition.RequiresApproval
// and SideEffectClass. Monitor/watch approval is handled by the watch
// intake flow. This separation avoids fragile keyword-based risk guessing
// at task intake time.
func taskContractRequiresApproval(message, jobType string, domains []string, deliverables []TaskContractDeliverable) bool {
	if jobType == taskContractJobDelivery || jobType == taskContractJobDeployment {
		return true
	}
	if taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery, taskDeliverableDeployment) {
		return true
	}
	if taskExplicitlyRequestsMessageDelivery(message) {
		return true
	}
	if taskExplicitlyRequestsDeployment(message) {
		return true
	}
	return false
}

func hasRequiredTaskContractMissingInfo(items []TaskContractMissingInfo) bool {
	for _, item := range items {
		if item.Required {
			return true
		}
	}
	return false
}

func taskMissingInfoLabel(id string) string {
	switch strings.TrimSpace(id) {
	case taskMissingInfoSourceTarget:
		return "目标对象"
	case taskMissingInfoDeliveryTarget:
		return "发送位置"
	case taskMissingInfoSchedule:
		return "执行时间"
	case taskMissingInfoDeploymentScope:
		return "部署目标"
	default:
		return "补充信息"
	}
}

func taskMissingInfoInputMode(id string) string {
	switch strings.TrimSpace(id) {
	case taskMissingInfoSourceTarget:
		return "reference"
	case taskMissingInfoDeliveryTarget:
		return "destination"
	case taskMissingInfoSchedule:
		return "schedule"
	case taskMissingInfoDeploymentScope:
		return "deployment_target"
	default:
		return "text"
	}
}

func taskMissingInfoPlaceholder(id string) string {
	switch strings.TrimSpace(id) {
	case taskMissingInfoSourceTarget:
		return "<文件路径 / URL / 仓库 / 截图>"
	case taskMissingInfoDeliveryTarget:
		return "<邮箱 / Slack 频道 / 飞书群 / 当前会话>"
	case taskMissingInfoSchedule:
		return "<例如 从下周一开始，每天 09:00>"
	case taskMissingInfoDeploymentScope:
		return "<staging / prod / 服务名 / URL>"
	default:
		return "<请补充>"
	}
}

func taskNeedsDeliveryTarget(message, jobType string, domains []string, deliverables []TaskContractDeliverable) bool {
	if taskIntentCancelsOrStops(message) {
		return false
	}
	if jobType == taskContractJobDelivery || taskContractDeliverablesContain(deliverables, taskDeliverableMessageDelivery) {
		return true
	}
	if !hasSemanticDomain(domains, DomainEmail, DomainChannel, DomainCalendar) {
		return false
	}
	return taskExplicitlyRequestsMessageDelivery(message)
}

func taskExplicitlyRequestsMessageDelivery(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if taskDeliveryNegated(lower) {
		return false
	}
	if fallbackTaskContractMentionsDeliveryIntent(lower) {
		return true
	}
	if taskRequestsExternalNotification(lower) && messageHasExternalDeliveryTarget(message) {
		return true
	}
	return false
}

// taskDeliveryNegated returns true when the message includes structured
// delivery-disable markers such as dry-run or send=false. Natural-language
// negation is intentionally left to the analyzer path.
func taskDeliveryNegated(lower string) bool {
	return fallbackTaskContractMentionsDeliveryNegation(lower)
}

func taskRequestsExternalNotification(lower string) bool {
	return fallbackTaskContractMentionsExternalNotification(lower)
}

func taskExplicitlyRequestsExternalSubmission(message string, domains []string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	hasBrowser := false
	for _, domain := range domains {
		if ToolDomain(domain) == DomainBrowser {
			hasBrowser = true
			break
		}
	}
	if !hasBrowser {
		return false
	}
	return fallbackTaskContractMentionsExternalSubmission(lower)
}

func taskNeedsSchedule(message string, mode ExecutionMode, jobType string, domains []string, deliverables []TaskContractDeliverable) bool {
	if taskIntentCancelsOrStops(message) {
		return false
	}
	if shouldSuppressWatchDomainsForBrowserContext(message, domains) {
		return false
	}
	if mode == ExecutionModeWatch || jobType == taskContractJobMonitor || jobType == taskContractJobAutomation {
		return true
	}
	if taskContractDeliverablesContain(deliverables, taskDeliverableWatchAlert) || hasSemanticDomain(domains, DomainWatch, DomainCron) {
		return true
	}
	return messageRequestsScheduledExecution(message)
}

func messageRequestsScheduledExecution(message string) bool {
	if messageHasSchedule(message) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(message))
	return fallbackTaskContractMentionsScheduledExecution(lower)
}

func taskIntentCancelsOrStops(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return fallbackTaskContractMentionsCancellation(lower)
}

func messageHasSchedule(message string) bool {
	trimmed := strings.TrimSpace(message)
	if taskContractCronPattern.MatchString(trimmed) ||
		taskContractTimePattern.MatchString(trimmed) ||
		taskContractISODatePattern.MatchString(trimmed) ||
		taskContractSlashDatePattern.MatchString(trimmed) ||
		taskContractStructuredSchedulePattern.MatchString(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return fallbackTaskContractMentionsScheduleReference(lower)
}

func messageHasDeliveryTarget(message string) bool {
	return messageTargetsCurrentConversation(message) || messageHasExternalDeliveryTarget(message)
}

func messageHasExternalDeliveryTarget(message string) bool {
	trimmed := strings.TrimSpace(message)
	if taskContractEmailPattern.MatchString(trimmed) ||
		taskContractChannelPattern.MatchString(trimmed) ||
		taskContractMentionPattern.MatchString(trimmed) ||
		(strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://")) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return fallbackTaskContractMentionsExternalDeliveryTarget(lower)
}

func messageTargetsCurrentConversation(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return fallbackTaskContractMentionsCurrentConversation(lower)
}

func messageHasDeploymentTarget(message string) bool {
	trimmed := strings.TrimSpace(message)
	if taskContractURLPattern.MatchString(trimmed) ||
		taskContractArtifactPattern.MatchString(trimmed) ||
		taskContractIPPattern.MatchString(trimmed) {
		return true
	}
	for _, token := range evidenceTokenPattern.FindAllString(strings.ToLower(trimmed), -1) {
		token = strings.Trim(token, ".,;:!?()[]{}<>\"'")
		if token == "" || strings.Contains(token, "/") || strings.Contains(token, `\`) {
			continue
		}
		if looksLikeLocalFileToken(token) {
			continue
		}
		if taskContractHostPattern.MatchString(token) {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	return fallbackTaskContractMentionsDeploymentTarget(lower)
}

func looksLikeLocalFileToken(token string) bool {
	switch strings.TrimPrefix(pathExt(token), ".") {
	case "md", "txt", "rtf", "doc", "docx", "odt",
		"csv", "tsv", "xlsx", "xls", "ods",
		"ppt", "pptx", "key",
		"pdf", "json", "yaml", "yml", "toml", "xml",
		"log", "go", "js", "ts", "tsx", "jsx", "py", "java", "rb", "rs",
		"c", "cc", "cpp", "h", "hpp", "sh", "bash", "zsh",
		"html", "css", "scss", "sql",
		"png", "jpg", "jpeg", "gif", "webp", "svg",
		"mp3", "wav", "m4a", "mp4", "mov":
		return true
	default:
		return false
	}
}

func pathExt(token string) string {
	lastDot := strings.LastIndex(token, ".")
	if lastDot < 0 || lastDot == len(token)-1 {
		return ""
	}
	return token[lastDot:]
}

func extractTaskContractTarget(message string) string {
	trimmed := strings.TrimSpace(message)
	for _, pattern := range []*regexp.Regexp{
		taskContractURLPattern,
		taskContractArtifactPattern,
		taskContractEmailPattern,
		taskContractPathPattern,
	} {
		if match := pattern.FindString(trimmed); match != "" {
			return strings.TrimSpace(match)
		}
	}
	return ""
}

func preflightHasCheck(report *RunPreflightReport, id string) bool {
	if report == nil {
		return false
	}
	id = strings.TrimSpace(id)
	for _, check := range report.Checks {
		if strings.TrimSpace(check.ID) == id {
			return true
		}
	}
	return false
}

func taskContractEventMap(contract *TaskContract) map[string]any {
	if contract == nil {
		return nil
	}
	return map[string]any{
		"job_type":                   strings.TrimSpace(contract.JobType),
		"confidence":                 contract.Confidence,
		"requires_external_effect":   contract.RequiresExternalEffect,
		"requires_approval":          contract.RequiresApproval,
		"capability_hint_count":      len(contract.CapabilityHints),
		"expected_deliverable_count": len(contract.ExpectedDeliverables),
		"missing_info_count":         len(contract.MissingInfo),
		"resolved_info_count":        len(contract.ResolvedInfo),
	}
}

func applyTaskContractClarifications(contract *TaskContract, metadata map[string]any) {
	if contract == nil || len(metadata) == 0 {
		return
	}
	values := parseClarificationSlotValues(metadata)
	if len(values) == 0 {
		return
	}
	remaining := make([]TaskContractMissingInfo, 0, len(contract.MissingInfo))
	resolved := make([]TaskContractResolvedInfo, 0, len(values))
	seenResolved := make(map[string]struct{}, len(values))
	for _, item := range contract.MissingInfo {
		value := strings.TrimSpace(values[item.ID])
		if value == "" {
			remaining = append(remaining, item)
			continue
		}
		seenResolved[item.ID] = struct{}{}
		resolved = append(resolved, TaskContractResolvedInfo{
			ID:        item.ID,
			Label:     normalize.FirstNonEmpty(item.Label, taskMissingInfoLabel(item.ID)),
			Value:     value,
			Source:    "clarification",
			InputMode: normalize.FirstNonEmpty(item.InputMode, taskMissingInfoInputMode(item.ID)),
		})
	}
	for id, value := range values {
		id = strings.TrimSpace(id)
		value = strings.TrimSpace(value)
		if id == "" || value == "" {
			continue
		}
		if _, ok := seenResolved[id]; ok {
			continue
		}
		resolved = append(resolved, TaskContractResolvedInfo{
			ID:        id,
			Label:     taskMissingInfoLabel(id),
			Value:     value,
			Source:    "clarification",
			InputMode: taskMissingInfoInputMode(id),
		})
	}
	if len(remaining) == 0 {
		contract.MissingInfo = nil
	} else {
		contract.MissingInfo = remaining
	}
	if len(resolved) > 0 {
		contract.ResolvedInfo = append(contract.ResolvedInfo, resolved...)
	}
}

func parseClarificationSlotValues(metadata map[string]any) map[string]string {
	raw, ok := metadata[MetadataKeyClarificationSlots]
	if !ok || raw == nil {
		return nil
	}
	out := make(map[string]string)
	switch typed := raw.(type) {
	case map[string]string:
		for id, value := range typed {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				out[strings.TrimSpace(id)] = trimmed
			}
		}
	case map[string]any:
		for id, value := range typed {
			trimmedID := strings.TrimSpace(id)
			if trimmedID == "" || value == nil {
				continue
			}
			if trimmed := strings.TrimSpace(valueToString(value)); trimmed != "" {
				out[trimmedID] = trimmed
			}
		}
	case []map[string]any:
		for _, item := range typed {
			id := strings.TrimSpace(valueToString(item["id"]))
			value := strings.TrimSpace(valueToString(item["value"]))
			if id != "" && value != "" {
				out[id] = value
			}
		}
	case []any:
		for _, rawItem := range typed {
			item, _ := rawItem.(map[string]any)
			id := strings.TrimSpace(valueToString(item["id"]))
			value := strings.TrimSpace(valueToString(item["value"]))
			if id != "" && value != "" {
				out[id] = value
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func valueToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
