package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
)

func TestBuildAuthorizationDeciderDefaultsToOpen(t *testing.T) {
	t.Parallel()

	decider, err := buildAuthorizationDecider(Config{})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}
	summary := authz.Describe(decider)
	if summary.Kind != "open" {
		t.Fatalf("summary.Kind = %q, want open", summary.Kind)
	}
}

func TestBuildAuthorizationDeciderPrefersInjectedDecider(t *testing.T) {
	t.Parallel()

	want := authz.DecisionFunc(func(_ context.Context, _ authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
		return authz.AuthorizationDecision{Allowed: true, Source: "injected"}, nil
	})

	decider, err := buildAuthorizationDecider(Config{
		AuthorizationDecider: want,
		AuthZConfig: config.AuthZConfig{
			Mode: "webhook",
			Webhook: config.AuthZWebhookConfig{
				URL: "https://policy.example.com/decide",
			},
		},
		AuthConfig: config.AuthConfig{
			RBAC: config.AuthRBACConfig{Mode: "overlay"},
		},
	})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}

	decision, err := decider.Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Source != "injected" {
		t.Fatalf("decision.Source = %q, want injected", decision.Source)
	}
}

func TestBuildAuthorizationDeciderUsesConfiguredRBAC(t *testing.T) {
	t.Parallel()

	decider, err := buildAuthorizationDecider(Config{
		AuthConfig: config.AuthConfig{
			RBAC: config.AuthRBACConfig{
				DefaultRole: "viewer",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}
	summary := authz.Describe(decider)
	if summary.Kind != "rbac" {
		t.Fatalf("summary.Kind = %q, want rbac", summary.Kind)
	}
}

func TestBuildAuthorizationDeciderHonorsExplicitOpenMode(t *testing.T) {
	t.Parallel()

	decider, err := buildAuthorizationDecider(Config{
		AuthZConfig: config.AuthZConfig{
			Mode: "open",
		},
		AuthConfig: config.AuthConfig{
			RBAC: config.AuthRBACConfig{
				DefaultRole: "viewer",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}

	summary := authz.Describe(decider)
	if summary.Kind != "open" {
		t.Fatalf("summary.Kind = %q, want open", summary.Kind)
	}

	decision, err := decider.Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
}

func TestBuildAuthorizationDeciderRejectsExplicitRBACModeWithoutRBACConfig(t *testing.T) {
	t.Parallel()

	_, err := buildAuthorizationDecider(Config{
		AuthZConfig: config.AuthZConfig{
			Mode: "rbac",
		},
	})
	if err == nil {
		t.Fatal("expected buildAuthorizationDecider() to reject authz.mode=rbac without auth.rbac")
	}
}

func TestBuildAuthorizationDeciderUsesWebhookExternalDecider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":true,"source":"policy-service"}`))
	}))
	defer server.Close()

	decider, err := buildAuthorizationDecider(Config{
		AuthZConfig: config.AuthZConfig{
			Mode: "webhook",
			Webhook: config.AuthZWebhookConfig{
				URL: server.URL,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}
	summary := authz.Describe(decider)
	if summary.Kind != "external" {
		t.Fatalf("summary.Kind = %q, want external", summary.Kind)
	}

	decision, err := decider.Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceRuns,
		Action:   authz.ActionExecute,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if decision.Source != "policy-service" {
		t.Fatalf("decision.Source = %q, want policy-service", decision.Source)
	}
}

func TestBuildAuthZFallbackDeciderUsesDenyPolicy(t *testing.T) {
	t.Parallel()

	decider := buildAuthZFallbackDecider(config.AuthZConfig{Fallback: "deny"}, config.AuthRBACConfig{})
	if decider == nil {
		t.Fatal("buildAuthZFallbackDecider() = nil, want deny decider")
	}

	decision, err := decider.Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceConfig,
		Action:   authz.ActionWrite,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = %v, want false", decision.Allowed)
	}
	if decision.Source != "authz-fallback:deny" {
		t.Fatalf("decision.Source = %q, want authz-fallback:deny", decision.Source)
	}
}

func TestBuildAuthorizationDeciderWebhookFallbackUsesConfiguredRBAC(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	decider, err := buildAuthorizationDecider(Config{
		AuthZConfig: config.AuthZConfig{
			Mode:     "webhook",
			Fallback: "rbac",
			Webhook: config.AuthZWebhookConfig{
				URL: server.URL,
			},
		},
		AuthConfig: config.AuthConfig{
			RBAC: config.AuthRBACConfig{
				DefaultRole: "viewer",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAuthorizationDecider() error = %v", err)
	}

	decision, err := decider.Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
		Principal: &authz.Principal{
			Subject: "viewer",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = %v, want false because fallback viewer cannot read operator", decision.Allowed)
	}
	if decision.Source != "rbac" {
		t.Fatalf("decision.Source = %q, want rbac", decision.Source)
	}
	if got := decision.Metadata["fallback_reason"]; got == "" {
		t.Fatal("fallback_reason = empty, want delegated failure cause")
	}
}

func TestGatewayAuthZIntrospectionReportsExternalSummary(t *testing.T) {
	t.Parallel()

	handler := newTestGateway(t, Config{
		AuthorizationDecider: authz.ExternalDecider{
			Name: "corp-policy",
			Delegate: authz.DecisionFunc(func(_ context.Context, req authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
				if req.Resource != authz.ResourceConfig || req.Action != authz.ActionRead {
					return authz.AuthorizationDecision{}, errors.New("unexpected request shape")
				}
				return authz.AuthorizationDecision{Allowed: true, Source: "corp-policy"}, nil
			}),
		},
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "corp-key", Name: "corp", Enabled: true},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/authz", nil)
	req.Header.Set("X-API-Key", "corp-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/authz status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload authorizationSummaryResponse
	if err := decodeJSONResponse(rec, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Summary.Kind != "external" {
		t.Fatalf("summary.Kind = %q, want external", payload.Summary.Kind)
	}
	if payload.Summary.Name != "corp-policy" {
		t.Fatalf("summary.Name = %q, want corp-policy", payload.Summary.Name)
	}
	if payload.Decision == nil || payload.Decision.Source != "corp-policy" {
		t.Fatalf("decision = %#v, want source corp-policy", payload.Decision)
	}
}

func decodeJSONResponse(rec *httptest.ResponseRecorder, out any) error {
	return json.NewDecoder(rec.Body).Decode(out)
}
