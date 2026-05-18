package modules

import (
	"context"
	"reflect"
	"testing"
)

func TestContributionsNormalizedDeduplicatesAndSorts(t *testing.T) {
	input := Contributions{
		Tools: []Component{
			{Kind: ComponentKindTool, Name: "zeta"},
			{Name: "alpha"},
			{Kind: ComponentKindTool, Name: "alpha"},
		},
		SkillDirs: []Component{
			{Path: "./skills"},
			{Path: "./extras"},
			{Path: "./skills"},
		},
	}

	got := input.Normalized()
	if len(got.Tools) != 2 {
		t.Fatalf("len(got.Tools) = %d, want 2", len(got.Tools))
	}
	if got.Tools[0].Name != "alpha" || got.Tools[1].Name != "zeta" {
		t.Fatalf("got tool order = %#v", got.Tools)
	}
	if len(got.SkillDirs) != 2 {
		t.Fatalf("len(got.SkillDirs) = %d, want 2", len(got.SkillDirs))
	}
	if got.SkillDirs[0].Kind != ComponentKindSkillsDir {
		t.Fatalf("skill dir kind = %q, want %q", got.SkillDirs[0].Kind, ComponentKindSkillsDir)
	}
	if got.SkillDirs[0].Path != "skills" || got.SkillDirs[1].Path != "extras" {
		t.Fatalf("skill dir order = %#v", got.SkillDirs)
	}
}

func TestContributionsComponentsStableOrdering(t *testing.T) {
	input := Contributions{
		Channels: []Component{{Name: "slack", Kind: ComponentKindChannel}},
		Tools: []Component{
			{Name: "fs.read", Kind: ComponentKindTool},
			{Name: "fs.list", Kind: ComponentKindTool},
		},
	}

	got := input.Components()
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"slack", "fs.list", "fs.read"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("component order = %#v, want %#v", names, want)
	}
}

func TestContributionsNormalizeCompatibilityComponents(t *testing.T) {
	input := Contributions{
		ConfigContracts: []Component{
			{Name: "compat"},
			{Name: "compat"},
		},
		RuntimeBridges: []Component{
			{Name: "openclaw-native-runtime"},
		},
	}

	got := input.Normalized()
	if len(got.ConfigContracts) != 1 || got.ConfigContracts[0].Kind != ComponentKindConfig {
		t.Fatalf("config contracts = %#v", got.ConfigContracts)
	}
	if len(got.RuntimeBridges) != 1 || got.RuntimeBridges[0].Kind != ComponentKindRuntimeBridge {
		t.Fatalf("runtime bridges = %#v", got.RuntimeBridges)
	}
	if got.Count() != 2 {
		t.Fatalf("count = %d, want 2", got.Count())
	}
}

func TestStaticModuleDefaults(t *testing.T) {
	module := StaticModule{
		ManifestValue: Manifest{Name: "official-pack"},
		ContributionsValue: Contributions{
			Tools: []Component{{Name: "fs.read", Kind: ComponentKindTool}},
		},
	}

	manifest := module.Manifest()
	if manifest.ID != "official-pack" {
		t.Fatalf("manifest.ID = %q, want %q", manifest.ID, "official-pack")
	}
	if manifest.Kind != "capability_pack" {
		t.Fatalf("manifest.Kind = %q, want capability_pack", manifest.Kind)
	}
	if manifest.Level != ModuleLevelMinimal {
		t.Fatalf("manifest.Level = %q, want %q", manifest.Level, ModuleLevelMinimal)
	}

	plan := module.PlanReload()
	if plan.Action != ReloadActionHot {
		t.Fatalf("plan.Action = %q, want %q", plan.Action, ReloadActionHot)
	}

	health := module.Health(context.Background())
	if health.Status != HealthUnknown {
		t.Fatalf("health.Status = %q, want %q", health.Status, HealthUnknown)
	}
}
