package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillMarkdownRendersFrontMatterAndTLDR(t *testing.T) {
	t.Parallel()

	skill := Skill{
		Name:        "release-helper",
		Description: "Run the release checks in one place.",
		TLDR:        "Use this skill when you need the release checklist.",
		Body:        "## Usage\n\n```bash\nmake release-check\n```\n",
	}

	markdown := skill.Markdown()
	for _, token := range []string{
		`name: "release-helper"`,
		`description: "Run the release checks in one place."`,
		"# Release Helper",
		"## TL;DR",
		"make release-check",
	} {
		if !strings.Contains(markdown, token) {
			t.Fatalf("Markdown() missing %q in %q", token, markdown)
		}
	}
}

func TestSkillWriteToDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skill := Skill{
		Name:        "ops-runbook",
		Description: "Starter runbook.",
	}

	if err := skill.WriteToDir(root); err != nil {
		t.Fatalf("WriteToDir() error = %v", err)
	}

	path := filepath.Join(root, "ops-runbook", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if !strings.Contains(string(data), "Starter runbook.") {
		t.Fatalf("file content = %q", string(data))
	}
}
