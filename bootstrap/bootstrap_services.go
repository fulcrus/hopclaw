package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	feishupairing "github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/config"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/diagnostics"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	"github.com/fulcrus/hopclaw/internal/update"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/model"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
)

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

func initLogging(cfg config.LoggingConfig) error {
	return logging.Init(logging.LogConfig{
		Level:           cfg.Level,
		Format:          cfg.Format,
		Output:          cfg.Output,
		FilePath:        cfg.FilePath,
		MaxSizeMB:       cfg.MaxSizeMB,
		RedactKeys:      cfg.RedactKeys,
		SubsystemLevels: cfg.SubsystemLevels,
		ConsoleCapture:  cfg.ConsoleCapture,
		Sampling: logging.SamplingConfig{
			Enabled:     cfg.Sampling.Enabled,
			InitialN:    cfg.Sampling.InitialN,
			ThereafterN: cfg.Sampling.ThereafterN,
			IntervalSec: cfg.Sampling.IntervalSec,
		},
	})
}

func initUpdateChecks(cfg config.UpdateConfig) {
	if !updatePolicyConfigured(cfg) {
		return
	}
	policy := update.DefaultPolicy()
	if cfg.Enabled != nil {
		policy.Enabled = *cfg.Enabled
	}
	if cfg.CheckOnStart != nil {
		policy.CheckOnStart = *cfg.CheckOnStart
	}
	if cfg.CheckInterval > 0 {
		policy.CheckInterval = cfg.CheckInterval
	}
	if channel := strings.TrimSpace(cfg.Channel); channel != "" {
		policy.Channel = channel
	}
	if manifestURL := strings.TrimSpace(cfg.ManifestURL); manifestURL != "" {
		policy.ManifestURL = manifestURL
	}
	if skipVersion := strings.TrimSpace(cfg.SkipVersion); skipVersion != "" {
		policy.SkipVersion = skipVersion
	}
	update.BackgroundCheck(policy)
}

func updatePolicyConfigured(cfg config.UpdateConfig) bool {
	return cfg.Enabled != nil ||
		cfg.CheckOnStart != nil ||
		cfg.CheckInterval > 0 ||
		strings.TrimSpace(cfg.Channel) != "" ||
		strings.TrimSpace(cfg.ManifestURL) != "" ||
		strings.TrimSpace(cfg.SkipVersion) != ""
}

func initDiagnostics(cfg config.DiagnosticsConfig) error {
	if !diagnosticsConfigured(cfg) {
		return nil
	}
	if dir := strings.TrimSpace(cfg.BugReportDir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create diagnostics bug_report_dir: %w", err)
		}
	}
	if diagnostics.CollectorEnabled(cfg) {
		if err := os.MkdirAll(diagnostics.CollectorDir(cfg), 0o755); err != nil {
			return fmt.Errorf("create diagnostics collector_dir: %w", err)
		}
		if strings.TrimSpace(cfg.CollectorAuthToken) == "" {
			return fmt.Errorf("diagnostics collector_enabled requires collector_auth_token")
		}
	}
	if telemetry.CollectorEnabled(cfg) {
		if err := os.MkdirAll(telemetry.CollectorDir(cfg), 0o755); err != nil {
			return fmt.Errorf("create telemetry collector_dir: %w", err)
		}
		if strings.TrimSpace(cfg.TelemetryCollectorAuthToken) == "" {
			return fmt.Errorf("diagnostics telemetry_collector_enabled requires telemetry_collector_auth_token")
		}
	}
	log.Info("diagnostics configured",
		"enabled", enabledOrDefault(cfg.Enabled, false),
		"bug_report_dir", strings.TrimSpace(cfg.BugReportDir),
		"include_logs", enabledOrDefault(cfg.IncludeLogs, false),
		"telemetry_enabled", enabledOrDefault(cfg.TelemetryEnabled, false),
		"telemetry_endpoint", strings.TrimSpace(cfg.TelemetryEndpoint),
		"telemetry_collector_enabled", enabledOrDefault(cfg.TelemetryCollectorEnabled, false),
		"telemetry_collector_dir", strings.TrimSpace(cfg.TelemetryCollectorDir),
		"crash_reports_enabled", enabledOrDefault(cfg.CrashReportsEnabled, false),
		"upload_url", strings.TrimSpace(cfg.UploadURL),
		"collector_enabled", diagnostics.CollectorEnabled(cfg),
		"collector_dir", diagnostics.CollectorDir(cfg),
	)
	return nil
}

func diagnosticsConfigured(cfg config.DiagnosticsConfig) bool {
	return cfg.Enabled != nil ||
		strings.TrimSpace(cfg.BugReportDir) != "" ||
		cfg.IncludeLogs != nil ||
		cfg.MaxLogBytes > 0 ||
		len(cfg.RedactPatterns) > 0 ||
		cfg.TelemetryEnabled != nil ||
		strings.TrimSpace(cfg.TelemetryEndpoint) != "" ||
		strings.TrimSpace(cfg.TelemetryToken) != "" ||
		cfg.TelemetryTimeout > 0 ||
		cfg.TelemetryDebugLog != nil ||
		cfg.TelemetryCollectorEnabled != nil ||
		strings.TrimSpace(cfg.TelemetryCollectorDir) != "" ||
		strings.TrimSpace(cfg.TelemetryCollectorAuthToken) != "" ||
		cfg.TelemetryCollectorMaxUploadBytes > 0 ||
		cfg.CrashReportsEnabled != nil ||
		strings.TrimSpace(cfg.UploadURL) != "" ||
		strings.TrimSpace(cfg.UploadToken) != "" ||
		cfg.UploadTimeout > 0 ||
		cfg.CollectorEnabled != nil ||
		strings.TrimSpace(cfg.CollectorDir) != "" ||
		strings.TrimSpace(cfg.CollectorAuthToken) != "" ||
		cfg.CollectorMaxUploadBytes > 0
}

func initTunnelSupport(cfg config.TunnelConfig) error {
	if !enabledOrDefault(cfg.Enabled, false) {
		return nil
	}
	provider := strings.TrimSpace(strings.ToLower(cfg.Provider))
	switch provider {
	case "ssh", "":
		if cfg.Host == "" {
			return fmt.Errorf("tunnel: ssh mode requires host to be set")
		}
	case "tailscale":
		// Tailscale validation is handled at Start time.
	default:
		return fmt.Errorf("tunnel: unsupported provider %q (supported: ssh, tailscale)", provider)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Memory / Embedding
// ---------------------------------------------------------------------------

func initMemoryStore(cfg config.Config, storeDB *sql.DB) (agent.MemoryStore, error) {
	layout := resolveStorageLayout(cfg)
	memoryPath := strings.TrimSpace(cfg.MemoryStorage)
	var store agent.MemoryStore
	type embeddableMemoryStore interface {
		SetEmbedding(agent.EmbeddingClient)
	}

	if cfg.MemoryStorage != "" {
		kvStore, err := agent.NewSQLiteKVStore(cfg.MemoryStorage)
		if err != nil {
			return nil, fmt.Errorf("init memory store: %w", err)
		}
		store = kvStore
		memoryPath = cfg.MemoryStorage
	} else if storeDB != nil {
		kvStore, err := agent.NewSQLiteKVStoreFromDB(storeDB)
		if err != nil {
			return nil, fmt.Errorf("init shared sqlite memory store: %w", err)
		}
		store = kvStore
		memoryPath = layout.knowledgeDBPath
	} else if !strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") {
		kvStore, err := agent.NewSQLiteKVStore(layout.knowledgeDBPath)
		if err != nil {
			return nil, fmt.Errorf("init default sqlite memory store: %w", err)
		}
		store = kvStore
		memoryPath = layout.knowledgeDBPath
	} else {
		store = agent.NewInMemoryKVStore()
	}

	if enabledOrDefault(cfg.Embedding.Enabled, false) {
		if embeddable, ok := store.(embeddableMemoryStore); ok {
			embeddingClient, err := initEmbeddingRegistry(cfg.Embedding)
			if err != nil {
				return nil, fmt.Errorf("init embedding registry: %w", err)
			}
			embeddable.SetEmbedding(embeddingClient)
		}
	}
	if governed := agent.NewGovernedMemoryStore(store); governed != nil {
		store = governed
	}
	mirrored, err := agent.NewMirroredMemoryStore(store, layout.memoryNotebook)
	if err != nil {
		return nil, fmt.Errorf("init mirrored memory store: %w", err)
	}
	if err := mirrored.Sync(context.Background()); err != nil {
		return nil, fmt.Errorf("sync memory notebook: %w", err)
	}
	store = mirrored
	switch {
	case strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") && strings.TrimSpace(cfg.MemoryStorage) == "":
		if enabledOrDefault(cfg.Embedding.Enabled, false) {
			log.Info("memory store: in-memory with embeddings", "model", cfg.Embedding.Model)
		} else {
			log.Info("memory store: in-memory")
		}
	default:
		if enabledOrDefault(cfg.Embedding.Enabled, false) {
			log.Info("memory store: persistent sqlite with embeddings", "path", memoryPath, "model", cfg.Embedding.Model, "notebook", layout.memoryNotebook)
		} else {
			log.Info("memory store: persistent sqlite", "path", memoryPath, "notebook", layout.memoryNotebook)
		}
	}
	return store, nil
}

func initKnowledgeService(cfg config.Config, knowledgeDB *sql.DB) (*knowledge.Service, error) {
	layout := resolveStorageLayout(cfg)
	var store knowledge.Store
	var err error
	switch {
	case knowledgeDB != nil:
		store, err = knowledge.NewSQLiteStoreFromDB(knowledgeDB)
	case !strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory"):
		store, err = knowledge.NewSQLiteStore(layout.knowledgeDBPath)
	default:
		store, err = knowledge.NewSQLiteStore(":memory:")
	}
	if err != nil {
		return nil, fmt.Errorf("init knowledge store: %w", err)
	}
	var embedding agent.EmbeddingClient
	if enabledOrDefault(cfg.Embedding.Enabled, false) {
		embedding, err = initEmbeddingRegistry(cfg.Embedding)
		if err != nil {
			return nil, fmt.Errorf("init embedding registry: %w", err)
		}
	}
	svc, err := knowledge.NewService(store, embedding)
	if err != nil {
		return nil, fmt.Errorf("init knowledge service: %w", err)
	}
	log.Info("knowledge service: ready", "db", layout.knowledgeDBPath, "semantic_search", embedding != nil)
	return svc, nil
}

func initChannelThreadBindings(cfg config.Config) *channels.ThreadBinding {
	base := strings.TrimSpace(cfg.Store.Path)
	if base == "" {
		base = ".hopclaw/state"
	}
	return channels.NewPersistentThreadBinding(filepath.Join(base, "channels", "thread-bindings.json"))
}

func initEmbeddingRegistry(cfg config.EmbeddingConfig) (agent.EmbeddingClient, error) {
	var client agent.EmbeddingClient
	if len(cfg.Providers) == 0 {
		client = initLegacyEmbeddingClient(cfg)
	} else {
		registry := model.NewEmbeddingRegistry()
		for name, pcfg := range cfg.Providers {
			c, err := buildEmbeddingProvider(pcfg)
			if err != nil {
				return nil, fmt.Errorf("build embedding provider %q: %w", name, err)
			}
			if info, ok := model.LookupEmbeddingModel(pcfg.Model); ok {
				c = model.NewBatchEmbeddingClient(c, info)
			}
			registry.Register(name, c)
		}
		defaultName := cfg.Provider
		if defaultName == "" {
			for name := range cfg.Providers {
				defaultName = name
				break
			}
		}
		registry.SetDefault(defaultName)
		if cfg.Fallback != "" {
			registry.SetFallback(cfg.Fallback)
		}
		client = registry
	}

	// Wrap with cache to avoid redundant API calls for identical texts.
	return model.NewCachedEmbeddingClient(client, cfg.CacheSize), nil
}

func initLegacyEmbeddingClient(cfg config.EmbeddingConfig) agent.EmbeddingClient {
	return model.NewEmbeddingClient(model.EmbeddingConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	})
}

func buildEmbeddingProvider(cfg config.EmbeddingProviderConfig) (agent.EmbeddingClient, error) {
	return model.NewEmbeddingClientForProvider(model.EmbeddingProviderAPI(cfg.API), model.EmbeddingClientBuildInput{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	})
}

// ---------------------------------------------------------------------------
// Skills
// ---------------------------------------------------------------------------

func initSkills(ctx context.Context, cfg config.SkillsConfig, workspaceRoot string, moduleCatalog *modules.Store) (*skill.Service, context.CancelFunc, error) {
	roots := make([]skill.DiscoveryRoot, 0)

	for _, dir := range cfg.Dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		roots = append(roots, skill.DiscoveryRoot{
			Kind:     skill.SourceWorkspace,
			Path:     dir,
			Priority: 500,
		})
	}

	if cfg.AutoDetect {
		defaults := DefaultDiscoveryRoots(workspaceRoot)
		existing := make(map[string]bool, len(roots))
		for _, r := range roots {
			existing[r.Path] = true
		}
		for _, dr := range defaults {
			if !existing[dr.Path] {
				roots = append(roots, dr)
			}
		}
	}

	for _, dir := range pluginSkillDirPaths(moduleCatalog) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		roots = append(roots, pluginSkillDiscoveryRoot(workspaceRoot, dir))
	}

	if len(roots) == 0 {
		return nil, nil, nil
	}
	service := skill.NewService(skill.ServiceConfig{
		Roots: roots,
		Evaluator: skill.Evaluator{
			SecretPresence: func(keys []string) map[string]skill.SecretStatus {
				if len(keys) == 0 {
					return nil
				}
				out := make(map[string]skill.SecretStatus, len(keys))
				for _, key := range keys {
					trimmed := strings.TrimSpace(key)
					if trimmed == "" {
						continue
					}
					value, ok := os.LookupEnv(trimmed)
					out[trimmed] = skill.SecretStatus{
						Resolved: ok && value != "",
						Source:   "runtime_env",
					}
				}
				return out
			},
		},
		WatchInterval: cfg.RefreshInterval,
		OnRefresh: func(snapshot skill.RegistrySnapshot) {
			if moduleCatalog == nil {
				return
			}
			moduleCatalog.SwapWith(func(current modules.Catalog) modules.Catalog {
				return modules.WithSkillModules(current, snapshot)
			})
		},
	})
	if _, err := service.Refresh(ctx); err != nil {
		return nil, nil, err
	}

	var stop context.CancelFunc
	if enabledOrDefault(cfg.AutoRefresh, true) {
		watchCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		watcher := service.Watcher(func(snapshot skill.RegistrySnapshot) {
			log.Info("skills refreshed", "fingerprint", snapshot.Fingerprint, "count", len(snapshot.Ordered), "blocked", len(snapshot.Blocked))
		})
		watcher.OnError = func(err error) {
			log.Warn("skills refresh failed", "error", err)
		}
		go func() {
			defer close(done)
			if err := watcher.Run(watchCtx); err != nil && err != context.Canceled {
				log.Warn("skills watcher stopped", "error", err)
			}
		}()
		stop = cancelAndWait(cancel, done)
	}
	return service, stop, nil
}

func pluginSkillDiscoveryRoot(workspaceRoot, path string) skill.DiscoveryRoot {
	root := skill.DiscoveryRoot{
		Kind: skill.SourcePlugin,
		Path: path,
	}
	if isBundledExtensionSkillDir(workspaceRoot, path) {
		root.Kind = skill.SourceBundled
		root.Priority = 200
	}
	return root
}

func pluginSkillDirPaths(moduleCatalog *modules.Store) []string {
	if moduleCatalog == nil {
		return nil
	}
	projections := moduleCatalog.SkillDirProjections()
	if len(projections) == 0 {
		return nil
	}
	paths := make([]string, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		path := strings.TrimSpace(projection.Path)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return paths
}

func isBundledExtensionSkillDir(workspaceRoot, path string) bool {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	path = strings.TrimSpace(path)
	if workspaceRoot == "" || path == "" {
		return false
	}
	bundledRoot := filepath.Clean(filepath.Join(workspaceRoot, "extensions"))
	rel, err := filepath.Rel(bundledRoot, filepath.Clean(path))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ---------------------------------------------------------------------------
// Cron helpers
// ---------------------------------------------------------------------------

func cronEnabled(cfg config.CronConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return strings.TrimSpace(cfg.StorePath) != ""
}

func initCronStore(cfg config.CronConfig) (*cronsvc.Store, error) {
	path := strings.TrimSpace(cfg.StorePath)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for cron store: %w", err)
		}
		path = filepath.Join(home, ".hopclaw", "cron-jobs.json")
	}
	return cronsvc.Load(path)
}

func watchEnabled(cfg config.WatchConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return strings.TrimSpace(cfg.StorePath) != ""
}

func initWatchStore(cfg config.WatchConfig) (*watch.Store, error) {
	path := strings.TrimSpace(cfg.StorePath)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for watch store: %w", err)
		}
		path = filepath.Join(home, ".hopclaw", "watch-jobs.json")
	}
	return watch.Load(path)
}

func wakeupEnabled(cfg config.WakeupConfig) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	return strings.TrimSpace(cfg.StorePath) != ""
}

func initWakeupStore(cfg config.WakeupConfig) (*wakeup.Store, error) {
	path := strings.TrimSpace(cfg.StorePath)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for wakeup store: %w", err)
		}
		path = filepath.Join(home, ".hopclaw", "wakeup-triggers.json")
	}
	return wakeup.Load(path)
}

func initDeviceAuth(cfg config.Config) (*deviceauth.Store, *deviceauth.PairingManager, error) {
	root := strings.TrimSpace(cfg.Store.Path)
	if root == "" || strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "memory") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve home dir for device auth: %w", err)
		}
		root = filepath.Join(home, ".hopclaw")
	}
	store := deviceauth.NewStore(root)
	if err := store.Load(); err != nil {
		return nil, nil, fmt.Errorf("load device auth store: %w", err)
	}
	pairing := deviceauth.NewPairingManager(store)
	pairing.Start()
	return store, pairing, nil
}

func initChannelPairing(cfg config.Config) (*feishupairing.Manager, error) {
	stateDir := filepath.Dir(strings.TrimSpace(cfg.Store.Path))
	if strings.TrimSpace(stateDir) == "" || stateDir == "." {
		stateDir = ".hopclaw"
	}
	storePath := filepath.Join(stateDir, "channels", "pairing.json")
	store := feishupairing.NewFileStore(storePath)
	return feishupairing.NewManager(store), nil
}

type runtimeAutomationSubmitter struct {
	runtime *runtimesvc.Service
}

func (s *runtimeAutomationSubmitter) Submit(ctx context.Context, req automation.SubmitRequest) (*runtimesvc.RunResult, error) {
	run, err := s.runtime.Submit(ctx, runtimesvc.SubmitRequest{
		SessionKey:   req.SessionKey,
		Content:      req.Content,
		Model:        req.Model,
		AutomationID: req.AutomationID,
		Metadata:     req.Metadata,
		Execute:      req.Execute,
	})
	if err != nil {
		return nil, err
	}
	return s.runtime.GetRunResult(ctx, run.ID)
}

func (s *runtimeAutomationSubmitter) GetRunResult(ctx context.Context, runID string) (*runtimesvc.RunResult, error) {
	return s.runtime.GetRunResult(ctx, runID)
}

type channelCronDeliverer struct {
	mu       sync.RWMutex
	channels *channelmgr.Manager
	ready    bool
	readyCh  chan struct{}
}

func newChannelCronDeliverer(channels *channelmgr.Manager) *channelCronDeliverer {
	return &channelCronDeliverer{
		channels: channels,
		readyCh:  make(chan struct{}),
	}
}

func (d *channelCronDeliverer) SetChannels(channels *channelmgr.Manager) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.channels = channels
	d.mu.Unlock()
}

func (d *channelCronDeliverer) MarkReady() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.ready {
		d.mu.Unlock()
		return
	}
	d.ready = true
	close(d.readyCh)
	d.mu.Unlock()
}

func (d *channelCronDeliverer) MarkNotReady() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.ready {
		d.mu.Unlock()
		return
	}
	d.ready = false
	d.readyCh = make(chan struct{})
	d.mu.Unlock()
}

func (d *channelCronDeliverer) readySignal() <-chan struct{} {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	ch := d.readyCh
	d.mu.RUnlock()
	return ch
}

func (d *channelCronDeliverer) manager() *channelmgr.Manager {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	manager := d.channels
	d.mu.RUnlock()
	return manager
}

func (d *channelCronDeliverer) DeliverMessage(ctx context.Context, target automation.DeliveryTarget, content string) error {
	if d == nil {
		return fmt.Errorf("cron: channel deliverer is not configured")
	}
	if ready := d.readySignal(); ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	channel := strings.TrimSpace(target.Channel)
	manager := d.manager()
	if manager == nil {
		return fmt.Errorf("cron: channel manager is not configured")
	}
	adapter, ok := manager.Get(channel)
	if !ok {
		return fmt.Errorf("cron: unknown channel %q", channel)
	}
	msg := channels.OutboundMessage{
		TargetID: strings.TrimSpace(target.Target),
		Content:  content,
	}
	if accountID := strings.TrimSpace(target.AccountID); accountID != "" {
		msg.Metadata = map[string]any{"account_id": accountID}
	}
	return adapter.Send(ctx, msg)
}

// ---------------------------------------------------------------------------
// Keychain helpers
// ---------------------------------------------------------------------------

func collectKeychainKeysFromInventory(inventory config.SecretRefInventory) []string {
	if len(inventory.Items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(inventory.Items))
	for _, item := range inventory.Items {
		switch item.Kind {
		case config.SecretRefKindEnv, config.SecretRefKindKeychain:
			if strings.TrimSpace(item.Locator) != "" {
				keys = append(keys, item.Locator)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}
