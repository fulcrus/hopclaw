package controlplane

import (
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestBuildUserSurfaceSummaryPersonalLocal(t *testing.T) {
	t.Parallel()

	summary := BuildUserSurfaceSummary(&EffectiveConfigSnapshot{
		RuntimeProfile: config.RuntimeProfileTrustedDesktop,
		Approval: ApprovalPolicy{
			RequireApprovalForWrite:        true,
			AllowLocalWriteWithoutApproval: true,
			DefaultGrantScope:              "once",
			MaxGrantScope:                  "session",
		},
	}, false, 0, 0, 0)

	if summary.Mode != UserSurfaceModePersonalLocal {
		t.Fatalf("Mode = %q, want %q", summary.Mode, UserSurfaceModePersonalLocal)
	}
	if summary.StartupDiagnostics != UserSurfaceStartupQuietWhenHealthy {
		t.Fatalf("StartupDiagnostics = %q, want %q", summary.StartupDiagnostics, UserSurfaceStartupQuietWhenHealthy)
	}
	if summary.Approval.LocalWrite != "auto_allow" || summary.Approval.ExternalWrite != "confirm" || summary.Approval.DefaultGrantScope != "once" || summary.Approval.MaxGrantScope != "session" {
		t.Fatalf("Approval = %+v", summary.Approval)
	}
}

func TestBuildUserSurfaceSummaryManagedWhenGoverned(t *testing.T) {
	t.Parallel()

	summary := BuildUserSurfaceSummary(&EffectiveConfigSnapshot{
		RuntimeProfile: config.RuntimeProfileDesktop,
		Approval: ApprovalPolicy{
			RequireApprovalForWrite:  true,
			RequireApprovalCommunity: true,
			DenyDestructive:          true,
		},
	}, true, 1, 1, 1)

	if summary.Mode != UserSurfaceModeManaged {
		t.Fatalf("Mode = %q, want %q", summary.Mode, UserSurfaceModeManaged)
	}
	if summary.StartupDiagnostics != UserSurfaceStartupActionableOnly {
		t.Fatalf("StartupDiagnostics = %q, want %q", summary.StartupDiagnostics, UserSurfaceStartupActionableOnly)
	}
	if summary.Approval.LocalWrite != "confirm" || summary.Approval.Destructive != "deny" || summary.Approval.CommunityTools != "confirm" {
		t.Fatalf("Approval = %+v", summary.Approval)
	}
}
