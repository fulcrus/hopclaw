package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadManifestSupportsOpenClawManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data := `{
  "id": "openai",
  "name": "OpenAI",
  "enabledByDefault": false,
  "legacyPluginIds": ["openai-auth", "openai-legacy"],
  "autoEnableWhenConfiguredProviders": ["openai", "openai-codex"],
  "kind": ["provider", "media"],
  "providers": ["openai", "openai-codex"],
  "skills": ["./skills"],
  "channels": ["console"],
  "providerDiscoveryEntry": "./provider-discovery.ts",
  "modelSupport": {
    "modelPrefixes": ["gpt-", "o4"],
    "modelPatterns": ["^chatgpt-.*$"]
  },
  "cliBackends": ["codex-cli"],
  "providerAuthEnvVars": {
    "openai": ["OPENAI_API_KEY"]
  },
  "providerAuthAliases": {
    "openai-codex": "openai"
  },
  "channelEnvVars": {
    "console": ["OPENCLAW_TOKEN"]
  },
  "providerAuthChoices": [
    {
      "provider": "openai-codex",
      "method": "oauth",
      "choiceId": "openai-codex",
      "deprecatedChoiceIds": ["codex-cli"],
      "choiceLabel": "OpenAI Codex",
      "groupId": "openai",
      "groupLabel": "OpenAI",
      "optionKey": "openaiApiKey",
      "cliFlag": "--openai-api-key",
      "cliOption": "--openai-api-key <key>",
      "cliDescription": "OpenAI API key",
      "onboardingScopes": ["text-inference", "image-generation"]
    }
  ],
  "contracts": {
    "mediaUnderstandingProviders": ["openai", "openai-codex"],
    "imageGenerationProviders": ["openai"]
  },
  "configContracts": {
    "compatibilityRuntimePaths": ["tools.web.search.apiKey"],
    "dangerousFlags": [
      {
        "path": "webSearch.region",
        "equals": "cn"
      }
    ]
  },
  "uiHints": {
    "webSearch.apiKey": {
      "label": "Search API Key",
      "help": "Provider-specific search API key",
      "sensitive": true
    }
  },
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "webSearch": {
        "type": "object",
        "properties": {
          "apiKey": {
            "type": "string"
          }
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, openClawManifestFile), []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	packageData := `{
  "name": "@openclaw/openai-provider",
  "version": "2026.4.10",
  "description": "OpenClaw OpenAI provider plugins",
  "openclaw": {
    "extensions": ["./index.ts"],
    "setupEntry": "./setup-entry.ts",
    "channel": {
      "id": "console",
      "label": "Console",
      "configuredState": {
        "specifier": "./configured-state",
        "exportName": "hasConsoleConfiguredState"
      }
    },
    "install": {
      "npmSpec": "@openclaw/openai-provider",
      "defaultChoice": "npm"
    },
    "bundle": {
      "stageRuntimeDependencies": true
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, packageManifestFile), []byte(packageData), 0o644); err != nil {
		t.Fatalf("WriteFile(package.json) error = %v", err)
	}

	loaded, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	if loaded.Manifest.Format != ManifestFormatOpenClawJSON {
		t.Fatalf("manifest format = %q, want %q", loaded.Manifest.Format, ManifestFormatOpenClawJSON)
	}
	if loaded.Manifest.Name != "OpenAI" {
		t.Fatalf("manifest name = %q, want OpenAI", loaded.Manifest.Name)
	}
	if loaded.Manifest.Version != "2026.4.10" {
		t.Fatalf("manifest version = %q, want 2026.4.10", loaded.Manifest.Version)
	}
	if loaded.Manifest.Description != "OpenClaw OpenAI provider plugins" {
		t.Fatalf("manifest description = %q", loaded.Manifest.Description)
	}
	if loaded.Manifest.OpenClawID != "openai" {
		t.Fatalf("OpenClawID = %q, want openai", loaded.Manifest.OpenClawID)
	}
	if loaded.Manifest.EnabledByDefault == nil || *loaded.Manifest.EnabledByDefault {
		t.Fatalf("EnabledByDefault = %#v, want false", loaded.Manifest.EnabledByDefault)
	}
	if !reflect.DeepEqual(loaded.Manifest.LegacyPluginIDs, []string{"openai-auth", "openai-legacy"}) {
		t.Fatalf("LegacyPluginIDs = %#v", loaded.Manifest.LegacyPluginIDs)
	}
	if !reflect.DeepEqual(loaded.Manifest.AutoEnableWhenConfiguredProviders, []string{"openai", "openai-codex"}) {
		t.Fatalf("AutoEnableWhenConfiguredProviders = %#v", loaded.Manifest.AutoEnableWhenConfiguredProviders)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawKinds, []string{"provider", "media"}) {
		t.Fatalf("OpenClawKinds = %#v", loaded.Manifest.OpenClawKinds)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawChannels, []string{"console"}) {
		t.Fatalf("OpenClawChannels = %#v", loaded.Manifest.OpenClawChannels)
	}
	if loaded.Manifest.OpenClawProviderDiscoveryEntry != "./provider-discovery.ts" {
		t.Fatalf("OpenClawProviderDiscoveryEntry = %q", loaded.Manifest.OpenClawProviderDiscoveryEntry)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawModelSupport, OpenClawModelSupport{
		ModelPrefixes: []string{"gpt-", "o4"},
		ModelPatterns: []string{"^chatgpt-.*$"},
	}) {
		t.Fatalf("OpenClawModelSupport = %#v", loaded.Manifest.OpenClawModelSupport)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawCLIBackends, []string{"codex-cli"}) {
		t.Fatalf("OpenClawCLIBackends = %#v", loaded.Manifest.OpenClawCLIBackends)
	}
	if loaded.Manifest.OpenClawPackageName != "@openclaw/openai-provider" {
		t.Fatalf("OpenClawPackageName = %q", loaded.Manifest.OpenClawPackageName)
	}
	if loaded.Manifest.OpenClawPackageVersion != "2026.4.10" {
		t.Fatalf("OpenClawPackageVersion = %q", loaded.Manifest.OpenClawPackageVersion)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawExtensions, []string{"./index.ts"}) {
		t.Fatalf("OpenClawExtensions = %#v", loaded.Manifest.OpenClawExtensions)
	}
	if loaded.Manifest.OpenClawSetupEntry != "./setup-entry.ts" {
		t.Fatalf("OpenClawSetupEntry = %q", loaded.Manifest.OpenClawSetupEntry)
	}
	if loaded.Manifest.OpenClawChannelMetadata["id"] != "console" {
		t.Fatalf("OpenClawChannelMetadata = %#v", loaded.Manifest.OpenClawChannelMetadata)
	}
	if len(loaded.Manifest.OpenClawPackageMetadata) == 0 {
		t.Fatal("expected OpenClawPackageMetadata to be preserved")
	}
	if len(loaded.Manifest.SkillsDirs) != 1 || loaded.Manifest.SkillsDirs[0] != "./skills" {
		t.Fatalf("skills dirs = %#v", loaded.Manifest.SkillsDirs)
	}
	provider, ok := loaded.Manifest.Providers["openai"]
	if !ok {
		t.Fatal("expected translated openai provider")
	}
	if provider.API != "openai-completions" {
		t.Fatalf("provider.API = %q, want openai-completions", provider.API)
	}
	if provider.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("provider.BaseURL = %q", provider.BaseURL)
	}
	if len(provider.EnvVars) != 1 || provider.EnvVars[0] != "OPENAI_API_KEY" {
		t.Fatalf("provider.EnvVars = %#v", provider.EnvVars)
	}
	if provider.APIKeyHint == "" {
		t.Fatal("expected translated api key hint")
	}
	if len(loaded.Manifest.ProviderAuthEnvVars) != 1 || len(loaded.Manifest.ProviderAuthEnvVars["openai"]) != 1 {
		t.Fatalf("ProviderAuthEnvVars = %#v", loaded.Manifest.ProviderAuthEnvVars)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawProviderAuthAliases, map[string]string{"openai-codex": "openai"}) {
		t.Fatalf("OpenClawProviderAuthAliases = %#v", loaded.Manifest.OpenClawProviderAuthAliases)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawChannelEnvVars, map[string][]string{"console": {"OPENCLAW_TOKEN"}}) {
		t.Fatalf("OpenClawChannelEnvVars = %#v", loaded.Manifest.OpenClawChannelEnvVars)
	}
	if len(loaded.Manifest.OpenClawProviderAuthChoices) != 1 {
		t.Fatalf("OpenClawProviderAuthChoices = %#v", loaded.Manifest.OpenClawProviderAuthChoices)
	}
	if choice := loaded.Manifest.OpenClawProviderAuthChoices[0]; choice.Provider != "openai-codex" || choice.ChoiceID != "openai-codex" {
		t.Fatalf("OpenClawProviderAuthChoices[0] = %#v", choice)
	}
	if len(loaded.Manifest.UIHints) != 1 || !loaded.Manifest.UIHints["webSearch.apiKey"].Sensitive {
		t.Fatalf("UIHints = %#v", loaded.Manifest.UIHints)
	}
	if !reflect.DeepEqual(loaded.Manifest.OpenClawContracts, map[string][]string{
		"imageGenerationProviders":    {"openai"},
		"mediaUnderstandingProviders": {"openai", "openai-codex"},
	}) {
		t.Fatalf("OpenClawContracts = %#v", loaded.Manifest.OpenClawContracts)
	}
	if paths, ok := loaded.Manifest.OpenClawConfigContracts["compatibilityRuntimePaths"].([]any); !ok || len(paths) != 1 || paths[0] != "tools.web.search.apiKey" {
		t.Fatalf("OpenClawConfigContracts.compatibilityRuntimePaths = %#v", loaded.Manifest.OpenClawConfigContracts["compatibilityRuntimePaths"])
	}
	if len(loaded.Manifest.UnsupportedProviders) != 1 || loaded.Manifest.UnsupportedProviders[0] != "openai-codex" {
		t.Fatalf("UnsupportedProviders = %#v", loaded.Manifest.UnsupportedProviders)
	}
	if loaded.ModuleManifest().DefaultEnabled {
		t.Fatal("ModuleManifest().DefaultEnabled = true, want false")
	}

	manager := NewManager()
	if err := manager.Register(loaded); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	providers := manager.Providers()
	if _, ok := providers["openai"]; !ok {
		t.Fatalf("expected unscoped openai provider key, got %#v", providers)
	}
	if _, ok := providers["openai/openai"]; ok {
		t.Fatalf("unexpected scoped compatibility provider key in %#v", providers)
	}

	components := loaded.Components()
	foundConfigContract := false
	foundRuntimeBridge := false
	for _, item := range components {
		switch item.Kind {
		case ComponentKindConfig:
			foundConfigContract = true
			if item.Metadata["format"] != string(ManifestFormatOpenClawJSON) {
				t.Fatalf("config contract format = %#v", item.Metadata["format"])
			}
			if item.Metadata["id"] != "openai" {
				t.Fatalf("config contract id = %#v", item.Metadata["id"])
			}
			if item.Metadata["enabled_by_default"] != false {
				t.Fatalf("config contract enabled_by_default = %#v", item.Metadata["enabled_by_default"])
			}
			if !reflect.DeepEqual(item.Metadata["legacy_plugin_ids"], []string{"openai-auth", "openai-legacy"}) {
				t.Fatalf("config contract legacy_plugin_ids = %#v", item.Metadata["legacy_plugin_ids"])
			}
			if !reflect.DeepEqual(item.Metadata["auto_enable_when_configured_providers"], []string{"openai", "openai-codex"}) {
				t.Fatalf("config contract auto_enable_when_configured_providers = %#v", item.Metadata["auto_enable_when_configured_providers"])
			}
			if !reflect.DeepEqual(item.Metadata["kinds"], []string{"media", "provider"}) {
				t.Fatalf("config contract kinds = %#v", item.Metadata["kinds"])
			}
			if !reflect.DeepEqual(item.Metadata["channels"], []string{"console"}) {
				t.Fatalf("config contract channels = %#v", item.Metadata["channels"])
			}
			if item.Metadata["provider_discovery_entry"] != "./provider-discovery.ts" {
				t.Fatalf("config contract provider_discovery_entry = %#v", item.Metadata["provider_discovery_entry"])
			}
			if item.Metadata["package_name"] != "@openclaw/openai-provider" {
				t.Fatalf("config contract package_name = %#v", item.Metadata["package_name"])
			}
			if item.Metadata["package_version"] != "2026.4.10" {
				t.Fatalf("config contract package_version = %#v", item.Metadata["package_version"])
			}
			if !reflect.DeepEqual(item.Metadata["model_support"], OpenClawModelSupport{
				ModelPrefixes: []string{"gpt-", "o4"},
				ModelPatterns: []string{"^chatgpt-.*$"},
			}) {
				t.Fatalf("config contract model_support = %#v", item.Metadata["model_support"])
			}
			if !reflect.DeepEqual(item.Metadata["cli_backends"], []string{"codex-cli"}) {
				t.Fatalf("config contract cli_backends = %#v", item.Metadata["cli_backends"])
			}
			if !reflect.DeepEqual(item.Metadata["provider_auth_aliases"], map[string]string{"openai-codex": "openai"}) {
				t.Fatalf("config contract provider_auth_aliases = %#v", item.Metadata["provider_auth_aliases"])
			}
			if !reflect.DeepEqual(item.Metadata["channel_env_vars"], map[string][]string{"console": {"OPENCLAW_TOKEN"}}) {
				t.Fatalf("config contract channel_env_vars = %#v", item.Metadata["channel_env_vars"])
			}
			if choices, ok := item.Metadata["provider_auth_choices"].([]OpenClawProviderAuthChoice); !ok || len(choices) != 1 || choices[0].ChoiceID != "openai-codex" {
				t.Fatalf("config contract provider_auth_choices = %#v", item.Metadata["provider_auth_choices"])
			}
			if !reflect.DeepEqual(item.Metadata["contracts"], map[string][]string{
				"imageGenerationProviders":    {"openai"},
				"mediaUnderstandingProviders": {"openai", "openai-codex"},
			}) {
				t.Fatalf("config contract contracts = %#v", item.Metadata["contracts"])
			}
			if !reflect.DeepEqual(item.Metadata["package_openclaw_keys"], []string{"bundle", "channel", "extensions", "install", "setupEntry"}) {
				t.Fatalf("config contract package_openclaw_keys = %#v", item.Metadata["package_openclaw_keys"])
			}
			configContracts, ok := item.Metadata["config_contracts"].(map[string]any)
			if !ok {
				t.Fatalf("config contract config_contracts = %#v", item.Metadata["config_contracts"])
			}
			if paths, ok := configContracts["compatibilityRuntimePaths"].([]any); !ok || len(paths) != 1 || paths[0] != "tools.web.search.apiKey" {
				t.Fatalf("config contract config_contracts.compatibilityRuntimePaths = %#v", configContracts["compatibilityRuntimePaths"])
			}
		case ComponentKindRuntimeBridge:
			foundRuntimeBridge = true
			if item.Metadata["status"] != string(OpenClawRuntimeBridgeStatusDiscoveredNotLoaded) {
				t.Fatalf("runtime bridge status = %#v", item.Metadata["status"])
			}
			if item.Metadata["reason"] == "" {
				t.Fatalf("runtime bridge reason = %#v", item.Metadata["reason"])
			}
			if item.Metadata["package_name"] != "@openclaw/openai-provider" {
				t.Fatalf("runtime bridge package_name = %#v", item.Metadata["package_name"])
			}
			if item.Metadata["package_version"] != "2026.4.10" {
				t.Fatalf("runtime bridge package_version = %#v", item.Metadata["package_version"])
			}
			discovery, ok := item.Metadata["provider_discovery_entry"].(map[string]any)
			if !ok || discovery["specifier"] != "./provider-discovery.ts" || discovery["path"] != filepath.Join(dir, "provider-discovery.ts") {
				t.Fatalf("runtime bridge provider_discovery_entry = %#v", item.Metadata["provider_discovery_entry"])
			}
			entries, ok := item.Metadata["runtime_entries"].([]map[string]any)
			if !ok || len(entries) != 1 || entries[0]["specifier"] != "./index.ts" || entries[0]["path"] != filepath.Join(dir, "index.ts") {
				t.Fatalf("runtime bridge runtime_entries = %#v", item.Metadata["runtime_entries"])
			}
			setup, ok := item.Metadata["setup_entry"].(map[string]any)
			if !ok || setup["specifier"] != "./setup-entry.ts" || setup["path"] != filepath.Join(dir, "setup-entry.ts") {
				t.Fatalf("runtime bridge setup_entry = %#v", item.Metadata["setup_entry"])
			}
			channel, ok := item.Metadata["channel"].(map[string]any)
			if !ok || channel["id"] != "console" || channel["label"] != "Console" {
				t.Fatalf("runtime bridge channel = %#v", item.Metadata["channel"])
			}
			configuredState, ok := channel["configured_state"].(map[string]any)
			if !ok || configuredState["specifier"] != "./configured-state" || configuredState["export_name"] != "hasConsoleConfiguredState" || configuredState["path"] != filepath.Join(dir, "configured-state") {
				t.Fatalf("runtime bridge configured_state = %#v", channel["configured_state"])
			}
		}
	}
	if !foundConfigContract {
		t.Fatalf("expected config contract component in %#v", components)
	}
	if !foundRuntimeBridge {
		t.Fatalf("expected runtime bridge component in %#v", components)
	}
}

func TestLoadManifestRejectsOpenClawPackageRuntimeEntryOutsideRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifest := `{
  "id": "compat-plugin",
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "properties": {}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, openClawManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	packageData := `{
  "name": "@openclaw/compat-plugin",
  "openclaw": {
    "extensions": ["../outside.ts"]
  }
}`
	if err := os.WriteFile(filepath.Join(dir, packageManifestFile), []byte(packageData), 0o644); err != nil {
		t.Fatalf("WriteFile(package.json) error = %v", err)
	}

	if _, err := loadManifest(dir); err == nil {
		t.Fatal("expected openclaw package runtime entry validation error")
	}
}

func TestDefaultPluginDirsIncludesExtensionRoots(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	dirs := DefaultPluginDirs(workspaceRoot)
	expected := []string{
		filepath.Join(workspaceRoot, ".hopclaw", "extensions"),
		filepath.Join(workspaceRoot, ".openclaw", "extensions"),
		filepath.Join(workspaceRoot, "extensions"),
	}
	for _, want := range expected {
		found := false
		for _, dir := range dirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("DefaultPluginDirs() missing %q in %#v", want, dirs)
		}
	}
}

func TestTranslateOpenClawManifestSupportsRuntimeCompatibleProviders(t *testing.T) {
	t.Parallel()

	manifest, err := translateOpenClawManifest([]byte(`{
  "id": "compat-suite",
  "providers": ["kimi", "kimi-coding", "minimax-portal", "qwen-portal", "sglang", "openai-codex"],
  "providerAuthEnvVars": {
    "kimi": ["KIMI_API_KEY", "KIMICODE_API_KEY"],
    "kimi-coding": ["KIMI_API_KEY", "KIMICODE_API_KEY"],
    "minimax-portal": ["MINIMAX_OAUTH_TOKEN", "MINIMAX_API_KEY"],
    "qwen-portal": ["QWEN_OAUTH_TOKEN", "QWEN_PORTAL_API_KEY"],
    "sglang": ["SGLANG_API_KEY"]
  },
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "properties": {}
  }
}`))
	if err != nil {
		t.Fatalf("translateOpenClawManifest() error = %v", err)
	}

	tests := []struct {
		name        string
		wantAPI     string
		wantBaseURL string
		wantModel   string
		wantEnvVars []string
	}{
		{
			name:        "kimi",
			wantAPI:     "openai-completions",
			wantBaseURL: "https://api.moonshot.ai/v1",
			wantModel:   "kimi-k2.5",
			wantEnvVars: []string{"KIMI_API_KEY", "KIMICODE_API_KEY"},
		},
		{
			name:        "kimi-coding",
			wantAPI:     "anthropic-messages",
			wantBaseURL: "https://api.kimi.com/coding/",
			wantModel:   "k2p5",
			wantEnvVars: []string{"KIMI_API_KEY", "KIMICODE_API_KEY"},
		},
		{
			name:        "minimax-portal",
			wantAPI:     "anthropic-messages",
			wantBaseURL: "https://api.minimax.io/anthropic",
			wantModel:   "MiniMax-M2.5",
			wantEnvVars: []string{"MINIMAX_OAUTH_TOKEN", "MINIMAX_API_KEY"},
		},
		{
			name:        "qwen-portal",
			wantAPI:     "openai-completions",
			wantBaseURL: "https://portal.qwen.ai/v1",
			wantModel:   "qwen3.5-plus",
			wantEnvVars: []string{"QWEN_OAUTH_TOKEN", "QWEN_PORTAL_API_KEY"},
		},
		{
			name:        "sglang",
			wantAPI:     "openai-completions",
			wantBaseURL: "http://127.0.0.1:30000/v1",
			wantEnvVars: []string{"SGLANG_API_KEY"},
		},
	}

	for _, tt := range tests {
		provider, ok := manifest.Providers[tt.name]
		if !ok {
			t.Fatalf("expected translated provider %q in %#v", tt.name, manifest.Providers)
		}
		if provider.API != tt.wantAPI {
			t.Fatalf("%s API = %q, want %q", tt.name, provider.API, tt.wantAPI)
		}
		if provider.BaseURL != tt.wantBaseURL {
			t.Fatalf("%s BaseURL = %q, want %q", tt.name, provider.BaseURL, tt.wantBaseURL)
		}
		if provider.DefaultModel != tt.wantModel {
			t.Fatalf("%s DefaultModel = %q, want %q", tt.name, provider.DefaultModel, tt.wantModel)
		}
		if len(provider.EnvVars) != len(tt.wantEnvVars) {
			t.Fatalf("%s EnvVars = %#v, want %#v", tt.name, provider.EnvVars, tt.wantEnvVars)
		}
		for i := range tt.wantEnvVars {
			if provider.EnvVars[i] != tt.wantEnvVars[i] {
				t.Fatalf("%s EnvVars = %#v, want %#v", tt.name, provider.EnvVars, tt.wantEnvVars)
			}
		}
		if provider.APIKeyHint == "" {
			t.Fatalf("%s expected translated api key hint", tt.name)
		}
	}

	if len(manifest.UnsupportedProviders) != 1 || manifest.UnsupportedProviders[0] != "openai-codex" {
		t.Fatalf("UnsupportedProviders = %#v", manifest.UnsupportedProviders)
	}
}
