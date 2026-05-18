package bootstrap

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/i18n"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/internal/edition"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/usage"
)

type preparedRuntimeControlPlane struct {
	config             config.Config
	policyEngine       agent.PolicyEngine
	usageStore         usage.Store
	runtimeEventReader runtimesvc.EventReplayReader
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
	effectiveConfig    *controloverlay.Resolver
	effectiveLayers    []controlsnapshot.Layer
	snapshotBuilder    func(config.Config, []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot
	snapshotState      *effectiveSnapshotState
	governanceAdapters []controlgov.Adapter
}

func prepareRuntimeControlPlane(
	ctx context.Context,
	cfg config.Config,
	deps Dependencies,
	foundation *preparedBootstrapFoundation,
	component *agent.AgentComponent,
) (*preparedRuntimeControlPlane, error) {
	resolvedPolicy := resolveDefaultPolicy(cfg.Runtime.Profile, cfg.Skills)
	policyCfg := resolvedPolicy.Config
	policyCfg.SafePatterns = append([]string(nil), cfg.ExecApproval.SafePatterns...)
	policyCfg.BlockedCommands = append([]string(nil), cfg.Security.BlockedCommands...)
	policyCfg.DangerousTools = cfg.Security.DangerousTools

	basePolicyEngine := policy.NewDefaultEngine(policyCfg)
	var policyEngine agent.PolicyEngine = basePolicyEngine
	if deps.Policy != nil {
		policyEngine = policy.NewChainEngine(
			policy.Layer{Name: "base", Engine: basePolicyEngine},
			policy.Layer{Name: "overlay", Engine: deps.Policy},
		)
	}
	if policyEngine != nil {
		policy.WireSecurityAuditor(policyEngine, audit.NewSecurityAuditor(buildSecurityAuditorOptions(cfg, foundation.bus)...))
		component.WithPolicy(policyEngine)
	}
	if foundation.approvals != nil {
		component.WithApprovals(foundation.approvals)
	}
	component.WithEventBus(foundation.bus)

	usageStore, runtimeEventReader, err := prepareRuntimeUsageInfra(cfg, foundation)
	if err != nil {
		return nil, err
	}
	component.WithUsageTracker(usage.NewTracker(usageStore))

	configuredApprovalProviders, err := buildConfiguredApprovalProviders(cfg.ExecApproval.Providers)
	if err != nil {
		return nil, fmt.Errorf("build approval providers: %w", err)
	}
	configuredGovernanceAdapters, err := buildConfiguredGovernanceAdapters(cfg.Runtime.Governance.Adapters)
	if err != nil {
		return nil, fmt.Errorf("build governance adapters: %w", err)
	}

	allApprovalProviders := append([]controlapproval.Provider(nil), configuredApprovalProviders...)
	allApprovalProviders = append(allApprovalProviders, deps.ApprovalProviders...)
	approvalRegistry := controlapproval.NewProviderRegistry(approvalProviderDescriptors(cfg.ExecApproval.Providers), allApprovalProviders...)

	allGovernanceAdapters := append([]controlgov.Adapter(nil), configuredGovernanceAdapters...)
	allGovernanceAdapters = append(allGovernanceAdapters, deps.GovernanceAdapters...)
	governanceRegistry := controlgov.NewAdapterRegistry(governanceAdapterDescriptors(cfg.Runtime.Governance.Adapters), allGovernanceAdapters...)

	auditRegistry := controlaudit.NewRegistry(auditSinkDescriptors(cfg), enabledAuditSinkNames(cfg)...)
	snapshotLayers := effectiveConfigLayers(resolvedPolicy, deps.Policy != nil)
	snapshotState := newEffectiveSnapshotStateFromControlPlane(
		edition.Edition,
		deps.Policy != nil,
		&controlPlaneRuntimeState{
			resolvedPolicy:     resolvedPolicy,
			policyEngine:       policyEngine,
			approvalRegistry:   approvalRegistry,
			governanceRegistry: governanceRegistry,
			auditRegistry:      auditRegistry,
		},
	)
	snapshotBuilder := func(effectiveCfg config.Config, layers []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot {
		return snapshotState.Build(effectiveCfg, layers)
	}

	var effectiveStoreReader controloverlay.StoreReader
	if foundation.configStore != nil {
		effectiveStoreReader = foundation.configStore
	}
	effectiveResolver, err := controloverlay.NewResolver(ctx, foundation.baseConfig, effectiveStoreReader, controloverlay.Options{
		BaseLayers:      snapshotLayers,
		SnapshotBuilder: snapshotBuilder,
	})
	if err != nil {
		return nil, fmt.Errorf("init effective config resolver: %w", err)
	}
	cfg = effectiveResolver.RuntimeCurrent()
	i18n.ApplyConfiguredLocale(cfg.Locale)

	return &preparedRuntimeControlPlane{
		config:             cfg,
		policyEngine:       policyEngine,
		usageStore:         usageStore,
		runtimeEventReader: runtimeEventReader,
		approvalRegistry:   approvalRegistry,
		governanceRegistry: governanceRegistry,
		auditRegistry:      auditRegistry,
		effectiveConfig:    effectiveResolver,
		effectiveLayers:    snapshotLayers,
		snapshotBuilder:    snapshotBuilder,
		snapshotState:      snapshotState,
		governanceAdapters: governanceRegistry.Adapters(),
	}, nil
}

func buildSecurityAuditorOptions(cfg config.Config, bus eventbus.Bus) []audit.SecurityAuditorOption {
	opts := []audit.SecurityAuditorOption{
		audit.WithEventPublisher(bus),
		audit.WithPathChecker(audit.NewPathChecker(cfg.Security.AllowedPaths)),
		audit.WithContentValidator(
			audit.NewContentValidator().
				WithBlockedDomains(cfg.Security.BlockedDomains).
				WithMaxContentSize(cfg.Security.MaxContentSize),
		),
	}
	if len(cfg.Security.DangerousTools) > 0 {
		opts = append(opts, audit.WithDangerousTools(cfg.Security.DangerousTools))
	}
	if len(cfg.Security.CustomPatterns) > 0 {
		patternConfigs := make([]audit.CustomPatternConfig, len(cfg.Security.CustomPatterns))
		for i, p := range cfg.Security.CustomPatterns {
			patternConfigs[i] = audit.CustomPatternConfig{
				Name:     p.Name,
				Pattern:  p.Pattern,
				Severity: p.Severity,
				Category: p.Category,
			}
		}
		opts = append(opts, audit.WithCustomPatterns(patternConfigs))
	}
	return opts
}

func prepareRuntimeUsageInfra(cfg config.Config, foundation *preparedBootstrapFoundation) (usage.Store, runtimesvc.EventReplayReader, error) {
	if foundation.storeDB != nil {
		var runtimeEventReader runtimesvc.EventReplayReader
		if foundation.auditDB != nil {
			sqliteEventSink := store.NewSQLiteEventSink(foundation.auditDB)
			foundation.bus.Subscribe(eventbus.FilteredSink{
				Inner:  sqliteEventSink,
				Filter: eventbus.PersistDefaultRuntimeEvent,
			})
			runtimeEventReader = sqliteEventSink
		}
		return store.NewSQLiteUsageStore(foundation.storeDB), runtimeEventReader, nil
	}

	usageStore, err := initUsageStore(cfg.UsageStorage)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize usage store: %w", err)
	}
	return usageStore, nil, nil
}
