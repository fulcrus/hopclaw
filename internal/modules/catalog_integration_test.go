package modules_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func TestBuildCatalogCombinesBuiltinAndPluginModules(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name:        "echo-pack",
			Description: "Echo pack",
			Tools: []plugin.ToolDecl{
				{Name: "echo.reply", Description: "Reply through echo"},
			},
		},
	}); err != nil {
		t.Fatalf("Register(echo-pack): %v", err)
	}

	builtinModules := toolruntime.BuiltinModules(toolruntime.BuiltinsConfig{Root: t.TempDir()})
	catalog := modules.BuildCatalog(builtinModules, manager.Modules())

	if catalog.Len() != len(builtinModules)+1 {
		t.Fatalf("catalog.Len() = %d, want %d", catalog.Len(), len(builtinModules)+1)
	}
	if _, ok := catalog.Find("builtin:core"); !ok {
		t.Fatal("catalog.Find(builtin:core) = false, want true")
	}
	if _, ok := catalog.Find("plugin:echo-pack"); !ok {
		t.Fatal("catalog.Find(plugin:echo-pack) = false, want true")
	}

	contrib := catalog.Contributions()
	if !hasTool(contrib.Tools, "fs.list") {
		t.Fatal("expected builtin tool fs.list in aggregated contributions")
	}
	if !hasTool(contrib.Tools, "echo.reply") {
		t.Fatal("expected plugin tool echo.reply in aggregated contributions")
	}
}

func TestCatalogProviderProjectionsMatchPluginManagerProviderIDs(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	for _, loaded := range []plugin.LoadedPlugin{
		{
			Dir: t.TempDir(),
			Manifest: plugin.Manifest{
				Name:   "openai",
				Format: plugin.ManifestFormatOpenClawJSON,
				Providers: map[string]plugin.ProviderDecl{
					"openai": {
						API:              "openai-completions",
						BaseURL:          "https://api.openai.com/v1",
						DefaultModel:     "gpt-4o",
						PreferUnscopedID: true,
					},
				},
			},
		},
		{
			Dir: t.TempDir(),
			Manifest: plugin.Manifest{
				Name:   "beta",
				Format: plugin.ManifestFormatOpenClawJSON,
				Providers: map[string]plugin.ProviderDecl{
					"openai": {
						API:              "openai-completions",
						BaseURL:          "https://alt-openai.example/v1",
						DefaultModel:     "gpt-4.1",
						PreferUnscopedID: true,
					},
				},
			},
		},
		{
			Dir: t.TempDir(),
			Manifest: plugin.Manifest{
				Name: "demo",
				Providers: map[string]plugin.ProviderDecl{
					"copilot": {
						API:          "github-copilot",
						BaseURL:      "https://copilot-proxy.example.test",
						APIKey:       "test-key",
						Timeout:      45 * time.Second,
						Headers:      map[string]string{"X-Plugin": "demo"},
						DefaultModel: "gpt-4o",
						EnvVars:      []string{"GITHUB_TOKEN"},
						APIKeyHint:   "Use a GitHub token or GITHUB_TOKEN.",
					},
				},
			},
		},
	} {
		if err := manager.Register(loaded); err != nil {
			t.Fatalf("Register(%s) error = %v", loaded.Manifest.Name, err)
		}
	}

	catalog := modules.BuildCatalog(manager.Modules())
	projections := catalog.ProviderProjections()

	got := make([]string, 0, len(projections))
	for _, projection := range projections {
		got = append(got, projection.Name)
		if projection.Name == "demo/copilot" {
			if projection.ModuleName != "demo" {
				t.Fatalf("demo/copilot module name = %q", projection.ModuleName)
			}
			if !reflect.DeepEqual(projection.EnvVars, []string{"GITHUB_TOKEN"}) {
				t.Fatalf("demo/copilot env vars = %#v", projection.EnvVars)
			}
			if projection.APIKeyHint != "Use a GitHub token or GITHUB_TOKEN." {
				t.Fatalf("demo/copilot api key hint = %q", projection.APIKeyHint)
			}
			if projection.APIKey != "test-key" {
				t.Fatalf("demo/copilot api key = %q", projection.APIKey)
			}
			if projection.Timeout != 45*time.Second {
				t.Fatalf("demo/copilot timeout = %s", projection.Timeout)
			}
			if !reflect.DeepEqual(projection.Headers, map[string]string{"X-Plugin": "demo"}) {
				t.Fatalf("demo/copilot headers = %#v", projection.Headers)
			}
		}
	}
	raw, err := json.Marshal(projections)
	if err != nil {
		t.Fatalf("Marshal(provider projections) error = %v", err)
	}
	text := string(raw)
	for _, forbidden := range []string{"test-key", "X-Plugin"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("provider projections leaked sensitive runtime data %q: %s", forbidden, text)
		}
	}
	module, ok := catalog.Find("plugin:demo")
	if !ok {
		t.Fatal("expected plugin:demo module in catalog")
	}
	meta := module.Contributions().Providers[0].Metadata
	for _, forbidden := range []string{"api_key", "access_key_id", "secret_key", "session_token", "headers"} {
		if _, exists := meta[forbidden]; exists {
			t.Fatalf("provider metadata leaked %q: %#v", forbidden, meta)
		}
	}

	want := make([]string, 0, len(manager.Providers()))
	for name := range manager.Providers() {
		want = append(want, name)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider projections = %v, want %v", got, want)
	}
}

func TestCatalogDirectoryProjectionsMatchPluginManagerDiscoveryRoots(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	for _, loaded := range []plugin.LoadedPlugin{
		{
			Dir: filepath.Join(t.TempDir(), "zeta"),
			Manifest: plugin.Manifest{
				Name:      "zeta",
				SkillsDir: "skills",
				HooksDir:  "hooks",
			},
		},
		{
			Dir: filepath.Join(t.TempDir(), "alpha"),
			Manifest: plugin.Manifest{
				Name:       "alpha",
				SkillsDirs: []string{"skills", "extras"},
				HooksDir:   "hooks",
			},
		},
	} {
		if err := manager.Register(loaded); err != nil {
			t.Fatalf("Register(%s) error = %v", loaded.Manifest.Name, err)
		}
	}

	catalog := modules.BuildCatalog(manager.Modules())

	skillDirs := make([]string, 0, len(catalog.SkillDirProjections()))
	for _, projection := range catalog.SkillDirProjections() {
		skillDirs = append(skillDirs, projection.Path)
	}
	if want := manager.SkillDirs(); !reflect.DeepEqual(skillDirs, want) {
		t.Fatalf("skill dir projections = %v, want %v", skillDirs, want)
	}

	hookDirs := make([]string, 0, len(catalog.HookDirProjections()))
	for _, projection := range catalog.HookDirProjections() {
		hookDirs = append(hookDirs, projection.Path)
	}
	if want := manager.HookDirs(); !reflect.DeepEqual(hookDirs, want) {
		t.Fatalf("hook dir projections = %v, want %v", hookDirs, want)
	}
}

func TestCatalogToolProjectionsIncludePluginToolContracts(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Tools: []plugin.ToolDecl{
				{
					Name:        "zeta.echo",
					Description: "Echo through zeta",
					Endpoint:    "https://example.com/zeta",
				},
				{
					Name:        "alpha.echo",
					Description: "Echo through alpha",
					Endpoint:    "https://example.com/alpha",
					Timeout:     45 * time.Second,
					InputSchema: map[string]any{"type": "object"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register(demo) error = %v", err)
	}

	projections := modules.BuildCatalog(manager.Modules()).ToolProjections()
	if len(projections) != 2 {
		t.Fatalf("tool projections = %#v", projections)
	}
	if projections[0].Name != "alpha.echo" || projections[1].Name != "zeta.echo" {
		t.Fatalf("tool projection order = %#v", projections)
	}
	if projections[0].Endpoint != "https://example.com/alpha" || projections[0].Timeout != 45*time.Second {
		t.Fatalf("alpha tool projection = %#v", projections[0])
	}
	if projections[0].InputSchema["type"] != "object" {
		t.Fatalf("alpha input schema = %#v", projections[0].InputSchema)
	}
}

func TestCatalogChannelProjectionsMatchPluginManagerChannels(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: filepath.Join(t.TempDir(), "demo"),
		Manifest: plugin.Manifest{
			Name: "demo",
			Channels: map[string]plugin.ChannelDecl{
				"alerts": {
					Type:        "webhook",
					CallbackURL: "https://example.com/alerts",
					Secret:      "demo-secret",
				},
				"chat": {
					Type:         "stdio",
					Command:      "./bin/chat-plugin",
					Args:         []string{"--json"},
					Env:          map[string]string{"MODE": "prod"},
					WorkDir:      "runtime",
					Capabilities: []string{"send", "history"},
					Config:       map[string]any{"workspace": "demo"},
					MaxRestarts:  5,
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register(demo) error = %v", err)
	}

	catalog := modules.BuildCatalog(manager.Modules())
	projections := catalog.ChannelProjections()
	if len(projections) != 2 {
		t.Fatalf("channel projections = %#v", projections)
	}
	if projections[0].Name != "demo/alerts" || projections[1].Name != "demo/chat" {
		t.Fatalf("channel projection order = %#v", projections)
	}
	if projections[0].Type != "webhook" || projections[0].CallbackURL != "https://example.com/alerts" || projections[0].Secret != "demo-secret" {
		t.Fatalf("alerts channel projection = %#v", projections[0])
	}
	if projections[1].Type != "stdio" || projections[1].Command != "./bin/chat-plugin" || projections[1].MaxRestarts != 5 {
		t.Fatalf("chat channel projection = %#v", projections[1])
	}
	if projections[1].ModuleDir == "" {
		t.Fatalf("chat channel module dir = %q, want non-empty", projections[1].ModuleDir)
	}
	if !reflect.DeepEqual(projections[1].Args, []string{"--json"}) {
		t.Fatalf("chat channel args = %#v", projections[1].Args)
	}
	if !reflect.DeepEqual(projections[1].Env, map[string]string{"MODE": "prod"}) {
		t.Fatalf("chat channel env = %#v", projections[1].Env)
	}
	if !reflect.DeepEqual(projections[1].Capabilities, []string{"history", "send"}) {
		t.Fatalf("chat channel capabilities = %#v", projections[1].Capabilities)
	}
	if projections[1].Config["workspace"] != "demo" {
		t.Fatalf("chat channel config = %#v", projections[1].Config)
	}
	raw, err := json.Marshal(projections)
	if err != nil {
		t.Fatalf("Marshal(channel projections) error = %v", err)
	}
	text := string(raw)
	for _, forbidden := range []string{"demo-secret", "\"MODE\":\"prod\"", "\"workspace\":\"demo\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("channel projections leaked sensitive runtime data %q: %s", forbidden, text)
		}
	}
	module, ok := catalog.Find("plugin:demo")
	if !ok {
		t.Fatal("expected plugin:demo module in catalog")
	}
	channelMeta := module.Contributions().Channels[0].Metadata
	for _, forbidden := range []string{"secret", "env", "config"} {
		if _, exists := channelMeta[forbidden]; exists {
			t.Fatalf("channel metadata leaked %q: %#v", forbidden, channelMeta)
		}
	}
}

func TestCatalogAgentProjectionsIncludePluginAgentContracts(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Agents: map[string]plugin.AgentDecl{
				"ops": {
					Description:  "Ops preset",
					SystemPrompt: "You are the ops agent.",
					Model:        "gpt-5",
					Tools:        []string{"fs.read"},
					Skills:       []string{"summarize"},
					MaxTokens:    4096,
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register(demo) error = %v", err)
	}

	projections := modules.BuildCatalog(manager.Modules()).AgentProjections()
	if len(projections) != 1 {
		t.Fatalf("agent projections = %#v", projections)
	}
	if projections[0].Name != "demo/ops" || projections[0].SystemPrompt != "You are the ops agent." {
		t.Fatalf("agent projection = %#v", projections[0])
	}
	if !reflect.DeepEqual(projections[0].Tools, []string{"fs.read"}) {
		t.Fatalf("agent projection tools = %#v", projections[0].Tools)
	}
	if !reflect.DeepEqual(projections[0].Skills, []string{"summarize"}) {
		t.Fatalf("agent projection skills = %#v", projections[0].Skills)
	}
	if projections[0].MaxTokens != 4096 {
		t.Fatalf("agent projection max tokens = %d", projections[0].MaxTokens)
	}
}

func TestCatalogMCPServerProjectionsIncludePluginRuntimeContracts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: filepath.Join(root, "demo"),
		Manifest: plugin.Manifest{
			Name: "demo",
			MCPServers: map[string]plugin.MCPServerDecl{
				"worker": {
					Description: "Worker MCP server",
					ServerConfig: plugin.ServerConfig{
						Command: "./bin/mcp-worker",
						Args:    []string{"--json"},
						Env:     map[string]string{"MODE": "prod"},
						WorkDir: "runtime",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register(demo) error = %v", err)
	}

	projections := modules.BuildCatalog(manager.Modules()).MCPServerProjections()
	if len(projections) != 1 {
		t.Fatalf("mcp projections = %#v", projections)
	}
	if projections[0].Name != "demo/worker" {
		t.Fatalf("mcp projection name = %q", projections[0].Name)
	}
	if projections[0].RuntimeName() != "demo.worker" {
		t.Fatalf("mcp runtime name = %q", projections[0].RuntimeName())
	}
	if projections[0].ResolvedCommand() != filepath.Join(root, "demo", "bin", "mcp-worker") {
		t.Fatalf("mcp resolved command = %q", projections[0].ResolvedCommand())
	}
	if projections[0].ResolvedWorkDir() != filepath.Join(root, "demo", "runtime") {
		t.Fatalf("mcp resolved workdir = %q", projections[0].ResolvedWorkDir())
	}
	if !reflect.DeepEqual(projections[0].Args, []string{"--json"}) {
		t.Fatalf("mcp args = %#v", projections[0].Args)
	}
	if !reflect.DeepEqual(projections[0].Env, map[string]string{"MODE": "prod"}) {
		t.Fatalf("mcp env = %#v", projections[0].Env)
	}
	raw, err := json.Marshal(projections)
	if err != nil {
		t.Fatalf("Marshal(mcp projections) error = %v", err)
	}
	if strings.Contains(string(raw), "\"MODE\":\"prod\"") {
		t.Fatalf("mcp projections leaked runtime env: %s", string(raw))
	}
}

func hasTool(list []modules.Component, name string) bool {
	for _, item := range list {
		if item.Name == name {
			return true
		}
	}
	return false
}
