package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParseAppliesDefaultsAndExpandsEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENCLAW_STATE", root)
	cfg, err := Parse([]byte(`
store:
  backend: jsonl
  path: ${OPENCLAW_STATE}
skills:
  include_catalog: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Server.Address != "127.0.0.1:16280" {
		t.Fatalf("cfg.Server.Address = %q", cfg.Server.Address)
	}
	if cfg.Store.Path != root {
		t.Fatalf("cfg.Store.Path = %q", cfg.Store.Path)
	}
	if cfg.Agent.DefaultModel != "unconfigured-model" {
		t.Fatalf("cfg.Agent.DefaultModel = %q", cfg.Agent.DefaultModel)
	}
	if strings.TrimSpace(cfg.Agent.SystemPrompt) != strings.TrimSpace(defaultAgentSystemPrompt) {
		t.Fatalf("cfg.Agent.SystemPrompt = %q", cfg.Agent.SystemPrompt)
	}
	if cfg.Runtime.Profile != RuntimeProfileTrustedDesktop {
		t.Fatalf("cfg.Runtime.Profile = %q", cfg.Runtime.Profile)
	}
	if cfg.Runtime.StatusReminderDelay != 6*time.Second {
		t.Fatalf("cfg.Runtime.StatusReminderDelay = %s", cfg.Runtime.StatusReminderDelay)
	}
	// Default profile is "trusted_desktop", so install_policy defaults to "auto" (C端).
	if cfg.Skills.InstallPolicy != SkillInstallPolicyAuto {
		t.Fatalf("cfg.Skills.InstallPolicy = %q", cfg.Skills.InstallPolicy)
	}
	if cfg.Skills.EnsureLimit != 5 {
		t.Fatalf("cfg.Skills.EnsureLimit = %d", cfg.Skills.EnsureLimit)
	}
}

func TestParseAppliesStatePruneIntervalDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
runtime:
  state:
    runs_retention: 24h
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Runtime.State.PruneInterval != time.Hour {
		t.Fatalf("cfg.Runtime.State.PruneInterval = %v, want %v", cfg.Runtime.State.PruneInterval, time.Hour)
	}
}

func TestParseAppliesAgentMaxRunDurationDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`store: {backend: memory}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Agent.MaxRunDuration != 10*time.Minute {
		t.Fatalf("cfg.Agent.MaxRunDuration = %v, want %v", cfg.Agent.MaxRunDuration, 10*time.Minute)
	}
}

func TestParsePreservesExplicitDefaultableValues(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
agent:
  system_prompt: |
    Custom system prompt.
  max_tool_rounds: 4
  max_run_duration: 2m
  dedupe_window: 3m
runtime:
  status_reminder_delay: 11s
tools:
  builtins:
    default_exec_timeout: 45s
  local_exec:
    default_timeout: 75s
skills:
  install_policy: deny
  ensure_limit: 9
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Agent.MaxToolRounds != 4 {
		t.Fatalf("cfg.Agent.MaxToolRounds = %d, want 4", cfg.Agent.MaxToolRounds)
	}
	if strings.TrimSpace(cfg.Agent.SystemPrompt) != "Custom system prompt." {
		t.Fatalf("cfg.Agent.SystemPrompt = %q, want custom prompt", cfg.Agent.SystemPrompt)
	}
	if cfg.Agent.MaxRunDuration != 2*time.Minute {
		t.Fatalf("cfg.Agent.MaxRunDuration = %v, want %v", cfg.Agent.MaxRunDuration, 2*time.Minute)
	}
	if cfg.Agent.DedupeWindow != 3*time.Minute {
		t.Fatalf("cfg.Agent.DedupeWindow = %v, want %v", cfg.Agent.DedupeWindow, 3*time.Minute)
	}
	if cfg.Runtime.StatusReminderDelay != 11*time.Second {
		t.Fatalf("cfg.Runtime.StatusReminderDelay = %v, want %v", cfg.Runtime.StatusReminderDelay, 11*time.Second)
	}
	if cfg.Tools.Builtins.DefaultExecTimeout != 45*time.Second {
		t.Fatalf("cfg.Tools.Builtins.DefaultExecTimeout = %v, want %v", cfg.Tools.Builtins.DefaultExecTimeout, 45*time.Second)
	}
	if cfg.Tools.LocalExec.DefaultTimeout != 75*time.Second {
		t.Fatalf("cfg.Tools.LocalExec.DefaultTimeout = %v, want %v", cfg.Tools.LocalExec.DefaultTimeout, 75*time.Second)
	}
	if cfg.Skills.InstallPolicy != SkillInstallPolicyDeny {
		t.Fatalf("cfg.Skills.InstallPolicy = %q, want %q", cfg.Skills.InstallPolicy, SkillInstallPolicyDeny)
	}
	if cfg.Skills.EnsureLimit != 9 {
		t.Fatalf("cfg.Skills.EnsureLimit = %d, want 9", cfg.Skills.EnsureLimit)
	}
}

func TestExampleConfigsMatchDefaultAgentSystemPrompt(t *testing.T) {
	t.Parallel()

	for _, rel := range []string{"../config.example.yaml", "../docker/config.example.yaml"} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Clean(rel))
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", rel, err)
			}
			var cfg Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("yaml.Unmarshal(%s) error = %v", rel, err)
			}
			cfg.applyDefaults()
			if strings.TrimSpace(cfg.Agent.SystemPrompt) != strings.TrimSpace(defaultAgentSystemPrompt) {
				t.Fatalf("%s agent.system_prompt = %q, want default prompt", rel, cfg.Agent.SystemPrompt)
			}
		})
	}
}

func TestLoadReadsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("store:\n  backend: memory\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Store.Backend != "memory" {
		t.Fatalf("cfg.Store.Backend = %q", cfg.Store.Backend)
	}
}

func TestParseAllowsExplicitToolDisable(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
tools:
  builtins:
    enabled: false
  local_exec:
    enabled: false
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Tools.Builtins.Enabled == nil || *cfg.Tools.Builtins.Enabled {
		t.Fatalf("cfg.Tools.Builtins.Enabled = %#v", cfg.Tools.Builtins.Enabled)
	}
	if cfg.Tools.LocalExec.Enabled == nil || *cfg.Tools.LocalExec.Enabled {
		t.Fatalf("cfg.Tools.LocalExec.Enabled = %#v", cfg.Tools.LocalExec.Enabled)
	}
}

func TestValidateRejectsNegativeStateLimits(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Store: StoreConfig{Backend: "memory"},
		Runtime: RuntimeConfig{
			Profile: RuntimeProfileDesktop,
			State: StatusConfig{
				JSONLStartupLimit: -1,
			},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject negative runtime.state.jsonl_startup_limit")
	}
}

func TestValidateRejectsNegativeAgentMaxRunDuration(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Store: StoreConfig{Backend: "memory"},
		Runtime: RuntimeConfig{
			Profile: RuntimeProfileDesktop,
		},
		Agent: AgentConfig{
			MaxRunDuration: -time.Second,
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject negative agent.max_run_duration")
	}
}

func TestParseRejectsNegativeDefaultableValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "agent max tool rounds",
			data: `
store: {backend: memory}
agent:
  max_tool_rounds: -1
`,
			want: "agent.max_tool_rounds must be >= 0",
		},
		{
			name: "runtime status reminder delay",
			data: `
store: {backend: memory}
runtime:
  status_reminder_delay: -1s
`,
			want: "runtime.status_reminder_delay must be >= 0",
		},
		{
			name: "runtime artifacts inline threshold",
			data: `
store: {backend: memory}
runtime:
  artifacts:
    inline_threshold: -1
`,
			want: "runtime.artifacts.inline_threshold must be >= 0",
		},
		{
			name: "tools builtins default exec timeout",
			data: `
store: {backend: memory}
tools:
  builtins:
    default_exec_timeout: -1s
`,
			want: "tools.builtins.default_exec_timeout must be >= 0",
		},
		{
			name: "tools builtins max read bytes",
			data: `
store: {backend: memory}
tools:
  builtins:
    max_read_bytes: -1
`,
			want: "tools.builtins.max_read_bytes must be >= 0",
		},
		{
			name: "tools local exec default timeout",
			data: `
store: {backend: memory}
tools:
  local_exec:
    default_timeout: -1s
`,
			want: "tools.local_exec.default_timeout must be >= 0",
		},
		{
			name: "skills ensure limit",
			data: `
store: {backend: memory}
skills:
  ensure_limit: -1
`,
			want: "skills.ensure_limit must be >= 0",
		},
		{
			name: "heartbeat interval",
			data: `
store: {backend: memory}
heartbeat:
  interval: -1s
`,
			want: "heartbeat.interval must be >= 0",
		},
		{
			name: "wire max entries",
			data: `
store: {backend: memory}
wire:
  max_entries: -1
`,
			want: "wire.max_entries must be >= 0",
		},
		{
			name: "wire retention time",
			data: `
store: {backend: memory}
wire:
  retention_time: -1s
`,
			want: "wire.retention_time must be >= 0",
		},
		{
			name: "sandbox timeout",
			data: `
store: {backend: memory}
sandbox:
  timeout: -1
`,
			want: "sandbox.timeout must be >= 0",
		},
		{
			name: "exec approval timeout",
			data: `
store: {backend: memory}
exec_approval:
  approval_timeout: -1s
`,
			want: "exec_approval.approval_timeout must be >= 0",
		},
		{
			name: "exec approval grace period",
			data: `
store: {backend: memory}
exec_approval:
  grace_period: -1s
`,
			want: "exec_approval.grace_period must be >= 0",
		},
		{
			name: "approval provider callback max age",
			data: `
store: {backend: memory}
exec_approval:
  providers:
    - name: jira
      callback_auth:
        max_age: -1s
`,
			want: "exec_approval.providers[0].callback_auth.max_age must be >= 0",
		},
		{
			name: "approval provider webhook timeout",
			data: `
store: {backend: memory}
exec_approval:
  providers:
    - name: jira
      webhook:
        submit_url: https://approvals.example.com/submit
        timeout: -1s
`,
			want: "exec_approval.providers[0].webhook.timeout must be >= 0",
		},
		{
			name: "authz webhook timeout",
			data: `
store: {backend: memory}
authz:
  mode: webhook
  webhook:
    url: https://authz.example.com/decide
    timeout: -1s
`,
			want: "authz.webhook.timeout must be >= 0",
		},
		{
			name: "governance adapter webhook timeout",
			data: `
store: {backend: memory}
runtime:
  governance:
    adapters:
      - name: audit-hub
        webhook:
          url: https://governance.example.com/events
          timeout: -1s
`,
			want: "runtime.governance.adapters[0].webhook.timeout must be >= 0",
		},
		{
			name: "audit sink webhook timeout",
			data: `
store: {backend: memory}
runtime:
  audit:
    sinks:
      - name: webhook-sink
        webhook:
          url: https://audit.example.com/events
          timeout: -1s
`,
			want: "runtime.audit.sinks[0].webhook.timeout must be >= 0",
		},
		{
			name: "audit delivery max backoff",
			data: `
store: {backend: memory}
runtime:
  audit:
    sinks:
      - name: webhook-sink
        webhook:
          url: https://audit.example.com/events
    delivery:
      max_backoff: -1s
`,
			want: "runtime.audit.delivery.max_backoff must be >= 0",
		},
		{
			name: "audit delivery batch size",
			data: `
store: {backend: memory}
runtime:
  audit:
    sinks:
      - name: webhook-sink
        webhook:
          url: https://audit.example.com/events
    delivery:
      batch_size: -1
`,
			want: "runtime.audit.delivery.batch_size must be >= 0",
		},
		{
			name: "governance delivery max backoff",
			data: `
store: {backend: memory}
runtime:
  governance:
    delivery:
      max_backoff: -1s
`,
			want: "runtime.governance.delivery.max_backoff must be >= 0",
		},
		{
			name: "governance delivery max attempts",
			data: `
store: {backend: memory}
runtime:
  governance:
    delivery:
      max_attempts: -1
`,
			want: "runtime.governance.delivery.max_attempts must be >= 0",
		},
		{
			name: "channel health check interval",
			data: `
store: {backend: memory}
channel_health:
  check_interval: -1s
`,
			want: "channel_health.check_interval must be >= 0",
		},
		{
			name: "channel health max restarts per hour",
			data: `
store: {backend: memory}
channel_health:
  max_restarts_per_hour: -1
`,
			want: "channel_health.max_restarts_per_hour must be >= 0",
		},
		{
			name: "security max content size",
			data: `
store: {backend: memory}
security:
  max_content_size: -1
`,
			want: "security.max_content_size must be >= 0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(tt.data))
			if err == nil {
				t.Fatalf("Parse() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsUnknownRuntimeVerifierSeverity(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Store: StoreConfig{Backend: "memory"},
		Runtime: RuntimeConfig{
			Profile: RuntimeProfileDesktop,
			Verification: VerificationConfig{
				VerifierSeverities: map[string]string{
					"email.result": "critical",
				},
			},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject unknown runtime verification severity")
	}
}

func TestParseNormalizesRuntimeVerifierSeverityOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store: {backend: memory}
runtime:
  verification:
    verifier_severities:
      " email.result ": " BLOCKING "
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := cfg.Runtime.Verification.VerifierSeverities["email.result"]; got != "blocking" {
		t.Fatalf("runtime.verification.verifier_severities[email.result] = %q, want %q", got, "blocking")
	}
}

func TestParseCapabilitiesConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
tools:
  capabilities:
    exec:
      mode: allowlist
      allowlist: ["git *", "npm *"]
      denylist: ["rm -rf /"]
      timeout: 60s
      max_output: 1048576
    net:
      allow_private: false
      max_download: 104857600
      deny_hosts: ["evil.com"]
    fs:
      allowed_roots: ["/tmp"]
      deny_patterns: ["*.env", "*secret*"]
      skip_dirs: [".git", "vendor"]
    layer2:
      git: true
      container: false
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Tools.Capabilities.Exec.Mode != "allowlist" {
		t.Fatalf("exec.mode = %q", cfg.Tools.Capabilities.Exec.Mode)
	}
	if len(cfg.Tools.Capabilities.Exec.Allowlist) != 2 {
		t.Fatalf("exec.allowlist len = %d", len(cfg.Tools.Capabilities.Exec.Allowlist))
	}
	if len(cfg.Tools.Capabilities.Exec.Denylist) != 1 {
		t.Fatalf("exec.denylist len = %d", len(cfg.Tools.Capabilities.Exec.Denylist))
	}
	if cfg.Tools.Capabilities.Net.AllowPrivate {
		t.Fatal("net.allow_private should be false")
	}
	if cfg.Tools.Capabilities.Net.MaxDownload != 104857600 {
		t.Fatalf("net.max_download = %d", cfg.Tools.Capabilities.Net.MaxDownload)
	}
	if len(cfg.Tools.Capabilities.FS.DenyPatterns) != 2 {
		t.Fatalf("fs.deny_patterns len = %d", len(cfg.Tools.Capabilities.FS.DenyPatterns))
	}
	// skip_dirs should be the explicit config value, not defaults.
	if len(cfg.Tools.Capabilities.FS.SkipDirs) != 2 {
		t.Fatalf("fs.skip_dirs len = %d, want 2", len(cfg.Tools.Capabilities.FS.SkipDirs))
	}
	if cfg.Tools.Capabilities.Layer2.Git == nil || !*cfg.Tools.Capabilities.Layer2.Git {
		t.Fatal("layer2.git should be true")
	}
	if cfg.Tools.Capabilities.Layer2.Container == nil || *cfg.Tools.Capabilities.Layer2.Container {
		t.Fatal("layer2.container should be false")
	}
}

func TestParseCapabilitiesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`store: {backend: memory}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Tools.Capabilities.Exec.Mode != "approve" {
		t.Fatalf("default exec.mode = %q, want approve", cfg.Tools.Capabilities.Exec.Mode)
	}
	if cfg.Tools.Capabilities.Net.AllowLocal == nil || !*cfg.Tools.Capabilities.Net.AllowLocal {
		t.Fatal("default net.allow_local should be true")
	}
	if len(cfg.Tools.Capabilities.FS.SkipDirs) == 0 {
		t.Fatal("default fs.skip_dirs should have entries")
	}
}

func TestParseSkillsAutoDetect(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
skills:
  auto_detect: true
  auto_refresh: true
  dirs: ["./skills"]
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.Skills.AutoDetect {
		t.Fatal("skills.auto_detect should be true")
	}
	if cfg.Skills.AutoRefresh == nil || !*cfg.Skills.AutoRefresh {
		t.Fatal("skills.auto_refresh should be true")
	}
}

func TestParseSkillsAutoRefreshDefaultsToTrue(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
skills:
  auto_detect: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Skills.AutoRefresh == nil || !*cfg.Skills.AutoRefresh {
		t.Fatal("skills.auto_refresh should default to true")
	}
}

func TestParseSkillsAutoRefreshCanBeDisabled(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
skills:
  auto_refresh: false
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Skills.AutoRefresh == nil {
		t.Fatal("skills.auto_refresh should remain explicitly set")
	}
	if *cfg.Skills.AutoRefresh {
		t.Fatal("skills.auto_refresh should be false")
	}
}

func TestParseSkillInstallPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
skills:
  install_policy: auto
  ensure_limit: 7
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Skills.InstallPolicy != SkillInstallPolicyAuto {
		t.Fatalf("cfg.Skills.InstallPolicy = %q", cfg.Skills.InstallPolicy)
	}
	if cfg.Skills.EnsureLimit != 7 {
		t.Fatalf("cfg.Skills.EnsureLimit = %d", cfg.Skills.EnsureLimit)
	}
}

func TestParseRejectsUnsupportedSkillInstallPolicy(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
skills:
  install_policy: maybe
`))
	if err == nil {
		t.Fatal("expected Parse() to reject unsupported skills.install_policy")
	}
}

func TestParseRejectsUnsupportedQueueMode(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
agent:
  queue_mode: interrupt
`))
	if err == nil {
		t.Fatal("expected Parse() to reject interrupt queue mode")
	}
}

func TestParseAllowsEmailIMAPConfig(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
tools:
  services:
    email:
      imap_host: imap.example.com
      imap_port: 993
      username: user@example.com
      password: secret
`))
	if err != nil {
		t.Fatalf("expected Parse() to accept IMAP config, got %v", err)
	}
}

func TestParseRejectsPartialSpeechConfig(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
tools:
  services:
    speech:
      base_url: https://example.com
`))
	if err == nil {
		t.Fatal("expected Parse() to reject partial speech config")
	}
}

func TestParseAllowsBrowserHostConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
hosts:
  browser:
    enabled: true
    base_url: http://127.0.0.1:9223
    auth_token: secret
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Hosts.Browser.Enabled == nil || !*cfg.Hosts.Browser.Enabled {
		t.Fatalf("hosts.browser.enabled = %#v", cfg.Hosts.Browser.Enabled)
	}
	if cfg.Hosts.Browser.BaseURL != "http://127.0.0.1:9223" {
		t.Fatalf("hosts.browser.base_url = %q", cfg.Hosts.Browser.BaseURL)
	}
}

func TestParseAllowsManagedBrowserHostWithoutBaseURL(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
hosts:
  browser:
    enabled: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Hosts.Browser.Enabled == nil || !*cfg.Hosts.Browser.Enabled {
		t.Fatalf("hosts.browser.enabled = %#v", cfg.Hosts.Browser.Enabled)
	}
	if cfg.Hosts.Browser.BaseURL != "" {
		t.Fatalf("hosts.browser.base_url = %q, want empty for managed mode", cfg.Hosts.Browser.BaseURL)
	}
}

func TestParseAllowsDesktopHostConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
hosts:
  desktop:
    enabled: true
    base_url: http://127.0.0.1:9224
    auth_token: secret
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Hosts.Desktop.Enabled == nil || !*cfg.Hosts.Desktop.Enabled {
		t.Fatalf("hosts.desktop.enabled = %#v", cfg.Hosts.Desktop.Enabled)
	}
	if cfg.Hosts.Desktop.BaseURL != "http://127.0.0.1:9224" {
		t.Fatalf("hosts.desktop.base_url = %q", cfg.Hosts.Desktop.BaseURL)
	}
}

func TestParseAllowsManagedDesktopHostWithoutBaseURL(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
hosts:
  desktop:
    enabled: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Hosts.Desktop.Enabled == nil || !*cfg.Hosts.Desktop.Enabled {
		t.Fatalf("hosts.desktop.enabled = %#v", cfg.Hosts.Desktop.Enabled)
	}
	if cfg.Hosts.Desktop.BaseURL != "" {
		t.Fatalf("hosts.desktop.base_url = %q, want empty for managed mode", cfg.Hosts.Desktop.BaseURL)
	}
}

func TestParseRejectsAmbiguousDefaultModelWithMultipleProviders(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
agent:
  default_model: gpt-4o
models:
  providers:
    openai:
      api: openai-completions
      base_url: https://api.openai.com/v1
    deepseek:
      api: openai-completions
      base_url: https://api.deepseek.com/v1
`))
	if err == nil {
		t.Fatal("expected Parse() to reject ambiguous default model")
	}
}

func TestParseAllowsMultipleProvidersWithDefaultProvider(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
agent:
  default_model: gpt-4o
models:
  default_provider: openai
  providers:
    openai:
      api: openai-completions
      base_url: https://api.openai.com/v1
    deepseek:
      api: openai-completions
      base_url: https://api.deepseek.com/v1
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Models.DefaultProvider != "openai" {
		t.Fatalf("cfg.Models.DefaultProvider = %q", cfg.Models.DefaultProvider)
	}
}

func TestParseRejectsUnknownQualifiedDefaultModelProvider(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
agent:
  default_model: thirdparty/gpt-4o
models:
  providers:
    openai:
      api: openai-completions
      base_url: https://api.openai.com/v1
    deepseek:
      api: openai-completions
      base_url: https://api.deepseek.com/v1
`))
	if err == nil {
		t.Fatal("expected Parse() to reject unknown qualified default model provider")
	}
}

func TestParseAllowsQualifiedDefaultModelForProviderNameWithSlash(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
agent:
  default_model: demo/copilot/gpt-4o
models:
  providers:
    demo/copilot:
      api: github-copilot
      api_key: ghp_test
      default_model: gpt-4o
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := cfg.Agent.DefaultModel; got != "demo/copilot/gpt-4o" {
		t.Fatalf("cfg.Agent.DefaultModel = %q", got)
	}
}

func TestParseRejectsCatalogProviderRequiringBaseURL(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
agent:
  default_model: litellm/gpt-4o-mini
models:
  providers:
    litellm:
      api: openai-completions
      api_key: test
`))
	if err == nil {
		t.Fatal("expected Parse() to require base_url for litellm")
	}
}

func TestParseAppliesProductionProfileDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
server:
  auth_token: secret
store:
  backend: jsonl
  path: ./.hopclaw/state
runtime:
  profile: production
  audit:
    enabled: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Runtime.Profile != RuntimeProfileProduction {
		t.Fatalf("cfg.Runtime.Profile = %q", cfg.Runtime.Profile)
	}
	if cfg.Tools.Capabilities.Exec.Mode != "allowlist" {
		t.Fatalf("exec.mode = %q", cfg.Tools.Capabilities.Exec.Mode)
	}
	if cfg.Tools.Capabilities.Net.AllowLocal == nil || *cfg.Tools.Capabilities.Net.AllowLocal {
		t.Fatalf("allow_local = %#v", cfg.Tools.Capabilities.Net.AllowLocal)
	}
	// Production profile (B端) defaults to "ask" for skill installation.
	if cfg.Skills.InstallPolicy != SkillInstallPolicyAsk {
		t.Fatalf("cfg.Skills.InstallPolicy = %q, want %q", cfg.Skills.InstallPolicy, SkillInstallPolicyAsk)
	}
}

func TestParseApprovalProvidersAppliesCallbackHeaderDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
exec_approval:
  providers:
    - name: jira
      callback_auth:
        token: secret-jira
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.ExecApproval.Providers) != 1 {
		t.Fatalf("len(cfg.ExecApproval.Providers) = %d", len(cfg.ExecApproval.Providers))
	}
	if cfg.ExecApproval.Providers[0].CallbackAuth.HeaderName != "X-HopClaw-Approval-Token" {
		t.Fatalf("callback header = %q", cfg.ExecApproval.Providers[0].CallbackAuth.HeaderName)
	}
}

func TestParseRejectsDuplicateApprovalProviderNames(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
exec_approval:
  providers:
    - name: jira
    - name: JIRA
`))
	if err == nil {
		t.Fatal("expected Parse() to reject duplicate exec_approval.providers names")
	}
}

func TestParseApprovalProviderHMACDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
exec_approval:
  providers:
    - name: jira
      callback_auth:
        secret: secret-hmac
      webhook:
        submit_url: https://approvals.example.com/submit
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	provider := cfg.ExecApproval.Providers[0]
	if provider.CallbackAuth.Mode != "hmac" {
		t.Fatalf("callback mode = %q", provider.CallbackAuth.Mode)
	}
	if provider.CallbackAuth.SignatureHeader != "X-HopClaw-Signature" {
		t.Fatalf("signature header = %q", provider.CallbackAuth.SignatureHeader)
	}
	if provider.CallbackAuth.TimestampHeader != "X-HopClaw-Timestamp" {
		t.Fatalf("timestamp header = %q", provider.CallbackAuth.TimestampHeader)
	}
	if provider.CallbackAuth.MaxAge != 5*time.Minute {
		t.Fatalf("max age = %v", provider.CallbackAuth.MaxAge)
	}
	if provider.Webhook.Timeout != 15*time.Second {
		t.Fatalf("webhook timeout = %v", provider.Webhook.Timeout)
	}
}

func TestParseRejectsApprovalProviderWebhookWithoutEndpoint(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
exec_approval:
  providers:
    - name: jira
      type: webhook
`))
	if err == nil {
		t.Fatal("expected Parse() to reject webhook approval provider without endpoint")
	}
}

func TestParseGovernanceAdapterAppliesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
runtime:
  governance:
    adapters:
      - name: audit-hub
        webhook:
          url: https://governance.example.com/events
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Runtime.Governance.Adapters) != 1 {
		t.Fatalf("len(cfg.Runtime.Governance.Adapters) = %d", len(cfg.Runtime.Governance.Adapters))
	}
	adapter := cfg.Runtime.Governance.Adapters[0]
	if adapter.Webhook.Timeout != 15*time.Second {
		t.Fatalf("adapter.Webhook.Timeout = %v", adapter.Webhook.Timeout)
	}
	if adapter.Webhook.IncludeSnapshot == nil || !*adapter.Webhook.IncludeSnapshot {
		t.Fatalf("adapter.Webhook.IncludeSnapshot = %#v", adapter.Webhook.IncludeSnapshot)
	}
	if cfg.Runtime.Governance.Delivery.Backend != "memory" {
		t.Fatalf("cfg.Runtime.Governance.Delivery.Backend = %q", cfg.Runtime.Governance.Delivery.Backend)
	}
	if cfg.Runtime.Governance.Delivery.MaxAttempts != 8 {
		t.Fatalf("cfg.Runtime.Governance.Delivery.MaxAttempts = %d", cfg.Runtime.Governance.Delivery.MaxAttempts)
	}
	if cfg.Runtime.Governance.Delivery.BaseBackoff != 5*time.Second {
		t.Fatalf("cfg.Runtime.Governance.Delivery.BaseBackoff = %v", cfg.Runtime.Governance.Delivery.BaseBackoff)
	}
	if cfg.Runtime.Governance.Delivery.MaxBackoff != 5*time.Minute {
		t.Fatalf("cfg.Runtime.Governance.Delivery.MaxBackoff = %v", cfg.Runtime.Governance.Delivery.MaxBackoff)
	}
	if cfg.Runtime.Governance.Delivery.PollInterval != 2*time.Second {
		t.Fatalf("cfg.Runtime.Governance.Delivery.PollInterval = %v", cfg.Runtime.Governance.Delivery.PollInterval)
	}
	if cfg.Runtime.Governance.Delivery.BatchSize != 32 {
		t.Fatalf("cfg.Runtime.Governance.Delivery.BatchSize = %d", cfg.Runtime.Governance.Delivery.BatchSize)
	}
}

func TestParseAuthZWebhookAppliesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
authz:
  webhook:
    url: https://authz.example.com/decide
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.AuthZ.Mode != "" {
		t.Fatalf("cfg.AuthZ.Mode = %q, want empty explicit mode", cfg.AuthZ.Mode)
	}
	if cfg.AuthZ.Webhook.Timeout != 5*time.Second {
		t.Fatalf("cfg.AuthZ.Webhook.Timeout = %v", cfg.AuthZ.Webhook.Timeout)
	}
}

func TestParseRejectsAuthZFallbackRBACWithoutRBACConfig(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
authz:
  mode: webhook
  fallback: rbac
  webhook:
    url: https://authz.example.com/decide
`))
	if err == nil {
		t.Fatal("expected Parse() to reject authz.fallback=rbac without auth.rbac")
	}
}

func TestParseAuditSinkAppliesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: siem
        webhook:
          url: https://audit.example.com/events
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Runtime.Audit.Sinks) != 1 {
		t.Fatalf("len(cfg.Runtime.Audit.Sinks) = %d", len(cfg.Runtime.Audit.Sinks))
	}
	if cfg.Runtime.Audit.Sinks[0].Webhook.Timeout != 15*time.Second {
		t.Fatalf("Webhook.Timeout = %v", cfg.Runtime.Audit.Sinks[0].Webhook.Timeout)
	}
	if cfg.Runtime.Audit.Delivery.Backend != "memory" {
		t.Fatalf("Delivery.Backend = %q, want memory", cfg.Runtime.Audit.Delivery.Backend)
	}
	if cfg.Runtime.Audit.Delivery.MaxAttempts != 8 {
		t.Fatalf("Delivery.MaxAttempts = %d, want 8", cfg.Runtime.Audit.Delivery.MaxAttempts)
	}
	if cfg.Runtime.Audit.Delivery.PollInterval != 2*time.Second {
		t.Fatalf("Delivery.PollInterval = %v, want 2s", cfg.Runtime.Audit.Delivery.PollInterval)
	}
	if cfg.Runtime.Audit.Delivery.BatchSize != 32 {
		t.Fatalf("Delivery.BatchSize = %d, want 32", cfg.Runtime.Audit.Delivery.BatchSize)
	}
}

func TestParseRejectsAuditSinkWebhookWithoutURL(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: siem
        type: webhook
`))
	if err == nil {
		t.Fatal("expected Parse() to reject audit sink without URL")
	}
}

func TestParseAuditSinkElasticsearchDefaultsAndInfersType(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: corp-es
        elasticsearch:
          url: https://es.example.com
          index: hopclaw-audit
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Runtime.Audit.Sinks) != 1 {
		t.Fatalf("len(cfg.Runtime.Audit.Sinks) = %d", len(cfg.Runtime.Audit.Sinks))
	}
	sink := cfg.Runtime.Audit.Sinks[0]
	if sink.Type != "elasticsearch" {
		t.Fatalf("Type = %q, want elasticsearch", sink.Type)
	}
	if sink.Elasticsearch.Timeout != 15*time.Second {
		t.Fatalf("Elasticsearch.Timeout = %v", sink.Elasticsearch.Timeout)
	}
}

func TestParseRejectsAuditSinkElasticsearchWithoutIndex(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: corp-es
        type: elasticsearch
        elasticsearch:
          url: https://es.example.com
`))
	if err == nil {
		t.Fatal("expected Parse() to reject elasticsearch audit sink without index")
	}
}

func TestParseAuditSinkSplunkDefaultsAndInfersType(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: corp-splunk
        splunk_hec:
          url: https://splunk.example.com/services/collector
          token: env:SPLUNK_HEC_TOKEN
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Runtime.Audit.Sinks) != 1 {
		t.Fatalf("len(cfg.Runtime.Audit.Sinks) = %d", len(cfg.Runtime.Audit.Sinks))
	}
	sink := cfg.Runtime.Audit.Sinks[0]
	if sink.Type != "splunk_hec" {
		t.Fatalf("Type = %q, want splunk_hec", sink.Type)
	}
	if sink.SplunkHEC.Timeout != 15*time.Second {
		t.Fatalf("SplunkHEC.Timeout = %v", sink.SplunkHEC.Timeout)
	}
}

func TestParseRejectsAuditSinkMultipleConnectorKinds(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  audit:
    enabled: true
    sinks:
      - name: invalid-sink
        webhook:
          url: https://audit.example.com/events
        elasticsearch:
          url: https://es.example.com
          index: hopclaw-audit
`))
	if err == nil {
		t.Fatal("expected Parse() to reject audit sink with multiple connector kinds")
	}
}

func TestParseAuditDeliveryDefaultsToSQLiteWhenStoreIsSQLite(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: sqlite
runtime:
  audit:
    enabled: true
    sinks:
      - name: siem
        webhook:
          url: https://audit.example.com/events
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Runtime.Audit.Delivery.Backend != "sqlite" {
		t.Fatalf("Delivery.Backend = %q, want sqlite", cfg.Runtime.Audit.Delivery.Backend)
	}
}

func TestParseRejectsSQLiteAuditDeliveryWithoutSQLiteStore(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: jsonl
runtime:
  audit:
    enabled: true
    delivery:
      backend: sqlite
    sinks:
      - name: siem
        webhook:
          url: https://audit.example.com/events
`))
	if err == nil {
		t.Fatal("expected Parse() to reject sqlite audit delivery without sqlite store backend")
	}
}

func TestParseRejectsGovernanceAdapterWebhookWithoutURL(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  governance:
    adapters:
      - name: audit-hub
        type: webhook
`))
	if err == nil {
		t.Fatal("expected Parse() to reject governance webhook adapter without url")
	}
}

func TestParseRejectsGovernanceAdapterUnknownKind(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  governance:
    adapters:
      - name: audit-hub
        webhook:
          url: https://governance.example.com/events
          kinds:
            - unknown_kind
`))
	if err == nil {
		t.Fatal("expected Parse() to reject unknown governance adapter kind")
	}
}

func TestParseGovernanceDeliveryInheritsStoreBackendAndPath(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: jsonl
  path: /tmp/hopclaw-state
runtime:
  governance:
    adapters:
      - name: audit-hub
        webhook:
          url: https://governance.example.com/events
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Runtime.Governance.Delivery.Backend != "jsonl" {
		t.Fatalf("cfg.Runtime.Governance.Delivery.Backend = %q", cfg.Runtime.Governance.Delivery.Backend)
	}
	if cfg.Runtime.Governance.Delivery.Path != "/tmp/hopclaw-state" {
		t.Fatalf("cfg.Runtime.Governance.Delivery.Path = %q", cfg.Runtime.Governance.Delivery.Path)
	}
}

func TestParseRejectsUnsupportedGovernanceDeliveryBackend(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
store:
  backend: memory
runtime:
  governance:
    delivery:
      backend: redis
`))
	if err == nil {
		t.Fatal("expected Parse() to reject unsupported governance delivery backend")
	}
}

func TestParseRejectsProductionProfileWithoutRequiredSafetyControls(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "missing auth token",
			raw: `
store:
  backend: jsonl
  path: ./.hopclaw/state
runtime:
  profile: production
  audit:
    enabled: true
`,
		},
		{
			name: "audit disabled",
			raw: `
server:
  auth_token: secret
store:
  backend: jsonl
  path: ./.hopclaw/state
runtime:
  profile: production
  audit:
    enabled: false
`,
		},
		{
			name: "memory store",
			raw: `
server:
  auth_token: secret
store:
  backend: memory
runtime:
  profile: production
  audit:
    enabled: true
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tc.raw)); err == nil {
				t.Fatal("expected Parse() to reject unsafe production profile config")
			}
		})
	}
}

func TestParseRejectsInvalidAuthConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "jwt missing secret",
			raw: `
store:
  backend: memory
auth:
  jwt:
    algorithm: HS256
`,
		},
		{
			name: "oauth2 missing redirect uri",
			raw: `
store:
  backend: memory
auth:
  oauth2:
    client_id: desktop
    issuer: https://issuer.example.com
`,
		},
		{
			name: "session without primary auth",
			raw: `
store:
  backend: memory
auth:
  session:
    secure: false
`,
		},
		{
			name: "rbac invalid mode",
			raw: `
store:
  backend: memory
auth:
  bearer_token: secret
  rbac:
    mode: something
`,
		},
		{
			name: "rbac duplicate role",
			raw: `
store:
  backend: memory
auth:
  bearer_token: secret
  rbac:
    roles:
      - name: auditor
        grants:
          - resource: audit
            permissions: [read]
      - name: auditor
        grants:
          - resource: hooks
            permissions: [read]
`,
		},
		{
			name: "rbac invalid resource",
			raw: `
store:
  backend: memory
auth:
  bearer_token: secret
  rbac:
    roles:
      - name: auditor
        grants:
          - resource: dashboards
            permissions: [read]
`,
		},
		{
			name: "rbac invalid permission",
			raw: `
store:
  backend: memory
auth:
  bearer_token: secret
  rbac:
    roles:
      - name: auditor
        grants:
          - resource: audit
            permissions: [inspect]
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tc.raw)); err == nil {
				t.Fatal("expected Parse() to reject invalid auth config")
			}
		})
	}
}

func TestParseAcceptsRBACConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
store:
  backend: memory
auth:
  bearer_token: secret
  rbac:
    mode: overlay
    default_role: viewer
    scope_prefixes: ["rbac:"]
    role_metadata_keys: ["role", "rbac_role"]
    group_metadata_keys: ["groups", "team_groups"]
    group_roles:
      audit-team: auditor
    roles:
      - name: auditor
        extends: [viewer]
        grants:
          - resource: operator
            permissions: [read]
          - resource: audit
            permissions: [read]
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Auth.RBAC.Mode != "overlay" {
		t.Fatalf("auth.rbac.mode = %q", cfg.Auth.RBAC.Mode)
	}
	if cfg.Auth.RBAC.DefaultRole != "viewer" {
		t.Fatalf("auth.rbac.default_role = %q", cfg.Auth.RBAC.DefaultRole)
	}
	if len(cfg.Auth.RBAC.Roles) != 1 || cfg.Auth.RBAC.Roles[0].Name != "auditor" {
		t.Fatalf("unexpected auth.rbac.roles = %+v", cfg.Auth.RBAC.Roles)
	}
}

func TestResolveSecretsResolvesFields(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
server:
  auth_token: "keychain:server-token"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "keychain:openai-key"
    model: "gpt-4o"
channels:
  telegram:
    bot_token: "keychain:tg-token"
  slack:
    bot_token: "keychain:slack-bot"
    app_token: "keychain:slack-app"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Use a resolver that strips the "keychain:" prefix and adds "RESOLVED-".
	resolver := func(v string) string {
		const prefix = "keychain:"
		if len(v) > len(prefix) && v[:len(prefix)] == prefix {
			return "RESOLVED-" + v[len(prefix):]
		}
		return v
	}

	cfg.ResolveSecrets(resolver)

	if cfg.Server.AuthToken != "RESOLVED-server-token" {
		t.Fatalf("server.auth_token = %q", cfg.Server.AuthToken)
	}
	if cfg.Models.OpenAICompat.APIKey != "RESOLVED-openai-key" {
		t.Fatalf("models.openai_compat.api_key = %q", cfg.Models.OpenAICompat.APIKey)
	}
	if cfg.Channels.Telegram.BotToken != "RESOLVED-tg-token" {
		t.Fatalf("channels.telegram.bot_token = %q", cfg.Channels.Telegram.BotToken)
	}
	if cfg.Channels.Slack.BotToken != "RESOLVED-slack-bot" {
		t.Fatalf("channels.slack.bot_token = %q", cfg.Channels.Slack.BotToken)
	}
	if cfg.Channels.Slack.AppToken != "RESOLVED-slack-app" {
		t.Fatalf("channels.slack.app_token = %q", cfg.Channels.Slack.AppToken)
	}
}

func TestResolveSecretsNilResolver(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
server:
  auth_token: "original"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// nil resolver should be a no-op.
	cfg.ResolveSecrets(nil)

	if cfg.Server.AuthToken != "original" {
		t.Fatalf("server.auth_token = %q, want %q", cfg.Server.AuthToken, "original")
	}
}

func TestResolveSecretsProviderFields(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
agent:
  default_model: "provider1/model"
models:
  default_provider: provider1
  providers:
    provider1:
      api: openai-completions
      base_url: "https://api.example.com/v1"
      api_key: "keychain:p1-key"
      api_keys:
        - "keychain:p1-key-a"
        - "keychain:p1-key-b"
      secret_key: "keychain:p1-secret"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolver := func(v string) string {
		const prefix = "keychain:"
		if len(v) > len(prefix) && v[:len(prefix)] == prefix {
			return "R-" + v[len(prefix):]
		}
		return v
	}

	cfg.ResolveSecrets(resolver)

	p := cfg.Models.Providers["provider1"]
	if p.APIKey != "R-p1-key" {
		t.Fatalf("provider1.api_key = %q", p.APIKey)
	}
	if p.SecretKey != "R-p1-secret" {
		t.Fatalf("provider1.secret_key = %q", p.SecretKey)
	}
	if len(p.APIKeys) != 2 || p.APIKeys[0] != "R-p1-key-a" || p.APIKeys[1] != "R-p1-key-b" {
		t.Fatalf("provider1.api_keys = %v", p.APIKeys)
	}
}

func TestResolveSecretsLeavesLiteralsUnchanged(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`
server:
  auth_token: "my-literal-token"
channels:
  telegram:
    bot_token: "123456:ABC-DEF"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolver := func(v string) string {
		return v // identity resolver
	}

	cfg.ResolveSecrets(resolver)

	if cfg.Server.AuthToken != "my-literal-token" {
		t.Fatalf("server.auth_token = %q", cfg.Server.AuthToken)
	}
	if cfg.Channels.Telegram.BotToken != "123456:ABC-DEF" {
		t.Fatalf("channels.telegram.bot_token = %q", cfg.Channels.Telegram.BotToken)
	}
}

func TestResolveSecretsCoversControlPlaneFields(t *testing.T) {
	t.Parallel()

	cfg := Config{
		AuthZ: AuthZConfig{
			Webhook: AuthZWebhookConfig{
				Secret: "env:AUTHZ_WEBHOOK_SECRET",
				Headers: map[string]string{
					"Authorization": "keychain:authz-webhook-auth",
				},
			},
		},
		ExecApproval: ExecApprovalConfig{
			Providers: []ApprovalProviderConfig{{
				CallbackAuth: ApprovalCallbackAuthConfig{
					Token:  "env:APPROVAL_CALLBACK_TOKEN",
					Secret: "keychain:approval-callback-secret",
				},
				Webhook: ApprovalWebhookProviderConfig{
					Secret: "keychain:approval-webhook-secret",
					Headers: map[string]string{
						"Authorization": "env:APPROVAL_WEBHOOK_AUTH",
					},
				},
			}},
		},
		Runtime: RuntimeConfig{
			Governance: GovernanceConfig{
				Adapters: []GovernanceAdapterConfig{{
					Webhook: GovernanceWebhookAdapterConfig{
						Secret: "env:GOVERNANCE_WEBHOOK_SECRET",
						Headers: map[string]string{
							"X-API-Key": "keychain:governance-header-key",
						},
					},
				}},
			},
			Audit: AuditConfig{
				Sinks: []AuditSinkConfig{{
					Webhook: AuditWebhookSinkConfig{
						Secret: "keychain:audit-webhook-secret",
						Headers: map[string]string{
							"Authorization": "env:AUDIT_WEBHOOK_AUTH",
						},
					},
				}, {
					Elasticsearch: AuditElasticsearchSinkConfig{
						APIKey: "env:ELASTICSEARCH_AUDIT_API_KEY",
						Headers: map[string]string{
							"Authorization": "keychain:es-audit-auth",
						},
					},
				}, {
					SplunkHEC: AuditSplunkHECSinkConfig{
						Token: "keychain:SPLUNK_AUDIT_TOKEN",
						Headers: map[string]string{
							"Authorization": "env:SPLUNK_AUDIT_AUTH",
						},
					},
				}},
			},
		},
		Tools: ToolsConfig{
			Services: ServicesConfig{
				Calendar: CalendarServiceConfig{Password: "keychain:calendar-password"},
			},
		},
		Diagnostics: DiagnosticsConfig{
			TelemetryToken:              "env:TELEMETRY_TOKEN",
			TelemetryCollectorAuthToken: "keychain:telemetry-collector-token",
			UploadToken:                 "env:DIAGNOSTICS_UPLOAD_TOKEN",
			CollectorAuthToken:          "keychain:diagnostics-collector-token",
		},
	}

	resolver := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "env:") {
			return "R-" + strings.TrimPrefix(v, "env:")
		}
		if strings.HasPrefix(v, "keychain:") {
			return "R-" + strings.TrimPrefix(v, "keychain:")
		}
		return v
	}

	cfg.ResolveSecrets(resolver)

	if cfg.AuthZ.Webhook.Secret != "R-AUTHZ_WEBHOOK_SECRET" {
		t.Fatalf("authz webhook secret = %q", cfg.AuthZ.Webhook.Secret)
	}
	if cfg.AuthZ.Webhook.Headers["Authorization"] != "R-authz-webhook-auth" {
		t.Fatalf("authz webhook header = %q", cfg.AuthZ.Webhook.Headers["Authorization"])
	}
	provider := cfg.ExecApproval.Providers[0]
	if provider.CallbackAuth.Token != "R-APPROVAL_CALLBACK_TOKEN" {
		t.Fatalf("callback token = %q", provider.CallbackAuth.Token)
	}
	if provider.CallbackAuth.Secret != "R-approval-callback-secret" {
		t.Fatalf("callback secret = %q", provider.CallbackAuth.Secret)
	}
	if provider.Webhook.Secret != "R-approval-webhook-secret" {
		t.Fatalf("webhook secret = %q", provider.Webhook.Secret)
	}
	if provider.Webhook.Headers["Authorization"] != "R-APPROVAL_WEBHOOK_AUTH" {
		t.Fatalf("webhook header = %q", provider.Webhook.Headers["Authorization"])
	}
	adapter := cfg.Runtime.Governance.Adapters[0]
	if adapter.Webhook.Secret != "R-GOVERNANCE_WEBHOOK_SECRET" {
		t.Fatalf("governance secret = %q", adapter.Webhook.Secret)
	}
	if adapter.Webhook.Headers["X-API-Key"] != "R-governance-header-key" {
		t.Fatalf("governance header = %q", adapter.Webhook.Headers["X-API-Key"])
	}
	if cfg.Runtime.Audit.Sinks[0].Webhook.Secret != "R-audit-webhook-secret" {
		t.Fatalf("audit secret = %q", cfg.Runtime.Audit.Sinks[0].Webhook.Secret)
	}
	if cfg.Runtime.Audit.Sinks[0].Webhook.Headers["Authorization"] != "R-AUDIT_WEBHOOK_AUTH" {
		t.Fatalf("audit header = %q", cfg.Runtime.Audit.Sinks[0].Webhook.Headers["Authorization"])
	}
	if cfg.Runtime.Audit.Sinks[1].Elasticsearch.APIKey != "R-ELASTICSEARCH_AUDIT_API_KEY" {
		t.Fatalf("elasticsearch audit api key = %q", cfg.Runtime.Audit.Sinks[1].Elasticsearch.APIKey)
	}
	if cfg.Runtime.Audit.Sinks[1].Elasticsearch.Headers["Authorization"] != "R-es-audit-auth" {
		t.Fatalf("elasticsearch audit header = %q", cfg.Runtime.Audit.Sinks[1].Elasticsearch.Headers["Authorization"])
	}
	if cfg.Runtime.Audit.Sinks[2].SplunkHEC.Token != "R-SPLUNK_AUDIT_TOKEN" {
		t.Fatalf("splunk audit token = %q", cfg.Runtime.Audit.Sinks[2].SplunkHEC.Token)
	}
	if cfg.Runtime.Audit.Sinks[2].SplunkHEC.Headers["Authorization"] != "R-SPLUNK_AUDIT_AUTH" {
		t.Fatalf("splunk audit header = %q", cfg.Runtime.Audit.Sinks[2].SplunkHEC.Headers["Authorization"])
	}
	if cfg.Tools.Services.Calendar.Password != "R-calendar-password" {
		t.Fatalf("calendar password = %q", cfg.Tools.Services.Calendar.Password)
	}
	if cfg.Diagnostics.UploadToken != "R-DIAGNOSTICS_UPLOAD_TOKEN" {
		t.Fatalf("diagnostics upload token = %q", cfg.Diagnostics.UploadToken)
	}
	if cfg.Diagnostics.CollectorAuthToken != "R-diagnostics-collector-token" {
		t.Fatalf("diagnostics collector auth token = %q", cfg.Diagnostics.CollectorAuthToken)
	}
	if cfg.Diagnostics.TelemetryToken != "R-TELEMETRY_TOKEN" {
		t.Fatalf("diagnostics telemetry token = %q", cfg.Diagnostics.TelemetryToken)
	}
	if cfg.Diagnostics.TelemetryCollectorAuthToken != "R-telemetry-collector-token" {
		t.Fatalf("diagnostics telemetry collector auth token = %q", cfg.Diagnostics.TelemetryCollectorAuthToken)
	}
}

func TestSecretInventorySummarizesControlPlaneReferences(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{AuthToken: "keychain:server-auth"},
		Models: ModelsConfig{
			OpenAICompat: OpenAICompatConfig{
				Headers: map[string]string{
					"Authorization": "env:OPENAI_AUTH_HEADER",
					"Content-Type":  "application/json",
				},
			},
		},
		ExecApproval: ExecApprovalConfig{
			Providers: []ApprovalProviderConfig{{
				Webhook: ApprovalWebhookProviderConfig{
					Secret: "literal-secret",
				},
			}},
		},
		Runtime: RuntimeConfig{
			Governance: GovernanceConfig{
				Adapters: []GovernanceAdapterConfig{{
					Webhook: GovernanceWebhookAdapterConfig{
						Headers: map[string]string{
							"X-API-Key": "keychain:governance-api-key",
						},
					},
				}},
			},
		},
		Diagnostics: DiagnosticsConfig{
			TelemetryToken: "keychain:telemetry-token",
			UploadToken:    "env:DIAGNOSTICS_UPLOAD_TOKEN",
		},
	}

	inventory := cfg.SecretInventory()
	if inventory.Count != 6 {
		t.Fatalf("inventory.Count = %d, want 6", inventory.Count)
	}
	if inventory.ByKind["keychain"] != 3 {
		t.Fatalf("keychain count = %d, want 3", inventory.ByKind["keychain"])
	}
	if inventory.ByKind["env"] != 2 {
		t.Fatalf("env count = %d, want 2", inventory.ByKind["env"])
	}
	if inventory.ByKind["literal"] != 1 {
		t.Fatalf("literal count = %d, want 1", inventory.ByKind["literal"])
	}

	seen := map[string]SecretRefSummary{}
	for _, item := range inventory.Items {
		seen[item.Path] = item
	}
	if item := seen["server.auth_token"]; item.Kind != SecretRefKindKeychain || item.Locator != "keychain:server-auth" {
		t.Fatalf("server.auth_token = %+v", item)
	}
	if item := seen["models.openai_compat.headers[Authorization]"]; item.Kind != SecretRefKindEnv || item.Locator != "env:OPENAI_AUTH_HEADER" {
		t.Fatalf("openai auth header = %+v", item)
	}
	if item := seen["exec_approval.providers[0].webhook.secret"]; item.Kind != SecretRefKindLiteral || item.Locator != "" {
		t.Fatalf("approval webhook secret = %+v", item)
	}
	if item := seen["runtime.governance.adapters[0].webhook.headers[X-API-Key]"]; item.Kind != SecretRefKindKeychain || item.Locator != "keychain:governance-api-key" {
		t.Fatalf("governance api key = %+v", item)
	}
	if item := seen["diagnostics.upload_token"]; item.Kind != SecretRefKindEnv || item.Locator != "env:DIAGNOSTICS_UPLOAD_TOKEN" {
		t.Fatalf("diagnostics upload token = %+v", item)
	}
	if item := seen["diagnostics.telemetry_token"]; item.Kind != SecretRefKindKeychain || item.Locator != "keychain:telemetry-token" {
		t.Fatalf("diagnostics telemetry token = %+v", item)
	}
}

func TestSecretInventoryIncludesAuditConnectorSecrets(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Runtime: RuntimeConfig{
			Audit: AuditConfig{
				Sinks: []AuditSinkConfig{{
					Elasticsearch: AuditElasticsearchSinkConfig{
						APIKey: "env:ELASTICSEARCH_AUDIT_API_KEY",
						Headers: map[string]string{
							"Authorization": "keychain:es-audit-auth",
						},
					},
				}, {
					SplunkHEC: AuditSplunkHECSinkConfig{
						Token: "keychain:splunk-audit-token",
						Headers: map[string]string{
							"Authorization": "env:SPLUNK_AUDIT_AUTH",
						},
					},
				}},
			},
		},
	}

	inventory := cfg.SecretInventory()
	seen := map[string]SecretRefSummary{}
	for _, item := range inventory.Items {
		seen[item.Path] = item
	}
	if item := seen["runtime.audit.sinks[0].elasticsearch.api_key"]; item.Kind != SecretRefKindEnv || item.Locator != "env:ELASTICSEARCH_AUDIT_API_KEY" {
		t.Fatalf("elasticsearch api key = %+v", item)
	}
	if item := seen["runtime.audit.sinks[0].elasticsearch.headers[Authorization]"]; item.Kind != SecretRefKindKeychain || item.Locator != "keychain:es-audit-auth" {
		t.Fatalf("elasticsearch header = %+v", item)
	}
	if item := seen["runtime.audit.sinks[1].splunk_hec.token"]; item.Kind != SecretRefKindKeychain || item.Locator != "keychain:splunk-audit-token" {
		t.Fatalf("splunk token = %+v", item)
	}
	if item := seen["runtime.audit.sinks[1].splunk_hec.headers[Authorization]"]; item.Kind != SecretRefKindEnv || item.Locator != "env:SPLUNK_AUDIT_AUTH" {
		t.Fatalf("splunk header = %+v", item)
	}
}
