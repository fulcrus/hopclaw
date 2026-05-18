package authzrbac

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
)

func TestDefaultDeciderAllowsOperatorSurfaces(t *testing.T) {
	t.Parallel()

	decision, err := NewDefaultDecider().Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
		Principal: &authz.Principal{
			Scopes: []string{"role:operator"},
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if got := decision.Metadata["resolved_role"]; got != "operator" {
		t.Fatalf("resolved_role = %q, want operator", got)
	}
}

func TestDefaultDeciderCanFailClosedWithViewerDefault(t *testing.T) {
	t.Parallel()

	decision, err := NewFromConfig(config.AuthRBACConfig{
		DefaultRole: "viewer",
	}).Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = %v, want false", decision.Allowed)
	}
}

func TestDefaultDeciderFallsBackToImplicitOperatorForAuthenticatedCaller(t *testing.T) {
	t.Parallel()

	decision, err := NewFromConfig(config.AuthRBACConfig{
		Mode: "overlay",
	}).Decide(context.Background(), authz.AuthorizationRequest{
		Resource: authz.ResourceOperator,
		Action:   authz.ActionRead,
		Principal: &authz.Principal{
			Subject: "user:test",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if got := decision.Metadata["resolved_role"]; got != "operator" {
		t.Fatalf("resolved_role = %q, want operator", got)
	}
}

func TestDescribeAuthorizationReturnsRoleBindings(t *testing.T) {
	t.Parallel()

	summary := NewFromConfig(config.AuthRBACConfig{
		ScopePrefixes: []string{"rbac:"},
		Roles: []config.AuthRBACRoleConfig{
			{
				Name: "auditor",
				Grants: []config.AuthRBACGrantConfig{
					{Resource: "config", Permissions: []string{"read"}},
					{Resource: "audit", Permissions: []string{"read"}},
				},
			},
		},
	}).DescribeAuthorization()

	if summary.Kind != "rbac" {
		t.Fatalf("Kind = %q, want rbac", summary.Kind)
	}
	if len(summary.Bindings) == 0 {
		t.Fatalf("Bindings = %+v, want non-empty", summary.Bindings)
	}
	if got := summary.Metadata["implicit_authenticated_role"]; got != "operator" {
		t.Fatalf("implicit_authenticated_role = %q, want operator", got)
	}
}
