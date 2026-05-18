package profile

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type Surface string

const (
	SurfaceBrowser Surface = "browser"
	SurfaceDesktop Surface = "desktop"
)

const (
	MetadataKeyExecutionTrace = "hopclaw_execution_trace"
)

const (
	TransportNativeCommand       = "native_command"
	TransportEmbeddedModelBridge = "embedded_model_bridge"
	TransportMenuInvoke          = "menu_invoke"
	TransportSemanticUIAction    = "semantic_ui_action"
	TransportVerifiedHotkey      = "verified_hotkey"
	TransportStructuredText      = "structured_text_or_clipboard"
	TransportOCRAnchoredVisual   = "ocr_anchored_visual"
	TransportVisionOrCoordinate  = "vision_or_coordinate"
	TransportBrowserNavigation   = "browser_navigation"
	TransportBrowserARIA         = "browser_aria"
	TransportBrowserDOM          = "browser_dom"
	TransportBrowserScript       = "browser_script"
	TransportBrowserKeyboard     = "browser_keyboard"
	TransportBrowserSnapshot     = "browser_snapshot"
	TransportBrowserVisual       = "browser_visual"
)

const (
	ModeDeterministic  = "deterministic_mode"
	ModeMixed          = "mixed_mode"
	ModeVisualFallback = "visual_fallback_mode"
	ModeViewless       = "viewless_command_mode"
)

type Match struct {
	AppNames    []string
	BundleIDs   []string
	DriverIDs   []string
	Domains     []string
	URLContains []string
	Titles      []string
}

func (m Match) normalized() Match {
	return Match{
		AppNames:    normalizeList(m.AppNames),
		BundleIDs:   normalizeList(m.BundleIDs),
		DriverIDs:   normalizeList(m.DriverIDs),
		Domains:     normalizeList(m.Domains),
		URLContains: normalizeList(m.URLContains),
		Titles:      normalizeList(m.Titles),
	}
}

type Action struct {
	ID                  string
	Aliases             []string
	RiskLevel           string
	RequiredReadyState  string
	PreferredTransports []string
	AllowedTransports   []string
	VerificationPolicy  string
	RecoveryBudget      int
	SceneHints          []string
}

func (a Action) normalized() Action {
	a.ID = strings.TrimSpace(a.ID)
	a.Aliases = normalizeList(a.Aliases)
	a.RiskLevel = strings.TrimSpace(strings.ToLower(a.RiskLevel))
	if a.RiskLevel == "" {
		a.RiskLevel = "medium"
	}
	a.RequiredReadyState = strings.TrimSpace(a.RequiredReadyState)
	a.PreferredTransports = normalizeList(a.PreferredTransports)
	a.AllowedTransports = normalizeList(a.AllowedTransports)
	a.VerificationPolicy = strings.TrimSpace(a.VerificationPolicy)
	a.SceneHints = normalizeList(a.SceneHints)
	if a.RecoveryBudget < 0 {
		a.RecoveryBudget = 0
	}
	return a
}

type Profile struct {
	ID                  string
	Surface             Surface
	Family              string
	SupportTier         string
	ViewModel           string
	Match               Match
	PreferredTransports []string
	Actions             []Action
}

func (p Profile) Normalized() Profile {
	p.ID = strings.TrimSpace(p.ID)
	p.Family = strings.TrimSpace(p.Family)
	p.SupportTier = strings.TrimSpace(strings.ToLower(p.SupportTier))
	p.ViewModel = strings.TrimSpace(strings.ToLower(p.ViewModel))
	p.Match = p.Match.normalized()
	p.PreferredTransports = normalizeList(p.PreferredTransports)
	if len(p.Actions) > 0 {
		actions := make([]Action, 0, len(p.Actions))
		for _, action := range p.Actions {
			normalizedAction := action.normalized()
			if normalizedAction.ID == "" {
				continue
			}
			actions = append(actions, normalizedAction)
		}
		p.Actions = actions
	}
	return p
}

func (p Profile) LookupAction(actionID string) (Action, bool) {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return Action{}, false
	}
	for _, action := range p.Actions {
		if action.ID == actionID {
			return action, true
		}
		for _, alias := range action.Aliases {
			if alias == actionID {
				return action, true
			}
		}
	}
	return Action{}, false
}

type TargetContext struct {
	Surface        Surface
	Operation      string
	SemanticAction string
	AppName        string
	BundleID       string
	DriverID       string
	URL            string
	Domain         string
	Title          string
}

func (c TargetContext) Normalized() TargetContext {
	c.Operation = strings.TrimSpace(c.Operation)
	c.SemanticAction = strings.TrimSpace(c.SemanticAction)
	c.AppName = strings.TrimSpace(c.AppName)
	c.BundleID = strings.TrimSpace(c.BundleID)
	c.DriverID = strings.TrimSpace(c.DriverID)
	c.URL = strings.TrimSpace(c.URL)
	c.Domain = normalizeDomain(c.Domain)
	if c.Domain == "" {
		c.Domain = domainFromURL(c.URL)
	}
	c.Title = strings.TrimSpace(c.Title)
	return c
}

type Resolution struct {
	Profile     Profile
	Action      Action
	ProfileHit  bool
	ActionHit   bool
	Score       int
	MatchReason string
}

type Registry struct {
	profiles []Profile
}

func NewRegistry(profiles []Profile) *Registry {
	items := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		normalizedProfile := profile.Normalized()
		if normalizedProfile.ID == "" || normalizedProfile.Surface == "" {
			continue
		}
		items = append(items, normalizedProfile)
	}
	return &Registry{profiles: items}
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(defaultProfiles())
}

func (r *Registry) Profiles(surface Surface) []Profile {
	if r == nil {
		return nil
	}
	out := make([]Profile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		if surface != "" && profile.Surface != surface {
			continue
		}
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) Resolve(target TargetContext) Resolution {
	target = target.Normalized()
	if r == nil || target.Surface == "" {
		return Resolution{}
	}
	best := Resolution{}
	bestScore := 0
	for _, profile := range r.profiles {
		if profile.Surface != target.Surface {
			continue
		}
		score, reason := matchProfile(profile, target)
		if score <= 0 {
			continue
		}
		resolution := Resolution{
			Profile:     profile,
			ProfileHit:  true,
			Score:       score,
			MatchReason: reason,
		}
		if action, ok := profile.LookupAction(target.SemanticAction); ok {
			resolution.Action = action
			resolution.ActionHit = true
		}
		if score > bestScore {
			best = resolution
			bestScore = score
		}
	}
	return best
}

type RouteRequest struct {
	Target            TargetContext
	Mode              string
	AllowedTransports []string
	TransportHint     string
}

type ScoredTransport struct {
	Name  string `json:"name,omitempty"`
	Score int    `json:"score,omitempty"`
}

type ExecutionTrace struct {
	Surface             string            `json:"surface,omitempty"`
	Capability          string            `json:"capability,omitempty"`
	Operation           string            `json:"operation,omitempty"`
	ToolName            string            `json:"tool_name,omitempty"`
	ToolCallID          string            `json:"tool_call_id,omitempty"`
	SemanticAction      string            `json:"semantic_action,omitempty"`
	ProfileID           string            `json:"profile_id,omitempty"`
	ProfileFamily       string            `json:"profile_family,omitempty"`
	SupportTier         string            `json:"support_tier,omitempty"`
	ViewModel           string            `json:"view_model,omitempty"`
	ProfileHit          bool              `json:"profile_hit,omitempty"`
	ActionHit           bool              `json:"action_hit,omitempty"`
	MatchReason         string            `json:"match_reason,omitempty"`
	RiskLevel           string            `json:"risk_level,omitempty"`
	RequiredReadyState  string            `json:"required_ready_state,omitempty"`
	VerificationPolicy  string            `json:"verification_policy,omitempty"`
	RecoveryBudget      int               `json:"recovery_budget,omitempty"`
	RouteSource         string            `json:"route_source,omitempty"`
	Strategy            string            `json:"strategy,omitempty"`
	PreferredTransport  string            `json:"preferred_transport,omitempty"`
	PreferredTransports []string          `json:"preferred_transports,omitempty"`
	AllowedTransports   []string          `json:"allowed_transports,omitempty"`
	ChosenTransport     string            `json:"chosen_transport,omitempty"`
	FallbackPath        []string          `json:"fallback_path,omitempty"`
	TransportScores     []ScoredTransport `json:"transport_scores,omitempty"`
	Confidence          float64           `json:"confidence,omitempty"`
	ExecutionMode       string            `json:"execution_mode,omitempty"`
}

func (t ExecutionTrace) Normalized() ExecutionTrace {
	t.Surface = strings.TrimSpace(t.Surface)
	t.Capability = strings.TrimSpace(t.Capability)
	t.Operation = strings.TrimSpace(t.Operation)
	t.ToolName = strings.TrimSpace(t.ToolName)
	t.ToolCallID = strings.TrimSpace(t.ToolCallID)
	t.SemanticAction = strings.TrimSpace(t.SemanticAction)
	t.ProfileID = strings.TrimSpace(t.ProfileID)
	t.ProfileFamily = strings.TrimSpace(t.ProfileFamily)
	t.SupportTier = strings.TrimSpace(t.SupportTier)
	t.ViewModel = strings.TrimSpace(t.ViewModel)
	t.MatchReason = strings.TrimSpace(t.MatchReason)
	t.RiskLevel = strings.TrimSpace(t.RiskLevel)
	t.RequiredReadyState = strings.TrimSpace(t.RequiredReadyState)
	t.VerificationPolicy = strings.TrimSpace(t.VerificationPolicy)
	t.RouteSource = strings.TrimSpace(t.RouteSource)
	t.Strategy = strings.TrimSpace(t.Strategy)
	t.PreferredTransport = strings.TrimSpace(t.PreferredTransport)
	t.PreferredTransports = normalizeList(t.PreferredTransports)
	t.AllowedTransports = normalizeList(t.AllowedTransports)
	t.ChosenTransport = strings.TrimSpace(t.ChosenTransport)
	t.FallbackPath = normalizeList(t.FallbackPath)
	if len(t.TransportScores) > 0 {
		scores := make([]ScoredTransport, 0, len(t.TransportScores))
		for _, item := range t.TransportScores {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			scores = append(scores, ScoredTransport{Name: name, Score: item.Score})
		}
		t.TransportScores = scores
	}
	t.ExecutionMode = strings.TrimSpace(t.ExecutionMode)
	if t.Confidence < 0 {
		t.Confidence = 0
	}
	if t.Confidence > 1 {
		t.Confidence = 1
	}
	return t
}

func (t ExecutionTrace) MetadataMap() map[string]any {
	normalizedTrace := t.Normalized()
	body, err := json.Marshal(normalizedTrace)
	if err != nil {
		return map[string]any{
			"surface":          normalizedTrace.Surface,
			"capability":       normalizedTrace.Capability,
			"operation":        normalizedTrace.Operation,
			"semantic_action":  normalizedTrace.SemanticAction,
			"profile_id":       normalizedTrace.ProfileID,
			"chosen_transport": normalizedTrace.ChosenTransport,
		}
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{
			"surface":          normalizedTrace.Surface,
			"capability":       normalizedTrace.Capability,
			"operation":        normalizedTrace.Operation,
			"semantic_action":  normalizedTrace.SemanticAction,
			"profile_id":       normalizedTrace.ProfileID,
			"chosen_transport": normalizedTrace.ChosenTransport,
		}
	}
	return out
}

func DecodeExecutionTrace(metadata map[string]any) (ExecutionTrace, bool) {
	if len(metadata) == 0 {
		return ExecutionTrace{}, false
	}
	raw, ok := metadata[MetadataKeyExecutionTrace]
	if !ok || raw == nil {
		return ExecutionTrace{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return ExecutionTrace{}, false
	}
	var trace ExecutionTrace
	if err := json.Unmarshal(body, &trace); err != nil {
		return ExecutionTrace{}, false
	}
	normalizedTrace := trace.Normalized()
	if normalizedTrace.Capability == "" && normalizedTrace.Operation == "" && normalizedTrace.ChosenTransport == "" && normalizedTrace.ProfileID == "" {
		return ExecutionTrace{}, false
	}
	return normalizedTrace, true
}

type SessionState struct {
	Surface            Surface
	AppName            string
	BundleID           string
	DriverID           string
	URL                string
	Domain             string
	Title              string
	ProfileID          string
	ProfileFamily      string
	SupportTier        string
	LastSemanticAction string
	LastTransport      string
	LastExecutionMode  string
}

func (s SessionState) Normalized() SessionState {
	s.AppName = strings.TrimSpace(s.AppName)
	s.BundleID = strings.TrimSpace(s.BundleID)
	s.DriverID = strings.TrimSpace(s.DriverID)
	s.URL = strings.TrimSpace(s.URL)
	s.Domain = normalizeDomain(s.Domain)
	if s.Domain == "" {
		s.Domain = domainFromURL(s.URL)
	}
	s.Title = strings.TrimSpace(s.Title)
	s.ProfileID = strings.TrimSpace(s.ProfileID)
	s.ProfileFamily = strings.TrimSpace(s.ProfileFamily)
	s.SupportTier = strings.TrimSpace(s.SupportTier)
	s.LastSemanticAction = strings.TrimSpace(s.LastSemanticAction)
	s.LastTransport = strings.TrimSpace(s.LastTransport)
	s.LastExecutionMode = strings.TrimSpace(s.LastExecutionMode)
	return s
}

func (s SessionState) Metadata() map[string]any {
	normalizedState := s.Normalized()
	out := map[string]any{
		"surface": normalizedState.Surface,
	}
	if normalizedState.AppName != "" {
		out["app"] = normalizedState.AppName
	}
	if normalizedState.BundleID != "" {
		out["bundle_id"] = normalizedState.BundleID
	}
	if normalizedState.DriverID != "" {
		out["driver_id"] = normalizedState.DriverID
	}
	if normalizedState.URL != "" {
		out["url"] = normalizedState.URL
	}
	if normalizedState.Domain != "" {
		out["domain"] = normalizedState.Domain
	}
	if normalizedState.Title != "" {
		out["title"] = normalizedState.Title
	}
	if normalizedState.ProfileID != "" {
		out["profile_id"] = normalizedState.ProfileID
	}
	if normalizedState.ProfileFamily != "" {
		out["profile_family"] = normalizedState.ProfileFamily
	}
	if normalizedState.SupportTier != "" {
		out["support_tier"] = normalizedState.SupportTier
	}
	if normalizedState.LastSemanticAction != "" {
		out["last_semantic_action"] = normalizedState.LastSemanticAction
	}
	if normalizedState.LastTransport != "" {
		out["last_transport"] = normalizedState.LastTransport
	}
	if normalizedState.LastExecutionMode != "" {
		out["last_execution_mode"] = normalizedState.LastExecutionMode
	}
	return out
}

type Router struct {
	registry *Registry
}

func NewRouter(registry *Registry) *Router {
	if registry == nil {
		registry = NewDefaultRegistry()
	}
	return &Router{registry: registry}
}

func (r *Router) Plan(req RouteRequest) ExecutionTrace {
	target := req.Target.Normalized()
	resolution := Resolution{}
	if r != nil && r.registry != nil {
		resolution = r.registry.Resolve(target)
	}
	trace := ExecutionTrace{
		Surface:        string(target.Surface),
		Capability:     string(target.Surface),
		Operation:      target.Operation,
		SemanticAction: normalize.FirstNonEmpty(target.SemanticAction, target.Operation),
		ProfileHit:     resolution.ProfileHit,
		ActionHit:      resolution.ActionHit,
		MatchReason:    resolution.MatchReason,
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	if mode == "" {
		mode = "balanced"
	}
	trace.Strategy = mode

	preferred := defaultTransportsForTarget(target)
	trace.RouteSource = "kernel_default"
	if resolution.ProfileHit {
		trace.ProfileID = resolution.Profile.ID
		trace.ProfileFamily = resolution.Profile.Family
		trace.SupportTier = resolution.Profile.SupportTier
		trace.ViewModel = resolution.Profile.ViewModel
		if len(resolution.Profile.PreferredTransports) > 0 {
			preferred = append([]string(nil), resolution.Profile.PreferredTransports...)
			trace.RouteSource = "profile_default"
		}
	}

	action := Action{}
	if resolution.ActionHit {
		action = resolution.Action
		if len(action.PreferredTransports) > 0 {
			preferred = append([]string(nil), action.PreferredTransports...)
			trace.RouteSource = "profile_action"
		}
		trace.RiskLevel = action.RiskLevel
		trace.RequiredReadyState = action.RequiredReadyState
		trace.VerificationPolicy = action.VerificationPolicy
		trace.RecoveryBudget = action.RecoveryBudget
	}
	if trace.RiskLevel == "" {
		trace.RiskLevel = inferRisk(trace.SemanticAction, trace.Operation)
	}
	if trace.RequiredReadyState == "" {
		trace.RequiredReadyState = defaultReadyState(trace.Surface, trace.SemanticAction)
	}
	if trace.VerificationPolicy == "" {
		trace.VerificationPolicy = defaultVerificationPolicy(trace.Surface, trace.SemanticAction, trace.Operation)
	}

	allowed := normalizeList(req.AllowedTransports)
	if len(allowed) == 0 && len(action.AllowedTransports) > 0 {
		allowed = append([]string(nil), action.AllowedTransports...)
	}
	if len(allowed) == 0 {
		allowed = append([]string(nil), preferred...)
	}

	scores := scoreTransports(target, preferred, allowed, req.TransportHint, trace.RiskLevel, trace.ViewModel)
	trace.TransportScores = scores
	if len(scores) > 0 {
		trace.PreferredTransport = scores[0].Name
	}
	trace.PreferredTransports = transportNames(scores)
	trace.AllowedTransports = normalizeList(allowed)
	return trace.Normalized()
}

func (r *Router) Finalize(trace ExecutionTrace, chosenTransport string) ExecutionTrace {
	trace = trace.Normalized()
	chosenTransport = strings.TrimSpace(chosenTransport)
	if chosenTransport == "" {
		chosenTransport = trace.PreferredTransport
	}
	trace.ChosenTransport = chosenTransport

	index := -1
	for i, item := range trace.TransportScores {
		if item.Name == chosenTransport {
			index = i
			break
		}
	}
	if index > 0 {
		trace.FallbackPath = transportNames(trace.TransportScores[:index])
	}
	trace.ExecutionMode = classifyExecutionMode(trace.ViewModel, chosenTransport, len(trace.FallbackPath) > 0)
	trace.Confidence = confidenceForTrace(trace, index)
	return trace.Normalized()
}

func matchProfile(profile Profile, target TargetContext) (int, string) {
	score := 0
	reasons := make([]string, 0, 4)
	if target.BundleID != "" {
		for _, bundleID := range profile.Match.BundleIDs {
			if strings.EqualFold(bundleID, target.BundleID) {
				score += 100
				reasons = append(reasons, "bundle_id")
				break
			}
		}
	}
	if target.DriverID != "" {
		for _, driverID := range profile.Match.DriverIDs {
			if strings.EqualFold(driverID, target.DriverID) {
				score += 95
				reasons = append(reasons, "driver_id")
				break
			}
		}
	}
	if target.AppName != "" {
		appName := strings.ToLower(target.AppName)
		for _, candidate := range profile.Match.AppNames {
			switch {
			case strings.EqualFold(candidate, target.AppName):
				score += 90
				reasons = append(reasons, "app_name")
			case strings.Contains(appName, strings.ToLower(candidate)) || strings.Contains(strings.ToLower(candidate), appName):
				score += 60
				reasons = append(reasons, "app_name_partial")
			}
		}
	}
	if target.Domain != "" {
		domain := normalizeDomain(target.Domain)
		for _, candidate := range profile.Match.Domains {
			if domain == candidate || strings.HasSuffix(domain, "."+candidate) {
				score += 110
				reasons = append(reasons, "domain")
				break
			}
		}
	}
	if target.URL != "" {
		lowerURL := strings.ToLower(target.URL)
		for _, candidate := range profile.Match.URLContains {
			if strings.Contains(lowerURL, strings.ToLower(candidate)) {
				score += 70
				reasons = append(reasons, "url")
				break
			}
		}
	}
	if target.Title != "" {
		lowerTitle := strings.ToLower(target.Title)
		for _, candidate := range profile.Match.Titles {
			if strings.Contains(lowerTitle, strings.ToLower(candidate)) {
				score += 30
				reasons = append(reasons, "title")
				break
			}
		}
	}
	return score, strings.Join(normalize.DedupeStrings(reasons), "+")
}

func defaultTransportsForTarget(target TargetContext) []string {
	if target.Surface == SurfaceDesktop {
		switch target.Operation {
		case "invoke_driver_action":
			return []string{
				TransportNativeCommand,
				TransportEmbeddedModelBridge,
				TransportMenuInvoke,
				TransportSemanticUIAction,
				TransportVerifiedHotkey,
				TransportStructuredText,
				TransportOCRAnchoredVisual,
				TransportVisionOrCoordinate,
			}
		case "invoke_command":
			return []string{
				TransportMenuInvoke,
				TransportNativeCommand,
				TransportVerifiedHotkey,
			}
		case "find_element", "click_element", "set_element_value", "clear_element", "get_element_value", "assert_element":
			return []string{
				TransportSemanticUIAction,
				TransportVerifiedHotkey,
				TransportOCRAnchoredVisual,
			}
		case "find_text", "click_text", "screenshot":
			return []string{
				TransportOCRAnchoredVisual,
				TransportVisionOrCoordinate,
			}
		case "type_text":
			return []string{
				TransportStructuredText,
				TransportVerifiedHotkey,
				TransportOCRAnchoredVisual,
			}
		case "hotkey":
			return []string{
				TransportVerifiedHotkey,
				TransportStructuredText,
				TransportOCRAnchoredVisual,
			}
		default:
			return []string{
				TransportNativeCommand,
				TransportMenuInvoke,
				TransportSemanticUIAction,
				TransportVerifiedHotkey,
				TransportStructuredText,
				TransportOCRAnchoredVisual,
				TransportVisionOrCoordinate,
			}
		}
	}

	switch target.Operation {
	case "navigate":
		return []string{TransportBrowserNavigation}
	case "snapshot", "snapshot_aria", "screenshot", "screenshot_labeled":
		return []string{TransportBrowserSnapshot}
	case "click_aria", "type_aria":
		return []string{
			TransportBrowserARIA,
			TransportBrowserDOM,
			TransportBrowserKeyboard,
			TransportBrowserVisual,
		}
	case "keyboard":
		return []string{
			TransportBrowserKeyboard,
			TransportBrowserARIA,
			TransportBrowserDOM,
			TransportBrowserVisual,
		}
	case "eval":
		return []string{
			TransportBrowserScript,
			TransportBrowserDOM,
			TransportBrowserARIA,
		}
	default:
		return []string{
			TransportBrowserDOM,
			TransportBrowserARIA,
			TransportBrowserKeyboard,
			TransportBrowserVisual,
		}
	}
}

func scoreTransports(target TargetContext, preferred []string, allowed []string, hint string, riskLevel string, viewModel string) []ScoredTransport {
	preferred = normalizeList(preferred)
	allowed = normalizeList(allowed)
	if len(preferred) == 0 {
		preferred = defaultTransportsForTarget(target)
	}
	filter := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		filter[item] = struct{}{}
	}
	scores := make([]ScoredTransport, 0, len(preferred))
	for idx, transport := range preferred {
		if len(filter) > 0 {
			if _, ok := filter[transport]; !ok {
				continue
			}
		}
		score := 1000 - idx*100
		if strings.TrimSpace(hint) == transport {
			score += 35
		}
		switch riskLevel {
		case "high":
			if isWeakTransport(transport) {
				score -= 320
			}
		case "low":
			if isWeakTransport(transport) {
				score -= 40
			}
		}
		if isMediaBrowseAction(target.SemanticAction) && isVisualTransport(transport) {
			score += 110
		}
		if strings.Contains(viewModel, "viewless") && isVisualTransport(transport) {
			score -= 700
		}
		scores = append(scores, ScoredTransport{Name: transport, Score: score})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Score == scores[j].Score {
			return scores[i].Name < scores[j].Name
		}
		return scores[i].Score > scores[j].Score
	})
	return scores
}

func confidenceForTrace(trace ExecutionTrace, chosenIndex int) float64 {
	confidence := 0.72
	if trace.ProfileHit {
		confidence += 0.12
	}
	if trace.ActionHit {
		confidence += 0.06
	}
	if chosenIndex > 0 {
		confidence -= float64(chosenIndex) * 0.08
	}
	if isVisualTransport(trace.ChosenTransport) {
		confidence -= 0.18
	}
	if trace.ChosenTransport == "" {
		confidence -= 0.25
	}
	if confidence < 0.15 {
		confidence = 0.15
	}
	if confidence > 0.99 {
		confidence = 0.99
	}
	return confidence
}

func classifyExecutionMode(viewModel string, chosenTransport string, usedFallback bool) string {
	switch {
	case strings.Contains(viewModel, "viewless") && !isVisualTransport(chosenTransport):
		return ModeViewless
	case isVisualTransport(chosenTransport):
		return ModeVisualFallback
	case usedFallback || chosenTransport == TransportVerifiedHotkey || chosenTransport == TransportStructuredText || chosenTransport == TransportBrowserKeyboard:
		return ModeMixed
	default:
		return ModeDeterministic
	}
}

func defaultReadyState(surface string, semanticAction string) string {
	switch {
	case surface == string(SurfaceDesktop) && strings.Contains(semanticAction, "play"):
		return "interactive"
	case surface == string(SurfaceDesktop):
		return "scene_ready"
	default:
		return "page_ready"
	}
}

func defaultVerificationPolicy(surface string, semanticAction string, operation string) string {
	switch {
	case strings.Contains(semanticAction, "search"):
		return "scene_transition"
	case strings.Contains(semanticAction, "play"):
		return "state_change"
	case operation == "snapshot" || operation == "screenshot":
		return "artifact_available"
	case surface == string(SurfaceBrowser):
		return "dom_or_url"
	default:
		return "state_change"
	}
}

func inferRisk(semanticAction string, operation string) string {
	lower := strings.ToLower(strings.TrimSpace(semanticAction + " " + operation))
	switch {
	case strings.Contains(lower, "delete"),
		strings.Contains(lower, "remove"),
		strings.Contains(lower, "publish"),
		strings.Contains(lower, "export"),
		strings.Contains(lower, "save"):
		return "high"
	case strings.Contains(lower, "open"),
		strings.Contains(lower, "project"),
		strings.Contains(lower, "submit"),
		strings.Contains(lower, "play_toggle"),
		strings.Contains(lower, "pause"),
		strings.Contains(lower, "play"):
		return "medium"
	default:
		return "low"
	}
}

func isMediaBrowseAction(action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	return strings.Contains(action, "next_item") || strings.Contains(action, "open_first") || strings.Contains(action, "media.")
}

func isVisualTransport(transport string) bool {
	switch transport {
	case TransportOCRAnchoredVisual, TransportVisionOrCoordinate, TransportBrowserVisual:
		return true
	default:
		return false
	}
}

func isWeakTransport(transport string) bool {
	switch transport {
	case TransportVerifiedHotkey, TransportStructuredText, TransportOCRAnchoredVisual, TransportVisionOrCoordinate, TransportBrowserKeyboard, TransportBrowserVisual:
		return true
	default:
		return false
	}
}

func transportNames(scores []ScoredTransport) []string {
	if len(scores) == 0 {
		return nil
	}
	out := make([]string, 0, len(scores))
	for _, item := range scores {
		if name := strings.TrimSpace(item.Name); name != "" {
			out = append(out, name)
		}
	}
	return normalize.DedupeStrings(out)
}

func normalizeList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, strings.ToLower(trimmed))
	}
	return normalize.DedupeStrings(out)
}

func normalizeDomain(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "www.")
	raw = strings.TrimSuffix(raw, "/")
	return raw
}

func domainFromURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeDomain(parsed.Hostname())
}
