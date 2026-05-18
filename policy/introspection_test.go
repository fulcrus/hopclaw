package policy

import (
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
)

func TestDescribeEngineReportsChainLayers(t *testing.T) {
	t.Parallel()

	base := NewDefaultEngine(Config{
		RequireApprovalForWrite:        true,
		AllowLocalWriteWithoutApproval: true,
		RequireApprovalCommunity:       true,
		DenyDestructive:                true,
		SkillInstallPolicy:             "ask",
		SafePatterns:                   []string{"git status"},
		DangerousTools:                 []string{"deploy.prod"},
		DefaultApprovalScope:           approval.ScopeOnce,
		MaxApprovalScope:               approval.ScopeSession,
	})
	base.SetGrantStore(approval.NewGrantStore())
	base.SetSecurityAuditor(audit.NewSecurityAuditor())

	summary := DescribeEngine(NewChainEngine(
		Layer{Name: "base", Engine: base},
		Layer{Name: "overlay", Engine: &chainStaticEngine{decision: Decision{Action: ActionAllow}}},
	))

	if summary.Kind != "chain" || !summary.Layered {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.LayerCount != 2 || len(summary.Layers) != 2 {
		t.Fatalf("layers = %+v", summary.Layers)
	}
	first := summary.Layers[0]
	if first.Name != "base" || first.Type != "default" {
		t.Fatalf("first layer = %+v", first)
	}
	if !first.GrantStoreWired || !first.SecurityAuditWired {
		t.Fatalf("first layer wiring = %+v", first)
	}
	if first.DangerousToolCount != 1 || first.SafePatternCount != 1 {
		t.Fatalf("first layer counts = %+v", first)
	}
	if !first.AllowLocalWriteWithoutApproval {
		t.Fatalf("first layer local write posture = %+v", first)
	}
	if first.ApprovalDefaults.DefaultScope != string(approval.ScopeOnce) || first.ApprovalDefaults.MaxScope != string(approval.ScopeSession) {
		t.Fatalf("approval defaults = %+v", first.ApprovalDefaults)
	}
	second := summary.Layers[1]
	if second.Name != "overlay" || second.Type == "" {
		t.Fatalf("second layer = %+v", second)
	}
}
