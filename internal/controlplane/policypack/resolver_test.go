package policypack

import (
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/policy"
)

func TestResolveDesktopProfile(t *testing.T) {
	t.Parallel()

	got := Resolve(ResolveInput{
		RuntimeProfile:     config.RuntimeProfileDesktop,
		SkillInstallPolicy: config.SkillInstallPolicyAuto,
	})

	want := policy.Config{
		AllowUnknownTools:        true,
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
		DenyDestructive:          false,
		SkillInstallPolicy:       config.SkillInstallPolicyAuto,
		DefaultApprovalScope:     approval.ScopeOnce,
		MaxApprovalScope:         approval.ScopeSession,
		SkillInstallDefaultScope: approval.ScopeOnce,
		SkillInstallMaxScope:     approval.ScopeOnce,
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
	if got.ProfileID != "default-desktop" {
		t.Fatalf("ProfileID = %q", got.ProfileID)
	}
	if got.RuntimeProfile != config.RuntimeProfileDesktop {
		t.Fatalf("RuntimeProfile = %q", got.RuntimeProfile)
	}
	if !reflect.DeepEqual(got.PackIDs(), []string{PackBaseCore, PackDesktopDefault}) {
		t.Fatalf("PackIDs() = %#v", got.PackIDs())
	}
}

func TestResolveTrustedDesktopProfile(t *testing.T) {
	t.Parallel()

	got := Resolve(ResolveInput{
		RuntimeProfile: config.RuntimeProfileTrustedDesktop,
	})

	want := policy.Config{
		AllowUnknownTools:              true,
		RequireApprovalForWrite:        true,
		AllowLocalWriteWithoutApproval: true,
		RequireApprovalCommunity:       false,
		SkipManifestApproval:           true,
		DenyDestructive:                false,
		DefaultApprovalScope:           approval.ScopeOnce,
		MaxApprovalScope:               approval.ScopeSession,
		SkillInstallDefaultScope:       approval.ScopeOnce,
		SkillInstallMaxScope:           approval.ScopeOnce,
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
	if got.ProfileID != "default-trusted_desktop" {
		t.Fatalf("ProfileID = %q", got.ProfileID)
	}
	if !reflect.DeepEqual(got.PackIDs(), []string{PackBaseCore, PackTrustedDesktopDefault}) {
		t.Fatalf("PackIDs() = %#v", got.PackIDs())
	}
}

func TestResolveNormalizesUnknownInputs(t *testing.T) {
	t.Parallel()

	got := Resolve(ResolveInput{
		RuntimeProfile: "unknown",
	})

	if got.ProfileID != "default-desktop" {
		t.Fatalf("ProfileID = %q", got.ProfileID)
	}
	if got.RuntimeProfile != config.RuntimeProfileDesktop {
		t.Fatalf("RuntimeProfile = %q", got.RuntimeProfile)
	}
}

func TestResolveDefaultsToTrustedDesktopWhenProfileOmitted(t *testing.T) {
	t.Parallel()

	got := Resolve(ResolveInput{})

	want := policy.Config{
		AllowUnknownTools:              true,
		RequireApprovalForWrite:        true,
		AllowLocalWriteWithoutApproval: true,
		RequireApprovalCommunity:       false,
		SkipManifestApproval:           true,
		DenyDestructive:                false,
		DefaultApprovalScope:           approval.ScopeOnce,
		MaxApprovalScope:               approval.ScopeSession,
		SkillInstallDefaultScope:       approval.ScopeOnce,
		SkillInstallMaxScope:           approval.ScopeOnce,
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
	if got.ProfileID != "default-trusted_desktop" {
		t.Fatalf("ProfileID = %q", got.ProfileID)
	}
	if got.RuntimeProfile != config.RuntimeProfileTrustedDesktop {
		t.Fatalf("RuntimeProfile = %q", got.RuntimeProfile)
	}
	if !reflect.DeepEqual(got.PackIDs(), []string{PackBaseCore, PackTrustedDesktopDefault}) {
		t.Fatalf("PackIDs() = %#v", got.PackIDs())
	}
}
