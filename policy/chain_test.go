package policy

import (
	"context"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
)

type chainStaticEngine struct {
	decision Decision
}

func (e *chainStaticEngine) EvaluateTool(context.Context, ToolContext) (Decision, error) {
	return e.decision, nil
}

type chainObservedEngine struct {
	decision        Decision
	grantStore      *approval.GrantStore
	securityAuditor *audit.SecurityAuditor
}

func (e *chainObservedEngine) EvaluateTool(context.Context, ToolContext) (Decision, error) {
	return e.decision, nil
}

func (e *chainObservedEngine) SetGrantStore(gs *approval.GrantStore) {
	e.grantStore = gs
}

func (e *chainObservedEngine) SetSecurityAuditor(auditor *audit.SecurityAuditor) {
	e.securityAuditor = auditor
}

func TestChainEngineRequireApprovalBeatsAllow(t *testing.T) {
	t.Parallel()

	engine := NewChainEngine(
		Layer{
			Name: "base",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				Reasons:      []string{"base approval required"},
				PolicySource: "policy.test/base",
				Summary:      "approval required by base policy",
			}},
		},
		Layer{
			Name: "overlay",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionAllow,
				PolicySource: "policy.test/overlay_allow",
				Summary:      "explicitly allowed by overlay",
			}},
		},
	)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{ToolName: "fs.write"})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionRequireApproval)
	}
	if decision.PolicySource != "policy.test/base" {
		t.Fatalf("decision.PolicySource = %q, want base source", decision.PolicySource)
	}
	if decision.Summary != "approval required by base policy" {
		t.Fatalf("decision.Summary = %q", decision.Summary)
	}
	wantLayers := []string{"policy.test/base", "policy.test/overlay_allow"}
	if !reflect.DeepEqual(decision.PolicyLayers, wantLayers) {
		t.Fatalf("decision.PolicyLayers = %#v, want %#v", decision.PolicyLayers, wantLayers)
	}
}

func TestChainEngineDenyBeatsApproval(t *testing.T) {
	t.Parallel()

	engine := NewChainEngine(
		Layer{
			Name: "base",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				Reasons:      []string{"base approval required"},
				PolicySource: "policy.test/base",
				Summary:      "approval required by base policy",
			}},
		},
		Layer{
			Name: "overlay",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionDeny,
				Reasons:      []string{"overlay denied the operation"},
				PolicySource: "policy.test/overlay_deny",
				Summary:      "denied by overlay policy",
			}},
		},
	)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{ToolName: "fs.write"})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionDeny)
	}
	if decision.PolicySource != "policy.test/overlay_deny" {
		t.Fatalf("decision.PolicySource = %q, want overlay deny source", decision.PolicySource)
	}
	if decision.Summary != "denied by overlay policy" {
		t.Fatalf("decision.Summary = %q", decision.Summary)
	}
	wantLayers := []string{"policy.test/base", "policy.test/overlay_deny"}
	if !reflect.DeepEqual(decision.PolicyLayers, wantLayers) {
		t.Fatalf("decision.PolicyLayers = %#v, want %#v", decision.PolicyLayers, wantLayers)
	}
}

func TestChainEngineFallsBackToLayerNameForPolicySourceAndLayers(t *testing.T) {
	t.Parallel()

	engine := NewChainEngine(
		Layer{
			Name:   "base",
			Engine: &chainStaticEngine{decision: Decision{Action: ActionAllow}},
		},
	)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{ToolName: "fs.read"})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.PolicySource != "policy.chain/base" {
		t.Fatalf("decision.PolicySource = %q, want %q", decision.PolicySource, "policy.chain/base")
	}
	wantLayers := []string{"policy.chain/base"}
	if !reflect.DeepEqual(decision.PolicyLayers, wantLayers) {
		t.Fatalf("decision.PolicyLayers = %#v, want %#v", decision.PolicyLayers, wantLayers)
	}
}

func TestChainEngineForwardsGrantStoreAndSecurityAuditor(t *testing.T) {
	t.Parallel()

	base := &chainObservedEngine{decision: Decision{Action: ActionAllow}}
	overlay := &chainObservedEngine{decision: Decision{Action: ActionAllow}}
	engine := NewChainEngine(
		Layer{Name: "base", Engine: base},
		Layer{Name: "overlay", Engine: overlay},
	)

	grantStore := approval.NewGrantStore()
	auditor := audit.NewSecurityAuditor()

	if !WireGrantStore(engine, grantStore) {
		t.Fatal("expected grant store wiring to succeed")
	}
	if !WireSecurityAuditor(engine, auditor) {
		t.Fatal("expected security auditor wiring to succeed")
	}
	if base.grantStore != grantStore || overlay.grantStore != grantStore {
		t.Fatalf("grant store wiring mismatch: base=%p overlay=%p want=%p", base.grantStore, overlay.grantStore, grantStore)
	}
	if base.securityAuditor != auditor || overlay.securityAuditor != auditor {
		t.Fatalf("security auditor wiring mismatch: base=%p overlay=%p want=%p", base.securityAuditor, overlay.securityAuditor, auditor)
	}
}

func TestChainEngineMergesApprovalPoliciesRestrictively(t *testing.T) {
	t.Parallel()

	engine := NewChainEngine(
		Layer{
			Name: "base",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				PolicySource: "policy.test/base",
				ApprovalPolicy: &domaingov.ApprovalPolicy{
					DefaultScope: approval.ScopeSession,
					MaxScope:     approval.ScopeAlways,
				},
			}},
		},
		Layer{
			Name: "overlay",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				PolicySource: "policy.test/overlay",
				ApprovalPolicy: &domaingov.ApprovalPolicy{
					DefaultScope: approval.ScopeOnce,
					MaxScope:     approval.ScopeSession,
				},
			}},
		},
	)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{ToolName: "fs.write"})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.ApprovalPolicy == nil {
		t.Fatal("expected approval policy")
	}
	if decision.ApprovalPolicy.DefaultScope != approval.ScopeOnce {
		t.Fatalf("decision.ApprovalPolicy.DefaultScope = %q, want %q", decision.ApprovalPolicy.DefaultScope, approval.ScopeOnce)
	}
	if decision.ApprovalPolicy.MaxScope != approval.ScopeSession {
		t.Fatalf("decision.ApprovalPolicy.MaxScope = %q, want %q", decision.ApprovalPolicy.MaxScope, approval.ScopeSession)
	}
}

func TestChainEngineInvalidApprovalScopeFailsClosed(t *testing.T) {
	t.Parallel()

	engine := NewChainEngine(
		Layer{
			Name: "base",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				PolicySource: "policy.test/base",
				ApprovalPolicy: &domaingov.ApprovalPolicy{
					DefaultScope: "invalid",
					MaxScope:     approval.ScopeAlways,
				},
			}},
		},
		Layer{
			Name: "overlay",
			Engine: &chainStaticEngine{decision: Decision{
				Action:       ActionRequireApproval,
				PolicySource: "policy.test/overlay",
				ApprovalPolicy: &domaingov.ApprovalPolicy{
					DefaultScope: approval.ScopeSession,
					MaxScope:     approval.ScopeAlways,
				},
			}},
		},
	)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{ToolName: "fs.write"})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.ApprovalPolicy == nil {
		t.Fatal("expected approval policy")
	}
	if decision.ApprovalPolicy.DefaultScope != approval.ScopeOnce {
		t.Fatalf("decision.ApprovalPolicy.DefaultScope = %q, want %q", decision.ApprovalPolicy.DefaultScope, approval.ScopeOnce)
	}
	if decision.ApprovalPolicy.MaxScope != approval.ScopeAlways {
		t.Fatalf("decision.ApprovalPolicy.MaxScope = %q, want %q", decision.ApprovalPolicy.MaxScope, approval.ScopeAlways)
	}
}
