package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/triage"
)

const sessionRunTriageCacheKey = "run_triage_cache"
const runTriageCacheTTL = 10 * time.Minute

var triageLog = logging.WithSubsystem("agent")

type cachedRunTriage struct {
	Signature string              `json:"signature"`
	Mode      ExecutionMode       `json:"mode"`
	Preflight *RunPreflightReport `json:"preflight,omitempty"`
	Trace     *RunTriageTrace     `json:"trace,omitempty"`
	SavedAt   time.Time           `json:"saved_at,omitempty"`
}

func (a *AgentComponent) triageRun(ctx context.Context, msg IncomingMessage, session *Session, signal *SemanticSignal) (ExecutionMode, *RunPreflightReport, *RunTriageTrace) {
	req := submitRunTriageRequest(msg, session, a.config.DefaultModel, a.planner != nil, signal)
	if cached, ok := loadCachedRunTriage(session, req); ok {
		trace := cloneRunTriageTrace(cached.Trace)
		if trace == nil {
			trace = &RunTriageTrace{}
		}
		trace.Source = "session_cache"
		trace.Mode = cached.Mode
		trace.CacheHit = true
		if trace.GeneratedAt.IsZero() {
			trace.GeneratedAt = time.Now().UTC()
		}
		applyRunTriageTraceToSemanticSignal(signal, cached.Mode, cached.Preflight, trace)
		return cached.Mode, cloneRunPreflightReport(cached.Preflight), trace
	}
	mode := ExecutionMode("")
	var trace *RunTriageTrace
	var triageErr error
	if a != nil && a.runTriage != nil {
		if decision, err := a.runTriage.AnalyzeRun(ctx, req); err == nil {
			mode = normalizeExecutionModeDecision(ExecutionModeDecision{
				Mode:       ExecutionMode(strings.TrimSpace(decision.ExecutionMode)),
				Reason:     decision.Reason,
				Confidence: decision.Confidence,
			}, req.PlannerAvailable).Mode
			analysis := triageAnalysisFromDecision(decision)
			trace = buildRunTriageTrace("model", mode, req.Message, analysis)
			if decision.RequiresCurrentInfo {
				trace.RequiresCurrentInfo = true
			}
			applyRunTriageTraceToSemanticSignal(signal, mode, nil, trace)
		} else {
			triageErr = err
			triageLog.Warn("run triage analyzer failed; falling back to selector", "error", err)
		}
	}
	if mode == "" {
		mode = a.selectExecutionMode(ctx, msg, session)
		analysis := semanticSignalPreflightAnalysis(signal)
		if strings.TrimSpace(analysis.Reason) == "" {
			analysis.Reason = "selector_or_triage_unavailable"
		}
		trace = buildRunTriageTrace("fallback", mode, strings.TrimSpace(msg.Content), analysis)
		if triageErr != nil {
			trace.Error = triageErr.Error()
		}
		if signal != nil && signal.RequiresCurrentInfo {
			trace.RequiresCurrentInfo = true
		}
		applyRunTriageTraceToSemanticSignal(signal, mode, nil, trace)
	}
	report := a.buildRunPreflight(ctx, msg, session, mode, signal)
	return mode, report, trace
}

func triageAnalysisFromDecision(decision triage.RunDecision) PreflightAnalysis {
	return PreflightAnalysis{
		NeedsReference:           decision.NeedsReference,
		NeedsReferenceSet:        decision.NeedsReference,
		NeedsConfirmation:        decision.NeedsConfirmation,
		NeedsConfirmSet:          decision.NeedsConfirmation,
		SuggestedDomains:         decision.SuggestedDomains,
		DetectedDomains:          decision.SuggestedDomains,
		DomainsSpecified:         len(decision.SuggestedDomains) > 0,
		DetectedDomainsSpecified: len(decision.SuggestedDomains) > 0,
		Reason:                   decision.Reason,
		Confidence:               decision.Confidence,
	}
}

func buildRunTriageTrace(source string, mode ExecutionMode, message string, analysis PreflightAnalysis) *RunTriageTrace {
	suggestedDomains := normalizeSemanticDomains(analysis.SuggestedDomains)
	return &RunTriageTrace{
		Source:              strings.TrimSpace(source),
		Mode:                mode,
		NeedsReference:      analysis.NeedsReference,
		NeedsReferenceSet:   analysis.NeedsReferenceSet || analysis.NeedsReference,
		NeedsConfirmation:   analysis.NeedsConfirmation,
		NeedsConfirmSet:     analysis.NeedsConfirmSet || analysis.NeedsConfirmation,
		RequiresCurrentInfo: triageRequiresCurrentInfo(message, analysis),
		Reason:              analysis.Reason,
		Confidence:          analysis.Confidence,
		SuggestedDomains:    append([]string(nil), suggestedDomains...),
		GeneratedAt:         time.Now().UTC(),
	}
}

func triageRequiresCurrentInfo(message string, analysis PreflightAnalysis) bool {
	if DetectFast(message).RequiresCurrentInfo {
		return true
	}
	domains := append([]string(nil), analysis.DetectedDomains...)
	domains = append(domains, analysis.SuggestedDomains...)
	return semanticDomainsRequireCurrentInfo(domains)
}

func triageAnalysisFromPreflight(report *RunPreflightReport, reason string, confidence float64) PreflightAnalysis {
	return PreflightAnalysis{
		NeedsReference:    preflightHasCheck(report, "reference_gap"),
		NeedsReferenceSet: report != nil,
		NeedsConfirmation: preflightHasCheck(report, "expected_confirmation"),
		NeedsConfirmSet:   report != nil,
		SuggestedDomains:  cloneStrings(reportSuggestedDomains(report)),
		DetectedDomains:   cloneStrings(report.DetectedDomains),
		Reason:            strings.TrimSpace(reason),
		Confidence:        confidence,
	}
}

func reportSuggestedDomains(report *RunPreflightReport) []string {
	if report == nil {
		return nil
	}
	return report.SuggestedDomains
}

func rememberRunTriage(ctx context.Context, sessions SessionStore, sessionID string, req triage.RunRequest, mode ExecutionMode, report *RunPreflightReport, trace *RunTriageTrace) bool {
	if sessions == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	session, unlock, err := sessions.LoadForExecution(ctx, sessionID)
	if err != nil {
		return false
	}
	defer unlock()
	if session.Metadata == nil {
		session.Metadata = make(map[string]any, 1)
	}
	session.Metadata[sessionRunTriageCacheKey] = cachedRunTriage{
		Signature: runTriageSignature(req),
		Mode:      mode,
		Preflight: cloneRunPreflightReport(report),
		Trace:     cloneRunTriageTrace(trace),
		SavedAt:   time.Now().UTC(),
	}
	return sessions.Save(ctx, session) == nil
}

func loadCachedRunTriage(session *Session, req triage.RunRequest) (*cachedRunTriage, bool) {
	if session == nil || session.Metadata == nil {
		return nil, false
	}
	raw, ok := session.Metadata[sessionRunTriageCacheKey]
	if !ok || raw == nil {
		return nil, false
	}
	cached, ok := parseCachedRunTriage(raw)
	if !ok {
		return nil, false
	}
	if cached.Signature == "" || cached.Signature != runTriageSignature(req) {
		return nil, false
	}
	if cached.Mode == "" {
		return nil, false
	}
	if !cached.SavedAt.IsZero() && time.Since(cached.SavedAt) > runTriageCacheTTL {
		return nil, false
	}
	return cached, true
}

func parseCachedRunTriage(raw any) (*cachedRunTriage, bool) {
	switch typed := raw.(type) {
	case cachedRunTriage:
		out := typed
		out.Preflight = cloneRunPreflightReport(typed.Preflight)
		out.Trace = cloneRunTriageTrace(typed.Trace)
		return &out, true
	case *cachedRunTriage:
		if typed == nil {
			return nil, false
		}
		out := *typed
		out.Preflight = cloneRunPreflightReport(typed.Preflight)
		out.Trace = cloneRunTriageTrace(typed.Trace)
		return &out, true
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var decoded cachedRunTriage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, false
	}
	if decoded.Signature == "" || decoded.Mode == "" {
		return nil, false
	}
	decoded.Preflight = cloneRunPreflightReport(decoded.Preflight)
	decoded.Trace = cloneRunTriageTrace(decoded.Trace)
	return &decoded, true
}

func runTriageSignature(req triage.RunRequest) string {
	payload, err := json.Marshal(req)
	if err != nil {
		payload = []byte(strings.Join([]string{
			strings.TrimSpace(req.Model),
			strings.TrimSpace(req.Message),
			strings.TrimSpace(req.SessionSummary),
			strings.TrimSpace(req.LanguageHint),
			boolString(req.PlannerAvailable),
			boolString(req.MainSemanticPath),
			boolString(req.SemanticSignal != nil),
			boolString(req.SemanticSignal != nil && req.SemanticSignal.MainSemanticPath),
			boolString(req.SemanticSignal != nil && req.SemanticSignal.RequiresCurrentInfo),
			func() string {
				if req.SemanticSignal == nil {
					return ""
				}
				return strings.TrimSpace(req.SemanticSignal.LanguageHint)
			}(),
		}, "\n"))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
