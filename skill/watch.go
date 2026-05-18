package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	Registry  *Registry
	Roots     []DiscoveryRoot
	Interval  time.Duration
	OnRefresh func(RegistrySnapshot)
	OnError   func(error)
}

var ignoredSkillDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"dist":         {},
	"build":        {},
	".cache":       {},
	".venv":        {},
	"venv":         {},
	"__pycache__":  {},
}

func (w Watcher) Run(ctx context.Context) error {
	if w.Registry == nil {
		return nil
	}
	interval := w.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	var lastFingerprint string
	refresh := func() error {
		snapshot, err := w.Registry.Refresh(ctx, w.Roots)
		if err != nil {
			return err
		}
		if snapshot.Fingerprint == lastFingerprint {
			return nil
		}
		lastFingerprint = snapshot.Fingerprint
		if w.OnRefresh != nil {
			w.OnRefresh(*snapshot)
		}
		return nil
	}

	if err := refresh(); err != nil {
		if w.OnError != nil {
			w.OnError(err)
		} else {
			return err
		}
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err == nil {
		defer fsWatcher.Close()
		if err := addSkillWatchRoots(fsWatcher, w.Roots); err != nil {
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
				if shouldIgnoreWatchEvent(event.Name) {
					continue
				}
				if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
					if err := addWatchPath(fsWatcher, event.Name); err != nil {
						w.reportError(err)
					}
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
					resetWatcherTimer(refreshTimer, 250*time.Millisecond)
				}
			case err, ok := <-fsWatcher.Errors:
				if !ok {
					return nil
				}
				w.reportError(err)
			case <-refreshTimer.C:
				if err := refresh(); err != nil {
					w.reportError(err)
				}
			case <-ticker.C:
				if err := refresh(); err != nil {
					w.reportError(err)
				}
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
			if err := refresh(); err != nil {
				w.reportError(err)
			}
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

func addSkillWatchRoots(watcher *fsnotify.Watcher, roots []DiscoveryRoot) error {
	for _, root := range roots {
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		if err := addWatchPath(watcher, root.Path); err != nil {
			return err
		}
	}
	return nil
}

func addWatchPath(watcher *fsnotify.Watcher, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		if shouldIgnoreWatchEvent(path) {
			return nil
		}
		return watcher.Add(filepath.Dir(path))
	}
	return filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if shouldIgnoreWatchEvent(current) {
			return filepath.SkipDir
		}
		if err := watcher.Add(current); err != nil {
			// Duplicate watch registrations are harmless.
			if strings.Contains(err.Error(), "already exists") {
				return nil
			}
			return err
		}
		return nil
	})
}

func shouldIgnoreWatchEvent(path string) bool {
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if _, ok := ignoredSkillDirNames[part]; ok {
			return true
		}
	}
	return false
}

func resetWatcherTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}
