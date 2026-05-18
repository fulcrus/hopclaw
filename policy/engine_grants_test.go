package policy

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// Grant store integration — session grants
// ---------------------------------------------------------------------------

func TestEngineSessionGrantAllows(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("sess-1", "fs.write", approval.ScopeSession)

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
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
		t.Fatalf("decision.Action = %q, want allow (session grant)", decision.Action)
	}
}

func TestEngineScopedGrantAllowsMatchingPathOnly(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.GrantScoped("sess-1", "fs.write", approval.ScopeSession, approval.ResourceScope{
		PathPrefixes: []string{"reports"},
	})

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
		Input:     map[string]any{"path": "reports/daily.txt"},
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "fs.write", SideEffectClass: "local_write"},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow", decision.Action)
	}

	decision, err = engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
		Input:     map[string]any{"path": "secrets.txt"},
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "fs.write", SideEffectClass: "local_write"},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval", decision.Action)
	}
}

func TestEngineSessionGrantDoesNotLeakToOtherSession(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("sess-1", "fs.write", approval.ScopeSession)

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-2",
		ToolName:  "fs.write",
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
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval (no grant for sess-2)", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Grant store integration — remembered grants
// ---------------------------------------------------------------------------

func TestEngineAlwaysGrantAllowsWithinSameSession(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("sess-1", "fs.write", approval.ScopeAlways)

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
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
		t.Fatalf("decision.Action = %q, want allow (remembered grant)", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Grant store integration — deny grants
// ---------------------------------------------------------------------------

func TestEngineSessionDenyOverridesEverything(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("sess-1", "fs.write", approval.ScopeDeny)

	engine := NewDefaultEngine(Config{AllowUnknownTools: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "fs.write",
				SideEffectClass: "read",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q, want deny", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Grant store integration — revoke
// ---------------------------------------------------------------------------

func TestEngineRevokedGrantFallsThrough(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("sess-1", "fs.write", approval.ScopeSession)
	gs.Revoke("sess-1", "fs.write")

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
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
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval (revoked)", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Grant store — nil grant store
// ---------------------------------------------------------------------------

func TestEngineNilGrantStoreFallsThrough(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	// No SetGrantStore call.

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "sess-1",
		ToolName:  "fs.write",
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
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Grant store — empty session ID skips grant check
// ---------------------------------------------------------------------------

func TestEngineEmptySessionIDSkipsGrantCheck(t *testing.T) {
	t.Parallel()

	gs := approval.NewGrantStore()
	gs.Grant("", "fs.write", approval.ScopeSession)

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	engine.SetGrantStore(gs)

	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		SessionID: "",
		ToolName:  "fs.write",
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
	// Empty session ID should skip grant check, falling through to require approval.
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval (empty session)", decision.Action)
	}
}
