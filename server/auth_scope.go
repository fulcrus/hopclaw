package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	authscope "github.com/fulcrus/hopclaw/internal/authscope"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type requestAuthScope struct {
	Subject       string
	AutomationIDs []string
	Scoped        bool
}

func requestAuthScopeFromHeaders(r *http.Request) requestAuthScope {
	if r == nil {
		return requestAuthScope{}
	}
	automationIDs := authscope.AutomationIDsFromHeaderValues(r.Header.Values(authscope.HeaderAutomationID))
	return requestAuthScope{
		Subject:       strings.TrimSpace(r.Header.Get(authscope.HeaderSubject)),
		AutomationIDs: automationIDs,
		Scoped:        len(automationIDs) > 0,
	}.normalize()
}

func requestScopeFilter(r *http.Request) (agent.ScopeFilter, error) {
	return requestAuthScopeFromHeaders(r).scopeFilter(), nil
}

func requestScopeFilterWithAuthScope(scope requestAuthScope, _, _ string) (agent.ScopeFilter, error) {
	return scope.scopeFilter(), nil
}

func applySubmitAuthScope(r *http.Request, req *runtimesvc.SubmitRequest) error {
	if req == nil {
		return nil
	}
	return applyAutomationIDAuthScope(r, &req.AutomationID)
}

func applyAutomationIDAuthScope(r *http.Request, automationID *string) error {
	if automationID == nil {
		return nil
	}
	resolved, err := requestAuthScopeFromHeaders(r).resolveAutomationID(*automationID)
	if err != nil {
		return err
	}
	*automationID = resolved
	return nil
}

func applyInteractionAuthScope(r *http.Request, req *runtimesvc.InteractionRequest) error {
	if req == nil {
		return nil
	}
	return applyAutomationIDAuthScope(r, &req.AutomationID)
}

func (s requestAuthScope) constrain(_, _, _, _ string, _ bool) (requestAuthScope, error) {
	return s.normalize(), nil
}

func requestAuthScopeFromClaims(claims []string) requestAuthScope {
	automationIDs := authscope.AutomationIDsFromClaims(claims)
	return requestAuthScope{
		AutomationIDs: automationIDs,
		Scoped:        len(automationIDs) > 0,
	}.normalize()
}

func mergeRequestAuthScope(base requestAuthScope, claims []string) requestAuthScope {
	base = base.normalize()
	claimed := requestAuthScopeFromClaims(claims).normalize()
	switch {
	case !base.Scoped:
		return claimed
	case !claimed.Scoped:
		return base
	default:
		return requestAuthScope{
			Subject:       base.Subject,
			AutomationIDs: authscope.IntersectAutomationIDs(base.AutomationIDs, claimed.AutomationIDs),
			Scoped:        true,
		}.normalize()
	}
}

func (s requestAuthScope) normalize() requestAuthScope {
	s.Subject = strings.TrimSpace(s.Subject)
	s.AutomationIDs = authscope.NormalizeAutomationIDs(s.AutomationIDs)
	if !s.Scoped && len(s.AutomationIDs) == 0 {
		s.AutomationIDs = nil
	}
	return s
}

func (s requestAuthScope) scopeFilter() agent.ScopeFilter {
	s = s.normalize()
	return agent.ScopeFilter{
		AutomationIDs: s.AutomationIDs,
		Deny:          s.Scoped && len(s.AutomationIDs) == 0,
	}.Normalize()
}

func (s requestAuthScope) resolveAutomationID(automationID string) (string, error) {
	s = s.normalize()
	automationID = strings.TrimSpace(automationID)
	if !s.Scoped {
		return automationID, nil
	}
	if len(s.AutomationIDs) == 0 {
		return "", fmt.Errorf("auth scope does not allow any automation")
	}
	if automationID != "" {
		if !authscope.ContainsAutomationID(s.AutomationIDs, automationID) {
			return "", fmt.Errorf("automation_id %q is outside the authenticated scope", automationID)
		}
		return automationID, nil
	}
	if len(s.AutomationIDs) == 1 {
		return s.AutomationIDs[0], nil
	}
	return "", fmt.Errorf("automation_id is required when the authenticated scope contains multiple automations")
}
