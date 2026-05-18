package controlplane

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

type Layer struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Source string `json:"source,omitempty"`
}

type ApprovalPolicy struct {
	ExecMode                       string   `json:"exec_mode,omitempty"`
	SkillInstallPolicy             string   `json:"skill_install_policy,omitempty"`
	DangerousToolCount             int      `json:"dangerous_tool_count,omitempty"`
	RequireApprovalForWrite        bool     `json:"require_approval_for_write,omitempty"`
	AllowLocalWriteWithoutApproval bool     `json:"allow_local_write_without_approval,omitempty"`
	RequireApprovalCommunity       bool     `json:"require_approval_community,omitempty"`
	DenyDestructive                bool     `json:"deny_destructive,omitempty"`
	DefaultGrantScope              string   `json:"default_grant_scope,omitempty"`
	MaxGrantScope                  string   `json:"max_grant_scope,omitempty"`
	HasPolicyOverlay               bool     `json:"has_policy_overlay,omitempty"`
	ExternalProviderNames          []string `json:"external_provider_names,omitempty"`
	CallbackProviderNames          []string `json:"callback_provider_names,omitempty"`
}

type CapabilitySurface struct {
	BuiltinsEnabled  bool `json:"builtins_enabled,omitempty"`
	LocalExecEnabled bool `json:"local_exec_enabled,omitempty"`
	SearchEnabled    bool `json:"search_enabled,omitempty"`
	EmailEnabled     bool `json:"email_enabled,omitempty"`
	CalendarEnabled  bool `json:"calendar_enabled,omitempty"`
	WatchEnabled     bool `json:"watch_enabled,omitempty"`
	CronEnabled      bool `json:"cron_enabled,omitempty"`
	BrowserEnabled   bool `json:"browser_enabled,omitempty"`
	DesktopEnabled   bool `json:"desktop_enabled,omitempty"`
}

type EffectiveConfigSnapshot struct {
	ID                     string            `json:"id"`
	Edition                string            `json:"edition,omitempty"`
	Locale                 string            `json:"locale,omitempty"`
	DefaultModel           string            `json:"default_model,omitempty"`
	RuntimeProfile         string            `json:"runtime_profile,omitempty"`
	PolicyProfileID        string            `json:"policy_profile_id,omitempty"`
	PolicyPackIDs          []string          `json:"policy_pack_ids,omitempty"`
	GovernanceAdapterNames []string          `json:"governance_adapter_names,omitempty"`
	StoreBackend           string            `json:"store_backend,omitempty"`
	AuthEnabled            bool              `json:"auth_enabled,omitempty"`
	Approval               ApprovalPolicy    `json:"approval"`
	Capabilities           CapabilitySurface `json:"capabilities"`
	Layers                 []Layer           `json:"layers,omitempty"`
	GeneratedAt            string            `json:"generated_at,omitempty"`
}

type BuildOptions struct {
	Edition                string
	PolicyProfileID        string
	PolicyPackIDs          []string
	GovernanceAdapterNames []string
	Approval               *ApprovalPolicy
	Layers                 []Layer
	GeneratedAt            time.Time
}

func Build(cfg config.Config, opts BuildOptions) *EffectiveConfigSnapshot {
	generatedAt := opts.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	layers := cloneLayers(opts.Layers)
	if len(layers) == 0 {
		layers = []Layer{{
			Name:   "base",
			Kind:   "static",
			Source: "runtime_config",
		}}
	}
	approvalPolicy := ApprovalPolicy{
		ExecMode:           strings.TrimSpace(cfg.Tools.Capabilities.Exec.Mode),
		SkillInstallPolicy: strings.TrimSpace(cfg.Skills.InstallPolicy),
		DangerousToolCount: len(cfg.Security.DangerousTools),
	}
	if opts.Approval != nil {
		approvalPolicy = *opts.Approval
	}
	snapshot := &EffectiveConfigSnapshot{
		Edition:                strings.TrimSpace(opts.Edition),
		Locale:                 strings.TrimSpace(cfg.Locale),
		DefaultModel:           strings.TrimSpace(cfg.Agent.DefaultModel),
		RuntimeProfile:         strings.TrimSpace(cfg.Runtime.Profile),
		PolicyProfileID:        strings.TrimSpace(opts.PolicyProfileID),
		PolicyPackIDs:          cloneStrings(opts.PolicyPackIDs),
		GovernanceAdapterNames: cloneStrings(opts.GovernanceAdapterNames),
		StoreBackend:           strings.TrimSpace(cfg.Store.Backend),
		AuthEnabled:            cfg.HasAuth(),
		Approval:               approvalPolicy,
		Capabilities: CapabilitySurface{
			BuiltinsEnabled:  boolValue(cfg.Tools.Builtins.Enabled, true),
			LocalExecEnabled: boolValue(cfg.Tools.LocalExec.Enabled, true),
			SearchEnabled:    strings.TrimSpace(cfg.Tools.Services.Search.Provider) != "" || boolValue(cfg.Tools.Capabilities.Layer2.Search, false),
			EmailEnabled:     strings.TrimSpace(cfg.Tools.Services.Email.SMTPHost) != "" || strings.TrimSpace(cfg.Tools.Services.Email.IMAPHost) != "" || boolValue(cfg.Tools.Capabilities.Layer2.Email, false),
			CalendarEnabled:  strings.TrimSpace(cfg.Tools.Services.Calendar.CalDAVURL) != "" || boolValue(cfg.Tools.Capabilities.Layer2.Calendar, false),
			WatchEnabled:     boolValue(cfg.Watch.Enabled, false),
			CronEnabled:      boolValue(cfg.Cron.Enabled, false),
			BrowserEnabled:   boolValue(cfg.Hosts.Browser.Enabled, false),
			DesktopEnabled:   boolValue(cfg.Hosts.Desktop.Enabled, false),
		},
		Layers:      layers,
		GeneratedAt: generatedAt.Format(time.RFC3339),
	}
	snapshot.ID = buildSnapshotID(snapshot)
	return snapshot
}

func (s *EffectiveConfigSnapshot) Clone() *EffectiveConfigSnapshot {
	if s == nil {
		return nil
	}
	out := *s
	out.Layers = cloneLayers(s.Layers)
	out.PolicyPackIDs = cloneStrings(s.PolicyPackIDs)
	out.GovernanceAdapterNames = cloneStrings(s.GovernanceAdapterNames)
	out.Approval.ExternalProviderNames = cloneStrings(s.Approval.ExternalProviderNames)
	out.Approval.CallbackProviderNames = cloneStrings(s.Approval.CallbackProviderNames)
	return &out
}

func buildSnapshotID(snapshot *EffectiveConfigSnapshot) string {
	if snapshot == nil {
		return ""
	}
	payload := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%t|%s|%s|%s|%s|%d|%t|%t|%t|%t|%s|%s|%t",
		snapshot.Edition,
		snapshot.DefaultModel,
		snapshot.RuntimeProfile,
		snapshot.PolicyProfileID,
		strings.Join(snapshot.PolicyPackIDs, ","),
		strings.Join(snapshot.GovernanceAdapterNames, ","),
		snapshot.AuthEnabled,
		snapshot.StoreBackend,
		snapshot.GeneratedAt,
		snapshot.Approval.ExecMode,
		snapshot.Approval.SkillInstallPolicy,
		snapshot.Approval.DangerousToolCount,
		snapshot.Approval.RequireApprovalForWrite,
		snapshot.Approval.AllowLocalWriteWithoutApproval,
		snapshot.Approval.RequireApprovalCommunity,
		snapshot.Approval.DenyDestructive,
		snapshot.Approval.DefaultGrantScope,
		snapshot.Approval.MaxGrantScope,
		snapshot.Approval.HasPolicyOverlay,
	)
	payload += "|" + strings.Join(snapshot.Approval.ExternalProviderNames, ",")
	payload += "|" + strings.Join(snapshot.Approval.CallbackProviderNames, ",")
	sum := sha1.Sum([]byte(payload))
	return "ecs-" + hex.EncodeToString(sum[:6])
}

func cloneLayers(in []Layer) []Layer {
	if len(in) == 0 {
		return nil
	}
	out := make([]Layer, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func boolValue(v *bool, defaultValue bool) bool {
	if v == nil {
		return defaultValue
	}
	return *v
}
