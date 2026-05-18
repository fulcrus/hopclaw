package gateway

import (
	"net/http"

	"github.com/fulcrus/hopclaw/authz"
)

type authorizationSummaryResponse struct {
	Summary  authz.Summary                `json:"summary"`
	Decision *authz.AuthorizationDecision `json:"decision,omitempty"`
	Identity *AuthIdentity                `json:"identity,omitempty"`
}

func (g *Gateway) handleAuthorizationSummary(w http.ResponseWriter, r *http.Request) {
	decider := g.authzDecider
	if decider == nil {
		decider = authz.OpenDecider{}
	}
	identity := AuthIdentityFromContext(r.Context())
	req := accessRequirementForRequest(r)
	decision, err := decider.Decide(r.Context(), authz.AuthorizationRequest{
		Resource:  req.resource,
		Action:    req.action,
		Method:    r.Method,
		Path:      r.URL.Path,
		Principal: principalFromIdentity(identity),
	})
	if err != nil {
		gwError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, authorizationSummaryResponse{
		Summary:  authz.Describe(decider),
		Decision: &decision,
		Identity: identity,
	})
}
