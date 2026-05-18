package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPaths_IncludesEnvVar(t *testing.T) {
	t.Setenv("HOPCLAW_CONFIG", "/custom/path.yaml")

	paths := DefaultConfigPaths()
	if len(paths) == 0 {
		t.Fatal("DefaultConfigPaths() returned empty")
	}
	if paths[0] != "/custom/path.yaml" {
		t.Errorf("first path = %q, want /custom/path.yaml", paths[0])
	}
}

func TestDefaultConfigPaths_NoEnvVar(t *testing.T) {
	t.Setenv("HOPCLAW_CONFIG", "")

	paths := DefaultConfigPaths()
	// Should include CWD and home paths but not the env var.
	for _, p := range paths {
		if p == "" {
			t.Error("DefaultConfigPaths() contains empty string")
		}
	}
	if len(paths) < 2 {
		t.Errorf("expected at least 2 paths, got %d", len(paths))
	}
}

func TestDiscoverConfigPath_NotFound(t *testing.T) {
	// Clear env var so it doesn't find one.
	t.Setenv("HOPCLAW_CONFIG", "")
	t.Setenv("HOME", t.TempDir())

	p := DiscoverConfigPath()
	if p != "" {
		t.Errorf("DiscoverConfigPath() = %q, want empty", p)
	}
}

func TestDiscoverConfigPath_FoundInHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("HOPCLAW_CONFIG", "")

	configDir := filepath.Join(tmp, ".hopclaw")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("server:\n  address: ':8080'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := DiscoverConfigPath()
	if p != configFile {
		t.Errorf("DiscoverConfigPath() = %q, want %q", p, configFile)
	}
}

func TestGenerateDefaultConfig_NoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MOONSHOT_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("XIAOMI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("QIANFAN_API_KEY", "")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("VOLCENGINE_API_KEY", "")
	t.Setenv("HUNYUAN_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")

	cfg := GenerateDefaultConfig()
	if !strings.Contains(cfg, "unconfigured-model") {
		t.Error("expected minimal config with unconfigured-model")
	}
}

func TestGenerateDefaultConfig_OpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	cfg := GenerateDefaultConfig()
	if !strings.Contains(cfg, "sk-test") {
		t.Error("expected config to contain the API key")
	}
	if !strings.Contains(cfg, "openai") {
		t.Error("expected config to reference openai")
	}
}

func TestGenerateDefaultConfig_Anthropic(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("GOOGLE_API_KEY", "")

	cfg := GenerateDefaultConfig()
	if !strings.Contains(cfg, "sk-ant-test") {
		t.Error("expected config to contain the API key")
	}
	if !strings.Contains(cfg, "anthropic") {
		t.Error("expected config to reference anthropic")
	}
}

func TestDetectAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "test-key")

	provider, key := DetectAPIKey()
	if provider != "google" {
		t.Errorf("provider = %q, want google", provider)
	}
	if key != "test-key" {
		t.Errorf("key = %q, want test-key", key)
	}
}

func TestDetectAPIKey_DomesticProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-test")

	provider, key := DetectAPIKey()
	if provider != "deepseek" {
		t.Errorf("provider = %q, want deepseek", provider)
	}
	if key != "deepseek-test" {
		t.Errorf("key = %q, want deepseek-test", key)
	}
}

func TestDetectAPIKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-test")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-test")

	got := DetectAPIKeys()
	if len(got) != 3 {
		t.Fatalf("len(DetectAPIKeys()) = %d, want 3", len(got))
	}
	if got[0].Provider != "openai" || got[0].Key != "sk-test" {
		t.Fatalf("first detected key = %+v", got[0])
	}
	if got[1].Provider != "google" || got[1].Key != "google-test" {
		t.Fatalf("second detected key = %+v", got[1])
	}
	if got[2].Provider != "deepseek" || got[2].Key != "deepseek-test" {
		t.Fatalf("third detected key = %+v", got[2])
	}
}

func TestDetectAPIKey_IgnoresBedrockCredentialEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA_TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")

	provider, key := DetectAPIKey()
	if provider != "" || key != "" {
		t.Fatalf("DetectAPIKey() = (%q, %q), want empty result", provider, key)
	}
}

func TestDetectAPIKey_Xiaomi(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("XIAOMI_API_KEY", "xiaomi-test")

	provider, key := DetectAPIKey()
	if provider != "xiaomi" {
		t.Errorf("provider = %q, want xiaomi", provider)
	}
	if key != "xiaomi-test" {
		t.Errorf("key = %q, want xiaomi-test", key)
	}
}

func TestHasAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	if HasAPIKey() {
		t.Error("HasAPIKey() = true, want false with no keys set")
	}

	t.Setenv("OPENAI_API_KEY", "sk-test")
	if !HasAPIKey() {
		t.Error("HasAPIKey() = false, want true with key set")
	}
}

func TestProviderEnvVarExamples(t *testing.T) {
	examples := ProviderEnvVarExamples(4)
	if len(examples) != 4 {
		t.Fatalf("len(ProviderEnvVarExamples(4)) = %d, want 4", len(examples))
	}
	if examples[0] != "OPENAI_API_KEY" {
		t.Fatalf("examples[0] = %q, want OPENAI_API_KEY", examples[0])
	}
	if examples[1] != "ANTHROPIC_API_KEY" {
		t.Fatalf("examples[1] = %q, want ANTHROPIC_API_KEY", examples[1])
	}
	if examples[2] != "GOOGLE_API_KEY" {
		t.Fatalf("examples[2] = %q, want GOOGLE_API_KEY", examples[2])
	}
	if examples[3] != "DEEPSEEK_API_KEY" {
		t.Fatalf("examples[3] = %q, want DEEPSEEK_API_KEY", examples[3])
	}
	for _, example := range ProviderEnvVarExamples(32) {
		if strings.HasPrefix(example, "AWS_") {
			t.Fatalf("ProviderEnvVarExamples should not include AWS credential env vars: %q", example)
		}
	}
}

func TestProviderEnvExportHints(t *testing.T) {
	hints := ProviderEnvExportHints(2)
	if len(hints) != 2 {
		t.Fatalf("len(ProviderEnvExportHints(2)) = %d, want 2", len(hints))
	}
	if hints[0] != "export OPENAI_API_KEY=..." {
		t.Fatalf("hints[0] = %q", hints[0])
	}
	if hints[1] != "export ANTHROPIC_API_KEY=..." {
		t.Fatalf("hints[1] = %q", hints[1])
	}
}

func TestMissingAPIKeyTexts(t *testing.T) {
	if got := MissingAPIKeyMessage(); !strings.Contains(got, "OPENAI_API_KEY") {
		t.Fatalf("MissingAPIKeyMessage() = %q", got)
	}
	if got := MissingAPIKeyDoctorDetail(); !strings.Contains(got, "DEEPSEEK_API_KEY") {
		t.Fatalf("MissingAPIKeyDoctorDetail() = %q", got)
	}
	if got := MissingAPIKeyDoctorFix(); got != "set a supported provider API key env var such as OPENAI_API_KEY or ANTHROPIC_API_KEY" {
		t.Fatalf("MissingAPIKeyDoctorFix() = %q", got)
	}
}
