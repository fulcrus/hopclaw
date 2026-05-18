package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/canvas"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/hooks"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/sandbox"
)

type preparedOperatorAddons struct {
	allowlistManager  *allowlist.Manager
	sandboxRunner     *sandbox.Runner
	isolationManager  *isolation.Manager
	healthMonitor     *health.Monitor
	hookExecutor      *hooks.Executor
	canvasHost        *canvas.Host
	discoveryResolver discovery.Resolver
	pluginInstaller   *plugin.Installer
	keychainWatcher   *keychain.Watcher
}

func prepareOperatorAddons(
	ctx context.Context,
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
	channelManager *channelmgr.Manager,
) *preparedOperatorAddons {
	var allowlistManager *allowlist.Manager
	if enabledOrDefault(cfg.Allowlist.Enabled, false) && len(cfg.Allowlist.Channels) > 0 {
		rules := make([]allowlist.ChannelRules, 0, len(cfg.Allowlist.Channels))
		for _, ch := range cfg.Allowlist.Channels {
			rules = append(rules, allowlist.ChannelRules{
				Channel:     ch.Channel,
				AllowAll:    ch.AllowAll,
				AllowUsers:  ch.AllowUsers,
				DenyUsers:   ch.DenyUsers,
				AllowGroups: ch.AllowGroups,
				DenyGroups:  ch.DenyGroups,
			})
		}
		allowlistManager = allowlist.NewManager(rules)
	}

	var sandboxRunner *sandbox.Runner
	if enabledOrDefault(cfg.Sandbox.Enabled, false) {
		runner, sandboxErr := sandbox.NewRunner(sandbox.Config{
			Enabled:       true,
			Image:         cfg.Sandbox.Image,
			MemoryLimit:   cfg.Sandbox.MemoryLimit,
			CPULimit:      cfg.Sandbox.CPULimit,
			Timeout:       cfg.Sandbox.Timeout,
			NetworkMode:   cfg.Sandbox.NetworkMode,
			WorkDir:       cfg.Sandbox.WorkDir,
			AllowedImages: cfg.Sandbox.AllowedImages,
		})
		if sandboxErr != nil {
			foundation.startupWarnings.Add("sandbox_runner", sandboxErr)
			log.Warn("sandbox runner init failed", "error", sandboxErr)
		} else {
			sandboxRunner = runner
		}
	}

	var isolationManager *isolation.Manager
	if enabledOrDefault(cfg.Isolation.Enabled, false) {
		baseDir := operatorIsolationBaseDir(cfg)
		if baseDir != "" {
			mgr, isoErr := isolation.NewManager(baseDir)
			if isoErr != nil {
				foundation.startupWarnings.Add("isolation_manager", isoErr)
				log.Warn("isolation manager init failed", "error", isoErr)
			} else {
				isolationManager = mgr
			}
		}
	}

	var discoveryResolver discovery.Resolver
	if enabledOrDefault(cfg.Discovery.Enabled, false) && strings.TrimSpace(cfg.Discovery.Method) != "" {
		discoveryCfg := discovery.Config{
			Enabled:      true,
			Method:       discovery.Method(cfg.Discovery.Method),
			Service:      cfg.Discovery.Service,
			Peers:        cfg.Discovery.Peers,
			InstanceName: cfg.Discovery.InstanceName,
			Port:         cfg.Discovery.Port,
			Interface:    cfg.Discovery.Interface,
		}
		resolver, discErr := discovery.NewResolver(discoveryCfg)
		if discErr != nil {
			foundation.startupWarnings.Add("discovery_resolver", discErr)
			log.Warn("discovery resolver init failed", "error", discErr)
		} else {
			discoveryResolver = resolver
			self := discovery.Peer{
				ID:      cfg.Server.Address,
				Name:    cfg.Discovery.InstanceName,
				Version: cfg.Server.Version,
			}
			if err := resolver.Announce(ctx, self); err != nil {
				foundation.startupWarnings.Add("discovery_announce", err)
				log.Warn("discovery announce failed", "error", err)
			}
		}
	}

	var canvasHost *canvas.Host
	if enabledOrDefault(cfg.Canvas.Enabled, false) {
		host, canvasErr := canvas.NewHost(canvas.HostConfig{
			Enabled:    true,
			Port:       cfg.Canvas.Port,
			Root:       cfg.Canvas.Root,
			LiveReload: enabledOrDefault(cfg.Canvas.LiveReload, true),
			TokenTTL:   cfg.Canvas.TokenTTL,
		})
		if canvasErr != nil {
			foundation.startupWarnings.Add("canvas_host", canvasErr)
			log.Warn("canvas host init failed", "error", canvasErr)
		} else {
			canvasHost = host
			if err := host.Start(); err != nil {
				foundation.startupWarnings.Add("canvas_host_start", err)
				log.Warn("canvas host start failed", "error", err)
			}
		}
	}

	var healthMonitor *health.Monitor
	if enabledOrDefault(cfg.ChannelHealth.Enabled, true) {
		healthMonitor = health.NewMonitor(health.Config{
			CheckInterval:      cfg.ChannelHealth.CheckInterval,
			StaleSocketTimeout: cfg.ChannelHealth.StaleSocketTimeout,
			StuckRunTimeout:    cfg.ChannelHealth.StuckRunTimeout,
			StartupGrace:       cfg.ChannelHealth.StartupGrace,
			MaxRestartsPerHour: cfg.ChannelHealth.MaxRestartsPerHour,
		}, channelManager, foundation.bus)
	}

	hookStore := hooks.NewInMemoryStore()
	hookExecutor := hooks.NewExecutor(hookStore)
	if err := hooks.SyncModuleHooks(ctx, hookStore, runtimeCore.moduleCatalog.HookDirProjections()); err != nil {
		log.Warn("plugin hooks sync failed", "error", err)
	}
	runtimeCore.component.WithHooks(hookExecutor)
	foundation.bus.Subscribe(&hookEventSink{executor: hookExecutor})

	pluginInstaller := plugin.NewInstaller(runtimeCore.plugins)
	if len(cfg.Plugins.Dirs) > 0 && strings.TrimSpace(cfg.Plugins.Dirs[0]) != "" {
		pluginInstaller.PluginDir = strings.TrimSpace(cfg.Plugins.Dirs[0])
	}

	var keychainWatcher *keychain.Watcher
	if keychainKeys := collectEffectiveKeychainKeys(cfg, runtimeCore.effectiveConfig); len(keychainKeys) > 0 {
		keychainWatcher = keychain.NewWatcher(time.Minute, keychainKeys)
		keychainWatcher.Subscribe(func(event keychain.ChangeEvent) {
			log.Info("keychain secret changed", "key", event.Key, "source", event.Source)
		})
		if err := keychainWatcher.Start(ctx); err != nil {
			log.Warn("keychain watcher start failed", "error", err)
		}
	}

	return &preparedOperatorAddons{
		allowlistManager:  allowlistManager,
		sandboxRunner:     sandboxRunner,
		isolationManager:  isolationManager,
		healthMonitor:     healthMonitor,
		hookExecutor:      hookExecutor,
		canvasHost:        canvasHost,
		discoveryResolver: discoveryResolver,
		pluginInstaller:   pluginInstaller,
		keychainWatcher:   keychainWatcher,
	}
}

func collectEffectiveKeychainKeys(cfg config.Config, resolver *controloverlay.Resolver) []string {
	return collectKeychainKeysFromInventory(currentEffectiveSecretInventory(cfg, resolver))
}

func operatorIsolationBaseDir(cfg config.Config) string {
	baseDir := strings.TrimSpace(cfg.Isolation.BaseDir)
	if baseDir != "" {
		return baseDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hopclaw", "workspaces")
}
