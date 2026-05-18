package browser

import (
	"strings"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

func (c *Capability) prepareRoutedRequest(req captypes.InvokeRequest) (captypes.InvokeRequest, capprofile.ExecutionTrace) {
	params := supportmaps.Clone(req.Params)
	state := c.lookupState(req.SessionID)
	target := capprofile.TargetContext{
		Surface:        capprofile.SurfaceBrowser,
		Operation:      strings.TrimSpace(req.Operation),
		SemanticAction: inferBrowserSemanticAction(req.Operation, params),
		URL:            normalize.FirstNonEmpty(normalize.String(params["url"]), state.URL),
		Domain:         normalize.FirstNonEmpty(normalize.String(params["domain"]), state.Domain),
		Title:          normalize.FirstNonEmpty(normalize.String(params["title"]), state.Title),
	}
	trace := c.router.Plan(capprofile.RouteRequest{
		Target:            target,
		AllowedTransports: normalize.StringSlice(params["allowed_transports"]),
		TransportHint:     normalize.FirstNonEmpty(normalize.String(params["preferred_transport"]), normalize.String(params["transport"])),
	})
	if params == nil {
		params = make(map[string]any, 8)
	}
	applyRouteHints(params, target, trace)
	return captypes.InvokeRequest{
		Operation: req.Operation,
		SessionID: req.SessionID,
		Params:    params,
	}, trace
}

func (c *Capability) finalizeRouteTrace(trace capprofile.ExecutionTrace, chosenTransport string) capprofile.ExecutionTrace {
	if c.router == nil {
		return trace.Normalized()
	}
	return c.router.Finalize(trace, chosenTransport)
}

func (c *Capability) decorateInvokeResult(result *captypes.InvokeResult, trace capprofile.ExecutionTrace) {
	if result == nil {
		return
	}
	trace = trace.Normalized()
	if result.Metadata == nil {
		result.Metadata = make(map[string]any, 1)
	}
	result.Metadata[capprofile.MetadataKeyExecutionTrace] = trace.MetadataMap()
	if result.Data == nil {
		result.Data = make(map[string]any, 4)
	}
	if trace.ProfileID != "" {
		result.Data["profile_id"] = trace.ProfileID
	}
	if trace.ProfileFamily != "" {
		result.Data["profile_family"] = trace.ProfileFamily
	}
	if trace.SupportTier != "" {
		result.Data["support_tier"] = trace.SupportTier
	}
	if normalize.String(result.Data["transport"]) == "" && trace.ChosenTransport != "" {
		result.Data["transport"] = trace.ChosenTransport
	}
	if normalize.String(result.Data["strategy"]) == "" && trace.RouteSource != "" {
		result.Data["strategy"] = trace.RouteSource
	}
	result.Data["transport_telemetry"] = trace.MetadataMap()
}

func (c *Capability) rememberSessionState(sessionID string, req captypes.InvokeRequest, result *captypes.InvokeResult, trace capprofile.ExecutionTrace) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	state := c.lookupState(sessionID)
	state.Surface = capprofile.SurfaceBrowser
	state.URL = normalize.FirstNonEmpty(normalize.String(result.Data["url"]), normalize.String(req.Params["url"]), state.URL)
	state.Domain = normalize.FirstNonEmpty(normalize.String(result.Data["domain"]), state.Domain)
	state.Title = normalize.FirstNonEmpty(normalize.String(result.Data["title"]), state.Title)
	state.ProfileID = normalize.FirstNonEmpty(trace.ProfileID, state.ProfileID)
	state.ProfileFamily = normalize.FirstNonEmpty(trace.ProfileFamily, state.ProfileFamily)
	state.SupportTier = normalize.FirstNonEmpty(trace.SupportTier, state.SupportTier)
	state.LastSemanticAction = normalize.FirstNonEmpty(trace.SemanticAction, state.LastSemanticAction)
	state.LastTransport = normalize.FirstNonEmpty(trace.ChosenTransport, state.LastTransport)
	state.LastExecutionMode = normalize.FirstNonEmpty(trace.ExecutionMode, state.LastExecutionMode)
	c.storeState(sessionID, state)
}

func (c *Capability) lookupState(sessionID string) capprofile.SessionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.states == nil {
		return capprofile.SessionState{}
	}
	return c.states[strings.TrimSpace(sessionID)].Normalized()
}

func (c *Capability) storeState(sessionID string, state capprofile.SessionState) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.states == nil {
		c.states = make(map[string]capprofile.SessionState)
	}
	normalizedState := state.Normalized()
	c.states[sessionID] = normalizedState
	if handle, ok := c.sessions[sessionID]; ok && handle != nil {
		if handle.Metadata == nil {
			handle.Metadata = make(map[string]any, 8)
		}
		for key, value := range normalizedState.Metadata() {
			handle.Metadata[key] = value
		}
	}
}

func inferBrowserSemanticAction(operation string, params map[string]any) string {
	if explicit := strings.TrimSpace(normalize.String(params["semantic_action"])); explicit != "" {
		return explicit
	}
	switch strings.TrimSpace(operation) {
	case browsertypes.ActionNavigate:
		return "page.navigate"
	case browsertypes.ActionClick, browsertypes.ActionClickAria:
		return "page.click"
	case browsertypes.ActionType, browsertypes.ActionTypeAria, browsertypes.ActionFill:
		return "page.type"
	case browsertypes.ActionSelectOption:
		return "page.select"
	case browsertypes.ActionKeyboard:
		return "page.keyboard"
	case browsertypes.ActionSnapshot, browsertypes.ActionSnapshotAria, browsertypes.ActionScreenshot, browsertypes.ActionScreenshotLabeled:
		return "page.inspect"
	default:
		return strings.TrimSpace(operation)
	}
}

func inferBrowserTransport(operation string, data map[string]any) string {
	if transport := strings.TrimSpace(normalize.String(data["transport"])); transport != "" {
		return transport
	}
	switch strings.TrimSpace(operation) {
	case browsertypes.ActionNavigate:
		return capprofile.TransportBrowserNavigation
	case browsertypes.ActionClickAria, browsertypes.ActionTypeAria:
		return capprofile.TransportBrowserARIA
	case browsertypes.ActionKeyboard:
		return capprofile.TransportBrowserKeyboard
	case browsertypes.ActionEval:
		return capprofile.TransportBrowserScript
	case browsertypes.ActionSnapshot, browsertypes.ActionSnapshotAria, browsertypes.ActionScreenshot, browsertypes.ActionScreenshotLabeled:
		return capprofile.TransportBrowserSnapshot
	default:
		return capprofile.TransportBrowserDOM
	}
}

func applyRouteHints(params map[string]any, target capprofile.TargetContext, trace capprofile.ExecutionTrace) {
	if _, ok := params["target_scope"]; !ok {
		scope := map[string]any{
			"surface": "browser",
		}
		if target.URL != "" {
			scope["url"] = target.URL
		}
		if target.Domain != "" {
			scope["domain"] = target.Domain
		}
		params["target_scope"] = scope
	}
	if _, ok := params["preferred_transport"]; !ok && trace.PreferredTransport != "" {
		params["preferred_transport"] = trace.PreferredTransport
	}
	if _, ok := params["preferred_transports"]; !ok && len(trace.PreferredTransports) > 0 {
		params["preferred_transports"] = append([]string(nil), trace.PreferredTransports...)
	}
	if _, ok := params["allowed_transports"]; !ok && len(trace.AllowedTransports) > 0 {
		params["allowed_transports"] = append([]string(nil), trace.AllowedTransports...)
	}
	if _, ok := params["risk_level"]; !ok && trace.RiskLevel != "" {
		params["risk_level"] = trace.RiskLevel
	}
	if _, ok := params["required_ready_state"]; !ok && trace.RequiredReadyState != "" {
		params["required_ready_state"] = trace.RequiredReadyState
	}
	if _, ok := params["verification_policy"]; !ok && trace.VerificationPolicy != "" {
		params["verification_policy"] = trace.VerificationPolicy
	}
	if _, ok := params["recovery_budget"]; !ok && trace.RecoveryBudget > 0 {
		params["recovery_budget"] = trace.RecoveryBudget
	}
	if trace.ProfileID != "" {
		params["route_profile_id"] = trace.ProfileID
		params["route_profile_family"] = trace.ProfileFamily
		params["route_support_tier"] = trace.SupportTier
	}
}
