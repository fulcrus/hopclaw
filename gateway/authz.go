package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/authz"
)

type accessRequirement struct {
	resource authz.Resource
	action   authz.Action
}

func (g *Gateway) authenticatedHandler(next http.Handler, requireAuthorization bool) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := g.authenticateRequest(r)
		if err != nil {
			if g.authInitErr != nil {
				gwError(w, http.StatusServiceUnavailable, g.authInitErr.Error())
				return
			}
			writeAuthError(r.Context(), w, err.Error())
			return
		}

		if identity == nil {
			if g.authConfigured() {
				writeAuthError(r.Context(), w, "missing or invalid auth credentials")
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		ctx := contextWithAuthIdentity(r.Context(), identity)
		reqWithIdentity := authScopeFromIdentity(identity).applyHeaders(r.WithContext(ctx))
		if requireAuthorization {
			if err := g.authorizeRequest(reqWithIdentity, identity); err != nil {
				writeAuthorizationError(w, err.Error())
				return
			}
		}
		next.ServeHTTP(w, reqWithIdentity)
	})

	if g.authSessionStore != nil {
		return AuthSessionCSRFMiddleware(g.authSessionStore, authSessionCookieName(g.authSessionConfig.CookieName))(handler)
	}
	return handler
}

func (g *Gateway) authenticateRequest(r *http.Request) (*AuthIdentity, error) {
	if g.authInitErr != nil {
		return nil, g.authInitErr
	}
	if g.authChain == nil {
		return nil, nil
	}
	return g.authChain.Authenticate(r)
}

func (g *Gateway) authConfigured() bool {
	return g.authChain != nil && len(g.authChain.providers) > 0
}

func (g *Gateway) authorizeRequest(r *http.Request, identity *AuthIdentity) error {
	if identity == nil {
		return fmt.Errorf("authentication required")
	}

	req := accessRequirementForRequest(r)
	decider := g.authzDecider
	if decider == nil {
		decider = authz.OpenDecider{}
	}
	decision, err := decider.Decide(r.Context(), authz.AuthorizationRequest{
		Resource:  req.resource,
		Action:    req.action,
		Method:    r.Method,
		Path:      r.URL.Path,
		Principal: principalFromIdentity(identity),
	})
	if err != nil {
		return fmt.Errorf("authorization check failed: %w", err)
	}
	if !decision.Allowed {
		if reason := strings.TrimSpace(decision.Reason); reason != "" {
			return errors.New(reason)
		}
		return fmt.Errorf("insufficient permissions")
	}
	return nil
}

func accessRequirementForRequest(r *http.Request) accessRequirement {
	path := strings.TrimSpace(r.URL.Path)
	switch {
	case path == operatorWebSocketPath:
		return accessRequirement{resource: authz.ResourceRuns, action: authz.ActionExecute}
	case path == "/v1/chat/completions":
		return accessRequirement{resource: authz.ResourceRuns, action: authz.ActionExecute}
	case path == "/dashboard/upload", path == "/webchat/upload":
		return accessRequirement{resource: authz.ResourceRuns, action: authz.ActionWrite}
	case path == "/dashboard/sse", path == "/webchat/sse":
		return accessRequirement{resource: authz.ResourceRuns, action: authz.ActionRead}
	case strings.HasPrefix(path, "/runtime/"):
		return runtimeAccessRequirement(r.Method, path)
	case strings.HasPrefix(path, "/operator/"):
		return operatorAccessRequirement(r)
	case strings.HasPrefix(path, "/channels"):
		return accessRequirement{resource: authz.ResourceChannels, action: methodAction(r.Method)}
	default:
		return accessRequirement{resource: authz.ResourceAll, action: methodAction(r.Method)}
	}
}

func operatorAccessRequirement(r *http.Request) accessRequirement {
	method := r.Method
	path := strings.TrimSpace(r.URL.Path)
	req := accessRequirement{
		resource: authz.ResourceOperator,
		action:   methodAction(method),
	}

	switch {
	case path == "/operator/durable-facts":
		switch strings.TrimSpace(r.URL.Query().Get("view")) {
		case "config":
			req.resource = authz.ResourceConfig
		case "context":
			req.resource = authz.ResourceKnowledge
		}
	case path == "/operator/authz":
		req.resource = authz.ResourceConfig
	case strings.HasPrefix(path, "/operator/policy/"),
		strings.HasPrefix(path, "/operator/controlplane/"):
		req.resource = authz.ResourceConfig
	case strings.HasPrefix(path, "/operator/cron/"):
		req.resource = authz.ResourceCron
	case strings.HasPrefix(path, "/operator/watch/"):
		req.resource = authz.ResourceWatch
	case strings.HasPrefix(path, "/operator/wakeup/"):
		req.resource = authz.ResourceWakeup
	case strings.HasPrefix(path, "/operator/hooks"):
		req.resource = authz.ResourceHooks
	case strings.HasPrefix(path, "/operator/usage/"):
		req.resource = authz.ResourceUsage
	case strings.HasPrefix(path, "/operator/wire/"):
		req.resource = authz.ResourceWire
	case strings.HasPrefix(path, "/operator/sandbox/"):
		req.resource = authz.ResourceSandbox
	case strings.HasPrefix(path, "/operator/approvals"):
		req.resource = authz.ResourceApprovals
	case strings.HasPrefix(path, "/operator/governance/"):
		req.resource = authz.ResourceGovernance
	case strings.HasPrefix(path, "/operator/audit/"):
		req.resource = authz.ResourceAudit
	case strings.HasPrefix(path, "/operator/channels"),
		strings.HasPrefix(path, "/operator/allowlist"),
		strings.HasPrefix(path, "/operator/pairing"):
		req.resource = authz.ResourceChannels
	case strings.HasPrefix(path, "/operator/config"),
		strings.HasPrefix(path, "/operator/models"),
		strings.HasPrefix(path, "/operator/setup"):
		req.resource = authz.ResourceConfig
	case strings.HasPrefix(path, "/operator/skills"):
		req.resource = authz.ResourceSkills
	case strings.HasPrefix(path, "/operator/knowledge"):
		req.resource = authz.ResourceKnowledge
	case strings.HasPrefix(path, "/operator/plugins"):
		req.resource = authz.ResourcePlugins
	case strings.HasPrefix(path, "/operator/discovery/"):
		req.resource = authz.ResourceDiscovery
	}

	switch {
	case strings.HasSuffix(path, "/resolve"), strings.HasSuffix(path, "/approve"), strings.HasSuffix(path, "/cancel"):
		req.action = authz.ActionApprove
	case strings.HasSuffix(path, "/run"),
		strings.HasSuffix(path, "/fire"),
		strings.HasSuffix(path, "/replay"),
		strings.HasSuffix(path, "/redrive"),
		path == "/operator/sandbox/exec",
		path == "/operator/models/validate",
		path == "/operator/models/test-chat",
		path == "/operator/channels/validate",
		path == "/operator/channels/detect",
		path == "/operator/channels/test-message",
		path == "/operator/tools/test":
		req.action = authz.ActionExecute
	}

	return req
}

func runtimeAccessRequirement(method, path string) accessRequirement {
	req := accessRequirement{
		resource: authz.ResourceRuns,
		action:   methodAction(method),
	}

	switch {
	case strings.HasPrefix(path, "/runtime/tools"):
		req.resource = authz.ResourceTools
	case strings.HasPrefix(path, "/runtime/sessions"):
		req.resource = authz.ResourceSessions
	case strings.HasPrefix(path, "/runtime/approvals"):
		req.resource = authz.ResourceApprovals
	case strings.HasPrefix(path, "/runtime/memory"):
		req.resource = authz.ResourceSessions
	case strings.HasPrefix(path, "/runtime/events"):
		req.resource = authz.ResourceRuns
	case strings.HasPrefix(path, "/runtime/artifacts"):
		req.resource = authz.ResourceRuns
	case strings.HasPrefix(path, "/runtime/interact"):
		req.resource = authz.ResourceRuns
	case strings.HasPrefix(path, "/runtime/runs"):
		req.resource = authz.ResourceRuns
	}

	switch {
	case strings.HasSuffix(path, "/resolve"):
		req.action = authz.ActionApprove
	case strings.HasSuffix(path, "/resume"), strings.HasSuffix(path, "/cancel"), path == "/runtime/interact", path == "/runtime/runs", path == "/v1/chat/completions":
		req.action = authz.ActionExecute
	}

	return req
}

func methodAction(method string) authz.Action {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return authz.ActionRead
	case http.MethodPost:
		return authz.ActionWrite
	case http.MethodPut, http.MethodPatch, http.MethodDelete:
		return authz.ActionWrite
	default:
		return authz.ActionWrite
	}
}

func principalFromIdentity(identity *AuthIdentity) *authz.Principal {
	if identity == nil {
		return nil
	}
	principal := &authz.Principal{
		Subject:  identity.Subject,
		Provider: identity.Provider,
		Scopes:   append([]string(nil), identity.Scopes...),
	}
	if len(identity.Metadata) > 0 {
		principal.Metadata = make(map[string]string, len(identity.Metadata))
		for key, value := range identity.Metadata {
			principal.Metadata[key] = value
		}
	}
	return principal
}
