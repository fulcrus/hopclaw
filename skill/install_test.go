package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMarkdownRichInstallMetadata(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: tooling
description: Tooling helpers
metadata:
  openclaw:
    install:
      - id: ripgrep
        kind: brew
        formula: ripgrep
        bins: [rg]
      - id: helper
        kind: download
        url: https://example.com/helper.zip
        archive: zip
        extract: true
        stripComponents: 1
        targetDir: tools/helper
---
# Tooling
`)

	spec, err := ParseSkillMarkdown(data)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown() error = %v", err)
	}
	if len(spec.OpenClaw.Install) != 2 {
		t.Fatalf("len(install) = %d", len(spec.OpenClaw.Install))
	}
	if spec.OpenClaw.Install[0].ResolvedKind() != "brew" {
		t.Fatalf("install[0].kind = %q", spec.OpenClaw.Install[0].ResolvedKind())
	}
	if spec.OpenClaw.Install[1].TargetDir != "tools/helper" {
		t.Fatalf("install[1].targetDir = %q", spec.OpenClaw.Install[1].TargetDir)
	}
	if spec.OpenClaw.Install[1].Extract == nil || !*spec.OpenClaw.Install[1].Extract {
		t.Fatalf("install[1].extract = %#v", spec.OpenClaw.Install[1].Extract)
	}
}

func TestInstallExecutorShellAndSkipByBins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executor := DefaultInstallExecutor()
	results, err := executor.Execute(context.Background(), root, []InstallSpec{
		{
			ID:     "bootstrap",
			Script: "mkdir -p generated && printf ready > generated/marker.txt",
		},
		{
			ID:     "skip-shell",
			Kind:   "shell",
			Bins:   []string{"sh"},
			Script: "exit 99",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].Status != InstallStepRan {
		t.Fatalf("results[0].Status = %q", results[0].Status)
	}
	if results[1].Status != InstallStepSkipped {
		t.Fatalf("results[1].Status = %q", results[1].Status)
	}
	data, err := os.ReadFile(filepath.Join(root, "generated", "marker.txt"))
	if err != nil {
		t.Fatalf("ReadFile(marker): %v", err)
	}
	if strings.TrimSpace(string(data)) != "ready" {
		t.Fatalf("marker = %q", string(data))
	}
}

func TestInstallExecutorDownloadExtractZip(t *testing.T) {
	prev := allowPrivateSkillDownloads
	allowPrivateSkillDownloads = true
	defer func() { allowPrivateSkillDownloads = prev }()

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	entry, err := zipWriter.Create("bundle/bin/tool.txt")
	if err != nil {
		t.Fatalf("Create(zip entry): %v", err)
	}
	if _, err := entry.Write([]byte("hello")); err != nil {
		t.Fatalf("Write(zip entry): %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("Close(zipWriter): %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()

	root := t.TempDir()
	extract := true
	executor := DefaultInstallExecutor()
	results, err := executor.Execute(context.Background(), root, []InstallSpec{{
		ID:              "helper",
		Kind:            "download",
		URL:             server.URL + "/helper.zip",
		Archive:         "zip",
		Extract:         &extract,
		StripComponents: 1,
		TargetDir:       "vendor/helper",
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 1 || results[0].Status != InstallStepRan {
		t.Fatalf("results = %#v", results)
	}
	data, err := os.ReadFile(filepath.Join(root, "vendor", "helper", "bin", "tool.txt"))
	if err != nil {
		t.Fatalf("ReadFile(extracted): %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("extracted content = %q", string(data))
	}
}

func TestBuildInstallCommandEnvResolvesRefsAndSanitizesHostEnv(t *testing.T) {
	t.Setenv("HOPCLAW_INSTALL_SECRET", "install-secret")
	t.Setenv("HOPCLAW_INSTALL_LEAK", "host-only")

	env, err := buildInstallCommandEnv([]string{
		"TOKEN=env:HOPCLAW_INSTALL_SECRET",
		"MODE=literal",
	})
	if err != nil {
		t.Fatalf("buildInstallCommandEnv() error = %v", err)
	}
	if got := envSliceValue(env, "TOKEN"); got != "install-secret" {
		t.Fatalf("TOKEN = %q, want %q", got, "install-secret")
	}
	if got := envSliceValue(env, "MODE"); got != "literal" {
		t.Fatalf("MODE = %q, want %q", got, "literal")
	}
	if got := envSliceValue(env, "HOPCLAW_INSTALL_LEAK"); got != "" {
		t.Fatalf("unexpected host env leak = %q", got)
	}
	if got := envSliceValue(env, "PATH"); got == "" {
		t.Fatal("PATH should be present in child env")
	}
}

func TestLocalInstallerRunsBundleInstallers(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("MkdirAll(bundle): %v", err)
	}
	content := `---
name: auto-setup
description: Auto setup
metadata:
  openclaw:
    install:
      - id: setup
        kind: shell
        script: |
          mkdir -p generated
          printf ready > generated/setup.txt
---
# Auto setup
`
	if err := os.WriteFile(filepath.Join(bundle, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}

	installer := LocalInstaller{
		Layout: ClawHubLayout{Root: filepath.Join(tmp, "clawhub")},
	}
	result, err := installer.InstallFromBundle(context.Background(), InstallRequest{
		SkillID: "auto-setup",
		Version: "1.0.0",
	}, bundle)
	if err != nil {
		t.Fatalf("InstallFromBundle() error = %v", err)
	}
	if len(result.InstallerSteps) != 1 || result.InstallerSteps[0].Status != InstallStepRan {
		t.Fatalf("InstallerSteps = %#v", result.InstallerSteps)
	}
	data, err := os.ReadFile(filepath.Join(result.InstallDir, "generated", "setup.txt"))
	if err != nil {
		t.Fatalf("ReadFile(setup): %v", err)
	}
	if strings.TrimSpace(string(data)) != "ready" {
		t.Fatalf("setup marker = %q", string(data))
	}
}

func envSliceValue(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}
