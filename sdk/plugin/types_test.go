package plugin

import (
	"testing"
	"time"
)

func TestExportedTypesAreUsable(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Name:        "demo",
		Version:     "1.2.3",
		Description: "Demo plugin",
		Providers: map[string]ProviderDecl{
			"demo-provider": {
				API:          "openai-completions",
				BaseURL:      "https://api.example.com/v1",
				DefaultModel: "demo-model",
				Timeout:      5 * time.Second,
				Headers: map[string]string{
					"X-Test": "yes",
				},
				EnvVars:    []string{"DEMO_API_KEY"},
				APIKeyHint: "Set DEMO_API_KEY",
			},
		},
		Channels: map[string]ChannelDecl{
			"demo-channel": {
				Type:         "stdio",
				Command:      "./demo-channel",
				Args:         []string{"serve"},
				Env:          map[string]string{"MODE": "test"},
				Capabilities: []string{"send"},
				Config:       map[string]any{"color": "blue"},
				MaxRestarts:  3,
			},
		},
		Tools: []ToolDecl{{
			Name:        "demo.tool",
			Description: "Demo HTTP tool",
			Endpoint:    "https://tools.example.com/demo",
			Timeout:     3 * time.Second,
			InputSchema: map[string]any{"type": "object"},
		}},
		MCPServers: map[string]MCPServerDecl{
			"browser": {
				Description: "Browser bridge",
				ServerConfig: ServerConfig{
					Command: "npx",
					Args:    []string{"@modelcontextprotocol/server-playwright"},
					Env:     map[string]string{"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": "1"},
					WorkDir: "/tmp/browser",
				},
			},
		},
		Agents: map[string]AgentDecl{
			"ops": {
				Description:  "Ops preset",
				SystemPrompt: "You are an ops agent.",
				Model:        "gpt-5",
				Tools:        []string{"demo.tool"},
				Skills:       []string{"deploy"},
				MaxTokens:    2048,
			},
		},
		UIHints: map[string]ConfigUIHint{
			"providers.demo-provider.api_key": {
				Label:     "Demo API key",
				Help:      "Injected from env",
				Sensitive: true,
			},
		},
	}

	if got := manifest.MCPServers["browser"].Command; got != "npx" {
		t.Fatalf("MCPServers[browser].Command = %q, want npx", got)
	}
	if got := manifest.Channels["demo-channel"].Command; got != "./demo-channel" {
		t.Fatalf("Channels[demo-channel].Command = %q", got)
	}
	if got := manifest.Providers["demo-provider"].Timeout; got != 5*time.Second {
		t.Fatalf("Providers[demo-provider].Timeout = %v", got)
	}

	kinds := []ComponentKind{
		ComponentKindProvider,
		ComponentKindChannel,
		ComponentKindTool,
		ComponentKindCommand,
		ComponentKindConfig,
		ComponentKindRuntimeBridge,
		ComponentKindSkillsDir,
		ComponentKindHooksDir,
		ComponentKindMCPServer,
		ComponentKindAgent,
	}
	if len(kinds) != 10 {
		t.Fatalf("len(kinds) = %d, want 10", len(kinds))
	}
}

func TestValidateManifest(t *testing.T) {
	t.Parallel()

	valid := Manifest{
		Name:    "demo",
		Version: "1.0.0-beta.1",
		Providers: map[string]ProviderDecl{
			"demo": {API: "openai-completions"},
		},
	}
	if errs := ValidateManifest(valid); len(errs) != 0 {
		t.Fatalf("ValidateManifest(valid) errors = %#v", errs)
	}

	invalid := Manifest{
		Version: "1.two.3",
		Providers: map[string]ProviderDecl{
			"broken": {},
		},
	}
	errs := ValidateManifest(invalid)
	if len(errs) != 3 {
		t.Fatalf("ValidateManifest(invalid) len = %d, want 3 (%#v)", len(errs), errs)
	}
}
