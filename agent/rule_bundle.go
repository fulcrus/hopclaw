package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

const (
	projectRuleFileMaxBytes          = 32 * 1024
	projectRuleMaxInstructions       = 24
	projectRuleMaxInstructionChars   = 220
	projectRuleRepoPriorityBase      = 300
	projectRuleWorkspacePriorityBase = 400
)

var projectRuleListPrefixPattern = regexp.MustCompile(`^\d+[.)]\s+`)

type RuleBundle struct {
	Source       string
	Scope        string
	Priority     int
	OriginPath   string
	Instructions []string
}

type projectRuleRoot struct {
	path         string
	scope        string
	priorityBase int
}

func loadProjectRuleBundles(runtimeCtx skill.RuntimeContext) []RuleBundle {
	roots := projectRuleRoots(runtimeCtx)
	if len(roots) == 0 {
		return nil
	}
	out := make([]RuleBundle, 0, len(roots)*2)
	for _, root := range roots {
		for idx, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(root.path, name)
			bundle, ok := loadProjectRuleBundle(path, root.scope, root.priorityBase+idx*10)
			if !ok {
				continue
			}
			out = append(out, bundle)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].OriginPath < out[j].OriginPath
	})
	return out
}

func projectRuleRoots(runtimeCtx skill.RuntimeContext) []projectRuleRoot {
	seen := make(map[string]struct{}, 2)
	roots := make([]projectRuleRoot, 0, 2)
	appendRoot := func(path, scope string, priorityBase int) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		roots = append(roots, projectRuleRoot{
			path:         path,
			scope:        scope,
			priorityBase: priorityBase,
		})
	}
	appendRoot(runtimeCtx.Git.Root, "repo", projectRuleRepoPriorityBase)
	appendRoot(runtimeCtx.Workspace.Root, "workspace", projectRuleWorkspacePriorityBase)
	return roots
}

func loadProjectRuleBundle(path, scope string, priority int) (RuleBundle, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return RuleBundle{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RuleBundle{}, false
	}
	if len(data) > projectRuleFileMaxBytes {
		data = data[:projectRuleFileMaxBytes]
	}
	instructions := extractProjectRuleInstructions(string(data))
	if len(instructions) == 0 {
		return RuleBundle{}, false
	}
	return RuleBundle{
		Source:       strings.TrimSpace(filepath.Base(path)),
		Scope:        strings.TrimSpace(scope),
		Priority:     priority,
		OriginPath:   path,
		Instructions: instructions,
	}, true
}

func extractProjectRuleInstructions(raw string) []string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, projectRuleMaxInstructions)
	seen := make(map[string]struct{}, projectRuleMaxInstructions)
	currentHeading := ""
	inFence := false
	inFrontmatter := false
	frontmatterEligible := true

	appendInstruction := func(text string) {
		text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
		if text == "" {
			return
		}
		if len(text) > projectRuleMaxInstructionChars {
			text = text[:projectRuleMaxInstructionChars-1] + "…"
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if frontmatterEligible && trimmed == "---" {
			inFrontmatter = !inFrontmatter
			frontmatterEligible = false
			continue
		}
		frontmatterEligible = false
		if inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			currentHeading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			continue
		}

		text := strings.TrimSpace(trimmed)
		switch {
		case strings.HasPrefix(text, "- [ ] "), strings.HasPrefix(text, "- [x] "), strings.HasPrefix(text, "- [X] "):
			text = strings.TrimSpace(text[6:])
		case strings.HasPrefix(text, "- "), strings.HasPrefix(text, "* "), strings.HasPrefix(text, "+ "):
			text = strings.TrimSpace(text[2:])
		case projectRuleListPrefixPattern.MatchString(text):
			text = strings.TrimSpace(projectRuleListPrefixPattern.ReplaceAllString(text, ""))
		}
		if text == "" {
			continue
		}
		if currentHeading != "" && !strings.EqualFold(currentHeading, "agents") && !strings.EqualFold(currentHeading, "claude") {
			text = currentHeading + ": " + text
		}
		appendInstruction(text)
		if len(out) >= projectRuleMaxInstructions {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func renderProjectRulePrompt(bundles []RuleBundle) string {
	if len(bundles) == 0 {
		return ""
	}
	lines := []string{
		"<project_rules>",
		"Project instruction files were discovered for the current repository or workspace.",
		"Use them as operating constraints for this run unless a higher-priority system, policy, approval, or safety rule conflicts.",
		"Conflict handling:",
		"- repo-root rules appear before workspace-local rules",
		"- later workspace-local rules override earlier repo-root rules when they conflict",
	}
	for _, bundle := range bundles {
		lines = append(lines, "")
		lines = append(lines, "Source: "+renderRuleBundleSource(bundle))
		for _, instruction := range bundle.Instructions {
			lines = append(lines, "- "+instruction)
		}
	}
	lines = append(lines, "</project_rules>")
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderRuleBundleSource(bundle RuleBundle) string {
	label := bundle.Scope
	if label == "" {
		label = "project"
	}
	source := strings.TrimSpace(bundle.Source)
	if source == "" {
		source = filepath.Base(strings.TrimSpace(bundle.OriginPath))
	}
	if source == "" {
		return label
	}
	return label + " / " + source
}
