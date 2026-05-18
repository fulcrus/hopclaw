package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/modelrouter"
)

// ---------------------------------------------------------------------------
// models test
// ---------------------------------------------------------------------------

func TestModelsTest_MockServer(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsCompletionsPath {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := modelsCompletionResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "Hi there!"}},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 12},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Create a temp config that points to the test server.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "test-model"
models:
  openai_compat:
    base_url: "` + srv.URL + `"
    api_key: "test-key"
    model: "test-model"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	ctx := context.Background()
	start := makeTestCompletionRequest()

	var resp modelsCompletionResponse
	err := client.Post(ctx, modelsCompletionsPath, start, &resp)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if resp.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 12 {
		t.Errorf("expected 12 tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestModelsTest_LatencyMeasured(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsCompletionResponse{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 5},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "test-model"
models:
  openai_compat:
    base_url: "` + srv.URL + `"
    api_key: "test-key"
    model: "test-model"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := modelsTestResult{
		Provider: "default",
		Model:    "test-model",
		Status:   "ok",
	}

	// Verify result has the expected fields.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"provider", "model", "status", "latency_ms"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in test result", key)
		}
	}
}

func TestRunModelsStatusWithClientUsesOperatorStatusContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsStatusPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(operatorStatusResponse{
			OK:              true,
			Version:         "1.2.3",
			Uptime:          "10m0s",
			CapabilityCount: 7,
		})
	}))
	defer srv.Close()

	restore := captureStdout(t)
	err := runModelsStatusWithClient(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, false)
	if err != nil {
		t.Fatalf("runModelsStatusWithClient() error = %v", err)
	}

	output := restore()
	for _, want := range []string{
		"OK:           true",
		"Version:      1.2.3",
		"Uptime:       10m0s",
		"Capabilities: 7",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunModelsTestPrintsProbeTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsCompletionsPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsCompletionResponse{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 9},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "test-model"
models:
  openai_compat:
    base_url: "` + srv.URL + `"
    api_key: "test-key"
    model: "test-model"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	oldConfig := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = oldConfig }()

	oldFactory := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) {
		return &GatewayClient{BaseURL: srv.URL, HTTP: srv.Client()}, nil
	}
	defer func() { newGatewayClient = oldFactory }()

	restore := captureStdout(t)
	if err := runModelsTest(context.Background(), ""); err != nil {
		t.Fatalf("runModelsTest() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, "Testing provider:") || !strings.Contains(output, "Testing model:    test-model") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestRunModelsTestReturnsNonZeroOnProbeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "upstream unavailable"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "test-model"
models:
  openai_compat:
    base_url: "` + srv.URL + `"
    api_key: "test-key"
    model: "test-model"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	oldConfig := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = oldConfig }()

	oldFactory := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) {
		return &GatewayClient{BaseURL: srv.URL, HTTP: srv.Client()}, nil
	}
	defer func() { newGatewayClient = oldFactory }()

	restore := captureStdout(t)
	err := runModelsTest(context.Background(), "")
	if err == nil {
		t.Fatal("expected non-zero probe failure")
	}
	var exitErr *cliExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v, want cli exit error 1", err)
	}
	if output := restore(); !strings.Contains(output, "Status:   fail") || !strings.Contains(output, "upstream unavailable") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestModelsProgressIndicatorRendersTTYProgressAndClearsOnStop(t *testing.T) {
	oldWriter := modelsProgressWriter
	oldInterval := modelsProgressInterval
	oldCanRender := modelsProgressCanRender
	defer func() {
		modelsProgressWriter = oldWriter
		modelsProgressInterval = oldInterval
		modelsProgressCanRender = oldCanRender
	}()

	var output bytes.Buffer
	modelsProgressWriter = &output
	modelsProgressInterval = 5 * time.Millisecond
	modelsProgressCanRender = func() bool { return true }

	progress := startModelsProgressIndicator("Benchmarking openai")
	time.Sleep(20 * time.Millisecond)
	progress.Update("Benchmarking anthropic")
	time.Sleep(20 * time.Millisecond)
	progress.Stop()

	got := output.String()
	if !strings.Contains(got, "Benchmarking openai") {
		t.Fatalf("progress output missing initial message: %q", got)
	}
	if !strings.Contains(got, "Benchmarking anthropic") {
		t.Fatalf("progress output missing updated message: %q", got)
	}
	if !strings.Contains(got, "\r\033[2K") {
		t.Fatalf("progress output missing clear-line sequence: %q", got)
	}
}

// ---------------------------------------------------------------------------
// models info
// ---------------------------------------------------------------------------

func TestModelsInfo_DefaultProvider(t *testing.T) {
	// Modifies global flagConfig and flagJSON — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "gpt-4o"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "sk-test-key"
    model: "gpt-4o"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	err := runModelsInfo(context.Background(), "default")
	if err != nil {
		t.Fatalf("runModelsInfo: %v", err)
	}
}

func TestModelsInfo_NamedProvider(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "anthropic/claude-sonnet-4-20250514"
models:
  default_provider: anthropic
  providers:
    anthropic:
      api: anthropic-messages
      api_key: "sk-ant-test-key"
      default_model: "claude-sonnet-4-20250514"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	err := runModelsInfo(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("runModelsInfo: %v", err)
	}
}

func TestModelsInfo_NotFound(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "gpt-4o"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "test-key"
    model: "gpt-4o"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	err := runModelsInfo(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

// ---------------------------------------------------------------------------
// models bench
// ---------------------------------------------------------------------------

func TestModelsBench_MockServer(t *testing.T) {

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsCompletionResponse{
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	ctx := context.Background()
	result := benchProvider(ctx, client, "test-provider", "test-model")

	if result.Provider != "test-provider" {
		t.Errorf("expected provider test-provider, got %q", result.Provider)
	}
	if result.Model != "test-model" {
		t.Errorf("expected model test-model, got %q", result.Model)
	}
	if result.Failures != 0 {
		t.Errorf("expected 0 failures, got %d", result.Failures)
	}
	if result.MinMs > result.MaxMs {
		t.Errorf("min (%d) should be <= max (%d)", result.MinMs, result.MaxMs)
	}
	if result.AvgMs < result.MinMs || result.AvgMs > result.MaxMs {
		t.Errorf("avg (%d) should be between min (%d) and max (%d)", result.AvgMs, result.MinMs, result.MaxMs)
	}
}

func TestModelsBench_FailureHandling(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer srv.Close()

	client := &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}

	ctx := context.Background()
	result := benchProvider(ctx, client, "failing", "bad-model")

	if result.Failures != modelsBenchIterations {
		t.Errorf("expected %d failures, got %d", modelsBenchIterations, result.Failures)
	}
	if result.MinMs != 0 || result.AvgMs != 0 || result.MaxMs != 0 {
		t.Error("latency values should be 0 when all requests fail")
	}
}

// ---------------------------------------------------------------------------
// models list / router helpers
// ---------------------------------------------------------------------------

func TestBuildCLIModelProviders_DefaultProvider(t *testing.T) {

	cfg := config.ModelsConfig{
		OpenAICompat: config.OpenAICompatConfig{
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "test-key",
			Model:   "gpt-4o",
		},
	}

	providers, defaultProvider, err := buildCLIModelProviders(cfg)
	if err != nil {
		t.Fatalf("buildCLIModelProviders: %v", err)
	}
	if defaultProvider != "default" {
		t.Fatalf("defaultProvider = %q, want default", defaultProvider)
	}
	entry, ok := providers["default"]
	if !ok {
		t.Fatal("default provider missing")
	}
	if entry.DefaultModel != "gpt-4o" {
		t.Fatalf("default model = %q, want gpt-4o", entry.DefaultModel)
	}
}

func TestBuildCLIModelProviders_MergesCatalog(t *testing.T) {

	cfg := config.ModelsConfig{
		DefaultProvider: "anthropic",
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				APIKey:       "sk-ant-test",
				DefaultModel: "claude-sonnet-4-20250514",
			},
		},
	}

	providers, defaultProvider, err := buildCLIModelProviders(cfg)
	if err != nil {
		t.Fatalf("buildCLIModelProviders: %v", err)
	}
	if defaultProvider != "anthropic" {
		t.Fatalf("defaultProvider = %q, want anthropic", defaultProvider)
	}
	entry := providers["anthropic"]
	if entry.API == "" {
		t.Fatal("expected catalog API to be filled")
	}
	if entry.BaseURL == "" {
		t.Fatal("expected catalog BaseURL to be filled")
	}
}

func TestLoadOperatorModelStateUsesEffectiveContract(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsBasePath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsOperatorListResponse{
			Providers: []modelsOperatorProvider{
				{
					Name:         "default",
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					DefaultModel: "gpt-4o",
					HasKey:       true,
					Source:       "openai_compat",
					Timeout:      "30s",
					HeaderCount:  1,
					Mutable:      false,
					ConfigScope:  "openai_compat",
				},
				{
					Name:         "anthropic",
					API:          "anthropic-messages",
					BaseURL:      "https://api.anthropic.com",
					DefaultModel: "claude-sonnet-4-20250514",
					HasKey:       true,
					APIKeysCount: 2,
					Source:       "api",
					Mutable:      true,
					ConfigScope:  "providers",
					CapabilityMatrix: model.CapabilityMatrix{
						ProviderName:         "anthropic",
						ProviderAPI:          model.APIAnthropicMessages,
						Model:                "claude-sonnet-4-20250514",
						SupportsSystemPrompt: true,
						SupportsTemperature:  true,
						SupportsMaxTokens:    true,
						SupportsTools:        true,
						SupportsToolReplay:   true,
						SupportsStreaming:    false,
						Source:               "operator_contract",
					},
				},
			},
			Count:             2,
			DefaultProvider:   "anthropic",
			AgentDefaultModel: "anthropic/claude-sonnet-4-20250514",
		})
	}))
	defer srv.Close()

	state, err := loadOperatorModelState(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("loadOperatorModelState() error = %v", err)
	}
	if state.DefaultProvider != "anthropic" {
		t.Fatalf("DefaultProvider = %q, want anthropic", state.DefaultProvider)
	}
	if state.AgentDefaultModel != "anthropic/claude-sonnet-4-20250514" {
		t.Fatalf("AgentDefaultModel = %q", state.AgentDefaultModel)
	}
	if len(state.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(state.Providers))
	}
	defaultEntry, ok := state.Providers["default"]
	if !ok {
		t.Fatal("default provider missing")
	}
	if defaultEntry.DefaultModel != "gpt-4o" {
		t.Fatalf("default entry = %+v", defaultEntry)
	}
	defaultDetail := state.Details["default"]
	if defaultDetail.ConfigSource != "openai_compat" || defaultDetail.Mutable {
		t.Fatalf("default detail = %+v", defaultDetail)
	}
	anthropicDetail := state.Details["anthropic"]
	if anthropicDetail.ConfigSource != "api" || !anthropicDetail.Mutable {
		t.Fatalf("anthropic detail = %+v", anthropicDetail)
	}
	if anthropicDetail.APIKeysCount != 2 {
		t.Fatalf("anthropic detail api key pool = %d, want 2", anthropicDetail.APIKeysCount)
	}
	if matrix := state.CapabilityMatrices["anthropic"]; matrix.Source != "operator_contract" || matrix.SupportsStreaming {
		t.Fatalf("anthropic capability matrix = %+v", matrix)
	}
}

func TestLoadModelRouterProfilesPrefersOperatorSurface(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsRouterPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsOperatorRouterResponse{
			Profiles: []modelrouter.ProfileView{{
				ID:              "gpt-4o",
				Provider:        "default",
				Priority:        100,
				ContextWindow:   128000,
				MaxOutputTokens: 8192,
				Enabled:         true,
				Supports: map[modelrouter.Capability]bool{
					modelrouter.CapabilityChat:      true,
					modelrouter.CapabilityTools:     false,
					modelrouter.CapabilityStreaming: false,
				},
			}},
			Count:           1,
			DefaultProvider: "default",
		})
	}))
	defer srv.Close()

	profiles, err := loadModelRouterProfiles(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("loadModelRouterProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].ID != "gpt-4o" || profiles[0].Provider != "default" {
		t.Fatalf("profiles[0] = %+v", profiles[0])
	}
	if profiles[0].Supports[modelrouter.CapabilityTools] || profiles[0].Supports[modelrouter.CapabilityStreaming] {
		t.Fatalf("profiles[0].Supports = %#v", profiles[0].Supports)
	}
}

func TestLoadModelRouterProfilesFallsBackToOperatorModelState(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case modelsRouterPath:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		case modelsBasePath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(modelsOperatorListResponse{
				Providers: []modelsOperatorProvider{{
					Name:         "openai",
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					DefaultModel: "gpt-4o",
					HasKey:       true,
					Source:       "yaml",
					Mutable:      true,
					ConfigScope:  "providers",
					CapabilityMatrix: model.CapabilityMatrix{
						ProviderName:         "openai",
						ProviderAPI:          model.APIOpenAICompletions,
						Model:                "gpt-4o",
						SupportsSystemPrompt: true,
						SupportsTemperature:  true,
						SupportsMaxTokens:    true,
						SupportsTools:        false,
						SupportsStreaming:    false,
						Source:               "operator_contract",
					},
				}},
				Count:           1,
				DefaultProvider: "openai",
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	profiles, err := loadModelRouterProfiles(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("loadModelRouterProfiles() error = %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected router profiles from operator model fallback")
	}
	for _, profile := range profiles {
		if profile.ID != "gpt-4o" {
			continue
		}
		if profile.Supports[modelrouter.CapabilityTools] {
			t.Fatal("expected operator model fallback to keep tools=false")
		}
		if profile.Supports[modelrouter.CapabilityStreaming] {
			t.Fatal("expected operator model fallback to keep streaming=false")
		}
		return
	}
	t.Fatal("expected gpt-4o router profile")
}

func TestCapabilityMatrixForStatePrefersOperatorContract(t *testing.T) {

	entry := model.ProviderEntry{
		API:          model.APIOpenAICompletions,
		DefaultModel: "gpt-4o",
	}
	state := modelProviderState{
		Providers: map[string]model.ProviderEntry{
			"openai": entry,
		},
		CapabilityMatrices: map[string]model.CapabilityMatrix{
			"openai": {
				ProviderName:         "openai",
				ProviderAPI:          model.APIOpenAICompletions,
				Model:                "gpt-4o",
				SupportsSystemPrompt: true,
				SupportsTemperature:  true,
				SupportsMaxTokens:    true,
				SupportsTools:        false,
				SupportsStreaming:    false,
				Source:               "operator_contract",
			},
		},
	}

	matrix := capabilityMatrixForState(state, "openai", entry)
	if matrix.Source != "operator_contract" {
		t.Fatalf("Source = %q, want operator_contract", matrix.Source)
	}
	if matrix.SupportsTools {
		t.Fatal("expected operator contract to override local tool capability inference")
	}
	if matrix.SupportsStreaming {
		t.Fatal("expected operator contract to override local streaming capability inference")
	}
}

func TestRouterProfilesForStatePreferOperatorCapabilityContract(t *testing.T) {

	state := modelProviderState{
		Providers: map[string]model.ProviderEntry{
			"openai": {
				API:          model.APIOpenAICompletions,
				DefaultModel: "gpt-4o",
			},
		},
		DefaultProvider: "openai",
		CapabilityMatrices: map[string]model.CapabilityMatrix{
			"openai": {
				ProviderName:         "openai",
				ProviderAPI:          model.APIOpenAICompletions,
				Model:                "gpt-4o",
				SupportsSystemPrompt: true,
				SupportsTemperature:  true,
				SupportsMaxTokens:    true,
				SupportsTools:        false,
				SupportsStreaming:    false,
				Source:               "operator_contract",
			},
		},
	}

	profiles := routerProfilesForState(state)
	for _, profile := range profiles {
		if profile.ID != "gpt-4o" {
			continue
		}
		if profile.Supports[modelrouter.CapabilityTools] {
			t.Fatal("expected router profile tools support to honor operator contract override")
		}
		if profile.Supports[modelrouter.CapabilityStreaming] {
			t.Fatal("expected router profile streaming support to honor operator contract override")
		}
		return
	}
	t.Fatal("expected gpt-4o router profile")
}

func TestSortModelRowsByCatalog(t *testing.T) {

	rows := []modelsListRow{
		{Provider: "anthropic"},
		{Provider: "custom-z"},
		{Provider: "default"},
		{Provider: "openai"},
	}
	sortModelRowsByCatalog(rows, localCLISetupCatalog())

	got := []string{rows[0].Provider, rows[1].Provider, rows[2].Provider, rows[3].Provider}
	want := []string{"default", "openai", "anthropic", "custom-z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows order = %#v, want %#v", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// resolveTestTarget
// ---------------------------------------------------------------------------

func TestResolveTestTarget_DefaultProvider(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "gpt-4o"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "test-key"
    model: "gpt-4o"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	provider, modelID, err := resolveTestTarget(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveTestTarget: %v", err)
	}
	if provider != "default" {
		t.Errorf("expected default provider, got %q", provider)
	}
	if modelID != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", modelID)
	}
}

func TestResolveTestTarget_NamedProvider(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "anthropic/claude-sonnet-4-20250514"
models:
  default_provider: anthropic
  providers:
    anthropic:
      api: anthropic-messages
      api_key: "test-key"
      default_model: "claude-sonnet-4-20250514"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	provider, modelID, err := resolveTestTarget(context.Background(), nil, "anthropic")
	if err != nil {
		t.Fatalf("resolveTestTarget: %v", err)
	}
	if provider != "anthropic" {
		t.Errorf("expected anthropic, got %q", provider)
	}
	if modelID != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude-sonnet-4-20250514, got %q", modelID)
	}
}

func TestResolveTestTarget_NotFound(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "gpt-4o"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "test-key"
    model: "gpt-4o"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	_, _, err := resolveTestTarget(context.Background(), nil, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent provider")
	}
}

// ---------------------------------------------------------------------------
// providerOrDefault
// ---------------------------------------------------------------------------

func TestProviderOrDefault(t *testing.T) {

	if got := providerOrDefault(""); got != "default" {
		t.Errorf("expected 'default', got %q", got)
	}
	if got := providerOrDefault("anthropic"); got != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// modelsInfoResult JSON
// ---------------------------------------------------------------------------

func TestModelsInfoResult_JSON(t *testing.T) {

	r := modelsInfoResult{
		Name:         "test",
		API:          "openai-completions",
		BaseURL:      "https://api.example.com",
		DefaultModel: "gpt-4o",
		HasAPIKey:    true,
		APIKeysCount: 2,
		Timeout:      "30s",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"name", "api", "base_url", "default_model", "has_api_key", "api_keys_count", "timeout"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// modelsBenchResult JSON
// ---------------------------------------------------------------------------

func TestModelsBenchResult_JSON(t *testing.T) {

	r := modelsBenchResult{
		Provider: "test",
		Model:    "gpt-4o",
		MinMs:    100,
		AvgMs:    150,
		MaxMs:    200,
		Failures: 0,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"provider", "model", "min_ms", "avg_ms", "max_ms", "failures"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeTestCompletionRequest() modelsCompletionRequest {
	return modelsCompletionRequest{
		Model: "test-model",
		Messages: []modelsCompletionMessage{
			{Role: "user", Content: "Hello"},
		},
	}
}
