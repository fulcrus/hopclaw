package bootstrap

import (
	"context"
	"reflect"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/plugin"
)

type pluginWatcherCallbackContextKey struct{}

func resolvedPluginDirs(cfg config.Config) []string {
	dirs := append([]string(nil), cfg.Plugins.Dirs...)
	if cfg.Plugins.AutoDiscover {
		dirs = append(dirs, plugin.DefaultPluginDirs(cfg.Tools.Builtins.Root)...)
	}
	return normalize.DedupeStrings(trimmedNonEmptyStrings(dirs))
}

func trimmedNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (a *App) restartPluginWatcherLocked(ctx context.Context) {
	if a == nil {
		return
	}
	a.pluginWatchGen++
	if a.closed {
		if a.pluginWatchStop != nil {
			a.pluginWatchStop()
			a.pluginWatchStop = nil
		}
		return
	}
	if a.pluginWatchStop != nil {
		a.pluginWatchStop()
		a.pluginWatchStop = nil
	}
	if !enabledOrDefault(a.Config.Plugins.Enabled, true) {
		return
	}
	dirs := resolvedPluginDirs(a.Config)
	if len(dirs) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	initialFingerprint, err := plugin.FingerprintDirs(dirs)
	if err != nil {
		log.Warn("plugin watcher initial fingerprint failed", "error", err)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	watchGen := a.pluginWatchGen
	watcher := plugin.Watcher{
		Dirs:               dirs,
		Interval:           a.Config.Skills.RefreshInterval,
		InitialFingerprint: initialFingerprint,
		OnChange: func() {
			a.dispatchPluginWatcherRefresh(watchCtx, watchGen)
		},
		OnError: func(err error) {
			log.Warn("plugin watcher failed", "error", err)
		},
	}
	go func() {
		defer close(done)
		if err := watcher.Run(watchCtx); err != nil && err != context.Canceled {
			log.Warn("plugin watcher stopped", "error", err)
		}
	}()
	a.pluginWatchStop = cancelAndWait(cancel, done)
	log.Info("plugin watcher started", "dirs", dirs)
}

func markPluginWatcherCallbackContext(ctx context.Context, watchGen uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, pluginWatcherCallbackContextKey{}, watchGen)
}

func pluginWatcherCallbackGeneration(ctx context.Context) (uint64, bool) {
	if ctx == nil {
		return 0, false
	}
	watchGen, ok := ctx.Value(pluginWatcherCallbackContextKey{}).(uint64)
	return watchGen, ok
}

func sanitizePluginWatcherRestartContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if _, ok := pluginWatcherCallbackGeneration(ctx); ok {
		return context.Background()
	}
	return ctx
}

func (a *App) requestPluginWatcherRestartLocked(ctx context.Context) {
	if a == nil {
		return
	}
	if watchGen, ok := pluginWatcherCallbackGeneration(ctx); ok && watchGen == a.pluginWatchGen {
		a.deferPluginWatcherRestartLocked()
		return
	}
	a.restartPluginWatcherLocked(sanitizePluginWatcherRestartContext(ctx))
}

func (a *App) deferPluginWatcherRestartLocked() {
	if a == nil || a.pluginWatchRestartQueued {
		return
	}
	a.pluginWatchRestartQueued = true
	go func() {
		a.refreshMu.Lock()
		defer a.refreshMu.Unlock()
		if !a.pluginWatchRestartQueued {
			return
		}
		a.pluginWatchRestartQueued = false
		a.restartPluginWatcherLocked(context.Background())
	}()
}

func (a *App) dispatchPluginWatcherRefresh(ctx context.Context, watchGen uint64) {
	if a == nil {
		return
	}
	go func() {
		ctx = markPluginWatcherCallbackContext(ctx, watchGen)
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		if err := a.refreshPluginsFromWatcher(ctx, watchGen); err != nil && err != context.Canceled {
			log.Warn("plugin auto-refresh failed", "error", err)
		}
	}()
}

func (a *App) refreshPluginsFromWatcher(ctx context.Context, watchGen uint64) error {
	if a == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.closed || a.pluginWatchGen != watchGen {
		return nil
	}
	return a.refreshPluginsLocked(ctx, false)
}

func pluginsRuntimeConfigChanged(oldCfg, newCfg config.Config) bool {
	if !reflect.DeepEqual(oldCfg.Plugins, newCfg.Plugins) {
		return true
	}
	return strings.TrimSpace(oldCfg.Tools.Builtins.Root) != strings.TrimSpace(newCfg.Tools.Builtins.Root)
}
