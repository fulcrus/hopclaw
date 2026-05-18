package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

// ---------------------------------------------------------------------------
// Non-interactive onboarding
// ---------------------------------------------------------------------------

func TestOnboardNonInteractive_CreatesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("OPENAI_API_KEY", "sk-test-noninteractive-key")

	err := runOnboardNonInteractive()
	if err != nil {
		t.Fatalf("runOnboardNonInteractive: %v", err)
	}

	cfgPath := filepath.Join(dir, ".hopclaw", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "server:") {
		t.Error("config missing server section")
	}
	if !strings.Contains(content, "openai") || !strings.Contains(content, "api_key") {
		t.Error("config missing openai provider setup")
	}
}

func TestOnboardNonInteractive_ExistingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("OPENAI_API_KEY", "sk-test-existing")

	// Create existing config.
	cfgDir := filepath.Join(dir, ".hopclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existingContent := "server:\n  address: \"127.0.0.1:9999\"\n"
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(existingContent), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runOnboardNonInteractive()
	if err != nil {
		t.Fatalf("runOnboardNonInteractive: %v", err)
	}

	// Config should not be overwritten.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "9999") {
		t.Error("existing config was overwritten")
	}
}

func TestOnboardNonInteractive_NoAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	err := runOnboardNonInteractive()
	if err == nil {
		t.Fatal("expected error when no API key set")
	}
	if err.Error() != config.MissingAPIKeyMessage() {
		t.Errorf("error = %q, want %q", err.Error(), config.MissingAPIKeyMessage())
	}
}

func TestEnsureWebFirstConfig_CreatesMinimalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configPath := filepath.Join(dir, ".hopclaw", "config.yaml")

	reused, err := ensureWebFirstConfig(configPath)
	if err != nil {
		t.Fatalf("ensureWebFirstConfig: %v", err)
	}
	if reused {
		t.Fatal("expected new config, got reused=true")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `default_model: "unconfigured-model"`) {
		t.Fatalf("expected minimal config with unconfigured model:\n%s", content)
	}
	if strings.Contains(content, "\nmodels:\n") {
		t.Fatalf("minimal web-first config should not render models section:\n%s", content)
	}
}

func TestEnsureWebFirstConfig_ReusesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configPath := filepath.Join(dir, ".hopclaw", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "server:\n  address: \"127.0.0.1:9999\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	reused, err := ensureWebFirstConfig(configPath)
	if err != nil {
		t.Fatalf("ensureWebFirstConfig: %v", err)
	}
	if !reused {
		t.Fatal("expected existing config to be reused")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Fatalf("existing config was modified:\n%s", string(data))
	}
}

func TestBuildBackgroundGatewayEnvAddsNoBrowserWithoutLeakingHostEnv(t *testing.T) {
	t.Setenv("HOPCLAW_ONBOARD_LEAK", "host-only")

	env := buildBackgroundGatewayEnv()
	if got := envSliceValue(env, "HOPCLAW_NO_BROWSER"); got != "1" {
		t.Fatalf("HOPCLAW_NO_BROWSER = %q, want %q", got, "1")
	}
	if got := envSliceValue(env, "HOPCLAW_ONBOARD_LEAK"); got != "" {
		t.Fatalf("unexpected host env leak = %q", got)
	}
	if got := envSliceValue(env, "PATH"); got == "" {
		t.Fatal("PATH should be present in child env")
	}
}

func TestSuggestAvailableGatewayAddress_PicksAlternativeWhenBusy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	busyAddr := listener.Addr().String()
	got := suggestAvailableGatewayAddress(busyAddr)
	if got == busyAddr {
		t.Fatalf("suggestAvailableGatewayAddress(%q) = %q, want alternative address", busyAddr, got)
	}
	if !canListenOnAddress(got) {
		t.Fatalf("suggested address %q is not available", got)
	}
}

func TestExistingOnboardConfigSummary(t *testing.T) {
	catalog := localCLISetupCatalog()
	cfg := config.Config{
		Server: config.ServerConfig{
			Address:   "127.0.0.1:17080",
			AuthToken: "secret-token",
		},
		Agent: config.AgentConfig{
			DefaultModel: "gpt-4o-mini",
		},
		Models: config.ModelsConfig{
			DefaultProvider: "default",
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-test",
				Model:   "gpt-4o-mini",
			},
		},
		Channels: config.ChannelsConfig{
			Feishu: config.FeishuChannelConfig{
				AppID:     "cli-test-app",
				AppSecret: "cli-test-secret",
			},
		},
	}

	summary := existingOnboardConfigSummary(cfg, catalog)
	for _, want := range []string{"Bearer", "OpenAI Compatible", "Feishu / Lark", "127.0.0.1:17080"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestPrintPlatformDaemonGuidance(t *testing.T) {

	// Just verify it doesn't panic.
	printPlatformDaemonGuidance()
}

func TestVerifyGatewayConnectivity(t *testing.T) {
	// Uses flagConfig global — cannot be parallel.
	old := flagConfig
	flagConfig = filepath.Join(t.TempDir(), "nonexistent.yaml")
	defer func() { flagConfig = old }()

	// Just verify it doesn't panic — it should print a message about
	// the gateway being unreachable.
	verifyGatewayConnectivity()
}

func TestPrintOnboardSummary(t *testing.T) {

	// Just verify it doesn't panic.
	printOnboardSummary()
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestOnboardConstants(t *testing.T) {

	if onboardTotalSteps != 7 {
		t.Errorf("expected 7 total steps, got %d", onboardTotalSteps)
	}
	if onboardVerifyMaxRetries < 1 {
		t.Error("expected at least 1 retry")
	}
	if len(config.OnboardingChannelProfiles()) == 0 {
		t.Error("expected at least one onboarding channel profile")
	}
	if len(recommendedSkills) == 0 {
		t.Error("expected at least one recommended skill")
	}
}

func TestAvailableRecommendedSkillsPrefersLocalSkillDirs(t *testing.T) {

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	skillDir := filepath.Join(tmp, "skills", "summarize")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# summarize"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := availableRecommendedSkills(context.Background(), nil, []string{"summarize", "translate"})
	if len(got) != 1 || got[0] != "summarize" {
		t.Fatalf("availableRecommendedSkills() = %#v, want [summarize]", got)
	}
}

func TestAvailableRecommendedSkillsChecksRemoteCatalog(t *testing.T) {

	queries := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operator/skills/catalog" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		if query != "weather" {
			_ = json.NewEncoder(w).Encode(catalogSkillsResponse{})
			return
		}
		_ = json.NewEncoder(w).Encode(catalogSkillsResponse{
			Items: []catalogSkillRow{{ID: "weather", Name: "weather"}},
			Count: 1,
		})
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, HTTP: server.Client()}
	got := availableRecommendedSkills(context.Background(), client, []string{"weather", "github"})
	if len(got) != 1 || got[0] != "weather" {
		t.Fatalf("availableRecommendedSkills() = %#v, want [weather]", got)
	}
	if len(queries) != 2 {
		t.Fatalf("catalog queries = %#v, want [weather github]", queries)
	}
}

func envSliceValue(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}
