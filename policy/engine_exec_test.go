package policy

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// Safe command whitelist for exec tools
// ---------------------------------------------------------------------------

func TestEngineExecSafeCommandAllowed(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.run",
		Input:    map[string]any{"command": "ls -la"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.run",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow for safe command 'ls -la'", decision.Action)
	}
}

func TestEngineExecShellSafeCommand(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.shell",
		Input:    map[string]any{"command": "git status"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.shell",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow for safe git status", decision.Action)
	}
}

func TestEngineExecScriptSafeCommand(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.script",
		Input:    map[string]any{"command": "pwd"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.script",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow for safe pwd", decision.Action)
	}
}

func TestEngineExecUnsafeCommandRequiresApproval(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.run",
		Input:    map[string]any{"command": "rm -rf /tmp/test"},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.run",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval for unsafe command", decision.Action)
	}
}

func TestEngineExecNoCommandField(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.run",
		Input:    map[string]any{"script": "echo hi"}, // wrong field name
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.run",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval (no command field)", decision.Action)
	}
}

func TestEngineExecNilInput(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.run",
		Input:    nil,
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.run",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval (nil input)", decision.Action)
	}
}

func TestEngineNonExecToolNotMatchedByWhitelist(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "fs.write",
		Input:    map[string]any{"command": "ls -la"}, // command field ignored for non-exec
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
		t.Fatalf("decision.Action = %q, want require_approval (not an exec tool)", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// AllowUnknownTools
// ---------------------------------------------------------------------------

func TestEngineAllowUnknownToolsTrue(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{AllowUnknownTools: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "mysterious.tool",
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow", decision.Action)
	}
}

func TestEngineAllowUnknownToolsFalse(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{AllowUnknownTools: false})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "mysterious.tool",
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q, want deny", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// DenyDestructive
// ---------------------------------------------------------------------------

func TestEngineDenyDestructive(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{DenyDestructive: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "wipe.disk",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "wipe.disk",
				SideEffectClass: "destructive",
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

func TestEngineDestructiveAllowedWhenNotDenied(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{DenyDestructive: false})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "wipe.disk",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "wipe.disk",
				SideEffectClass: "destructive",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// RequiresApproval manifest flag
// ---------------------------------------------------------------------------

func TestEngineManifestRequiresApproval(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.run",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:             "deploy.run",
				SideEffectClass:  "external_write",
				RequiresApproval: true,
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
// Ineligible tool
// ---------------------------------------------------------------------------

func TestEngineIneligibleToolDenied(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "broken.tool",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{Name: "broken.tool"},
			Eligibility: skill.EligibilityResult{
				Eligible: false,
				Reasons:  []string{"missing config"},
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionDeny {
		t.Fatalf("decision.Action = %q, want deny", decision.Action)
	}
	if len(decision.Reasons) == 0 || decision.Reasons[0] != "missing config" {
		t.Fatalf("decision.Reasons = %v", decision.Reasons)
	}
}

// ---------------------------------------------------------------------------
// Read-only side effect allowed
// ---------------------------------------------------------------------------

func TestEngineReadSideEffectAllowed(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{RequireApprovalForWrite: true})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "fs.read",
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "fs.read",
				SideEffectClass: "read",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow (read side effect)", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Custom safe patterns
// ---------------------------------------------------------------------------

func TestEngineCustomSafePatterns(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		SafePatterns:            []string{`^go\s+(build|test)(\s|$)`},
		RequireApprovalForWrite: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "exec.run",
		Input:    map[string]any{"command": "go test ./..."},
		Tool: &skill.BoundTool{
			Manifest: skill.ToolManifest{
				Name:            "exec.run",
				SideEffectClass: "local_write",
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow for custom safe pattern", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Skill install policy
// ---------------------------------------------------------------------------

func TestEngineSkillInstallAsk(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{SkillInstallPolicy: ""}) // default = ask
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.install",
		Input:    map[string]any{"name": "test-skill"},
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "skill.install"},
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

func TestEngineSkillInstallAuto(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{SkillInstallPolicy: "auto"})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.install",
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "skill.install"},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow", decision.Action)
	}
}

func TestEngineSkillInstallDeny(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{SkillInstallPolicy: "deny"})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.install",
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "skill.install"},
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

func TestEngineSkillEnsureRecognized(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{SkillInstallPolicy: "ask"})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "skill.ensure",
		Input:    map[string]any{"goal": "research"},
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "skill.ensure"},
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
// Dangerous tools
// ---------------------------------------------------------------------------

func TestEngineDangerousToolRequiresApproval(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DangerousTools: []string{"deploy.prod"},
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "deploy.prod",
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "deploy.prod", SideEffectClass: "read"},
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

func TestEngineNonDangerousToolNotAffected(t *testing.T) {
	t.Parallel()

	engine := NewDefaultEngine(Config{
		DangerousTools: []string{"deploy.prod"},
	})
	decision, err := engine.EvaluateTool(context.Background(), ToolContext{
		ToolName: "file.read",
		Tool: &skill.BoundTool{
			Manifest:    skill.ToolManifest{Name: "file.read", SideEffectClass: "read"},
			Eligibility: skill.EligibilityResult{Eligible: true},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision.Action = %q, want allow", decision.Action)
	}
}
