package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

func TestExtractProjectRuleInstructionsStripsCodeFencesAndHeadings(t *testing.T) {
	t.Parallel()

	raw := "# Project Rules\n\n" +
		"## Coding\n" +
		"- Be precise.\n" +
		"- Run targeted tests.\n\n" +
		"```bash\n" +
		"rm -rf /\n" +
		"```\n\n" +
		"## Output\n" +
		"1. Keep answers concise.\n"
	instructions := extractProjectRuleInstructions(raw)
	if len(instructions) < 3 {
		t.Fatalf("instructions = %#v, want extracted instructions", instructions)
	}
	joined := strings.Join(instructions, "\n")
	if strings.Contains(joined, "rm -rf /") {
		t.Fatalf("instructions leaked fenced code: %#v", instructions)
	}
	if !strings.Contains(joined, "Coding: Be precise.") {
		t.Fatalf("instructions = %#v, want heading-qualified bullet", instructions)
	}
	if !strings.Contains(joined, "Output: Keep answers concise.") {
		t.Fatalf("instructions = %#v, want numbered item", instructions)
	}
}

func TestLoadProjectRuleBundlesIncludesRepoAndWorkspaceRootsInPriorityOrder(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "subproject")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "AGENTS.md"), []byte("- Repo rule"), 0o644); err != nil {
		t.Fatalf("WriteFile(repo AGENTS.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "CLAUDE.md"), []byte("- Workspace rule"), 0o644); err != nil {
		t.Fatalf("WriteFile(workspace CLAUDE.md) error = %v", err)
	}

	bundles := loadProjectRuleBundles(skill.RuntimeContext{
		Git:       skill.GitContext{InRepo: true, Root: repoRoot},
		Workspace: skill.WorkspaceContext{Root: workspaceRoot},
	})
	if len(bundles) != 2 {
		t.Fatalf("len(bundles) = %d, want 2 (%#v)", len(bundles), bundles)
	}
	if bundles[0].Scope != "repo" || bundles[0].Source != "AGENTS.md" {
		t.Fatalf("bundles[0] = %#v, want repo AGENTS first", bundles[0])
	}
	if bundles[1].Scope != "workspace" || bundles[1].Source != "CLAUDE.md" {
		t.Fatalf("bundles[1] = %#v, want workspace CLAUDE second", bundles[1])
	}
}

func TestRenderProjectRulePromptIncludesConflictGuidance(t *testing.T) {
	t.Parallel()

	prompt := renderProjectRulePrompt([]RuleBundle{
		{
			Scope:        "repo",
			Source:       "AGENTS.md",
			Instructions: []string{"Use focused tests first."},
		},
		{
			Scope:        "workspace",
			Source:       "CLAUDE.md",
			Instructions: []string{"Keep final answers terse."},
		},
	})
	if !strings.Contains(prompt, "<project_rules>") {
		t.Fatalf("prompt = %q, want project_rules tag", prompt)
	}
	if !strings.Contains(prompt, "workspace-local rules override earlier repo-root rules") {
		t.Fatalf("prompt = %q, want conflict guidance", prompt)
	}
	if !strings.Contains(prompt, "Source: repo / AGENTS.md") || !strings.Contains(prompt, "Source: workspace / CLAUDE.md") {
		t.Fatalf("prompt = %q, want source labels", prompt)
	}
}
