package plugin

import "strings"

// NewManifest returns a trimmed manifest starter for SDK-based plugins.
func NewManifest(name, version, description string) Manifest {
	return Manifest{
		Name:        strings.TrimSpace(name),
		Version:     strings.TrimSpace(version),
		Description: strings.TrimSpace(description),
	}
}

// Clone returns a deep copy of the manifest.
func (m Manifest) Clone() Manifest {
	return cloneManifest(m)
}

// SkillRoots returns the manifest skill directories in declaration order with
// empty values and duplicates removed.
func (m Manifest) SkillRoots() []string {
	roots := make([]string, 0, len(m.SkillsDirs)+1)
	if dir := strings.TrimSpace(m.SkillsDir); dir != "" {
		roots = append(roots, dir)
	}
	for _, raw := range m.SkillsDirs {
		if dir := strings.TrimSpace(raw); dir != "" {
			roots = append(roots, dir)
		}
	}
	if len(roots) == 0 {
		return nil
	}

	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	return out
}

// PrimarySkillDir returns the first configured skill directory, if any.
func (m Manifest) PrimarySkillDir() string {
	roots := m.SkillRoots()
	if len(roots) == 0 {
		return ""
	}
	return roots[0]
}
