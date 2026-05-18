package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// env.info tests
// ---------------------------------------------------------------------------

func TestEnvInfo(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-info",
		Name:  "env.info",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("env.info error = %v", err)
	}

	var payload struct {
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		GoVersion string `json:"go_version"`
		CPUs      int    `json:"cpus"`
		Hostname  string `json:"hostname"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.OS != runtime.GOOS {
		t.Fatalf("os = %q, want %q", payload.OS, runtime.GOOS)
	}
	if payload.Arch != runtime.GOARCH {
		t.Fatalf("arch = %q, want %q", payload.Arch, runtime.GOARCH)
	}
	if payload.CPUs <= 0 {
		t.Fatalf("cpus = %d, should be > 0", payload.CPUs)
	}
	if payload.Hostname == "" {
		t.Fatal("hostname should not be empty")
	}
}

// ---------------------------------------------------------------------------
// env.get tests
// ---------------------------------------------------------------------------

func TestEnvGet(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})

	// PATH should always be set.
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-env-get",
		Name: "env.get",
		Input: map[string]any{
			"names": []any{"PATH"},
		},
	}})
	if err != nil {
		t.Fatalf("env.get error = %v", err)
	}

	var payload struct {
		Vars []struct {
			Name     string `json:"name"`
			Exists   bool   `json:"exists"`
			Source   string `json:"source"`
			Managed  bool   `json:"managed"`
			Redacted bool   `json:"redacted"`
		} `json:"vars"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if len(payload.Vars) != 1 {
		t.Fatalf("vars length = %d, want 1", len(payload.Vars))
	}
	if payload.Vars[0].Name != "PATH" {
		t.Fatalf("vars[0].name = %q, want PATH", payload.Vars[0].Name)
	}
	if !payload.Vars[0].Exists {
		t.Fatal("PATH should exist")
	}
	if payload.Vars[0].Source == "" {
		t.Fatal("PATH should report a source")
	}
	if !payload.Vars[0].Redacted {
		t.Fatal("PATH should be redacted")
	}
	if payload.Vars[0].Managed {
		t.Fatal("PATH should not be reported as managed")
	}
}

func TestEnvGetNonExistent(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-env-get-miss",
		Name: "env.get",
		Input: map[string]any{
			"names": []any{"HOPCLAW_NONEXISTENT_VAR_XYZ"},
		},
	}})
	if err != nil {
		t.Fatalf("env.get error = %v", err)
	}

	var payload struct {
		Vars []struct {
			Name     string `json:"name"`
			Exists   bool   `json:"exists"`
			Redacted bool   `json:"redacted"`
		} `json:"vars"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if len(payload.Vars) != 1 {
		t.Fatalf("vars length = %d, want 1", len(payload.Vars))
	}
	if payload.Vars[0].Name != "HOPCLAW_NONEXISTENT_VAR_XYZ" {
		t.Fatalf("vars[0].name = %q", payload.Vars[0].Name)
	}
	if payload.Vars[0].Exists {
		t.Fatal("non-existent var should have exists=false")
	}
	if payload.Vars[0].Redacted {
		t.Fatal("missing var should not be redacted")
	}
}

// ---------------------------------------------------------------------------
// env.set tests
// ---------------------------------------------------------------------------

func TestEnvSet(t *testing.T) {
	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	varName := "HOPCLAW_TEST_ENV_SET_VAR"
	session := &agent.Session{ID: "sess-1"}
	run := &agent.Run{ID: "run-1"}

	results, err := builtins.ExecuteBatch(context.Background(), run, session, []agent.ToolCall{{
		ID:   "call-env-set",
		Name: "env.set",
		Input: map[string]any{
			"name":  varName,
			"value": "test_value_123",
		},
	}})
	if err != nil {
		t.Fatalf("env.set error = %v", err)
	}

	var payload struct {
		Name    string `json:"name"`
		Scope   string `json:"scope"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Name != varName {
		t.Fatalf("name = %q", payload.Name)
	}
	if payload.Scope != "run" {
		t.Fatalf("scope = %q, want run", payload.Scope)
	}

	results, err = builtins.ExecuteBatch(context.Background(), run, session, []agent.ToolCall{{
		ID:   "call-env-get-overlay",
		Name: "env.get",
		Input: map[string]any{
			"name": varName,
		},
	}})
	if err != nil {
		t.Fatalf("env.get error = %v", err)
	}
	var visibility struct {
		Name     string `json:"name"`
		Exists   bool   `json:"exists"`
		Source   string `json:"source"`
		Managed  bool   `json:"managed"`
		Redacted bool   `json:"redacted"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &visibility); err != nil {
		t.Fatalf("Unmarshal overlay error = %v", err)
	}
	if !visibility.Exists || visibility.Source != "overlay" || !visibility.Redacted || visibility.Managed {
		t.Fatalf("overlay visibility = %+v", visibility)
	}
	if got := os.Getenv(varName); got != "" {
		t.Fatalf("os.Getenv(%q) = %q, want host process unchanged", varName, got)
	}
}

// ---------------------------------------------------------------------------
// env.probe tests
// ---------------------------------------------------------------------------

func TestEnvProbe(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-probe",
		Name:  "env.probe",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("env.probe error = %v", err)
	}

	var payload struct {
		OS    string `json:"os"`
		Arch  string `json:"arch"`
		Shell string `json:"shell"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.OS == "" {
		t.Fatal("os should not be empty")
	}
	if payload.Arch == "" {
		t.Fatal("arch should not be empty")
	}
}

// ---------------------------------------------------------------------------
// env.refresh tests
// ---------------------------------------------------------------------------

func TestEnvRefresh(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-refresh",
		Name:  "env.refresh",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("env.refresh error = %v", err)
	}

	// Should return some result without error.
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Content == "" {
		t.Fatal("result content should not be empty")
	}
}

func TestSkillInspectReportsMissingDependencies(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills")
	skillDir := filepath.Join(skillRoot, "github-pr")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
name: github-pr
description: review github pull requests
metadata: {"openclaw":{"primaryEnv":"GITHUB_TOKEN","requires":{"env":["GITHUB_TOKEN"]}}}
---
# github-pr
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: skillRoot}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("skill refresh error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	builtins.ApplyBindings(BuiltinsBindings{SkillService: svc})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-skill-inspect",
		Name: "skill.inspect",
		Input: map[string]any{
			"name": "github-pr",
		},
	}})
	if err != nil {
		t.Fatalf("skill.inspect error = %v", err)
	}

	var payload struct {
		Found    bool `json:"found"`
		Eligible bool `json:"eligible"`
		Ready    bool `json:"ready"`
		Checks   []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
			Name   string `json:"name"`
		} `json:"checks"`
		Actions []string `json:"next_actions"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if !payload.Found {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Eligible || payload.Ready {
		t.Fatalf("skill should not be ready without env: %+v", payload)
	}
	foundMissingEnv := false
	for _, check := range payload.Checks {
		if check.Kind == "env" && check.Name == "GITHUB_TOKEN" && check.Status == "missing" {
			foundMissingEnv = true
		}
	}
	if !foundMissingEnv {
		t.Fatalf("expected missing env check: %+v", payload.Checks)
	}
	if len(payload.Actions) == 0 {
		t.Fatalf("expected next actions: %+v", payload)
	}
}
