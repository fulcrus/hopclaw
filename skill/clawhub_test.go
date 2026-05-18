package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileClawHubSearchLocalIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Create index dir with a test skill.
	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "code-review.json"), catalogEntry{
		ID:      "code-review",
		Name:    "Code Review",
		Version: "1.0.0",
		Summary: "Automated code review skill",
	})
	writeJSON(t, filepath.Join(indexDir, "test-gen.json"), catalogEntry{
		ID:      "test-gen",
		Name:    "Test Generator",
		Version: "0.5.0",
		Summary: "Generate unit tests automatically",
	})

	ctx := context.Background()

	// Search all.
	results, err := client.Search(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Search by name.
	results, err = client.Search(ctx, "code")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'code', got %d", len(results))
	}
	if results[0].ID != "code-review" {
		t.Fatalf("expected code-review, got %s", results[0].ID)
	}

	// Search by summary.
	results, err = client.Search(ctx, "unit tests")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'unit tests', got %d", len(results))
	}
	if results[0].ID != "test-gen" {
		t.Fatalf("expected test-gen, got %s", results[0].ID)
	}

	// Search no match.
	results, err = client.Search(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'nonexistent', got %d", len(results))
	}
}

func TestFileClawHubSearchEmptyIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	ctx := context.Background()
	results, err := client.Search(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results from empty index, got %d", len(results))
	}
}

func TestFileClawHubSearchDirectorySourceWithBundleManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := NewFileClawHubClient(root)

	sourceDir := filepath.Join(t.TempDir(), "sources")
	bundleDir := filepath.Join(sourceDir, "feishu-suite")
	if err := os.MkdirAll(filepath.Join(bundleDir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "runtime", "run.py"), []byte("#!/usr/bin/env python3\nprint('ok')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "BUNDLE.yaml"), []byte(`
id: feishu-suite
version: 0.1.0
name: Feishu Suite
description: Feishu bundle tools
runtime:
  type: executable
  executable:
    entry: runtime/run.py
tools:
  - name: feishu.doc.read
    side_effect_class: read
`), 0o644); err != nil {
		t.Fatal(err)
	}

	client.Sources = []string{sourceDir}

	results, err := client.Search(context.Background(), "feishu")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "feishu-suite" {
		t.Fatalf("result ID = %q", results[0].ID)
	}
	if results[0].BundleDir != bundleDir {
		t.Fatalf("bundle dir = %q", results[0].BundleDir)
	}
}

func TestFileClawHubSyncRemoteJSONSource(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			t.Fatalf("unexpected query in sync: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"skills": []RegistrySkill{{
				ID:        "feishu-suite",
				Name:      "Feishu Suite",
				Version:   "0.1.0",
				Summary:   "Feishu bundle tools",
				BundleURL: "https://example.com/feishu-suite.tar.gz",
			}},
		})
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	client.Sources = []string{server.URL}

	if err := client.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(client.Layout.IndexDir(), "feishu-suite.json"))
	if err != nil {
		t.Fatalf("read synced index: %v", err)
	}
	var entry catalogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal synced entry: %v", err)
	}
	if entry.BundleURL != "https://example.com/feishu-suite.tar.gz" {
		t.Fatalf("bundle url = %q", entry.BundleURL)
	}
}

func TestFileClawHubInstallFromLocalBundle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Create a bundle directory with a SKILL.md.
	bundleDir := filepath.Join(t.TempDir(), "my-skill-bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Test Skill\nA test."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "run.sh"), []byte("#!/bin/bash\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create index entry with bundle_dir.
	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "my-skill.json"), catalogEntry{
		ID:        "my-skill",
		Name:      "My Skill",
		Version:   "1.2.3",
		Summary:   "A test skill",
		BundleDir: bundleDir,
	})

	ctx := context.Background()
	result, err := client.Install(ctx, InstallRequest{SkillID: "my-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillID != "my-skill" {
		t.Fatalf("expected skill ID 'my-skill', got %q", result.SkillID)
	}
	if result.Version != "1.2.3" {
		t.Fatalf("expected version '1.2.3', got %q", result.Version)
	}
	if result.InstallDir == "" {
		t.Fatal("install dir should not be empty")
	}

	// Verify the files were copied.
	if _, err := os.Stat(filepath.Join(result.InstallDir, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not found in install dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.InstallDir, "run.sh")); err != nil {
		t.Fatalf("run.sh not found in install dir: %v", err)
	}

	// Verify lock file was created.
	installed, err := client.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed skill, got %d", len(installed))
	}
	if installed[0].SkillID != "my-skill" {
		t.Fatalf("expected 'my-skill' in lock, got %q", installed[0].SkillID)
	}
}

func TestFileClawHubInstallEmptyID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	ctx := context.Background()
	_, err := client.Install(ctx, InstallRequest{})
	if err == nil {
		t.Fatal("expected error for empty skill ID")
	}
}

func TestFileClawHubInstallNotFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	ctx := context.Background()
	_, err := client.Install(ctx, InstallRequest{SkillID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestFileClawHubRemove(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Install a skill first.
	bundleDir := filepath.Join(t.TempDir(), "remove-test")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Remove Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "remove-me.json"), catalogEntry{
		ID: "remove-me", Name: "Remove Me", Version: "1.0.0",
		Summary: "Will be removed", BundleDir: bundleDir,
	})

	ctx := context.Background()
	result, err := client.Install(ctx, InstallRequest{SkillID: "remove-me"})
	if err != nil {
		t.Fatal(err)
	}

	// Verify install dir exists.
	if _, err := os.Stat(result.InstallDir); err != nil {
		t.Fatalf("install dir should exist after install: %v", err)
	}

	// Remove.
	if err := client.Remove("remove-me"); err != nil {
		t.Fatal(err)
	}

	// Verify install dir was removed.
	if _, err := os.Stat(result.InstallDir); !os.IsNotExist(err) {
		t.Fatal("install dir should be removed after Remove()")
	}

	// Verify lock file no longer contains the skill.
	installed, err := client.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("expected 0 installed after remove, got %d", len(installed))
	}
}

func TestFileClawHubRemoveNotInstalled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	err := client.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error removing non-installed skill")
	}
}

func TestFileClawHubUpdate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Create bundle and index entry.
	bundleDir := filepath.Join(t.TempDir(), "update-test")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Update Test v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "update-me.json"), catalogEntry{
		ID: "update-me", Name: "Update Me", Version: "2.0.0",
		Summary: "Will be updated", BundleDir: bundleDir,
	})

	ctx := context.Background()
	result, err := client.Update(ctx, "update-me")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "2.0.0" {
		t.Fatalf("expected version '2.0.0', got %q", result.Version)
	}
}

func TestFileClawHubPin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Install a skill first.
	bundleDir := filepath.Join(t.TempDir(), "pin-test")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Pin Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "pin-me.json"), catalogEntry{
		ID: "pin-me", Name: "Pin Me", Version: "1.0.0",
		Summary: "Will be pinned", BundleDir: bundleDir,
	})

	ctx := context.Background()
	if _, err := client.Install(ctx, InstallRequest{SkillID: "pin-me"}); err != nil {
		t.Fatal(err)
	}

	// Pin.
	if err := client.Pin(ctx, "pin-me", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	// Verify pinned.
	installed, err := client.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(installed))
	}
	if !installed[0].Pinned {
		t.Fatal("skill should be pinned")
	}
}

func TestFileClawHubSyncRemote(t *testing.T) {
	t.Parallel()

	// Set up a mock remote hub server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/skills/search" {
			http.NotFound(w, r)
			return
		}
		skills := []RegistrySkill{
			{ID: "remote-skill-1", Name: "Remote Skill 1", Version: "1.0.0", Summary: "From remote"},
			{ID: "remote-skill-2", Name: "Remote Skill 2", Version: "2.0.0", Summary: "Also from remote"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skills)
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	client.BaseURL = server.URL + "/v1"

	ctx := context.Background()
	if err := client.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// Clear BaseURL so Search only hits local index (avoid mock server returning unfiltered results).
	client.BaseURL = ""

	// Verify index files were created.
	results, err := client.Search(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 synced skills, got %d", len(results))
	}

	// Verify specific skill by searching unique text.
	results, err = client.Search(ctx, "also from remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "remote-skill-2" {
		t.Fatalf("expected remote-skill-2, got %q", results[0].ID)
	}
}

func TestFileClawHubInstallAfterSyncUsesPersistedBundleURL(t *testing.T) {
	t.Parallel()

	bundleBytes := makeSkillTarGz(t, map[string]string{
		"SKILL.md":            "# Remote Skill\nInstalled from synced bundle.\n",
		"skill.manifest.json": `{"version":"1","tool":{"name":"remote.run","side_effect_class":"read","idempotent":true,"execution_key":"session:{id}"},"runtime":{"entry":"scripts/run.sh","shell":"bash"},"security":{"trust":"community"}}`,
		"scripts/run.sh":      "#!/bin/sh\necho remote\n",
	})

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/skills/search":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]RegistrySkill{{
				ID:        "remote-install",
				Name:      "remote-install",
				Version:   "1.0.0",
				Summary:   "Remote installable skill",
				BundleURL: server.URL + "/bundles/remote-install.tar.gz",
			}})
		case "/bundles/remote-install.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(bundleBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	client.BaseURL = server.URL + "/v1"

	ctx := context.Background()
	if err := client.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Simulate restart after startup sync. Installation should still work from the
	// cached index entry because bundle_url is now persisted locally.
	client.BaseURL = ""

	result, err := client.Install(ctx, InstallRequest{SkillID: "remote-install"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.InstallDir == "" {
		t.Fatal("install dir should not be empty")
	}
	if _, err := os.Stat(filepath.Join(result.InstallDir, "SKILL.md")); err != nil {
		t.Fatalf("installed bundle missing SKILL.md: %v", err)
	}
}

func TestFileClawHubSearchRemoteMerge(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skills := []RegistrySkill{
			{ID: "shared", Name: "Shared", Version: "2.0.0", Summary: "Remote version"},
			{ID: "remote-only", Name: "Remote Only", Version: "1.0.0", Summary: "Only on remote"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skills)
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	client.BaseURL = server.URL + "/v1"

	// Add a local entry with same ID as a remote entry.
	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "shared.json"), catalogEntry{
		ID: "shared", Name: "Shared", Version: "1.0.0", Summary: "Local version",
	})

	ctx := context.Background()
	results, err := client.Search(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 results: local "shared" preferred over remote, plus "remote-only".
	if len(results) != 2 {
		t.Fatalf("expected 2 results (local preferred), got %d", len(results))
	}

	// Local version should win for "shared".
	for _, r := range results {
		if r.ID == "shared" && r.Version != "1.0.0" {
			t.Fatalf("expected local version '1.0.0' for 'shared', got %q", r.Version)
		}
	}
}

func TestFileClawHubSyncNoRemote(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// No BaseURL set — sync should be a no-op.
	ctx := context.Background()
	if err := client.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFileClawHubInstalledEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	installed, err := client.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("expected 0 installed, got %d", len(installed))
	}
}

func TestFileClawHubInstallWithVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	bundleDir := filepath.Join(t.TempDir(), "versioned")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte("# Versioned"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "versioned.json"), catalogEntry{
		ID: "versioned", Name: "Versioned", Version: "1.0.0",
		Summary: "Versioned skill", BundleDir: bundleDir,
	})

	ctx := context.Background()
	result, err := client.Install(ctx, InstallRequest{SkillID: "versioned", Version: "custom-v1"})
	if err != nil {
		t.Fatal(err)
	}
	// Requested version should override catalog version.
	if result.Version != "custom-v1" {
		t.Fatalf("expected version 'custom-v1', got %q", result.Version)
	}
}

func TestFileClawHubPublishUploadsTarGzBundle(t *testing.T) {
	t.Parallel()

	skillDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Publish Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/sh\necho publish\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".hidden"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "node_modules", "ignored.js"), []byte("console.log('ignore')"), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		gotAuthToken string
		gotSlug      string
		gotVersion   string
		archiveFiles map[string]string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/skills/publish" {
			t.Fatalf("path = %s, want /v1/skills/publish", r.URL.Path)
		}
		gotAuthToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		gotSlug = r.Header.Get("X-Skill-Slug")
		gotVersion = r.Header.Get("X-Skill-Version")

		gzr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("NewReader(): %v", err)
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		archiveFiles = make(map[string]string)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("tar.Next(): %v", err)
			}
			if hdr.FileInfo().IsDir() {
				continue
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("ReadAll(%s): %v", hdr.Name, err)
			}
			archiveFiles[hdr.Name] = string(data)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(PublishResult{
			Slug:    "publish-test",
			Version: "1.2.3",
			URL:     "https://example.com/skills/publish-test",
		}); err != nil {
			t.Fatalf("Encode(): %v", err)
		}
	}))
	defer server.Close()

	client := NewFileClawHubClient(t.TempDir())
	client.BaseURL = server.URL + "/v1"
	client.AuthToken = "publish-token"
	client.HTTPClient = server.Client()

	result, err := client.Publish(context.Background(), PublishRequest{
		SkillDir: skillDir,
		Slug:     "publish-test",
		Version:  "1.2.3",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if result.Slug != "publish-test" {
		t.Fatalf("result.Slug = %q", result.Slug)
	}
	if result.Version != "1.2.3" {
		t.Fatalf("result.Version = %q", result.Version)
	}
	if gotAuthToken != "publish-token" {
		t.Fatalf("auth token = %q", gotAuthToken)
	}
	if gotSlug != "publish-test" {
		t.Fatalf("slug header = %q", gotSlug)
	}
	if gotVersion != "1.2.3" {
		t.Fatalf("version header = %q", gotVersion)
	}
	if archiveFiles["SKILL.md"] != "# Publish Test\n" {
		t.Fatalf("SKILL.md content = %q", archiveFiles["SKILL.md"])
	}
	if archiveFiles["scripts/run.sh"] != "#!/bin/sh\necho publish\n" {
		t.Fatalf("scripts/run.sh content = %q", archiveFiles["scripts/run.sh"])
	}
	if _, ok := archiveFiles[".hidden"]; ok {
		t.Fatal("hidden files should not be uploaded")
	}
	if _, ok := archiveFiles["node_modules/ignored.js"]; ok {
		t.Fatal("node_modules files should not be uploaded")
	}
}

func TestClawHubLayout(t *testing.T) {
	t.Parallel()
	layout := ClawHubLayout{Root: "/home/user/.hopclaw/clawhub"}

	if got := layout.IndexDir(); got != "/home/user/.hopclaw/clawhub/index" {
		t.Fatalf("IndexDir = %q", got)
	}
	if got := layout.CacheDir(); got != "/home/user/.hopclaw/clawhub/cache" {
		t.Fatalf("CacheDir = %q", got)
	}
	if got := layout.BundleDir("my-skill", "1.0.0"); got != "/home/user/.hopclaw/clawhub/cache/bundles/my-skill/1.0.0" {
		t.Fatalf("BundleDir = %q", got)
	}
	if got := layout.InstallDir("my-skill", "1.0.0"); got != "/home/user/.hopclaw/clawhub/installs/my-skill/1.0.0" {
		t.Fatalf("InstallDir = %q", got)
	}
	if got := layout.SkillsLockPath(); got != "/home/user/.hopclaw/clawhub/locks/skills.lock.json" {
		t.Fatalf("SkillsLockPath = %q", got)
	}
}

func TestDefaultClawHubRoot(t *testing.T) {
	t.Parallel()
	got := DefaultClawHubRoot("/home/user")
	if got != "/home/user/.hopclaw/clawhub" {
		t.Fatalf("DefaultClawHubRoot = %q", got)
	}
}

// --- helper ---

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeSkillTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		data := []byte(content)
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if strings.HasSuffix(name, ".sh") {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s): %v", name, err)
		}
		if _, err := io.Copy(tw, bytes.NewReader(data)); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}
