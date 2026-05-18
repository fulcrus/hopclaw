package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Loader interface {
	Discover(ctx context.Context, roots []DiscoveryRoot) ([]SkillSource, error)
	Load(ctx context.Context, src SkillSource) (*ExternalSkillSpec, error)
}

type FilesystemLoader struct {
	Limits Limits
}

func (fl FilesystemLoader) effectiveLimits() Limits {
	lim := fl.Limits
	defaults := DefaultLimits()
	if lim.MaxFileSize <= 0 {
		lim.MaxFileSize = defaults.MaxFileSize
	}
	if lim.MaxSkillsPerDir <= 0 {
		lim.MaxSkillsPerDir = defaults.MaxSkillsPerDir
	}
	if lim.MaxTotalSkills <= 0 {
		lim.MaxTotalSkills = defaults.MaxTotalSkills
	}
	if lim.MaxPromptChars <= 0 {
		lim.MaxPromptChars = defaults.MaxPromptChars
	}
	return lim
}

// ignoredDirNames are directories that should never be scanned for skills.
var ignoredDirNames = map[string]struct{}{
	"node_modules":  {},
	"vendor":        {},
	"__pycache__":   {},
	".mypy_cache":   {},
	".pytest_cache": {},
	".tox":          {},
	".venv":         {},
}

// isSkillCandidateDir returns true if the dir entry should be considered
// during skill discovery. Handles symlinks by resolving them, and skips
// hidden directories and known irrelevant directories.
func isSkillCandidateDir(entry os.DirEntry, parentPath string) bool {
	name := entry.Name()
	if len(name) == 0 {
		return false
	}
	// Skip hidden directories.
	if name[0] == '.' {
		return false
	}
	// Skip known irrelevant directories.
	if _, ok := ignoredDirNames[name]; ok {
		return false
	}
	// Direct directory.
	if entry.IsDir() {
		return true
	}
	// Symlink → resolve and check if target is a directory.
	if entry.Type()&os.ModeSymlink != 0 {
		resolved, err := os.Stat(filepath.Join(parentPath, name))
		if err == nil && resolved.IsDir() {
			return true
		}
	}
	return false
}

func (fl FilesystemLoader) Discover(ctx context.Context, roots []DiscoveryRoot) ([]SkillSource, error) {
	lim := fl.effectiveLimits()
	var out []SkillSource
	for _, root := range roots {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if root.Path == "" {
			continue
		}
		stat, err := os.Stat(root.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", root.Path, err)
		}
		if !stat.IsDir() {
			continue
		}

		rootCount := 0

		// Root itself may be a skill/bundle dir.
		if hasSkillMarkdown(root.Path) || hasBundleManifest(root.Path) {
			out = append(out, SkillSource{
				Kind:     root.Kind,
				Root:     root.Path,
				Dir:      root.Path,
				NameHint: filepath.Base(root.Path),
				Priority: root.effectivePriority(),
			})
			continue
		}

		entries, err := os.ReadDir(root.Path)
		if err != nil {
			return nil, fmt.Errorf("read dir %s: %w", root.Path, err)
		}
		for _, entry := range entries {
			if rootCount >= lim.MaxSkillsPerDir {
				break
			}
			if !isSkillCandidateDir(entry, root.Path) {
				continue
			}
			dir := filepath.Join(root.Path, entry.Name())
			if hasSkillMarkdown(dir) || hasBundleManifest(dir) {
				out = append(out, SkillSource{
					Kind:     root.Kind,
					Root:     root.Path,
					Dir:      dir,
					NameHint: entry.Name(),
					Priority: root.effectivePriority(),
				})
				rootCount++
				continue
			}

			// Nested skills pattern: {subdir}/skills/{name}/SKILL.md
			nestedSkillsDir := filepath.Join(dir, "skills")
			nestedEntries, err := os.ReadDir(nestedSkillsDir)
			if err == nil {
				for _, nestedEntry := range nestedEntries {
					if rootCount >= lim.MaxSkillsPerDir {
						break
					}
					if !isSkillCandidateDir(nestedEntry, nestedSkillsDir) {
						continue
					}
					nestedDir := filepath.Join(nestedSkillsDir, nestedEntry.Name())
					if !hasSkillMarkdown(nestedDir) && !hasBundleManifest(nestedDir) {
						continue
					}
					out = append(out, SkillSource{
						Kind:     root.Kind,
						Root:     root.Path,
						Dir:      nestedDir,
						NameHint: nestedEntry.Name(),
						Priority: root.effectivePriority(),
					})
					rootCount++
				}
			}

			if versionDir, ok := latestVersionedSkillDir(dir); ok {
				out = append(out, SkillSource{
					Kind:     root.Kind,
					Root:     root.Path,
					Dir:      versionDir,
					NameHint: entry.Name(),
					Priority: root.effectivePriority(),
				})
				rootCount++
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Dir < out[j].Dir
	})
	return out, nil
}

func latestVersionedSkillDir(parent string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	best := ""
	var bestVer semVersion
	for _, entry := range entries {
		if !isSkillCandidateDir(entry, parent) {
			continue
		}
		dir := filepath.Join(parent, entry.Name())
		if !hasSkillMarkdown(dir) && !hasBundleManifest(dir) {
			continue
		}
		ver := parseSemver(entry.Name())
		if best == "" || compareSemver(ver, bestVer) > 0 {
			best = dir
			bestVer = ver
		}
	}
	return best, best != ""
}

func (fl FilesystemLoader) Load(_ context.Context, src SkillSource) (*ExternalSkillSpec, error) {
	lim := fl.effectiveLimits()
	manifestPath := filepath.Join(src.Dir, "SKILL.md")
	if hasBundleManifest(src.Dir) {
		manifestPath = filepath.Join(src.Dir, "BUNDLE.yaml")
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", manifestPath, err)
	}
	if info.Size() > lim.MaxFileSize {
		return nil, fmt.Errorf("%s in %s exceeds max file size (%d > %d bytes)", filepath.Base(manifestPath), src.Dir, info.Size(), lim.MaxFileSize)
	}
	return ParseDir(src.Dir)
}
