package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/skill"
)

func TestEffectiveSystemPromptAppendsChannelRulesFromCapabilityMetadata(t *testing.T) {
	t.Parallel()

	base := "You are a helpful assistant."
	session := &Session{
		Key: "api:session-1",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive":     true,
				"threading":       true,
				"mobile":          true,
				"inline_delivery": true,
			},
		},
	}

	got := effectiveSystemPrompt(base, nil, session)
	if !strings.Contains(got, "<channel_rules>") {
		t.Fatalf("expected channel_rules in prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "mobile-first interactive chat channel") {
		t.Fatalf("expected mobile guidance in prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "current thread or topic") {
		t.Fatalf("expected threading guidance in prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "Do NOT save the answer to a file") {
		t.Fatalf("expected inline delivery guidance in prompt, got:\n%s", got)
	}
}

func TestEffectiveSystemPromptDoesNotGuessFromSessionKeyPrefix(t *testing.T) {
	t.Parallel()

	base := "You are a helpful assistant."
	channelSession := &Session{Key: "feishu:chat-123"}
	got := effectiveSystemPrompt(base, nil, channelSession)
	if strings.Contains(got, "<channel_rules>") {
		t.Fatalf("unexpected channel_rules when only session key prefix is present:\n%s", got)
	}
}

func TestEffectiveSystemPromptWithRuntimeContextAppendsProjectRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Rules\n- Prefer targeted tests.\n- Keep diffs small."), 0o644); err != nil {
		t.Fatalf("WriteFile(AGENTS.md) error = %v", err)
	}

	got := effectiveSystemPromptWithRuntimeContext("Base prompt", nil, &Session{Key: "api:session-1"}, skill.RuntimeContext{
		Workspace: skill.WorkspaceContext{Root: root},
	})
	if !strings.Contains(got, "<project_rules>") {
		t.Fatalf("prompt = %q, want project_rules block", got)
	}
	if !strings.Contains(got, "Prefer targeted tests.") || !strings.Contains(got, "Keep diffs small.") {
		t.Fatalf("prompt = %q, want project instructions", got)
	}
}

func TestEffectiveSystemPromptUsesNestedChannelCapabilityDescriptor(t *testing.T) {
	t.Parallel()

	got := effectiveSystemPrompt("Base prompt", nil, &Session{
		Key: "direct-session",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive":     true,
				"inline_delivery": true,
			},
		},
	})
	if !strings.Contains(got, "<channel_rules>") {
		t.Fatalf("expected channel_rules from nested channel_capabilities, got:\n%s", got)
	}
	if strings.Contains(got, "current thread or topic") {
		t.Fatalf("did not expect threading guidance without threading capability, got:\n%s", got)
	}
}

func TestEffectiveSystemPromptKeepsComplexityAwareConcisenessRule(t *testing.T) {
	t.Parallel()

	got := effectiveSystemPrompt("Base prompt", nil, &Session{
		Key: "direct-session",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive":     true,
				"inline_delivery": true,
			},
		},
	})
	if !strings.Contains(got, "match response depth to task complexity") {
		t.Fatalf("prompt = %q, want complexity-aware conciseness rule", got)
	}
	if !strings.Contains(got, "caveats") || !strings.Contains(got, "next-step recommendations") {
		t.Fatalf("prompt = %q, want caveats and next-step guidance", got)
	}
}
