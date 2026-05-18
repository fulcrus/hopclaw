package policy

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type Layer struct {
	Name   string
	Engine Engine
}

type ChainEngine struct {
	layers []Layer
}

func NewChainEngine(layers ...Layer) *ChainEngine {
	engine := &ChainEngine{}
	for _, layer := range layers {
		engine.AddLayer(layer.Name, layer.Engine)
	}
	return engine
}

func (e *ChainEngine) AddLayer(name string, engine Engine) *ChainEngine {
	if e == nil || engine == nil {
		return e
	}
	e.layers = append(e.layers, Layer{
		Name:   strings.TrimSpace(name),
		Engine: engine,
	})
	return e
}

func (e *ChainEngine) EvaluateTool(ctx context.Context, call ToolContext) (Decision, error) {
	if e == nil || len(e.layers) == 0 {
		return finalizeDecision(Decision{
			Action:       ActionAllow,
			PolicySource: "policy.chain/empty",
			Summary:      "allowed because no policy layers are configured",
		}), nil
	}

	final := Decision{Action: ActionAllow}
	winningPriority := actionPriority(final.Action)
	layerSources := make([]string, 0, len(e.layers))
	evaluated := false

	for _, layer := range e.layers {
		if layer.Engine == nil {
			continue
		}
		evaluated = true

		decision, err := layer.Engine.EvaluateTool(ctx, call)
		if err != nil {
			return Decision{}, err
		}
		decision = decision.Normalized()

		layerSource := chainLayerSource(layer, decision)
		if layerSource != "" {
			layerSources = append(layerSources, layerSource)
		}

		final.Reasons = append(final.Reasons, decision.Reasons...)
		final.ReasonCodes = append(final.ReasonCodes, decision.ReasonCodes...)
		final.AuditLabels = append(final.AuditLabels, decision.AuditLabels...)
		final.ApprovalPolicy = mergeApprovalPolicies(final.ApprovalPolicy, decision.ApprovalPolicy)

		priority := actionPriority(decision.Action)
		switch {
		case priority > winningPriority:
			final.Action = decision.Action
			final.PolicySource = normalize.FirstNonEmpty(decision.PolicySource, layerSource)
			final.Summary = strings.TrimSpace(decision.Summary)
			winningPriority = priority
		case priority == winningPriority:
			if winningPriority == actionPriority(ActionAllow) {
				final.PolicySource = normalize.FirstNonEmpty(decision.PolicySource, layerSource, final.PolicySource)
				if strings.TrimSpace(decision.Summary) != "" {
					final.Summary = strings.TrimSpace(decision.Summary)
				}
			} else {
				final.PolicySource = normalize.FirstNonEmpty(decision.PolicySource, layerSource, final.PolicySource)
				if strings.TrimSpace(decision.Summary) != "" {
					final.Summary = strings.TrimSpace(decision.Summary)
				}
			}
		}
	}

	if !evaluated {
		return finalizeDecision(Decision{
			Action:       ActionAllow,
			PolicySource: "policy.chain/empty",
			Summary:      "allowed because no active policy layers are configured",
		}), nil
	}

	final.PolicyLayers = dedupePolicyLayers(layerSources)
	if final.Action != ActionRequireApproval {
		final.ApprovalPolicy = nil
	}
	return finalizeDecision(final), nil
}

func (e *ChainEngine) SetGrantStore(gs *approval.GrantStore) {
	if e == nil || gs == nil {
		return
	}
	for _, layer := range e.layers {
		WireGrantStore(layer.Engine, gs)
	}
}

func (e *ChainEngine) SetSecurityAuditor(auditor *audit.SecurityAuditor) {
	if e == nil || auditor == nil {
		return
	}
	for _, layer := range e.layers {
		WireSecurityAuditor(layer.Engine, auditor)
	}
}

func actionPriority(action Action) int {
	switch action {
	case ActionDeny:
		return 2
	case ActionRequireApproval:
		return 1
	default:
		return 0
	}
}

func chainLayerSource(layer Layer, decision Decision) string {
	if source := strings.TrimSpace(decision.PolicySource); source != "" {
		return source
	}
	if name := strings.TrimSpace(layer.Name); name != "" {
		return "policy.chain/" + name
	}
	return "policy.chain/layer"
}

func dedupePolicyLayers(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeApprovalPolicies(current, next *domaingov.ApprovalPolicy) *domaingov.ApprovalPolicy {
	if current == nil && next == nil {
		return nil
	}
	if current == nil {
		policy := next.Normalized()
		if policy.Empty() {
			return nil
		}
		return &policy
	}
	if next == nil {
		policy := current.Normalized()
		if policy.Empty() {
			return nil
		}
		return &policy
	}
	defaultScope, err := approval.NarrowerScopeChecked(current.DefaultScope, next.DefaultScope)
	if err != nil {
		defaultScope = approval.ScopeOnce
	}
	maxScope, err := approval.NarrowerScopeChecked(current.MaxScope, next.MaxScope)
	if err != nil {
		maxScope = approval.ScopeOnce
	}
	merged := domaingov.ApprovalPolicy{
		DefaultScope: defaultScope,
		MaxScope:     maxScope,
	}.Normalized()
	if merged.Empty() {
		return nil
	}
	return &merged
}
