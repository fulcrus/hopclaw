package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors plugin roots for manifest-level changes and asks the caller
// to refresh runtime state when the discovered plugin fingerprint changes.
type Watcher struct {
	Dirs               []string
	Interval           time.Duration
	InitialFingerprint string
	OnChange           func()
	OnError            func(error)
}

func (w Watcher) Run(ctx context.Context) error {
	interval := w.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	lastFingerprint := strings.TrimSpace(w.InitialFingerprint)
	if lastFingerprint == "" {
		var err error
		lastFingerprint, err = fingerprintPluginDirs(w.Dirs)
		if err != nil {
			w.reportError(err)
		}
	}

	refresh := func() {
		next, err := fingerprintPluginDirs(w.Dirs)
		if err != nil {
			w.reportError(err)
			return
		}
		if next == lastFingerprint {
			return
		}
		lastFingerprint = next
		if w.OnChange != nil {
			w.OnChange()
		}
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err == nil {
		defer fsWatcher.Close()
		if err := addPluginWatchRoots(fsWatcher, w.Dirs); err != nil {
			w.reportError(err)
		}

		refreshTimer := time.NewTimer(interval)
		if !refreshTimer.Stop() {
			select {
			case <-refreshTimer.C:
			default:
			}
		}
		defer refreshTimer.Stop()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case event, ok := <-fsWatcher.Events:
				if !ok {
					return nil
				}
				if shouldIgnorePluginWatchPath(event.Name) {
					continue
				}
				if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
					if err := addPluginWatchPath(fsWatcher, event.Name); err != nil {
						w.reportError(err)
					}
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
					resetPluginWatchTimer(refreshTimer, 250*time.Millisecond)
				}
			case err, ok := <-fsWatcher.Errors:
				if !ok {
					return nil
				}
				w.reportError(err)
			case <-refreshTimer.C:
				refresh()
			case <-ticker.C:
				if err := addPluginWatchRoots(fsWatcher, w.Dirs); err != nil {
					w.reportError(err)
				}
				refresh()
			}
		}
	}

	if !errors.Is(err, os.ErrNotExist) {
		w.reportError(err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			refresh()
		}
	}
}

func (w Watcher) reportError(err error) {
	if err == nil {
		return
	}
	if w.OnError != nil {
		w.OnError(err)
	}
}

func addPluginWatchRoots(watcher *fsnotify.Watcher, dirs []string) error {
	for _, dir := range dirs {
		if err := addPluginWatchPath(watcher, dir); err != nil {
			return err
		}
	}
	return nil
}

func addPluginWatchPath(watcher *fsnotify.Watcher, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	path := expandHome(raw)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(path)
			if parent == "." || parent == "" {
				return nil
			}
			return watcher.Add(parent)
		}
		return err
	}
	if !info.IsDir() {
		if shouldIgnorePluginWatchPath(path) {
			return nil
		}
		return watcher.Add(filepath.Dir(path))
	}
	if shouldIgnorePluginWatchPath(path) {
		return nil
	}
	if err := watcher.Add(path); err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		if shouldIgnorePluginWatchPath(child) {
			continue
		}
		if entry.IsDir() {
			if err := watcher.Add(child); err != nil && !strings.Contains(err.Error(), "already exists") {
				return err
			}
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(child)
			if err == nil && info.IsDir() {
				if err := watcher.Add(child); err != nil && !strings.Contains(err.Error(), "already exists") {
					return err
				}
			}
		}
	}
	return nil
}

func shouldIgnorePluginWatchPath(path string) bool {
	path = filepath.ToSlash(path)
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "", ".git", "node_modules", ".disabled", "dist", "build", ".cache":
			if part != "" {
				return true
			}
		}
	}
	return false
}

func resetPluginWatchTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func fingerprintPluginDirs(dirs []string) (string, error) {
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, raw := range dirs {
		dir := expandHome(strings.TrimSpace(raw))
		if dir == "" {
			continue
		}
		collectPluginManifestState(dir, &paths, seen)
	}
	sort.Strings(paths)
	sum := sha256.Sum256([]byte(strings.Join(paths, "\n")))
	return hex.EncodeToString(sum[:8]), nil
}

// FingerprintDirs returns the current manifest fingerprint for plugin roots.
func FingerprintDirs(dirs []string) (string, error) {
	return fingerprintPluginDirs(dirs)
}

func collectPluginManifestState(root string, out *[]string, seen map[string]struct{}) {
	appendManifestState(root, out, seen)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		child := filepath.Join(root, entry.Name())
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			info, err := os.Stat(child)
			if err != nil || !info.IsDir() {
				continue
			}
		}
		appendManifestState(child, out, seen)
	}
}

func appendManifestState(dir string, out *[]string, seen map[string]struct{}) {
	for _, name := range []string{manifestFile, openClawManifestFile} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		*out = append(*out, absPath+"|"+strconvFormatInt(info.Size())+"|"+strconvFormatInt(info.ModTime().UnixNano()))
	}
}

func strconvFormatInt(v int64) string { return strconv.FormatInt(v, 10) }
