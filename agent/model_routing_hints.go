package agent

import (
	"github.com/fulcrus/hopclaw/modelrouter"
)

func routeCapabilitiesForTurn(run *Run, prompt string, tools []ToolDefinition) []modelrouter.Capability {
	harness := buildRunHarnessSpec(run, nil, prompt, tools)
	required := requiredCapabilities(tools)
	if harness.Model.RequireThinking {
		required = append(required, modelrouter.CapabilityThinking)
	}
	return required
}

func routingDomainsForTurn(run *Run, prompt string) []string {
	domains := make([]string, 0, 8)
	if run != nil {
		if run.Preflight != nil {
			domains = append(domains, run.Preflight.DetectedDomains...)
			domains = append(domains, run.Preflight.SuggestedDomains...)
		}
		if run.TaskContract != nil {
			domains = append(domains, run.TaskContract.SuggestedDomains...)
		}
	}
	heuristic := domainsToStrings(detectStructuredEvidence(prompt))
	if len(domains) == 0 {
		return heuristic
	}
	if len(heuristic) == 0 {
		return normalizeSemanticDomains(domains)
	}
	return sanitizeSuggestedDomainsForMessage(prompt, normalizeSemanticDomains(append(domains, heuristic...)))
}
