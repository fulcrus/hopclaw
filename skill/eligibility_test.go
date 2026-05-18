package skill

import (
	"fmt"
	"os"
	"testing"
)

func TestEvaluateEligibleWithNoDeps(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "simple"},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if !result.Eligible {
		t.Fatalf("expected eligible, got reasons %v", result.Reasons)
	}
	if result.Always {
		t.Fatal("Always should be false for non-always skills")
	}
}

func TestEvaluateAlwaysEligible(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt:   PromptSkill{Name: "always-on"},
		OpenClaw: OpenClawMetadata{Always: true},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if !result.Eligible {
		t.Fatalf("always-on should be eligible, got reasons %v", result.Reasons)
	}
	if !result.Always {
		t.Fatal("Always should be true")
	}
}

func TestEvaluateAlwaysSkipsBinChecks(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "always-skip-bins"},
		OpenClaw: OpenClawMetadata{
			Always:   true,
			Requires: RequiresSpec{Bins: []string{"nonexistent-binary"}},
		},
	}
	eval := Evaluator{
		LookPath: func(string) (string, error) {
			return "", &os.PathError{Op: "lookpath", Path: "x", Err: os.ErrNotExist}
		},
	}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if !result.Eligible {
		t.Fatalf("always-on should skip bin checks, got reasons %v", result.Reasons)
	}
}

func TestEvaluateIneligibleByOS(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt:   PromptSkill{Name: "macos-only"},
		OpenClaw: OpenClawMetadata{OS: []string{"darwin"}},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if result.Eligible {
		t.Fatal("expected ineligible on linux for darwin-only skill")
	}
	if len(result.Reasons) == 0 {
		t.Fatal("expected reason for ineligibility")
	}
	foundReason := false
	for _, r := range result.Reasons {
		if r == "unsupported OS" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("expected 'unsupported OS' reason, got %v", result.Reasons)
	}
}

func TestEvaluateEligibleByOS(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt:   PromptSkill{Name: "linux-ok"},
		OpenClaw: OpenClawMetadata{OS: []string{"linux", "darwin"}},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if !result.Eligible {
		t.Fatalf("expected eligible on linux, got reasons %v", result.Reasons)
	}
}

func TestEvaluateIneligibleByMissingBin(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "needs-git"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Bins: []string{"git", "gh"}},
		},
	}
	eval := Evaluator{
		LookPath: func(file string) (string, error) {
			if file == "git" {
				return "/usr/bin/git", nil
			}
			return "", fmt.Errorf("not found: %s", file)
		},
	}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if result.Eligible {
		t.Fatal("expected ineligible when gh is missing")
	}
	foundReason := false
	for _, r := range result.Reasons {
		if r == "missing binary: gh" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("expected 'missing binary: gh' reason, got %v", result.Reasons)
	}
}

func TestEvaluateAnyBinsEligibleWhenOnePresent(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "any-http"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{AnyBins: []string{"curl", "wget", "httpie"}},
		},
	}
	eval := Evaluator{
		LookPath: func(file string) (string, error) {
			if file == "wget" {
				return "/usr/bin/wget", nil
			}
			return "", fmt.Errorf("not found: %s", file)
		},
	}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if !result.Eligible {
		t.Fatalf("expected eligible when wget is present, got reasons %v", result.Reasons)
	}
}

func TestEvaluateAnyBinsIneligibleWhenNonePresent(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "any-http"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{AnyBins: []string{"curl", "wget"}},
		},
	}
	eval := Evaluator{
		LookPath: func(string) (string, error) {
			return "", fmt.Errorf("not found")
		},
	}
	result := eval.Evaluate(pkg, RuntimeContext{GOOS: "linux"})

	if result.Eligible {
		t.Fatal("expected ineligible when no anyBins present")
	}
}

func TestEvaluateIneligibleByMissingEnv(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "needs-token"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Env: []string{"API_TOKEN"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		SecretPresence: map[string]SecretStatus{
			"OTHER_VAR": {Resolved: true, Source: "runtime_env"},
		},
	})

	if result.Eligible {
		t.Fatal("expected ineligible when API_TOKEN is missing")
	}
}

func TestEvaluateEligibleByEnvFromRuntime(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "needs-token"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Env: []string{"API_TOKEN"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		SecretPresence: map[string]SecretStatus{
			"API_TOKEN": {Resolved: true, Source: "runtime_env"},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible when API_TOKEN is set, got reasons %v", result.Reasons)
	}
}

func TestEvaluateEnvInjectionViaManagedAPIKey(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "managed-tool"},
		OpenClaw: OpenClawMetadata{
			SkillKey:   "dev.managed",
			PrimaryEnv: "TOOL_API_KEY",
			Requires:   RequiresSpec{Env: []string{"TOOL_API_KEY"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"dev.managed": {InjectedEnv: map[string]SecretStatus{
				"TOOL_API_KEY": {Resolved: true, Source: "managed"},
			}},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible via managed injection, got reasons %v", result.Reasons)
	}
	if !containsString(result.InjectedEnv, "TOOL_API_KEY") {
		t.Fatalf("InjectedEnv = %v", result.InjectedEnv)
	}
}

func TestEvaluateEnvInjectionViaManagedEnvMap(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "env-map"},
		OpenClaw: OpenClawMetadata{
			SkillKey: "env.map",
			Requires: RequiresSpec{Env: []string{"CUSTOM_VAR"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"env.map": {InjectedEnv: map[string]SecretStatus{
				"CUSTOM_VAR": {Resolved: true, Source: "managed"},
			}},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible via managed env map, got reasons %v", result.Reasons)
	}
	if !containsString(result.InjectedEnv, "CUSTOM_VAR") {
		t.Fatalf("InjectedEnv = %v", result.InjectedEnv)
	}
}

func TestEvaluateDisabledByManagedConfig(t *testing.T) {
	t.Parallel()

	disabled := false
	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "disabled"},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"disabled": {Enabled: &disabled},
		},
	})

	if result.Eligible {
		t.Fatal("expected ineligible when disabled by managed config")
	}
	foundReason := false
	for _, r := range result.Reasons {
		if r == "disabled by managed config" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("expected 'disabled by managed config' reason, got %v", result.Reasons)
	}
}

func TestEvaluateEnabledByManagedConfig(t *testing.T) {
	t.Parallel()

	enabled := true
	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "enabled"},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"enabled": {Enabled: &enabled},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible when enabled by managed config, got reasons %v", result.Reasons)
	}
}

func TestEvaluateIneligibleByMissingConfig(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "needs-config"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Config: []string{"feature.enabled"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		ConfigTruth: map[string]ConfigStatus{
			"feature.disabled": {Present: true, Truthy: true, Source: "runtime_config"},
		},
	})

	if result.Eligible {
		t.Fatal("expected ineligible when config path is missing")
	}
}

func TestEvaluateEligibleByConfig(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "has-config"},
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Config: []string{"feature.enabled"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		ConfigTruth: map[string]ConfigStatus{
			"feature.enabled": {Present: true, Truthy: true, Source: "runtime_config"},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible when config path is present, got reasons %v", result.Reasons)
	}
}

func TestEvaluateConfigFromManagedEntryConfig(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "managed-config"},
		OpenClaw: OpenClawMetadata{
			SkillKey: "cfg.tool",
			Requires: RequiresSpec{Config: []string{"auth.enabled"}},
		},
	}
	eval := Evaluator{}
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"cfg.tool": {ConfigTruth: map[string]ConfigStatus{
				"auth.enabled": {Present: true, Truthy: true, Source: "managed"},
			}},
		},
	})

	if !result.Eligible {
		t.Fatalf("expected eligible via managed config, got reasons %v", result.Reasons)
	}
}

func TestTruthyAtPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		root map[string]any
		path string
		want bool
	}{
		{"nil root", nil, "a", false},
		{"empty path", map[string]any{"a": true}, "", false},
		{"bool true", map[string]any{"a": true}, "a", true},
		{"bool false", map[string]any{"a": false}, "a", false},
		{"string non-empty", map[string]any{"a": "yes"}, "a", true},
		{"string empty", map[string]any{"a": ""}, "a", false},
		{"int non-zero", map[string]any{"a": 42}, "a", true},
		{"int zero", map[string]any{"a": 0}, "a", false},
		{"int64 non-zero", map[string]any{"a": int64(1)}, "a", true},
		{"int64 zero", map[string]any{"a": int64(0)}, "a", false},
		{"float64 non-zero", map[string]any{"a": 3.14}, "a", true},
		{"float64 zero", map[string]any{"a": float64(0)}, "a", false},
		{"nil value", map[string]any{"a": nil}, "a", false},
		{"nested path", map[string]any{"a": map[string]any{"b": map[string]any{"c": true}}}, "a.b.c", true},
		{"nested missing", map[string]any{"a": map[string]any{"b": true}}, "a.b.c", false},
		{"non-map intermediate", map[string]any{"a": "string"}, "a.b", false},
		{"map value", map[string]any{"a": map[string]any{"x": 1}}, "a", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truthyAtPath(tt.root, tt.path)
			if got != tt.want {
				t.Fatalf("truthyAtPath(%v, %q) = %v, want %v", tt.root, tt.path, got, tt.want)
			}
		})
	}
}

func TestEvaluatorEnrichRuntimeContextLoadsMissingSecretPresence(t *testing.T) {
	t.Parallel()

	eval := Evaluator{
		SecretPresence: func(keys []string) map[string]SecretStatus {
			out := make(map[string]SecretStatus, len(keys))
			for _, key := range keys {
				out[key] = SecretStatus{Resolved: key == "KEY", Source: "runtime_env"}
			}
			return out
		},
	}
	ctx := eval.EnrichRuntimeContext(RuntimeContext{GOOS: "linux"}, []*SkillPackage{{
		OpenClaw: OpenClawMetadata{
			Requires: RequiresSpec{Env: []string{"KEY"}},
		},
	}})
	status, ok := ctx.SecretPresence["KEY"]
	if !ok || !status.Resolved {
		t.Fatalf("secret presence = %#v", ctx.SecretPresence)
	}
}

func TestEvaluateUsesConfigKeyOverName(t *testing.T) {
	t.Parallel()

	disabled := false
	pkg := &SkillPackage{
		Prompt: PromptSkill{Name: "display-name"},
		OpenClaw: OpenClawMetadata{
			SkillKey: "actual.key",
		},
	}
	eval := Evaluator{}

	// Disable via skillKey, not name.
	result := eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"actual.key": {Enabled: &disabled},
		},
	})
	if result.Eligible {
		t.Fatal("expected ineligible when disabled via skillKey")
	}

	// Disabling via name should NOT work when skillKey is set.
	result = eval.Evaluate(pkg, RuntimeContext{
		GOOS: "linux",
		Managed: map[string]ManagedEntry{
			"display-name": {Enabled: &disabled},
		},
	})
	if !result.Eligible {
		t.Fatal("expected eligible because managed entry uses wrong key")
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
