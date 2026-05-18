package config

import (
	"strings"
	"time"
)

type UpdateConfig struct {
	Enabled       *bool         `yaml:"enabled" json:"enabled,omitempty"`
	CheckOnStart  *bool         `yaml:"check_on_start" json:"check_on_start,omitempty"`
	CheckInterval time.Duration `yaml:"check_interval" json:"check_interval,omitempty"`
	Channel       string        `yaml:"channel" json:"channel,omitempty"`
	ManifestURL   string        `yaml:"manifest_url" json:"manifest_url,omitempty"`
	SkipVersion   string        `yaml:"skip_version" json:"skip_version,omitempty"`
}

type DiagnosticsConfig struct {
	Enabled                          *bool         `yaml:"enabled" json:"enabled,omitempty"`
	BugReportDir                     string        `yaml:"bug_report_dir" json:"bug_report_dir,omitempty"`
	IncludeLogs                      *bool         `yaml:"include_logs" json:"include_logs,omitempty"`
	MaxLogBytes                      int64         `yaml:"max_log_bytes" json:"max_log_bytes,omitempty"`
	RedactPatterns                   []string      `yaml:"redact_patterns" json:"redact_patterns,omitempty"`
	TelemetryEnabled                 *bool         `yaml:"telemetry_enabled" json:"telemetry_enabled,omitempty"`
	TelemetryEndpoint                string        `yaml:"telemetry_endpoint" json:"telemetry_endpoint,omitempty"`
	TelemetryToken                   string        `yaml:"telemetry_token" json:"telemetry_token,omitempty"`
	TelemetryTimeout                 time.Duration `yaml:"telemetry_timeout" json:"telemetry_timeout,omitempty"`
	TelemetryDebugLog                *bool         `yaml:"telemetry_debug_log" json:"telemetry_debug_log,omitempty"`
	TelemetryCollectorEnabled        *bool         `yaml:"telemetry_collector_enabled" json:"telemetry_collector_enabled,omitempty"`
	TelemetryCollectorDir            string        `yaml:"telemetry_collector_dir" json:"telemetry_collector_dir,omitempty"`
	TelemetryCollectorAuthToken      string        `yaml:"telemetry_collector_auth_token" json:"telemetry_collector_auth_token,omitempty"`
	TelemetryCollectorMaxUploadBytes int64         `yaml:"telemetry_collector_max_upload_bytes" json:"telemetry_collector_max_upload_bytes,omitempty"`
	CrashReportsEnabled              *bool         `yaml:"crash_reports_enabled" json:"crash_reports_enabled,omitempty"`
	UploadURL                        string        `yaml:"upload_url" json:"upload_url,omitempty"`
	UploadToken                      string        `yaml:"upload_token" json:"upload_token,omitempty"`
	UploadTimeout                    time.Duration `yaml:"upload_timeout" json:"upload_timeout,omitempty"`
	CollectorEnabled                 *bool         `yaml:"collector_enabled" json:"collector_enabled,omitempty"`
	CollectorDir                     string        `yaml:"collector_dir" json:"collector_dir,omitempty"`
	CollectorAuthToken               string        `yaml:"collector_auth_token" json:"collector_auth_token,omitempty"`
	CollectorMaxUploadBytes          int64         `yaml:"collector_max_upload_bytes" json:"collector_max_upload_bytes,omitempty"`
}

type LoggingConfig struct {
	Level           string            `json:"level" yaml:"level"`
	Format          string            `json:"format" yaml:"format"`
	Output          string            `json:"output" yaml:"output"`
	FilePath        string            `json:"file_path" yaml:"file_path"`
	MaxSizeMB       int               `json:"max_size_mb" yaml:"max_size_mb"`
	RedactKeys      []string          `json:"redact_keys" yaml:"redact_keys"`
	SubsystemLevels map[string]string `json:"subsystem_levels" yaml:"subsystem_levels"`
	ConsoleCapture  bool              `json:"console_capture" yaml:"console_capture"`
	Sampling        LogSamplingConfig `json:"sampling" yaml:"sampling"`
}

type LogSamplingConfig struct {
	Enabled     bool `json:"enabled" yaml:"enabled"`
	InitialN    int  `json:"initial_n" yaml:"initial_n"`
	ThereafterN int  `json:"thereafter_n" yaml:"thereafter_n"`
	IntervalSec int  `json:"interval_sec" yaml:"interval_sec"`
}

type SecurityConfig struct {
	AllowedPaths    []string          `json:"allowed_paths" yaml:"allowed_paths"`
	BlockedDomains  []string          `json:"blocked_domains" yaml:"blocked_domains"`
	BlockedCommands []string          `json:"blocked_commands" yaml:"blocked_commands"`
	MaxContentSize  int64             `json:"max_content_size" yaml:"max_content_size"`
	DangerousTools  []string          `json:"dangerous_tools" yaml:"dangerous_tools"`
	CustomPatterns  []SecurityPattern `json:"custom_patterns" yaml:"custom_patterns"`
}

type SecurityPattern struct {
	Name     string `json:"name" yaml:"name"`
	Pattern  string `json:"pattern" yaml:"pattern"`
	Severity string `json:"severity" yaml:"severity"`
	Category string `json:"category" yaml:"category"`
}

type EmbeddingConfig struct {
	Enabled   *bool                              `yaml:"enabled" json:"enabled,omitempty"`
	Provider  string                             `yaml:"provider" json:"provider,omitempty"`
	BaseURL   string                             `yaml:"base_url" json:"base_url,omitempty"`
	APIKey    string                             `yaml:"api_key" json:"api_key,omitempty"`
	Model     string                             `yaml:"model" json:"model,omitempty"`
	Fallback  string                             `yaml:"fallback" json:"fallback,omitempty"`
	CacheSize int                                `yaml:"cache_size" json:"cache_size,omitempty"`
	Providers map[string]EmbeddingProviderConfig `yaml:"providers" json:"providers,omitempty"`
}

type EmbeddingProviderConfig struct {
	API     string `yaml:"api" json:"api,omitempty"`
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	APIKey  string `yaml:"api_key" json:"api_key,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

type CronConfig struct {
	Enabled          *bool         `yaml:"enabled" json:"enabled"`
	StorePath        string        `yaml:"store_path" json:"store_path"`
	ExecutionTimeout time.Duration `yaml:"execution_timeout" json:"execution_timeout"`
}

type WatchConfig struct {
	Enabled          *bool         `yaml:"enabled" json:"enabled"`
	StorePath        string        `yaml:"store_path" json:"store_path"`
	ExecutionTimeout time.Duration `yaml:"execution_timeout" json:"execution_timeout"`
}

type HeartbeatConfig struct {
	Enabled  *bool         `yaml:"enabled" json:"enabled"`
	Interval time.Duration `yaml:"interval" json:"interval"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout"`
}

type WireConfig struct {
	Enabled       *bool         `yaml:"enabled" json:"enabled"`
	MaxEntries    int           `yaml:"max_entries" json:"max_entries"`
	MaxBodyBytes  int           `yaml:"max_body_bytes" json:"max_body_bytes"`
	RetentionTime time.Duration `yaml:"retention_time" json:"retention_time"`
	RedactHeaders []string      `yaml:"redact_headers" json:"redact_headers"`
	Providers     []string      `yaml:"providers" json:"providers"`
}

type WakeupConfig struct {
	Enabled   *bool  `yaml:"enabled"`
	StorePath string `yaml:"store_path"`
}

type AllowlistConfig struct {
	Enabled  *bool                    `yaml:"enabled"`
	Channels []AllowlistChannelConfig `yaml:"channels"`
}

type AllowlistChannelConfig struct {
	Channel     string   `yaml:"channel"`
	AllowAll    bool     `yaml:"allow_all"`
	AllowUsers  []string `yaml:"allow_users"`
	DenyUsers   []string `yaml:"deny_users"`
	AllowGroups []string `yaml:"allow_groups"`
	DenyGroups  []string `yaml:"deny_groups"`
}

type SandboxConfig struct {
	Enabled       *bool    `yaml:"enabled"`
	Image         string   `yaml:"image"`
	MemoryLimit   string   `yaml:"memory_limit"`
	CPULimit      string   `yaml:"cpu_limit"`
	Timeout       int      `yaml:"timeout"`
	NetworkMode   string   `yaml:"network_mode"`
	WorkDir       string   `yaml:"work_dir"`
	AllowedImages []string `yaml:"allowed_images"`
	ProcessMode   *bool    `yaml:"process_mode"`
}

type IsolationConfig struct {
	Enabled *bool  `yaml:"enabled"`
	BaseDir string `yaml:"base_dir"`
}

type TunnelConfig struct {
	Enabled    *bool  `yaml:"enabled"`
	Provider   string `yaml:"provider"`    // "ssh" or "tailscale"
	Host       string `yaml:"host"`        // SSH: remote host
	Port       int    `yaml:"port"`        // SSH: remote port (default 22)
	User       string `yaml:"user"`        // SSH: user
	KeyFile    string `yaml:"key_file"`    // SSH: private key path
	RemoteHost string `yaml:"remote_host"` // SSH: remote bind host
	RemotePort int    `yaml:"remote_port"` // SSH: remote bind port
	LocalPort  int    `yaml:"local_port"`  // local port to expose
	AuthToken  string `yaml:"auth_token"`  // provider auth token
}

type DiscoveryConfig struct {
	Enabled      *bool    `yaml:"enabled" json:"enabled,omitempty"`
	Method       string   `yaml:"method" json:"method,omitempty"`
	Service      string   `yaml:"service" json:"service,omitempty"`
	Peers        []string `yaml:"peers" json:"peers,omitempty"`
	InstanceName string   `yaml:"instance_name" json:"instance_name,omitempty"`
	Port         int      `yaml:"port" json:"port,omitempty"`
	Interface    string   `yaml:"interface" json:"interface,omitempty"`
}

type CanvasConfig struct {
	Enabled    *bool         `yaml:"enabled" json:"enabled,omitempty"`
	Port       int           `yaml:"port" json:"port,omitempty"`
	Root       string        `yaml:"root" json:"root,omitempty"`
	LiveReload *bool         `yaml:"live_reload" json:"live_reload,omitempty"`
	TokenTTL   time.Duration `yaml:"token_ttl" json:"token_ttl,omitempty"`
}

type ExecApprovalConfig struct {
	SafePatterns    []string                 `yaml:"safe_patterns" json:"safe_patterns,omitempty"`
	ApprovalTimeout time.Duration            `yaml:"approval_timeout" json:"approval_timeout,omitempty"`
	GracePeriod     time.Duration            `yaml:"grace_period" json:"grace_period,omitempty"`
	Providers       []ApprovalProviderConfig `yaml:"providers" json:"providers,omitempty"`
}

type ApprovalProviderConfig struct {
	Name         string                        `yaml:"name" json:"name,omitempty"`
	Type         string                        `yaml:"type" json:"type,omitempty"`
	Enabled      *bool                         `yaml:"enabled" json:"enabled,omitempty"`
	CallbackAuth ApprovalCallbackAuthConfig    `yaml:"callback_auth" json:"callback_auth,omitempty"`
	Webhook      ApprovalWebhookProviderConfig `yaml:"webhook" json:"webhook,omitempty"`
	Metadata     map[string]any                `yaml:"metadata" json:"metadata,omitempty"`
}

type ApprovalCallbackAuthConfig struct {
	Mode            string        `yaml:"mode" json:"mode,omitempty"`
	HeaderName      string        `yaml:"header_name" json:"header_name,omitempty"`
	Token           string        `yaml:"token" json:"token,omitempty"`
	Secret          string        `yaml:"secret" json:"secret,omitempty"`
	SignatureHeader string        `yaml:"signature_header" json:"signature_header,omitempty"`
	TimestampHeader string        `yaml:"timestamp_header" json:"timestamp_header,omitempty"`
	MaxAge          time.Duration `yaml:"max_age" json:"max_age,omitempty"`
}

type ApprovalWebhookProviderConfig struct {
	SubmitURL string            `yaml:"submit_url" json:"submit_url,omitempty"`
	UpdateURL string            `yaml:"update_url" json:"update_url,omitempty"`
	SyncURL   string            `yaml:"sync_url" json:"sync_url,omitempty"`
	Timeout   time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers   map[string]string `yaml:"headers" json:"headers,omitempty"`
	Secret    string            `yaml:"secret" json:"secret,omitempty"`
}

type ChannelHealthConfig struct {
	Enabled            *bool         `yaml:"enabled" json:"enabled,omitempty"`
	CheckInterval      time.Duration `yaml:"check_interval" json:"check_interval,omitempty"`
	StaleSocketTimeout time.Duration `yaml:"stale_socket_timeout" json:"stale_socket_timeout,omitempty"`
	StuckRunTimeout    time.Duration `yaml:"stuck_run_timeout" json:"stuck_run_timeout,omitempty"`
	StartupGrace       time.Duration `yaml:"startup_grace" json:"startup_grace,omitempty"`
	MaxRestartsPerHour int           `yaml:"max_restarts_per_hour" json:"max_restarts_per_hour,omitempty"`
}

type AuthConfig struct {
	BearerToken string             `yaml:"bearer_token" json:"bearer_token,omitempty"`
	JWT         *AuthJWTConfig     `yaml:"jwt" json:"jwt,omitempty"`
	APIKeys     []AuthKeyEntry     `yaml:"api_keys" json:"api_keys,omitempty"`
	OAuth2      *AuthOAuth2Config  `yaml:"oauth2" json:"oauth2,omitempty"`
	Session     *AuthSessionConfig `yaml:"session" json:"session,omitempty"`
	RBAC        AuthRBACConfig     `yaml:"rbac" json:"rbac,omitempty"`
}

type AuthZConfig struct {
	Mode     string             `yaml:"mode" json:"mode,omitempty"`
	Fallback string             `yaml:"fallback" json:"fallback,omitempty"`
	Webhook  AuthZWebhookConfig `yaml:"webhook" json:"webhook,omitempty"`
}

type AuthZWebhookConfig struct {
	URL     string            `yaml:"url" json:"url,omitempty"`
	Timeout time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
	Secret  string            `yaml:"secret" json:"secret,omitempty"`
}

type AuthRBACConfig struct {
	Mode              string               `yaml:"mode" json:"mode,omitempty"`
	DefaultRole       string               `yaml:"default_role" json:"default_role,omitempty"`
	ScopePrefixes     []string             `yaml:"scope_prefixes" json:"scope_prefixes,omitempty"`
	RoleMetadataKeys  []string             `yaml:"role_metadata_keys" json:"role_metadata_keys,omitempty"`
	GroupMetadataKeys []string             `yaml:"group_metadata_keys" json:"group_metadata_keys,omitempty"`
	GroupRoles        map[string]string    `yaml:"group_roles" json:"group_roles,omitempty"`
	Roles             []AuthRBACRoleConfig `yaml:"roles" json:"roles,omitempty"`
}

type AuthRBACRoleConfig struct {
	Name    string                `yaml:"name" json:"name"`
	Extends []string              `yaml:"extends,omitempty" json:"extends,omitempty"`
	Replace bool                  `yaml:"replace,omitempty" json:"replace,omitempty"`
	Grants  []AuthRBACGrantConfig `yaml:"grants,omitempty" json:"grants,omitempty"`
}

type AuthRBACGrantConfig struct {
	Resource    string   `yaml:"resource" json:"resource"`
	Permissions []string `yaml:"permissions" json:"permissions"`
}

type AuthOAuth2Config struct {
	Issuer       string   `yaml:"issuer" json:"issuer,omitempty"`
	ClientID     string   `yaml:"client_id" json:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret" json:"client_secret,omitempty"`
	RedirectURI  string   `yaml:"redirect_uri" json:"redirect_uri,omitempty"`
	Scopes       []string `yaml:"scopes" json:"scopes,omitempty"`
	DiscoveryURL string   `yaml:"discovery_url" json:"discovery_url,omitempty"`
}

type AuthSessionConfig struct {
	CookieName   string        `yaml:"cookie_name" json:"cookie_name,omitempty"`
	CookieDomain string        `yaml:"cookie_domain" json:"cookie_domain,omitempty"`
	MaxAge       time.Duration `yaml:"max_age" json:"max_age,omitempty"`
	Secure       bool          `yaml:"secure" json:"secure,omitempty"`
}

type AuthJWTConfig struct {
	Secret    string        `yaml:"secret" json:"secret,omitempty"`
	PublicKey string        `yaml:"public_key" json:"public_key,omitempty"`
	Issuer    string        `yaml:"issuer" json:"issuer,omitempty"`
	Audience  string        `yaml:"audience" json:"audience,omitempty"`
	Algorithm string        `yaml:"algorithm" json:"algorithm,omitempty"`
	ClockSkew time.Duration `yaml:"clock_skew" json:"clock_skew,omitempty"`
}

type AuthKeyEntry struct {
	Key     string   `yaml:"key" json:"key"`
	Name    string   `yaml:"name" json:"name"`
	Scopes  []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	Enabled bool     `yaml:"enabled" json:"enabled"`
}

func (c *Config) HasAuth() bool {
	if strings.TrimSpace(c.Server.AuthToken) != "" {
		return true
	}
	if strings.TrimSpace(c.Auth.BearerToken) != "" {
		return true
	}
	if c.Auth.JWT != nil && (strings.TrimSpace(c.Auth.JWT.Secret) != "" || strings.TrimSpace(c.Auth.JWT.PublicKey) != "") {
		return true
	}
	if len(c.Auth.APIKeys) > 0 {
		return true
	}
	if c.Auth.OAuth2 != nil && strings.TrimSpace(c.Auth.OAuth2.ClientID) != "" {
		return true
	}
	if c.Auth.Session != nil && strings.TrimSpace(c.Auth.Session.CookieName) != "" {
		return true
	}
	return false
}
