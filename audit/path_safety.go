package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Sensitive path patterns
// ---------------------------------------------------------------------------

var sensitivePathPatterns = []string{
	".ssh",
	".gnupg",
	".aws",
	".env",
	".git/config",
	"id_rsa",
	"id_ed25519",
	"credentials",
	"secrets",
	".kube/config",
	".docker/config.json",
}

// ---------------------------------------------------------------------------
// PathChecker
// ---------------------------------------------------------------------------

// PathChecker validates file paths for safety issues such as path traversal,
// symlink escapes, null bytes, and access to sensitive files.
type PathChecker struct {
	allowedRoots      []string
	sensitivePatterns []string
}

// NewPathChecker creates a PathChecker that restricts paths to the given
// allowed root directories. If allowedRoots is empty, only generic safety
// checks (traversal, null bytes, sensitive patterns) are applied.
func NewPathChecker(allowedRoots []string) *PathChecker {
	cleaned := make([]string, 0, len(allowedRoots))
	for _, root := range allowedRoots {
		r := strings.TrimSpace(root)
		if r == "" {
			continue
		}
		// Resolve symlinks on the root itself so comparisons against
		// EvalSymlinks results are consistent (e.g. macOS /var -> /private/var).
		resolved, err := filepath.EvalSymlinks(r)
		if err == nil {
			r = resolved
		}
		cleaned = append(cleaned, filepath.Clean(r))
	}
	return &PathChecker{
		allowedRoots:      cleaned,
		sensitivePatterns: sensitivePathPatterns,
	}
}

// CheckPath validates a file path for safety issues.
// Returns nil if safe, or an error describing the issue.
func (c *PathChecker) CheckPath(path string) error {
	if err := c.checkNullBytes(path); err != nil {
		return err
	}
	if err := c.checkTraversal(path); err != nil {
		return err
	}
	if err := c.checkSensitivePatterns(path); err != nil {
		return err
	}
	if err := c.checkAbsoluteInRelative(path); err != nil {
		return err
	}
	if err := c.checkSymlinks(path); err != nil {
		return err
	}
	return nil
}

// checkNullBytes rejects paths containing null bytes, which can be used to
// truncate paths in C-based libraries.
func (c *PathChecker) checkNullBytes(path string) error {
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path contains null byte")
	}
	return nil
}

// checkTraversal rejects paths that contain ".." path components.
// Both raw input and cleaned forms are inspected: an absolute path like
// "/tmp/../etc/passwd" cleans to "/etc/passwd" (no ".." remaining) but the
// raw input clearly shows traversal intent.
func (c *PathChecker) checkTraversal(path string) error {
	// Check the raw path for ".." segments (normalised to forward slashes).
	normalised := filepath.ToSlash(path)
	for _, component := range strings.Split(normalised, "/") {
		if component == ".." {
			return fmt.Errorf("path traversal detected in %q", path)
		}
	}
	// Also check the cleaned path.
	cleaned := filepath.Clean(path)
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".." {
			return fmt.Errorf("path traversal detected in %q", path)
		}
	}
	return nil
}

// checkSensitivePatterns rejects paths that match known sensitive file
// patterns such as SSH keys, cloud credentials, and env files.
func (c *PathChecker) checkSensitivePatterns(path string) error {
	normalised := filepath.ToSlash(filepath.Clean(path))
	lower := strings.ToLower(normalised)
	for _, pattern := range c.sensitivePatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return fmt.Errorf("access to sensitive path %q (matched %q)", path, pattern)
		}
	}
	return nil
}

// checkAbsoluteInRelative warns when an absolute path is supplied but only
// relative paths are expected (i.e. allowed roots are configured).
func (c *PathChecker) checkAbsoluteInRelative(path string) error {
	if len(c.allowedRoots) == 0 || !filepath.IsAbs(path) {
		return nil
	}
	// Resolve the incoming path so the comparison is consistent with the
	// resolved allowed roots (e.g. macOS /tmp -> /private/tmp).
	// If the full path doesn't exist, resolve the longest existing ancestor.
	cleaned := filepath.Clean(path)
	resolved := resolveExistingPrefix(cleaned)
	for _, root := range c.allowedRoots {
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) || resolved == root {
			return nil
		}
	}
	return fmt.Errorf("absolute path %q is outside allowed roots", path)
}

// resolveExistingPrefix resolves symlinks on the longest existing prefix of
// path and appends the remaining unresolved suffix.
func resolveExistingPrefix(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	// Walk up to find the longest ancestor that exists.
	dir := filepath.Dir(path)
	for dir != path {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			suffix := strings.TrimPrefix(path, dir)
			return filepath.Join(resolved, suffix)
		}
		path = dir
		dir = filepath.Dir(dir)
	}
	return filepath.Clean(path)
}

// checkSymlinks resolves symlinks and verifies the real path stays within
// allowed roots. Silently skipped if the path does not exist on disk.
func (c *PathChecker) checkSymlinks(path string) error {
	if len(c.allowedRoots) == 0 {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// Path does not exist yet; skip symlink check.
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	resolved = filepath.Clean(resolved)
	for _, root := range c.allowedRoots {
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) || resolved == root {
			return nil
		}
	}
	return fmt.Errorf("symlink resolves to %q which is outside allowed roots", resolved)
}
