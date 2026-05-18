package gateway

import (
	"net/http"

	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type agentListResponse struct {
	Items []runtimesvc.AgentProfile `json:"items"`
	Count int                       `json:"count"`
}

type agentGetResponse struct {
	Agent runtimesvc.AgentProfile `json:"agent"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleAgentsList returns all registered agent profiles.
//
//	GET /operator/agents
func (g *Gateway) handleAgentsList(w http.ResponseWriter, _ *http.Request) {
	router := g.agentRouter()
	if router == nil {
		gwJSON(w, http.StatusOK, agentListResponse{Items: []runtimesvc.AgentProfile{}, Count: 0})
		return
	}
	profiles := router.List()
	gwJSON(w, http.StatusOK, agentListResponse{Items: profiles, Count: len(profiles)})
}

// handleAgentGet returns a single agent profile by name.
//
//	GET /operator/agents/{name}
func (g *Gateway) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	router := g.agentRouter()
	if router == nil {
		gwError(w, http.StatusNotFound, "agent routing not configured")
		return
	}
	name := r.PathValue("name")
	profile, ok := router.Get(name)
	if !ok {
		gwError(w, http.StatusNotFound, "agent profile not found")
		return
	}
	gwJSON(w, http.StatusOK, agentGetResponse{Agent: *profile})
}

// agentRouter returns the AgentRouter from the runtime service, or nil.
func (g *Gateway) agentRouter() *runtimesvc.AgentRouter {
	if g.runtime == nil {
		return nil
	}
	return g.runtime.AgentRouter()
}
