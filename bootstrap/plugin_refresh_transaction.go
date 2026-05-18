package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
)

type preparedPluginChannels struct {
	build               channelBuildResult
	changes             channelRuntimeDiff
	oldManagedProcesses map[string]*channelregistry.ManagedProcessPlan
}

type channelBuildResult struct {
	installations   []pluginChannelInstallation
	webhookAdapters map[string]*webhook.Adapter
	processManager  *plugin.ProcessManager
}

type pluginChannelInstallation struct {
	name           string
	adapter        channels.Adapter
	bridge         channelBridge
	managedProcess *channelregistry.ManagedProcessPlan
}

type pluginHookSyncPlan struct {
	store      hooks.Store
	desired    map[string]hooks.Hook
	updatedOld []hooks.Hook
	addedIDs   []string
	stale      []*hooks.Hook
	applied    bool
}

func buildPluginHookSyncPlan(ctx context.Context, store hooks.Store, moduleCatalog *modules.Store) (*pluginHookSyncPlan, error) {
	if store == nil {
		return nil, nil
	}
	var projections []modules.DirectoryProjection
	if moduleCatalog != nil {
		projections = moduleCatalog.HookDirProjections()
	}
	desired, err := hooks.CollectModuleHooks(projections)
	if err != nil {
		return nil, err
	}
	existing, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	plan := &pluginHookSyncPlan{
		store:   store,
		desired: desired,
	}
	for _, item := range existing {
		if item == nil || item.Source != "plugin" {
			continue
		}
		if _, ok := desired[pluginHookPlanKey(item.SourceRef, item.Name)]; ok {
			continue
		}
		plan.stale = append(plan.stale, item)
	}
	return plan, nil
}

func pluginHookPlanKey(sourceRef, name string) string {
	return strings.TrimSpace(sourceRef) + "::" + strings.TrimSpace(name)
}

func (p *pluginHookSyncPlan) Apply(ctx context.Context) error {
	if p == nil || p.store == nil {
		return nil
	}
	p.applied = true
	existing, err := p.store.List(ctx)
	if err != nil {
		return err
	}
	existingByKey := make(map[string]*hooks.Hook)
	for _, item := range existing {
		if item == nil || item.Source != "plugin" {
			continue
		}
		existingByKey[pluginHookPlanKey(item.SourceRef, item.Name)] = item
	}

	keys := make([]string, 0, len(p.desired))
	for key := range p.desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		desiredHook := p.desired[key]
		if existingHook, ok := existingByKey[key]; ok {
			desiredHook.ID = existingHook.ID
			desiredHook.CreatedAt = existingHook.CreatedAt
			p.updatedOld = append(p.updatedOld, *existingHook)
			if _, err := p.store.Update(ctx, desiredHook); err != nil {
				return fmt.Errorf("update plugin hook %q: %w", key, err)
			}
			continue
		}
		added, err := p.store.Add(ctx, desiredHook)
		if err != nil {
			return fmt.Errorf("add plugin hook %q: %w", key, err)
		}
		p.addedIDs = append(p.addedIDs, added.ID)
	}
	return nil
}

func (p *pluginHookSyncPlan) Commit(ctx context.Context) {
	if p == nil || p.store == nil || !p.applied {
		return
	}
	for _, item := range p.stale {
		if item == nil {
			continue
		}
		if err := p.store.Remove(ctx, item.ID); err != nil {
			log.Warn("remove stale plugin hook after refresh failed", "hook_id", item.ID, "error", err)
		}
	}
}

func (p *pluginHookSyncPlan) Rollback(ctx context.Context) {
	if p == nil || p.store == nil || !p.applied {
		return
	}
	for i := len(p.addedIDs) - 1; i >= 0; i-- {
		if err := p.store.Remove(ctx, p.addedIDs[i]); err != nil {
			log.Warn("remove newly added plugin hook during rollback failed", "hook_id", p.addedIDs[i], "error", err)
		}
	}
	for i := len(p.updatedOld) - 1; i >= 0; i-- {
		if _, err := p.store.Update(ctx, p.updatedOld[i]); err != nil {
			log.Warn("restore plugin hook during rollback failed", "hook_id", p.updatedOld[i].ID, "error", err)
		}
	}
}

type pluginRefreshTxn struct {
	app *App

	nextPlugins     *plugin.Manager
	nextMCP         pluginMCPRuntime
	nextMCPPlan     *preparedPluginMCPRuntime
	nextModelClient agent.ModelClient
	nextRouter      agent.ModelRouter
	nextToolExec    agent.ToolExecutor
	nextSkill       *preparedSkillService
	nextChannels    *preparedPluginChannels
	nextHookPlan    *pluginHookSyncPlan
	nextAgentRouter *runtimesvc.AgentRouter

	replaceModels      bool
	replaceMCP         bool
	replaceTools       bool
	replaceAgentRouter bool

	oldPlugins        *plugin.Manager
	oldMCP            pluginMCPRuntime
	oldModelClient    agent.ModelClient
	oldRouter         agent.ModelRouter
	oldToolExec       agent.ToolExecutor
	oldSkillService   *skill.Service
	oldSkillWatchStop context.CancelFunc
	oldAgentRouter    *runtimesvc.AgentRouter
	oldWebhooks       map[string]*webhook.Adapter
	oldChannelBridges []namedChannelBridge
	oldChangedBridges []namedChannelBridge
	oldPluginAdapters map[string]channels.Adapter
	oldProcessManager *plugin.ProcessManager

	nextActiveBridges []namedChannelBridge
	applied           bool
	finalized         bool
	appliedDomains    []runtimeTransactionDomain
}

func (t *pluginRefreshTxn) captureOldState() {
	if t == nil || t.app == nil {
		return
	}
	a := t.app
	t.oldPlugins = a.Plugins
	t.oldMCP = a.mcpRuntime
	if a.modelRuntime != nil {
		t.oldModelClient = a.modelRuntime.current()
	}
	if a.routerRuntime != nil {
		t.oldRouter = a.routerRuntime.current()
	}
	if a.toolRuntime != nil {
		t.oldToolExec = a.toolRuntime.current()
	}
	t.oldSkillService = a.SkillService
	t.oldSkillWatchStop = a.skillWatchStop
	if a.Runtime != nil {
		t.oldAgentRouter = a.Runtime.AgentRouter()
	}
	t.oldWebhooks = cloneWebhookAdapters(a.Webhooks)
	t.oldChannelBridges = append([]namedChannelBridge(nil), a.channelBridges...)
	t.oldProcessManager = a.processManager
}

func (t *pluginRefreshTxn) releasePrepared(ctx context.Context) {
	if t == nil {
		return
	}
	if t.nextSkill != nil && t.nextSkill.watchStop != nil {
		t.nextSkill.watchStop()
	}
	if t.nextMCPPlan != nil && t.nextMCPPlan.release != nil {
		t.nextMCPPlan.release(ctx)
	} else if t.nextMCP != nil && t.nextMCP != t.oldMCP {
		_ = t.nextMCP.Stop()
	}
	if t.nextChannels != nil && t.nextChannels.build.processManager != nil {
		t.nextChannels.build.processManager.Stop(ctx)
	}
}

func (t *pluginRefreshTxn) apply(ctx context.Context) error {
	if t == nil || t.app == nil {
		return nil
	}
	applied, err := applyRuntimeTransactionDomains(ctx, t.domains())
	t.appliedDomains = applied
	if err != nil {
		return formatRuntimeTransactionApplyError(applied, err)
	}
	t.applied = true
	return nil
}

func (t *pluginRefreshTxn) domains() []runtimeTransactionDomain {
	if t == nil || t.app == nil {
		return nil
	}
	a := t.app
	var domains []runtimeTransactionDomain

	if t.replaceModels {
		domains = append(domains, runtimeTransactionDomain{
			name: "models",
			apply: func(context.Context) error {
				if !a.customModel && a.modelRuntime != nil {
					a.modelRuntime.Swap(t.nextModelClient)
				}
				if !a.customRouter && a.routerRuntime != nil {
					a.routerRuntime.Swap(t.nextRouter)
				}
				return nil
			},
			rollback: func(context.Context) {
				if a.modelRuntime != nil {
					a.modelRuntime.Swap(t.oldModelClient)
				}
				if a.routerRuntime != nil && !a.customRouter {
					a.routerRuntime.Swap(t.oldRouter)
				}
			},
		})
	}

	if t.replaceTools && a.toolRuntime != nil {
		domains = append(domains, runtimeTransactionDomain{
			name: "tools",
			apply: func(context.Context) error {
				a.toolRuntime.Swap(t.nextToolExec)
				return nil
			},
			rollback: func(context.Context) {
				a.toolRuntime.Swap(t.oldToolExec)
			},
		})
	}

	if t.nextSkill != nil {
		domains = append(domains, runtimeTransactionDomain{
			name: "skills",
			apply: func(context.Context) error {
				a.applyPreparedSkillService(t.nextSkill)
				return nil
			},
			commit: func(context.Context) {
				if t.oldSkillWatchStop != nil {
					t.oldSkillWatchStop()
				}
			},
			rollback: func(context.Context) {
				a.applyPreparedSkillService(&preparedSkillService{
					service:   t.oldSkillService,
					watchStop: t.oldSkillWatchStop,
				})
				if t.nextSkill.watchStop != nil {
					t.nextSkill.watchStop()
				}
			},
		})
	}

	domains = append(domains, runtimeTransactionDomain{
		name: "plugin-runtime",
		apply: func(ctx context.Context) error {
			if t.replaceMCP && t.nextMCPPlan != nil && t.nextMCPPlan.apply != nil {
				if err := t.nextMCPPlan.apply(ctx); err != nil {
					return err
				}
			}
			if t.replaceAgentRouter && a.Runtime != nil {
				a.Runtime.SetAgentRouter(t.nextAgentRouter)
			}
			a.Plugins = t.nextPlugins
			if t.replaceMCP {
				a.mcpRuntime = t.nextMCP
			}
			return nil
		},
		commit: func(ctx context.Context) {
			if t.replaceMCP && t.nextMCPPlan != nil && t.nextMCPPlan.commit != nil {
				t.nextMCPPlan.commit(ctx)
			}
			if t.replaceMCP && t.oldMCP != nil && t.oldMCP != t.nextMCP {
				_ = t.oldMCP.Stop()
			}
		},
		rollback: func(ctx context.Context) {
			a.Plugins = t.oldPlugins
			if t.replaceMCP {
				a.mcpRuntime = t.oldMCP
			}
			if t.replaceAgentRouter && a.Runtime != nil {
				a.Runtime.SetAgentRouter(t.oldAgentRouter)
			}
			if t.replaceMCP && t.nextMCPPlan != nil && t.nextMCPPlan.rollback != nil {
				t.nextMCPPlan.rollback(ctx)
			}
			if t.replaceMCP && t.nextMCP != nil && t.nextMCP != t.oldMCP {
				_ = t.nextMCP.Stop()
			}
		},
	})

	if t.nextHookPlan != nil {
		domains = append(domains, runtimeTransactionDomain{
			name: "hooks",
			apply: func(ctx context.Context) error {
				return t.nextHookPlan.Apply(ctx)
			},
			commit: func(ctx context.Context) {
				t.nextHookPlan.Commit(ctx)
			},
			rollback: func(ctx context.Context) {
				t.nextHookPlan.Rollback(ctx)
			},
		})
	}

	if t.nextChannels != nil {
		domains = append(domains, runtimeTransactionDomain{
			name: "channels",
			apply: func(ctx context.Context) error {
				return t.applyChannels(ctx)
			},
			commit: func(ctx context.Context) {
				for _, entry := range t.oldChangedBridges {
					entry.bridge.Stop()
				}
				for name, adapter := range t.oldPluginAdapters {
					_ = adapter.Disconnect(context.Background())
					log.Debug("previous plugin channel disconnected after refresh", "channel", name)
				}
				if t.oldProcessManager != nil && t.oldProcessManager != t.app.processManager {
					t.oldProcessManager.Stop(ctx)
				}
			},
			rollback: func(ctx context.Context) {
				if a.Channels != nil {
					for _, entry := range t.nextActiveBridges {
						entry.bridge.Stop()
					}
					toRemove := append([]string(nil), t.nextChannels.changes.Added...)
					toRemove = append(toRemove, t.nextChannels.changes.Updated...)
					sort.Strings(toRemove)
					for _, name := range toRemove {
						if adapter, ok := a.Channels.Unregister(name); ok {
							_ = adapter.Disconnect(context.Background())
						}
						if a.processManager != nil {
							a.processManager.Remove(name)
						}
						clearChannelConnectWarning(a.startupWarnings, name)
					}
					oldNames := make([]string, 0, len(t.oldPluginAdapters))
					for name := range t.oldPluginAdapters {
						oldNames = append(oldNames, name)
					}
					sort.Strings(oldNames)
					for _, name := range oldNames {
						adapter := t.oldPluginAdapters[name]
						if err := a.Channels.Register(name, adapter); err != nil {
							log.Warn("restore plugin channel during rollback failed", "channel", name, "error", err)
						}
					}
					if t.oldProcessManager != nil {
						for _, name := range oldNames {
							plan := t.nextChannels.oldManagedProcesses[name]
							if plan == nil {
								continue
							}
							if err := t.oldProcessManager.Supervise(plan.Config, plan.Spawn); err != nil {
								log.Warn("restore managed plugin channel during rollback failed", "channel", name, "error", err)
							}
						}
					}
					for _, entry := range t.oldChangedBridges {
						entry.bridge.Start(ctx)
						clearChannelConnectWarning(a.startupWarnings, entry.name)
					}
				}
				if t.nextChannels.build.processManager != nil && t.nextChannels.build.processManager != t.oldProcessManager {
					t.nextChannels.build.processManager.Stop(ctx)
				}
				a.applyChannelRuntimeState(a.Channels, t.oldWebhooks, t.oldChannelBridges, t.oldProcessManager)
				if a.automationDeliverer != nil {
					a.automationDeliverer.MarkReady()
				}
				a.syncActiveChannelSessionsForReload(ctx, t.nextChannels.changes.ChangedNames(), t.oldChannelBridges)
			},
		})
	}

	domains = append(domains, runtimeTransactionDomain{
		name: "rewire",
		apply: func(context.Context) error {
			a.refreshModuleCatalogForConfigLocked(a.Config)
			a.wireBuiltinsForConfigLocked(a.Config)
			a.wireIntegrationGatewayForConfigLocked(a.Config)
			return nil
		},
		rollback: func(context.Context) {
			a.refreshModuleCatalogForConfigLocked(a.Config)
			a.wireBuiltinsForConfigLocked(a.Config)
			a.wireIntegrationGatewayForConfigLocked(a.Config)
		},
	})

	return domains
}

func (t *pluginRefreshTxn) applyChannels(ctx context.Context) error {
	if t == nil || t.app == nil || t.app.Channels == nil || t.nextChannels == nil || !t.nextChannels.changes.HasChanges() {
		return nil
	}
	a := t.app
	if a.automationDeliverer != nil {
		a.automationDeliverer.MarkNotReady()
		a.automationDeliverer.SetChannels(a.Channels)
	}

	t.oldPluginAdapters = make(map[string]channels.Adapter)
	t.oldChangedBridges = nil
	oldBridgeByName := bridgeByName(t.oldChannelBridges)

	nextWebhooks := cloneWebhookAdapters(t.oldWebhooks)
	if nextWebhooks == nil && len(t.nextChannels.build.webhookAdapters) > 0 {
		nextWebhooks = make(map[string]*webhook.Adapter, len(t.nextChannels.build.webhookAdapters))
	}

	toReplace := append([]string(nil), t.nextChannels.changes.Removed...)
	toReplace = append(toReplace, t.nextChannels.changes.Updated...)
	sort.Strings(toReplace)
	for _, name := range toReplace {
		if bridge, ok := oldBridgeByName[name]; ok {
			bridge.bridge.Stop()
			t.oldChangedBridges = append(t.oldChangedBridges, bridge)
		}
		if adapter, ok := a.Channels.Unregister(name); ok {
			t.oldPluginAdapters[name] = adapter
			if err := adapter.Disconnect(ctx); err != nil {
				return fmt.Errorf("disconnect plugin channel %q: %w", name, err)
			}
		}
		if t.oldProcessManager != nil {
			t.oldProcessManager.Remove(name)
		}
		if key, ok := pluginWebhookKeyFromChannelName(name); ok && nextWebhooks != nil {
			delete(nextWebhooks, key)
		}
		clearChannelConnectWarning(a.startupWarnings, name)
	}

	kept := make([]namedChannelBridge, 0, len(t.oldChannelBridges))
	for _, entry := range t.oldChannelBridges {
		if !t.nextChannels.changes.Contains(entry.name) {
			kept = append(kept, entry)
		}
	}

	installationByName := make(map[string]pluginChannelInstallation, len(t.nextChannels.build.installations))
	for _, installation := range t.nextChannels.build.installations {
		installationByName[installation.name] = installation
	}

	nextProcessManager := t.oldProcessManager
	if nextProcessManager == nil {
		nextProcessManager = t.nextChannels.build.processManager
	}

	toAdd := append([]string(nil), t.nextChannels.changes.Added...)
	toAdd = append(toAdd, t.nextChannels.changes.Updated...)
	sort.Strings(toAdd)

	started := make([]namedChannelBridge, 0, len(toAdd))
	for _, name := range toAdd {
		installation, ok := installationByName[name]
		if !ok {
			return fmt.Errorf("prepared plugin channel %q missing from refresh build", name)
		}
		if installation.managedProcess != nil {
			if nextProcessManager == nil {
				nextProcessManager = plugin.NewProcessManager()
			}
			if err := nextProcessManager.Supervise(installation.managedProcess.Config, installation.managedProcess.Spawn); err != nil {
				return fmt.Errorf("supervise plugin channel %q: %w", installation.name, err)
			}
		}
		if err := a.Channels.Register(installation.name, installation.adapter); err != nil {
			return fmt.Errorf("register plugin channel %q: %w", installation.name, err)
		}
		if err := installation.adapter.Connect(ctx); err != nil {
			log.Warn("plugin channel connect failed", "channel", installation.name, "error", err)
			recordChannelConnectWarning(a.startupWarnings, installation.name, err)
		} else {
			clearChannelConnectWarning(a.startupWarnings, installation.name)
			installation.bridge.Start(ctx)
			started = append(started, namedChannelBridge{
				name:   installation.name,
				bridge: installation.bridge,
			})
		}
		if key, ok := pluginWebhookKeyFromChannelName(installation.name); ok {
			if nextWebhooks == nil {
				nextWebhooks = make(map[string]*webhook.Adapter)
			}
			nextWebhooks[key] = t.nextChannels.build.webhookAdapters[key]
		}
	}

	t.nextActiveBridges = started
	a.applyChannelRuntimeState(a.Channels, nextWebhooks, append(kept, started...), nextProcessManager)
	if a.automationDeliverer != nil {
		a.automationDeliverer.MarkReady()
	}
	a.syncActiveChannelSessionsForReload(ctx, t.nextChannels.changes.ChangedNames(), append(kept, started...))
	return nil
}

func (t *pluginRefreshTxn) Commit(ctx context.Context) {
	if t == nil || t.finalized {
		return
	}
	t.finalized = true
	if !t.applied {
		t.releasePrepared(ctx)
		return
	}
	commitRuntimeTransactionDomains(ctx, t.appliedDomains)
}

func (t *pluginRefreshTxn) Rollback(ctx context.Context) {
	if t == nil || t.finalized {
		return
	}
	t.finalized = true
	if !t.applied {
		if t.nextHookPlan != nil {
			t.nextHookPlan.Rollback(ctx)
		}
		t.releasePrepared(ctx)
		return
	}

	if t.app == nil {
		if t.nextHookPlan != nil {
			t.nextHookPlan.Rollback(ctx)
		}
		t.releasePrepared(ctx)
		return
	}
	rollbackRuntimeTransactionDomains(ctx, t.appliedDomains)
	t.app.refreshModuleCatalogForConfigLocked(t.app.Config)
	t.app.wireBuiltinsForConfigLocked(t.app.Config)
	t.app.wireIntegrationGatewayForConfigLocked(t.app.Config)
}
