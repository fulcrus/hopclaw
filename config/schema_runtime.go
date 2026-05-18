package config

import "time"

type ServerConfig struct {
	Address   string `yaml:"address"`
	AuthToken string `yaml:"auth_token"`
	Version   string `yaml:"-"`
}

type StoreConfig struct {
	Backend string `yaml:"backend"`
	Path    string `yaml:"path"`
}

type AgentConfig struct {
	SystemPrompt            string                `yaml:"system_prompt"`
	DefaultModel            string                `yaml:"default_model"`
	TriageModel             string                `yaml:"triage_model"`
	MinActionConfidence     float64               `yaml:"min_action_confidence"`
	MaxRunDuration          time.Duration         `yaml:"max_run_duration"`
	MaxToolRounds           int                   `yaml:"max_tool_rounds"`
	MaxToolRecoveryAttempts int                   `yaml:"max_tool_recovery_attempts"`
	QueueMode               string                `yaml:"queue_mode"`
	DedupeWindow            time.Duration         `yaml:"dedupe_window"`
	DefaultContextWindow    int                   `yaml:"default_context_window"`
	SubmitRateLimit         SubmitRateLimitConfig `yaml:"submit_rate_limit" json:"submit_rate_limit"`
}

type SubmitRateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute" json:"requests_per_minute"`
	BurstSize         int `yaml:"burst_size" json:"burst_size"`
}

type RuntimeConfig struct {
	Profile             string             `yaml:"profile"`
	StatusReminderDelay time.Duration      `yaml:"status_reminder_delay"`
	Audit               AuditConfig        `yaml:"audit"`
	Governance          GovernanceConfig   `yaml:"governance" json:"governance,omitempty"`
	Verification        VerificationConfig `yaml:"verification" json:"verification,omitempty"`
	Artifacts           ArtifactsConfig    `yaml:"artifacts"`
	State               StatusConfig       `yaml:"state"`
}

type VerificationConfig struct {
	VerifierSeverities map[string]string `yaml:"verifier_severities" json:"verifier_severities,omitempty"`
}

const (
	RuntimeProfileDesktop        = "desktop"
	RuntimeProfileTrustedDesktop = "trusted_desktop"
	RuntimeProfileProduction     = "production"
)

type AuditConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Output   string              `yaml:"output"`
	Delivery AuditDeliveryConfig `yaml:"delivery" json:"delivery,omitempty"`
	Sinks    []AuditSinkConfig   `yaml:"sinks" json:"sinks,omitempty"`
}

type AuditDeliveryConfig struct {
	Backend      string        `yaml:"backend" json:"backend,omitempty"`
	MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts,omitempty"`
	BaseBackoff  time.Duration `yaml:"base_backoff" json:"base_backoff,omitempty"`
	MaxBackoff   time.Duration `yaml:"max_backoff" json:"max_backoff,omitempty"`
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval,omitempty"`
	BatchSize    int           `yaml:"batch_size" json:"batch_size,omitempty"`
}

type AuditSinkConfig struct {
	Name          string                       `yaml:"name" json:"name,omitempty"`
	Type          string                       `yaml:"type" json:"type,omitempty"`
	Enabled       *bool                        `yaml:"enabled" json:"enabled,omitempty"`
	Webhook       AuditWebhookSinkConfig       `yaml:"webhook" json:"webhook,omitempty"`
	Elasticsearch AuditElasticsearchSinkConfig `yaml:"elasticsearch" json:"elasticsearch,omitempty"`
	SplunkHEC     AuditSplunkHECSinkConfig     `yaml:"splunk_hec" json:"splunk_hec,omitempty"`
	Metadata      map[string]any               `yaml:"metadata" json:"metadata,omitempty"`
}

type AuditWebhookSinkConfig struct {
	URL     string            `yaml:"url" json:"url,omitempty"`
	Timeout time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
	Secret  string            `yaml:"secret" json:"secret,omitempty"`
}

type AuditElasticsearchSinkConfig struct {
	URL     string            `yaml:"url" json:"url,omitempty"`
	Index   string            `yaml:"index" json:"index,omitempty"`
	Timeout time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
	APIKey  string            `yaml:"api_key" json:"api_key,omitempty"`
}

type AuditSplunkHECSinkConfig struct {
	URL        string            `yaml:"url" json:"url,omitempty"`
	Token      string            `yaml:"token" json:"token,omitempty"`
	Timeout    time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers    map[string]string `yaml:"headers" json:"headers,omitempty"`
	Source     string            `yaml:"source" json:"source,omitempty"`
	SourceType string            `yaml:"source_type" json:"source_type,omitempty"`
	Index      string            `yaml:"index" json:"index,omitempty"`
	Host       string            `yaml:"host" json:"host,omitempty"`
}

type GovernanceConfig struct {
	Delivery GovernanceDeliveryConfig  `yaml:"delivery" json:"delivery,omitempty"`
	Adapters []GovernanceAdapterConfig `yaml:"adapters" json:"adapters,omitempty"`
}

type GovernanceDeliveryConfig struct {
	Backend      string        `yaml:"backend" json:"backend,omitempty"`
	Path         string        `yaml:"path" json:"path,omitempty"`
	MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts,omitempty"`
	BaseBackoff  time.Duration `yaml:"base_backoff" json:"base_backoff,omitempty"`
	MaxBackoff   time.Duration `yaml:"max_backoff" json:"max_backoff,omitempty"`
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval,omitempty"`
	BatchSize    int           `yaml:"batch_size" json:"batch_size,omitempty"`
}

type GovernanceAdapterConfig struct {
	Name     string                         `yaml:"name" json:"name,omitempty"`
	Type     string                         `yaml:"type" json:"type,omitempty"`
	Enabled  *bool                          `yaml:"enabled" json:"enabled,omitempty"`
	Webhook  GovernanceWebhookAdapterConfig `yaml:"webhook" json:"webhook,omitempty"`
	Metadata map[string]any                 `yaml:"metadata" json:"metadata,omitempty"`
}

type GovernanceWebhookAdapterConfig struct {
	URL             string            `yaml:"url" json:"url,omitempty"`
	Timeout         time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers         map[string]string `yaml:"headers" json:"headers,omitempty"`
	Secret          string            `yaml:"secret" json:"secret,omitempty"`
	IncludeSnapshot *bool             `yaml:"include_snapshot" json:"include_snapshot,omitempty"`
	Kinds           []string          `yaml:"kinds" json:"kinds,omitempty"`
}

type ArtifactsConfig struct {
	Enabled         *bool         `yaml:"enabled"`
	Path            string        `yaml:"path"`
	InlineThreshold int           `yaml:"inline_threshold"`
	PreviewChars    int           `yaml:"preview_chars"`
	Retention       time.Duration `yaml:"retention"`
}

type StatusConfig struct {
	SessionsRetention time.Duration `yaml:"sessions_retention"`
	RunsRetention     time.Duration `yaml:"runs_retention"`
	EventsRetention   time.Duration `yaml:"events_retention"`
	PruneInterval     time.Duration `yaml:"prune_interval"`
	JSONLStartupLimit int           `yaml:"jsonl_startup_limit"`
}

type StateConfig = StatusConfig

type SkillsConfig struct {
	IncludeCatalog  bool                      `yaml:"include_catalog"`
	Dirs            []string                  `yaml:"dirs"`
	Config          map[string]map[string]any `yaml:"config"`
	AutoDetect      bool                      `yaml:"auto_detect"`
	AutoRefresh     *bool                     `yaml:"auto_refresh"`
	RefreshInterval time.Duration             `yaml:"refresh_interval"`
	InstallPolicy   string                    `yaml:"install_policy"`
	EnsureLimit     int                       `yaml:"ensure_limit"`
	Hub             SkillsHubConfig           `yaml:"hub"`
}

type SkillsHubConfig struct {
	URL         string   `yaml:"url"`
	Token       string   `yaml:"token"`
	Sources     []string `yaml:"sources"`
	SyncOnStart bool     `yaml:"sync_on_start"`
}

const (
	SkillInstallPolicyAsk  = "ask"
	SkillInstallPolicyAuto = "auto"
	SkillInstallPolicyDeny = "deny"
)

type ModelsConfig struct {
	DefaultProvider string                    `yaml:"default_provider"`
	OpenAICompat    OpenAICompatConfig        `yaml:"openai_compat"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
}

type OpenAICompatConfig struct {
	BaseURL   string            `yaml:"base_url"`
	APIKey    string            `yaml:"api_key"`
	Fallbacks []string          `yaml:"fallbacks"`
	Model     string            `yaml:"model"`
	Timeout   time.Duration     `yaml:"timeout"`
	Headers   map[string]string `yaml:"headers"`
}

type ProviderConfig struct {
	API          string            `yaml:"api"`
	BaseURL      string            `yaml:"base_url"`
	Region       string            `yaml:"region"`
	APIKey       string            `yaml:"api_key"`
	APIKeys      []string          `yaml:"api_keys"`
	Fallbacks    []string          `yaml:"fallbacks"`
	AccessKeyID  string            `yaml:"access_key_id"`
	SecretKey    string            `yaml:"secret_key"`
	SessionToken string            `yaml:"session_token"`
	DefaultModel string            `yaml:"default_model"`
	Timeout      time.Duration     `yaml:"timeout"`
	Headers      map[string]string `yaml:"headers"`
}

type ToolsConfig struct {
	Builtins     BuiltinsConfig       `yaml:"builtins"`
	LocalExec    LocalExecConfig      `yaml:"local_exec"`
	Capabilities CapabilitiesConfig   `yaml:"capabilities"`
	Services     ServicesConfig       `yaml:"services"`
	External     []ExternalToolConfig `yaml:"external"`
}

type CapabilitiesConfig struct {
	Exec   ExecConstraints `yaml:"exec"`
	Net    NetConstraints  `yaml:"net"`
	FS     FSConstraints   `yaml:"fs"`
	Layer2 Layer2Config    `yaml:"layer2"`
}

type ExecConstraints struct {
	Mode      string        `yaml:"mode"`
	Allowlist []string      `yaml:"allowlist"`
	Denylist  []string      `yaml:"denylist"`
	Timeout   time.Duration `yaml:"timeout"`
	MaxOutput int           `yaml:"max_output"`
}

type NetConstraints struct {
	AllowPrivate bool     `yaml:"allow_private"`
	AllowLocal   *bool    `yaml:"allow_local"`
	MaxDownload  int64    `yaml:"max_download"`
	DenyHosts    []string `yaml:"deny_hosts"`
	AllowHosts   []string `yaml:"allow_hosts"`
}

type FSConstraints struct {
	AllowedRoots []string `yaml:"allowed_roots"`
	DenyPatterns []string `yaml:"deny_patterns"`
	SkipDirs     []string `yaml:"skip_dirs"`
}

type Layer2Config struct {
	Git       *bool `yaml:"git"`
	Media     *bool `yaml:"media"`
	Container *bool `yaml:"container"`
	Packages  *bool `yaml:"packages"`
	Search    *bool `yaml:"search"`
	Speech    *bool `yaml:"speech"`
	Email     *bool `yaml:"email"`
	Session   *bool `yaml:"session"`
	Calendar  *bool `yaml:"calendar"`
}

type ServicesConfig struct {
	Search   SearchServiceConfig   `yaml:"search"`
	Email    EmailServiceConfig    `yaml:"email"`
	Speech   SpeechServiceConfig   `yaml:"speech"`
	Calendar CalendarServiceConfig `yaml:"calendar"`
}

type CalendarServiceConfig struct {
	CalDAVURL string `yaml:"caldav_url"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type SearchServiceConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"`
}

type EmailServiceConfig struct {
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	IMAPHost string `yaml:"imap_host"`
	IMAPPort int    `yaml:"imap_port"`
}

type SpeechServiceConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

type BuiltinsConfig struct {
	Enabled            *bool         `yaml:"enabled"`
	Root               string        `yaml:"root"`
	AllowedPaths       []string      `yaml:"allowed_paths"`
	DefaultExecTimeout time.Duration `yaml:"default_exec_timeout"`
	MaxReadBytes       int           `yaml:"max_read_bytes"`
}

type LocalExecConfig struct {
	Enabled        *bool         `yaml:"enabled"`
	DefaultTimeout time.Duration `yaml:"default_timeout"`
}

type PluginsConfig struct {
	Enabled      *bool    `yaml:"enabled"`
	Dirs         []string `yaml:"dirs"`
	AutoDiscover bool     `yaml:"auto_discover"`
}

type ExternalToolConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Endpoint    string         `yaml:"endpoint"`
	Timeout     string         `yaml:"timeout,omitempty"`
	InputSchema map[string]any `yaml:"input_schema,omitempty"`
}

type WebhookChannelConfig struct {
	Enabled   *bool                            `yaml:"enabled"`
	Instances map[string]WebhookInstanceConfig `yaml:"instances"`
}

type WebhookInstanceConfig struct {
	CallbackURL string `yaml:"callback_url"`
	Secret      string `yaml:"secret,omitempty"`
}

type HostsConfig struct {
	Browser BrowserHostConfig `yaml:"browser"`
	Desktop DesktopHostConfig `yaml:"desktop"`
}

type BrowserHostConfig struct {
	Enabled     *bool         `yaml:"enabled"`
	BaseURL     string        `yaml:"base_url"`
	AuthToken   string        `yaml:"auth_token"`
	ChromePath  string        `yaml:"chrome_path,omitempty"`
	Headless    *bool         `yaml:"headless,omitempty"`
	NoSandbox   *bool         `yaml:"no_sandbox,omitempty"`
	IdleTimeout time.Duration `yaml:"idle_timeout,omitempty"`
}

type DesktopHostConfig struct {
	Enabled     *bool         `yaml:"enabled"`
	BaseURL     string        `yaml:"base_url"`
	AuthToken   string        `yaml:"auth_token"`
	IdleTimeout time.Duration `yaml:"idle_timeout,omitempty"`
}
