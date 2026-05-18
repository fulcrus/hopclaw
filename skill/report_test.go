package skill

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRuntimeReportIncludesStructuredChecksAndActions(t *testing.T) {
	t.Parallel()

	pkg := &SkillPackage{
		ID: "skill-1",
		Prompt: PromptSkill{
			Name:        "github-pr",
			Description: "Review pull requests",
			Location:    "workspace:github-pr",
		},
		Kind:   SkillKindExecutable,
		Status: StatusReady,
		Trust:  TrustCommunity,
		Source: SkillSource{
			Kind:     SourceWorkspace,
			Root:     "/tmp/skills",
			Dir:      "/tmp/skills/github-pr",
			NameHint: "github-pr",
		},
		OpenClaw: OpenClawMetadata{
			PrimaryEnv: "GITHUB_TOKEN",
			Requires: RequiresSpec{
				Bins: []string{"gh"},
				Env:  []string{"GITHUB_TOKEN"},
			},
			Install: []InstallSpec{{
				ID:      "gh-cli",
				Kind:    "brew",
				Formula: "gh",
				Bins:    []string{"gh"},
			}},
		},
		ToolManifests: []ToolManifest{{
			Name:             "github.pr.review",
			SideEffectClass:  "external_write",
			RequiresApproval: true,
			ExecutionKey:     "github:pr:{number}",
		}},
	}

	report := BuildRuntimeReport(pkg, RuntimeContext{
		GOOS: "linux",
	}, Evaluator{
		LookPath: func(name string) (string, error) {
			return "", context.DeadlineExceeded
		},
	})

	if !report.Found || !report.Loaded {
		t.Fatalf("report = %+v", report)
	}
	if report.Eligible || report.Ready {
		t.Fatalf("report should not be ready: %+v", report)
	}
	if len(report.Checks) < 2 {
		t.Fatalf("expected structured checks, got %+v", report.Checks)
	}
	if !containsNextAction(report.NextActions, "brew install gh") {
		t.Fatalf("expected brew install hint in next actions: %+v", report.NextActions)
	}
	if !containsNextAction(report.NextActions, "set env GITHUB_TOKEN") {
		t.Fatalf("expected env hint in next actions: %+v", report.NextActions)
	}
}

func TestServiceInspectSourceBuildsReport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "skills", "demo")
	mustWriteSkill(t, dir, "demo", "demo skill")

	svc := NewService(ServiceConfig{})
	report, err := svc.InspectSource(context.Background(), dir, SourceWorkspace, RuntimeContext{GOOS: "linux"})
	if err != nil {
		t.Fatalf("InspectSource() error = %v", err)
	}
	if !report.Found {
		t.Fatalf("expected found report: %+v", report)
	}
	if report.Loaded {
		t.Fatalf("InspectSource should report loaded=false for source inspection: %+v", report)
	}
	if report.Name != "demo" {
		t.Fatalf("report.Name = %q", report.Name)
	}
}

func containsNextAction(actions []string, fragment string) bool {
	for _, action := range actions {
		if strings.Contains(strings.ToLower(action), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}
