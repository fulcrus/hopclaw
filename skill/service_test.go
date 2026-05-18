package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceRefreshAndBindReturnsSessionSnapshot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "alpha"), "alpha", "alpha skill")
	mustWriteSkill(t, filepath.Join(root, "beta"), "beta", "beta skill")

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})

	runtimeCtx := RuntimeContext{GOOS: "linux"}
	session, err := svc.RefreshAndBind(context.Background(), runtimeCtx)
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}
	if len(session.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(session.Skills))
	}
	if session.Fingerprint == "" {
		t.Fatal("Fingerprint should not be empty")
	}
	if session.ContextFingerprint == "" {
		t.Fatal("ContextFingerprint should not be empty")
	}
}

func TestServiceBindSessionUsesExistingSnapshot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "tool"), "tool", "tool skill")

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})

	// First do a refresh to populate the registry.
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	// BindSession should work without another refresh.
	session := svc.BindSession(RuntimeContext{GOOS: "linux"})
	if len(session.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(session.Skills))
	}
	bound, ok := session.Resolve("tool")
	if !ok {
		t.Fatal("tool not found in session")
	}
	if !bound.Eligibility.Eligible {
		t.Fatalf("tool should be eligible, got reasons %v", bound.Eligibility.Reasons)
	}
}

func TestServiceBindSessionFiltersIneligible(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "open"), "open", "open skill")

	restrictedDir := filepath.Join(root, "restricted")
	if err := os.MkdirAll(restrictedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := `---
name: restricted
description: needs secret
metadata: {"openclaw":{"requires":{"env":["SECRET_KEY"]}}}
---
# restricted
`
	if err := os.WriteFile(filepath.Join(restrictedDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})

	session, err := svc.RefreshAndBind(context.Background(), RuntimeContext{GOOS: "linux"})
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}

	// Both skills should exist in the session.
	if len(session.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(session.Skills))
	}

	// Only the open skill should be in the prompt catalog.
	if len(session.PromptCatalog) != 1 {
		t.Fatalf("expected 1 prompt catalog entry, got %d", len(session.PromptCatalog))
	}
	if session.PromptCatalog[0].Name != "open" {
		t.Fatalf("prompt entry = %q", session.PromptCatalog[0].Name)
	}

	// Restricted should be ineligible.
	restrictedBound, ok := session.Resolve("restricted")
	if !ok {
		t.Fatal("restricted not in session")
	}
	if restrictedBound.Eligibility.Eligible {
		t.Fatal("restricted should be ineligible without SECRET_KEY")
	}
}

func TestServiceRefreshAndBindPromptBlock(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "demo"), "demo", "demo skill")

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})

	session, err := svc.RefreshAndBind(context.Background(), RuntimeContext{GOOS: "linux"})
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}
	if !strings.Contains(session.PromptBlock, `<skills>`) {
		t.Fatalf("PromptBlock = %q", session.PromptBlock)
	}
	if !strings.Contains(session.PromptBlock, `name="demo"`) {
		t.Fatalf("PromptBlock should contain demo skill: %q", session.PromptBlock)
	}
}

func TestServiceSnapshot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "snap"), "snap", "snap skill")

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
	})

	// Before refresh, snapshot should be empty.
	snap := svc.Snapshot()
	if len(snap.Skills) != 0 {
		t.Fatalf("expected empty snapshot before refresh, got %d", len(snap.Skills))
	}

	// After refresh, snapshot should have the skill.
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	snap = svc.Snapshot()
	if len(snap.Skills) != 1 {
		t.Fatalf("expected 1 skill after refresh, got %d", len(snap.Skills))
	}
}

func TestServiceDefaultLimits(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{})
	// Service should use default limits when none provided.
	if svc.limits.MaxFileSize != DefaultLimits().MaxFileSize {
		t.Fatalf("MaxFileSize = %d", svc.limits.MaxFileSize)
	}
	if svc.limits.MaxTotalSkills != DefaultLimits().MaxTotalSkills {
		t.Fatalf("MaxTotalSkills = %d", svc.limits.MaxTotalSkills)
	}
}

func TestServiceCustomLimits(t *testing.T) {
	t.Parallel()

	customLimits := Limits{
		MaxFileSize:     1024,
		MaxSkillsPerDir: 10,
		MaxTotalSkills:  20,
		MaxPromptChars:  5000,
	}
	svc := NewService(ServiceConfig{Limits: customLimits})
	if svc.limits.MaxFileSize != 1024 {
		t.Fatalf("MaxFileSize = %d", svc.limits.MaxFileSize)
	}
	if svc.limits.MaxTotalSkills != 20 {
		t.Fatalf("MaxTotalSkills = %d", svc.limits.MaxTotalSkills)
	}
}

func TestSessionSkillSnapshotResolveTool(t *testing.T) {
	t.Parallel()

	snap := SessionSkillSnapshot{
		Ordered: []BoundSkill{
			{
				Package: &SkillPackage{
					Kind:   SkillKindExecutable,
					Prompt: PromptSkill{Name: "exec-skill"},
					ToolManifests: []ToolManifest{
						{Name: "exec.run", Aliases: []string{"exec.execute"}},
					},
				},
				Eligibility: EligibilityResult{Eligible: true},
			},
		},
	}

	// Resolve by primary name.
	tool, ok := snap.ResolveTool("exec.run")
	if !ok {
		t.Fatal("ResolveTool(exec.run) not found")
	}
	if tool.Manifest.Name != "exec.run" {
		t.Fatalf("tool name = %q", tool.Manifest.Name)
	}

	// Resolve by alias.
	tool, ok = snap.ResolveTool("exec.execute")
	if !ok {
		t.Fatal("ResolveTool(exec.execute) not found by alias")
	}

	// Case-insensitive match.
	tool, ok = snap.ResolveTool("EXEC.RUN")
	if !ok {
		t.Fatal("ResolveTool should be case-insensitive")
	}

	// Not found.
	_, ok = snap.ResolveTool("nonexistent")
	if ok {
		t.Fatal("ResolveTool(nonexistent) should return false")
	}
}

func TestTrimPromptCatalog(t *testing.T) {
	t.Parallel()

	entries := make([]PromptCatalogEntry, 20)
	for i := range entries {
		entries[i] = PromptCatalogEntry{
			Name:        fmt.Sprintf("skill-%02d", i),
			Description: strings.Repeat("x", 100),
			Location:    "workspace:test",
		}
	}

	snap := &SessionSkillSnapshot{
		PromptCatalog: entries,
		PromptBlock:   FormatPromptCatalog(entries),
	}

	// Set a very tight limit.
	trimPromptCatalog(snap, 500)
	if len(snap.PromptBlock) > 500 {
		t.Fatalf("PromptBlock length = %d, exceeds limit 500", len(snap.PromptBlock))
	}
	if len(snap.PromptCatalog) != 20 {
		t.Fatalf("expected full prompt catalog to be preserved, got %d entries", len(snap.PromptCatalog))
	}
	if !strings.Contains(snap.PromptBlock, "omitted due to size") {
		t.Fatalf("PromptBlock = %q, want omission notice after trimming", snap.PromptBlock)
	}
}

func TestTrimPromptCatalogNoTrimNeeded(t *testing.T) {
	t.Parallel()

	entries := []PromptCatalogEntry{
		{Name: "a", Description: "short"},
	}
	snap := &SessionSkillSnapshot{
		PromptCatalog: entries,
		PromptBlock:   FormatPromptCatalog(entries),
	}
	originalLen := len(snap.PromptCatalog)
	trimPromptCatalog(snap, 10000)
	if len(snap.PromptCatalog) != originalLen {
		t.Fatal("should not trim when under limit")
	}
}

func TestTrimPromptCatalogZeroLimit(t *testing.T) {
	t.Parallel()

	entries := []PromptCatalogEntry{{Name: "a"}}
	snap := &SessionSkillSnapshot{
		PromptCatalog: entries,
		PromptBlock:   FormatPromptCatalog(entries),
	}
	trimPromptCatalog(snap, 0)
	// Zero limit should be a no-op.
	if len(snap.PromptCatalog) != 1 {
		t.Fatal("zero limit should not trim")
	}
}

func TestFingerprintRuntimeContext(t *testing.T) {
	t.Parallel()

	ctx1 := RuntimeContext{
		GOOS:               "linux",
		SecretPresence:     map[string]SecretStatus{"A": {Resolved: true, Source: "runtime_env"}},
		Git:                GitContext{Remotes: []string{"origin", "upstream"}},
		Workspace:          WorkspaceContext{Markers: []string{"package.json", "go.mod"}},
		ModuleCapabilities: []string{"browser", "git"},
	}
	ctx2 := RuntimeContext{
		GOOS:               "linux",
		SecretPresence:     map[string]SecretStatus{"A": {Resolved: false, Source: "runtime_env"}},
		Git:                GitContext{Remotes: []string{"upstream", "origin"}},
		Workspace:          WorkspaceContext{Markers: []string{"go.mod", "package.json"}},
		ModuleCapabilities: []string{"git", "browser"},
	}
	ctx3 := RuntimeContext{
		GOOS:               "linux",
		SecretPresence:     map[string]SecretStatus{"A": {Resolved: true, Source: "runtime_env"}},
		Git:                GitContext{Remotes: []string{"upstream", "origin", "origin"}},
		Workspace:          WorkspaceContext{Markers: []string{"go.mod", "package.json", "go.mod"}},
		ModuleCapabilities: []string{"git", "browser", "browser"},
	}

	fp1 := FingerprintRuntimeContext(ctx1)
	fp2 := FingerprintRuntimeContext(ctx2)
	fp3 := FingerprintRuntimeContext(ctx3)

	if fp1 == "" {
		t.Fatal("fingerprint should not be empty")
	}
	if fp1 == fp2 {
		t.Fatal("different contexts should have different fingerprints")
	}
	if fp1 != fp3 {
		t.Fatal("equivalent contexts should have same fingerprints regardless of slice order")
	}
}

func TestToolNameMatchesCaseInsensitive(t *testing.T) {
	t.Parallel()

	manifest := ToolManifest{
		Name:    "MyTool",
		Aliases: []string{"my-tool", "MT"},
	}

	if !toolNameMatches(manifest, "mytool") {
		t.Fatal("should match case-insensitively")
	}
	if !toolNameMatches(manifest, "MY-TOOL") {
		t.Fatal("should match alias case-insensitively")
	}
	if !toolNameMatches(manifest, " mt ") {
		t.Fatal("should match alias with whitespace trimming")
	}
	if toolNameMatches(manifest, "other") {
		t.Fatal("should not match non-matching name")
	}
}

func TestFormatPromptCatalog(t *testing.T) {
	t.Parallel()

	entries := []PromptCatalogEntry{
		{Name: "alpha", Description: "Alpha tool", Location: "workspace:alpha"},
		{Name: "beta", Description: "Beta & <tool>", Location: "user:beta"},
	}
	block := FormatPromptCatalog(entries)

	if !strings.Contains(block, `<skills>`) {
		t.Fatal("should start with <skills>")
	}
	if !strings.Contains(block, `</skills>`) {
		t.Fatal("should end with </skills>")
	}
	if !strings.Contains(block, `name="alpha"`) {
		t.Fatal("should contain alpha")
	}
	// XML escaping.
	if !strings.Contains(block, `Beta &amp; &lt;tool&gt;`) {
		t.Fatalf("should escape XML: %q", block)
	}
}

func TestFormatPromptCatalogEmpty(t *testing.T) {
	t.Parallel()

	block := FormatPromptCatalog(nil)
	if block != "<skills>\n</skills>" {
		t.Fatalf("empty catalog = %q", block)
	}
}

func TestFormatPromptCatalogWithNotice(t *testing.T) {
	t.Parallel()

	block := FormatPromptCatalogWithNotice([]PromptCatalogEntry{
		{Name: "alpha", Description: "Alpha tool", Location: "workspace:alpha"},
	}, 3, "Use skill.ensure if a needed capability is missing.")
	if !strings.Contains(block, "<note>") {
		t.Fatalf("catalog should include note when omitted > 0: %q", block)
	}
	if !strings.Contains(block, "3 additional skills omitted.") {
		t.Fatalf("catalog note missing omitted count: %q", block)
	}
}

func TestServiceBindSessionWithEvaluator(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "git-tool"), "git-tool", "git tool")

	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}},
		Evaluator: Evaluator{
			LookPath: func(file string) (string, error) {
				if file == "git" {
					return "/usr/bin/git", nil
				}
				return "", fmt.Errorf("not found")
			},
		},
	})

	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	session := svc.BindSession(RuntimeContext{GOOS: "linux"})
	bound, ok := session.Resolve("git-tool")
	if !ok {
		t.Fatal("git-tool not found in session")
	}
	if !bound.Eligibility.Eligible {
		t.Fatalf("git-tool should be eligible, got reasons %v", bound.Eligibility.Reasons)
	}
}
