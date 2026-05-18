package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// handleAgentsList
// ---------------------------------------------------------------------------

func TestAgentsListNoRouter(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/agents", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("no router: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload agentListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestAgentsListWithProfiles(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	router := runtimesvc.NewAgentRouter([]runtimesvc.AgentProfile{
		{Name: "alpha", Description: "first agent"},
		{Name: "beta", Description: "second agent"},
	})
	gw.runtime.SetAgentRouter(router)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/agents", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload agentListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// handleAgentGet
// ---------------------------------------------------------------------------

func TestAgentGetNoRouter(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/agents/alpha", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no router: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAgentGetFound(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	router := runtimesvc.NewAgentRouter([]runtimesvc.AgentProfile{
		{Name: "alpha", Description: "first agent", Model: "gpt-4"},
	})
	gw.runtime.SetAgentRouter(router)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/agents/alpha", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload agentGetResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Agent.Name != "alpha" {
		t.Fatalf("name = %q, want alpha", payload.Agent.Name)
	}
	if payload.Agent.Model != "gpt-4" {
		t.Fatalf("model = %q, want gpt-4", payload.Agent.Model)
	}
}

func TestAgentGetNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	router := runtimesvc.NewAgentRouter([]runtimesvc.AgentProfile{
		{Name: "alpha", Description: "first agent"},
	})
	gw.runtime.SetAgentRouter(router)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/agents/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
