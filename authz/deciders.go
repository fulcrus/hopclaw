package authz

import (
	"context"
	"errors"
)

// DecisionFunc adapts a function into an AuthorizationDecider.
type DecisionFunc func(context.Context, AuthorizationRequest) (AuthorizationDecision, error)

// Decide evaluates an authorization request.
func (fn DecisionFunc) Decide(ctx context.Context, req AuthorizationRequest) (AuthorizationDecision, error) {
	if fn == nil {
		return AuthorizationDecision{}, errors.New("authorization decision function is nil")
	}
	return fn(ctx, req)
}

// OpenDecider allows every request. It is the default toC experience where
// no extra authorization policy needs to be configured.
type OpenDecider struct{}

// Decide allows the request unconditionally.
func (OpenDecider) Decide(_ context.Context, req AuthorizationRequest) (AuthorizationDecision, error) {
	return AuthorizationDecision{
		Allowed: true,
		Reason:  "open decider allows all requests",
		Source:  "open",
		Metadata: map[string]string{
			"resource": string(req.Resource),
			"action":   string(req.Action),
		},
	}, nil
}

// DescribeAuthorization reports the open default authorization model.
func (OpenDecider) DescribeAuthorization() Summary {
	return Summary{
		Kind:          "open",
		Name:          "OpenDecider",
		Mode:          "toc",
		DefaultEffect: "allow",
		Resources:     ResourceNames(AllResources()),
		Actions:       ActionNames(AllActions()),
		Bindings: []BindingSummary{
			{
				Name:        "all-callers",
				Kind:        "policy",
				Description: "Zero-config allow-all authorization for standalone deployments.",
				Resources:   []string{"*"},
				Actions:     ActionNames(AllActions()),
			},
		},
		Notes: []string{
			"Use ExternalDecider or a custom AuthorizationDecider to enforce enterprise policy.",
		},
	}
}

// ExternalDecider delegates authorization to an injected enterprise policy.
// A fallback decider can be supplied to define fail-open or fail-closed
// behavior when the external policy is unavailable.
type ExternalDecider struct {
	Name     string
	Delegate AuthorizationDecider
	Fallback AuthorizationDecider
}

// Decide evaluates the request through the external delegate. When the
// delegate returns an error, the optional fallback decider is used.
func (d ExternalDecider) Decide(ctx context.Context, req AuthorizationRequest) (AuthorizationDecision, error) {
	if d.Delegate == nil {
		if d.Fallback != nil {
			decision, err := d.Fallback.Decide(ctx, req)
			if decision.Source == "" {
				decision.Source = d.sourceName() + ":fallback"
			}
			return decision, err
		}
		return AuthorizationDecision{
			Allowed: false,
			Reason:  "external authorization delegate is not configured",
			Source:  d.sourceName(),
		}, nil
	}

	decision, err := d.Delegate.Decide(ctx, req)
	if err == nil {
		if decision.Source == "" {
			decision.Source = d.sourceName()
		}
		return decision, nil
	}
	if d.Fallback == nil {
		return AuthorizationDecision{}, err
	}

	fallbackDecision, fallbackErr := d.Fallback.Decide(ctx, req)
	if fallbackDecision.Source == "" {
		fallbackDecision.Source = d.sourceName() + ":fallback"
	}
	if fallbackDecision.Metadata == nil {
		fallbackDecision.Metadata = map[string]string{}
	}
	fallbackDecision.Metadata["fallback_reason"] = err.Error()
	return fallbackDecision, fallbackErr
}

// DescribeAuthorization reports the delegated authorization model.
func (d ExternalDecider) DescribeAuthorization() Summary {
	notes := []string{
		"Authorization is delegated to an injected external policy.",
	}
	if d.Fallback != nil {
		notes = append(notes, "A fallback decider is configured for delegate failures.")
	}
	return Summary{
		Kind:          "external",
		Name:          d.sourceName(),
		Mode:          "tob",
		DefaultEffect: "delegate",
		Resources:     ResourceNames(AllResources()),
		Actions:       ActionNames(AllActions()),
		Notes:         notes,
		Metadata: map[string]string{
			"has_delegate": boolString(d.Delegate != nil),
			"has_fallback": boolString(d.Fallback != nil),
		},
	}
}

func (d ExternalDecider) sourceName() string {
	if d.Name != "" {
		return d.Name
	}
	return "ExternalDecider"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
