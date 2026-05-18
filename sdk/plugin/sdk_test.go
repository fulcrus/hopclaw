package plugin

import (
	"context"
	"errors"
	"testing"
)

type stubRuntime struct {
	manifest Manifest
	config   map[string]any
	env      map[string]string
}

func (s stubRuntime) Manifest() Manifest {
	return s.manifest
}

func (s stubRuntime) Config() map[string]any {
	return cloneMapAny(s.config)
}

func (s stubRuntime) LookupEnv(key string) (string, bool) {
	value, ok := s.env[key]
	return value, ok
}

func (s stubRuntime) Emit(context.Context, Event) error {
	return nil
}

func (s stubRuntime) Logf(string, ...any) {
}

func TestConfigValue(t *testing.T) {
	t.Parallel()

	runtime := stubRuntime{
		config: map[string]any{
			"mode": "demo",
		},
	}

	value, err := ConfigValue(runtime, "mode")
	if err != nil {
		t.Fatalf("ConfigValue() error = %v", err)
	}
	if got := value.(string); got != "demo" {
		t.Fatalf("ConfigValue() = %q, want %q", got, "demo")
	}

	_, err = ConfigValue(runtime, "missing")
	if !errors.Is(err, ErrConfigKeyAbsent) {
		t.Fatalf("ConfigValue(missing) error = %v, want ErrConfigKeyAbsent", err)
	}

	_, err = ConfigValue(nil, "mode")
	if !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("ConfigValue(nil) error = %v, want ErrNilRuntime", err)
	}
}

func TestCloneManifestIsolatedCopies(t *testing.T) {
	t.Parallel()

	original := Manifest{
		ConfigSchema: map[string]any{
			"nested": map[string]any{
				"enabled": true,
			},
		},
		ProviderAuthEnvVars: map[string][]string{
			"demo": {"DEMO_API_KEY"},
		},
		Providers: map[string]ProviderDecl{
			"demo": {
				Headers: map[string]string{"X-Test": "1"},
				EnvVars: []string{"DEMO_API_KEY"},
			},
		},
		Channels: map[string]ChannelDecl{
			"demo": {
				Args:         []string{"serve"},
				Env:          map[string]string{"MODE": "test"},
				Capabilities: []string{"send"},
				Config:       map[string]any{"color": "blue"},
			},
		},
		Tools: []ToolDecl{{
			Name:        "demo.tool",
			InputSchema: map[string]any{"type": "object"},
		}},
		MCPServers: map[string]MCPServerDecl{
			"browser": {
				ServerConfig: ServerConfig{
					Args: []string{"run"},
					Env:  map[string]string{"A": "1"},
				},
			},
		},
		Agents: map[string]AgentDecl{
			"ops": {
				Tools:  []string{"demo.tool"},
				Skills: []string{"deploy"},
			},
		},
	}

	cloned := cloneManifest(original)
	cloned.ConfigSchema["nested"].(map[string]any)["enabled"] = false
	cloned.ProviderAuthEnvVars["demo"][0] = "UPDATED"
	cloned.Providers["demo"] = ProviderDecl{
		Headers: map[string]string{"X-Test": "2"},
	}
	channel := cloned.Channels["demo"]
	channel.Args[0] = "updated"
	channel.Config["color"] = "green"
	cloned.Channels["demo"] = channel
	cloned.Tools[0].InputSchema["type"] = "string"
	server := cloned.MCPServers["browser"]
	server.Args[0] = "changed"
	cloned.MCPServers["browser"] = server
	agent := cloned.Agents["ops"]
	agent.Tools[0] = "updated.tool"
	cloned.Agents["ops"] = agent

	if original.ConfigSchema["nested"].(map[string]any)["enabled"].(bool) != true {
		t.Fatalf("original ConfigSchema mutated = %#v", original.ConfigSchema)
	}
	if original.ProviderAuthEnvVars["demo"][0] != "DEMO_API_KEY" {
		t.Fatalf("original ProviderAuthEnvVars mutated = %#v", original.ProviderAuthEnvVars)
	}
	if original.Providers["demo"].Headers["X-Test"] != "1" {
		t.Fatalf("original Providers mutated = %#v", original.Providers)
	}
	if original.Channels["demo"].Args[0] != "serve" {
		t.Fatalf("original Channels args mutated = %#v", original.Channels)
	}
	if original.Channels["demo"].Config["color"] != "blue" {
		t.Fatalf("original Channels config mutated = %#v", original.Channels)
	}
	if original.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("original Tools mutated = %#v", original.Tools)
	}
	if original.MCPServers["browser"].Args[0] != "run" {
		t.Fatalf("original MCPServers mutated = %#v", original.MCPServers)
	}
	if original.Agents["ops"].Tools[0] != "demo.tool" {
		t.Fatalf("original Agents mutated = %#v", original.Agents)
	}
}
