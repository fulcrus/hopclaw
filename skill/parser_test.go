package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMarkdownFullFrontmatter(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: code-review
description: Automated code review
homepage: https://example.com/code-review
user-invocable: true
disable-model-invocation: false
command-dispatch: tool
command-tool: review.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.codereview
    primaryEnv: REVIEW_TOKEN
    always: false
    emoji: "\U0001F50D"
    os:
      - linux
      - darwin
    requires:
      bins:
        - git
      env:
        - REVIEW_TOKEN
---
# Code Review

Use the code review tool to analyze pull requests.
`)

	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Name != "code-review" {
		t.Fatalf("Name = %q", spec.Name)
	}
	if spec.Description != "Automated code review" {
		t.Fatalf("Description = %q", spec.Description)
	}
	if spec.Homepage != "https://example.com/code-review" {
		t.Fatalf("Homepage = %q", spec.Homepage)
	}
	if !spec.UserInvocable {
		t.Fatal("UserInvocable should be true")
	}
	if spec.DisableModelInvocation {
		t.Fatal("DisableModelInvocation should be false")
	}
	if spec.CommandDispatch != "tool" {
		t.Fatalf("CommandDispatch = %q", spec.CommandDispatch)
	}
	if spec.CommandTool != "review.run" {
		t.Fatalf("CommandTool = %q", spec.CommandTool)
	}
	if spec.CommandArgMode != "raw" {
		t.Fatalf("CommandArgMode = %q", spec.CommandArgMode)
	}
	if spec.OpenClaw.SkillKey != "dev.codereview" {
		t.Fatalf("SkillKey = %q", spec.OpenClaw.SkillKey)
	}
	if spec.OpenClaw.PrimaryEnv != "REVIEW_TOKEN" {
		t.Fatalf("PrimaryEnv = %q", spec.OpenClaw.PrimaryEnv)
	}
	if spec.OpenClaw.Always {
		t.Fatal("Always should be false")
	}
	if len(spec.OpenClaw.OS) != 2 {
		t.Fatalf("OS = %v", spec.OpenClaw.OS)
	}
	if len(spec.OpenClaw.Requires.Bins) != 1 || spec.OpenClaw.Requires.Bins[0] != "git" {
		t.Fatalf("Requires.Bins = %v", spec.OpenClaw.Requires.Bins)
	}
	if len(spec.OpenClaw.Requires.Env) != 1 || spec.OpenClaw.Requires.Env[0] != "REVIEW_TOKEN" {
		t.Fatalf("Requires.Env = %v", spec.OpenClaw.Requires.Env)
	}
	if !strings.Contains(spec.Body, "analyze pull requests") {
		t.Fatalf("Body = %q", spec.Body)
	}
}

func TestParseSkillMarkdownMinimal(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: simple
---
# Simple Skill

Do simple things.
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Name != "simple" {
		t.Fatalf("Name = %q", spec.Name)
	}
	// Description should be inferred from body.
	if spec.Description != "Simple Skill" {
		t.Fatalf("Description = %q", spec.Description)
	}
	if !spec.UserInvocable {
		t.Fatal("UserInvocable should default to true")
	}
	if spec.DisableModelInvocation {
		t.Fatal("DisableModelInvocation should default to false")
	}
}

func TestParseSkillMarkdownMissingNameReturnsError(t *testing.T) {
	t.Parallel()

	data := []byte(`---
description: No name provided
---
# Body
`)
	_, err := ParseSkillMarkdown(data)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestParseSkillMarkdownNoFrontmatter(t *testing.T) {
	t.Parallel()

	data := []byte(`# Just a markdown file

No frontmatter here.
`)
	_, err := ParseSkillMarkdown(data)
	if err == nil {
		t.Fatal("expected error for missing name (no frontmatter)")
	}
}

func TestParseSkillMarkdownBOMHandled(t *testing.T) {
	t.Parallel()

	// UTF-8 BOM prefix.
	data := []byte("\xef\xbb\xbf---\nname: bom-test\ndescription: BOM test\n---\n# BOM\n")
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Name != "bom-test" {
		t.Fatalf("Name = %q", spec.Name)
	}
}

func TestParseSkillMarkdownUserInvocableFalse(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: hidden
description: Hidden from users
user-invocable: false
---
# Hidden
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.UserInvocable {
		t.Fatal("UserInvocable should be false when explicitly set")
	}
}

func TestParseSkillMarkdownDisableModelInvocationTrue(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: no-model
description: Disable model invocation
disable-model-invocation: true
---
# No Model
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if !spec.DisableModelInvocation {
		t.Fatal("DisableModelInvocation should be true")
	}
}

func TestParseSkillMarkdownMetadataAsJSONString(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: json-meta
description: JSON metadata
metadata: '{"openclaw":{"skillKey":"test.key","always":true}}'
---
# JSON Meta
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.OpenClaw.SkillKey != "test.key" {
		t.Fatalf("SkillKey = %q", spec.OpenClaw.SkillKey)
	}
	if !spec.OpenClaw.Always {
		t.Fatal("Always should be true")
	}
}

func TestParseSkillMarkdownHomepageFallback(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: homepage-test
description: Test homepage fallback
metadata:
  openclaw:
    homepage: https://fallback.example.com
---
# Homepage
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Homepage != "https://fallback.example.com" {
		t.Fatalf("Homepage = %q", spec.Homepage)
	}
}

func TestParseSkillMarkdownHomepageExplicitWins(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: homepage-explicit
description: Explicit homepage
homepage: https://explicit.example.com
metadata:
  openclaw:
    homepage: https://fallback.example.com
---
# Homepage
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Homepage != "https://explicit.example.com" {
		t.Fatalf("Homepage = %q", spec.Homepage)
	}
}

func TestParseSkillMarkdownInferDescription(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: infer-desc
---
# This Is The Title

Some body text follows.
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Description != "This Is The Title" {
		t.Fatalf("inferred Description = %q", spec.Description)
	}
}

func TestParseSkillMarkdownFrontmatterMap(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: with-fm
description: Has frontmatter
metadata:
  openclaw:
    skillKey: fm.key
---
# FM
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.Frontmatter == nil {
		t.Fatal("Frontmatter map should not be nil")
	}
	if _, ok := spec.Frontmatter["name"]; !ok {
		t.Fatal("Frontmatter should contain 'name' key")
	}
}

func TestSplitFrontmatterNoClosingMarker(t *testing.T) {
	t.Parallel()

	data := []byte("---\nname: broken\nno closing marker")
	fmBytes, body := splitFrontmatter(data)
	if fmBytes != nil {
		t.Fatalf("expected nil frontmatter bytes, got %q", string(fmBytes))
	}
	if !strings.Contains(body, "broken") {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitFrontmatterEmptyInput(t *testing.T) {
	t.Parallel()

	fmBytes, body := splitFrontmatter(nil)
	if fmBytes != nil || body != "" {
		t.Fatalf("fmBytes=%v, body=%q", fmBytes, body)
	}
}

func TestParseDirMissingSkillFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := ParseDir(dir)
	if err == nil {
		t.Fatal("expected error for missing SKILL.md")
	}
	if err.Error() != ErrMissingSkillFile.Error() {
		t.Fatalf("error = %v", err)
	}
}

func TestParseDirLoadsSupportingFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillContent := `---
name: with-files
description: Has supporting files
---
# With Files
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper.sh): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatalf("MkdirAll(lib): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "utils.py"), []byte("# util"), 0o644); err != nil {
		t.Fatalf("WriteFile(utils.py): %v", err)
	}

	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if spec.Name != "with-files" {
		t.Fatalf("Name = %q", spec.Name)
	}
	if len(spec.SupportingFiles) != 2 {
		t.Fatalf("expected 2 supporting files, got %d: %v", len(spec.SupportingFiles), spec.SupportingFiles)
	}
	// Supporting files should be sorted.
	if spec.SupportingFiles[0].Path != "helper.sh" {
		t.Fatalf("first file = %q", spec.SupportingFiles[0].Path)
	}
	if spec.SupportingFiles[1].Path != "lib/utils.py" {
		t.Fatalf("second file = %q", spec.SupportingFiles[1].Path)
	}
}

func TestParseDirLoadsCompanionManifestJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillContent := `---
name: with-companion
description: Has companion manifest
---
# Companion
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	manifest := `{
  "version": "1",
  "tool": {
    "name": "companion.run",
    "side_effect_class": "read"
  },
  "runtime": {
    "entry": "run.sh",
    "shell": "bash"
  },
  "security": {
    "trust": "internal"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if spec.Companion == nil {
		t.Fatal("Companion should not be nil")
	}
	if spec.Companion.Tool.Name != "companion.run" {
		t.Fatalf("Companion tool name = %q", spec.Companion.Tool.Name)
	}
	if spec.Companion.Runtime.Entry != "run.sh" {
		t.Fatalf("Companion runtime entry = %q", spec.Companion.Runtime.Entry)
	}
	if spec.Companion.Security.Trust != "internal" {
		t.Fatalf("Companion trust = %q", spec.Companion.Security.Trust)
	}
}

func TestParseDirNoCompanionManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillContent := `---
name: no-companion
description: No companion
---
# No Companion
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}

	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if spec.Companion != nil {
		t.Fatal("Companion should be nil when no manifest file")
	}
}

func TestParseSkillMarkdownInstallSpecsParsed(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: installer
description: Has install specs
metadata:
  openclaw:
    install:
      - id: setup
        kind: shell
        script: echo hello
      - id: fetch
        kind: download
        url: https://example.com/tool.tar.gz
        archive: tar.gz
---
# Installer
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if len(spec.OpenClaw.Install) != 2 {
		t.Fatalf("expected 2 install specs, got %d", len(spec.OpenClaw.Install))
	}
	if spec.OpenClaw.Install[0].ID != "setup" {
		t.Fatalf("install[0].ID = %q", spec.OpenClaw.Install[0].ID)
	}
	if spec.OpenClaw.Install[0].Script != "echo hello" {
		t.Fatalf("install[0].Script = %q", spec.OpenClaw.Install[0].Script)
	}
	if spec.OpenClaw.Install[1].URL != "https://example.com/tool.tar.gz" {
		t.Fatalf("install[1].URL = %q", spec.OpenClaw.Install[1].URL)
	}
}

func TestParseSkillMarkdownRequiresAnyBins(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: any-bins
description: Needs at least one
metadata:
  openclaw:
    requires:
      anyBins:
        - curl
        - wget
        - httpie
---
# Any Bins
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if len(spec.OpenClaw.Requires.AnyBins) != 3 {
		t.Fatalf("AnyBins = %v", spec.OpenClaw.Requires.AnyBins)
	}
}

func TestInferDescriptionSkipsEmptyLines(t *testing.T) {
	t.Parallel()

	got := inferDescription("\n\n# Title\nBody text")
	if got != "Title" {
		t.Fatalf("inferDescription = %q", got)
	}
}

func TestInferDescriptionEmptyBody(t *testing.T) {
	t.Parallel()

	got := inferDescription("")
	if got != "" {
		t.Fatalf("inferDescription = %q", got)
	}
}

func TestParseSkillMarkdownMetadataWithoutOpenClawKey(t *testing.T) {
	t.Parallel()

	// When metadata has no "openclaw" key, the entire metadata map is used.
	data := []byte(`---
name: flat-meta
description: Flat metadata
metadata:
  skillKey: flat.key
  always: true
---
# Flat
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if spec.OpenClaw.SkillKey != "flat.key" {
		t.Fatalf("SkillKey = %q", spec.OpenClaw.SkillKey)
	}
	if !spec.OpenClaw.Always {
		t.Fatal("Always should be true")
	}
}

func TestParseSkillMarkdownRequiresConfig(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: config-req
description: Needs config
metadata:
  openclaw:
    requires:
      config:
        - feature.enabled
        - auth.mode
---
# Config Required
`)
	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if len(spec.OpenClaw.Requires.Config) != 2 {
		t.Fatalf("Requires.Config = %v", spec.OpenClaw.Requires.Config)
	}
}
