package snapshot

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

func TestBuildSnapshot(t *testing.T) {
	t.Parallel()

	trueValue := true
	cfg := config.Config{
		Locale: "zh-CN",
		Store: config.StoreConfig{
			Backend: "sqlite",
		},
		Agent: config.AgentConfig{
			DefaultModel: "gpt-5",
		},
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileProduction,
		},
		Watch: config.WatchConfig{
			Enabled: &trueValue,
		},
		Cron: config.CronConfig{
			Enabled: &trueValue,
		},
		Hosts: config.HostsConfig{
			Browser: config.BrowserHostConfig{Enabled: &trueValue},
			Desktop: config.DesktopHostConfig{Enabled: &trueValue},
		},
		Skills: config.SkillsConfig{
			InstallPolicy: config.SkillInstallPolicyAsk,
		},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: &trueValue},
			LocalExec: config.LocalExecConfig{Enabled: &trueValue},
			Capabilities: config.CapabilitiesConfig{
				Layer2: config.Layer2Config{
					Search:   &trueValue,
					Email:    &trueValue,
					Calendar: &trueValue,
				},
			},
		},
	}

	snapshot := Build(cfg, BuildOptions{
		Edition:                "enterprise",
		PolicyProfileID:        "business-production",
		PolicyPackIDs:          []string{"base-core", "business-default", "production-default"},
		GovernanceAdapterNames: []string{"audit-hub"},
		Approval: &ApprovalPolicy{
			ExecMode:                       "allowlist",
			SkillInstallPolicy:             config.SkillInstallPolicyAsk,
			DangerousToolCount:             2,
			RequireApprovalForWrite:        true,
			AllowLocalWriteWithoutApproval: true,
			RequireApprovalCommunity:       true,
			DenyDestructive:                true,
			DefaultGrantScope:              "once",
			MaxGrantScope:                  "session",
			HasPolicyOverlay:               true,
			ExternalProviderNames:          []string{"jira"},
			CallbackProviderNames:          []string{"jira"},
		},
		GeneratedAt: time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC),
		Layers: []Layer{{
			Name:   "test",
			Kind:   "base",
			Source: "unit",
		}},
	})
	if snapshot == nil {
		t.Fatal("Build() returned nil")
	}
	if snapshot.DefaultModel != "gpt-5" {
		t.Fatalf("DefaultModel = %q", snapshot.DefaultModel)
	}
	if !snapshot.Capabilities.WatchEnabled || !snapshot.Capabilities.CronEnabled {
		t.Fatalf("Capabilities = %#v", snapshot.Capabilities)
	}
	if snapshot.GeneratedAt != "2026-03-19T09:00:00Z" {
		t.Fatalf("GeneratedAt = %q", snapshot.GeneratedAt)
	}
	if snapshot.PolicyProfileID != "business-production" {
		t.Fatalf("PolicyProfileID = %q", snapshot.PolicyProfileID)
	}
	if !reflect.DeepEqual(snapshot.PolicyPackIDs, []string{"base-core", "business-default", "production-default"}) {
		t.Fatalf("PolicyPackIDs = %#v", snapshot.PolicyPackIDs)
	}
	if !reflect.DeepEqual(snapshot.GovernanceAdapterNames, []string{"audit-hub"}) {
		t.Fatalf("GovernanceAdapterNames = %#v", snapshot.GovernanceAdapterNames)
	}
	if !snapshot.Approval.RequireApprovalForWrite || !snapshot.Approval.AllowLocalWriteWithoutApproval || !snapshot.Approval.RequireApprovalCommunity || !snapshot.Approval.DenyDestructive {
		t.Fatalf("Approval = %#v", snapshot.Approval)
	}
	if snapshot.Approval.DefaultGrantScope != "once" || snapshot.Approval.MaxGrantScope != "session" {
		t.Fatalf("Approval grant scope = %#v", snapshot.Approval)
	}
	if !snapshot.Approval.HasPolicyOverlay {
		t.Fatalf("Approval.HasPolicyOverlay = %#v", snapshot.Approval)
	}
	if !reflect.DeepEqual(snapshot.Approval.ExternalProviderNames, []string{"jira"}) {
		t.Fatalf("Approval.ExternalProviderNames = %#v", snapshot.Approval.ExternalProviderNames)
	}
	if snapshot.ID == "" {
		t.Fatal("expected non-empty snapshot ID")
	}
	clone := snapshot.Clone()
	clone.Layers[0].Name = "mutated"
	clone.PolicyPackIDs[0] = "mutated-pack"
	clone.GovernanceAdapterNames[0] = "mutated-gov"
	if snapshot.Layers[0].Name != "test" {
		t.Fatal("Clone() should deep copy layers")
	}
	if snapshot.PolicyPackIDs[0] != "base-core" {
		t.Fatal("Clone() should deep copy policy pack IDs")
	}
	if snapshot.GovernanceAdapterNames[0] != "audit-hub" {
		t.Fatal("Clone() should deep copy governance adapter names")
	}
	clone.Approval.ExternalProviderNames[0] = "mutated-provider"
	if snapshot.Approval.ExternalProviderNames[0] != "jira" {
		t.Fatal("Clone() should deep copy approval provider names")
	}
}
