package gateway

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	authscope "github.com/fulcrus/hopclaw/internal/authscope"
)

const (
	headerAuthScopeSubject      = authscope.HeaderSubject
	headerAuthScopeAutomationID = authscope.HeaderAutomationID
)

type authScope struct {
	Provider      string
	Subject       string
	AutomationIDs []string
	Scoped        bool
}

func authScopeFromIdentity(identity *AuthIdentity) authScope {
	if identity == nil {
		return authScope{}
	}
	automationIDs := authscope.NormalizeAutomationIDs(append(
		authscope.AutomationIDsFromClaims(identity.Scopes),
		authscope.AutomationIDsFromMetadata(identity.Metadata)...,
	))
	return authScope{
		Provider:      strings.TrimSpace(identity.Provider),
		Subject:       strings.TrimSpace(identity.Subject),
		AutomationIDs: automationIDs,
		Scoped:        len(automationIDs) > 0,
	}.normalize()
}

func (s authScope) applyHeaders(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	s = s.normalize()
	req := r.Clone(r.Context())
	req.Header = r.Header.Clone()
	req.Header.Del(headerAuthScopeSubject)
	req.Header.Del(headerAuthScopeAutomationID)
	if s.Subject != "" {
		req.Header.Set(headerAuthScopeSubject, s.Subject)
	}
	if len(s.AutomationIDs) > 0 {
		req.Header.Set(headerAuthScopeAutomationID, strings.Join(s.AutomationIDs, ","))
	}
	return req
}

func (s authScope) isZero() bool {
	return !s.normalize().Scoped
}

func (s authScope) constrain(_, _, _, _ string, _ bool) (authScope, error) {
	return s.normalize(), nil
}

func (s authScope) scopeFilter() agent.ScopeFilter {
	s = s.normalize()
	return agent.ScopeFilter{
		AutomationIDs: s.AutomationIDs,
		Deny:          s.Scoped && len(s.AutomationIDs) == 0,
	}.Normalize()
}

func requestScopeFilterWithAuthScope(scope authScope) (agent.ScopeFilter, error) {
	return scope.scopeFilter(), nil
}

func (s authScope) matches(_, _, automationID, _ string) bool {
	return s.allowsAutomationID(automationID)
}

func (s authScope) normalize() authScope {
	s.Provider = strings.TrimSpace(s.Provider)
	s.Subject = strings.TrimSpace(s.Subject)
	s.AutomationIDs = authscope.NormalizeAutomationIDs(s.AutomationIDs)
	if !s.Scoped && len(s.AutomationIDs) == 0 {
		s.AutomationIDs = nil
	}
	return s
}

func (s authScope) allowsAutomationID(automationID string) bool {
	s = s.normalize()
	if !s.Scoped {
		return true
	}
	if len(s.AutomationIDs) == 0 {
		return false
	}
	return authscope.ContainsAutomationID(s.AutomationIDs, automationID)
}

func (s authScope) resolveAutomationID(automationID string) (string, error) {
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
