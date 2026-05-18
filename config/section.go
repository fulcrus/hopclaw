package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	sectionOverlayLegacyPrefix = "section."
	sectionOverlayKeyPrefix    = "config.section."
)

// SectionOverlayKey returns the canonical overlay key for a top-level config section.
func SectionOverlayKey(section string) string {
	trimmed := strings.TrimSpace(section)
	if trimmed == "" {
		return ""
	}
	return sectionOverlayKeyPrefix + trimmed
}

// LegacySectionOverlayKey returns the legacy dynamic_settings key used before
// the domain registry was introduced.
func LegacySectionOverlayKey(section string) string {
	trimmed := strings.TrimSpace(section)
	if trimmed == "" {
		return ""
	}
	return sectionOverlayLegacyPrefix + trimmed
}

// ExtractSection returns a specific top-level section of the config by name.
func ExtractSection(cfg Config, section string) (any, error) {
	switch strings.TrimSpace(section) {
	case "server":
		return cfg.Server, nil
	case "auth":
		return cfg.Auth, nil
	case "authz":
		return cfg.AuthZ, nil
	case "store":
		return cfg.Store, nil
	case "agent":
		return cfg.Agent, nil
	case "runtime":
		return cfg.Runtime, nil
	case "skills":
		return cfg.Skills, nil
	case "models":
		return cfg.Models, nil
	case "tools":
		return cfg.Tools, nil
	case "hosts":
		return cfg.Hosts, nil
	case "channels":
		return cfg.Channels, nil
	case "plugins":
		return cfg.Plugins, nil
	case "cron":
		return cfg.Cron, nil
	case "heartbeat":
		return cfg.Heartbeat, nil
	case "wire":
		return cfg.Wire, nil
	case "wakeup":
		return cfg.Wakeup, nil
	case "allowlist":
		return cfg.Allowlist, nil
	case "sandbox":
		return cfg.Sandbox, nil
	case "security":
		return cfg.Security, nil
	case "discovery":
		return cfg.Discovery, nil
	case "logging":
		return cfg.Logging, nil
	case "exec_approval":
		return cfg.ExecApproval, nil
	case "channel_health":
		return cfg.ChannelHealth, nil
	case "isolation":
		return cfg.Isolation, nil
	case "tunnel":
		return cfg.Tunnel, nil
	case "canvas":
		return cfg.Canvas, nil
	case "locale":
		return cfg.Locale, nil
	default:
		return nil, fmt.Errorf("unknown config section %q", section)
	}
}

// ApplySection replaces a top-level section, applies defaults, and validates
// the resulting config.
func ApplySection(cfg Config, section string, sectionValue any) (Config, error) {
	rootBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("marshal current config: %w", err)
	}
	root := make(map[string]any)
	if err := yaml.Unmarshal(rootBytes, &root); err != nil {
		return Config{}, fmt.Errorf("decode current config: %w", err)
	}
	key := strings.TrimSpace(section)
	if key == "" {
		return Config{}, fmt.Errorf("section is required")
	}
	if sectionValue == nil {
		delete(root, key)
	} else {
		root[key] = sectionValue
	}
	nextBytes, err := yaml.Marshal(root)
	if err != nil {
		return Config{}, fmt.Errorf("encode updated config: %w", err)
	}
	var next Config
	if err := yaml.Unmarshal(nextBytes, &next); err != nil {
		return Config{}, fmt.Errorf("apply updated config: %w", err)
	}
	preserveSecretPlaceholders(&next, cfg)
	next.ApplyDefaults()
	if err := next.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate updated config: %w", err)
	}
	return next, nil
}
