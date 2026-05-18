package runtimeenv

import (
	"sort"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/execenv"
	runtimeprobe "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
	"gopkg.in/yaml.v3"
)

type ChildEnvProfile = execenv.ChildEnvProfile

const (
	ModuleExecProfile    ChildEnvProfile = execenv.ModuleExecProfile
	InstallerExecProfile ChildEnvProfile = execenv.InstallerExecProfile
)

type ManagedSkillSpec struct {
	Enabled     *bool
	InjectedEnv map[string]string
	ConfigTruth map[string]skill.ConfigStatus
}

type SecretResolver = execenv.SecretResolver

func DefaultSecretResolver() SecretResolver {
	return execenv.DefaultSecretResolver()
}

func BuildRuntimeFacts(workDir string, cfg config.Config) skill.RuntimeContext {
	ctx := runtimeprobe.DetectContext(workDir)
	ctx.ConfigTruth = FlattenConfigTruth(configAsMap(cfg), "runtime_config")
	specs := BuildManagedSkillSpecs(cfg)
	if len(specs) == 0 {
		return ctx
	}

	resolver := DefaultSecretResolver()
	ctx.Managed = make(map[string]skill.ManagedEntry, len(specs))
	for key, spec := range specs {
		entry := skill.ManagedEntry{
			Enabled:     spec.Enabled,
			InjectedEnv: make(map[string]skill.SecretStatus),
			ConfigTruth: cloneConfigTruth(spec.ConfigTruth),
		}
		for envKey, raw := range spec.InjectedEnv {
			presence := resolver.Presence(raw)
			status := skill.SecretStatus{
				Resolved: presence.Resolved,
				Source:   presence.Source,
			}
			if status.Source != "" {
				status.Source = "managed"
			}
			entry.InjectedEnv[envKey] = status
		}
		if len(entry.InjectedEnv) == 0 {
			entry.InjectedEnv = nil
		}
		if len(entry.ConfigTruth) == 0 {
			entry.ConfigTruth = nil
		}
		ctx.Managed[key] = entry
	}
	return ctx
}

func BuildManagedSkillSpecs(cfg config.Config) map[string]ManagedSkillSpec {
	entries := make(map[string]ManagedSkillSpec)

	for skillKey, values := range cfg.Skills.Config {
		trimmed := strings.TrimSpace(skillKey)
		if trimmed == "" {
			continue
		}
		entry := entries[trimmed]
		entry.ConfigTruth = mergeConfigTruth(entry.ConfigTruth, FlattenConfigTruth(values, "runtime_config"))
		entries[trimmed] = entry
	}

	openAIKey := openAIAPIKey(cfg)
	addInjectedEnv(entries, "ai.openai-image-gen", "OPENAI_API_KEY", openAIKey)
	addInjectedEnv(entries, "ai.openai-whisper-api", "OPENAI_API_KEY", openAIKey)

	if botToken := strings.TrimSpace(cfg.Channels.Slack.BotToken); botToken != "" {
		addInjectedEnv(entries, "comm.slack", "SLACK_TOKEN", botToken)
		if appToken := strings.TrimSpace(cfg.Channels.Slack.AppToken); appToken != "" {
			addInjectedEnv(entries, "comm.slack", "SLACK_APP_TOKEN", appToken)
		}
	}

	feishuAppID := strings.TrimSpace(cfg.Channels.Feishu.AppID)
	feishuSecret := strings.TrimSpace(cfg.Channels.Feishu.AppSecret)
	feishuDomain := strings.TrimSpace(cfg.Channels.Feishu.Domain)
	if feishuAppID == "" && len(cfg.Channels.Feishu.Accounts) > 0 {
		accountID := strings.TrimSpace(cfg.Channels.Feishu.DefaultAccount)
		if accountID == "" {
			accountKeys := make([]string, 0, len(cfg.Channels.Feishu.Accounts))
			for key := range cfg.Channels.Feishu.Accounts {
				accountKeys = append(accountKeys, key)
			}
			sort.Strings(accountKeys)
			if len(accountKeys) > 0 {
				accountID = accountKeys[0]
			}
		}
		if account, ok := cfg.Channels.Feishu.Accounts[accountID]; ok {
			feishuAppID = strings.TrimSpace(account.AppID)
			feishuSecret = strings.TrimSpace(account.AppSecret)
			if domain := strings.TrimSpace(account.Domain); domain != "" {
				feishuDomain = domain
			}
		}
	}
	if feishuAppID != "" {
		for _, skillKey := range []string{"enterprise.feishu-doc", "enterprise.feishu-suite", "enterprise.feishu-wiki"} {
			addInjectedEnv(entries, skillKey, "FEISHU_APP_ID", feishuAppID)
			addInjectedEnv(entries, skillKey, "FEISHU_APP_SECRET", feishuSecret)
			addInjectedEnv(entries, skillKey, "FEISHU_DOMAIN", feishuDomain)
		}
	}

	addEmailManagedEnv(entries, cfg.Tools.Services.Email)

	if password := strings.TrimSpace(cfg.Channels.BlueBubbles.Password); password != "" {
		addInjectedEnv(entries, "comm.bluebubbles", "BLUEBUBBLES_PASSWORD", password)
		addInjectedEnv(entries, "comm.bluebubbles", "BLUEBUBBLES_BASE_URL", strings.TrimSpace(cfg.Channels.BlueBubbles.BaseURL))
	}

	if len(entries) == 0 {
		return nil
	}
	return entries
}

func ResolveSkillInjectedEnv(cfg config.Config, pkg *skill.SkillPackage) (map[string]string, error) {
	if pkg == nil {
		return nil, nil
	}
	specs := BuildManagedSkillSpecs(cfg)
	spec, ok := specs[pkg.ConfigKey()]
	if !ok || len(spec.InjectedEnv) == 0 {
		return nil, nil
	}
	return DefaultSecretResolver().ResolveMap(spec.InjectedEnv)
}

func configAsMap(cfg config.Config) map[string]any {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func FlattenConfigTruth(root map[string]any, source string) map[string]skill.ConfigStatus {
	if len(root) == 0 {
		return nil
	}
	out := make(map[string]skill.ConfigStatus)
	keys := make([]string, 0, len(root))
	for key := range root {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		flattenConfigTruth(out, key, root[key], source)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func BuildChildEnv(profile ChildEnvProfile, requiredSystemEnv []string, explicitEnv map[string]string, injectedEnv map[string]string, overlay map[string]string) []string {
	return execenv.BuildChildEnv(profile, requiredSystemEnv, explicitEnv, injectedEnv, overlay)
}

func BaselineKeys(profile ChildEnvProfile) []string {
	return execenv.BaselineKeys(profile)
}

func ParseEnvPairs(pairs []string) map[string]string {
	return execenv.ParseEnvPairs(pairs)
}

func LookPathWithEnv(name string, overlay map[string]string) (string, error) {
	return execenv.LookPathWithEnv(name, overlay)
}

func flattenConfigTruth(out map[string]skill.ConfigStatus, path string, value any, source string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	out[trimmed] = skill.ConfigStatus{
		Present: true,
		Truthy:  isTruthy(value),
		Source:  strings.TrimSpace(source),
	}
	node, ok := value.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(node))
	for key := range node {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		flattenConfigTruth(out, trimmed+"."+key, node[key], source)
	}
}

func isTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func mergeConfigTruth(base, extra map[string]skill.ConfigStatus) map[string]skill.ConfigStatus {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]skill.ConfigStatus, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func cloneConfigTruth(in map[string]skill.ConfigStatus) map[string]skill.ConfigStatus {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]skill.ConfigStatus, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func addInjectedEnv(entries map[string]ManagedSkillSpec, skillKey, envKey, value string) {
	if strings.TrimSpace(skillKey) == "" || strings.TrimSpace(envKey) == "" || strings.TrimSpace(value) == "" {
		return
	}
	entry := entries[skillKey]
	if entry.InjectedEnv == nil {
		entry.InjectedEnv = make(map[string]string)
	}
	entry.InjectedEnv[envKey] = value
	entries[skillKey] = entry
}

func addEmailManagedEnv(entries map[string]ManagedSkillSpec, cfg config.EmailServiceConfig) {
	address := strings.TrimSpace(cfg.Username)
	if address == "" {
		address = strings.TrimSpace(cfg.From)
	}
	if address == "" && strings.TrimSpace(cfg.Password) == "" {
		return
	}
	addInjectedEnv(entries, "comm.email", "EMAIL_ADDRESS", address)
	addInjectedEnv(entries, "comm.email", "EMAIL_PASSWORD", strings.TrimSpace(cfg.Password))
	addInjectedEnv(entries, "comm.email", "EMAIL_SMTP_HOST", strings.TrimSpace(cfg.SMTPHost))
	addInjectedEnv(entries, "comm.email", "EMAIL_IMAP_HOST", strings.TrimSpace(cfg.IMAPHost))
	if cfg.SMTPPort > 0 {
		addInjectedEnv(entries, "comm.email", "EMAIL_SMTP_PORT", strconv.Itoa(cfg.SMTPPort))
	}
	if cfg.IMAPPort > 0 {
		addInjectedEnv(entries, "comm.email", "EMAIL_IMAP_PORT", strconv.Itoa(cfg.IMAPPort))
	}
}

func openAIAPIKey(cfg config.Config) string {
	if key := strings.TrimSpace(cfg.Models.OpenAICompat.APIKey); key != "" {
		return key
	}
	providerKeys := make([]string, 0, len(cfg.Models.Providers))
	for key := range cfg.Models.Providers {
		providerKeys = append(providerKeys, key)
	}
	sort.Strings(providerKeys)
	for _, name := range providerKeys {
		provider := cfg.Models.Providers[name]
		if !strings.EqualFold(strings.TrimSpace(name), "openai") && !strings.EqualFold(strings.TrimSpace(provider.API), "openai-completions") {
			continue
		}
		if key := strings.TrimSpace(provider.APIKey); key != "" {
			return key
		}
		for _, candidate := range provider.APIKeys {
			if trimmed := strings.TrimSpace(candidate); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
