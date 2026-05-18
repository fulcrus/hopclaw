package modules

import (
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

func TestStoreSnapshotsRemainIsolatedAcrossSwaps(t *testing.T) {
	t.Parallel()

	store := NewStore(BuildCatalog([]StaticModule{
		{ManifestValue: Manifest{ID: "builtin:core", Name: "core"}},
	}))

	first := store.Snapshot()
	store.Swap(BuildCatalog([]StaticModule{
		{ManifestValue: Manifest{ID: "plugin:echo", Name: "echo"}},
	}))

	if first.Len() != 1 {
		t.Fatalf("first.Len() = %d, want 1", first.Len())
	}
	if _, ok := first.Find("builtin:core"); !ok {
		t.Fatal("first snapshot lost builtin:core")
	}
	if _, ok := store.Find("plugin:echo"); !ok {
		t.Fatal("store.Find(plugin:echo) = false, want true")
	}
	if _, ok := store.Find("builtin:core"); ok {
		t.Fatal("store.Find(builtin:core) = true, want false after swap")
	}
}

func TestStoreVersionChangesAcrossSwaps(t *testing.T) {
	t.Parallel()

	store := NewStore(BuildCatalog([]StaticModule{
		{ManifestValue: Manifest{ID: "builtin:core", Name: "core"}},
	}))
	first := store.SnapshotState()
	if first.Version == "" {
		t.Fatal("expected initial module catalog version")
	}

	store.Swap(BuildCatalog([]StaticModule{
		{ManifestValue: Manifest{ID: "plugin:echo", Name: "echo"}},
	}))
	second := store.SnapshotState()
	if second.Version == "" {
		t.Fatal("expected updated module catalog version")
	}
	if first.Version == second.Version {
		t.Fatalf("module catalog version did not change: %q", first.Version)
	}
}

func TestStoreProjectionMethodsExposeSnapshotVersionAndLevel(t *testing.T) {
	t.Parallel()

	store := NewStore(BuildCatalog([]StaticModule{{
		ManifestValue: Manifest{
			ID:       "plugin:demo",
			Name:     "demo",
			Source:   SourcePlugin,
			Delivery: DeliveryManifest,
			Level:    ModuleLevelDeclared,
		},
		ContributionsValue: Contributions{
			Tools: []Component{{Kind: ComponentKindTool, Name: "demo.echo"}},
		},
	}}))

	version := store.ProjectionVersion()
	if version == "" {
		t.Fatal("expected projection version")
	}

	tools := store.ToolProjections()
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	if tools[0].ProjectionVersion != version {
		t.Fatalf("projection version = %q, want %q", tools[0].ProjectionVersion, version)
	}
	if tools[0].Level != ModuleLevelDeclared {
		t.Fatalf("tool level = %q, want %q", tools[0].Level, ModuleLevelDeclared)
	}
}

func TestStoreSwapWithUsesLatestSnapshot(t *testing.T) {
	t.Parallel()

	store := NewStore(BuildCatalog([]StaticModule{
		{ManifestValue: Manifest{ID: "builtin:core", Name: "core"}},
	}))
	stale := store.Snapshot()

	store.Swap(BuildCatalog(stale.Modules(), []StaticModule{
		{ManifestValue: Manifest{ID: "plugin:echo", Name: "echo"}},
	}))

	store.SwapWith(func(current Catalog) Catalog {
		if _, ok := current.Find("plugin:echo"); !ok {
			t.Fatal("SwapWith() should apply on the latest snapshot")
		}
		return WithSkillModules(current, skill.RegistrySnapshot{
			Ordered: []*skill.SkillPackage{{
				ID:     "pkg-writer",
				Status: skill.StatusReady,
				Kind:   skill.SkillKindPrompt,
				Prompt: skill.PromptSkill{Name: "writer"},
				Source: skill.SkillSource{
					Kind: skill.SourceWorkspace,
					Dir:  "/workspace/skills/writer",
				},
			}},
		})
	})

	if _, ok := store.Find("builtin:core"); !ok {
		t.Fatal("builtin:core should be preserved")
	}
	if _, ok := store.Find("plugin:echo"); !ok {
		t.Fatal("plugin:echo should be preserved")
	}
	projections := store.SkillProjections()
	if len(projections) != 1 || projections[0].Name != "writer" {
		t.Fatalf("skill projections = %#v", projections)
	}
}
