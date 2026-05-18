package toolspec

import (
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

func TestNormalizeDefinitionTrimsAndClones(t *testing.T) {
	t.Parallel()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	def := NormalizeDefinition(ToolDefinition{
		Name:               "  tools.echo  ",
		Description:        "  Echo text  ",
		InputSchema:        inputSchema,
		EligibilityReasons: []string{"missing capability"},
	})

	if def.Name != "tools.echo" {
		t.Fatalf("def.Name = %q, want tools.echo", def.Name)
	}
	if def.Description != "Echo text" {
		t.Fatalf("def.Description = %q, want Echo text", def.Description)
	}
	if def.Availability.Status != AvailabilityBlocked {
		t.Fatalf("def.Availability.Status = %q, want %q", def.Availability.Status, AvailabilityBlocked)
	}

	inputSchema["type"] = "broken"
	if def.InputSchema["type"] != "object" {
		t.Fatalf("def.InputSchema mutated with source map: %#v", def.InputSchema)
	}
}

func TestNormalizeResolvedToolMergesManifestAndEligibility(t *testing.T) {
	t.Parallel()

	tool := NormalizeResolvedTool(&ResolvedTool{
		Descriptor: ToolDefinition{
			Eligible: false,
		},
		Manifest: skill.ToolManifest{
			Name:             "skill.translate",
			Description:      "Translate text",
			SideEffectClass:  "none",
			Idempotent:       true,
			RequiresApproval: false,
			ExecutionKey:     "skill.translate",
		},
		Eligibility: skill.EligibilityResult{
			Eligible: false,
			Reasons:  []string{"needs package"},
		},
		Package: &skill.SkillPackage{
			Trust: skill.TrustVerified,
			Source: skill.SkillSource{
				Dir: "/tmp/translate-skill",
			},
		},
		ExecutorRef: "skill:translate",
	})

	if tool == nil {
		t.Fatal("NormalizeResolvedTool() returned nil")
	}
	if tool.Descriptor.Name != "skill.translate" {
		t.Fatalf("tool.Descriptor.Name = %q, want skill.translate", tool.Descriptor.Name)
	}
	if tool.Descriptor.Description != "Translate text" {
		t.Fatalf("tool.Descriptor.Description = %q, want Translate text", tool.Descriptor.Description)
	}
	if tool.Descriptor.Source != "skill" {
		t.Fatalf("tool.Descriptor.Source = %q, want skill", tool.Descriptor.Source)
	}
	if tool.Descriptor.SourceRef != "/tmp/translate-skill" {
		t.Fatalf("tool.Descriptor.SourceRef = %q, want /tmp/translate-skill", tool.Descriptor.SourceRef)
	}
	if len(tool.Descriptor.EligibilityReasons) != 1 || tool.Descriptor.EligibilityReasons[0] != "needs package" {
		t.Fatalf("tool.Descriptor.EligibilityReasons = %#v", tool.Descriptor.EligibilityReasons)
	}
}
