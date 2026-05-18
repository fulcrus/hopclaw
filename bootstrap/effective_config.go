package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlpolicy "github.com/fulcrus/hopclaw/internal/controlplane/policypack"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

var buildBuiltinChannels = func(ctx context.Context, deps channelregistry.RuntimeDeps) (channelregistry.RuntimeBuildResult, error) {
	return channelregistry.BuildRuntime(ctx, deps, channelregistry.RuntimeBuildOptions{
		IncludeBuiltin: true,
	})
}
var validateEffectiveConfigRefresh = validateAppIntegrity

type committedRefreshError struct {
	err error
}

func (e *committedRefreshError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *committedRefreshError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func wrapCommittedRefreshError(err error) error {
	if err == nil {
		return nil
	}
	return &committedRefreshError{err: err}
}

func isCommittedRefreshError(err error) bool {
	var target *committedRefreshError
	return errors.As(err, &target)
}

type RefreshPlan struct {
	ChangeSet            config.ChangeSet         `json:"change_set"`
	Graph                ReloadGraph              `json:"graph"`
	PostCommit           []ReloadPostCommitAction `json:"post_commit"`
	RebuildModels        bool                     `json:"rebuild_models"`
	RebuildSkills        bool                     `json:"rebuild_skills"`
	RebuildHosts         bool                     `json:"rebuild_hosts"`
	RebuildTools         bool                     `json:"rebuild_tools"`
	RebuildChannels      bool                     `json:"rebuild_channels"`
	RebuildPolicy        bool                     `json:"rebuild_policy"`
	RebuildApproval      bool                     `json:"rebuild_approval"`
	RebuildGovernance    bool                     `json:"rebuild_governance"`
	RebuildAudit         bool                     `json:"rebuild_audit"`
	RebuildApprovalTimer bool                     `json:"rebuild_approval_timer"`
	RebuildChannelHealth bool                     `json:"rebuild_channel_health"`
	RefreshRuntimeConfig bool                     `json:"refresh_runtime_config"`
	RefreshCredentials   bool                     `json:"refresh_credentials"`
}

func (p RefreshPlan) HasStage(stage ReloadStage) bool {
	return p.Graph.HasStage(stage)
}

func (p RefreshPlan) RuntimeStages() []ReloadStage {
	return p.Graph.RuntimeStages()
}

func (p RefreshPlan) HasPostCommitAction(action ReloadPostCommitAction) bool {
	for _, candidate := range p.PostCommit {
		if candidate == action {
			return true
		}
	}
	return false
}

type ModuleReloader interface {
	Plan(diff config.ChangeSet, oldSnapshot, newSnapshot *controlsnapshot.EffectiveConfigSnapshot) RefreshPlan
	Apply(ctx context.Context, plan RefreshPlan, oldCfg, newCfg config.Config, oldSnapshot, newSnapshot *controlsnapshot.EffectiveConfigSnapshot) (RefreshApplyTransaction, error)
}

type controlPlaneRuntimeState struct {
	resolvedPolicy     controlpolicy.Resolved
	policyEngine       agent.PolicyEngine
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
	governanceDispatch *controlgov.ReliableDispatcher
	governanceDB       *sql.DB
	approvalTimeout    *approval.TimeoutService
	runtimeAuditSink   eventbus.Sink
}

func (s *controlPlaneRuntimeState) release(sharedDB *sql.DB) {
	if s == nil {
		return
	}
	if s.governanceDispatch != nil {
		s.governanceDispatch.Stop()
	}
	if s.governanceDB != nil && s.governanceDB != sharedDB {
		_ = s.governanceDB.Close()
	}
}

type appModuleReloader struct {
	app   *App
	state *controlPlaneRuntimeState
}

type preparedSkillService struct {
	service   *skill.Service
	watchStop context.CancelFunc
}

type preparedToolRuntime struct {
	baseTools agent.ToolExecutor
	builtins  *toolruntime.Builtins
	runtime   agent.ToolExecutor
}

type preparedChannels struct {
	manager       *channelmgr.Manager
	build         channelregistry.RuntimeBuildResult
	activeBridges []namedChannelBridge
	changes       channelRuntimeDiff
}

func (r appModuleReloader) Plan(diff config.ChangeSet, oldSnapshot, newSnapshot *controlsnapshot.EffectiveConfigSnapshot) RefreshPlan {
	plan := RefreshPlan{
		ChangeSet:            diff,
		RefreshRuntimeConfig: true,
		RefreshCredentials:   true,
	}
	if !diff.HasChanges() {
		plan.Graph, _ = newReloadGraph(ReloadStageConfig)
		return plan
	}
	plan.RebuildModels = diff.HasSection("models")
	plan.RebuildSkills = diff.HasSection("skills")
	plan.RebuildHosts = diff.HasSection("hosts")
	plan.RebuildTools = diff.HasSection("tools") || diff.HasSection("skills") || plan.RebuildHosts
	plan.RebuildChannels = diff.HasSection("channels")
	plan.RebuildPolicy = diff.HasSection("runtime") || diff.HasSection("skills") || diff.HasSection("exec_approval") || diff.HasSection("security") || diff.HasSection("tools")
	plan.RebuildApproval = plan.RebuildPolicy || diff.HasSection("exec_approval")
	plan.RebuildGovernance = diff.HasSection("runtime")
	plan.RebuildAudit = diff.HasSection("runtime")
	plan.RebuildApprovalTimer = diff.HasSection("exec_approval")
	plan.RebuildChannelHealth = diff.HasSection("channel_health") || diff.HasSection("channels")
	graphStages := []ReloadStage{ReloadStageConfig}
	if plan.RebuildModels {
		graphStages = append(graphStages, ReloadStageModels)
	}
	if plan.RebuildSkills {
		graphStages = append(graphStages, ReloadStageModules)
	}
	if plan.RebuildHosts {
		graphStages = append(graphStages, ReloadStageHosts)
	}
	if plan.RebuildTools {
		graphStages = append(graphStages, ReloadStageTools)
	}
	if plan.RebuildChannels {
		graphStages = append(graphStages, ReloadStageChannels)
	}
	if plan.RebuildPolicy {
		graphStages = append(graphStages, ReloadStagePolicy)
	}
	if plan.RebuildApproval || plan.RebuildApprovalTimer {
		graphStages = append(graphStages, ReloadStageApprovals)
	}
	if plan.RebuildGovernance || plan.RebuildAudit {
		graphStages = append(graphStages, ReloadStageGovernance)
	}
	if plan.RebuildSkills || plan.RebuildHosts || plan.RebuildTools || plan.RebuildChannels {
		graphStages = append(graphStages, ReloadStageProjections)
	}
	graph, err := newReloadGraph(graphStages...)
	if err != nil {
		panic(err)
	}
	plan.Graph = graph
	return plan
}

func (r appModuleReloader) Apply(ctx context.Context, plan RefreshPlan, oldCfg, newCfg config.Config, oldSnapshot, newSnapshot *controlsnapshot.EffectiveConfigSnapshot) (RefreshApplyTransaction, error) {
	if r.app == nil {
		return noopRefreshApplyTxn{}, nil
	}
	a := r.app
	var (
		nextModelClient agent.ModelClient
		nextRouter      agent.ModelRouter
	)

	if plan.RebuildModels && !a.customModel {
		modelClient, router, err := initModelRuntime(newCfg.Models, a.ModuleCatalog)
		if err != nil {
			return nil, fmt.Errorf("refresh model runtime: %w", err)
		}
		nextModelClient = modelClient
		nextRouter = router
	}

	txn := &refreshApplyTxn{
		app:             a,
		plan:            plan,
		oldCfg:          oldCfg,
		newCfg:          newCfg,
		nextModelClient: nextModelClient,
		nextRouter:      nextRouter,
		nextState:       r.state,
	}

	for _, stage := range plan.RuntimeStages() {
		switch stage {
		case ReloadStageModules:
			prepared, err := a.prepareSkillServiceLocked(ctx, newCfg)
			if err != nil {
				txn.releasePrepared(ctx)
				return nil, err
			}
			txn.nextSkills = prepared
		case ReloadStageHosts:
			prepared, err := a.prepareHostRuntimeLocked(newCfg)
			if err != nil {
				txn.releasePrepared(ctx)
				return nil, err
			}
			txn.nextHosts = prepared
		case ReloadStageTools:
			capabilities := a.Capabilities
			if txn.nextHosts != nil {
				capabilities = txn.nextHosts.capabilities
			}
			prepared, err := a.prepareToolRuntimeLocked(ctx, newCfg, capabilities)
			if err != nil {
				txn.releasePrepared(ctx)
				return nil, err
			}
			txn.nextTools = prepared
		case ReloadStageChannels:
			prepared, err := a.prepareChannelsLocked(ctx, oldCfg, newCfg)
			if err != nil {
				txn.releasePrepared(ctx)
				return nil, err
			}
			txn.nextChannels = prepared
		}
	}

	txn.captureOldState()
	if err := txn.apply(ctx); err != nil {
		txn.Rollback(ctx)
		return nil, err
	}
	return txn, nil
}

func (a *App) ApplyBaseConfig(ctx context.Context, base config.Config) error {
	if a == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	previousBase := a.BaseConfig
	if a.ConfigStore != nil {
		syncer := config.NewConfigSyncer(a.ConfigStore, config.DefaultSyncOptions())
		if _, err := syncer.Sync(ctx, &base); err != nil {
			return fmt.Errorf("sync base config: %w", err)
		}
	}
	if err := a.refreshEffectiveConfigLocked(ctx, base); err != nil {
		if a.ConfigStore != nil && !isCommittedRefreshError(err) {
			syncer := config.NewConfigSyncer(a.ConfigStore, config.DefaultSyncOptions())
			if _, rollbackErr := syncer.Sync(ctx, &previousBase); rollbackErr != nil {
				log.Warn("rollback base config sync failed", "error", rollbackErr)
			}
		}
		return err
	}
	return nil
}

func (a *App) RefreshEffectiveConfig(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	return a.refreshEffectiveConfigLocked(ctx, a.BaseConfig)
}

func (a *App) refreshEffectiveConfigLocked(ctx context.Context, base config.Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	oldBase := a.BaseConfig
	oldCfg := a.Config
	oldSnapshot := a.currentEffectiveSnapshotLocked()
	oldResolver := a.effectiveConfig
	oldLayers := append([]controlsnapshot.Layer(nil), a.effectiveLayers...)

	var nextCfg config.Config
	var nextSnapshot *controlsnapshot.EffectiveConfigSnapshot
	var nextResolver *controloverlay.Resolver
	var nextLayers []controlsnapshot.Layer
	var state *controlPlaneRuntimeState
	var err error
	var storeReader controloverlay.StoreReader
	if a.ConfigStore != nil {
		storeReader = a.ConfigStore
	}

	if a.effectiveConfig != nil {
		baseResolver, err := controloverlay.NewResolver(ctx, base, storeReader, controloverlay.Options{
			BaseLayers:      a.effectiveLayers,
			SnapshotBuilder: a.snapshotBuilder,
		})
		if err != nil {
			return err
		}
		state, err = a.buildControlPlaneStateLocked(ctx, baseResolver.RuntimeCurrent())
		if err != nil {
			return err
		}
		nextLayers = effectiveConfigLayers(state.resolvedPolicy, a.policyOverlay != nil)
		nextResolver, err = controloverlay.NewResolver(ctx, base, storeReader, controloverlay.Options{
			BaseLayers:      nextLayers,
			SnapshotBuilder: a.snapshotBuilder,
		})
		if err != nil {
			return err
		}
		nextCfg = nextResolver.RuntimeCurrent()
		nextSnapshot = nextResolver.Snapshot()
	} else {
		nextCfg = base
		nextCfg.ResolveSecrets(keychain.ResolveField)
		state, err = a.buildControlPlaneStateLocked(ctx, nextCfg)
		if err != nil {
			return err
		}
		nextLayers = effectiveConfigLayers(state.resolvedPolicy, a.policyOverlay != nil)
		nextSnapshot = a.buildEffectiveSnapshot(nextCfg, nextLayers)
	}

	reloader := appModuleReloader{app: a, state: state}
	plan := reloader.Plan(config.Diff(oldCfg, nextCfg), oldSnapshot, nextSnapshot)
	plan.PostCommit = buildRefreshPostCommitActions(plan, oldCfg, nextCfg)
	txn, applyErr := reloader.Apply(ctx, plan, oldCfg, nextCfg, oldSnapshot, nextSnapshot)
	if applyErr != nil {
		return applyErr
	}
	execution := &effectiveConfigRefreshExecution{
		app:          a,
		plan:         plan,
		txn:          txn,
		state:        state,
		oldBase:      oldBase,
		nextBase:     base,
		oldCfg:       oldCfg,
		nextCfg:      nextCfg,
		oldSnapshot:  oldSnapshot,
		nextSnapshot: nextSnapshot,
		oldResolver:  oldResolver,
		nextResolver: nextResolver,
		oldLayers:    oldLayers,
		nextLayers:   nextLayers,
	}
	return execution.run(ctx)
}
