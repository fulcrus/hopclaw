package controlplane

import (
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

const (
	UserSurfaceModePersonalLocal = "personal_local"
	UserSurfaceModeManaged       = "managed"

	UserSurfaceStartupQuietWhenHealthy = "quiet_when_healthy"
	UserSurfaceStartupActionableOnly   = "actionable_only"
)

type UserSurfaceSummary struct {
	Mode               string                     `json:"mode,omitempty"`
	StartupDiagnostics string                     `json:"startup_diagnostics,omitempty"`
	Approval           UserSurfaceApprovalSummary `json:"approval"`
}

type UserSurfaceApprovalSummary struct {
	LocalWrite        string `json:"local_write,omitempty"`
	ExternalWrite     string `json:"external_write,omitempty"`
	Destructive       string `json:"destructive,omitempty"`
	CommunityTools    string `json:"community_tools,omitempty"`
	DefaultGrantScope string `json:"default_grant_scope,omitempty"`
	MaxGrantScope     string `json:"max_grant_scope,omitempty"`
}

func BuildUserSurfaceSummary(snapshot *EffectiveConfigSnapshot, authEnabled bool, approvalProviderCount, governanceAdapterCount, auditSinkCount int) UserSurfaceSummary {
	mode := UserSurfaceModeManaged
	if qualifiesPersonalLocalSurface(snapshot, authEnabled, approvalProviderCount, governanceAdapterCount, auditSinkCount) {
		mode = UserSurfaceModePersonalLocal
	}
	startup := UserSurfaceStartupActionableOnly
	if mode == UserSurfaceModePersonalLocal {
		startup = UserSurfaceStartupQuietWhenHealthy
	}
	return UserSurfaceSummary{
		Mode:               mode,
		StartupDiagnostics: startup,
		Approval:           buildUserSurfaceApprovalSummary(snapshot),
	}
}

func qualifiesPersonalLocalSurface(snapshot *EffectiveConfigSnapshot, authEnabled bool, approvalProviderCount, governanceAdapterCount, auditSinkCount int) bool {
	if authEnabled || approvalProviderCount > 0 || governanceAdapterCount > 0 || auditSinkCount > 0 {
		return false
	}
	profile := config.RuntimeProfileTrustedDesktop
	if snapshot != nil && strings.TrimSpace(snapshot.RuntimeProfile) != "" {
		profile = strings.TrimSpace(snapshot.RuntimeProfile)
	}
	switch profile {
	case config.RuntimeProfileDesktop, config.RuntimeProfileTrustedDesktop:
		return true
	default:
		return false
	}
}

func buildUserSurfaceApprovalSummary(snapshot *EffectiveConfigSnapshot) UserSurfaceApprovalSummary {
	if snapshot == nil {
		return UserSurfaceApprovalSummary{}
	}
	approval := snapshot.Approval
	localWrite := "auto_allow"
	if approval.RequireApprovalForWrite && !approval.AllowLocalWriteWithoutApproval {
		localWrite = "confirm"
	}
	externalWrite := "auto_allow"
	if approval.RequireApprovalForWrite {
		externalWrite = "confirm"
	}
	destructive := "auto_allow"
	switch {
	case approval.DenyDestructive:
		destructive = "deny"
	case approval.RequireApprovalForWrite:
		destructive = "confirm"
	}
	communityTools := "auto_allow"
	if approval.RequireApprovalCommunity {
		communityTools = "confirm"
	}
	return UserSurfaceApprovalSummary{
		LocalWrite:        localWrite,
		ExternalWrite:     externalWrite,
		Destructive:       destructive,
		CommunityTools:    communityTools,
		DefaultGrantScope: strings.TrimSpace(approval.DefaultGrantScope),
		MaxGrantScope:     strings.TrimSpace(approval.MaxGrantScope),
	}
}
