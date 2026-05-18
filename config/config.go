package config

import (
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	Server        ServerConfig        `json:"server" yaml:"server"`
	Auth          AuthConfig          `json:"auth" yaml:"auth"`
	AuthZ         AuthZConfig         `json:"authz,omitempty" yaml:"authz"`
	Store         StoreConfig         `json:"store" yaml:"store"`
	Agent         AgentConfig         `json:"agent" yaml:"agent"`
	Runtime       RuntimeConfig       `json:"runtime" yaml:"runtime"`
	Update        UpdateConfig        `json:"update" yaml:"update"`
	Diagnostics   DiagnosticsConfig   `json:"diagnostics" yaml:"diagnostics"`
	Skills        SkillsConfig        `json:"skills" yaml:"skills"`
	Models        ModelsConfig        `json:"models" yaml:"models"`
	Tools         ToolsConfig         `json:"tools" yaml:"tools"`
	Hosts         HostsConfig         `json:"hosts" yaml:"hosts"`
	Channels      ChannelsConfig      `json:"channels" yaml:"channels"`
	Plugins       PluginsConfig       `json:"plugins" yaml:"plugins"`
	Cron          CronConfig          `json:"cron" yaml:"cron"`
	Watch         WatchConfig         `json:"watch" yaml:"watch"`
	Heartbeat     HeartbeatConfig     `json:"heartbeat" yaml:"heartbeat"`
	Wire          WireConfig          `json:"wire" yaml:"wire"`
	Wakeup        WakeupConfig        `json:"wakeup" yaml:"wakeup"`
	Allowlist     AllowlistConfig     `json:"allowlist" yaml:"allowlist"`
	Sandbox       SandboxConfig       `json:"sandbox" yaml:"sandbox"`
	Isolation     IsolationConfig     `json:"isolation" yaml:"isolation"`
	Tunnel        TunnelConfig        `json:"tunnel" yaml:"tunnel"`
	ExecApproval  ExecApprovalConfig  `json:"exec_approval" yaml:"exec_approval"`
	ChannelHealth ChannelHealthConfig `json:"channel_health" yaml:"channel_health"`
	Embedding     EmbeddingConfig     `json:"embedding" yaml:"embedding"`
	Security      SecurityConfig      `json:"security" yaml:"security"`
	Discovery     DiscoveryConfig     `json:"discovery" yaml:"discovery"`
	Canvas        CanvasConfig        `json:"canvas" yaml:"canvas"`
	UsageStorage  string              `json:"usage_storage" yaml:"usage_storage"`
	MemoryStorage string              `json:"memory_storage" yaml:"memory_storage"`
	Logging       LoggingConfig       `json:"logging" yaml:"logging"`
	Locale        string              `json:"locale,omitempty" yaml:"locale"` // en, zh-CN, etc.
}

// SecretResolver resolves a config field value that may reference a
// keychain entry or environment variable. If resolution fails, the
// original value is returned unchanged so config loading is non-breaking.
type SecretResolver func(value string) string

// ResolveSecrets walks credential fields and applies resolver to each.
// Fields that are empty or do not start with a recognized prefix are
// returned unchanged. Callers should supply keychain.ResolveField as
// the resolver in production.
func (c *Config) ResolveSecrets(resolve SecretResolver) {
	if resolve == nil {
		return
	}
	c.walkSecretFields(func(_ string, value *string) {
		*value = resolve(*value)
	})
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	expanded := os.ExpandEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
