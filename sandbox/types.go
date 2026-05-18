package sandbox

import (
	"time"
)

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const (
	defaultImage       = "python:3.12-slim"
	defaultMemoryLimit = "256m"
	defaultCPULimit    = "1.0"
	defaultTimeout     = 30 // seconds
	defaultNetworkMode = "none"
	defaultWorkDir     = "/workspace"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config controls Docker sandbox behavior. Fields are tunable by operators
// via YAML config. Zero-value fields are replaced with defaults in NewRunner.
type Config struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	Image          string   `json:"image" yaml:"image"`                     // default "python:3.12-slim"
	MemoryLimit    string   `json:"memory_limit" yaml:"memory_limit"`       // default "256m"
	CPULimit       string   `json:"cpu_limit" yaml:"cpu_limit"`             // default "1.0"
	Timeout        int      `json:"timeout" yaml:"timeout"`                 // seconds, default 30
	NetworkMode    string   `json:"network_mode" yaml:"network_mode"`       // default "none"
	WorkDir        string   `json:"work_dir" yaml:"work_dir"`               // container workdir
	AllowedImages  []string `json:"allowed_images" yaml:"allowed_images"`   // whitelist
	SeccompProfile string   `json:"seccomp_profile" yaml:"seccomp_profile"` // path to seccomp JSON; empty = Docker default
}

// applyDefaults fills zero-value fields with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Image == "" {
		c.Image = defaultImage
	}
	if c.MemoryLimit == "" {
		c.MemoryLimit = defaultMemoryLimit
	}
	if c.CPULimit == "" {
		c.CPULimit = defaultCPULimit
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.NetworkMode == "" {
		c.NetworkMode = defaultNetworkMode
	}
	if c.WorkDir == "" {
		c.WorkDir = defaultWorkDir
	}
}

// ---------------------------------------------------------------------------
// ExecRequest
// ---------------------------------------------------------------------------

// ExecRequest describes a single sandboxed command execution.
type ExecRequest struct {
	Image   string            `json:"image,omitempty"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	Stdin   string            `json:"stdin,omitempty"`
	Timeout int               `json:"timeout,omitempty"` // override config timeout
}

// ---------------------------------------------------------------------------
// ExecResult
// ---------------------------------------------------------------------------

// ExecResult captures the output of a sandboxed command execution.
type ExecResult struct {
	ExitCode  int           `json:"exit_code"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	Duration  time.Duration `json:"duration"`
	TimedOut  bool          `json:"timed_out"`
	Truncated bool          `json:"truncated,omitempty"`
}
