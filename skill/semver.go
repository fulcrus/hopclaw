package skill

import (
	"strconv"
	"strings"
)

// semVersion represents a parsed semantic version.
type semVersion struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Raw        string
}

// parseSemver parses a version string like "1.2.3", "v1.2.3-beta.1", or "latest".
// Returns the parsed version or a fallback that sorts by raw string.
func parseSemver(raw string) semVersion {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "latest" {
		return semVersion{Major: 999999, Minor: 999999, Patch: 999999, Raw: raw}
	}

	s := strings.TrimPrefix(raw, "v")
	sv := semVersion{Raw: raw}

	// Split prerelease.
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		sv.Prerelease = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.SplitN(s, ".", 3)
	if len(parts) >= 1 {
		sv.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		sv.Minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		sv.Patch, _ = strconv.Atoi(parts[2])
	}

	return sv
}

// compareSemver returns -1, 0, or 1 comparing a to b.
func compareSemver(a, b semVersion) int {
	if a.Major != b.Major {
		return intCmp(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return intCmp(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return intCmp(a.Patch, b.Patch)
	}
	// No prerelease > has prerelease (1.0.0 > 1.0.0-beta).
	if a.Prerelease == "" && b.Prerelease != "" {
		return 1
	}
	if a.Prerelease != "" && b.Prerelease == "" {
		return -1
	}
	return strings.Compare(a.Prerelease, b.Prerelease)
}

func intCmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
