package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/skill"
)

func TestSkillEnsureInstallsMatchingSkillAndRefreshesRegistry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hubRoot := filepath.Join(root, "clawhub")
	bundleDir := filepath.Join(root, "bundles", "news-research")
	writeExecutableSkillBundle(t, bundleDir, "news-research", "research latest news", "news.fetch")
	writeCatalogEntry(t, filepath.Join(hubRoot, "index", "news-research.json"), map[string]any{
		"id":         "news-research",
		"name":       "news-research",
		"version":    "1.0.0",
		"summary":    "Search and fetch current news articles",
		"bundle_dir": bundleDir,
	})

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024, SkillEnsureLimit: 3})
	hub := skill.NewFileClawHubClient(hubRoot)
	service := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{
			{Kind: skill.SourceClawHub, Path: filepath.Join(hubRoot, "installs"), Priority: 300},
		},
	})
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("service.Refresh() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{
		ClawHub:      hub,
		SkillService: service,
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "skill.ensure",
		Input: map[string]any{
			"goal":           "search today's headlines and fetch articles",
			"required_tools": []any{"news.fetch"},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	var payload struct {
		Success    bool   `json:"success"`
		Resolved   bool   `json:"resolved"`
		Installed  bool   `json:"installed"`
		Name       string `json:"name"`
		Validation struct {
			Found bool `json:"found"`
			Ready bool `json:"ready"`
		} `json:"validation"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.Success || !payload.Resolved || !payload.Installed {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Name != "news-research" {
		t.Fatalf("payload.Name = %q", payload.Name)
	}
	if !payload.Validation.Found || !payload.Validation.Ready {
		t.Fatalf("validation = %+v", payload.Validation)
	}

	snapshot := service.Snapshot()
	found := false
	for _, pkg := range snapshot.Ordered {
		for _, manifest := range pkg.ToolManifests {
			if manifest.Name == "news.fetch" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected installed skill tool news.fetch to be present after refresh")
	}
}

func TestSkillEnsureReturnsAlreadyAvailableWhenRequiredToolLoaded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hubRoot := filepath.Join(root, "clawhub")
	installDir := filepath.Join(hubRoot, "installs", "news-research", "1.0.0")
	writeExecutableSkillBundle(t, installDir, "news-research", "research latest news", "news.fetch")

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024, SkillEnsureLimit: 3})
	hub := skill.NewFileClawHubClient(hubRoot)
	service := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{
			{Kind: skill.SourceClawHub, Path: filepath.Join(hubRoot, "installs"), Priority: 300},
		},
	})
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("service.Refresh() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{
		ClawHub:      hub,
		SkillService: service,
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "skill.ensure",
		Input: map[string]any{
			"goal":           "search today's headlines and fetch articles",
			"required_tools": []any{"news.fetch"},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	var payload struct {
		Success   bool   `json:"success"`
		Resolved  bool   `json:"resolved"`
		Installed bool   `json:"installed"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.Success || !payload.Resolved || payload.Installed {
		t.Fatalf("payload = %+v", payload)
	}
}

func writeCatalogEntry(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("Marshal(payload): %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeExecutableSkillBundle(t *testing.T, dir, name, description, toolName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	skillDoc := `---
name: ` + name + `
description: ` + description + `
---
# ` + name + `
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name":              toolName,
			"side_effect_class": "read",
			"idempotent":        true,
			"execution_key":     "session:{id}",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
		"security": map[string]any{
			"trust": "community",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.manifest.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh): %v", err)
	}
}
