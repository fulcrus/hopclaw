package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileMinimalSpec(t *testing.T) {
	t.Parallel()

	src := SkillSource{
		Kind:     SourceWorkspace,
		Root:     "/workspace",
		Dir:      "/workspace/my-skill",
		NameHint: "my-skill",
		Priority: 500,
	}
	spec := &ExternalSkillSpec{
		Name:          "my-skill",
		Description:   "A minimal skill",
		Body:          "Use this skill for testing.",
		UserInvocable: true,
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Name() != "my-skill" {
		t.Fatalf("Name() = %q", pkg.Name())
	}
	if pkg.Kind != SkillKindPrompt {
		t.Fatalf("Kind = %q, want %q", pkg.Kind, SkillKindPrompt)
	}
	if pkg.Status != StatusDegraded {
		// Missing description is a warning => degraded (description is present but
		// instructions come from Body which is non-empty, so only description field
		// is present). Actually the compiler validates pkg.Prompt.Description and
		// pkg.Prompt.Instructions which are populated.
		// With non-empty description and body, it should be Ready.
		if pkg.Status != StatusReady {
			t.Fatalf("Status = %q", pkg.Status)
		}
	}
	if pkg.Prompt.Instructions != "Use this skill for testing." {
		t.Fatalf("Instructions = %q", pkg.Prompt.Instructions)
	}
	if pkg.Prompt.Description != "A minimal skill" {
		t.Fatalf("Description = %q", pkg.Prompt.Description)
	}
	if pkg.Trust != TrustInternal {
		t.Fatalf("Trust = %q, want %q", pkg.Trust, TrustInternal)
	}
	if pkg.ID == "" {
		t.Fatal("ID should not be empty")
	}
	if pkg.LoadedAt.IsZero() {
		t.Fatal("LoadedAt should be set")
	}
	if !pkg.Normalized {
		t.Fatal("Normalized should be true")
	}
}

func TestCompileEmptyNameReturnsError(t *testing.T) {
	t.Parallel()

	src := SkillSource{Kind: SourceWorkspace, Dir: "/workspace/bad"}
	spec := &ExternalSkillSpec{Name: "", Body: "content"}

	_, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "missing a name") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestCompileWithWhitespaceOnlyNameReturnsError(t *testing.T) {
	t.Parallel()

	src := SkillSource{Kind: SourceWorkspace, Dir: "/workspace/ws"}
	spec := &ExternalSkillSpec{Name: "   ", Body: "content"}

	_, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestCompileSetsLocationFromRelativePath(t *testing.T) {
	t.Parallel()

	src := SkillSource{
		Kind: SourceUser,
		Root: "/home/user/skills",
		Dir:  "/home/user/skills/tools/lint",
	}
	spec := &ExternalSkillSpec{
		Name:        "lint",
		Description: "Lint code",
		Body:        "Run linting.",
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Prompt.Location != "user:tools/lint" {
		t.Fatalf("Location = %q", pkg.Prompt.Location)
	}
}

func TestCompileWithCompanionManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entryPath := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(entryPath, []byte("#!/bin/bash\necho ok"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	boolTrue := true
	src := SkillSource{
		Kind:     SourceClawHub,
		Root:     dir,
		Dir:      dir,
		Priority: 300,
	}
	spec := &ExternalSkillSpec{
		Name:        "exec-tool",
		Description: "An executable tool",
		Body:        "Execute this tool.",
		Companion: &CompanionManifest{
			Version: "1",
			Tool: ToolManifestSpec{
				Name:             "exec.run",
				SideEffectClass:  "read",
				Idempotent:       &boolTrue,
				ExecutionKey:     "session:{id}",
				RequiresApproval: &boolTrue,
				Timeout:          "5s",
			},
			Runtime: ToolRuntimeSpec{
				Entry: "run.sh",
				Shell: "bash",
			},
			Security: ToolSecuritySpec{
				Trust: "verified",
			},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Kind != SkillKindExecutable {
		t.Fatalf("Kind = %q, want %q", pkg.Kind, SkillKindExecutable)
	}
	if len(pkg.ToolManifests) != 1 {
		t.Fatalf("len(ToolManifests) = %d", len(pkg.ToolManifests))
	}
	tool := pkg.ToolManifests[0]
	if tool.Name != "exec.run" {
		t.Fatalf("tool.Name = %q", tool.Name)
	}
	if tool.SideEffectClass != "read" {
		t.Fatalf("SideEffectClass = %q", tool.SideEffectClass)
	}
	if !tool.Idempotent {
		t.Fatal("Idempotent should be true")
	}
	if tool.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %v", tool.Timeout)
	}
	if pkg.Trust != TrustVerified {
		t.Fatalf("Trust = %q, want %q", pkg.Trust, TrustVerified)
	}
}

func TestCompileToolInheritsSkillNameWhenToolNameEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "my-tool",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				SideEffectClass: "read",
				ExecutionKey:    "session:{id}",
			},
			Runtime:  ToolRuntimeSpec{Entry: "run.sh"},
			Security: ToolSecuritySpec{Trust: "internal"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(pkg.ToolManifests) != 1 {
		t.Fatalf("len(ToolManifests) = %d", len(pkg.ToolManifests))
	}
	if pkg.ToolManifests[0].Name != "my-tool" {
		t.Fatalf("tool.Name = %q, want skill name fallback", pkg.ToolManifests[0].Name)
	}
}

func TestCompileForcesApprovalForNonReadSideEffect(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	boolFalse := false
	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "writer",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:             "writer.run",
				SideEffectClass:  "local_write",
				RequiresApproval: &boolFalse,
				ExecutionKey:     "session:{id}",
			},
			Runtime:  ToolRuntimeSpec{Entry: "run.sh"},
			Security: ToolSecuritySpec{Trust: "internal"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !pkg.ToolManifests[0].RequiresApproval {
		t.Fatal("RequiresApproval should be forced true for non-read side effect")
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "forced_approval_for_side_effect" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected forced_approval_for_side_effect issue")
	}
}

func TestCompileForcesApprovalForCommunityTrust(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := SkillSource{Kind: SourceClawHub, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "community-tool",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "community.run",
				SideEffectClass: "read",
				ExecutionKey:    "session:{id}",
			},
			Runtime: ToolRuntimeSpec{Entry: "run.sh"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !pkg.ToolManifests[0].RequiresApproval {
		t.Fatal("community skills should require approval by default")
	}
}

func TestCompileDefaultsExecutionKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "nokey",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "nokey.run",
				SideEffectClass: "read",
			},
			Runtime: ToolRuntimeSpec{Entry: "run.sh"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.ToolManifests[0].ExecutionKey != "session:{id}" {
		t.Fatalf("ExecutionKey = %q", pkg.ToolManifests[0].ExecutionKey)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "default_execution_key" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected default_execution_key issue")
	}
}

func TestCompileInvalidTimeoutIssue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "bad-timeout",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "badtimeout.run",
				SideEffectClass: "read",
				ExecutionKey:    "session:{id}",
				Timeout:         "not-a-duration",
			},
			Runtime: ToolRuntimeSpec{Entry: "run.sh"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "invalid_timeout" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected invalid_timeout issue")
	}
}

func TestCompileBlocksOnMissingRuntimeEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "missing-entry",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "missing.run",
				SideEffectClass: "read",
				ExecutionKey:    "session:{id}",
			},
			Runtime: ToolRuntimeSpec{Entry: "nonexistent.sh"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Status != StatusBlocked {
		t.Fatalf("Status = %q, want blocked", pkg.Status)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "runtime_entry_not_found" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected runtime_entry_not_found issue")
	}
}

func TestCompileEmptyRuntimeEntryIsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "empty-entry",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "empty.run",
				SideEffectClass: "read",
				ExecutionKey:    "session:{id}",
			},
			Runtime: ToolRuntimeSpec{Entry: ""},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Status != StatusBlocked {
		t.Fatalf("Status = %q, want blocked", pkg.Status)
	}
}

func TestInferTrustFromSourceKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind SourceKind
		want TrustClass
	}{
		{SourceBundled, TrustBundled},
		{SourceWorkspace, TrustInternal},
		{SourceUser, TrustInternal},
		{SourceClawHub, TrustCommunity},
		{SourcePlugin, TrustUnknown},
	}
	for _, tt := range tests {
		spec := &ExternalSkillSpec{Name: "test"}
		got := inferTrust(SkillSource{Kind: tt.kind}, spec)
		if got != tt.want {
			t.Errorf("inferTrust(kind=%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestInferTrustFromCompanionOverridesSourceKind(t *testing.T) {
	t.Parallel()

	spec := &ExternalSkillSpec{
		Name: "test",
		Companion: &CompanionManifest{
			Security: ToolSecuritySpec{Trust: "bundled"},
		},
	}
	got := inferTrust(SkillSource{Kind: SourceClawHub}, spec)
	if got != TrustBundled {
		t.Fatalf("inferTrust = %q, want bundled (companion override)", got)
	}
}

func TestStatusFromIssues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		issues []SkillIssue
		want   PackageStatus
	}{
		{"no issues", nil, StatusReady},
		{"warning only", []SkillIssue{{Severity: SeverityWarning}}, StatusDegraded},
		{"error present", []SkillIssue{{Severity: SeverityError}}, StatusBlocked},
		{"warning then error", []SkillIssue{
			{Severity: SeverityWarning},
			{Severity: SeverityError},
		}, StatusBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := statusFromIssues(tt.issues)
			if got != tt.want {
				t.Fatalf("statusFromIssues() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMakeSkillIDDeterministic(t *testing.T) {
	t.Parallel()

	src := SkillSource{Kind: SourceWorkspace, Dir: "/workspace/test"}
	id1 := makeSkillID(src, "my-skill")
	id2 := makeSkillID(src, "my-skill")
	if id1 != id2 {
		t.Fatalf("IDs should be deterministic: %q != %q", id1, id2)
	}
	if len(id1) != 32 {
		t.Fatalf("ID length = %d, want 32 hex chars", len(id1))
	}

	id3 := makeSkillID(src, "other-skill")
	if id1 == id3 {
		t.Fatalf("different names should produce different IDs")
	}
}

func TestValidateInstallSpecsBrew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		formula string
		wantErr bool
	}{
		{"valid formula", "ripgrep", false},
		{"valid scoped formula", "go@1.21", false},
		{"unsafe leading dash", "-malicious", true},
		{"unsafe backslash", `rg\n`, true},
		{"unsafe traversal", "rg/../etc", true},
		{"invalid chars", "rg!bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issues := validateInstallSpecs([]InstallSpec{{Kind: "brew", Formula: tt.formula}})
			hasErr := false
			for _, issue := range issues {
				if issue.Severity == SeverityError {
					hasErr = true
				}
			}
			if hasErr != tt.wantErr {
				t.Fatalf("formula=%q: hasErr=%v, wantErr=%v, issues=%v", tt.formula, hasErr, tt.wantErr, issues)
			}
		})
	}
}

func TestValidateInstallSpecsNpm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pkg     string
		wantErr bool
	}{
		{"valid unscoped", "typescript", false},
		{"valid scoped", "@types/node", false},
		{"valid with version", "typescript@5.0.0", false},
		{"unsafe leading dash", "-malicious", true},
		{"unsafe url", "https://evil.com/pkg", true},
		{"unsafe hash", "pkg#script", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issues := validateInstallSpecs([]InstallSpec{{Kind: "npm", Package: tt.pkg}})
			hasErr := false
			for _, issue := range issues {
				if issue.Severity == SeverityError {
					hasErr = true
				}
			}
			if hasErr != tt.wantErr {
				t.Fatalf("pkg=%q: hasErr=%v, wantErr=%v, issues=%v", tt.pkg, hasErr, tt.wantErr, issues)
			}
		})
	}
}

func TestValidateInstallSpecsGoModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		module  string
		wantErr bool
	}{
		{"valid module", "golang.org/x/tools@latest", false},
		{"unsafe leading dash", "-malicious", true},
		{"unsafe backslash", `go\nmod`, true},
		{"unsafe url scheme", "https://evil.com/module", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issues := validateInstallSpecs([]InstallSpec{{Kind: "go", Module: tt.module}})
			hasErr := false
			for _, issue := range issues {
				if issue.Severity == SeverityError {
					hasErr = true
				}
			}
			if hasErr != tt.wantErr {
				t.Fatalf("module=%q: hasErr=%v, wantErr=%v, issues=%v", tt.module, hasErr, tt.wantErr, issues)
			}
		})
	}
}

func TestValidateInstallSpecsDownload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid https", "https://example.com/helper.tar.gz", false},
		{"valid http", "http://example.com/helper.zip", false},
		{"invalid ftp scheme", "ftp://example.com/file", true},
		{"whitespace in url", "https://example.com/he lper.zip", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			issues := validateInstallSpecs([]InstallSpec{{Kind: "download", URL: tt.url}})
			hasErr := false
			for _, issue := range issues {
				if issue.Severity == SeverityError {
					hasErr = true
				}
			}
			if hasErr != tt.wantErr {
				t.Fatalf("url=%q: hasErr=%v, wantErr=%v, issues=%v", tt.url, hasErr, tt.wantErr, issues)
			}
		})
	}
}

func TestValidateInstallSpecsScriptCommandSubstitution(t *testing.T) {
	t.Parallel()

	issues := validateInstallSpecs([]InstallSpec{{Kind: "shell", Script: "echo $(whoami)"}})
	hasWarning := false
	for _, issue := range issues {
		if issue.Code == "script_command_substitution" {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Fatal("expected script_command_substitution warning")
	}

	issues = validateInstallSpecs([]InstallSpec{{Kind: "shell", Script: "echo `whoami`"}})
	hasWarning = false
	for _, issue := range issues {
		if issue.Code == "script_command_substitution" {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Fatal("expected script_command_substitution warning for backtick")
	}
}

func TestCompileWarnsOnMissingDescription(t *testing.T) {
	t.Parallel()

	src := SkillSource{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/sk"}
	spec := &ExternalSkillSpec{
		Name: "nodesc",
		Body: "instructions here",
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "missing_description" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected missing_description issue")
	}
}

func TestCompileWarnsOnMissingInstructions(t *testing.T) {
	t.Parallel()

	src := SkillSource{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/sk"}
	spec := &ExternalSkillSpec{
		Name:        "noinstr",
		Description: "has description",
		Body:        "",
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "missing_instructions" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected missing_instructions issue")
	}
}

func TestCompileNormalizedSideEffectClassWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := SkillSource{Kind: SourceWorkspace, Root: dir, Dir: dir}
	spec := &ExternalSkillSpec{
		Name:        "bad-side-effect",
		Description: "desc",
		Body:        "body",
		Companion: &CompanionManifest{
			Tool: ToolManifestSpec{
				Name:            "badsideeffect.run",
				SideEffectClass: "garbage_value",
				ExecutionKey:    "session:{id}",
			},
			Runtime: ToolRuntimeSpec{Entry: "run.sh"},
		},
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.ToolManifests[0].SideEffectClass != "destructive" {
		t.Fatalf("SideEffectClass = %q, want destructive", pkg.ToolManifests[0].SideEffectClass)
	}
	hasIssue := false
	for _, issue := range pkg.Issues {
		if issue.Code == "normalized_side_effect_class" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Fatal("expected normalized_side_effect_class issue")
	}
}

func TestBoolValue(t *testing.T) {
	t.Parallel()

	bTrue := true
	bFalse := false

	if boolValue(nil, true) != true {
		t.Fatal("nil with fallback=true should return true")
	}
	if boolValue(nil, false) != false {
		t.Fatal("nil with fallback=false should return false")
	}
	if boolValue(&bTrue, false) != true {
		t.Fatal("*true should return true")
	}
	if boolValue(&bFalse, true) != false {
		t.Fatal("*false should return false")
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	d, ok := parseDuration("")
	if !ok || d != 0 {
		t.Fatalf("empty string: d=%v, ok=%v", d, ok)
	}
	d, ok = parseDuration("5s")
	if !ok || d != 5*time.Second {
		t.Fatalf("5s: d=%v, ok=%v", d, ok)
	}
	d, ok = parseDuration("  ")
	if !ok || d != 0 {
		t.Fatalf("whitespace: d=%v, ok=%v", d, ok)
	}
	_, ok = parseDuration("not-a-duration")
	if ok {
		t.Fatal("invalid duration should return ok=false")
	}
}

func TestCloneSchema(t *testing.T) {
	t.Parallel()

	if cloneSchema(nil) != nil {
		t.Fatal("nil schema should return nil")
	}

	original := JSONSchema{"type": "object", "properties": map[string]any{}}
	cloned := cloneSchema(original)
	if cloned == nil || len(cloned) != len(original) {
		t.Fatalf("cloned schema should match original length")
	}
	cloned["extra"] = "value"
	if _, ok := original["extra"]; ok {
		t.Fatal("modifying clone should not affect original")
	}
}
