package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const minBundledSkills = 56

// findSkillsDir locates the project-root skills/ directory by walking up from
// the current working directory.
func findSkillsDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}

	for {
		candidate := filepath.Join(dir, "skills")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find skills/ directory walking up from %s", dir)
		}
		dir = parent
	}
}

// collectSkillFiles walks the skills directory and returns all SKILL.md paths.
func collectSkillFiles(t *testing.T, skillsDir string) []string {
	t.Helper()

	var paths []string
	err := filepath.Walk(skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Base(path) == "SKILL.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk skills directory: %v", err)
	}
	return paths
}

// parseFrontmatter extracts the YAML frontmatter from a SKILL.md file and
// returns it as a nested map.
func parseFrontmatter(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	content := string(data)
	const marker = "---"
	if !strings.HasPrefix(strings.TrimSpace(content), marker) {
		t.Fatalf("%s: missing opening frontmatter delimiter", path)
	}

	// Find content between the two --- delimiters.
	lines := strings.Split(content, "\n")
	start := -1
	end := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == marker {
			if start == -1 {
				start = i
			} else {
				end = i
				break
			}
		}
	}
	if start == -1 || end == -1 {
		t.Fatalf("%s: could not find frontmatter delimiters", path)
	}

	fmBlock := strings.Join(lines[start+1:end], "\n")
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		t.Fatalf("%s: unmarshal frontmatter: %v", path, err)
	}
	return fm
}

// skillKeyFromFrontmatter navigates the metadata.openclaw.skillKey path in a
// parsed frontmatter map and returns the value, or empty string if absent.
func skillKeyFromFrontmatter(fm map[string]any) string {
	meta, ok := fm["metadata"]
	if !ok {
		return ""
	}
	metaMap, ok := meta.(map[string]any)
	if !ok {
		return ""
	}
	oc, ok := metaMap["openclaw"]
	if !ok {
		return ""
	}
	ocMap, ok := oc.(map[string]any)
	if !ok {
		return ""
	}
	key, ok := ocMap["skillKey"]
	if !ok {
		return ""
	}
	s, ok := key.(string)
	if !ok {
		return ""
	}
	return s
}

func TestBundledSkillsIntegrity(t *testing.T) {
	t.Parallel()

	skillsDir := os.Getenv("HOPCLAW_BUNDLED_SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = findSkillsDir(t)
	}

	paths := collectSkillFiles(t, skillsDir)

	// ------------------------------------------------------------------
	// Verify minimum count
	// ------------------------------------------------------------------
	if len(paths) < minBundledSkills {
		t.Fatalf("found %d bundled skills, want >= %d", len(paths), minBundledSkills)
	}

	seenKeys := make(map[string]string) // skillKey -> file path

	for _, path := range paths {
		rel, _ := filepath.Rel(skillsDir, path)
		fm := parseFrontmatter(t, path)

		// --------------------------------------------------------------
		// Required field: name
		// --------------------------------------------------------------
		name, ok := fm["name"]
		if !ok {
			t.Fatalf("%s: missing required field 'name'", rel)
		}
		nameStr, ok := name.(string)
		if !ok || strings.TrimSpace(nameStr) == "" {
			t.Fatalf("%s: 'name' must be a non-empty string, got %v", rel, name)
		}

		// --------------------------------------------------------------
		// Required field: description
		// --------------------------------------------------------------
		desc, ok := fm["description"]
		if !ok {
			t.Fatalf("%s: missing required field 'description'", rel)
		}
		descStr, ok := desc.(string)
		if !ok || strings.TrimSpace(descStr) == "" {
			t.Fatalf("%s: 'description' must be a non-empty string, got %v", rel, desc)
		}

		// --------------------------------------------------------------
		// Required field: metadata.openclaw.skillKey
		// --------------------------------------------------------------
		skillKey := skillKeyFromFrontmatter(fm)
		if skillKey == "" {
			t.Fatalf("%s: missing required field 'metadata.openclaw.skillKey'", rel)
		}

		// --------------------------------------------------------------
		// No duplicate skillKey values
		// --------------------------------------------------------------
		if existing, dup := seenKeys[skillKey]; dup {
			t.Fatalf("duplicate skillKey %q: %s and %s", skillKey, existing, rel)
		}
		seenKeys[skillKey] = rel
	}

	t.Logf("validated %d bundled skills, %d unique skillKeys", len(paths), len(seenKeys))
}
