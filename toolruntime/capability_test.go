package toolruntime

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestDormantGroupsReturnsMissingDependencyGroups(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	reg := NewLayer2Registry(Layer2Config{
		Root: t.TempDir(),
	})
	dormant := reg.DormantGroups()

	if len(dormant) == 0 {
		t.Fatal("expected at least one dormant group")
	}

	// Check structure of first dormant group.
	dg := dormant[0]
	if dg.Name == "" {
		t.Fatal("dormant group has empty name")
	}
	if dg.ToolCount == 0 {
		t.Fatalf("dormant group %q has 0 tools", dg.Name)
	}
	if len(dg.ToolNames) != dg.ToolCount {
		t.Fatalf("dormant group %q: ToolNames len %d != ToolCount %d", dg.Name, len(dg.ToolNames), dg.ToolCount)
	}
	if dg.InstallHint == "" {
		t.Fatalf("dormant group %q has empty InstallHint", dg.Name)
	}
}

func TestDormantGroupsExcludeDisabledGroups(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	reg := NewLayer2Registry(Layer2Config{
		Root:           t.TempDir(),
		DisabledGroups: map[string]bool{"search": true},
	})
	for _, dg := range reg.DormantGroups() {
		if dg.Name == "search" {
			t.Fatal("disabled search group should not surface as dormant")
		}
	}
}

func TestBuildCapabilityReport(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	layer2 := NewLayer2Registry(Layer2Config{
		Root:           root,
		DisabledGroups: map[string]bool{"search": true},
	})

	report := BuildCapabilityReport(builtins, layer2)

	// Layer 1 has 87 tools (81 original + 6 session/memory moved from L2).
	if report.ActiveCount < 87 {
		t.Fatalf("expected ActiveCount >= 87, got %d", report.ActiveCount)
	}
	if report.DormantCount == 0 {
		t.Fatal("expected some dormant tools when dependencies are missing")
	}
	if len(report.ActiveTools) == 0 {
		t.Fatal("ActiveTools should not be empty")
	}
	for _, name := range report.ActiveTools {
		if name == "net.serve" {
			t.Fatal("hidden builtin net.serve should not surface in capability report")
		}
	}
}

func TestBuildToolPromptNilLayer2(t *testing.T) {
	t.Parallel()
	result := BuildToolPrompt(nil, nil)
	if result != "" {
		t.Fatalf("expected empty string for nil layer2, got %q", result)
	}
}

func TestBuildToolPromptContainsDormantInfo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	layer2 := NewLayer2Registry(Layer2Config{Root: root})

	prompt := BuildToolPrompt(builtins, layer2)

	// Should contain dormant section if any group is dormant.
	dormant := layer2.DormantGroups()
	if len(dormant) > 0 {
		if !strings.Contains(prompt, "Dormant Tools") {
			t.Fatal("prompt should contain 'Dormant Tools' header")
		}
		if !strings.Contains(prompt, "`skill.ensure`") {
			t.Fatal("prompt should direct recovery through skill.ensure")
		}
		if strings.Contains(prompt, "pkg.install(") || strings.Contains(prompt, "env.refresh()") {
			t.Fatal("prompt should not recommend pkg.install or env.refresh directly")
		}
		if !strings.Contains(prompt, "Capability summary:") {
			t.Fatal("prompt should contain capability summary")
		}
	}
}

func TestBuildToolPromptOmitsDisabledGroups(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	layer2 := NewLayer2Registry(Layer2Config{
		Root:           root,
		DisabledGroups: map[string]bool{"search": true},
	})

	prompt := BuildToolPrompt(builtins, layer2)
	if strings.Contains(prompt, "**search**") {
		t.Fatalf("disabled group should not appear in dormant prompt: %q", prompt)
	}
}

func TestEnvProbeIncludesDormantGroups(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	layer2 := NewLayer2Registry(Layer2Config{Root: root})
	builtins.ApplyBindings(BuiltinsBindings{Layer2: layer2})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "env.probe", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	content := results[0].Content

	var probeResult map[string]any
	if err := json.Unmarshal([]byte(content), &probeResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have dormant_groups key when layer2 is wired.
	if _, ok := probeResult["dormant_groups"]; !ok {
		t.Fatal("env.probe result should contain dormant_groups when layer2 is wired")
	}
	if _, ok := probeResult["layer2_active_tools"]; !ok {
		t.Fatal("env.probe result should contain layer2_active_tools when layer2 is wired")
	}
}

func TestEnvProbeWithoutLayer2OmitsDormantGroups(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	// Not calling SetLayer2 — layer2 is nil.

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "env.probe", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	content := results[0].Content

	var probeResult map[string]any
	if err := json.Unmarshal([]byte(content), &probeResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// dormant_groups should be absent (nil) when layer2 is not wired.
	if dg, ok := probeResult["dormant_groups"]; ok && dg != nil {
		t.Fatalf("env.probe result should not contain dormant_groups without layer2, got %v", dg)
	}
}

func TestEnvRefreshCallsLayer2Probe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	layer2 := NewLayer2Registry(Layer2Config{Root: root})
	builtins.ApplyBindings(BuiltinsBindings{Layer2: layer2})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "env.refresh", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	content := results[0].Content

	var refreshResult map[string]any
	if err := json.Unmarshal([]byte(content), &refreshResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have newly_active, newly_dormant, groups keys.
	if _, ok := refreshResult["groups"]; !ok {
		t.Fatal("env.refresh should include groups")
	}
	if _, ok := refreshResult["summary"]; !ok {
		t.Fatal("env.refresh should include summary")
	}

	// groups should be an array with group entries.
	groups, ok := refreshResult["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatal("env.refresh groups should be a non-empty array")
	}

	// Each group entry should have group, active, tools fields.
	first, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatal("group entry should be an object")
	}
	if _, ok := first["group"]; !ok {
		t.Fatal("group entry should have 'group' field")
	}
	if _, ok := first["active"]; !ok {
		t.Fatal("group entry should have 'active' field")
	}
	if _, ok := first["tools"]; !ok {
		t.Fatal("group entry should have 'tools' field")
	}
}

func TestEnvRefreshWithoutLayer2StillWorks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "env.refresh", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	content := results[0].Content

	var refreshResult map[string]any
	if err := json.Unmarshal([]byte(content), &refreshResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// summary should still be present.
	if _, ok := refreshResult["summary"]; !ok {
		t.Fatal("env.refresh without layer2 should still include summary")
	}
}

func TestLayer2DisabledGroupsNeverActivate(t *testing.T) {
	t.Parallel()

	// Disable the "pkg" group which normally activates (no required bins).
	reg := NewLayer2Registry(Layer2Config{
		Root:           t.TempDir(),
		DisabledGroups: map[string]bool{"pkg": true},
	})

	// Verify pkg group is inactive despite having no required bins.
	for _, gs := range reg.GroupStatuses() {
		if gs.Name == "pkg" {
			if gs.Active {
				t.Fatal("pkg group should be inactive when disabled by config")
			}
			return
		}
	}
	t.Fatal("pkg group not found in statuses")
}

func TestLayer2DisabledGroupNotInDefinitions(t *testing.T) {
	t.Parallel()

	reg := NewLayer2Registry(Layer2Config{
		Root:           t.TempDir(),
		DisabledGroups: map[string]bool{"pkg": true},
	})

	defs := reg.ToolDefinitions(nil)
	foundBlockedPkg := false
	for _, d := range defs {
		if strings.HasPrefix(d.Name, "pkg.") {
			foundBlockedPkg = true
			if d.Availability.Status != agent.AvailabilityBlocked {
				t.Fatalf("disabled pkg tool %q availability = %q", d.Name, d.Availability.Status)
			}
		}
	}
	if !foundBlockedPkg {
		t.Fatal("expected disabled pkg tools to stay discoverable in the unified catalog")
	}
}

func TestDormantGroupsAreSorted(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	reg := NewLayer2Registry(Layer2Config{
		Root: t.TempDir(),
	})
	dormant := reg.DormantGroups()
	names := make([]string, 0, len(dormant))
	for _, group := range dormant {
		names = append(names, group.Name)
		if !sort.StringsAreSorted(group.ToolNames) {
			t.Fatalf("dormant group %q tool names are not sorted: %v", group.Name, group.ToolNames)
		}
		if !sort.StringsAreSorted(group.MissingBins) {
			t.Fatalf("dormant group %q missing bins are not sorted: %v", group.Name, group.MissingBins)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("dormant groups are not sorted: %v", names)
	}
}
