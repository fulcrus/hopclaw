package modules

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

func TestSkillModulesExposeSkillAndToolProjections(t *testing.T) {
	t.Parallel()

	pkg := &skill.SkillPackage{
		ID:     "pkg-123",
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusDegraded,
		Trust:  skill.TrustInternal,
		Prompt: skill.PromptSkill{
			Name:                   "writer",
			Description:            "Write files",
			UserInvocable:          true,
			DisableModelInvocation: true,
		},
		Source: skill.SkillSource{
			Kind: skill.SourceWorkspace,
			Root: "/workspace/skills",
			Dir:  "/workspace/skills/writer",
		},
		OpenClaw: skill.OpenClawMetadata{
			SkillKey: "dev.writer",
		},
		Issues: []skill.SkillIssue{{
			Message: "missing optional binary",
		}},
		ToolManifests: []skill.ToolManifest{{
			Name:             "writer.run",
			Description:      "Run writer",
			InputSchema:      map[string]any{"type": "object"},
			SideEffectClass:  "local_write",
			RequiresApproval: true,
			ExecutionKey:     "session:{id}",
			Timeout:          45 * time.Second,
			Runtime: skill.ToolRuntimeSpec{
				Entry: "./bin/run.sh",
				Shell: "/bin/sh",
			},
		}},
	}
	blocked := skill.BlockedSkill{
		Source: skill.SkillSource{
			Kind: skill.SourceClawHub,
			Root: "/catalog",
			Dir:  "/catalog/broken",
		},
		NameHint: "broken-skill",
		Issues: []skill.SkillIssue{{
			Message: "compile failed",
		}},
	}

	catalog := BuildCatalog(SkillModules(skill.RegistrySnapshot{
		Ordered: []*skill.SkillPackage{pkg},
		Blocked: []skill.BlockedSkill{blocked},
	}))

	projections := catalog.SkillProjections()
	if len(projections) != 2 {
		t.Fatalf("len(skill projections) = %d, want 2", len(projections))
	}
	if projections[0].Name != "broken-skill" || projections[1].Name != "writer" {
		t.Fatalf("skill projection order = %#v", projections)
	}
	if projections[1].ID != "pkg-123" {
		t.Fatalf("skill projection id = %q, want pkg-123", projections[1].ID)
	}
	if projections[1].Kind != string(skill.SkillKindExecutable) {
		t.Fatalf("skill projection kind = %q", projections[1].Kind)
	}
	if projections[1].Status != string(skill.StatusDegraded) {
		t.Fatalf("skill projection status = %q", projections[1].Status)
	}
	if projections[1].Trust != string(skill.TrustInternal) {
		t.Fatalf("skill projection trust = %q", projections[1].Trust)
	}
	if projections[1].SourceKind != string(skill.SourceWorkspace) {
		t.Fatalf("skill projection source kind = %q", projections[1].SourceKind)
	}
	if projections[1].SourceDir != "/workspace/skills/writer" {
		t.Fatalf("skill projection source dir = %q", projections[1].SourceDir)
	}
	if projections[1].ConfigKey != "dev.writer" {
		t.Fatalf("skill projection config key = %q", projections[1].ConfigKey)
	}
	if !projections[1].UserInvocable || !projections[1].DisableModelInvocation {
		t.Fatalf("skill projection flags = %#v", projections[1])
	}
	if !reflect.DeepEqual(projections[1].ToolNames, []string{"writer.run"}) {
		t.Fatalf("skill projection tools = %#v", projections[1].ToolNames)
	}
	if !projections[0].Blocked || projections[0].Status != string(skill.StatusBlocked) {
		t.Fatalf("blocked projection = %#v", projections[0])
	}

	tools := catalog.ToolProjections()
	if len(tools) != 1 {
		t.Fatalf("len(tool projections) = %d, want 1", len(tools))
	}
	if tools[0].Name != "writer.run" {
		t.Fatalf("tool projection name = %q", tools[0].Name)
	}
	if tools[0].ModuleID != projections[1].ModuleID {
		t.Fatalf("tool projection module id = %q, want %q", tools[0].ModuleID, projections[1].ModuleID)
	}
	if tools[0].Timeout != 45*time.Second {
		t.Fatalf("tool projection timeout = %s", tools[0].Timeout)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tool projection input schema = %#v", tools[0].InputSchema)
	}
}

func TestWithSkillModulesReplacesExistingSkillEntries(t *testing.T) {
	t.Parallel()

	base := BuildCatalog([]StaticModule{
		{
			ManifestValue: Manifest{
				ID:   "builtin:core",
				Name: "core",
			},
		},
		{
			ManifestValue: Manifest{
				ID:   "skill:stale",
				Name: "stale",
				Kind: ModuleKindSkill,
			},
		},
	})

	next := WithSkillModules(base, skill.RegistrySnapshot{
		Ordered: []*skill.SkillPackage{{
			ID:     "pkg-next",
			Status: skill.StatusReady,
			Kind:   skill.SkillKindPrompt,
			Prompt: skill.PromptSkill{Name: "fresh"},
			Source: skill.SkillSource{
				Kind: skill.SourceWorkspace,
				Dir:  "/workspace/skills/fresh",
			},
		}},
	})

	if next.Len() != 2 {
		t.Fatalf("next.Len() = %d, want 2", next.Len())
	}
	if _, ok := next.Find("builtin:core"); !ok {
		t.Fatal("expected builtin:core to be preserved")
	}
	if _, ok := next.Find("skill:stale"); ok {
		t.Fatal("stale skill module should be replaced")
	}
	projections := next.SkillProjections()
	if len(projections) != 1 || projections[0].Name != "fresh" {
		t.Fatalf("skill projections = %#v", projections)
	}
}
