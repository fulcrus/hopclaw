package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// SkillPlugin exposes a single release-grade SKILL.md contract from typed Go code.
type SkillPlugin interface {
	Skill() Skill
}

// Skill describes a single rendered SKILL.md asset.
type Skill struct {
	Name        string
	Description string
	Title       string
	TLDR        string
	Body        string
}

// DirectoryName returns the default on-disk directory name for the skill.
func (s Skill) DirectoryName() string {
	name := skillSlug(s.Name)
	if name == "" {
		return "skill"
	}
	return name
}

// DisplayTitle returns the markdown title to render for the skill.
func (s Skill) DisplayTitle() string {
	if title := strings.TrimSpace(s.Title); title != "" {
		return title
	}
	if name := strings.TrimSpace(s.Name); name != "" {
		return skillTitle(name)
	}
	return "Skill"
}

// Markdown renders the skill into a release-grade SKILL.md file.
func (s Skill) Markdown() string {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = s.DirectoryName()
	}
	tldr := strings.TrimSpace(s.TLDR)
	if tldr == "" {
		tldr = "Replace this starter content with the shortest safe workflow for your plugin users."
	}
	body := strings.TrimSpace(s.Body)
	if body == "" {
		body = "## Usage\n\n```bash\n# Add copy-pasteable commands here.\n```\n"
	}

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(strconv.Quote(name))
	builder.WriteString("\n")
	if description := strings.TrimSpace(s.Description); description != "" {
		builder.WriteString("description: ")
		builder.WriteString(strconv.Quote(description))
		builder.WriteString("\n")
	}
	builder.WriteString("---\n")
	builder.WriteString("# ")
	builder.WriteString(s.DisplayTitle())
	builder.WriteString("\n\n")
	builder.WriteString("## TL;DR\n\n")
	builder.WriteString(tldr)
	builder.WriteString("\n\n")
	builder.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		builder.WriteString("\n")
	}
	return builder.String()
}

// Files returns the rendered files for the skill directory.
func (s Skill) Files() map[string]string {
	return map[string]string{
		"SKILL.md": s.Markdown(),
	}
}

// WriteToDir materializes the skill files into the provided plugin skill root.
func (s Skill) WriteToDir(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("skill root is required")
	}
	targetDir := filepath.Join(root, s.DirectoryName())
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	for name, contents := range s.Files() {
		path := filepath.Join(targetDir, name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write skill file %s: %w", path, err)
		}
	}
	return nil
}

func skillSlug(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func skillTitle(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '-' || r == '_' || unicode.IsSpace(r)
	})
	if len(parts) == 0 {
		return "Skill"
	}
	for idx, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[idx] = string(runes)
	}
	return strings.Join(parts, " ")
}
