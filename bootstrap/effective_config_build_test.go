package bootstrap

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
)

func TestPrepareToolRuntimeLockedUsesModuleCatalogProjections(t *testing.T) {
	t.Parallel()

	moduleCatalog := modules.NewStore(modules.BuildCatalog([]modules.StaticModule{{
		ManifestValue: modules.Manifest{
			ID:       "plugin:demo-pack",
			Name:     "demo-pack",
			Source:   modules.SourcePlugin,
			Delivery: modules.DeliveryManifest,
			Level:    modules.ModuleLevelDeclared,
		},
		ContributionsValue: modules.Contributions{
			Tools: []modules.Component{{
				Kind:        modules.ComponentKindTool,
				Name:        "demo.echo",
				Description: "echo through projection",
				Metadata: map[string]any{
					"endpoint": "https://example.com/tool",
				},
			}},
		},
	}}))

	app := &App{
		AppRuntimeState: AppRuntimeState{
			ModuleCatalog: moduleCatalog,
		},
		appInternalState: appInternalState{
			customTools: true,
		},
	}

	prepared, err := app.prepareToolRuntimeLocked(context.Background(), config.Config{}, nil)
	if err != nil {
		t.Fatalf("prepareToolRuntimeLocked() error = %v", err)
	}
	if prepared == nil || prepared.runtime == nil {
		t.Fatalf("prepareToolRuntimeLocked() = %#v", prepared)
	}

	provider, ok := prepared.runtime.(agent.ToolDefinitionProvider)
	if !ok {
		t.Fatalf("runtime %#v does not expose tool definitions", prepared.runtime)
	}
	defs := provider.ToolDefinitions(nil)
	if !hasToolDefinition(defs, "demo.echo") {
		t.Fatalf("tool definitions = %#v", defs)
	}

	resolver, ok := prepared.runtime.(agent.ToolResolver)
	if !ok {
		t.Fatalf("runtime %#v does not resolve tools", prepared.runtime)
	}
	resolved, ok := resolver.ResolveTool(nil, "demo.echo")
	if !ok {
		t.Fatalf("ResolveTool(demo.echo) = false")
	}
	if resolved.Descriptor.Source != "external" {
		t.Fatalf("resolved source = %q, want external", resolved.Descriptor.Source)
	}
}

func hasToolDefinition(defs []agent.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
