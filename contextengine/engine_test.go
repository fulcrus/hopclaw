package contextengine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

func TestPrepareBindsSkillsAndBuildsSystemPrompt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteSkill(t, filepath.Join(root, "repo-review"), "repo-review", "Review repositories carefully")

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: root}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:    "You are a production coding agent.",
		IncludeSkillCatalog: true,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, svc)

	session := &Session{
		ID: "s1",
		Messages: []Message{
			{Role: RoleUser, Content: "Inspect the repo"},
			{Role: RoleAssistant, Content: "I will inspect it."},
		},
		Summary: "The user is asking for a repository review.",
	}
	run := &Run{
		ID:               "r1",
		SystemPrompt:     "Prefer precise findings.",
		MaxContextTokens: 400,
		MaxOutputTokens:  50,
	}

	prepared, budget, err := engine.Prepare(context.Background(), session, run, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if session.SkillSnapshot.Fingerprint == "" {
		t.Fatal("expected session to bind a skill snapshot")
	}
	if !strings.Contains(prepared.SystemPrompt, "<skills>") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if len(prepared.Messages) != 3 {
		t.Fatalf("len(Messages) = %d", len(prepared.Messages))
	}
	if prepared.Messages[0].Name != "session-summary" {
		t.Fatalf("summary message = %#v", prepared.Messages[0])
	}
	if budget.EstimatedInputTokens == 0 {
		t.Fatalf("budget = %#v", budget)
	}
}

func TestPrepareTrimsMessagesToBudget(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		KeepFirstN:           1,
		KeepLastN:            2,
		DefaultContextWindow: 60,
		DefaultOutputTokens:  10,
		MaxInputRatio:        0.75,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("a", 12)},
			{Role: RoleAssistant, Content: strings.Repeat("b", 12)},
			{Role: RoleUser, Content: strings.Repeat("c", 12)},
			{Role: RoleAssistant, Content: strings.Repeat("d", 12)},
			{Role: RoleUser, Content: strings.Repeat("e", 12)},
		},
	}

	prepared, budget, err := engine.Prepare(context.Background(), session, &Run{
		MaxContextTokens: 60,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if budget.EstimatedInputTokens > budget.MaxInputTokens {
		t.Fatalf("budget overflow: %#v", budget)
	}
	if len(prepared.Messages) >= len(session.Messages) {
		t.Fatalf("expected trimmed messages, got %d", len(prepared.Messages))
	}
	if prepared.Messages[0].Content != strings.Repeat("a", 12) {
		t.Fatalf("first preserved message = %#v", prepared.Messages[0])
	}
}

func TestAppendToolResultsAndCompact(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		ToolResultMaxChars:  10,
		CompactKeepLastN:    2,
		CompactSummaryChars: 80,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "first"},
			{Role: RoleAssistant, Content: "second"},
			{Role: RoleUser, Content: "third"},
		},
	}

	if err := engine.AppendToolResults(context.Background(), session, []ToolResult{{
		ToolName:    "fs.read",
		ToolCallID:  "call-1",
		Content:     "123456789012345",
		ArtifactURI: "artifact://tool-result/1",
	}}); err != nil {
		t.Fatalf("AppendToolResults() error = %v", err)
	}
	last := session.Messages[len(session.Messages)-1]
	if !strings.Contains(last.Content, "[truncated]") {
		t.Fatalf("tool result = %q", last.Content)
	}
	result, ok := resultmodel.DecodeToolResultMetadata(last.Metadata)
	if !ok {
		t.Fatalf("tool result metadata = %#v", last.Metadata)
	}
	if result.ArtifactURI != "artifact://tool-result/1" {
		t.Fatalf("artifact_uri = %q", result.ArtifactURI)
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
	if !strings.Contains(session.Summary, "[compact_reason] emergency") {
		t.Fatalf("Summary = %q", session.Summary)
	}
}

func TestPrepareRebindsSkillSnapshotWhenRegistryOrRuntimeContextChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteSkill(t, filepath.Join(root, "repo-review"), "repo-review", "Review repositories carefully")

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: root}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	engine := NewSlidingWindowEngine(Config{}, svc)
	session := &Session{ID: "s1"}

	if _, _, err := engine.Prepare(context.Background(), session, &Run{ID: "r1"}, skill.RuntimeContext{
		Workspace: skill.WorkspaceContext{ProjectType: "review"},
	}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	first := session.SkillSnapshot
	if first.Fingerprint == "" || first.ContextFingerprint == "" {
		t.Fatalf("first snapshot = %#v", first)
	}

	if _, _, err := engine.Prepare(context.Background(), session, &Run{ID: "r2"}, skill.RuntimeContext{
		Workspace: skill.WorkspaceContext{ProjectType: "edit"},
	}); err != nil {
		t.Fatalf("Prepare() after runtime change error = %v", err)
	}
	second := session.SkillSnapshot
	if second.ContextFingerprint == first.ContextFingerprint {
		t.Fatalf("expected runtime context change to rebind snapshot: first=%q second=%q", first.ContextFingerprint, second.ContextFingerprint)
	}

	mustWriteSkill(t, filepath.Join(root, "browser-ops"), "browser-ops", "Use browser tools")
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() after skill add error = %v", err)
	}
	if _, _, err := engine.Prepare(context.Background(), session, &Run{ID: "r3"}, skill.RuntimeContext{
		Workspace: skill.WorkspaceContext{ProjectType: "edit"},
	}); err != nil {
		t.Fatalf("Prepare() after registry change error = %v", err)
	}
	third := session.SkillSnapshot
	if third.Fingerprint == second.Fingerprint {
		t.Fatalf("expected registry change to rebind snapshot: second=%q third=%q", second.Fingerprint, third.Fingerprint)
	}
}

func TestPrepareIncludesPinnedFactsInSystemPrompt(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:    "You are a production coding agent.",
		PinnedFactsMaxChars: 400,
	}, nil)

	session := &Session{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "The workspace is repo-alpha.",
				Metadata: map[string]any{
					MetadataKeyPinnedFact: map[string]any{
						"key":     "workspace",
						"content": "The active workspace is repo-alpha.",
					},
				},
			},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "Pinned facts:") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "repo-alpha") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if len(session.PinnedFacts) != 1 || session.PinnedFacts[0].Key != "workspace" {
		t.Fatalf("PinnedFacts = %#v", session.PinnedFacts)
	}
}

func TestCompactPreservesPinnedFactsFromTrimmedMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 200,
		PinnedFactsMaxChars: 400,
	}, nil)

	session := &Session{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "The deployment target is staging.",
				Metadata: map[string]any{
					MetadataKeyPinnedFact: "Deployment target stays on staging until explicitly changed.",
				},
			},
			{Role: RoleAssistant, Content: "Understood."},
			{Role: RoleUser, Content: "Continue with the rollout plan."},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.PinnedFacts) != 1 {
		t.Fatalf("PinnedFacts = %#v", session.PinnedFacts)
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() after compact error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "staging") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
}

func mustWriteSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	content := `---
name: ` + name + `
description: ` + description + `
---
# ` + name + `
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
}
