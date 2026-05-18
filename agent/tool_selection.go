package agent

func (a *AgentComponent) prepareRunTools(run *Run, tools []ToolDefinition, userMessage string) []ToolDefinition {
	return a.prepareRunToolsWithSessionContext(run, tools, userMessage, "")
}

func (a *AgentComponent) prepareRunToolsWithSessionContext(run *Run, tools []ToolDefinition, userMessage, sessionSummary string) []ToolDefinition {
	signals := toolSelectionSignalsFromRunContext(run, userMessage, sessionSummary)
	return prepareToolsForModelWithSignals(filterModelVisibleTools(tools), userMessage, signals)
}

func toolSelectionSignalsFromRunContext(run *Run, userMessage, sessionSummary string) toolSelectionSignals {
	activated := activatedDomainsFromRun(run)
	var contract *TaskContract
	if run != nil {
		contract = run.TaskContract
	}
	if shouldPreferSessionBrowserToolPool(activated, sessionSummary, userMessage, contract) {
		activated = browserFollowUpDomains(activated, userMessage)
		signals := inferToolSelectionSignals(userMessage, activated, contract)
		signals.reuseSessionBrowserContext = true
		signals.browserSearchResultsContext = sessionHasSearchResultsContext(sessionSummary)
		signals.browserFocusDomains = buildBrowserFocusDomains(userMessage, activated, true)
		return signals
	}
	signals := inferToolSelectionSignals(userMessage, activated, contract)
	return signals
}

func activatedDomainsFromRun(run *Run) map[ToolDomain]bool {
	if run == nil {
		return nil
	}
	values := make([]string, 0, 8)
	if run.Preflight != nil {
		values = append(values, run.Preflight.DetectedDomains...)
	}
	if run.TaskContract != nil {
		values = append(values, run.TaskContract.SuggestedDomains...)
	}
	if run.Triage != nil {
		values = append(values, run.Triage.SuggestedDomains...)
	}
	values = normalizeSemanticDomains(values)
	if len(values) == 0 {
		return nil
	}
	out := make(map[ToolDomain]bool, len(values))
	for _, item := range values {
		out[ToolDomain(item)] = true
	}
	if deriveEffectiveDelegationContract(run, run.Delegation) != nil {
		out[DomainAgent] = true
	}
	return out
}

func shouldPreferSessionBrowserToolPool(activated map[ToolDomain]bool, sessionSummary, userMessage string, contract *TaskContract) bool {
	if contract != nil && contract.JobType == taskContractJobDelivery {
		return false
	}
	if !activated[DomainBrowser] {
		return false
	}
	if !sessionHasBrowserReferenceContext(sessionSummary) {
		return false
	}
	if !messageCanReuseSessionBrowserReference(userMessage, sessionSummary) {
		return false
	}
	return true
}

func browserFollowUpDomains(activated map[ToolDomain]bool, userMessage string) map[ToolDomain]bool {
	out := map[ToolDomain]bool{
		DomainBrowser: true,
	}
	for domain, enabled := range activated {
		if !enabled || !browserFocusCompanionDomains[domain] {
			continue
		}
		out[domain] = true
	}
	if browserRequestNeedsWorkspaceTools(userMessage) {
		out[DomainFS] = true
	}
	return out
}

func filterModelVisibleTools(tools []ToolDefinition) []ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if !toolVisibleToModel(tool) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func toolVisibleToModel(tool ToolDefinition) bool {
	switch tool.Availability.Status {
	case "", AvailabilityReady, AvailabilityDegraded:
	default:
		return false
	}
	if !tool.Eligible && tool.Availability.Status == "" && len(tool.EligibilityReasons) > 0 {
		return false
	}
	return true
}
