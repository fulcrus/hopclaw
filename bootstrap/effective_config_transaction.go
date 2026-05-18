package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type RefreshApplyTransaction interface {
	Commit(ctx context.Context)
	Rollback(ctx context.Context)
}

type noopRefreshApplyTxn struct{}

func (noopRefreshApplyTxn) Commit(context.Context)   {}
func (noopRefreshApplyTxn) Rollback(context.Context) {}

type refreshApplyTxn struct {
	app  *App
	plan RefreshPlan

	oldCfg config.Config
	newCfg config.Config

	nextModelClient agent.ModelClient
	nextRouter      agent.ModelRouter
	nextState       *controlPlaneRuntimeState
	nextSkills      *preparedSkillService
	nextHosts       *preparedHostRuntime
	nextTools       *preparedToolRuntime
	nextChannels    *preparedChannels

	oldModelClient agent.ModelClient
	oldRouter      agent.ModelRouter

	oldSkillService   *skill.Service
	oldSkillWatchStop context.CancelFunc

	oldManagedHelpers *managedHelpers
	oldBrowserClient  *browserclient.Client
	oldDesktopClient  *desktopclient.Client
	oldCapabilities   *capregistry.Registry

	oldBaseTools agent.ToolExecutor
	oldBuiltins  *toolruntime.Builtins
	oldToolExec  agent.ToolExecutor

	oldChannels        *channelmgr.Manager
	oldWebhooks        map[string]*webhook.Adapter
	oldChannelBridges  []namedChannelBridge
	oldProcessManager  *plugin.ProcessManager
	oldChannelAdapters map[string]channels.Adapter
	nextChannelBridges []namedChannelBridge

	oldSnapshotState        effectiveSnapshotRuntimeState
	oldApprovalSyncer       *controlapproval.Dispatcher
	oldGovernanceControl    *controlgov.ReliableDispatcher
	oldGovernanceDispatcher *controlgov.ReliableDispatcher
	oldGovernanceDeliveryDB *sql.DB
	oldApprovalTimeout      *approval.TimeoutService
	oldAuditSink            eventbus.Sink

	applied        bool
	finalized      bool
	appliedDomains []runtimeTransactionDomain
}

func (p *preparedChannels) cleanup(ctx context.Context) {
	if p == nil {
		return
	}
	cleanupChannelRuntime(ctx, p.manager, p.activeBridges, p.build.ProcessManager)
}

func cleanupChannelRuntime(ctx context.Context, manager *channelmgr.Manager, bridges []namedChannelBridge, processManager *plugin.ProcessManager) {
	for _, bridge := range bridges {
		bridge.bridge.Stop()
	}
	if manager != nil {
		if err := manager.DisconnectAll(ctx); err != nil {
			log.Warn("channel disconnect during refresh cleanup failed", "error", err)
		}
		for _, name := range manager.Names() {
			if adapter, ok := manager.Unregister(name); ok {
				_ = adapter.Disconnect(context.Background())
			}
		}
	}
	if processManager != nil {
		processManager.Stop(ctx)
	}
}

func cloneWebhookAdapters(input map[string]*webhook.Adapter) map[string]*webhook.Adapter {
	if input == nil {
		return nil
	}
	cloned := make(map[string]*webhook.Adapter, len(input))
	for key, adapter := range input {
		cloned[key] = adapter
	}
	return cloned
}

func (t *refreshApplyTxn) releasePrepared(ctx context.Context) {
	if t == nil {
		return
	}
	if t.nextSkills != nil && t.nextSkills.watchStop != nil {
		t.nextSkills.watchStop()
	}
	if t.nextHosts != nil {
		if err := t.nextHosts.cleanup(ctx); err != nil {
			log.Warn("stop prepared host runtime failed", "error", err)
		}
	}
	if t.nextChannels != nil {
		t.nextChannels.cleanup(ctx)
	}
	if t.nextState != nil {
		t.nextState.release(t.app.storeDB)
	}
}

func (t *refreshApplyTxn) captureOldState() {
	if t == nil || t.app == nil {
		return
	}
	a := t.app
	if a.modelRuntime != nil {
		t.oldModelClient = a.modelRuntime.current()
	}
	if a.routerRuntime != nil {
		t.oldRouter = a.routerRuntime.current()
	}
	t.oldSkillService = a.SkillService
	t.oldSkillWatchStop = a.skillWatchStop
	t.oldManagedHelpers = a.ManagedHelpers
	t.oldBrowserClient = a.browserClient
	t.oldDesktopClient = a.desktopClient
	t.oldCapabilities = a.Capabilities
	t.oldBaseTools = a.baseTools
	t.oldBuiltins = a.builtins
	if a.toolRuntime != nil {
		t.oldToolExec = a.toolRuntime.current()
	}
	t.oldChannels = a.Channels
	t.oldWebhooks = cloneWebhookAdapters(a.Webhooks)
	t.oldChannelBridges = append([]namedChannelBridge(nil), a.channelBridges...)
	t.oldProcessManager = a.processManager
	if a.snapshotState != nil {
		t.oldSnapshotState = a.snapshotState.current()
	}
	if a.approvalSyncer != nil {
		t.oldApprovalSyncer = a.approvalSyncer.current()
	}
	if a.governanceControl != nil {
		t.oldGovernanceControl = a.governanceControl.current()
	}
	t.oldGovernanceDispatcher = a.governanceDispatcher
	t.oldGovernanceDeliveryDB = a.governanceDeliveryDB
	t.oldApprovalTimeout = a.ApprovalTimeout
	if a.auditSink != nil {
		t.oldAuditSink = a.auditSink.current()
	}
}

func (t *refreshApplyTxn) apply(ctx context.Context) error {
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

func (t *refreshApplyTxn) domains() []runtimeTransactionDomain {
	if t == nil || t.app == nil {
		return nil
	}
	var domains []runtimeTransactionDomain
	for _, stage := range t.plan.RuntimeStages() {
		domains = append(domains, t.domainsForStage(stage)...)
	}
	return domains
}

func (t *refreshApplyTxn) domainsForStage(stage ReloadStage) []runtimeTransactionDomain {
	if t == nil || t.app == nil {
		return nil
	}
	a := t.app
	switch stage {
	case ReloadStageModels:
		if !t.plan.RebuildModels {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageModels),
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
		}}
	case ReloadStageModules:
		if t.nextSkills == nil {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageModules),
			apply: func(context.Context) error {
				a.applyPreparedSkillService(t.nextSkills)
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
				if t.nextSkills.watchStop != nil {
					t.nextSkills.watchStop()
				}
			},
		}}
	case ReloadStageHosts:
		if t.nextHosts == nil {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageHosts),
			apply: func(context.Context) error {
				a.applyHostRuntime(t.nextHosts)
				a.wireHostPackLocked()
				return nil
			},
			commit: func(ctx context.Context) {
				if err := stopManagedHelpers(ctx, t.oldManagedHelpers); err != nil {
					log.Warn("stop previous managed helpers after refresh failed", "error", err)
				}
			},
			rollback: func(ctx context.Context) {
				a.applyHostRuntime(&preparedHostRuntime{
					managedHelpers: t.oldManagedHelpers,
					browserClient:  t.oldBrowserClient,
					desktopClient:  t.oldDesktopClient,
					capabilities:   t.oldCapabilities,
				})
				a.wireHostPackLocked()
				if err := t.nextHosts.cleanup(ctx); err != nil {
					log.Warn("stop refreshed managed helpers during rollback failed", "error", err)
				}
			},
		}}
	case ReloadStageTools:
		if t.nextTools == nil {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageTools),
			apply: func(context.Context) error {
				if !a.customTools {
					a.baseTools = t.nextTools.baseTools
					a.builtins = t.nextTools.builtins
				}
				if a.toolRuntime != nil {
					a.toolRuntime.Swap(t.nextTools.runtime)
				}
				a.wireExtensionRegistryLocked()
				return nil
			},
			rollback: func(context.Context) {
				if !a.customTools {
					a.baseTools = t.oldBaseTools
					a.builtins = t.oldBuiltins
				}
				if a.toolRuntime != nil {
					a.toolRuntime.Swap(t.oldToolExec)
				}
				a.wireExtensionRegistryLocked()
			},
		}}
	case ReloadStageChannels:
		if t.nextChannels == nil {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageChannels),
			apply: func(ctx context.Context) error {
				return t.applyBuiltinChannelChanges(ctx)
			},
			commit: func(ctx context.Context) {
				t.commitBuiltinChannelChanges(ctx)
			},
			rollback: func(ctx context.Context) {
				t.rollbackBuiltinChannelChanges(ctx)
			},
		}}
	case ReloadStagePolicy:
		if t.nextState == nil || !t.plan.RebuildPolicy {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStagePolicy),
			apply: func(context.Context) error {
				a.applyRuntimePolicyEngine(t.nextState.policyEngine)
				return nil
			},
			rollback: func(context.Context) {
				a.applyRuntimePolicyEngine(t.oldSnapshotState.policyEngine)
			},
		}}
	case ReloadStageApprovals:
		if t.nextState == nil || (!t.plan.RebuildApproval && !t.plan.RebuildApprovalTimer) {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageApprovals),
			apply: func(ctx context.Context) error {
				if t.plan.RebuildApproval {
					a.applyRuntimeApprovalRegistry(t.nextState.approvalRegistry)
				}
				if t.plan.RebuildApprovalTimer {
					a.applyApprovalTimeout(ctx, t.oldApprovalTimeout, t.nextState.approvalTimeout)
				}
				return nil
			},
			rollback: func(ctx context.Context) {
				if t.plan.RebuildApprovalTimer {
					a.applyApprovalTimeout(ctx, a.ApprovalTimeout, t.oldApprovalTimeout)
				}
				if t.plan.RebuildApproval {
					a.applyRuntimeApprovalRegistry(t.oldSnapshotState.approvalRegistry)
					if a.approvalSyncer != nil {
						a.approvalSyncer.Swap(t.oldApprovalSyncer)
					}
				}
			},
		}}
	case ReloadStageGovernance:
		if t.nextState == nil || (!t.plan.RebuildGovernance && !t.plan.RebuildAudit) {
			return nil
		}
		return []runtimeTransactionDomain{{
			name: string(ReloadStageGovernance),
			apply: func(ctx context.Context) error {
				if t.plan.RebuildGovernance {
					if current := t.oldGovernanceControl; current != nil && current != t.nextState.governanceDispatch {
						current.Stop()
					} else if t.oldGovernanceControl == nil && t.oldGovernanceDispatcher != nil && t.oldGovernanceDispatcher != t.nextState.governanceDispatch {
						t.oldGovernanceDispatcher.Stop()
					}
					if t.nextState.governanceDispatch != nil {
						t.nextState.governanceDispatch.Start(ctx)
					}
					a.applyRuntimeGovernanceRegistry(t.nextState.governanceRegistry, t.nextState.governanceDispatch, t.nextState.governanceDB)
				}
				if t.plan.RebuildAudit {
					a.applyRuntimeAuditRegistry(ctx, t.nextState.auditRegistry, t.nextState.runtimeAuditSink)
				}
				return nil
			},
			rollback: func(ctx context.Context) {
				if t.plan.RebuildGovernance {
					if t.nextState.governanceDispatch != nil && t.nextState.governanceDispatch != t.oldGovernanceDispatcher {
						t.nextState.governanceDispatch.Stop()
					}
					if a.governanceControl != nil {
						a.governanceControl.replace(t.oldGovernanceControl)
					}
					if t.oldGovernanceControl != nil {
						t.oldGovernanceControl.Start(ctx)
					} else if t.oldGovernanceDispatcher != nil {
						t.oldGovernanceDispatcher.Start(ctx)
					}
					a.applyRuntimeGovernanceRegistry(t.oldSnapshotState.governanceRegistry, t.oldGovernanceDispatcher, t.oldGovernanceDeliveryDB)
				}
				if t.plan.RebuildAudit {
					a.applyRuntimeAuditRegistry(ctx, t.oldSnapshotState.auditRegistry, t.oldAuditSink)
				}
			},
		}}
	case ReloadStageProjections:
		var domains []runtimeTransactionDomain
		if t.nextSkills != nil || t.nextHosts != nil || t.nextTools != nil || t.nextChannels != nil {
			domains = append(domains, runtimeTransactionDomain{
				name: "projections:bindings",
				apply: func(context.Context) error {
					a.wireBuiltinsForConfigLocked(t.newCfg)
					return nil
				},
				rollback: func(context.Context) {
					a.wireBuiltinsForConfigLocked(t.oldCfg)
				},
			})
		}
		if t.nextSkills != nil || t.nextChannels != nil {
			domains = append(domains, runtimeTransactionDomain{
				name: "projections:gateway",
				apply: func(context.Context) error {
					a.wireIntegrationGatewayForConfigLocked(t.newCfg)
					return nil
				},
				rollback: func(context.Context) {
					a.wireIntegrationGatewayForConfigLocked(t.oldCfg)
				},
			})
		}
		if t.nextSkills != nil || t.nextHosts != nil || t.nextTools != nil || t.nextChannels != nil {
			domains = append(domains, runtimeTransactionDomain{
				name: string(ReloadStageProjections),
				apply: func(context.Context) error {
					a.refreshModuleCatalogForConfigLocked(t.newCfg)
					return nil
				},
			})
		}
		return domains
	}
	return nil
}

func (t *refreshApplyTxn) applyBuiltinChannelChanges(ctx context.Context) error {
	if t == nil || t.app == nil || t.nextChannels == nil || !t.nextChannels.changes.HasChanges() {
		return nil
	}
	a := t.app
	if a.Channels == nil {
		return nil
	}
	if a.automationDeliverer != nil {
		a.automationDeliverer.MarkNotReady()
		a.automationDeliverer.SetChannels(a.Channels)
	}

	t.oldChannelAdapters = make(map[string]channels.Adapter)
	t.nextChannelBridges = nil

	installationByName := make(map[string]channelregistry.Installation, len(t.nextChannels.build.Installations))
	for _, installation := range t.nextChannels.build.Installations {
		installationByName[installation.Name] = installation
	}

	nextWebhooks := cloneWebhookAdapters(a.Webhooks)
	keptBridges := make([]namedChannelBridge, 0, len(a.channelBridges))
	for _, bridge := range a.channelBridges {
		if !t.nextChannels.changes.Contains(bridge.name) {
			keptBridges = append(keptBridges, bridge)
		}
	}
	oldBridgeByName := bridgeByName(t.oldChannelBridges)

	toReplace := append([]string(nil), t.nextChannels.changes.Removed...)
	toReplace = append(toReplace, t.nextChannels.changes.Updated...)
	sort.Strings(toReplace)
	var disconnectErrs []error

	for _, name := range toReplace {
		if bridge, ok := oldBridgeByName[name]; ok {
			bridge.bridge.Stop()
		}
		if adapter, ok := a.Channels.Unregister(name); ok {
			t.oldChannelAdapters[name] = adapter
			if err := adapter.Disconnect(ctx); err != nil {
				disconnectErrs = append(disconnectErrs, fmt.Errorf("disconnect refreshed channel %q: %w", name, err))
			}
		}
		if key, ok := webhookKeyFromChannelName(name); ok && nextWebhooks != nil {
			delete(nextWebhooks, key)
		}
		clearChannelConnectWarning(a.startupWarnings, name)
	}
	if len(disconnectErrs) > 0 {
		return errors.Join(disconnectErrs...)
	}

	toAdd := append([]string(nil), t.nextChannels.changes.Added...)
	toAdd = append(toAdd, t.nextChannels.changes.Updated...)
	sort.Strings(toAdd)

	nextActive := append([]namedChannelBridge(nil), keptBridges...)
	for _, name := range toAdd {
		installation, ok := installationByName[name]
		if !ok {
			return fmt.Errorf("prepared channel %q missing from builtin refresh build", name)
		}
		if err := a.Channels.Register(name, installation.Adapter); err != nil {
			return fmt.Errorf("register refreshed channel %q: %w", name, err)
		}
		if err := installation.Adapter.Connect(ctx); err != nil {
			log.Warn("channel connect failed after per-channel refresh", "channel", name, "error", err)
			recordChannelConnectWarning(a.startupWarnings, name, err)
		} else {
			clearChannelConnectWarning(a.startupWarnings, name)
			bridge := namedChannelBridge{
				name:   name,
				bridge: installation.Bridge,
			}
			installation.Bridge.Start(ctx)
			t.nextChannelBridges = append(t.nextChannelBridges, bridge)
			nextActive = append(nextActive, bridge)
		}
		if key, ok := webhookKeyFromChannelName(name); ok {
			if nextWebhooks == nil {
				nextWebhooks = make(map[string]*webhook.Adapter)
			}
			nextWebhooks[key] = t.nextChannels.build.WebhookAdapters[key]
		}
	}

	a.applyChannelRuntimeState(a.Channels, nextWebhooks, nextActive, a.processManager)
	if a.automationDeliverer != nil {
		a.automationDeliverer.MarkReady()
	}
	a.syncActiveChannelSessionsForReload(ctx, t.nextChannels.changes.ChangedNames(), nextActive)
	return nil
}

func (t *refreshApplyTxn) commitBuiltinChannelChanges(ctx context.Context) {
	if t == nil || t.nextChannels == nil {
		return
	}

	transferred := append([]string(nil), t.nextChannels.changes.Added...)
	transferred = append(transferred, t.nextChannels.changes.Updated...)
	sort.Strings(transferred)
	for _, name := range transferred {
		if t.nextChannels.manager != nil {
			t.nextChannels.manager.Unregister(name)
		}
	}
	t.nextChannels.cleanup(ctx)
}

func (t *refreshApplyTxn) rollbackBuiltinChannelChanges(ctx context.Context) {
	if t == nil || t.app == nil {
		return
	}
	a := t.app
	if a.Channels != nil {
		for _, bridge := range t.nextChannelBridges {
			bridge.bridge.Stop()
		}

		changed := append([]string(nil), t.nextChannels.changes.ChangedNames()...)
		sort.Strings(changed)
		for _, name := range changed {
			if adapter, ok := a.Channels.Unregister(name); ok {
				_ = adapter.Disconnect(context.Background())
			}
		}

		restoreNames := make([]string, 0, len(t.oldChannelAdapters))
		for name := range t.oldChannelAdapters {
			restoreNames = append(restoreNames, name)
		}
		sort.Strings(restoreNames)
		for _, name := range restoreNames {
			if err := a.Channels.Register(name, t.oldChannelAdapters[name]); err != nil {
				log.Warn("restore builtin channel during rollback failed", "channel", name, "error", err)
			}
		}

		oldBridgeByName := bridgeByName(t.oldChannelBridges)
		for _, name := range restoreNames {
			if bridge, ok := oldBridgeByName[name]; ok {
				bridge.bridge.Start(ctx)
				clearChannelConnectWarning(a.startupWarnings, name)
			}
		}
	}

	a.applyChannelRuntimeState(a.Channels, t.oldWebhooks, t.oldChannelBridges, t.oldProcessManager)
	if a.automationDeliverer != nil {
		a.automationDeliverer.MarkReady()
	}
	a.syncActiveChannelSessionsForReload(ctx, t.nextChannels.changes.ChangedNames(), t.oldChannelBridges)
	if t.nextChannels != nil {
		t.nextChannels.cleanup(ctx)
	}
}

func (t *refreshApplyTxn) Commit(ctx context.Context) {
	if t == nil || t.finalized {
		return
	}
	t.finalized = true

	if !t.applied {
		t.releasePrepared(ctx)
		return
	}

	commitRuntimeTransactionDomains(ctx, t.appliedDomains)
	if t.nextState != nil && !t.plan.RebuildGovernance {
		t.nextState.release(t.app.storeDB)
	}
	if t.plan.RebuildGovernance && t.oldGovernanceDeliveryDB != nil && t.oldGovernanceDeliveryDB != t.app.governanceDeliveryDB && t.oldGovernanceDeliveryDB != t.app.storeDB {
		_ = t.oldGovernanceDeliveryDB.Close()
	}
}

func (t *refreshApplyTxn) Rollback(ctx context.Context) {
	if t == nil || t.finalized {
		return
	}
	t.finalized = true

	if !t.applied {
		t.releasePrepared(ctx)
		return
	}

	if t.app == nil {
		t.releasePrepared(ctx)
		return
	}

	rollbackRuntimeTransactionDomains(ctx, t.appliedDomains)
	if t.nextState != nil {
		t.nextState.release(t.app.storeDB)
	}
	if t.nextSkills != nil || t.nextHosts != nil || t.nextTools != nil || t.nextChannels != nil {
		t.app.refreshModuleCatalogForConfigLocked(t.oldCfg)
		t.app.wireBuiltinsForConfigLocked(t.oldCfg)
		t.app.wireIntegrationGatewayForConfigLocked(t.oldCfg)
	}
}
