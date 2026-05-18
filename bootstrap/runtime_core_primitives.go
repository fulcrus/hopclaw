package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type preparedRuntimeCorePrimitives struct {
	config            config.Config
	plugins           *plugin.Manager
	artifacts         artifact.Store
	capabilities      *capregistry.Registry
	extensionRegistry *extregistry.Registry
	moduleCatalog     *modules.Store
	skillService      *skill.Service
	knowledgeService  *knowledge.Service
	skillWatchStop    context.CancelFunc
	skillHub          skill.ClawHubClient
	managedHelpers    *managedHelpers
	browserClient     *browserclient.Client
	desktopClient     *desktopclient.Client
	memoryStore       agent.MemoryStore
	baseTools         agent.ToolExecutor
	builtins          *toolruntime.Builtins
	mcpRuntime        pluginMCPRuntime
	modelRuntime      *dynamicModelClient
	routerRuntime     *dynamicModelRouter
	toolRuntime       *dynamicToolExecutor
	skillBinder       *dynamicSkillBinder
	contextEngine     contextengine.ContextEngine
	runtimeProvider   agent.RuntimeContextProvider
}

func prepareRuntimeCorePrimitives(
	ctx context.Context,
	cfg config.Config,
	deps Dependencies,
	foundation *preparedBootstrapFoundation,
	appConfig func() config.Config,
) (*preparedRuntimeCorePrimitives, error) {
	pluginManager := plugin.NewManager()
	if enabledOrDefault(cfg.Plugins.Enabled, true) {
		if err := pluginManager.LoadAll(resolvedPluginDirs(cfg)); err != nil {
			log.Warn("plugin load error", "error", err)
		}
	}
	moduleCatalog := modules.NewStore(modules.BuildCatalog(
		runtimeConfigModules(cfg),
		buildRuntimeModuleCatalog(nil, pluginManager).Modules(),
	))

	skillService, skillWatchStop, err := initSkills(ctx, cfg.Skills, cfg.Tools.Builtins.Root, moduleCatalog)
	if err != nil {
		return nil, err
	}
	skillHub := initSkillHub(ctx, cfg.Skills)

	modelClient := deps.Model
	router := deps.Router
	if modelClient == nil {
		modelClient, router, err = initModelRuntime(cfg.Models, moduleCatalog)
		if err != nil {
			return nil, err
		}
	}

	artifactStore, err := resolveRuntimeArtifactStore(cfg, foundation, deps.Artifacts)
	if err != nil {
		return nil, err
	}
	memoryStore, err := initMemoryStore(cfg, foundation.knowledgeDB)
	if err != nil {
		return nil, err
	}
	knowledgeService, err := initKnowledgeService(cfg, foundation.knowledgeDB)
	if err != nil {
		return nil, err
	}

	managedHosts := initManagedHelpers(cfg)
	browserHostClient := newBrowserHostClient(cfg.Hosts.Browser, managedHosts.Browser)
	desktopHostClient := newDesktopHostClient(cfg.Hosts.Desktop, managedHosts.Desktop)
	capabilities := initCapabilities(browserHostClient, desktopHostClient)
	cfg = applyTrustedDesktopBuiltinDefaults(cfg)

	baseTools := deps.Tools
	var builtins *toolruntime.Builtins
	if baseTools == nil {
		tr, err := initTools(cfg, capabilities, artifactStore)
		if err != nil {
			return nil, fmt.Errorf("init tools: %w", err)
		}
		baseTools = tr.Executor
		builtins = tr.Builtins

		if capBlock := toolruntime.BuildToolPrompt(tr.Builtins, tr.Layer2); capBlock != "" {
			cfg.Agent.SystemPrompt = appendPromptSection(cfg.Agent.SystemPrompt, capBlock)
		}
		cfg.Agent.SystemPrompt = appendPromptSection(cfg.Agent.SystemPrompt, skillRecoveryPrompt(cfg.Skills))

		report := toolruntime.BuildCapabilityReport(tr.Builtins, tr.Layer2)
		log.Info("tools ready",
			"active", report.ActiveCount,
			"dormant", report.DormantCount,
			"dormant_groups", len(report.DormantGroups),
		)
	}

	moduleCatalog.Swap(modules.WithSkillModules(modules.BuildCatalog(
		runtimeConfigModules(cfg),
		buildRuntimeModuleCatalog(builtins, pluginManager).Modules(),
	), currentSkillSnapshot(skillService)))
	pluginMCP, pluginMCPExec, pluginMCPErr := startPluginMCP(ctx, moduleCatalog)
	if pluginMCPErr != nil {
		log.Warn("plugin mcp start degraded", "error", pluginMCPErr)
	}
	runtimeTools, err := composeRuntimeTools(ctx, baseTools, artifactStore, cfg, moduleCatalog, pluginMCPExec)
	if err != nil {
		return nil, fmt.Errorf("compose runtime tools: %w", err)
	}

	estimator := contextengine.NewCalibratedEstimator(contextengine.CharRatioEstimator{
		CharsPerToken:        4.0,
		ToolCharsPerToken:    2.0,
		EmptyMessageOverhead: 4,
	})
	modelRuntime := newDynamicModelClient(modelClient, estimator)
	routerRuntime := newDynamicModelRouter(router)
	toolRuntime := newDynamicToolExecutor(runtimeTools)
	extensionRegistry := extregistry.New(extregistry.Options{
		Tools:        toolRuntime,
		Capabilities: capabilities,
		Modules:      moduleCatalog,
	})
	skillBinder := newDynamicSkillBinder(skillService)
	var segmentWriter contextengine.SegmentWriter
	var segmentReader contextengine.SegmentReader
	var segmentSearcher contextengine.SegmentSearcher
	if foundation != nil {
		if writer, ok := foundation.sessions.(contextengine.SegmentWriter); ok {
			segmentWriter = writer
		}
		if reader, ok := foundation.sessions.(contextengine.SegmentReader); ok {
			segmentReader = reader
		}
		if searcher, ok := foundation.sessions.(contextengine.SegmentSearcher); ok {
			segmentSearcher = searcher
		}
	}
	var embeddingClient contextengine.EmbeddingClient
	if provider, ok := memoryStore.(interface{ EmbeddingClient() agent.EmbeddingClient }); ok {
		embeddingClient = provider.EmbeddingClient()
	}
	if embeddingClient == nil {
		segmentSearcher = nil
	}
	contextEngine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     cfg.Agent.SystemPrompt,
		IncludeSkillCatalog:  cfg.Skills.IncludeCatalog,
		DefaultContextWindow: cfg.Agent.DefaultContextWindow,
		DefaultOutputTokens:  4000,
		Estimator:            estimator,
		PreCompactHook:       contextengine.NewMemoryFlushHook(memoryStore),
		MemoryWriter:         memoryStore,
		MemoryReader:         newMemorySearchAdapter(memoryStore),
		SegmentWriter:        segmentWriter,
		SegmentReader:        segmentReader,
		SegmentSearcher:      segmentSearcher,
		EmbeddingClient:      embeddingClient,
	}, skillBinder)

	runtimeProvider := deps.Runtime
	if runtimeProvider == nil {
		runtimeProvider = newBootstrapRuntimeContextProvider(appConfig, cfg.Tools.Builtins.Root)
	}

	return &preparedRuntimeCorePrimitives{
		config:            cfg,
		plugins:           pluginManager,
		artifacts:         artifactStore,
		capabilities:      capabilities,
		extensionRegistry: extensionRegistry,
		moduleCatalog:     moduleCatalog,
		skillService:      skillService,
		knowledgeService:  knowledgeService,
		skillWatchStop:    skillWatchStop,
		skillHub:          skillHub,
		managedHelpers:    managedHosts,
		browserClient:     browserHostClient,
		desktopClient:     desktopHostClient,
		memoryStore:       memoryStore,
		baseTools:         baseTools,
		builtins:          builtins,
		mcpRuntime:        pluginMCP,
		modelRuntime:      modelRuntime,
		routerRuntime:     routerRuntime,
		toolRuntime:       toolRuntime,
		skillBinder:       skillBinder,
		contextEngine:     contextEngine,
		runtimeProvider:   runtimeProvider,
	}, nil
}

func resolveRuntimeArtifactStore(cfg config.Config, foundation *preparedBootstrapFoundation, preset artifact.Store) (artifact.Store, error) {
	if preset != nil {
		return preset, nil
	}
	if foundation.runtimeDB != nil {
		layout := resolveStorageLayout(cfg)
		artifactRoot := strings.TrimSpace(cfg.Runtime.Artifacts.Path)
		if artifactRoot == "" {
			artifactRoot = filepath.Join(layout.root, "artifacts")
		}
		artifactStore, err := store.NewSQLiteArtifactStore(foundation.runtimeDB, artifactRoot)
		if err != nil {
			return nil, fmt.Errorf("init sqlite artifact store: %w", err)
		}
		return artifactStore, nil
	}
	return initArtifacts(cfg)
}

// memorySearchAdapter adapts agent.MemoryStore to contextengine.MemoryReader.
type memorySearchAdapter struct {
	store agent.MemoryStore
}

func newMemorySearchAdapter(store agent.MemoryStore) contextengine.MemoryReader {
	if store == nil {
		return nil
	}
	return &memorySearchAdapter{store: store}
}

func (a *memorySearchAdapter) Search(ctx context.Context, query string) ([]contextengine.MemorySearchResult, error) {
	entries, err := a.store.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	results := make([]contextengine.MemorySearchResult, len(entries))
	for i, e := range entries {
		results[i] = contextengine.MemorySearchResult{Key: e.Key, Value: e.Value}
	}
	return results, nil
}

func applyTrustedDesktopBuiltinDefaults(cfg config.Config) config.Config {
	if !strings.EqualFold(strings.TrimSpace(cfg.Runtime.Profile), config.RuntimeProfileTrustedDesktop) {
		return cfg
	}
	if len(cfg.Tools.Builtins.AllowedPaths) > 0 {
		return cfg
	}
	home, _ := os.UserHomeDir()
	cfg.Tools.Builtins.AllowedPaths = []string{
		os.TempDir(),
		"/tmp",
		"/private/tmp",
	}
	if home != "" {
		cfg.Tools.Builtins.AllowedPaths = append(cfg.Tools.Builtins.AllowedPaths, home)
	}
	return cfg
}
