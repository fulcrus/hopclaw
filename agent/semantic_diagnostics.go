package agent

// SemanticPipelineSummary describes the shared semantic analyzer chain that
// feeds triage, preflight, and task-contract analysis.
type SemanticPipelineSummary struct {
	SharedSignalEnabled         bool     `json:"shared_signal_enabled"`
	LanguageProfileEnabled      bool     `json:"language_profile_enabled"`
	MainPathLanguageFamilies    []string `json:"main_path_language_families,omitempty"`
	RunTriageConfigured         bool     `json:"run_triage_configured"`
	PreflightAnalyzerConfigured bool     `json:"preflight_analyzer_configured"`
	TaskContractConfigured      bool     `json:"task_contract_analyzer_configured"`
}

// SemanticPipelineSummary returns the configured semantic analyzer chain in a
// form that operator diagnostics can expose directly.
func (a *AgentComponent) SemanticPipelineSummary() SemanticPipelineSummary {
	if a == nil {
		return SemanticPipelineSummary{}
	}
	return SemanticPipelineSummary{
		SharedSignalEnabled:         true,
		LanguageProfileEnabled:      true,
		MainPathLanguageFamilies:    SupportedSemanticLanguageFamilies(),
		RunTriageConfigured:         a.runTriage != nil,
		PreflightAnalyzerConfigured: a.preflight != nil,
		TaskContractConfigured:      a.taskContract != nil,
	}
}
