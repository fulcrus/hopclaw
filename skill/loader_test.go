package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseSkillMarkdown(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: repo-review
description: Review a repository carefully
homepage: https://example.com/review
user-invocable: true
disable-model-invocation: false
command-dispatch: tool
command-tool: review.run
command-arg-mode: raw
metadata: {"openclaw":{"skillKey":"code.review","primaryEnv":"REVIEW_TOKEN","requires":{"bins":["git"],"env":["REVIEW_TOKEN"]},"always":false}}
---
# Review

Use the review tool with care.
`)

	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}

	if spec.Name != "repo-review" {
		t.Fatalf("spec.Name = %q", spec.Name)
	}
	if spec.OpenClaw.SkillKey != "code.review" {
		t.Fatalf("spec.OpenClaw.SkillKey = %q", spec.OpenClaw.SkillKey)
	}
	if spec.CommandTool != "review.run" {
		t.Fatalf("spec.CommandTool = %q", spec.CommandTool)
	}
	if !strings.Contains(spec.Body, "Use the review tool") {
		t.Fatalf("spec.Body = %q", spec.Body)
	}
}

func TestRegistryRefreshHonorsRootPriority(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	user := filepath.Join(tmp, "user")
	mustWriteSkill(t, filepath.Join(workspace, "review"), "review", "workspace description")
	mustWriteSkill(t, filepath.Join(user, "review"), "review", "user description")

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	_, err := reg.Refresh(context.Background(), []DiscoveryRoot{
		{Kind: SourceUser, Path: user},
		{Kind: SourceWorkspace, Path: workspace},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	pkg, ok := reg.Resolve("review")
	if !ok {
		t.Fatal("Resolve(review) not found")
	}
	if pkg.Prompt.Description != "workspace description" {
		t.Fatalf("description = %q", pkg.Prompt.Description)
	}
}

func TestEligibilityUsesPrimaryEnvInjection(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "deploy"},
		OpenClaw: OpenClawMetadata{
			PrimaryEnv: "DEPLOY_TOKEN",
			Requires: RequiresSpec{
				Env: []string{"DEPLOY_TOKEN"},
			},
		},
	}

	eval := Evaluator{
		LookPath: func(string) (string, error) { return "/bin/echo", nil },
	}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: runtime.GOOS,
		Managed: map[string]ManagedEntry{
			"deploy": {InjectedEnv: map[string]SecretStatus{
				"DEPLOY_TOKEN": {Resolved: true, Source: "managed"},
			}},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible, got reasons %v", result.Reasons)
	}
	if len(result.InjectedEnv) != 1 || result.InjectedEnv[0] != "DEPLOY_TOKEN" {
		t.Fatalf("injected env = %#v", result.InjectedEnv)
	}
}

func TestParseDirLoadsCompanionManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteSkill(t, dir, "bundle-review", "bundle review")
	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name":              "bundle.review",
			"side_effect_class": "read",
			"idempotent":        true,
			"execution_key":     "session:{id}",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
		"security": map[string]any{
			"trust":             "community",
			"requires_approval": true,
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if spec.Companion == nil {
		t.Fatal("expected companion manifest")
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), SkillSource{
		Kind:     SourceClawHub,
		Root:     dir,
		Dir:      dir,
		NameHint: "bundle-review",
		Priority: 300,
	}, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Kind != SkillKindExecutable {
		t.Fatalf("pkg.Kind = %q", pkg.Kind)
	}
	if len(pkg.ToolManifests) != 1 || pkg.ToolManifests[0].Name != "bundle.review" {
		t.Fatalf("tool manifests = %#v", pkg.ToolManifests)
	}
	if pkg.Trust != TrustCommunity {
		t.Fatalf("pkg.Trust = %q", pkg.Trust)
	}
}

func TestLocalInstallerWritesLockFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "bundle")
	mustWriteSkill(t, bundle, "ops-skill", "ops skill")

	installer := LocalInstaller{
		Layout: ClawHubLayout{Root: filepath.Join(tmp, "clawhub")},
	}
	result, err := installer.InstallFromBundle(context.Background(), InstallRequest{
		SkillID: "ops-skill",
		Version: "1.2.3",
	}, bundle)
	if err != nil {
		t.Fatalf("InstallFromBundle() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.InstallDir, "SKILL.md")); err != nil {
		t.Fatalf("installed skill missing SKILL.md: %v", err)
	}

	lock, err := installer.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock() error = %v", err)
	}
	if len(lock.Skills) != 1 || lock.Skills[0].SkillID != "ops-skill" {
		t.Fatalf("lock = %#v", lock)
	}
}

func TestFilesystemLoaderDiscoversLatestVersionedClawHubInstall(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "clawhub", "installs")
	mustWriteSkill(t, filepath.Join(root, "ops-skill", "1.0.0"), "ops-skill", "old version")
	mustWriteSkill(t, filepath.Join(root, "ops-skill", "2.0.0"), "ops-skill", "new version")

	loader := FilesystemLoader{}
	sources, err := loader.Discover(context.Background(), []DiscoveryRoot{
		{Kind: SourceClawHub, Path: root},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(sources))
	}
	if !strings.HasSuffix(sources[0].Dir, filepath.Join("ops-skill", "2.0.0")) {
		t.Fatalf("sources[0].Dir = %q", sources[0].Dir)
	}
}

func TestRegistryRefreshKeepsWorkingWhenSkillIsBlocked(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "skills")
	mustWriteSkill(t, filepath.Join(root, "ready"), "ready", "ready skill")
	brokenDir := filepath.Join(root, "broken")
	mustWriteSkill(t, brokenDir, "broken", "broken skill")

	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name":              "broken.exec",
			"side_effect_class": "external_write",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest): %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "skill.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(context.Background(), []DiscoveryRoot{
		{Kind: SourceWorkspace, Path: root},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if _, ok := snapshot.Skills["ready"]; !ok {
		t.Fatal("expected ready skill to remain registered")
	}
	if len(snapshot.Blocked) != 1 {
		t.Fatalf("snapshot.Blocked = %#v", snapshot.Blocked)
	}
	if snapshot.Blocked[0].NameHint != "broken" {
		t.Fatalf("blocked name = %q", snapshot.Blocked[0].NameHint)
	}
}

func TestServiceRefreshAndBindBuildsPromptCatalog(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "skills")
	mustWriteSkill(t, filepath.Join(root, "eligible"), "eligible", "eligible skill")

	restricted := filepath.Join(root, "restricted")
	content := `---
name: restricted
description: restricted skill
metadata: {"openclaw":{"requires":{"env":["RESTRICTED_TOKEN"]}}}
---
# restricted
`
	if err := os.MkdirAll(restricted, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", restricted, err)
	}
	if err := os.WriteFile(filepath.Join(restricted, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}

	service := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{
			{Kind: SourceWorkspace, Path: root},
		},
	})
	sessionSnapshot, err := service.RefreshAndBind(context.Background(), RuntimeContext{})
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}
	if len(sessionSnapshot.Skills) != 2 {
		t.Fatalf("len(sessionSnapshot.Skills) = %d", len(sessionSnapshot.Skills))
	}
	if len(sessionSnapshot.PromptCatalog) != 1 {
		t.Fatalf("PromptCatalog = %#v", sessionSnapshot.PromptCatalog)
	}
	if sessionSnapshot.PromptCatalog[0].Name != "eligible" {
		t.Fatalf("prompt skill = %#v", sessionSnapshot.PromptCatalog[0])
	}
	if !strings.Contains(sessionSnapshot.PromptBlock, `name="eligible"`) {
		t.Fatalf("PromptBlock = %q", sessionSnapshot.PromptBlock)
	}

	restrictedSkill, ok := sessionSnapshot.Resolve("restricted")
	if !ok {
		t.Fatal("restricted skill missing from session snapshot")
	}
	if restrictedSkill.Eligibility.Eligible {
		t.Fatalf("expected restricted skill to be ineligible, got %#v", restrictedSkill.Eligibility)
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
		t.Fatalf("WriteFile(%s): %v", dir, err)
	}
}
