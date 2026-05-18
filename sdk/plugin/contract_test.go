package plugin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestManifestRoundTripJSONAndYAML(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Name:        "demo",
		Version:     "1.2.3",
		Description: "Demo plugin",
		Author:      "HopClaw",
		ConfigSchema: map[string]any{
			"type": "object",
		},
		UIHints: map[string]ConfigUIHint{
			"providers.demo.api_key": {
				Label:     "API key",
				Sensitive: true,
			},
		},
		ProviderAuthEnvVars: map[string][]string{
			"demo": {"DEMO_API_KEY"},
		},
		Providers: map[string]ProviderDecl{
			"demo": {
				API:          "openai-completions",
				BaseURL:      "https://api.example.com/v1",
				DefaultModel: "demo-model",
				Timeout:      5 * time.Second,
			},
		},
		Channels: map[string]ChannelDecl{
			"demo": {
				Type:         "stdio",
				Command:      "./demo-channel",
				Args:         []string{"serve"},
				Capabilities: []string{"send"},
			},
		},
		Tools: []ToolDecl{{
			Name:        "demo.tool",
			Description: "Demo tool",
			Endpoint:    "https://tools.example.com/demo",
		}},
		SkillsDir: "skills",
		HooksDir:  "hooks",
		MCPServers: map[string]MCPServerDecl{
			"browser": {
				Description: "Browser bridge",
				ServerConfig: ServerConfig{
					Name:    "browser",
					Command: "npx",
					Args:    []string{"@modelcontextprotocol/server-playwright"},
				},
			},
		},
		Agents: map[string]AgentDecl{
			"ops": {
				Description:  "Ops preset",
				SystemPrompt: "You are an ops agent.",
				Model:        "gpt-5",
			},
		},
		Commands: []CommandDecl{{
			Name: "inspect",
			Exec: "./bin/inspect",
		}},
	}

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		var decoded Manifest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if decoded.Name != manifest.Name || decoded.Providers["demo"].API != manifest.Providers["demo"].API {
			t.Fatalf("json round trip mismatch: %#v", decoded)
		}
		if decoded.Channels["demo"].Command != "./demo-channel" {
			t.Fatalf("channel command = %q", decoded.Channels["demo"].Command)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()

		data, err := yaml.Marshal(manifest)
		if err != nil {
			t.Fatalf("yaml.Marshal() error = %v", err)
		}

		var decoded Manifest
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("yaml.Unmarshal() error = %v", err)
		}
		if decoded.Name != manifest.Name || decoded.MCPServers["browser"].Command != "npx" {
			t.Fatalf("yaml round trip mismatch: %#v", decoded)
		}
		if decoded.Providers["demo"].Timeout != 5*time.Second {
			t.Fatalf("provider timeout = %v, want 5s", decoded.Providers["demo"].Timeout)
		}
	})
}

func TestValidateManifestContractBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Manifest
		want []string
	}{
		{
			name: "empty manifest",
			in:   Manifest{},
			want: []string{"manifest name is required"},
		},
		{
			name: "invalid version",
			in: Manifest{
				Name:    "demo",
				Version: "1.two.3",
			},
			want: []string{"invalid version"},
		},
		{
			name: "provider missing api",
			in: Manifest{
				Name: "demo",
				Providers: map[string]ProviderDecl{
					"broken": {},
				},
			},
			want: []string{"provider \"broken\": api is required"},
		},
		{
			name: "command missing name and exec",
			in: Manifest{
				Name: "demo",
				Commands: []CommandDecl{{
					Description: "broken",
				}},
			},
			want: []string{"command name is required", "command \"<unnamed>\": exec is required"},
		},
		{
			name: "valid relaxed version",
			in: Manifest{
				Name:    "demo",
				Version: "v1.0.0-beta.1",
				Providers: map[string]ProviderDecl{
					"demo": {API: "openai-completions"},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			errs := ValidateManifest(tt.in)
			if len(tt.want) == 0 {
				if len(errs) != 0 {
					t.Fatalf("ValidateManifest() errors = %#v", errs)
				}
				return
			}

			if len(errs) != len(tt.want) {
				t.Fatalf("ValidateManifest() len = %d, want %d (%#v)", len(errs), len(tt.want), errs)
			}
			for idx, want := range tt.want {
				if !strings.Contains(errs[idx].Error(), want) {
					t.Fatalf("error[%d] = %q, want substring %q", idx, errs[idx].Error(), want)
				}
			}
		})
	}
}

func TestManifestFieldTagsStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"name",
		"version",
		"description",
		"author",
		"config_schema",
		"ui_hints",
		"provider_auth_env_vars",
		"providers",
		"channels",
		"tools",
		"skills_dir",
		"skills_dirs",
		"hooks_dir",
		"mcp_servers",
		"agents",
		"commands",
	}

	if got := contractFieldNames(reflect.TypeOf(Manifest{}), "json"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest json fields = %#v, want %#v", got, want)
	}
	if got := contractFieldNames(reflect.TypeOf(Manifest{}), "yaml"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest yaml fields = %#v, want %#v", got, want)
	}
}

func TestEmptyManifestDefaultBehavior(t *testing.T) {
	t.Parallel()

	var manifest Manifest
	if manifest.Format != "" {
		t.Fatalf("Format = %q, want empty", manifest.Format)
	}
	if manifest.UnsupportedProviders != nil {
		t.Fatalf("UnsupportedProviders = %#v, want nil", manifest.UnsupportedProviders)
	}

	errs := ValidateManifest(manifest)
	if len(errs) != 1 || errs[0].Error() != "manifest name is required" {
		t.Fatalf("ValidateManifest(empty) = %#v", errs)
	}
}

func contractFieldNames(typ reflect.Type, tagName string) []string {
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get(tagName)
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}
