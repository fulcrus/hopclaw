package authz

import (
	"context"
	"errors"
	"testing"
)

func TestOpenDeciderAllowsRequests(t *testing.T) {
	t.Parallel()

	decision, err := OpenDecider{}.Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceConfig,
		Action:   ActionWrite,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if decision.Source != "open" {
		t.Fatalf("Source = %q, want open", decision.Source)
	}

	summary := Describe(OpenDecider{})
	if summary.Kind != "open" || summary.Mode != "toc" || summary.DefaultEffect != "allow" {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestExternalDeciderDelegates(t *testing.T) {
	t.Parallel()

	decider := ExternalDecider{
		Name: "corp-policy",
		Delegate: DecisionFunc(func(_ context.Context, req AuthorizationRequest) (AuthorizationDecision, error) {
			if req.Resource != ResourceOperator || req.Action != ActionRead {
				t.Fatalf("unexpected request = %+v", req)
			}
			return AuthorizationDecision{Allowed: true, Reason: "corp allow"}, nil
		}),
	}

	decision, err := decider.Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceOperator,
		Action:   ActionRead,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if decision.Source != "corp-policy" {
		t.Fatalf("Source = %q, want corp-policy", decision.Source)
	}
}

func TestExternalDeciderFallsBackOnError(t *testing.T) {
	t.Parallel()

	decider := ExternalDecider{
		Name: "corp-policy",
		Delegate: DecisionFunc(func(context.Context, AuthorizationRequest) (AuthorizationDecision, error) {
			return AuthorizationDecision{}, errors.New("policy backend unavailable")
		}),
		Fallback: OpenDecider{},
	}

	decision, err := decider.Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceRuns,
		Action:   ActionExecute,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = %v, want true", decision.Allowed)
	}
	if decision.Source != "open" {
		t.Fatalf("Source = %q, want open", decision.Source)
	}
	if got := decision.Metadata["fallback_reason"]; got != "policy backend unavailable" {
		t.Fatalf("fallback_reason = %q, want policy backend unavailable", got)
	}
}

func TestExternalDeciderFailsClosedWithoutDelegate(t *testing.T) {
	t.Parallel()

	decision, err := (ExternalDecider{}).Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceConfig,
		Action:   ActionWrite,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = %v, want false", decision.Allowed)
	}
	if decision.Source != "ExternalDecider" {
		t.Fatalf("Source = %q, want ExternalDecider", decision.Source)
	}
}
