package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/mcp"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
	toolregistry "github.com/fulcrus/hopclaw/toolruntime/registry"
)

type pluginMCPRuntime interface {
	Start(ctx context.Context) error
	Stop() error
	Tools() []mcp.Tool
	CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error)
}

var newPluginMCPRuntime = func(configs []mcp.ServerConfig) pluginMCPRuntime {
	return mcp.NewManager(configs)
}

var buildPluginChannels = channelregistry.BuildPlugins
var validatePluginRefresh = validateAppIntegrity
var buildRuntimeToolStack = toolregistry.BuildRuntime

type dynamicModelClient struct {
	mu        sync.RWMutex
	client    agent.ModelClient
	estimator calibratedTokenEstimator
}

type calibratedTokenEstimator interface {
	Estimate(text string) int
	EstimateMessages(msgs []contextengine.Message) int
	RecordActual(estimated, actual int)
}

func newDynamicModelClient(client agent.ModelClient, estimator calibratedTokenEstimator) *dynamicModelClient {
	return &dynamicModelClient{client: client, estimator: estimator}
}

func (d *dynamicModelClient) Swap(client agent.ModelClient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.client = client
}

func (d *dynamicModelClient) current() agent.ModelClient {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

func (d *dynamicModelClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	client := d.current()
	if client == nil {
		return nil, agent.ErrModelClientNil
	}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	d.recordObservedPromptUsage(req, resp)
	return resp, nil
}

func (d *dynamicModelClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	client := d.current()
	if client == nil {
		return nil, agent.ErrModelClientNil
	}
	if sc, ok := client.(agent.StreamingModelClient); ok {
		resp, err := sc.ChatStream(ctx, req, cb)
		if err != nil {
			return nil, err
		}
		d.recordObservedPromptUsage(req, resp)
		return resp, nil
	}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	d.recordObservedPromptUsage(req, resp)
	if cb != nil {
		if resp != nil && resp.Message.Content != "" {
			cb.OnTextDelta(ctx, resp.Message.Content)
		}
		cb.OnComplete(ctx)
	}
	return resp, nil
}

func (d *dynamicModelClient) recordObservedPromptUsage(req agent.ChatRequest, resp *agent.ModelResponse) {
	if d == nil || d.estimator == nil || resp == nil || resp.Usage == nil || resp.Usage.PromptTokens <= 0 {
		return
	}
	estimated := estimateChatRequestTokens(d.estimator, req)
	if estimated <= 0 {
		return
	}
	d.estimator.RecordActual(estimated, resp.Usage.PromptTokens)
}

func estimateChatRequestTokens(estimator interface {
	Estimate(text string) int
	EstimateMessages(msgs []contextengine.Message) int
}, req agent.ChatRequest) int {
	if estimator == nil {
		return 0
	}
	total := req.Budget.EstimatedInputTokens
	if total <= 0 {
		if prompt := strings.TrimSpace(req.SystemPrompt); prompt != "" {
			total += estimator.Estimate(prompt)
		}
		total += estimator.EstimateMessages(req.Messages)
	}
	if len(req.Tools) > 0 {
		if raw, err := json.Marshal(req.Tools); err == nil {
			total += estimator.Estimate(string(raw))
		}
	}
	return total
}

type dynamicModelRouter struct {
	mu     sync.RWMutex
	router agent.ModelRouter
}

func newDynamicModelRouter(router agent.ModelRouter) *dynamicModelRouter {
	return &dynamicModelRouter{router: router}
}

func (d *dynamicModelRouter) Swap(router agent.ModelRouter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.router = router
}

func (d *dynamicModelRouter) current() agent.ModelRouter {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.router
}

func (d *dynamicModelRouter) Select(ctx context.Context, req modelrouter.RouteRequest) (modelrouter.RouteDecision, error) {
	router := d.current()
	if router == nil {
		return modelrouter.RouteDecision{}, fmt.Errorf("model router is not configured")
	}
	return router.Select(ctx, req)
}

func (d *dynamicModelRouter) ReportFailure(ctx context.Context, modelID string, class modelrouter.FailureClass) error {
	router := d.current()
	if router == nil {
		return nil
	}
	return router.ReportFailure(ctx, modelID, class)
}

func (d *dynamicModelRouter) ReportSuccess(ctx context.Context, modelID string) error {
	router := d.current()
	if router == nil {
		return nil
	}
	return router.ReportSuccess(ctx, modelID)
}

type dynamicToolExecutor struct {
	mu   sync.RWMutex
	exec agent.ToolExecutor
}

func newDynamicToolExecutor(exec agent.ToolExecutor) *dynamicToolExecutor {
	return &dynamicToolExecutor{exec: exec}
}

func (d *dynamicToolExecutor) Swap(exec agent.ToolExecutor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.exec = exec
}

func (d *dynamicToolExecutor) current() agent.ToolExecutor {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.exec
}

func (d *dynamicToolExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	exec := d.current()
	if exec == nil {
		return nil, agent.ErrToolExecutorNil
	}
	return exec.ExecuteBatch(ctx, run, session, calls)
}

func (d *dynamicToolExecutor) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	exec := d.current()
	provider, ok := exec.(agent.ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return provider.ToolDefinitions(session)
}

func (d *dynamicToolExecutor) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	exec := d.current()
	resolver, ok := exec.(agent.ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}

type dynamicSkillBinder struct {
	mu     sync.RWMutex
	binder contextengine.SkillBinder
}

func newDynamicSkillBinder(binder contextengine.SkillBinder) *dynamicSkillBinder {
	return &dynamicSkillBinder{binder: binder}
}

func (d *dynamicSkillBinder) Swap(binder contextengine.SkillBinder) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.binder = binder
}

func (d *dynamicSkillBinder) current() contextengine.SkillBinder {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.binder
}

func (d *dynamicSkillBinder) Snapshot() skill.RegistrySnapshot {
	binder := d.current()
	if isNilDynamicSkillBinder(binder) {
		return skill.RegistrySnapshot{}
	}
	return binder.Snapshot()
}

func (d *dynamicSkillBinder) BindSession(runtimeCtx skill.RuntimeContext) skill.SessionSkillSnapshot {
	binder := d.current()
	if isNilDynamicSkillBinder(binder) {
		return skill.SessionSkillSnapshot{}
	}
	return binder.BindSession(runtimeCtx)
}

func isNilDynamicSkillBinder(binder contextengine.SkillBinder) bool {
	if binder == nil {
		return true
	}
	value := reflect.ValueOf(binder)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}

func composeRuntimeTools(ctx context.Context, base agent.ToolExecutor, artifactStore artifact.Store, cfg config.Config, moduleCatalog *modules.Store, pluginMCP agent.ToolExecutor) (agent.ToolExecutor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := buildRuntimeToolStack(ctx, base, artifactStore, cfg, moduleCatalog, pluginMCP)
	if err != nil {
		return nil, err
	}
	return result.Executor, nil
}

func (a *App) RefreshPlugins(ctx context.Context) error {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	return a.refreshPluginsLocked(ctx, true)
}

func (a *App) refreshPluginsLocked(ctx context.Context, restartWatcher bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	plugins := plugin.NewManager()
	if enabledOrDefault(a.Config.Plugins.Enabled, true) {
		if err := plugins.LoadAll(resolvedPluginDirs(a.Config)); err != nil {
			return fmt.Errorf("load plugins: %w", err)
		}
	}

	txn := &pluginRefreshTxn{
		app:         a,
		nextPlugins: plugins,
	}
	nextModuleCatalog := newModuleCatalogStore(a.builtins, plugins)
	currentProviderProjections := pluginProviderProjections(a.ModuleCatalog)
	nextProviderProjections := pluginProviderProjections(nextModuleCatalog)
	currentToolProjections := pluginToolProjections(a.ModuleCatalog)
	nextToolProjections := pluginToolProjections(nextModuleCatalog)
	currentMCPConfigs := pluginMCPServerConfigs(a.ModuleCatalog)
	nextMCPConfigs := pluginMCPServerConfigs(nextModuleCatalog)
	currentAgentProjections := pluginAgentProjections(a.ModuleCatalog)
	nextAgentProjections := pluginAgentProjections(nextModuleCatalog)
	skillDirsChanged := !reflect.DeepEqual(pluginSkillDirPaths(a.ModuleCatalog), pluginSkillDirPaths(nextModuleCatalog))

	txn.replaceModels = !a.customModel && !reflect.DeepEqual(currentProviderProjections, nextProviderProjections)
	txn.replaceMCP = !reflect.DeepEqual(currentMCPConfigs, nextMCPConfigs)
	txn.replaceTools = a.toolRuntime != nil && (!reflect.DeepEqual(currentToolProjections, nextToolProjections) || txn.replaceMCP)
	txn.replaceAgentRouter = !reflect.DeepEqual(currentAgentProjections, nextAgentProjections)

	if txn.replaceModels {
		modelClient, router, err := initModelRuntime(a.Config.Models, nextModuleCatalog)
		if err != nil {
			txn.releasePrepared(ctx)
			return fmt.Errorf("rebuild model runtime: %w", err)
		}
		txn.nextModelClient = modelClient
		txn.nextRouter = router
	}

	var nextMCP *preparedPluginMCPRuntime
	var mcpErr error
	if txn.replaceMCP {
		nextMCP, mcpErr = preparePluginMCPRuntime(ctx, a.mcpRuntime, nextModuleCatalog)
		if nextMCP != nil {
			txn.nextMCP = nextMCP.runtime
			txn.nextMCPPlan = nextMCP
		}
	}
	var mcpExec agent.ToolExecutor
	if nextMCP != nil {
		mcpExec = nextMCP.executor
	} else if a.mcpRuntime != nil {
		mcpExec = toolruntime.NewMCPExecutor(a.mcpRuntime)
	}
	if mcpErr != nil {
		log.Warn("plugin mcp start degraded", "error", mcpErr)
	}

	if txn.replaceTools {
		tools, err := composeRuntimeTools(ctx, a.baseTools, a.Artifacts, a.Config, nextModuleCatalog, mcpExec)
		if err != nil {
			txn.releasePrepared(ctx)
			return fmt.Errorf("compose runtime tools: %w", err)
		}
		txn.nextToolExec = tools
	}

	if skillDirsChanged {
		skillService, skillWatchStop, err := initSkills(ctx, a.Config.Skills, a.Config.Tools.Builtins.Root, nextModuleCatalog)
		if err != nil {
			txn.releasePrepared(ctx)
			return fmt.Errorf("refresh skills: %w", err)
		}
		txn.nextSkill = &preparedSkillService{
			service:   skillService,
			watchStop: skillWatchStop,
		}
	}

	if a.HookExecutor != nil {
		plan, err := buildPluginHookSyncPlan(ctx, a.HookExecutor.Store(), nextModuleCatalog)
		if err != nil {
			txn.releasePrepared(ctx)
			return fmt.Errorf("refresh plugin hooks: %w", err)
		}
		txn.nextHookPlan = plan
	}

	if a.Channels != nil {
		channelChanges := diffRuntimeChannelConfigs(
			channelregistry.PluginRuntimeChannelConfigs(a.ModuleCatalog),
			channelregistry.PluginRuntimeChannelConfigs(nextModuleCatalog),
		)
		if channelChanges.HasChanges() {
			stagingChannels := channelmgr.New()
			result, err := buildPluginChannels(ctx, a.channelRuntimeDeps(a.Config, stagingChannels, nextModuleCatalog))
			if err != nil {
				txn.releasePrepared(ctx)
				return fmt.Errorf("reload plugin channels: %w", err)
			}
			currentResult, err := buildPluginChannels(ctx, a.channelRuntimeDeps(a.Config, channelmgr.New(), a.ModuleCatalog))
			if err != nil {
				txn.releasePrepared(ctx)
				return fmt.Errorf("snapshot current plugin channels: %w", err)
			}
			installations := make([]pluginChannelInstallation, 0, len(result.Installations))
			for _, installation := range result.Installations {
				installations = append(installations, pluginChannelInstallation{
					name:           installation.Name,
					adapter:        installation.Adapter,
					bridge:         installation.Bridge,
					managedProcess: installation.ManagedProcess,
				})
			}
			txn.nextChannels = &preparedPluginChannels{
				build: channelBuildResult{
					installations:   installations,
					webhookAdapters: cloneWebhookAdapters(result.WebhookAdapters),
					processManager:  result.ProcessManager,
				},
				changes:             channelChanges,
				oldManagedProcesses: collectManagedPluginProcesses(currentResult.Installations),
			}
		}
	}

	if txn.replaceAgentRouter {
		txn.nextAgentRouter = buildPluginAgentRouter(nextModuleCatalog)
	}
	txn.captureOldState()
	if err := txn.apply(ctx); err != nil {
		txn.Rollback(ctx)
		return err
	}
	if err := validatePluginRefresh(ctx, a); err != nil {
		txn.Rollback(ctx)
		return err
	}
	txn.Commit(ctx)

	// Watcher-triggered refreshes should not synchronously restart the watcher
	// from inside its own callback path; config-driven refreshes still do.
	if restartWatcher {
		a.requestPluginWatcherRestartLocked(ctx)
	}
	return nil
}

func startPluginMCP(ctx context.Context, moduleCatalog *modules.Store) (pluginMCPRuntime, agent.ToolExecutor, error) {
	return startPluginMCPWithConfigs(ctx, pluginMCPServerConfigs(moduleCatalog))
}

func buildPluginAgentRouter(moduleCatalog *modules.Store) *runtimesvc.AgentRouter {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.AgentProjections()
	if len(projections) == 0 {
		return nil
	}
	profiles := make([]runtimesvc.AgentProfile, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		profiles = append(profiles, runtimesvc.AgentProfile{
			Name:         projection.Name,
			Description:  strings.TrimSpace(projection.Description),
			SystemPrompt: strings.TrimSpace(projection.SystemPrompt),
			Model:        strings.TrimSpace(projection.Model),
			Tools:        append([]string(nil), projection.Tools...),
			Skills:       append([]string(nil), projection.Skills...),
			MaxTokens:    projection.MaxTokens,
		})
	}
	if len(profiles) == 0 {
		return nil
	}
	return runtimesvc.NewAgentRouter(profiles)
}

func pluginProviderProjections(moduleCatalog *modules.Store) []modules.ProviderProjection {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.ProviderProjections()
	out := make([]modules.ProviderProjection, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		out = append(out, projection)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginToolProjections(moduleCatalog *modules.Store) []modules.ToolProjection {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.ToolProjections()
	out := make([]modules.ToolProjection, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		out = append(out, projection)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginAgentProjections(moduleCatalog *modules.Store) []modules.AgentProjection {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.AgentProjections()
	out := make([]modules.AgentProjection, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		out = append(out, projection)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isPluginWebhookKey(key string) bool {
	return strings.Contains(key, "/")
}

func collectManagedPluginProcesses(installations []channelregistry.Installation) map[string]*channelregistry.ManagedProcessPlan {
	if len(installations) == 0 {
		return nil
	}
	out := make(map[string]*channelregistry.ManagedProcessPlan)
	for _, installation := range installations {
		if installation.ManagedProcess == nil {
			continue
		}
		out[installation.Name] = installation.ManagedProcess
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
