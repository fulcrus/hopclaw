package toolruntime

import (
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

type DestinationAccount struct {
	ID          string         `json:"id,omitempty"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Enabled     bool           `json:"enabled,omitempty"`
	Default     bool           `json:"default,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type DestinationCatalog struct {
	ChannelAccounts map[string][]DestinationAccount `json:"channel_accounts,omitempty"`
}

func (c DestinationCatalog) Clone() DestinationCatalog {
	if len(c.ChannelAccounts) == 0 {
		return DestinationCatalog{}
	}
	out := DestinationCatalog{ChannelAccounts: make(map[string][]DestinationAccount, len(c.ChannelAccounts))}
	for provider, accounts := range c.ChannelAccounts {
		copied := make([]DestinationAccount, 0, len(accounts))
		for _, account := range accounts {
			next := account
			if len(account.Metadata) > 0 {
				next.Metadata = make(map[string]any, len(account.Metadata))
				for key, value := range account.Metadata {
					next.Metadata[key] = value
				}
			}
			copied = append(copied, next)
		}
		out.ChannelAccounts[provider] = copied
	}
	return out
}

func BuildDestinationCatalog(cfg config.Config) DestinationCatalog {
	catalog := DestinationCatalog{
		ChannelAccounts: map[string][]DestinationAccount{},
	}
	if accounts := buildFeishuDestinationAccounts(cfg.Channels.Feishu); len(accounts) > 0 {
		catalog.ChannelAccounts["feishu"] = accounts
	}
	return catalog
}

func buildFeishuDestinationAccounts(cfg config.FeishuChannelConfig) []DestinationAccount {
	topLevelEnabled := cfg.Enabled == nil || *cfg.Enabled
	defaultID := strings.TrimSpace(cfg.DefaultAccount)
	hasTopLevelCredentials := strings.TrimSpace(cfg.AppID) != "" && strings.TrimSpace(cfg.AppSecret) != ""

	if len(cfg.Accounts) == 0 {
		if !topLevelEnabled || !hasTopLevelCredentials {
			return nil
		}
		if defaultID == "" {
			defaultID = "default"
		}
		return []DestinationAccount{{
			ID:      defaultID,
			Label:   "Default account",
			Enabled: true,
			Default: true,
			Metadata: map[string]any{
				"domain": strings.TrimSpace(cfg.Domain),
			},
		}}
	}

	keys := make([]string, 0, len(cfg.Accounts))
	for key := range cfg.Accounts {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)

	if defaultID == "" && len(keys) > 0 {
		defaultID = keys[0]
	}

	out := make([]DestinationAccount, 0, len(keys))
	for _, key := range keys {
		account := cfg.Accounts[key]
		enabled := topLevelEnabled && (account.Enabled == nil || *account.Enabled)
		appID := firstCatalogNonEmpty(account.AppID, cfg.AppID)
		appSecret := firstCatalogNonEmpty(account.AppSecret, cfg.AppSecret)
		if !enabled || strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
			continue
		}
		label := strings.TrimSpace(account.Name)
		if label == "" {
			label = key
		}
		out = append(out, DestinationAccount{
			ID:          key,
			Label:       label,
			Description: strings.TrimSpace(firstCatalogNonEmpty(account.Domain, cfg.Domain)),
			Enabled:     true,
			Default:     key == defaultID,
			Metadata: map[string]any{
				"domain": firstCatalogNonEmpty(account.Domain, cfg.Domain),
			},
		})
	}
	return out
}

func firstCatalogNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
