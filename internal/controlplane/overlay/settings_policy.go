package overlay

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

type SettingApplyResult struct {
	Applied bool
	Legacy  bool
	Domain  string
}

type SettingPolicy struct {
	Key    string
	Domain string
	Apply  func(cfg *config.Config, raw json.RawMessage) error
}

type SettingPolicyRegistry struct {
	policies map[string]SettingPolicy
}

func NewSettingPolicyRegistry(policies ...SettingPolicy) *SettingPolicyRegistry {
	registry := &SettingPolicyRegistry{
		policies: make(map[string]SettingPolicy, len(policies)),
	}
	for _, policy := range policies {
		registry.Register(policy)
	}
	return registry
}

func DefaultSettingPolicyRegistry() *SettingPolicyRegistry {
	registry := NewSettingPolicyRegistry()
	for _, section := range []string{
		"server", "auth", "authz", "store", "agent", "runtime", "skills", "models", "tools", "hosts",
		"channels", "plugins", "cron", "heartbeat", "wire", "wakeup", "allowlist", "sandbox",
		"security", "discovery", "logging", "exec_approval", "channel_health", "isolation",
		"tunnel", "canvas", "locale",
	} {
		registry.Register(SectionSettingPolicy(section))
	}
	return registry
}

func SectionSettingPolicy(section string) SettingPolicy {
	trimmed := strings.TrimSpace(section)
	key := config.SectionOverlayKey(trimmed)
	return SettingPolicy{
		Key:    key,
		Domain: "config_section",
		Apply: func(cfg *config.Config, raw json.RawMessage) error {
			if cfg == nil {
				return nil
			}
			var value any
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("decode setting %q: %w", key, err)
				}
			}
			next, err := config.ApplySection(*cfg, trimmed, value)
			if err != nil {
				return err
			}
			*cfg = next
			return nil
		},
	}
}

func (r *SettingPolicyRegistry) Register(policy SettingPolicy) {
	if r == nil {
		return
	}
	key := strings.TrimSpace(policy.Key)
	if key == "" || policy.Apply == nil {
		return
	}
	policy.Key = key
	policy.Domain = strings.TrimSpace(policy.Domain)
	r.policies[key] = policy
}

func (r *SettingPolicyRegistry) Lookup(key string) (SettingPolicy, bool) {
	if r == nil {
		return SettingPolicy{}, false
	}
	policy, ok := r.policies[strings.TrimSpace(key)]
	return policy, ok
}

func (r *SettingPolicyRegistry) ValidateKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return fmt.Errorf("setting key is required")
	}
	if _, ok := r.Lookup(trimmed); ok {
		return nil
	}
	return fmt.Errorf("setting key %q is not registered", trimmed)
}

func (r *SettingPolicyRegistry) OverlayKeyForSection(section string) (string, error) {
	trimmed := strings.TrimSpace(section)
	if trimmed == "" {
		return "", fmt.Errorf("section is required")
	}
	key := config.SectionOverlayKey(trimmed)
	if err := r.ValidateKey(key); err != nil {
		return "", err
	}
	return key, nil
}
