package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"
)

// Project represents a registered project context for memory isolation.
type Project struct {
	ID        string    `json:"id"`        // proj_hopclaw_a3f7b2
	Name      string    `json:"name"`      // "HopClaw"
	Directory string    `json:"directory"` // "/home/user/projects/hopclaw"
	GitRepo   string    `json:"git_repo"`  // "github.com/fulcrus/hopclaw"
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

// GlobalProjectID is the special project ID for memories not tied to any project.
const GlobalProjectID = "proj_global_000000"

// ProjectID generates a stable, readable project identifier from an absolute directory path.
// Format: proj_{dirname}_{sha256_prefix_8}
// Example: /home/user/projects/hopclaw -> proj_hopclaw_a3f7b2e1
//
// Cross-platform: paths are normalized to forward slashes and lowercased
// before hashing, so the same logical directory produces the same ID
// on Windows, macOS, and Linux.
func ProjectID(absDir string) string {
	absDir = filepath.Clean(absDir)

	// Normalize for cross-platform consistency:
	// - forward slashes (Windows backslash → /)
	// - lowercase (macOS/Windows are case-insensitive)
	normalized := strings.ToLower(filepath.ToSlash(absDir))

	base := filepath.Base(absDir)
	name := sanitizeProjectName(base, 16)
	hash := sha256.Sum256([]byte(normalized))
	short := hex.EncodeToString(hash[:])[:8]
	return "proj_" + name + "_" + short
}

// sanitizeProjectName keeps only lowercase alphanumeric and underscore, truncated to maxLen.
func sanitizeProjectName(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else if r == '-' || r == '.' || r == ' ' {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	if out == "" {
		out = "unnamed"
	}
	return out
}
