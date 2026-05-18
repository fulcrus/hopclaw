package skill

import (
	"os"
	"path/filepath"
)

// ResolveBundledSkillsDir finds the bundled skills directory relative to the executable.
// It checks (in order):
//  1. HOPCLAW_BUNDLED_SKILLS_DIR env var
//  2. {executable_dir}/skills/
//  3. {executable_dir}/../skills/
//
// Returns empty string if not found.
func ResolveBundledSkillsDir() string {
	if dir := os.Getenv("HOPCLAW_BUNDLED_SKILLS_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exeDir := filepath.Dir(exe)

	candidates := []string{
		filepath.Join(exeDir, "skills"),
		filepath.Join(exeDir, "..", "skills"),
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}
