package runtimeenv

import (
	"testing"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/skill"
)

func TestBuildRuntimeFactsIncludesConfigTruthAndManagedPresence(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.Channels.Slack.BotToken = "xoxb-test"
	cfg.Skills.Config = map[string]map[string]any{
		"demo.skill": {
			"feature": map[string]any{
				"enabled": true,
			},
		},
	}

	ctx := BuildRuntimeFacts(t.TempDir(), cfg)

	if status, ok := ctx.ConfigTruth["channels.slack.bot_token"]; !ok || !status.Present || !status.Truthy {
		t.Fatalf("runtime config truth = %#v", ctx.ConfigTruth["channels.slack.bot_token"])
	}

	entry, ok := ctx.Managed["comm.slack"]
	if !ok {
		t.Fatalf("managed entries = %#v", ctx.Managed)
	}
	if status, ok := entry.InjectedEnv["SLACK_TOKEN"]; !ok || !status.Resolved || status.Source != "managed" {
		t.Fatalf("managed slack injection = %#v", entry.InjectedEnv)
	}

	skillEntry, ok := ctx.Managed["demo.skill"]
	if !ok {
		t.Fatalf("managed skill config truth = %#v", ctx.Managed)
	}
	if status, ok := skillEntry.ConfigTruth["feature.enabled"]; !ok || !status.Present || !status.Truthy {
		t.Fatalf("managed config truth = %#v", skillEntry.ConfigTruth["feature.enabled"])
	}
}

func TestResolveSkillInjectedEnvResolvesSecretReferences(t *testing.T) {
	t.Setenv("HOPCLAW_OPENAI_TOKEN", "sk-test")

	cfg := config.Config{}
	cfg.Models.OpenAICompat.APIKey = "env:HOPCLAW_OPENAI_TOKEN"

	values, err := ResolveSkillInjectedEnv(cfg, &skill.SkillPackage{
		Prompt: skill.PromptSkill{Name: "openai-image-gen"},
		OpenClaw: skill.OpenClawMetadata{
			SkillKey: "ai.openai-image-gen",
		},
	})
	if err != nil {
		t.Fatalf("ResolveSkillInjectedEnv() error = %v", err)
	}
	if values["OPENAI_API_KEY"] != "sk-test" {
		t.Fatalf("resolved env = %#v", values)
	}
}
