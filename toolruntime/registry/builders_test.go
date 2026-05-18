package registry

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	registrycap "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestBuildBaseReturnsRegistryOwnedProviders(t *testing.T) {
	t.Parallel()

	result, err := BuildBase(context.Background(), config.Config{
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled: boolPtr(true),
				Root:    t.TempDir(),
			},
			LocalExec: config.LocalExecConfig{
				Enabled: boolPtr(true),
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BuildBase() error = %v", err)
	}
	if result.Executor == nil {
		t.Fatal("expected composed base executor")
	}
	if result.Builtins == nil {
		t.Fatal("expected builtins provider instance")
	}
	if result.Layer2 == nil {
		t.Fatal("expected layer2 provider instance")
	}

	kinds := ProviderKinds(result.BuildResult)
	if len(kinds) < 5 {
		t.Fatalf("provider kinds = %#v, want builtin/services/layer2/operator/localexec", kinds)
	}
	if kinds[0] != "builtin" || kinds[1] != "services" || kinds[2] != "layer2" || kinds[3] != "operator" || kinds[4] != "localexec" {
		t.Fatalf("provider kinds = %#v", kinds)
	}
	defs := result.Executor.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	searchWeb, ok := findDefinition(defs, "search.web")
	if !ok {
		t.Fatalf("search.web missing from tool definitions: %#v", defs)
	}
	if searchWeb.Source != "services" {
		t.Fatalf("search.web source = %q, want services", searchWeb.Source)
	}
	gatewayStatus, ok := findDefinition(defs, "gateway.status")
	if !ok {
		t.Fatalf("gateway.status missing from tool definitions: %#v", defs)
	}
	if gatewayStatus.Source != "operator" {
		t.Fatalf("gateway.status source = %q, want operator", gatewayStatus.Source)
	}
	if _, ok := result.Layer2.ResolveTool(nil, "search.web"); ok {
		t.Fatal("layer2 should not expose search.web when services provider is assembled separately")
	}
	if _, ok := result.Layer2.ResolveTool(nil, "calendar.list_events"); ok {
		t.Fatal("layer2 should not expose calendar.list_events when services provider is assembled separately")
	}
}

func TestBuildRuntimeIncludesPluginExternalProvider(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	if err := plugins.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Tools: []plugin.ToolDecl{{
				Name:        "demo.echo",
				Description: "echo",
				Endpoint:    "https://example.com/tool",
			}},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	moduleCatalog := modules.NewStore(modules.BuildCatalog(plugins.Modules()))
	result, err := BuildRuntime(context.Background(), stubToolExecutor{}, nil, config.Config{}, moduleCatalog, nil)
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	if result.Executor == nil {
		t.Fatal("expected runtime executor")
	}

	kinds := ProviderKinds(result)
	if len(kinds) != 2 {
		t.Fatalf("provider kinds len = %d, want 2 (%#v)", len(kinds), kinds)
	}
	if kinds[0] != "base" || kinds[1] != "external" {
		t.Fatalf("provider kinds = %#v", kinds)
	}
	defs := result.Executor.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	def, ok := findDefinition(defs, "demo.echo")
	if !ok {
		t.Fatalf("tool definitions = %#v", defs)
	}
	if def.Source != "external" {
		t.Fatalf("demo.echo source = %q, want external", def.Source)
	}
}

func TestBuildRuntimePrefersConfigExternalToolModulesWhenPresent(t *testing.T) {
	t.Parallel()

	moduleCatalog := modules.NewStore(modules.BuildCatalog([]modules.StaticModule{{
		ManifestValue: modules.Manifest{
			ID:       "config:tool:web.lookup",
			Name:     "web.lookup",
			Source:   modules.SourceExternal,
			Delivery: modules.DeliveryWebhook,
			Level:    modules.ModuleLevelManaged,
		},
		ContributionsValue: modules.Contributions{
			Tools: []modules.Component{{
				Kind:        modules.ComponentKindTool,
				Name:        "web.lookup",
				Description: "Lookup URLs",
				Metadata: map[string]any{
					"endpoint": "https://projection.example/lookup",
				},
			}},
		},
	}}))

	result, err := BuildRuntime(context.Background(), nil, nil, config.Config{
		Tools: config.ToolsConfig{
			External: []config.ExternalToolConfig{{
				Name:     "web.lookup",
				Endpoint: "https://config.example/lookup",
			}},
		},
	}, moduleCatalog, nil)
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}

	defs := result.Executor.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	def, ok := findDefinition(defs, "web.lookup")
	if !ok {
		t.Fatalf("tool definitions = %#v", defs)
	}
	if def.SourceRef != "https://projection.example/lookup" {
		t.Fatalf("web.lookup source_ref = %q, want projection endpoint", def.SourceRef)
	}
}

func TestBuildRuntimeOrdersMCPAfterExternalProviders(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	if err := plugins.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Tools: []plugin.ToolDecl{{
				Name:        "demo.echo",
				Description: "echo",
				Endpoint:    "https://example.com/tool",
			}},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	moduleCatalog := modules.NewStore(modules.BuildCatalog(plugins.Modules()))
	result, err := BuildRuntime(context.Background(), stubToolExecutor{}, nil, config.Config{}, moduleCatalog, stubToolExecutor{})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}

	kinds := ProviderKinds(result)
	want := []string{"base", "external", "mcp"}
	if len(kinds) != len(want) {
		t.Fatalf("provider kinds len = %d, want %d (%#v)", len(kinds), len(want), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("provider kinds = %#v, want %#v", kinds, want)
		}
	}
}

func TestBuildBasePrefersCapabilityProjectionForBrowserTools(t *testing.T) {
	t.Parallel()

	caps := registrycap.New()
	if err := caps.Register(stubSessionCapability{
		manifest: captypes.Manifest{
			Name:          "browser",
			Kind:          captypes.KindSession,
			SessionScoped: true,
			Operations: []captypes.OperationSpec{
				{Name: "create_session", Description: "Open browser", SideEffectClass: "external_write"},
				{Name: "navigate", Description: "Navigate", SideEffectClass: "external_write"},
				{Name: "wait_for", Description: "Wait", SideEffectClass: "read"},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := BuildBase(context.Background(), config.Config{
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled: boolPtr(true),
				Root:    t.TempDir(),
			},
		},
	}, caps, nil)
	if err != nil {
		t.Fatalf("BuildBase() error = %v", err)
	}

	defs := result.Executor.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	openDef, ok := findDefinition(defs, "browser.open")
	if !ok {
		t.Fatalf("browser.open missing from tools: %#v", defs)
	}
	if openDef.Source != "capability" {
		t.Fatalf("browser.open source = %q, want capability", openDef.Source)
	}
	if _, ok := findDefinition(defs, "browser.create_session"); ok {
		t.Fatal("legacy browser.create_session should not be listed in tool definitions")
	}
	waitDef, ok := findDefinition(defs, "browser.wait")
	if !ok {
		t.Fatalf("browser.wait missing from tools: %#v", defs)
	}
	if waitDef.Source != "capability" {
		t.Fatalf("browser.wait source = %q, want capability", waitDef.Source)
	}
}

func TestBuildBasePrefersDesktopCapabilityBridgeForNodesTools(t *testing.T) {
	t.Parallel()

	caps := registrycap.New()
	if err := caps.Register(stubSessionCapability{
		manifest: captypes.Manifest{
			Name:          "desktop",
			Kind:          captypes.KindSession,
			SessionScoped: true,
			Operations: []captypes.OperationSpec{
				{Name: "screenshot", Description: "Capture desktop", SideEffectClass: "read"},
				{Name: "screen_record", Description: "Record desktop", SideEffectClass: "local_write"},
				{Name: "clipboard_read", Description: "Read clipboard", SideEffectClass: "read"},
				{Name: "clipboard_write", Description: "Write clipboard", SideEffectClass: "external_write"},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := BuildBase(context.Background(), config.Config{
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled: boolPtr(true),
				Root:    t.TempDir(),
			},
		},
	}, caps, nil)
	if err != nil {
		t.Fatalf("BuildBase() error = %v", err)
	}

	defs := result.Executor.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	screenCapture, ok := findDefinition(defs, "nodes.screen_capture")
	if !ok {
		t.Fatalf("nodes.screen_capture missing from tools: %#v", defs)
	}
	if screenCapture.Source != "capability_bridge" {
		t.Fatalf("nodes.screen_capture source = %q, want capability_bridge", screenCapture.Source)
	}
	clipboardRead, ok := findDefinition(defs, "nodes.clipboard_read")
	if !ok {
		t.Fatalf("nodes.clipboard_read missing from tools: %#v", defs)
	}
	if clipboardRead.Source != "capability_bridge" {
		t.Fatalf("nodes.clipboard_read source = %q, want capability_bridge", clipboardRead.Source)
	}
}

type stubToolExecutor struct{}

func (stubToolExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}

type stubSessionCapability struct {
	manifest captypes.Manifest
}

func (s stubSessionCapability) Manifest() captypes.Manifest { return s.manifest }

func (s stubSessionCapability) Health(context.Context) captypes.Health {
	return captypes.Health{Status: captypes.StatusReady}
}

func (s stubSessionCapability) Invoke(context.Context, captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}

func (s stubSessionCapability) OpenSession(context.Context, map[string]any) (*captypes.SessionHandle, error) {
	return &captypes.SessionHandle{ID: "cap-1", Capability: s.manifest.Name}, nil
}

func (s stubSessionCapability) CloseSession(context.Context, string) error { return nil }

func findDefinition(defs []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	for _, def := range defs {
		if def.Name == name {
			return def, true
		}
	}
	return agent.ToolDefinition{}, false
}

func boolPtr(v bool) *bool {
	return &v
}
