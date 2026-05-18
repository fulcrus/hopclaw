package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
)

func TestGatewayRuntimeEndpointsAcceptAPIKeyAuth(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthToken: "legacy-runtime-token",
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "api-key-1", Name: "client", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/runtime/sessions", nil)
	req.Header.Set("X-API-Key", "api-key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions with api key status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayOpenDeciderAllowsOperatorEndpointsWithoutExplicitRBAC(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "viewer-key", Name: "viewer", Enabled: true, Scopes: []string{"role:viewer"}},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "viewer-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status with open decider status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/sessions", nil)
	req.Header.Set("X-API-Key", "viewer-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions with open decider status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayProtectedRoutesFailClosedOnAuthInitError(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			JWT: &config.AuthJWTConfig{Algorithm: "HS256"},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /operator/status with invalid auth config status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayRBACDefaultRoleCanFailClosedForOperatorSurface(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "plain-key", Name: "plain", Enabled: true},
			},
			RBAC: config.AuthRBACConfig{
				DefaultRole: "viewer",
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "plain-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /operator/status with default viewer role status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayCustomRBACRoleCanReadOperatorSurface(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "auditor-key", Name: "auditor", Enabled: true, Scopes: []string{"rbac:auditor"}},
			},
			RBAC: config.AuthRBACConfig{
				ScopePrefixes: []string{"rbac:"},
				Roles: []config.AuthRBACRoleConfig{
					{
						Name: "auditor",
						Grants: []config.AuthRBACGrantConfig{
							{Resource: "operator", Permissions: []string{"read"}},
							{Resource: "audit", Permissions: []string{"read"}},
						},
					},
				},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "auditor-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status with auditor role status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/operator/hooks", nil)
	req.Header.Set("X-API-Key", "auditor-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /operator/hooks with auditor role status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayInjectedAuthorizationDeciderCanDenyOperatorSurface(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthorizationDecider: authz.ExternalDecider{
			Name: "corp-policy",
			Delegate: authz.DecisionFunc(func(_ context.Context, req authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
				if req.Resource == authz.ResourceOperator {
					return authz.AuthorizationDecision{Allowed: false, Reason: "corp policy denies operator access"}, nil
				}
				return authz.AuthorizationDecision{Allowed: true}, nil
			}),
		},
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "corp-key", Name: "corp", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "corp-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /operator/status with injected decider status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/runtime/sessions", nil)
	req.Header.Set("X-API-Key", "corp-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/sessions with injected decider status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayAuthorizationRequestsDoNotInjectLegacyRBACMetadata(t *testing.T) {
	t.Parallel()

	var captured authz.AuthorizationRequest
	handler := newTestGateway(t, Config{
		AuthorizationDecider: authz.DecisionFunc(func(_ context.Context, req authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
			captured = req
			return authz.AuthorizationDecision{Allowed: true, Source: "capture"}, nil
		}),
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "capture-key", Name: "capture", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "capture-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status status = %d body=%s", rec.Code, rec.Body.String())
	}
	if captured.Metadata != nil {
		t.Fatalf("authorization metadata = %#v, want nil generic contract", captured.Metadata)
	}
}

func TestGatewayConfiguredWebhookAuthZDeciderCanAllowOperatorSurface(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req authz.AuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if req.Resource != authz.ResourceOperator || req.Action != authz.ActionRead {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(authz.AuthorizationDecision{Allowed: true})
	}))
	defer server.Close()

	handler := newTestGateway(t, Config{
		AuthZConfig: config.AuthZConfig{
			Mode: "webhook",
			Webhook: config.AuthZWebhookConfig{
				URL: server.URL,
			},
		},
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "corp-key", Name: "corp", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "corp-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/status with webhook authz status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayInvalidWebhookAuthZInitFailsClosed(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthZConfig: config.AuthZConfig{
			Mode: "webhook",
			Webhook: config.AuthZWebhookConfig{
				URL: "://bad-url",
			},
		},
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "corp-key", Name: "corp", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/status", nil)
	req.Header.Set("X-API-Key", "corp-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /operator/status with invalid webhook authz status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayRBACKeepsImplicitOperatorFallbackWithoutDefaultRole(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "plain-key", Name: "plain", Enabled: true},
			},
			RBAC: config.AuthRBACConfig{
				Mode: "overlay",
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/authz", nil)
	req.Header.Set("X-API-Key", "plain-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/authz status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload authorizationSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Decision == nil {
		t.Fatal("Decision = nil, want authorization decision")
	}
	if got := payload.Decision.Metadata["resolved_role"]; got != "operator" {
		t.Fatalf("resolved_role = %q, want operator", got)
	}
}

func TestGatewayAuthZIntrospectionEndpointReportsResolvedRole(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "auditor-key", Name: "auditor", Enabled: true, Scopes: []string{"rbac:auditor"}},
			},
			RBAC: config.AuthRBACConfig{
				ScopePrefixes: []string{"rbac:"},
				Roles: []config.AuthRBACRoleConfig{
					{
						Name: "auditor",
						Grants: []config.AuthRBACGrantConfig{
							{Resource: "config", Permissions: []string{"read"}},
						},
					},
				},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/authz", nil)
	req.Header.Set("X-API-Key", "auditor-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/authz status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload authorizationSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Decision == nil {
		t.Fatalf("Decision = nil, want authorization decision")
	}
	if got := payload.Decision.Metadata["resolved_role"]; got != "auditor" {
		t.Fatalf("resolved_role = %q, want auditor", got)
	}
	if payload.Summary.Kind != "rbac" {
		t.Fatalf("summary.kind = %q, want rbac", payload.Summary.Kind)
	}
	if len(payload.Summary.Bindings) == 0 {
		t.Fatal("expected authz bindings in response")
	}
}

func TestGatewayDurableFactsAccessRequirementUsesFocusedResources(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/operator/durable-facts?view=config", nil)
	got := accessRequirementForRequest(req)
	if got.resource != authz.ResourceConfig || got.action != authz.ActionRead {
		t.Fatalf("config durable facts requirement = %#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/operator/durable-facts?view=context", nil)
	got = accessRequirementForRequest(req)
	if got.resource != authz.ResourceKnowledge || got.action != authz.ActionRead {
		t.Fatalf("context durable facts requirement = %#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/operator/durable-facts", nil)
	got = accessRequirementForRequest(req)
	if got.resource != authz.ResourceOperator || got.action != authz.ActionRead {
		t.Fatalf("operator durable facts requirement = %#v", got)
	}
}
