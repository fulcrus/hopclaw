package modules

import "testing"

func TestBuildCatalogSortsAndDeduplicatesModules(t *testing.T) {
	t.Parallel()

	core := StaticModule{ManifestValue: Manifest{ID: "builtin:core", Name: "core"}}
	alpha := StaticModule{ManifestValue: Manifest{ID: "plugin:alpha", Name: "alpha"}}
	duplicateCore := StaticModule{ManifestValue: Manifest{ID: "builtin:core", Name: "core-duplicate"}}

	catalog := BuildCatalog(
		[]StaticModule{alpha},
		[]StaticModule{duplicateCore, core},
	)

	if catalog.Len() != 2 {
		t.Fatalf("catalog.Len() = %d, want 2", catalog.Len())
	}
	modulesList := catalog.Modules()
	if modulesList[0].Manifest().ID != "builtin:core" || modulesList[1].Manifest().ID != "plugin:alpha" {
		t.Fatalf("catalog order = %q, %q", modulesList[0].Manifest().ID, modulesList[1].Manifest().ID)
	}
	if modulesList[0].Manifest().Name != "core-duplicate" {
		t.Fatalf("dedupe winner name = %q, want %q", modulesList[0].Manifest().Name, "core-duplicate")
	}
}

func TestCatalogFindManifestsAndContributions(t *testing.T) {
	t.Parallel()

	builtin := StaticModule{
		ManifestValue: Manifest{
			ID:     "builtin:core",
			Name:   "core",
			Source: SourceBuiltin,
		},
		ContributionsValue: Contributions{
			Tools: []Component{{Kind: ComponentKindTool, Name: "fs.list"}},
		},
	}
	plugin := StaticModule{
		ManifestValue: Manifest{
			ID:     "plugin:echo",
			Name:   "echo",
			Source: SourcePlugin,
		},
		ContributionsValue: Contributions{
			Channels:        []Component{{Kind: ComponentKindChannel, Name: "echo"}},
			Tools:           []Component{{Kind: ComponentKindTool, Name: "echo.reply"}},
			ConfigContracts: []Component{{Kind: ComponentKindConfig, Name: "compat"}},
			RuntimeBridges:  []Component{{Kind: ComponentKindRuntimeBridge, Name: "openclaw-native-runtime"}},
		},
	}

	catalog := BuildCatalog([]StaticModule{plugin}, []StaticModule{builtin})

	manifests := catalog.Manifests()
	if len(manifests) != 2 {
		t.Fatalf("len(manifests) = %d, want 2", len(manifests))
	}
	if manifests[0].ID != "builtin:core" || manifests[1].ID != "plugin:echo" {
		t.Fatalf("manifest order = %q, %q", manifests[0].ID, manifests[1].ID)
	}

	found, ok := catalog.Find("PLUGIN:ECHO")
	if !ok {
		t.Fatal("catalog.Find(plugin:echo) = false, want true")
	}
	if found.Manifest().Name != "echo" {
		t.Fatalf("found module name = %q, want %q", found.Manifest().Name, "echo")
	}

	contrib := catalog.Contributions()
	if len(contrib.Channels) != 1 || contrib.Channels[0].Name != "echo" {
		t.Fatalf("channel contributions = %#v", contrib.Channels)
	}
	if len(contrib.Tools) != 2 || contrib.Tools[0].Name != "echo.reply" || contrib.Tools[1].Name != "fs.list" {
		t.Fatalf("tool contributions = %#v", contrib.Tools)
	}
	if len(contrib.ConfigContracts) != 1 || contrib.ConfigContracts[0].Name != "compat" {
		t.Fatalf("config contributions = %#v", contrib.ConfigContracts)
	}
	if len(contrib.RuntimeBridges) != 1 || contrib.RuntimeBridges[0].Name != "openclaw-native-runtime" {
		t.Fatalf("runtime bridge contributions = %#v", contrib.RuntimeBridges)
	}
}
