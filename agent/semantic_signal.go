package agent

import (
	"strings"
	"time"
)

type SemanticSignal struct {
	Message                string          `json:"message,omitempty"`
	SessionSummary         string          `json:"session_summary,omitempty"`
	Language               LanguageProfile `json:"language,omitempty"`
	ExecutionMode          ExecutionMode   `json:"execution_mode,omitempty"`
	RequiresCurrentInfo    bool            `json:"requires_current_info,omitempty"`
	NeedsReference         bool            `json:"needs_reference,omitempty"`
	NeedsReferenceSet      bool            `json:"needs_reference_set,omitempty"`
	NeedsConfirmation      bool            `json:"needs_confirmation,omitempty"`
	NeedsConfirmSet        bool            `json:"needs_confirmation_set,omitempty"`
	SuggestedDomains       []string        `json:"suggested_domains,omitempty"`
	DomainsSpecified       bool            `json:"domains_specified,omitempty"`
	BrowserContextOnly     bool            `json:"browser_context_only,omitempty"`
	JobType                string          `json:"job_type,omitempty"`
	TargetSummary          string          `json:"target_summary,omitempty"`
	CapabilityHints        []string        `json:"capability_hints,omitempty"`
	DeliverableKinds       []string        `json:"deliverable_kinds,omitempty"`
	MissingInfoIDs         []string        `json:"missing_info_ids,omitempty"`
	MissingInfoSpecified   bool            `json:"missing_info_specified,omitempty"`
	RequiresExternalEffect *bool           `json:"requires_external_effect,omitempty"`
	RequiresApproval       *bool           `json:"requires_approval,omitempty"`
	Reason                 string          `json:"reason,omitempty"`
	Confidence             float64         `json:"confidence,omitempty"`
	PreflightAnalyzerReady bool            `json:"preflight_analyzer_ready,omitempty"`
	TriageReady            bool            `json:"triage_ready,omitempty"`
	TaskContractReady      bool            `json:"task_contract_ready,omitempty"`
	GeneratedAt            time.Time       `json:"generated_at,omitempty"`
}

func newSemanticSignal(message string, session *Session) *SemanticSignal {
	signal := &SemanticSignal{
		Message:     strings.TrimSpace(message),
		GeneratedAt: time.Now().UTC(),
	}
	signal.Language = detectLanguageProfile(signal.Message)
	if session != nil {
		signal.SessionSummary = strings.TrimSpace(session.Summary)
	}
	return signal
}

func initializeSemanticSignal(message string, session *Session, seed *SemanticSignal) *SemanticSignal {
	if seed == nil {
		return newSemanticSignal(message, session)
	}
	signal := cloneSemanticSignal(seed)
	if signal == nil {
		return newSemanticSignal(message, session)
	}
	if trimmed := strings.TrimSpace(message); trimmed != "" {
		signal.Message = trimmed
	} else {
		signal.Message = strings.TrimSpace(signal.Message)
	}
	if session != nil && strings.TrimSpace(signal.SessionSummary) == "" {
		signal.SessionSummary = strings.TrimSpace(session.Summary)
	}
	signal.Language = mergeLanguageProfile(signal.Language, detectLanguageProfile(signal.Message))
	if signal.GeneratedAt.IsZero() {
		signal.GeneratedAt = time.Now().UTC()
	}
	return signal
}

func cloneSemanticSignal(in *SemanticSignal) *SemanticSignal {
	if in == nil {
		return nil
	}
	out := *in
	out.SuggestedDomains = cloneStrings(in.SuggestedDomains)
	out.CapabilityHints = cloneStrings(in.CapabilityHints)
	out.DeliverableKinds = cloneStrings(in.DeliverableKinds)
	out.MissingInfoIDs = cloneStrings(in.MissingInfoIDs)
	if in.RequiresExternalEffect != nil {
		value := *in.RequiresExternalEffect
		out.RequiresExternalEffect = &value
	}
	if in.RequiresApproval != nil {
		value := *in.RequiresApproval
		out.RequiresApproval = &value
	}
	return &out
}

func CloneSemanticSignal(in *SemanticSignal) *SemanticSignal {
	return cloneSemanticSignal(in)
}

func diagnosticSemanticSignal(in *SemanticSignal) *SemanticSignal {
	out := cloneSemanticSignal(in)
	if out == nil {
		return nil
	}
	out.Message = ""
	out.SessionSummary = ""
	return out
}

func semanticSignalDomains(signal *SemanticSignal) []string {
	if signal == nil {
		return nil
	}
	return normalizeSemanticDomains(signal.SuggestedDomains)
}

func applyTriageAnalysisToSemanticSignal(signal *SemanticSignal, mode ExecutionMode, analysis PreflightAnalysis) {
	if signal == nil {
		return
	}
	signal.ExecutionMode = mode
	signal.NeedsReference = analysis.NeedsReference
	signal.NeedsReferenceSet = analysis.NeedsReferenceSet || analysis.NeedsReference
	signal.NeedsConfirmation = analysis.NeedsConfirmation
	signal.NeedsConfirmSet = analysis.NeedsConfirmSet || analysis.NeedsConfirmation
	if len(analysis.SuggestedDomains) > 0 {
		signal.SuggestedDomains = normalizeSemanticDomains(analysis.SuggestedDomains)
		signal.DomainsSpecified = true
	}
	if strings.TrimSpace(analysis.Reason) != "" {
		signal.Reason = strings.TrimSpace(analysis.Reason)
	}
	signal.BrowserContextOnly = signal.BrowserContextOnly || analysis.BrowserContextOnly
	if analysis.Confidence > signal.Confidence {
		signal.Confidence = analysis.Confidence
	}
	signal.TriageReady = true
}

func applyRunTriageTraceToSemanticSignal(signal *SemanticSignal, mode ExecutionMode, report *RunPreflightReport, trace *RunTriageTrace) {
	if signal == nil {
		return
	}
	if trace != nil {
		signal.RequiresCurrentInfo = signal.RequiresCurrentInfo || trace.RequiresCurrentInfo
		applyTriageAnalysisToSemanticSignal(signal, mode, PreflightAnalysis{
			NeedsReference:    trace.NeedsReference,
			NeedsReferenceSet: trace.NeedsReferenceSet || trace.NeedsReference,
			NeedsConfirmation: trace.NeedsConfirmation,
			NeedsConfirmSet:   trace.NeedsConfirmSet || trace.NeedsConfirmation,
			SuggestedDomains:  cloneStrings(trace.SuggestedDomains),
			DomainsSpecified:  len(trace.SuggestedDomains) > 0,
			Reason:            trace.Reason,
			Confidence:        trace.Confidence,
		})
		return
	}
	applyTriageAnalysisToSemanticSignal(signal, mode, triageAnalysisFromPreflight(report, "", 0))
}

func semanticSignalPreflightAnalysis(signal *SemanticSignal) PreflightAnalysis {
	if signal == nil {
		return PreflightAnalysis{}
	}
	return PreflightAnalysis{
		NeedsReference:    signal.NeedsReference,
		NeedsReferenceSet: signal.NeedsReferenceSet,
		NeedsConfirmation: signal.NeedsConfirmation,
		NeedsConfirmSet:   signal.NeedsConfirmSet,
		SuggestedDomains:  cloneStrings(signal.SuggestedDomains),
		DomainsSpecified:  signal.DomainsSpecified,
		Reason:            signal.Reason,
		Confidence:        signal.Confidence,
	}
}

func applyTaskContractAnalysisToSemanticSignal(signal *SemanticSignal, analysis TaskContractAnalysis) {
	if signal == nil {
		return
	}
	if jobType := normalizeTaskContractJobType(analysis.JobType); jobType != "" {
		signal.JobType = jobType
	}
	if target := strings.TrimSpace(analysis.TargetSummary); target != "" {
		signal.TargetSummary = target
	}
	if len(analysis.SuggestedDomains) > 0 {
		signal.SuggestedDomains = normalizeSemanticDomains(analysis.SuggestedDomains)
		signal.DomainsSpecified = true
	}
	signal.BrowserContextOnly = signal.BrowserContextOnly || analysis.BrowserContextOnly
	signal.CapabilityHints = normalizeCapabilityHints(analysis.CapabilityHints)
	if analysis.DeliverableKinds != nil {
		signal.DeliverableKinds = normalizeTaskContractDeliverableKinds(analysis.DeliverableKinds)
	}
	if analysis.MissingInfoSpecified {
		signal.MissingInfoIDs = normalizeTaskContractMissingInfoIDs(analysis.MissingInfoIDs)
		signal.MissingInfoSpecified = true
	}
	if analysis.RequiresExternalEffect != nil {
		value := *analysis.RequiresExternalEffect
		signal.RequiresExternalEffect = &value
	}
	if analysis.RequiresApproval != nil {
		value := *analysis.RequiresApproval
		signal.RequiresApproval = &value
	}
	if strings.TrimSpace(analysis.Reason) != "" {
		signal.Reason = strings.TrimSpace(analysis.Reason)
	}
	if analysis.Confidence > signal.Confidence {
		signal.Confidence = analysis.Confidence
	}
	signal.TaskContractReady = true
}
