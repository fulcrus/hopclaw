package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
	"github.com/fulcrus/hopclaw/internal/semanticschema"
)

const DefaultPreflightAnalyzerTimeout = 2 * time.Second

type ModelPreflightAnalyzer struct {
	model        ModelClient
	timeout      time.Duration
	defaultModel string
}

func NewModelPreflightAnalyzer(model ModelClient, timeout time.Duration) *ModelPreflightAnalyzer {
	if model == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultPreflightAnalyzerTimeout
	}
	return &ModelPreflightAnalyzer{model: model, timeout: timeout}
}

func (a *ModelPreflightAnalyzer) WithDefaultModel(model string) *ModelPreflightAnalyzer {
	if a != nil {
		a.defaultModel = strings.TrimSpace(model)
	}
	return a
}

func (a *ModelPreflightAnalyzer) Analyze(ctx context.Context, req PreflightAnalysisRequest) (PreflightAnalysis, error) {
	if a == nil || a.model == nil {
		return PreflightAnalysis{}, ErrModelClientNil
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	payload, err := json.Marshal(req)
	if err != nil {
		return PreflightAnalysis{}, err
	}
	modelName := strings.TrimSpace(req.Model)
	if a.defaultModel != "" {
		modelName = a.defaultModel
	}
	response, err := a.model.Chat(ctx, ChatRequest{
		Model:        modelName,
		SystemPrompt: preflightAnalyzerSystemPrompt,
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: string(payload),
		}},
		Budget: contextengine.Budget{
			ContextWindow:  2048,
			MaxInputTokens: 1024,
			ReservedOutput: 220,
		},
	})
	if err != nil || response == nil {
		if err != nil {
			return PreflightAnalysis{}, err
		}
		return PreflightAnalysis{}, ErrModelClientNil
	}
	return parsePreflightAnalysis(response.Message.Content), nil
}

var preflightAnalyzerSystemPrompt = semanticschema.BuildPreflightAnalyzerPrompt()

func (a *AgentComponent) buildRunPreflight(ctx context.Context, msg IncomingMessage, session *Session, mode ExecutionMode, signal *SemanticSignal) *RunPreflightReport {
	defaultModel := strings.TrimSpace(msg.Model)
	if a != nil {
		defaultModel = defaultString(msg.Model, a.config.DefaultModel)
	}
	req := PreflightAnalysisRequest{
		Model:          defaultModel,
		Message:        strings.TrimSpace(msg.Content),
		ExecutionMode:  mode,
		SemanticSignal: cloneSemanticSignal(signal),
	}
	if session != nil {
		req.SessionSummary = sessionReferenceSummary(session)
	}
	analysis := fallbackPreflightAnalysis(req)
	analyzerReady := false
	if a != nil && a.preflight != nil {
		if decided, err := a.preflight.Analyze(ctx, req); err == nil && preflightAnalysisHasSemanticContent(decided) {
			analysis = normalizePreflightAnalysisWithOptions(decided, req, false)
			analyzerReady = true
		}
	}
	if signal != nil {
		signal.PreflightAnalyzerReady = analyzerReady
	}
	applyTriageAnalysisToSemanticSignal(signal, mode, analysis)
	return buildRunPreflightWithAnalysis(req.Message, analysis)
}

func parsePreflightAnalysis(raw string) PreflightAnalysis {
	var analysis PreflightAnalysis
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &analysis); err != nil {
		return PreflightAnalysis{}
	}
	var fields map[string]json.RawMessage
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &fields); err == nil {
		_, analysis.NeedsReferenceSet = fields["needs_reference"]
		_, analysis.NeedsConfirmSet = fields["needs_confirmation"]
		_, analysis.DomainsSpecified = fields["suggested_domains"]
		_, analysis.DetectedDomainsSpecified = fields["detected_domains"]
		if !analysis.DetectedDomainsSpecified {
			if rawDomains, ok := fields["domains"]; ok {
				analysis.DetectedDomainsSpecified = true
				_ = json.Unmarshal(rawDomains, &analysis.DetectedDomains)
			}
		}
	}
	return analysis
}

func normalizePreflightAnalysis(analysis PreflightAnalysis, req PreflightAnalysisRequest) PreflightAnalysis {
	return normalizePreflightAnalysisWithOptions(analysis, req, true)
}

func normalizePreflightAnalysisWithOptions(analysis PreflightAnalysis, req PreflightAnalysisRequest, allowHeuristicDomains bool) PreflightAnalysis {
	if analysis.NeedsReference && !analysis.NeedsReferenceSet {
		analysis.NeedsReferenceSet = true
	}
	if analysis.NeedsConfirmation && !analysis.NeedsConfirmSet {
		analysis.NeedsConfirmSet = true
	}
	if len(analysis.SuggestedDomains) > 0 && !analysis.DomainsSpecified {
		analysis.DomainsSpecified = true
	}
	if len(analysis.DetectedDomains) > 0 && !analysis.DetectedDomainsSpecified {
		analysis.DetectedDomainsSpecified = true
	}
	normalized := normalizeSemanticDomains(analysis.SuggestedDomains)
	detected := normalizeSemanticDomains(analysis.DetectedDomains)
	if len(detected) == 0 && !analysis.DetectedDomainsSpecified && analysis.DomainsSpecified && len(normalized) > 0 {
		detected = append([]string(nil), normalized...)
	}
	if len(normalized) == 0 && req.SemanticSignal != nil {
		normalized = semanticSignalDomains(req.SemanticSignal)
	}
	heuristicDomains := domainsToStrings(detectStructuredEvidence(req.Message))
	if allowHeuristicDomains && len(normalized) == 0 && !analysis.DomainsSpecified && !analysis.DetectedDomainsSpecified {
		heuristicDomains = domainsToStrings(fallbackHeuristicDomains(req.Message))
	}
	if allowHeuristicDomains && len(normalized) == 0 {
		normalized = heuristicDomains
	}
	if allowHeuristicDomains && len(detected) == 0 && !analysis.DetectedDomainsSpecified {
		detected = heuristicDomains
	}
	normalized = augmentFallbackBrowserDomains(req, normalized)
	detected = augmentFallbackBrowserDomains(req, detected)
	normalized = sanitizeSuggestedDomainsForMessage(req.Message, normalized)
	detected = sanitizeSuggestedDomainsForMessage(req.Message, detected)
	if analysis.BrowserContextOnly || (req.SemanticSignal != nil && req.SemanticSignal.BrowserContextOnly) {
		normalized = removeSemanticDomains(normalized, DomainWatch, DomainCron)
		detected = removeSemanticDomains(detected, DomainWatch, DomainCron)
	}
	analysis.SuggestedDomains = normalized
	analysis.DetectedDomains = detected
	if !analysis.NeedsReferenceSet && req.SemanticSignal != nil && req.SemanticSignal.NeedsReferenceSet {
		analysis.NeedsReference = req.SemanticSignal.NeedsReference
		analysis.NeedsReferenceSet = true
	}
	if !analysis.NeedsConfirmSet && req.SemanticSignal != nil && req.SemanticSignal.NeedsConfirmSet {
		analysis.NeedsConfirmation = req.SemanticSignal.NeedsConfirmation
		analysis.NeedsConfirmSet = true
	}
	if !analysis.NeedsReferenceSet {
		analysis.NeedsReference = inferredNeedsReference(req, normalized)
	}
	if analysis.NeedsReference && containsConcreteReference(req.Message) {
		analysis.NeedsReference = false
	}
	return analysis
}

func preflightAnalysisHasSemanticContent(analysis PreflightAnalysis) bool {
	return analysis.NeedsReferenceSet ||
		analysis.NeedsConfirmSet ||
		analysis.DomainsSpecified ||
		analysis.DetectedDomainsSpecified ||
		analysis.NeedsReference ||
		analysis.NeedsConfirmation ||
		len(analysis.SuggestedDomains) > 0 ||
		len(analysis.DetectedDomains) > 0
}

func fallbackPreflightAnalysis(req PreflightAnalysisRequest) PreflightAnalysis {
	analysis := semanticSignalPreflightAnalysis(req.SemanticSignal)
	domains := analysis.SuggestedDomains
	if len(domains) == 0 {
		domains = domainsToStrings(fallbackHeuristicDomains(req.Message))
	}
	domains = augmentFallbackBrowserDomains(req, domains)
	analysis.SuggestedDomains = domains
	if len(domains) > 0 {
		analysis.DomainsSpecified = true
		analysis.DetectedDomains = append([]string(nil), domains...)
		analysis.DetectedDomainsSpecified = true
	}
	if !analysis.NeedsReferenceSet {
		analysis.NeedsReference = inferredNeedsReference(req, domains)
	}
	return analysis
}

func augmentFallbackBrowserDomains(req PreflightAnalysisRequest, domains []string) []string {
	out := normalizeSemanticDomains(domains)
	lower := strings.ToLower(strings.TrimSpace(req.Message))
	if lower == "" {
		return out
	}
	hasBrowserContext := fallbackPreflightMentionsBrowserContext(lower) ||
		fallbackTaskContractMentionsBrowserReference(lower) ||
		looksLikeSearchResultsExtractionRequest(req.Message)
	hasSessionBrowserFollowUp := sessionHasBrowserReferenceContext(req.SessionSummary) &&
		messageCanReuseSessionBrowserReference(req.Message, req.SessionSummary) &&
		messageLooksLikeBrowserFollowUp(lower)
	if !hasBrowserContext && !hasSessionBrowserFollowUp {
		return out
	}
	if hasSemanticDomain(out, DomainBrowser) {
		return out
	}
	out = append(out, string(DomainBrowser))
	return normalizeSemanticDomains(out)
}

func messageLooksLikeBrowserFollowUp(lower string) bool {
	return containsAny(lower,
		"click", "button", "submit", "input", "selector", "form", "type", "fill", "wait", "snapshot", "screenshot", "page title",
		"点击", "按钮", "提交", "输入", "选择器", "表单", "填写", "等待", "截图", "标题", "页面标题",
	)
}

func inferredNeedsReference(req PreflightAnalysisRequest, domains []string) bool {
	return inferredNeedsReferenceWithContract(req, domains, nil)
}

func inferredNeedsReferenceWithContract(req PreflightAnalysisRequest, domains []string, contract *TaskContract) bool {
	if hasConcreteSourceReference(req.Message, domains) {
		return false
	}
	if !messageNeedsConcreteReference(req.Message, domains, contract) {
		return false
	}
	if sessionHasBrowserReferenceContext(req.SessionSummary) && messageCanReuseSessionBrowserReference(req.Message, req.SessionSummary) {
		return false
	}
	return true
}

func messageNeedsConcreteReference(message string, domains []string, contract *TaskContract) bool {
	if messageNeedsBrowserReference(message, domains) {
		return true
	}
	if messageNeedsWorkspaceReference(message, domains) {
		return true
	}
	return taskContractNeedsConcreteReference(contract, domains)
}

func taskContractNeedsConcreteReference(contract *TaskContract, domains []string) bool {
	if contract == nil {
		return false
	}
	if contractMissingInfoRequired(contract, taskMissingInfoSourceTarget) {
		return true
	}
	if !hasSemanticDomain(domains, DomainBrowser) {
		return false
	}
	if taskContractDeliverablesContain(contract.ExpectedDeliverables, taskDeliverableBrowserEvidence) {
		return true
	}
	switch contract.JobType {
	case taskContractJobResearch, taskContractJobReport, taskContractJobMonitor, taskContractJobGeneral:
		return true
	default:
		return contract.RequiresExternalEffect
	}
}

func contractMissingInfoRequired(contract *TaskContract, id string) bool {
	if contract == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, item := range contract.MissingInfo {
		if strings.TrimSpace(item.ID) != id {
			continue
		}
		if item.Required {
			return true
		}
	}
	return false
}

func messageNeedsBrowserReference(message string, domains []string) bool {
	return hasSemanticDomain(domains, DomainBrowser)
}

func messageNeedsWorkspaceReference(message string, domains []string) bool {
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
	if fallbackPreflightMentionsWorkspaceReferenceSubject(lower) {
		return true
	}
	if !messageLooksLikeExistingSourceOperation(lower) {
		return false
	}
	if messageMentionsWorkspaceArtifact(lower) {
		return true
	}
	return hasSemanticDomain(domains,
		DomainFS,
		DomainDocument,
		DomainSheet,
		DomainPresentation,
		DomainPDF,
		DomainMedia,
		DomainGit,
	)
}

func messageLooksLikeWorkspaceExploratoryRead(lower string) bool {
	if fallbackPreflightMentionsWorkspaceMutation(lower) {
		return false
	}
	return fallbackPreflightMentionsWorkspaceRead(lower)
}

func messageLooksLikeExistingSourceOperation(lower string) bool {
	return fallbackPreflightMentionsWorkspaceMutation(lower) || fallbackPreflightMentionsExistingSourceReview(lower)
}

func messageMentionsWorkspaceArtifact(lower string) bool {
	return fallbackPreflightMentionsWorkspaceArtifact(lower)
}

func sessionHasBrowserReferenceContext(summary string) bool {
	ctx, ok := browserReferenceContextFromSummary(summary)
	if !ok {
		return false
	}
	return browserReferenceContextLooksLikeNavigablePage(ctx)
}

func messageCanReuseSessionBrowserReference(message, sessionSummary string) bool {
	ctx, ok := browserReferenceContextFromSummary(sessionSummary)
	if !ok || !browserReferenceContextLooksLikeNavigablePage(ctx) {
		return false
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	explicitURL := strings.TrimSpace(taskContractURLPattern.FindString(message))
	if explicitURL == "" {
		explicitURL = strings.TrimSpace(taskContractArtifactPattern.FindString(message))
	}
	if explicitURL != "" {
		if normalizeBrowserReferenceURL(ctx.URL) == "" {
			return false
		}
		return normalizeBrowserReferenceURL(explicitURL) == normalizeBrowserReferenceURL(ctx.URL)
	}

	lower := strings.ToLower(message)
	if lower == "" {
		return false
	}
	if fallbackPreflightRequestsSearchResultsReuse(lower) || fallbackPreflightMentionsSearchResultsContext(lower) {
		return sessionHasSearchResultsContext(sessionSummary)
	}
	if fallbackPreflightRequestsFreshNavigation(lower) {
		return false
	}
	if messageLooksLikeBrowserFollowUp(lower) {
		return true
	}
	return messageHasPageScopedBrowserReference(message)
}

func messageHasPageScopedBrowserReference(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return false
	}
	if ctx, ok := browserReferenceContextFromSummary(trimmed); ok {
		return browserReferenceContextLooksLikeNavigablePage(ctx)
	}
	for _, rawURL := range explicitBrowserReferenceURLs(trimmed) {
		if browserURLLooksLikeNavigablePage(rawURL) {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	if fallbackPreflightMentionsBrowserContext(lower) {
		return true
	}
	return containsAny(lower,
		"页面", "网页", "当前页面", "该页面",
		"page", "current page", "this page", "the page", "current tab",
		"página", "esta página",
	)
}

func sessionHasSearchResultsContext(summary string) bool {
	ctx, ok := browserReferenceContextFromSummary(summary)
	if !ok {
		return false
	}
	return browserReferenceContextLooksLikeSearchResults(ctx)
}

func hasConcreteSourceReference(message string, domains []string) bool {
	if strings.Contains(message, "http://") || strings.Contains(message, "https://") || strings.Contains(message, "artifact://") {
		return true
	}
	hasBrowser := false
	for _, domain := range domains {
		if ToolDomain(strings.TrimSpace(domain)) == DomainBrowser {
			hasBrowser = true
			break
		}
	}
	if !hasBrowser {
		return containsConcreteReference(message)
	}
	return false
}
