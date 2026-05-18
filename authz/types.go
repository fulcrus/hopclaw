package authz

import (
	"context"
	"strings"
)

// Resource is a stable authorization resource identifier.
type Resource string

const (
	ResourceSessions   Resource = "sessions"
	ResourceRuns       Resource = "runs"
	ResourceTools      Resource = "tools"
	ResourceConfig     Resource = "config"
	ResourceApprovals  Resource = "approvals"
	ResourceChannels   Resource = "channels"
	ResourceCron       Resource = "cron"
	ResourceHooks      Resource = "hooks"
	ResourceUsage      Resource = "usage"
	ResourceWire       Resource = "wire"
	ResourceOperator   Resource = "operator"
	ResourceWatch      Resource = "watch"
	ResourceWakeup     Resource = "wakeup"
	ResourceSandbox    Resource = "sandbox"
	ResourceGovernance Resource = "governance"
	ResourceAudit      Resource = "audit"
	ResourceSkills     Resource = "skills"
	ResourceKnowledge  Resource = "knowledge"
	ResourcePlugins    Resource = "plugins"
	ResourceDiscovery  Resource = "discovery"
	ResourceAll        Resource = "*"
)

// Action is a stable authorization action identifier.
type Action string

const (
	ActionRead    Action = "read"
	ActionWrite   Action = "write"
	ActionAdmin   Action = "admin"
	ActionApprove Action = "approve"
	ActionExecute Action = "execute"
)

// Principal describes the authenticated caller requesting access.
type Principal struct {
	Subject  string            `json:"subject,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Scopes   []string          `json:"scopes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AuthorizationRequest is the stable authorization input contract.
type AuthorizationRequest struct {
	Resource  Resource          `json:"resource"`
	Action    Action            `json:"action"`
	Method    string            `json:"method,omitempty"`
	Path      string            `json:"path,omitempty"`
	Principal *Principal        `json:"principal,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// AuthorizationDecision is the stable authorization output contract.
type AuthorizationDecision struct {
	Allowed  bool              `json:"allowed"`
	Reason   string            `json:"reason,omitempty"`
	Source   string            `json:"source,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AuthorizationDecider evaluates authorization requests.
type AuthorizationDecider interface {
	Decide(context.Context, AuthorizationRequest) (AuthorizationDecision, error)
}

// BindingSummary describes one policy unit exposed by an authorization backend.
type BindingSummary struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind,omitempty"`
	Description string   `json:"description,omitempty"`
	Resources   []string `json:"resources,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

// Summary describes the currently wired authorization strategy.
type Summary struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	DefaultEffect string            `json:"default_effect,omitempty"`
	Resources     []string          `json:"resources,omitempty"`
	Actions       []string          `json:"actions,omitempty"`
	Bindings      []BindingSummary  `json:"bindings,omitempty"`
	Notes         []string          `json:"notes,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Describer exposes a stable summary for operator introspection endpoints.
type Describer interface {
	DescribeAuthorization() Summary
}

// AllResources returns the canonical authorization resource catalog.
func AllResources() []Resource {
	return []Resource{
		ResourceAll,
		ResourceApprovals,
		ResourceAudit,
		ResourceChannels,
		ResourceConfig,
		ResourceCron,
		ResourceDiscovery,
		ResourceGovernance,
		ResourceHooks,
		ResourceKnowledge,
		ResourceOperator,
		ResourcePlugins,
		ResourceRuns,
		ResourceSandbox,
		ResourceSessions,
		ResourceSkills,
		ResourceTools,
		ResourceUsage,
		ResourceWakeup,
		ResourceWatch,
		ResourceWire,
	}
}

// AllActions returns the canonical authorization action catalog.
func AllActions() []Action {
	return []Action{
		ActionAdmin,
		ActionApprove,
		ActionExecute,
		ActionRead,
		ActionWrite,
	}
}

// ResourceNames renders the stable resource catalog as strings.
func ResourceNames(items []Resource) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

// ActionNames renders the stable action catalog as strings.
func ActionNames(items []Action) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

// ParseResource canonicalizes a resource identifier and reports whether it is
// part of the stable resource catalog.
func ParseResource(value string) (Resource, bool) {
	switch Resource(strings.ToLower(strings.TrimSpace(value))) {
	case ResourceAll,
		ResourceApprovals,
		ResourceAudit,
		ResourceChannels,
		ResourceConfig,
		ResourceCron,
		ResourceDiscovery,
		ResourceGovernance,
		ResourceHooks,
		ResourceKnowledge,
		ResourceOperator,
		ResourcePlugins,
		ResourceRuns,
		ResourceSandbox,
		ResourceSessions,
		ResourceSkills,
		ResourceTools,
		ResourceUsage,
		ResourceWakeup,
		ResourceWatch,
		ResourceWire:
		return Resource(strings.ToLower(strings.TrimSpace(value))), true
	default:
		return "", false
	}
}

// ParseAction canonicalizes an action identifier and reports whether it is
// part of the stable action catalog.
func ParseAction(value string) (Action, bool) {
	switch Action(strings.ToLower(strings.TrimSpace(value))) {
	case ActionAdmin, ActionApprove, ActionExecute, ActionRead, ActionWrite:
		return Action(strings.ToLower(strings.TrimSpace(value))), true
	default:
		return "", false
	}
}

// Describe returns a generic summary for a decider. Known deciders can
// override this through the Describer interface.
func Describe(decider AuthorizationDecider) Summary {
	if d, ok := decider.(Describer); ok {
		return d.DescribeAuthorization()
	}
	return Summary{
		Kind:          "custom",
		DefaultEffect: "delegate",
		Resources:     ResourceNames(AllResources()),
		Actions:       ActionNames(AllActions()),
		Notes: []string{
			"Authorization is enforced by a custom decider.",
		},
	}
}
