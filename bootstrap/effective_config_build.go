package bootstrap

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func (a *App) buildControlPlaneStateLocked(ctx context.Context, cfg config.Config) (*controlPlaneRuntimeState, error) {
	resolvedPolicy := resolveDefaultPolicy(cfg.Runtime.Profile, cfg.Skills)
	policyCfg := resolvedPolicy.Config
	policyCfg.SafePatterns = append([]string(nil), cfg.ExecApproval.SafePatterns...)
	policyCfg.BlockedCommands = append([]string(nil), cfg.Security.BlockedCommands...)
	policyCfg.DangerousTools = append([]string(nil), cfg.Security.DangerousTools...)

	basePolicyEngine := policy.NewDefaultEngine(policyCfg)
	var policyEngine agent.PolicyEngine = basePolicyEngine
	if a.policyOverlay != nil {
		policyEngine = policy.NewChainEngine(
			policy.Layer{Name: "base", Engine: basePolicyEngine},
			policy.Layer{Name: "overlay", Engine: a.policyOverlay},
		)
	}
	if policyEngine != nil {
		policy.WireSecurityAuditor(policyEngine, audit.NewSecurityAuditor(buildSecurityAuditorOptions(cfg, a.Bus)...))
		if a.GrantStore != nil {
			policy.WireGrantStore(policyEngine, a.GrantStore)
		}
	}

	configuredApprovalProviders, err := buildConfiguredApprovalProviders(cfg.ExecApproval.Providers)
	if err != nil {
		return nil, fmt.Errorf("build approval providers: %w", err)
	}
	allApprovalProviders := append([]controlapproval.Provider(nil), configuredApprovalProviders...)
	allApprovalProviders = append(allApprovalProviders, a.customApprovals...)
	approvalRegistry := controlapproval.NewProviderRegistry(approvalProviderDescriptors(cfg.ExecApproval.Providers), allApprovalProviders...)

	configuredGovernanceAdapters, err := buildConfiguredGovernanceAdapters(cfg.Runtime.Governance.Adapters)
	if err != nil {
		return nil, fmt.Errorf("build governance adapters: %w", err)
	}
	allGovernanceAdapters := append([]controlgov.Adapter(nil), configuredGovernanceAdapters...)
	allGovernanceAdapters = append(allGovernanceAdapters, a.customGovernance...)
	governanceRegistry := controlgov.NewAdapterRegistry(governanceAdapterDescriptors(cfg.Runtime.Governance.Adapters), allGovernanceAdapters...)
	auditRegistry := controlaudit.NewRegistry(auditSinkDescriptors(cfg), enabledAuditSinkNames(cfg)...)

	governanceDispatcher, governanceDB, err := newGovernanceDispatcher(cfg, a.storeDB, a.Bus, a.Runtime, governanceRegistry.Adapters())
	if err != nil {
		return nil, fmt.Errorf("init governance delivery store: %w", err)
	}
	runtimeAuditSink, err := newRuntimeAuditSink(cfg, a.auditDB)
	if err != nil {
		return nil, fmt.Errorf("init audit delivery store: %w", err)
	}

	return &controlPlaneRuntimeState{
		resolvedPolicy:     resolvedPolicy,
		policyEngine:       policyEngine,
		approvalRegistry:   approvalRegistry,
		governanceRegistry: governanceRegistry,
		auditRegistry:      auditRegistry,
		governanceDispatch: governanceDispatcher,
		governanceDB:       governanceDB,
		approvalTimeout:    a.buildApprovalTimeoutServiceLocked(cfg),
		runtimeAuditSink:   runtimeAuditSink,
	}, nil
}

func (a *App) buildApprovalTimeoutServiceLocked(cfg config.Config) *approval.TimeoutService {
	if a == nil {
		return nil
	}
	component := a.runtimeComponent()
	if component == nil {
		return nil
	}
	return newApprovalTimeoutService(cfg.ExecApproval, a.Approvals,
		func(ctx context.Context, ticketID string) (*approval.Ticket, error) {
			return component.ResolveApproval(ctx, ticketID, approval.Resolution{
				Status:     approval.StatusCancelled,
				ResolvedBy: "system_timeout",
				Note:       "approval timed out",
			})
		},
		a.Bus,
		a.Runtime,
		a.startupWarnings,
	)
}

func (a *App) prepareSkillServiceLocked(ctx context.Context, cfg config.Config) (*preparedSkillService, error) {
	skillService, skillWatchStop, err := initSkills(ctx, cfg.Skills, cfg.Tools.Builtins.Root, a.ModuleCatalog)
	if err != nil {
		return nil, fmt.Errorf("refresh skills: %w", err)
	}
	return &preparedSkillService{
		service:   skillService,
		watchStop: skillWatchStop,
	}, nil
}

func (a *App) prepareToolRuntimeLocked(ctx context.Context, cfg config.Config, capabilities *capregistry.Registry) (*preparedToolRuntime, error) {
	prepared := &preparedToolRuntime{
		baseTools: a.baseTools,
		builtins:  a.builtins,
	}
	if !a.customTools {
		result, err := initTools(cfg, capabilities, a.Artifacts)
		if err != nil {
			return nil, fmt.Errorf("refresh base tools: %w", err)
		}
		prepared.baseTools = result.Executor
		prepared.builtins = result.Builtins
	}

	var pluginMCPExec agent.ToolExecutor
	if a.mcpRuntime != nil {
		pluginMCPExec = toolruntime.NewMCPExecutor(a.mcpRuntime)
	}
	tools, err := composeRuntimeTools(ctx, prepared.baseTools, a.Artifacts, cfg, a.ModuleCatalog, pluginMCPExec)
	if err != nil {
		return nil, fmt.Errorf("compose runtime tools: %w", err)
	}
	prepared.runtime = tools
	return prepared, nil
}

func (a *App) prepareChannelsLocked(ctx context.Context, oldCfg, newCfg config.Config) (*preparedChannels, error) {
	if a == nil {
		return nil, nil
	}
	changes := diffBuiltinRuntimeChannels(oldCfg, newCfg)
	if !changes.HasChanges() {
		return &preparedChannels{changes: changes}, nil
	}
	manager := channelmgr.New()
	result, err := buildBuiltinChannels(ctx, a.channelRuntimeDeps(newCfg, manager, a.ModuleCatalog))
	if err != nil {
		return nil, fmt.Errorf("refresh channels: %w", err)
	}
	return &preparedChannels{
		manager: manager,
		build:   result,
		changes: changes,
	}, nil
}
