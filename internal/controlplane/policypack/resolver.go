package policypack

import (
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/policy"
)

const (
	KindBase           = "base"
	KindRuntimeProfile = "runtime_profile"
	SourceBuiltin      = "builtin"
)

const (
	PackBaseCore              = "base-core"
	PackDesktopDefault        = "desktop-default"
	PackTrustedDesktopDefault = "trusted-desktop-default"
	PackProductionDefault     = "production-default"
)

type ResolveInput struct {
	RuntimeProfile     string
	SkillInstallPolicy string
}

type Pack struct {
	ID     string
	Name   string
	Kind   string
	Source string
}

type Resolved struct {
	ProfileID      string
	RuntimeProfile string
	Config         policy.Config
	Packs          []Pack
}

func (r Resolved) PackIDs() []string {
	if len(r.Packs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(r.Packs))
	for _, pack := range r.Packs {
		id := strings.TrimSpace(pack.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func Resolve(input ResolveInput) Resolved {
	runtimeProfile := normalizeRuntimeProfile(input.RuntimeProfile)
	packs := []builtinPack{
		baseCorePack(),
		runtimeProfilePack(runtimeProfile),
	}
	cfg := policy.Config{}
	resolvedPacks := make([]Pack, 0, len(packs))
	for _, pack := range packs {
		cfg = pack.overlay.apply(cfg)
		resolvedPacks = append(resolvedPacks, pack.Pack)
	}
	cfg.SkillInstallPolicy = strings.TrimSpace(input.SkillInstallPolicy)
	cfg = cfg.Normalized()
	return Resolved{
		ProfileID:      profileID(runtimeProfile),
		RuntimeProfile: runtimeProfile,
		Config:         cfg,
		Packs:          resolvedPacks,
	}
}

type builtinPack struct {
	Pack
	overlay configOverlay
}

type configOverlay struct {
	AllowUnknownTools              *bool
	RequireApprovalForWrite        *bool
	AllowLocalWriteWithoutApproval *bool
	RequireApprovalCommunity       *bool
	SkipManifestApproval           *bool
	DenyDestructive                *bool
	DefaultApprovalScope           *approval.Scope
	MaxApprovalScope               *approval.Scope
	SkillInstallDefaultScope       *approval.Scope
	SkillInstallMaxScope           *approval.Scope
}

func (o configOverlay) apply(cfg policy.Config) policy.Config {
	if o.AllowUnknownTools != nil {
		cfg.AllowUnknownTools = *o.AllowUnknownTools
	}
	if o.RequireApprovalForWrite != nil {
		cfg.RequireApprovalForWrite = *o.RequireApprovalForWrite
	}
	if o.AllowLocalWriteWithoutApproval != nil {
		cfg.AllowLocalWriteWithoutApproval = *o.AllowLocalWriteWithoutApproval
	}
	if o.RequireApprovalCommunity != nil {
		cfg.RequireApprovalCommunity = *o.RequireApprovalCommunity
	}
	if o.SkipManifestApproval != nil {
		cfg.SkipManifestApproval = *o.SkipManifestApproval
	}
	if o.DenyDestructive != nil {
		cfg.DenyDestructive = *o.DenyDestructive
	}
	if o.DefaultApprovalScope != nil {
		cfg.DefaultApprovalScope = *o.DefaultApprovalScope
	}
	if o.MaxApprovalScope != nil {
		cfg.MaxApprovalScope = *o.MaxApprovalScope
	}
	if o.SkillInstallDefaultScope != nil {
		cfg.SkillInstallDefaultScope = *o.SkillInstallDefaultScope
	}
	if o.SkillInstallMaxScope != nil {
		cfg.SkillInstallMaxScope = *o.SkillInstallMaxScope
	}
	return cfg
}

func baseCorePack() builtinPack {
	return builtinPack{
		Pack: Pack{
			ID:     PackBaseCore,
			Name:   "Base Core",
			Kind:   KindBase,
			Source: SourceBuiltin,
		},
		overlay: configOverlay{
			AllowUnknownTools:        boolPtr(true),
			RequireApprovalForWrite:  boolPtr(true),
			RequireApprovalCommunity: boolPtr(true),
			DenyDestructive:          boolPtr(false),
			DefaultApprovalScope:     scopePtr(approval.ScopeOnce),
			MaxApprovalScope:         scopePtr(approval.ScopeSession),
			SkillInstallDefaultScope: scopePtr(approval.ScopeOnce),
			SkillInstallMaxScope:     scopePtr(approval.ScopeOnce),
		},
	}
}

func runtimeProfilePack(profile string) builtinPack {
	switch normalizeRuntimeProfile(profile) {
	case config.RuntimeProfileTrustedDesktop:
		return builtinPack{
			Pack: Pack{
				ID:     PackTrustedDesktopDefault,
				Name:   "Trusted Desktop",
				Kind:   KindRuntimeProfile,
				Source: SourceBuiltin,
			},
			overlay: configOverlay{
				RequireApprovalForWrite:        boolPtr(true),
				AllowLocalWriteWithoutApproval: boolPtr(true),
				RequireApprovalCommunity:       boolPtr(false),
				SkipManifestApproval:           boolPtr(true),
				DenyDestructive:                boolPtr(false),
			},
		}
	case config.RuntimeProfileProduction:
		return builtinPack{
			Pack: Pack{
				ID:     PackProductionDefault,
				Name:   "Production",
				Kind:   KindRuntimeProfile,
				Source: SourceBuiltin,
			},
			overlay: configOverlay{
				DenyDestructive: boolPtr(true),
			},
		}
	default:
		return builtinPack{
			Pack: Pack{
				ID:     PackDesktopDefault,
				Name:   "Desktop",
				Kind:   KindRuntimeProfile,
				Source: SourceBuiltin,
			},
		}
	}
}

func profileID(runtimeProfile string) string {
	return "default-" + normalizeRuntimeProfile(runtimeProfile)
}

func normalizeRuntimeProfile(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return config.RuntimeProfileTrustedDesktop
	case config.RuntimeProfileDesktop:
		return config.RuntimeProfileDesktop
	case config.RuntimeProfileTrustedDesktop:
		return config.RuntimeProfileTrustedDesktop
	case config.RuntimeProfileProduction:
		return config.RuntimeProfileProduction
	default:
		return config.RuntimeProfileDesktop
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func scopePtr(v approval.Scope) *approval.Scope {
	return &v
}
