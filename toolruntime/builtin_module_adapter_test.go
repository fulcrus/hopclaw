package toolruntime

import (
	"testing"

	"github.com/fulcrus/hopclaw/internal/modules"
)

func TestBuiltinModulesExposeBuiltinCategoryCatalog(t *testing.T) {
	t.Parallel()

	modulesList := BuiltinModules(BuiltinsConfig{Root: t.TempDir()})
	if len(modulesList) != 33 {
		t.Fatalf("len(modulesList) = %d, want 33", len(modulesList))
	}
	for _, item := range modulesList {
		if item.Manifest().Name == "gateway" {
			t.Fatal("gateway should no longer be exposed as a builtin module")
		}
	}

	core := findBuiltinModuleByName(t, modulesList, "core")
	manifest := core.Manifest()
	if manifest.ID != "builtin:core" {
		t.Fatalf("manifest.ID = %q, want %q", manifest.ID, "builtin:core")
	}
	if manifest.Source != modules.SourceBuiltin {
		t.Fatalf("manifest.Source = %q, want %q", manifest.Source, modules.SourceBuiltin)
	}
	if manifest.Delivery != modules.DeliveryEmbedded {
		t.Fatalf("manifest.Delivery = %q, want %q", manifest.Delivery, modules.DeliveryEmbedded)
	}
	if manifest.Description == "" {
		t.Fatal("expected builtin module description")
	}
}

func TestBuiltinModulesExposeToolContributionsAndMetadata(t *testing.T) {
	t.Parallel()

	cfg := BuiltinsConfig{Root: t.TempDir()}
	core := findBuiltinModuleByName(t, BuiltinModules(cfg), "core")
	contrib := core.Contributions()
	if len(contrib.Tools) != len(coreToolDefs(cfg)) {
		t.Fatalf("len(contrib.Tools) = %d, want %d", len(contrib.Tools), len(coreToolDefs(cfg)))
	}

	fsList := findModuleComponentByName(t, contrib.Tools, "fs.list")
	if fsList.Kind != modules.ComponentKindTool {
		t.Fatalf("fs.list kind = %q, want %q", fsList.Kind, modules.ComponentKindTool)
	}
	if fsList.Metadata["module_id"] != "builtin:core" {
		t.Fatalf("fs.list module_id = %#v, want %q", fsList.Metadata["module_id"], "builtin:core")
	}
	if fsList.Metadata["category"] != "core" {
		t.Fatalf("fs.list category = %#v, want %q", fsList.Metadata["category"], "core")
	}
	if fsList.Metadata["side_effect_class"] != "read" {
		t.Fatalf("fs.list side_effect_class = %#v, want %q", fsList.Metadata["side_effect_class"], "read")
	}
	if fsList.Metadata["idempotent"] != true {
		t.Fatalf("fs.list idempotent = %#v, want true", fsList.Metadata["idempotent"])
	}
	if _, ok := fsList.Metadata["hidden"]; ok {
		t.Fatalf("fs.list hidden = %#v, want absent", fsList.Metadata["hidden"])
	}

	fsDiff := findModuleComponentByName(t, contrib.Tools, "fs.diff")
	if fsDiff.Metadata["hidden"] != true {
		t.Fatalf("fs.diff hidden = %#v, want true", fsDiff.Metadata["hidden"])
	}
}

func TestBuiltinsModulesUsesRuntimeConfigSnapshot(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	modulesList := builtins.Modules()
	if len(modulesList) != len(builtinCategoryCatalog()) {
		t.Fatalf("len(modulesList) = %d, want %d", len(modulesList), len(builtinCategoryCatalog()))
	}

	netModule := findBuiltinModuleByName(t, modulesList, "net")
	netServe := findModuleComponentByName(t, netModule.Contributions().Tools, "net.serve")
	if netServe.Metadata["hidden"] != true {
		t.Fatalf("net.serve hidden = %#v, want true", netServe.Metadata["hidden"])
	}
}

func findBuiltinModuleByName(t *testing.T, list []modules.StaticModule, name string) modules.StaticModule {
	t.Helper()

	for _, item := range list {
		if item.Manifest().Name == name {
			return item
		}
	}
	t.Fatalf("builtin module %q not found", name)
	return modules.StaticModule{}
}

func findModuleComponentByName(t *testing.T, list []modules.Component, name string) modules.Component {
	t.Helper()

	for _, item := range list {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("module component %q not found", name)
	return modules.Component{}
}
