package toolruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errArtifactURI is returned when a path is an artifact URI, signalling the
// caller should use the artifact store instead of the filesystem.
var errArtifactURI = errors.New("path is an artifact URI")

// ws provides workspace-scoped path resolution. Embedded by both Layer 1
// (Builtins) and Layer 2 (Layer2Registry) to share sandboxing logic.
type ws struct {
	root         string
	rootAbs      string
	allowedPaths []string // additional absolute paths allowed outside root
	denyPatterns []string
}

func newWorkspace(root string) ws {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		rootAbs = root
	}
	rootAbs = filepath.Clean(rootAbs)
	return ws{root: root, rootAbs: rootAbs}
}

// setAllowedPaths configures additional directories that may be accessed
// outside the workspace root. Used by trusted_desktop to allow /tmp/, home
// directory, etc.
func (w *ws) setAllowedPaths(paths []string) {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(strings.TrimSpace(p))
		if err != nil {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(abs))
	}
	w.allowedPaths = cleaned
}

func (w *ws) setDenyPatterns(patterns []string) {
	cleaned := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		cleaned = append(cleaned, filepath.ToSlash(pattern))
	}
	w.denyPatterns = cleaned
}

func (w *ws) resolvePath(input string) (string, error) {
	return w.resolvePathWithMode(input, false, true)
}

func (w *ws) resolvePathWithOptions(input string, allowRoot bool) (string, error) {
	return w.resolvePathWithMode(input, allowRoot, true)
}

func (w *ws) resolvePathNoFollow(input string) (string, error) {
	return w.resolvePathWithMode(input, false, false)
}

func (w *ws) resolvePathWithMode(input string, allowRoot, followLeaf bool) (string, error) {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "artifact://") || strings.HasPrefix(trimmed, "artifact:/") {
		return "", fmt.Errorf("%w: %s", errArtifactURI, trimmed)
	}
	cleanInput := filepath.Clean(trimmed)
	if cleanInput == "" || cleanInput == "." {
		if allowRoot {
			return w.rootAbs, nil
		}
		return "", fmt.Errorf("path is required")
	}
	candidate := cleanInput
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(w.rootAbs, candidate)
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if err := w.authorizeAbsolutePath(absPath, followLeaf); err != nil {
		return "", err
	}
	if err := w.checkDeniedPath(absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

func (w *ws) authorizeAbsolutePath(absPath string, followLeaf bool) error {
	if withinAllowedRoots(absPath, w.allowedRoots()) && !followLeaf {
		return nil
	}

	info, err := os.Lstat(absPath)
	switch {
	case err == nil:
		if followLeaf {
			realPath, evalErr := filepath.EvalSymlinks(absPath)
			if evalErr != nil {
				return evalErr
			}
			if withinAllowedRoots(realPath, w.allowedRoots()) {
				return nil
			}
			return fmt.Errorf("path %q escapes builtin root %q", absPath, w.rootAbs)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			parentReal, parentErr := w.nearestExistingParentReal(filepath.Dir(absPath))
			if parentErr != nil {
				return parentErr
			}
			if !withinAllowedRoots(parentReal, w.allowedRoots()) {
				return fmt.Errorf("path %q escapes builtin root %q", absPath, w.rootAbs)
			}
		}
		if withinAllowedRoots(absPath, w.allowedRoots()) {
			return nil
		}
	case os.IsNotExist(err):
		parentReal, parentErr := w.nearestExistingParentReal(filepath.Dir(absPath))
		if parentErr != nil {
			return parentErr
		}
		if withinAllowedRoots(parentReal, w.allowedRoots()) {
			return nil
		}
	default:
		return err
	}
	return fmt.Errorf("path %q escapes builtin root %q", absPath, w.rootAbs)
}

func (w *ws) nearestExistingParentReal(start string) (string, error) {
	current := filepath.Clean(start)
	for {
		if _, err := os.Lstat(current); err == nil {
			realPath, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", evalErr
			}
			return filepath.Clean(realPath), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		current = parent
	}
}

func (w *ws) allowedRoots() []string {
	roots := make([]string, 0, len(w.allowedPaths)+1)
	appendRoot := func(path string) {
		path = filepath.Clean(path)
		roots = append(roots, path)
		if realPath, err := filepath.EvalSymlinks(path); err == nil {
			realPath = filepath.Clean(realPath)
			if realPath != path {
				roots = append(roots, realPath)
			}
		}
	}
	appendRoot(w.rootAbs)
	for _, allowed := range w.allowedPaths {
		appendRoot(allowed)
	}
	return roots
}

func withinAllowedRoots(candidate string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (w *ws) checkDeniedPath(absPath string) error {
	if !w.isDeniedPath(absPath) {
		return nil
	}
	return fmt.Errorf("path %q is denied by fs constraints", absPath)
}

func (w *ws) isDeniedPath(absPath string) bool {
	if len(w.denyPatterns) == 0 {
		return false
	}
	candidates := []string{
		filepath.ToSlash(filepath.Base(absPath)),
		filepath.ToSlash(absPath),
		filepath.ToSlash(w.displayPath(absPath)),
	}
	for _, pattern := range w.denyPatterns {
		for _, candidate := range candidates {
			matched, err := filepath.Match(pattern, candidate)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

func (w *ws) displayPath(absPath string) string {
	if rel, err := filepath.Rel(w.rootAbs, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(absPath)
}
