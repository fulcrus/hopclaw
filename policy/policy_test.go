package policy

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func TestDefaultEngineDeniesUnknownTools(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "unknown",
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q", decision.Action)
	}
	if decision.PolicySource != policySourceUnknownTool {
		t.Fatalf("decision.PolicySource = %q", decision.PolicySource)
	}
	if decision.Summary == "" {
		t.Fatal("expected policy summary")
	}
}

func TestDefaultEngineRequiresApprovalForCommunityWrites(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.run",
		Tool: &skill.BoundTool{
			Package: &skill.SkillPackage{
				Trust: skill.TrustCommunity,
			},
			Manifest: skill.ToolManifest{
				Name:            "deploy.run",
				SideEffectClass: "external_write",
			},
			Eligibility: skill.EligibilityResult{
				Eligible: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q", decision.Action)
	}
	if len(decision.Reasons) < 2 {
		t.Fatalf("decision.Reasons = %#v", decision.Reasons)
	}
	if decision.PolicySource == "" {
		t.Fatal("expected policy source")
	}
	if !strings.Contains(strings.ToLower(decision.Summary), "approval") {
		t.Fatalf("decision.Summary = %q", decision.Summary)
	}
}

func TestDefaultEngineDeniesIneligibleAndDestructiveTools(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DenyDestructive: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "wipe.disk",
		Tool: &skill.BoundTool{
			Package: &skill.SkillPackage{
				Trust: skill.TrustInternal,
			},
			Manifest: skill.ToolManifest{
				Name:            "wipe.disk",
				SideEffectClass: "destructive",
			},
			Eligibility: skill.EligibilityResult{
				Eligible: false,
				Reasons:  []string{"missing env: WIPE_TOKEN"},
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q", decision.Action)
	}
	if decision.Reasons[0] != "missing env: WIPE_TOKEN" {
		t.Fatalf("decision.Reasons = %#v", decision.Reasons)
	}
}

func TestDefaultEngineDangerousToolRequiresApproval(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DangerousTools: []string{"deploy.prod", "db.migrate"},
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.prod",
		Input:    map[string]any{"target": "production"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "deploy.prod",
				SideEffectClass: "external_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionRequireApproval)
	}
	foundReason := false
	for _, r := range decision.Reasons {
		if r == "tool is marked as dangerous" {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Fatalf("expected 'tool is marked as dangerous' reason, got: %v", decision.Reasons)
	}
}

func TestDefaultEngineManifestApprovalBypassIsVisible(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		SkipManifestApproval: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "desktop.click",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:             "desktop.click",
				SideEffectClass:  "external_write",
				RequiresApproval: true,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionAllow)
	}
	foundReason := false
	for _, reason := range decision.Reasons {
		if strings.Contains(reason, "bypass manifest approval") || strings.Contains(reason, "policy is configured to bypass manifest approval") {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Fatalf("expected explicit bypass reason, got %#v", decision.Reasons)
	}
}

func TestDefaultEngineDeniesBlockedAvailability(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "browser.navigate",
		Tool: &toolspec.ResolvedTool{
			Descriptor: toolspec.ToolDefinition{
				Name: "browser.navigate",
				Availability: toolspec.ToolAvailability{
					Status:  toolspec.AvailabilityBlocked,
					Reasons: []string{"browser host is down"},
				},
			},
			Manifest: skill.ToolManifest{
				Name:            "browser.navigate",
				SideEffectClass: "external_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q", decision.Action)
	}
	if len(decision.Reasons) == 0 || decision.Reasons[0] != "browser host is down" {
		t.Fatalf("decision.Reasons = %#v", decision.Reasons)
	}
}

func TestDefaultEngineDangerousToolCaseInsensitive(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DangerousTools: []string{"Deploy.Prod"},
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.prod",
		Input:    map[string]any{},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "deploy.prod",
				SideEffectClass: "read",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionRequireApproval)
	}
}

func TestDefaultEngineNonDangerousToolAllowed(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DangerousTools: []string{"deploy.prod"},
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "file.read",
		Input:    map[string]any{"path": "README.md"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "file.read",
				SideEffectClass: "read",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionAllow)
	}
}

func TestDefaultEngineSkillInstallPolicy(t *testing.T) {
	t.Parallel()

	baseTool := &skill.BoundTool{
		Manifest: skill.ToolManifest{
			Name:             "skill.ensure",
			SideEffectClass:  "local_write",
			RequiresApproval: true,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}

	engineAsk := NewDefaultEngine(Config{SkillInstallPolicy: "ask"})
	askDecision, err := engineAsk.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.ensure",
		Input:    map[string]any{"goal": "news research"},
		Tool:     baseTool,
	})
	if err != nil {
		t.Fatalf("EvaluateTool(ask) error = %v", err)
	}
	if askDecision.Action != ActionRequireApproval {
		t.Fatalf("ask decision.Action = %q", askDecision.Action)
	}

	engineAuto := NewDefaultEngine(Config{SkillInstallPolicy: "auto"})
	autoDecision, err := engineAuto.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.install",
		Input:    map[string]any{"name": "news-research"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:             "skill.install",
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool(auto) error = %v", err)
	}
	if autoDecision.Action != ActionAllow {
		t.Fatalf("auto decision.Action = %q", autoDecision.Action)
	}

	engineDeny := NewDefaultEngine(Config{SkillInstallPolicy: "deny"})
	denyDecision, err := engineDeny.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.install",
		Input:    map[string]any{"name": "news-research"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:             "skill.install",
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool(deny) error = %v", err)
	}
	if denyDecision.Action != ActionDeny {
		t.Fatalf("deny decision.Action = %q", denyDecision.Action)
	}
}

func TestDefaultEngineUsesConfiguredApprovalScopes(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		RequireApprovalForWrite: true,
		DefaultApprovalScope:    approval.ScopeSession,
		MaxApprovalScope:        approval.ScopeSession,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "fs.write",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "fs.write",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.ApprovalPolicy == nil {
		t.Fatal("expected approval policy")
	}
	if decision.ApprovalPolicy.DefaultScope != approval.ScopeSession || decision.ApprovalPolicy.MaxScope != approval.ScopeSession {
		t.Fatalf("decision.ApprovalPolicy = %#v", decision.ApprovalPolicy)
	}
}

func TestDefaultEngineAutoAllowsLocalWritesWhenConfigured(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		RequireApprovalForWrite:        true,
		AllowLocalWriteWithoutApproval: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "fs.write",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "fs.write",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionAllow)
	}
}

func TestDefaultEngineStillRequiresApprovalForExternalWritesWhenLocalWritesAutoAllowed(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		RequireApprovalForWrite:        true,
		AllowLocalWriteWithoutApproval: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.run",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "deploy.run",
				SideEffectClass: "external_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want %q", decision.Action, ActionRequireApproval)
	}
}

func TestDefaultEngineUsesConfiguredSkillInstallApprovalScopes(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		SkillInstallPolicy:       "ask",
		SkillInstallDefaultScope: approval.ScopeSession,
		SkillInstallMaxScope:     approval.ScopeSession,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.ensure",
		Input:    map[string]any{"goal": "research"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "skill.ensure",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.ApprovalPolicy == nil {
		t.Fatal("expected approval policy")
	}
	if decision.ApprovalPolicy.DefaultScope != approval.ScopeSession || decision.ApprovalPolicy.MaxScope != approval.ScopeSession {
		t.Fatalf("decision.ApprovalPolicy = %#v", decision.ApprovalPolicy)
	}
}
