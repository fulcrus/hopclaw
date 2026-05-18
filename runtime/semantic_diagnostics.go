package runtime

import "github.com/fulcrus/hopclaw/agent"

// SemanticIngressSummary describes which semantic ingress path is active and
// whether the shared analyzer stages are configured.
type SemanticIngressSummary struct {
	SharedSignalEnabled                  bool     `json:"shared_signal_enabled"`
	LanguageProfileEnabled               bool     `json:"language_profile_enabled"`
	MainPath                             string   `json:"main_path"`
	MainPathLanguageFamilies             []string `json:"main_path_language_families,omitempty"`
	InteractionIngressConfigured         bool     `json:"interaction_ingress_configured"`
	LegacyInteractionClassifierConfigured bool    `json:"legacy_interaction_classifier_configured"`
	LegacyAutomationClassifierConfigured bool     `json:"legacy_automation_classifier_configured"`
	RunTriageConfigured                  bool     `json:"run_triage_configured"`
	PreflightAnalyzerConfigured          bool     `json:"preflight_analyzer_configured"`
	TaskContractConfigured               bool     `json:"task_contract_analyzer_configured"`
}

// SemanticIngressSummary returns a diagnostic snapshot of the semantic ingress
// and submit-analysis pipeline.
func (s *Service) SemanticIngressSummary() SemanticIngressSummary {
	if s == nil {
		return SemanticIngressSummary{}
	}

	summary := SemanticIngressSummary{
		MainPath:                              "deterministic_only",
		InteractionIngressConfigured:          s.ingressClassifier != nil,
		LegacyInteractionClassifierConfigured: s.classifier != nil,
		LegacyAutomationClassifierConfigured:  s.automationClassifier != nil,
	}
	switch {
	case s.ingressClassifier != nil:
		summary.MainPath = "interaction_ingress"
	case s.classifier != nil:
		summary.MainPath = "legacy_interaction_classifier"
	}

	pipeline := agent.SemanticPipelineSummary{}
	if s.agent != nil {
		pipeline = s.agent.SemanticPipelineSummary()
	}
	summary.SharedSignalEnabled = pipeline.SharedSignalEnabled
	summary.LanguageProfileEnabled = pipeline.LanguageProfileEnabled
	summary.MainPathLanguageFamilies = append([]string(nil), pipeline.MainPathLanguageFamilies...)
	summary.RunTriageConfigured = pipeline.RunTriageConfigured
	summary.PreflightAnalyzerConfigured = pipeline.PreflightAnalyzerConfigured
	summary.TaskContractConfigured = pipeline.TaskContractConfigured
	return summary
}
