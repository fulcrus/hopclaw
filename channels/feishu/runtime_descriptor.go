package feishu

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/channels/registration"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Feishu
		if !runtimeDescriptorFeishuActive(cfg) {
			return nil
		}
		return []registration.Descriptor{{
			Name:          "feishu",
			Order:         100,
			RuntimeConfig: cfg,
			Build: func(context.Context) ([]registration.Installation, error) {
				adapter := New(Config{
					Enabled:           cfg.Enabled,
					DefaultAccount:    cfg.DefaultAccount,
					AppID:             cfg.AppID,
					AppSecret:         cfg.AppSecret,
					EncryptKey:        cfg.EncryptKey,
					VerificationToken: cfg.VerificationToken,
					Domain:            cfg.Domain,
					ConnectionMode:    cfg.ConnectionMode,
					DMPolicy:          cfg.DMPolicy,
					AllowFrom:         append([]string(nil), cfg.AllowFrom...),
					GroupPolicy:       cfg.GroupPolicy,
					GroupAllowFrom:    append([]string(nil), cfg.GroupAllowFrom...),
					RequireMention:    cfg.RequireMention,
					GroupSessionScope: cfg.GroupSessionScope,
					ReplyInThread:     cfg.ReplyInThread,
					DedupeTTL:         cfg.DedupeTTL,
					DedupeDir:         cfg.DedupeDir,
					Accounts:          runtimeDescriptorFeishuAccounts(cfg.Accounts),
				})
				if err := deps.ChannelManager.Register("feishu", adapter); err != nil {
					return nil, err
				}

				dedupeDir := strings.TrimSpace(cfg.DedupeDir)
				if dedupeDir == "" {
					dedupeDir = filepath.Join(deps.StorePath, "channels", "feishu-dedup.json")
				}
				bridge := NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithMessageDeduper(NewMessageDeduper(dedupeDir, cfg.DedupeTTL)).
					WithPairing(deps.PairingManager)
				return []registration.Installation{{
					Name:    "feishu",
					Adapter: adapter,
					Bridge:  bridge,
				}}, nil
			},
		}}
	})
}

func runtimeDescriptorFeishuActive(cfg config.FeishuChannelConfig) bool {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return false
	}
	if strings.TrimSpace(cfg.AppID) != "" && strings.TrimSpace(cfg.AppSecret) != "" {
		return true
	}
	for _, account := range cfg.Accounts {
		if account.Enabled != nil && !*account.Enabled {
			continue
		}
		if strings.TrimSpace(normalize.FirstNonEmpty(account.AppID, cfg.AppID)) != "" && strings.TrimSpace(normalize.FirstNonEmpty(account.AppSecret, cfg.AppSecret)) != "" {
			return true
		}
	}
	return false
}

func runtimeDescriptorFeishuAccounts(accounts map[string]config.FeishuAccountConfig) map[string]AccountConfig {
	if len(accounts) == 0 {
		return nil
	}
	out := make(map[string]AccountConfig, len(accounts))
	for key, account := range accounts {
		out[key] = AccountConfig{
			Enabled:           account.Enabled,
			Name:              account.Name,
			AppID:             account.AppID,
			AppSecret:         account.AppSecret,
			EncryptKey:        account.EncryptKey,
			VerificationToken: account.VerificationToken,
			Domain:            account.Domain,
			ConnectionMode:    account.ConnectionMode,
			DMPolicy:          account.DMPolicy,
			AllowFrom:         append([]string(nil), account.AllowFrom...),
			GroupPolicy:       account.GroupPolicy,
			GroupAllowFrom:    append([]string(nil), account.GroupAllowFrom...),
			RequireMention:    account.RequireMention,
			GroupSessionScope: account.GroupSessionScope,
			ReplyInThread:     account.ReplyInThread,
		}
	}
	return out
}
