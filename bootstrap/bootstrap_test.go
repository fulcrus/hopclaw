package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controlpolicy "github.com/fulcrus/hopclaw/internal/controlplane/policypack"
	governance "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/mcp"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
	"gopkg.in/yaml.v3"
)

func TestBootstrapRegistersBrowserToolsWhenHostConfigured(t *testing.T) {
	t.Parallel()

	enabled := true
	browserd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer browserd.Close()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1:0",
		},
		Store: config.StoreConfig{
			Backend: "memory",
		},
		Agent: config.AgentConfig{
			DefaultModel:  "test-model",
			MaxToolRounds: 4,
			QueueMode:     "enqueue",
		},
		Skills: config.SkillsConfig{},
		Runtime: config.RuntimeConfig{
			Artifacts: config.ArtifactsConfig{
				Enabled:         &enabled,
				InlineThreshold: 8192,
				PreviewChars:    512,
			},
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            &enabled,
				Root:               ".",
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       256 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        &enabled,
				DefaultTimeout: 30 * time.Second,
			},
		},
		Hosts: config.HostsConfig{
			Browser: config.BrowserHostConfig{
				Enabled: &enabled,
				BaseURL: browserd.URL,
			},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "browser.navigate") {
		t.Fatalf("browser.navigate missing from tools: %#v", tools)
	}
	if !containsTool(tools, "browser.screenshot") {
		t.Fatalf("browser.screenshot missing from tools: %#v", tools)
	}
	browserOpen, ok := findToolDefinition(tools, "browser.open")
	if !ok {
		t.Fatalf("browser.open missing from tools: %#v", tools)
	}
	if browserOpen.Source != "capability" {
		t.Fatalf("browser.open source = %q, want capability", browserOpen.Source)
	}
	if containsTool(tools, "browser.create_session") {
		t.Fatalf("browser.create_session should not be listed in canonical tools: %#v", tools)
	}
}

func TestBootstrapRegistersDesktopToolsWhenHostConfigured(t *testing.T) {
	t.Parallel()

	enabled := true
	desktopd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer desktopd.Close()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1:0",
		},
		Store: config.StoreConfig{
			Backend: "memory",
		},
		Agent: config.AgentConfig{
			DefaultModel:  "test-model",
			MaxToolRounds: 4,
			QueueMode:     "enqueue",
		},
		Skills: config.SkillsConfig{},
		Runtime: config.RuntimeConfig{
			Artifacts: config.ArtifactsConfig{
				Enabled:         &enabled,
				InlineThreshold: 8192,
				PreviewChars:    512,
			},
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            &enabled,
				Root:               ".",
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       256 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        &enabled,
				DefaultTimeout: 30 * time.Second,
			},
		},
		Hosts: config.HostsConfig{
			Desktop: config.DesktopHostConfig{
				Enabled: &enabled,
				BaseURL: desktopd.URL,
			},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "desktop.open_app") {
		t.Fatalf("desktop.open_app missing from tools: %#v", tools)
	}
	if !containsTool(tools, "desktop.screenshot") {
		t.Fatalf("desktop.screenshot missing from tools: %#v", tools)
	}
	nodeScreenCapture, ok := findToolDefinition(tools, "nodes.screen_capture")
	if !ok {
		t.Fatalf("nodes.screen_capture missing from tools: %#v", tools)
	}
	if nodeScreenCapture.Source != "capability_bridge" {
		t.Fatalf("nodes.screen_capture source = %q, want capability_bridge", nodeScreenCapture.Source)
	}
	nodeClipboardRead, ok := findToolDefinition(tools, "nodes.clipboard_read")
	if !ok {
		t.Fatalf("nodes.clipboard_read missing from tools: %#v", tools)
	}
	if nodeClipboardRead.Source != "capability_bridge" {
		t.Fatalf("nodes.clipboard_read source = %q, want capability_bridge", nodeClipboardRead.Source)
	}
}

func TestBootstrapDefaultRuntimeContextEnablesWorkspaceSkillFromConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillBundleWithRequirements(t, filepath.Join(root, "skills", "openai-image-gen"), "openai-image-gen", "img.generate", map[string]any{
		"openclaw": map[string]any{
			"skillKey":   "ai.openai-image-gen",
			"primaryEnv": "OPENAI_API_KEY",
			"requires": map[string]any{
				"env":    []string{"OPENAI_API_KEY"},
				"config": []string{"models.openai_compat.model"},
			},
		},
	})

	enabled := true
	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1:0",
		},
		Store: config.StoreConfig{
			Backend: "memory",
		},
		Agent: config.AgentConfig{
			DefaultModel:  "gpt-4o",
			MaxToolRounds: 4,
			QueueMode:     "enqueue",
		},
		Skills: config.SkillsConfig{
			AutoDetect: true,
		},
		Models: config.ModelsConfig{
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-configured",
				Model:   "gpt-4o",
			},
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            &enabled,
				Root:               root,
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       256 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        &enabled,
				DefaultTimeout: 30 * time.Second,
			},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tools, err := app.Runtime.ListTools(context.Background(), "skill-session")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "img.generate") {
		t.Fatalf("img.generate missing from tools: %#v", tools)
	}
}

func TestBootstrapActivatesPluginHooksMCPAndAgents(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	defer testRefreshGlobalsMu.Unlock()

	enabled := true
	pluginDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hooks", "notify.yaml"), []byte(`
hooks:
  - name: notify
    trigger: run.completed
    kind: http
    url: https://example.com/hook
    async: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(hook) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hopclaw.plugin.yaml"), []byte(`
name: demo
hooks_dir: hooks
mcp_servers:
  server:
    command: demo-mcp
agents:
  ops:
    description: Ops preset
    model: gpt-5
    tools: [fs.read]
    skills: [summarize]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	previousFactory := newPluginMCPRuntime
	newPluginMCPRuntime = func(_ []mcp.ServerConfig) pluginMCPRuntime {
		return &stubPluginMCPRuntime{
			tools: []mcp.Tool{{
				Name:        "demo.server__search",
				Description: "Search docs",
				InputSchema: map[string]any{"type": "object"},
			}},
		}
	}
	defer func() { newPluginMCPRuntime = previousFactory }()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent: config.AgentConfig{
			DefaultModel:  "test-model",
			MaxToolRounds: 4,
			QueueMode:     "enqueue",
		},
		Skills: config.SkillsConfig{},
		Runtime: config.RuntimeConfig{
			Artifacts: config.ArtifactsConfig{
				Enabled:         &enabled,
				InlineThreshold: 8192,
				PreviewChars:    512,
			},
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				Enabled:            &enabled,
				Root:               ".",
				DefaultExecTimeout: 30 * time.Second,
				MaxReadBytes:       256 * 1024,
			},
			LocalExec: config.LocalExecConfig{
				Enabled:        &enabled,
				DefaultTimeout: 30 * time.Second,
			},
		},
		Plugins: config.PluginsConfig{
			Enabled: &enabled,
			Dirs:    []string{pluginDir},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "demo.server__search") {
		t.Fatalf("demo.server__search missing from tools: %#v", tools)
	}

	router := app.Runtime.AgentRouter()
	if router == nil {
		t.Fatal("expected plugin agent router")
	}
	profile, ok := router.Get("demo/ops")
	if !ok {
		t.Fatal("expected plugin agent profile")
	}
	if profile.Model != "gpt-5" {
		t.Fatalf("profile.Model = %q", profile.Model)
	}

	hooks, err := app.HookExecutor.Store().List(context.Background())
	if err != nil {
		t.Fatalf("HookExecutor.Store().List() error = %v", err)
	}
	if len(hooks) != 1 || hooks[0].Name != "notify" {
		t.Fatalf("unexpected plugin hooks: %#v", hooks)
	}
}

func TestBootstrapInitializesEffectiveResolverWithoutConfigStore(t *testing.T) {
	t.Parallel()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	if app.EffectiveConfigResolver() == nil {
		t.Fatal("expected non-nil effective config resolver")
	}
	if snapshot := app.Runtime.EffectiveConfigSnapshot(); snapshot == nil {
		t.Fatal("expected runtime effective config snapshot")
	}
}

func TestBootstrapRefreshEffectiveConfigRebuildsTools(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if containsTool(tools, "demo.remote") {
		t.Fatalf("demo.remote unexpectedly present before refresh: %#v", tools)
	}

	next := cfg
	next.Tools.External = []config.ExternalToolConfig{{
		Name:        "demo.remote",
		Description: "Demo external tool",
		Endpoint:    "http://127.0.0.1/tool",
	}}
	next.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), next); err != nil {
		t.Fatalf("ApplyBaseConfig() error = %v", err)
	}

	tools, err = app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() after refresh error = %v", err)
	}
	if !containsTool(tools, "demo.remote") {
		t.Fatalf("demo.remote missing after refresh: %#v", tools)
	}
}

func TestBootstrapRefreshEffectiveConfigRebuildsHosts(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	browserA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1/profiles":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "alpha"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer browserA.Close()
	browserB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1/profiles":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "beta"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer browserB.Close()
	desktopA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer desktopA.Close()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
		Hosts: config.HostsConfig{
			Browser: config.BrowserHostConfig{Enabled: &enabled, BaseURL: browserA.URL},
			Desktop: config.DesktopHostConfig{Enabled: &enabled, BaseURL: desktopA.URL},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/operator/browser/profiles", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/browser/profiles status = %d body=%s", rec.Code, rec.Body.String())
	}
	var browserProfiles struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&browserProfiles); err != nil {
		t.Fatalf("decode browser profiles: %v", err)
	}
	if browserProfiles.Count != 1 || browserProfiles.Items[0].Name != "alpha" {
		t.Fatalf("unexpected browser profiles payload: %#v", browserProfiles)
	}
	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "desktop.open_app") {
		t.Fatalf("desktop.open_app missing before host refresh: %#v", tools)
	}

	next := cfg
	next.Hosts.Browser = config.BrowserHostConfig{Enabled: &enabled, BaseURL: browserB.URL}
	next.Hosts.Desktop = config.DesktopHostConfig{Enabled: &disabled}
	next.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), next); err != nil {
		t.Fatalf("ApplyBaseConfig() error = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/operator/browser/profiles", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/browser/profiles after refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&browserProfiles); err != nil {
		t.Fatalf("decode browser profiles after refresh: %v", err)
	}
	if browserProfiles.Count != 1 || browserProfiles.Items[0].Name != "beta" {
		t.Fatalf("unexpected browser profiles payload after refresh: %#v", browserProfiles)
	}
	tools, err = app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() after host refresh error = %v", err)
	}
	if containsTool(tools, "desktop.open_app") {
		t.Fatalf("desktop.open_app still present after host disable: %#v", tools)
	}
	if _, ok := app.Capabilities.Get("desktop"); ok {
		t.Fatal("desktop capability still registered after host disable")
	}
	if _, ok := app.Capabilities.Get("browser"); !ok {
		t.Fatal("browser capability missing after browser host refresh")
	}
}

func TestBootstrapNewProducesIntegrityCompleteApp(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	if err := app.ValidateIntegrity(); err != nil {
		t.Fatalf("ValidateIntegrity() error = %v", err)
	}
}

func TestBootstrapRefreshEffectiveConfigRebuildsChannels(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	if len(app.Channels.Names()) != 0 {
		t.Fatalf("Channels before refresh = %#v, want none", app.Channels.Names())
	}

	next := cfg
	next.Channels.Webhook = config.WebhookChannelConfig{
		Enabled: boolPtr(true),
		Instances: map[string]config.WebhookInstanceConfig{
			"ops": {
				CallbackURL: "https://example.com/callback",
				Secret:      "shared-secret",
			},
		},
	}
	next.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), next); err != nil {
		t.Fatalf("ApplyBaseConfig() error = %v", err)
	}

	if !containsString(app.Channels.Names(), "webhook:ops") {
		t.Fatalf("Channels after refresh = %#v, want webhook:ops", app.Channels.Names())
	}
	if _, ok := app.Webhooks["ops"]; !ok {
		t.Fatalf("Webhooks after refresh = %#v, want ops adapter", app.Webhooks)
	}
}

func TestBootstrapApplyBaseConfigRollsBackOnChannelRefreshFailure(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	defer testRefreshGlobalsMu.Unlock()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	originalBuildBuiltinChannels := buildBuiltinChannels
	buildBuiltinChannels = func(context.Context, channelregistry.RuntimeDeps) (channelregistry.RuntimeBuildResult, error) {
		return channelregistry.RuntimeBuildResult{}, fmt.Errorf("boom")
	}
	defer func() {
		buildBuiltinChannels = originalBuildBuiltinChannels
	}()

	next := cfg
	next.Channels.Webhook = config.WebhookChannelConfig{
		Enabled: boolPtr(true),
		Instances: map[string]config.WebhookInstanceConfig{
			"ops": {
				CallbackURL: "https://example.com/callback",
				Secret:      "shared-secret",
			},
		},
	}
	next.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), next); err == nil {
		t.Fatal("ApplyBaseConfig() error = nil, want failure")
	}
	if app.BaseConfig.Channels.Webhook.Enabled != nil {
		t.Fatalf("BaseConfig unexpectedly changed: %#v", app.BaseConfig.Channels.Webhook)
	}
	if len(app.Channels.Names()) != 0 {
		t.Fatalf("Channels after failed refresh = %#v, want none", app.Channels.Names())
	}
	if app.Config.Channels.Webhook.Enabled != nil {
		t.Fatalf("Config unexpectedly changed: %#v", app.Config.Channels.Webhook)
	}
	if resolver := app.EffectiveConfigResolver(); resolver != nil {
		if current := resolver.Current(); current.Channels.Webhook.Enabled != nil {
			t.Fatalf("effective config unexpectedly changed: %#v", current.Channels.Webhook)
		}
	}
}

func TestBootstrapApplyBaseConfigRollsBackLiveRuntimeOnPostApplyFailure(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	defer testRefreshGlobalsMu.Unlock()

	enabled := true
	disabled := false

	browserA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1/profiles":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "alpha"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer browserA.Close()

	browserB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1/profiles":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "beta"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer browserB.Close()

	desktopA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer desktopA.Close()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{RefreshInterval: 50 * time.Millisecond},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
		Hosts: config.HostsConfig{
			Browser: config.BrowserHostConfig{Enabled: &enabled, BaseURL: browserA.URL},
			Desktop: config.DesktopHostConfig{Enabled: &enabled, BaseURL: desktopA.URL},
		},
		Channels: config.ChannelsConfig{
			Webhook: config.WebhookChannelConfig{
				Enabled: boolPtr(true),
				Instances: map[string]config.WebhookInstanceConfig{
					"ops": {
						CallbackURL: "https://example.com/callback",
						Secret:      "shared-secret",
					},
				},
			},
		},
	}

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	oldBaseConfig := app.BaseConfig
	oldEffectiveConfig := app.Config
	oldSkillService := app.SkillService
	oldManagedHelpers := app.ManagedHelpers
	oldToolExec := app.toolRuntime.current()
	oldChannels := app.Channels
	oldWebhook := app.Webhooks["ops"]

	validateOriginal := validateEffectiveConfigRefresh
	validateEffectiveConfigRefresh = func(context.Context, *App) error {
		return fmt.Errorf("boom")
	}
	defer func() {
		validateEffectiveConfigRefresh = validateOriginal
	}()

	next := cfg
	next.Skills.RefreshInterval = 2 * time.Second
	next.Tools.External = []config.ExternalToolConfig{{
		Name:        "demo.remote",
		Description: "Demo external tool",
		Endpoint:    "http://127.0.0.1/tool",
	}}
	next.Hosts.Browser = config.BrowserHostConfig{Enabled: &enabled, BaseURL: browserB.URL}
	next.Hosts.Desktop = config.DesktopHostConfig{Enabled: &disabled}
	next.Channels.Webhook = config.WebhookChannelConfig{
		Enabled: boolPtr(true),
		Instances: map[string]config.WebhookInstanceConfig{
			"ops": {
				CallbackURL: "https://example.com/next",
				Secret:      "next-secret",
			},
		},
	}
	next.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), next); err == nil {
		t.Fatal("ApplyBaseConfig() error = nil, want failure")
	}

	if app.BaseConfig.Hosts.Browser.BaseURL != oldBaseConfig.Hosts.Browser.BaseURL {
		t.Fatalf("BaseConfig browser host = %q, want %q", app.BaseConfig.Hosts.Browser.BaseURL, oldBaseConfig.Hosts.Browser.BaseURL)
	}
	if app.Config.Hosts.Browser.BaseURL != oldEffectiveConfig.Hosts.Browser.BaseURL {
		t.Fatalf("Config browser host = %q, want %q", app.Config.Hosts.Browser.BaseURL, oldEffectiveConfig.Hosts.Browser.BaseURL)
	}
	if app.SkillService != oldSkillService {
		t.Fatal("skill service changed after failed refresh")
	}
	if app.ManagedHelpers != oldManagedHelpers {
		t.Fatal("managed helpers changed after failed refresh")
	}
	if got := app.toolRuntime.current(); got != oldToolExec {
		t.Fatal("tool runtime changed after failed refresh")
	}
	if app.Channels != oldChannels {
		t.Fatal("channel manager changed after failed refresh")
	}
	if got := app.Webhooks["ops"]; got != oldWebhook {
		t.Fatal("webhook adapter changed after failed refresh")
	}
	if _, ok := app.Capabilities.Get("desktop"); !ok {
		t.Fatal("desktop capability missing after failed refresh")
	}

	req := httptest.NewRequest(http.MethodGet, "/operator/browser/profiles", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/browser/profiles after rollback status = %d body=%s", rec.Code, rec.Body.String())
	}

	var browserProfiles struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&browserProfiles); err != nil {
		t.Fatalf("decode browser profiles after rollback: %v", err)
	}
	if browserProfiles.Count != 1 || browserProfiles.Items[0].Name != "alpha" {
		t.Fatalf("unexpected browser profiles payload after rollback: %#v", browserProfiles)
	}

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() after rollback error = %v", err)
	}
	if !containsTool(tools, "desktop.open_app") {
		t.Fatalf("desktop.open_app missing after rollback: %#v", tools)
	}
	if containsTool(tools, "demo.remote") {
		t.Fatalf("demo.remote unexpectedly present after rollback: %#v", tools)
	}
	if !containsString(app.Channels.Names(), "webhook:ops") {
		t.Fatalf("Channels after rollback = %#v, want webhook:ops", app.Channels.Names())
	}
}

func containsTool(tools []agent.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func findToolDefinition(tools []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return agent.ToolDefinition{}, false
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestDefaultDiscoveryRootsIncludeBundleDirs(t *testing.T) {
	t.Parallel()

	workspaceRoot := "/tmp/hopclaw-workspace"
	roots := DefaultDiscoveryRoots(workspaceRoot)

	paths := make(map[string]bool, len(roots))
	for _, root := range roots {
		paths[root.Path] = true
	}

	if !paths[filepath.Join(workspaceRoot, ".hopclaw", "bundles")] {
		t.Fatal("workspace .hopclaw bundles root missing")
	}
	if !paths[filepath.Join(workspaceRoot, "bundles")] {
		t.Fatal("workspace bundles root missing")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir(): %v", err)
	}
	if !paths[filepath.Join(home, ".hopclaw", "bundles")] {
		t.Fatal("user bundles root missing")
	}
}

func TestDefaultPolicyConfigForProfiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile string
		want    policy.Config
	}{
		{
			name:    "desktop",
			profile: config.RuntimeProfileDesktop,
			want: policy.Config{
				AllowUnknownTools:        true,
				RequireApprovalForWrite:  true,
				RequireApprovalCommunity: true,
				DenyDestructive:          false,
				DefaultApprovalScope:     approval.ScopeOnce,
				MaxApprovalScope:         approval.ScopeSession,
				SkillInstallDefaultScope: approval.ScopeOnce,
				SkillInstallMaxScope:     approval.ScopeOnce,
			},
		},
		{
			name:    "trusted desktop",
			profile: config.RuntimeProfileTrustedDesktop,
			want: policy.Config{
				AllowUnknownTools:              true,
				RequireApprovalForWrite:        true,
				AllowLocalWriteWithoutApproval: true,
				RequireApprovalCommunity:       false,
				SkipManifestApproval:           true,
				DenyDestructive:                false,
				DefaultApprovalScope:           approval.ScopeOnce,
				MaxApprovalScope:               approval.ScopeSession,
				SkillInstallDefaultScope:       approval.ScopeOnce,
				SkillInstallMaxScope:           approval.ScopeOnce,
			},
		},
		{
			name:    "production",
			profile: config.RuntimeProfileProduction,
			want: policy.Config{
				AllowUnknownTools:        true,
				RequireApprovalForWrite:  true,
				RequireApprovalCommunity: true,
				DenyDestructive:          true,
				DefaultApprovalScope:     approval.ScopeOnce,
				MaxApprovalScope:         approval.ScopeSession,
				SkillInstallDefaultScope: approval.ScopeOnce,
				SkillInstallMaxScope:     approval.ScopeOnce,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultPolicyConfig(tc.profile, config.SkillsConfig{})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("defaultPolicyConfig(%q) = %#v, want %#v", tc.profile, got, tc.want)
			}
		})
	}
}

func TestDefaultPolicyConfigCarriesSkillInstallPolicy(t *testing.T) {
	t.Parallel()

	got := defaultPolicyConfig(config.RuntimeProfileDesktop, config.SkillsConfig{InstallPolicy: config.SkillInstallPolicyAuto})
	if got.SkillInstallPolicy != config.SkillInstallPolicyAuto {
		t.Fatalf("got.SkillInstallPolicy = %q", got.SkillInstallPolicy)
	}
}

func TestResolveDefaultPolicyExposesPackChain(t *testing.T) {
	t.Parallel()

	got := resolveDefaultPolicy(config.RuntimeProfileProduction, config.SkillsConfig{InstallPolicy: config.SkillInstallPolicyAsk})
	if got.ProfileID != "default-production" {
		t.Fatalf("ProfileID = %q", got.ProfileID)
	}
	if !reflect.DeepEqual(got.PackIDs(), []string{
		controlpolicy.PackBaseCore,
		controlpolicy.PackProductionDefault,
	}) {
		t.Fatalf("PackIDs() = %#v", got.PackIDs())
	}
}

func TestBootstrapWiresGovernanceAdapters(t *testing.T) {
	t.Parallel()

	adapter := &bootstrapObservedGovernanceAdapter{}
	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 1, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:              &bootstrapModelClient{},
		Tools:              newBootstrapCountingToolExecutor("fs.read", "read"),
		GovernanceAdapters: []controlgov.Adapter{adapter},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.Runtime.EffectiveConfigSnapshot()
	if snapshot == nil {
		t.Fatal("expected effective config snapshot")
	}
	if err := app.Bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventSecurityRiskDetected,
		RunID:     "run-gov-1",
		SessionID: "session-gov-1",
		Attrs: map[string]any{
			"severity":                     "high",
			"summary":                      "test governance event",
			"effective_config_snapshot_id": snapshot.ID,
			"scope": map[string]any{
				"automation_id": "bootstrap-governance",
			},
		},
	}); err != nil {
		t.Fatalf("Bus.Publish() error = %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		records := adapter.snapshot()
		if len(records) == 1 {
			if records[0].Snapshot == nil || records[0].Snapshot.ID != snapshot.ID {
				t.Fatalf("record.Snapshot = %#v", records[0].Snapshot)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("len(records) = %d, want 1", len(adapter.snapshot()))
}

func TestBootstrapWiresApprovalProviders(t *testing.T) {
	t.Parallel()

	provider := &bootstrapObservedApprovalProvider{}
	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model: &bootstrapModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{ID: "call-appr-provider", Name: "fs.read"}},
			}},
		},
		Tools: newBootstrapCountingToolExecutor("fs.read", "read"),
		Policy: &bootstrapStaticPolicyEngine{
			decision: policy.Decision{Action: policy.ActionRequireApproval, Summary: "approval required"},
		},
		ApprovalProviders: []controlapproval.Provider{provider},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "approval-provider",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run.ID, agent.RunWaitingApproval)

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List() error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending) = %d, want 1", len(pending))
	}
	if _, err := app.Runtime.ResolveApproval(context.Background(), pending[0].ID, approval.Resolution{
		Status:     approval.StatusDenied,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if provider.submitCount() > 0 && provider.updateCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider counts submit=%d update=%d", provider.submitCount(), provider.updateCount())
}

func TestBootstrapBuildsConfiguredWebhookApprovalProvider(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var submitCount, updateCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Header.Get("X-HopClaw-Approval-Op") {
		case "submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"external_id": "jira-500",
				"status":      "submitted",
			})
		case "update":
			updateCount++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected approval op %q", r.Header.Get("X-HopClaw-Approval-Op"))
		}
	}))
	defer server.Close()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		ExecApproval: config.ExecApprovalConfig{
			Providers: []config.ApprovalProviderConfig{{
				Name: "jira",
				Webhook: config.ApprovalWebhookProviderConfig{
					SubmitURL: server.URL,
					UpdateURL: server.URL,
					Timeout:   5 * time.Second,
				},
			}},
		},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model: &bootstrapModelClient{
			responses: []*agent.ModelResponse{{
				ToolCalls: []agent.ToolCall{{ID: "call-appr-provider-webhook", Name: "fs.read"}},
			}},
		},
		Tools: newBootstrapCountingToolExecutor("fs.read", "read"),
		Policy: &bootstrapStaticPolicyEngine{
			decision: policy.Decision{Action: policy.ActionRequireApproval, Summary: "approval required"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "approval-provider-webhook",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run.ID, agent.RunWaitingApproval)

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List() error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending) = %d, want 1", len(pending))
	}
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(pending[0].External) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
		updated, err := app.Approvals.Get(context.Background(), pending[0].ID)
		if err == nil {
			pending[0] = updated
		}
	}
	if len(pending[0].External) != 1 || pending[0].External[0].Provider != "jira" || pending[0].External[0].ExternalID != "jira-500" {
		t.Fatalf("pending external refs = %#v", pending[0].External)
	}

	if _, err := app.Runtime.ResolveApproval(context.Background(), pending[0].ID, approval.Resolution{
		Status:     approval.StatusDenied,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		gotSubmit, gotUpdate := submitCount, updateCount
		mu.Unlock()
		if gotSubmit > 0 && gotUpdate > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("webhook provider counts submit=%d update=%d", submitCount, updateCount)
}

func TestBootstrapBuildsConfiguredGovernanceWebhookAdapter(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if got := strings.TrimSpace(r.Header.Get("X-HopClaw-Governance-Adapter")); got != "audit-hub" {
			t.Fatalf("adapter header = %q", got)
		}
		if got := strings.TrimSpace(r.Header.Get("X-HopClaw-Governance-Kind")); got != "security_event" {
			t.Fatalf("kind header = %q", got)
		}
		var payload controlgov.WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Adapter != "audit-hub" {
			t.Fatalf("payload.Adapter = %q", payload.Adapter)
		}
		if payload.Record.Kind != controlgov.KindSecurityEvent {
			t.Fatalf("payload.Record.Kind = %q", payload.Record.Kind)
		}
		if payload.Record.Snapshot == nil || len(payload.Record.Snapshot.GovernanceAdapterNames) != 1 || payload.Record.Snapshot.GovernanceAdapterNames[0] != "audit-hub" {
			t.Fatalf("payload.Record.Snapshot = %#v", payload.Record.Snapshot)
		}
		mu.Lock()
		seen = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 1, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{
			Governance: config.GovernanceConfig{
				Adapters: []config.GovernanceAdapterConfig{{
					Name: "audit-hub",
					Webhook: config.GovernanceWebhookAdapterConfig{
						URL:             server.URL,
						Timeout:         5 * time.Second,
						IncludeSnapshot: boolPtr(true),
						Kinds:           []string{"security_event"},
					},
				}},
			},
		},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model: &bootstrapModelClient{},
		Tools: newBootstrapCountingToolExecutor("fs.read", "read"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.Runtime.EffectiveConfigSnapshot()
	if snapshot == nil || len(snapshot.GovernanceAdapterNames) != 1 || snapshot.GovernanceAdapterNames[0] != "audit-hub" {
		t.Fatalf("snapshot.GovernanceAdapterNames = %#v", snapshot)
	}
	if err := app.Bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventSecurityRiskDetected,
		RunID:     "run-gov-webhook-1",
		SessionID: "session-gov-webhook-1",
		Attrs: map[string]any{
			"severity":                     "high",
			"summary":                      "test governance webhook event",
			"effective_config_snapshot_id": snapshot.ID,
		},
	}); err != nil {
		t.Fatalf("Bus.Publish() error = %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := seen
		mu.Unlock()
		if got {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected configured governance webhook adapter request")
}

func TestBootstrapGovernanceDeliveryRetriesConfiguredWebhook(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	deliveryRoot := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "retry", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 1, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{
			Governance: config.GovernanceConfig{
				Delivery: config.GovernanceDeliveryConfig{
					Backend:      "jsonl",
					Path:         deliveryRoot,
					MaxAttempts:  3,
					BaseBackoff:  10 * time.Millisecond,
					MaxBackoff:   10 * time.Millisecond,
					PollInterval: 5 * time.Millisecond,
					BatchSize:    8,
				},
				Adapters: []config.GovernanceAdapterConfig{{
					Name: "audit-hub",
					Webhook: config.GovernanceWebhookAdapterConfig{
						URL:             server.URL,
						Timeout:         500 * time.Millisecond,
						IncludeSnapshot: boolPtr(true),
					},
				}},
			},
		},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model: &bootstrapModelClient{},
		Tools: newBootstrapCountingToolExecutor("fs.read", "read"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	snapshot := app.Runtime.EffectiveConfigSnapshot()
	if err := app.Bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventSecurityRiskDetected,
		RunID:     "run-gov-retry-1",
		SessionID: "session-gov-retry-1",
		Attrs: map[string]any{
			"severity":                     "high",
			"summary":                      "retry configured governance webhook",
			"effective_config_snapshot_id": snapshot.ID,
		},
	}); err != nil {
		t.Fatalf("Bus.Publish() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if attempts.Load() >= 2 {
			entries, err := os.ReadDir(filepath.Join(deliveryRoot, "governance_deliveries"))
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) == 0 {
				t.Fatal("expected governance delivery journal entries")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected governance delivery retry, attempts=%d", attempts.Load())
}

func TestHookEventSinkFiresGovernanceDeliveryHook(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		received map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		mu.Lock()
		received = payload
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "governance-dead-letter",
		Enabled: true,
		Trigger: hooks.TriggerGovernanceDeliveryDeadLettered,
		Kind:    hooks.KindHTTP,
		URL:     server.URL,
	}); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	executor := hooks.NewExecutor(store)
	sink := &hookEventSink{executor: executor}
	eventTime := time.Now().UTC().Truncate(time.Second)
	if err := sink.Handle(context.Background(), eventbus.Event{
		ID:        "evt-gov-hook-1",
		Type:      eventbus.EventGovernanceDeliveryDeadLettered,
		RunID:     "run-gov-hook-1",
		SessionID: "sess-gov-hook-1",
		Time:      eventTime,
		Attrs: map[string]any{
			"delivery_id":     "gdel-hook-1",
			"adapter_name":    "audit-hub",
			"delivery_status": "dead_letter",
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	results := executor.RecentResults(1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerGovernanceDeliveryDeadLettered {
		t.Fatalf("result.Trigger = %q", results[0].Trigger)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := testStringValue(received["delivery_id"]); got != "gdel-hook-1" {
		t.Fatalf("delivery_id = %q, want gdel-hook-1", got)
	}
	if got := testStringValue(received["event_id"]); got != "evt-gov-hook-1" {
		t.Fatalf("event_id = %q, want evt-gov-hook-1", got)
	}
	if got := testStringValue(received["event_type"]); got != string(eventbus.EventGovernanceDeliveryDeadLettered) {
		t.Fatalf("event_type = %q", got)
	}
}

func TestHookEventSinkFiresApprovalResolvedHook(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		received map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		mu.Lock()
		received = payload
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := hooks.NewInMemoryStore()
	if _, err := store.Add(context.Background(), hooks.Hook{
		Name:    "approval-resolved-callback",
		Enabled: true,
		Trigger: hooks.TriggerApprovalResolved,
		Kind:    hooks.KindHTTP,
		URL:     server.URL,
	}); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	executor := hooks.NewExecutor(store)
	sink := &hookEventSink{executor: executor}
	eventTime := time.Now().UTC().Truncate(time.Second)
	if err := sink.Handle(context.Background(), eventbus.Event{
		ID:        "evt-approval-hook-1",
		Type:      eventbus.EventApprovalResolved,
		RunID:     "run-approval-hook-1",
		SessionID: "sess-approval-hook-1",
		Time:      eventTime,
		Attrs: map[string]any{
			"approval_id":   "appr-123",
			"approval_kind": "tool_calls",
			"status":        "approved",
			"resolved_by":   "security-reviewer",
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	results := executor.RecentResults(1)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Trigger != hooks.TriggerApprovalResolved {
		t.Fatalf("result.Trigger = %q", results[0].Trigger)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := testStringValue(received["approval_id"]); got != "appr-123" {
		t.Fatalf("approval_id = %q, want appr-123", got)
	}
	if got := testStringValue(received["status"]); got != "approved" {
		t.Fatalf("status = %q, want approved", got)
	}
	if got := testStringValue(received["event_type"]); got != string(eventbus.EventApprovalResolved) {
		t.Fatalf("event_type = %q", got)
	}
}

func testStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func writeSkillBundleWithRequirements(t *testing.T, dir, name, toolName string, metadata map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	frontmatter := map[string]any{
		"name":        name,
		"description": "test skill",
		"metadata":    metadata,
	}
	fmBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		t.Fatalf("yaml.Marshal(frontmatter): %v", err)
	}
	skillDoc := "---\n" + string(fmBytes) + "---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name":              toolName,
			"side_effect_class": "read",
			"idempotent":        true,
			"execution_key":     "session:{id}",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
		"security": map[string]any{
			"trust": "community",
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.manifest.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh): %v", err)
	}
}

func TestPluginDisableEnableRefreshesRuntimeTools(t *testing.T) {
	t.Parallel()

	enabled := true
	pluginRoot := t.TempDir()
	pluginDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	manifest := `name: demo
version: 1.0.0
description: demo plugin
tools:
  - name: demo.echo
    description: demo echo
    endpoint: http://127.0.0.1/tool
`
	if err := os.WriteFile(filepath.Join(pluginDir, "hopclaw.plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
		Plugins: config.PluginsConfig{Enabled: &enabled, Dirs: []string{pluginRoot}},
	}, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	tools, err := app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if !containsTool(tools, "demo.echo") {
		t.Fatalf("demo.echo missing from tools: %#v", tools)
	}
	handler := app.Gateway.Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/operator/plugins/demo/disable", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", rec.Code, rec.Body.String())
	}

	tools, err = app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() after disable error = %v", err)
	}
	if containsTool(tools, "demo.echo") {
		t.Fatalf("demo.echo still present after disable: %#v", tools)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/operator/plugins/demo/enable", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", rec.Code, rec.Body.String())
	}

	tools, err = app.Runtime.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() after enable error = %v", err)
	}
	if !containsTool(tools, "demo.echo") {
		t.Fatalf("demo.echo missing after enable: %#v", tools)
	}
}

func TestBootstrapWiresPolicyDependenciesIntoCustomEngine(t *testing.T) {
	t.Parallel()

	engine := &bootstrapObservedPolicyEngine{}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{Profile: config.RuntimeProfileDesktop},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:  &bootstrapModelClient{},
		Tools:  &bootstrapCountingToolExecutor{},
		Policy: engine,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	if engine.grantStore == nil {
		t.Fatal("expected bootstrap to wire grant store into custom policy engine")
	}
	if engine.securityAuditor == nil {
		t.Fatal("expected bootstrap to wire security auditor into custom policy engine")
	}
	if app.GrantStore == nil || engine.grantStore != app.GrantStore {
		t.Fatalf("grant store wiring mismatch: engine=%p app=%p", engine.grantStore, app.GrantStore)
	}
}

func TestBootstrapOverlayAllowStillRequiresBaseApproval(t *testing.T) {
	t.Parallel()

	model := &bootstrapModelClient{
		responses: []*agent.ModelResponse{{
			ToolCalls: []agent.ToolCall{{
				ID:   "call-overlay-allow",
				Name: "fs.write",
			}},
		}},
	}
	tools := newBootstrapCountingToolExecutor("fs.write", "local_write")
	overlay := &bootstrapStaticPolicyEngine{
		decision: policy.Decision{
			Action:       policy.ActionAllow,
			PolicySource: "test.policy/overlay_allow",
			Summary:      "overlay explicitly allows the tool",
		},
	}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{Profile: config.RuntimeProfileDesktop},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:  model,
		Tools:  tools,
		Policy: overlay,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "overlay-allow-session",
		Content:    "write the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run = waitForBootstrapRunStatus(t, app, run.ID, agent.RunWaitingApproval)

	if run.Governance == nil {
		t.Fatal("expected governance evaluation on run")
	}
	if run.Governance.Decision.Action != policy.ActionRequireApproval {
		t.Fatalf("run.Governance.Decision.Action = %q, want %q", run.Governance.Decision.Action, policy.ActionRequireApproval)
	}
	wantLayers := []string{"policy.default_engine/rules", "test.policy/overlay_allow"}
	if !reflect.DeepEqual(run.Governance.Decision.PolicyLayers, wantLayers) {
		t.Fatalf("run.Governance.Decision.PolicyLayers = %#v, want %#v", run.Governance.Decision.PolicyLayers, wantLayers)
	}

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List(pending) error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending approvals) = %d, want 1", len(pending))
	}
	if got := tools.calls.Load(); got != 0 {
		t.Fatalf("tool executor calls = %d, want 0 while approval is pending", got)
	}
}

func TestBootstrapOverlayDenyOverridesBaseApproval(t *testing.T) {
	t.Parallel()

	model := &bootstrapModelClient{
		responses: []*agent.ModelResponse{{
			ToolCalls: []agent.ToolCall{{
				ID:   "call-overlay-deny",
				Name: "fs.write",
			}},
		}},
	}
	tools := newBootstrapCountingToolExecutor("fs.write", "local_write")
	overlay := &bootstrapStaticPolicyEngine{
		decision: policy.Decision{
			Action:       policy.ActionDeny,
			Reasons:      []string{"overlay blocked write access"},
			PolicySource: "test.policy/overlay_deny",
			Summary:      "denied by overlay policy",
		},
	}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{Profile: config.RuntimeProfileDesktop},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:  model,
		Tools:  tools,
		Policy: overlay,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "overlay-deny-session",
		Content:    "write the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run = waitForBootstrapRunStatus(t, app, run.ID, agent.RunFailed)

	if run.Governance == nil {
		t.Fatal("expected governance evaluation on run")
	}
	if run.Governance.Decision.Action != policy.ActionDeny {
		t.Fatalf("run.Governance.Decision.Action = %q, want %q", run.Governance.Decision.Action, policy.ActionDeny)
	}
	if run.Governance.Decision.PolicySource != "test.policy/overlay_deny" {
		t.Fatalf("run.Governance.Decision.PolicySource = %q", run.Governance.Decision.PolicySource)
	}
	wantLayers := []string{"policy.default_engine/rules", "test.policy/overlay_deny"}
	if !reflect.DeepEqual(run.Governance.Decision.PolicyLayers, wantLayers) {
		t.Fatalf("run.Governance.Decision.PolicyLayers = %#v, want %#v", run.Governance.Decision.PolicyLayers, wantLayers)
	}
	if !strings.Contains(run.Error, "overlay blocked write access") {
		t.Fatalf("run.Error = %q, want overlay deny reason", run.Error)
	}

	allApprovals, err := app.Approvals.List(context.Background(), approval.ListFilter{})
	if err != nil {
		t.Fatalf("Approvals.List(all) error = %v", err)
	}
	if len(allApprovals) != 0 {
		t.Fatalf("len(all approvals) = %d, want 0", len(allApprovals))
	}
	if got := tools.calls.Load(); got != 0 {
		t.Fatalf("tool executor calls = %d, want 0 when denied by overlay", got)
	}
}

func TestBootstrapApprovalPolicyDefaultScopeAppliesSessionGrant(t *testing.T) {
	t.Parallel()

	model := &bootstrapModelClient{
		responses: []*agent.ModelResponse{
			{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-policy-default-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "first read complete",
				},
			},
			{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-policy-default-2",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "second read complete",
				},
			},
		},
	}
	tools := newBootstrapCountingToolExecutor("fs.read", "read")
	overlay := &bootstrapStaticPolicyEngine{
		decision: policy.Decision{
			Action:       policy.ActionRequireApproval,
			Reasons:      []string{"enterprise policy requires approval"},
			PolicySource: "test.policy/approval_scope",
			Summary:      "approval required by enterprise policy",
			ApprovalPolicy: &governance.ApprovalPolicy{
				DefaultScope: approval.ScopeSession,
				MaxScope:     approval.ScopeSession,
			},
		},
	}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{Profile: config.RuntimeProfileDesktop},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:  model,
		Tools:  tools,
		Policy: overlay,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run1, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "overlay-scope-default",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run1.ID, agent.RunWaitingApproval)

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List(pending) error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending approvals) = %d, want 1", len(pending))
	}

	rec := doBootstrapRequest(t, app.Gateway.Handler(), http.MethodPost, "/operator/approvals/"+pending[0].ID+"/resolve", `{"status":"approved","by":"tester"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve approval status = %d body=%s", rec.Code, rec.Body.String())
	}

	resolved, err := app.Approvals.Get(context.Background(), pending[0].ID)
	if err != nil {
		t.Fatalf("Approvals.Get() error = %v", err)
	}
	if resolved.Scope != approval.ScopeSession {
		t.Fatalf("resolved.Scope = %q, want %q", resolved.Scope, approval.ScopeSession)
	}
	waitForBootstrapRunStatus(t, app, run1.ID, agent.RunCompleted)

	run2, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "overlay-scope-default",
		Content:    "read the file again",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run2.ID, agent.RunCompleted)

	allApprovals, err := app.Approvals.List(context.Background(), approval.ListFilter{})
	if err != nil {
		t.Fatalf("Approvals.List(all) error = %v", err)
	}
	if len(allApprovals) != 1 {
		t.Fatalf("len(all approvals) = %d, want 1", len(allApprovals))
	}
	if got := tools.calls.Load(); got != 2 {
		t.Fatalf("tool executor calls = %d, want 2", got)
	}
}

func TestBootstrapApprovalPolicyRejectsBroaderScope(t *testing.T) {
	t.Parallel()

	model := &bootstrapModelClient{
		responses: []*agent.ModelResponse{{
			ToolCalls: []agent.ToolCall{{
				ID:   "call-policy-max-1",
				Name: "fs.read",
			}},
		}},
	}
	tools := newBootstrapCountingToolExecutor("fs.read", "read")
	overlay := &bootstrapStaticPolicyEngine{
		decision: policy.Decision{
			Action:       policy.ActionRequireApproval,
			Reasons:      []string{"approval required with bounded grant"},
			PolicySource: "test.policy/max_scope",
			Summary:      "approval required with bounded scope",
			ApprovalPolicy: &governance.ApprovalPolicy{
				DefaultScope: approval.ScopeOnce,
				MaxScope:     approval.ScopeSession,
			},
		},
	}

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model:  model,
		Tools:  tools,
		Policy: overlay,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "overlay-scope-max",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run.ID, agent.RunWaitingApproval)

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List(pending) error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending approvals) = %d, want 1", len(pending))
	}

	rec := doBootstrapRequest(t, app.Gateway.Handler(), http.MethodPost, "/operator/approvals/"+pending[0].ID+"/resolve", `{"status":"approved","scope":"always","by":"tester"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resolve approval status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	stillPending, err := app.Approvals.Get(context.Background(), pending[0].ID)
	if err != nil {
		t.Fatalf("Approvals.Get() error = %v", err)
	}
	if stillPending.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q, want pending", stillPending.Status)
	}
	if got := tools.calls.Load(); got != 0 {
		t.Fatalf("tool executor calls = %d, want 0", got)
	}
}

func TestBootstrapSessionApprovalGrantPreventsRepeatApproval(t *testing.T) {
	t.Parallel()

	model := &bootstrapModelClient{
		responses: []*agent.ModelResponse{
			{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-grant-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "first run complete",
				},
			},
			{
				ToolCalls: []agent.ToolCall{{
					ID:   "call-grant-2",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "second run complete",
				},
			},
		},
	}
	tools := newBootstrapCountingToolExecutor("fs.write", "local_write")

	app, err := New(context.Background(), config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 4, QueueMode: "enqueue"},
		Runtime: config.RuntimeConfig{Profile: config.RuntimeProfileDesktop},
		Skills: config.SkillsConfig{Dirs: []string{t.TempDir()}},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}, Dependencies{
		Model: model,
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	run1, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "grant-session",
		Content:    "write the file",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run1.ID, agent.RunWaitingApproval)

	pending, err := app.Approvals.List(context.Background(), approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("Approvals.List(pending) error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending approvals) = %d, want 1", len(pending))
	}

	rec := doBootstrapRequest(t, app.Gateway.Handler(), http.MethodPost, "/operator/approvals/"+pending[0].ID+"/resolve", `{"status":"approved","scope":"session","by":"tester"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve approval status = %d body=%s", rec.Code, rec.Body.String())
	}

	waitForBootstrapRunStatus(t, app, run1.ID, agent.RunCompleted)

	run2, err := app.Runtime.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "grant-session",
		Content:    "write the file again",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	waitForBootstrapRunStatus(t, app, run2.ID, agent.RunCompleted)

	allApprovals, err := app.Approvals.List(context.Background(), approval.ListFilter{})
	if err != nil {
		t.Fatalf("Approvals.List(all) error = %v", err)
	}
	if len(allApprovals) != 1 {
		t.Fatalf("len(all approvals) = %d, want 1", len(allApprovals))
	}
	if got := tools.calls.Load(); got != 2 {
		t.Fatalf("tool executor calls = %d, want 2", got)
	}
}

func TestDynamicSkillBinderHandlesTypedNil(t *testing.T) {
	t.Parallel()

	var service *skill.Service
	binder := newDynamicSkillBinder(service)

	if snapshot := binder.Snapshot(); len(snapshot.Ordered) != 0 {
		t.Fatalf("Snapshot().Ordered = %#v, want empty", snapshot.Ordered)
	}
	if session := binder.BindSession(skill.RuntimeContext{}); len(session.Ordered) != 0 {
		t.Fatalf("BindSession().Ordered = %#v, want empty", session.Ordered)
	}
}

func boolPtr(v bool) *bool { return &v }

type bootstrapModelClient struct {
	mu        sync.Mutex
	responses []*agent.ModelResponse
	index     int
}

func (m *bootstrapModelClient) Chat(_ context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	switch {
	case strings.Contains(req.SystemPrompt, "internal run triage engine"):
		return &agent.ModelResponse{Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: `{"execution_mode":"direct","needs_reference":false,"needs_confirmation":false,"suggested_domains":["fs"],"reason":"single tool action","confidence":0.98}`,
		}}, nil
	case strings.Contains(req.SystemPrompt, "internal execution mode selector"):
		return &agent.ModelResponse{Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: `{"mode":"direct","confidence":0.98,"reason":"single focused action"}`,
		}}, nil
	case strings.Contains(req.SystemPrompt, "internal preflight analyzer"):
		return &agent.ModelResponse{Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: `{"needs_reference":false,"needs_confirmation":false,"suggested_domains":["fs"],"reason":"target is self-contained","confidence":0.98}`,
		}}, nil
	case strings.Contains(req.SystemPrompt, "internal task-contract analyzer"):
		return &agent.ModelResponse{Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: `{"job_type":"general","suggested_domains":["fs"],"deliverable_kinds":["summary"],"missing_info_ids":[],"requires_external_effect":false,"requires_approval":false,"reason":"bootstrap test fixture","confidence":0.98}`,
		}}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.index >= len(m.responses) {
		return &agent.ModelResponse{}, nil
	}
	response := m.responses[m.index]
	m.index++
	return response, nil
}

type bootstrapObservedPolicyEngine struct {
	grantStore      *approval.GrantStore
	securityAuditor *audit.SecurityAuditor
}

type bootstrapObservedGovernanceAdapter struct {
	mu      sync.Mutex
	records []controlgov.Record
}

func (a *bootstrapObservedGovernanceAdapter) HandleGovernanceRecord(_ context.Context, record controlgov.Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return nil
}

func (a *bootstrapObservedGovernanceAdapter) snapshot() []controlgov.Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]controlgov.Record, len(a.records))
	copy(out, a.records)
	return out
}

type bootstrapObservedApprovalProvider struct {
	mu      sync.Mutex
	submits []controlapproval.SubmitRequest
	updates []controlapproval.UpdateRequest
}

func (p *bootstrapObservedApprovalProvider) Name() string { return "observed" }

func (p *bootstrapObservedApprovalProvider) SubmitApproval(_ context.Context, req controlapproval.SubmitRequest) (*controlapproval.Submission, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits = append(p.submits, req)
	return &controlapproval.Submission{ExternalID: "ext-observed"}, nil
}

func (p *bootstrapObservedApprovalProvider) UpdateApproval(_ context.Context, req controlapproval.UpdateRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.updates = append(p.updates, req)
	return nil
}

func (p *bootstrapObservedApprovalProvider) submitCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.submits)
}

func (p *bootstrapObservedApprovalProvider) updateCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.updates)
}

func (e *bootstrapObservedPolicyEngine) EvaluateTool(context.Context, policy.ToolContext) (policy.Decision, error) {
	return policy.Decision{
		Action:       policy.ActionAllow,
		PolicySource: "test.policy/bootstrap_observer",
		Summary:      "allowed by bootstrap observer",
	}, nil
}

func (e *bootstrapObservedPolicyEngine) SetGrantStore(gs *approval.GrantStore) {
	e.grantStore = gs
}

func (e *bootstrapObservedPolicyEngine) SetSecurityAuditor(auditor *audit.SecurityAuditor) {
	e.securityAuditor = auditor
}

type bootstrapStaticPolicyEngine struct {
	decision        policy.Decision
	grantStore      *approval.GrantStore
	securityAuditor *audit.SecurityAuditor
}

func (e *bootstrapStaticPolicyEngine) EvaluateTool(_ context.Context, call policy.ToolContext) (policy.Decision, error) {
	if e == nil {
		return policy.Decision{}, nil
	}
	if e.grantStore != nil {
		if e.grantStore.IsDenied(call.SessionID, call.ToolName) {
			return policy.Decision{
				Action:       policy.ActionDeny,
				PolicySource: "test.policy/bootstrap_static_grant",
				Summary:      "denied by bootstrap static grant policy",
			}, nil
		}
		if e.grantStore.IsGranted(call.SessionID, call.ToolName) {
			return policy.Decision{
				Action:       policy.ActionAllow,
				PolicySource: "test.policy/bootstrap_static_grant",
				Summary:      "allowed by bootstrap static grant policy",
			}, nil
		}
	}
	if e.decision.Action == "" {
		return policy.Decision{
			Action:       policy.ActionAllow,
			PolicySource: "test.policy/bootstrap_static",
			Summary:      "allowed by bootstrap static policy",
		}, nil
	}
	return e.decision, nil
}

func (e *bootstrapStaticPolicyEngine) SetGrantStore(gs *approval.GrantStore) {
	e.grantStore = gs
}

func (e *bootstrapStaticPolicyEngine) SetSecurityAuditor(auditor *audit.SecurityAuditor) {
	e.securityAuditor = auditor
}

type bootstrapCountingToolExecutor struct {
	calls    atomic.Int32
	resolved *agent.ResolvedTool
}

func newBootstrapCountingToolExecutor(name, sideEffect string) *bootstrapCountingToolExecutor {
	descriptor := agent.ToolDefinition{
		Name:            name,
		Description:     "bootstrap test tool",
		SideEffectClass: sideEffect,
		Eligible:        true,
		Availability: toolspec.ToolAvailability{
			Status: toolspec.AvailabilityReady,
		},
	}
	return &bootstrapCountingToolExecutor{
		resolved: &agent.ResolvedTool{
			Descriptor: descriptor,
			Manifest: skill.ToolManifest{
				Name:            name,
				Description:     "bootstrap test tool",
				SideEffectClass: sideEffect,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
			ExecutorRef: "bootstrap-test",
		},
	}
}

func (e *bootstrapCountingToolExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	e.calls.Add(1)
	if e.resolved == nil {
		return nil, nil
	}
	results := make([]contextengine.ToolResult, 0)
	for _, call := range calls {
		results = append(results, contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Summary:    "bootstrap tool executed",
			Content:    "ok",
		})
	}
	return results, nil
}

func (e *bootstrapCountingToolExecutor) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if e == nil || e.resolved == nil {
		return nil
	}
	return []agent.ToolDefinition{e.resolved.Descriptor}
}

func (e *bootstrapCountingToolExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if e == nil || e.resolved == nil {
		return nil, false
	}
	if e.resolved.Descriptor.Name != name {
		return nil, false
	}
	copy := *e.resolved
	return &copy, true
}

func doBootstrapRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func waitForBootstrapRunStatus(t *testing.T, app *App, runID string, want agent.RunStatus) *agent.Run {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	var last *agent.Run
	for time.Now().Before(deadline) {
		run, err := app.Runtime.GetRun(context.Background(), runID)
		if err == nil {
			last = run
			if run != nil && run.Status == want {
				return run
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("timed out waiting for run %s to reach %q", runID, want)
	}
	t.Fatalf("timed out waiting for run %s to reach %q; last status=%q error=%q", runID, want, last.Status, last.Error)
	return nil
}
