package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/authz"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/hooks"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

var log = logging.WithSubsystem("gateway")

// gatewayAuthComponents groups authentication and authorization dependencies
// used by Gateway handlers.
type gatewayAuthComponents struct {
	authChain         *AuthChain
	authInitErr       error
	authSessionStore  AuthSessionStore
	authSessionConfig AuthSessionConfig
	oauth2Provider    *OAuth2Provider
	authzDecider      authz.AuthorizationDecider
	policyEngine      policy.Engine
	credentials       config.SecretRefInventory
}

type gatewayRuntimeComponents struct {
	capabilities       *capregistry.Registry
	extensions         *extregistry.Registry
	runtime            *runtimesvc.Service
	webhooks           map[string]*webhook.Adapter
	channels           *channelmgr.Manager
	cron               *cron.Service
	watch              *watch.Service
	pairing            *pairing.Manager
	threadBindings     *channels.ThreadBinding
	heartbeat          *heartbeat.Service
	wire               *wire.Logger
	wakeup             *wakeup.Service
	allowlist          *allowlist.Manager
	sandbox            *sandbox.Runner
	approvals          approval.Store
	approvalProviders  controlplane.ApprovalProviderCatalog
	governanceAdapters controlplane.GovernanceAdapterCatalog
	auditSinks         controlplane.AuditSinkCatalog
	auditDelivery      audit.DeliveryController
	grantStore         *approval.GrantStore
	channelHealth      *health.Monitor
	hooks              *hooks.Executor
	usageStore         usage.Store
	discovery          discovery.Resolver
	wsHandler          *WSHandler
	uploads            *uploadStore
}

type gatewayPluginComponents struct {
	moduleCatalog   *modules.Store
	pluginInstaller *plugin.Installer
	pluginRuntime   PluginRuntimeController
}

type gatewayKnowledgeComponents struct {
	knowledge *knowledge.Service
}

type gatewaySkillComponents struct {
	skillService *skill.Service
	skillHub     skill.ClawHubClient
}

type gatewayHostComponents struct {
	browserClient  *browserclient.Client
	desktopClient  *desktopclient.Client
	deviceStore    *deviceauth.Store
	devicePairing  *deviceauth.PairingManager
	managedHelpers ManagedHelpersController
}

type gatewayConfigComponents struct {
	configWatcher      *config.Watcher
	configPath         string
	configMu           sync.Mutex // guards configPath read/write operations
	configStore        *store.ConfigStore
	effectiveCfg       controlplane.EffectiveConfigProvider
	configMutator      controlplane.ConfigMutator
	configReload       func(context.Context) error
	operationalWarning controlplane.OperationalWarningSource
}

// Gateway is the assembled HTTP entry point that wires runtime, operator, and
// integration surfaces into one handler tree.
type Gateway struct {
	publicServerHandler  http.Handler
	runtimeServerHandler http.Handler
	config               Config
	startAt              time.Time
	gatewayAuthComponents
	gatewayRuntimeComponents
	gatewayPluginComponents
	gatewayKnowledgeComponents
	gatewaySkillComponents
	gatewayHostComponents
	gatewayConfigComponents
}

// PluginRuntimeController refreshes plugin runtime state after plugin changes.
type PluginRuntimeController interface {
	RefreshPlugins(ctx context.Context) error
}

// ManagedHelpersController exposes status and reclaim for Browser/Desktop helpers (operator UI).
// Implemented by bootstrap.ManagedHelpersController when managed helpers are enabled.
type ManagedHelpersController interface {
	Status(ctx context.Context) (browser, desktop HelperState, err error)
	Reclaim(ctx context.Context, name string) error
}

// HelperState is the runtime state of one managed helper.
type HelperState struct {
	Status         string `json:"status"`           // "running" or "stopped"
	SessionCount   int    `json:"session_count"`    // when running
	LastUseAt      string `json:"last_use_at"`      // RFC3339
	IdleTimeoutSec int    `json:"idle_timeout_sec"` // 0 = no auto-stop
}

type helperStatusItem struct {
	Name string `json:"name"`
	HelperState
}

type helperStatusResponse struct {
	Browser HelperState        `json:"browser"`
	Desktop HelperState        `json:"desktop"`
	Helpers []helperStatusItem `json:"helpers"`
}

// Config carries the core services and registries required to construct a
// Gateway.
type Config struct {
	Version              string // build version string
	AuthToken            string // legacy single bearer token (still supported)
	AuthConfig           config.AuthConfig
	AuthZConfig          config.AuthZConfig
	AuthorizationDecider authz.AuthorizationDecider
	Diagnostics          config.DiagnosticsConfig
	Capabilities         *capregistry.Registry
	Extensions           *extregistry.Registry
	ModuleCatalog        *modules.Store
	Runtime              *runtimesvc.Service
	Channels             *channelmgr.Manager
	Webhooks             map[string]*webhook.Adapter // webhook ID → adapter
	Cron                 *cron.Service
	Pairing              *pairing.Manager

	// Production hardening.
	CORS      CORSConfig
	RateLimit RateLimitConfig
}

// AutomationServices groups optional automation subsystems that can be applied
// after gateway construction.
type AutomationServices struct {
	Cron      *cron.Service
	Watch     *watch.Service
	Heartbeat *heartbeat.Service
	Wire      *wire.Logger
	Wakeup    *wakeup.Service
}

// SupportServices groups approval, sandbox, and support-plane services for a
// Gateway.
type SupportServices struct {
	Approvals     approval.Store
	GrantStore    *approval.GrantStore
	Allowlist     *allowlist.Manager
	Sandbox       *sandbox.Runner
	Discovery     discovery.Resolver
	ChannelHealth *health.Monitor
	Hooks         *hooks.Executor
	UsageStore    usage.Store
}

// KernelServices groups control-plane services that back operator and runtime
// enforcement endpoints.
type KernelServices struct {
	ApprovalProviders    controlplane.ApprovalProviderCatalog
	GovernanceAdapters   controlplane.GovernanceAdapterCatalog
	AuditSinks           controlplane.AuditSinkCatalog
	AuditDelivery        audit.DeliveryController
	AuthorizationDecider authz.AuthorizationDecider
	PolicyEngine         policy.Engine
	Credentials          config.SecretRefInventory
	ThreadBindings       *channels.ThreadBinding
	WSHandler            *WSHandler
	DeviceStore          *deviceauth.Store
	DevicePairing        *deviceauth.PairingManager
	ConfigStore          *store.ConfigStore
	PluginRuntime        PluginRuntimeController
	EffectiveConfig      controlplane.EffectiveConfigProvider
	ConfigMutator        controlplane.ConfigMutator
	ConfigReloader       func(context.Context) error
	OperationalWarnings  controlplane.OperationalWarningSource
}

// IntegrationServices groups external integration services that the gateway
// exposes through HTTP routes.
type IntegrationServices struct {
	SkillService    *skill.Service
	SkillHub        skill.ClawHubClient
	Channels        *channelmgr.Manager
	Extensions      *extregistry.Registry
	ModuleCatalog   *modules.Store
	Webhooks        map[string]*webhook.Adapter
	PluginInstaller *plugin.Installer
}

// HostServices groups helper-daemon and capability services used by host
// management routes.
type HostServices struct {
	BrowserClient  *browserclient.Client
	DesktopClient  *desktopclient.Client
	Capabilities   *capregistry.Registry
	ManagedHelpers ManagedHelpersController
}

// KnowledgeServices groups optional knowledge subsystems for gateway routes.
type KnowledgeServices struct {
	Knowledge *knowledge.Service
}

// ConfigServices groups file-backed config watcher state for operator config
// routes.
type ConfigServices struct {
	Watcher *config.Watcher
	Path    string
}

// New constructs a Gateway, defaulting missing HTTP surfaces to `http.NotFoundHandler`.
func New(publicHandler, runtimeHandler http.Handler, cfg Config) *Gateway {
	authSetup := buildGatewayAuth(cfg)
	extensions := cfg.Extensions
	if extensions == nil {
		extensions = extregistry.New(extregistry.Options{
			Capabilities: cfg.Capabilities,
			Channels:     cfg.Channels,
			Tools:        runtimeToolInventory{runtime: cfg.Runtime},
		})
	}
	if publicHandler == nil {
		publicHandler = http.NotFoundHandler()
	}
	if runtimeHandler == nil {
		runtimeHandler = http.NotFoundHandler()
	}
	gw := &Gateway{
		publicServerHandler:  publicHandler,
		runtimeServerHandler: runtimeHandler,
		config:               cfg,
		startAt:              time.Now().UTC(),
		gatewayAuthComponents: gatewayAuthComponents{
			authChain:         authSetup.chain,
			authInitErr:       authSetup.err,
			authSessionStore:  authSetup.authSessionStore,
			authSessionConfig: authSetup.authSessionConfig,
			oauth2Provider:    authSetup.oauth2Provider,
			authzDecider:      authSetup.authzDecider,
		},
		gatewayRuntimeComponents: gatewayRuntimeComponents{
			capabilities: cfg.Capabilities,
			extensions:   extensions,
			runtime:      cfg.Runtime,
			webhooks:     cfg.Webhooks,
			channels:     cfg.Channels,
			cron:         cfg.Cron,
			pairing:      cfg.Pairing,
		},
		gatewayPluginComponents: gatewayPluginComponents{
			moduleCatalog: cfg.ModuleCatalog,
		},
	}
	if authSetup.err != nil {
		log.Warn("gateway auth initialization failed", "error", authSetup.err)
	}
	gw.initWebChatUploads()
	return gw
}

// ValidateIntegrity reports missing required gateway dependencies as one
// aggregated error.
func (g *Gateway) ValidateIntegrity() error {
	if g == nil {
		return errors.New("gateway integrity check failed: gateway is nil")
	}
	issues := make([]string, 0, 12)
	if g.publicServerHandler == nil {
		issues = append(issues, "public handler missing")
	}
	if g.runtimeServerHandler == nil {
		issues = append(issues, "runtime handler missing")
	}
	if g.runtime == nil {
		issues = append(issues, "runtime service missing")
	}
	if g.capabilities == nil {
		issues = append(issues, "capability registry missing")
	}
	if g.extensions == nil {
		issues = append(issues, "extension registry missing")
	}
	if g.channels == nil {
		issues = append(issues, "channel manager missing")
	}
	if g.threadBindings == nil {
		issues = append(issues, "thread binding manager missing")
	}
	if g.wsHandler == nil {
		issues = append(issues, "websocket handler missing")
	}
	if g.policyEngine == nil {
		issues = append(issues, "policy engine missing")
	}
	if g.approvalProviders == nil {
		issues = append(issues, "approval provider registry missing")
	}
	if g.governanceAdapters == nil {
		issues = append(issues, "governance adapter registry missing")
	}
	if g.auditSinks == nil {
		issues = append(issues, "audit sink registry missing")
	}
	if g.grantStore == nil {
		issues = append(issues, "grant store missing")
	}
	if g.effectiveCfg == nil {
		issues = append(issues, "effective config resolver missing")
	}
	if g.configMutator == nil {
		issues = append(issues, "config mutation service missing")
	}
	if g.configReload == nil {
		issues = append(issues, "config reload callback missing")
	}
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("gateway integrity check failed: %s", strings.Join(issues, ", "))
}

// ApplyAutomationServices installs a bundle of automation services onto the
// gateway.
func (g *Gateway) ApplyAutomationServices(services AutomationServices) {
	if g == nil {
		return
	}
	g.SetCron(services.Cron)
	g.SetWatch(services.Watch)
	g.SetHeartbeat(services.Heartbeat)
	g.SetWire(services.Wire)
	g.SetWakeup(services.Wakeup)
}

// ApplySupportServices installs support-plane services onto the gateway.
func (g *Gateway) ApplySupportServices(services SupportServices) {
	if g == nil {
		return
	}
	g.SetApprovals(services.Approvals)
	g.SetGrantStore(services.GrantStore)
	g.SetAllowlist(services.Allowlist)
	g.SetSandbox(services.Sandbox)
	g.SetDiscovery(services.Discovery)
	g.SetChannelHealth(services.ChannelHealth)
	g.SetHooks(services.Hooks)
	g.SetUsageStore(services.UsageStore)
}

// ApplyKernelServices installs control-plane services onto the gateway.
func (g *Gateway) ApplyKernelServices(services KernelServices) {
	if g == nil {
		return
	}
	g.SetApprovalProviderRegistry(services.ApprovalProviders)
	g.SetGovernanceAdapterRegistry(services.GovernanceAdapters)
	g.SetAuditSinkRegistry(services.AuditSinks)
	g.SetAuditDeliveryController(services.AuditDelivery)
	g.SetAuthorizationDecider(services.AuthorizationDecider)
	g.SetPolicyEngine(services.PolicyEngine)
	g.SetCredentialInventory(services.Credentials)
	g.SetThreadBindings(services.ThreadBindings)
	g.SetWSHandler(services.WSHandler)
	if services.DeviceStore != nil || services.DevicePairing != nil {
		g.SetDeviceAuth(services.DeviceStore, services.DevicePairing)
	}
	if services.ConfigStore != nil {
		g.SetConfigStore(services.ConfigStore)
	}
	if services.PluginRuntime != nil {
		g.SetPluginRuntime(services.PluginRuntime)
	}
	if services.EffectiveConfig != nil {
		g.SetEffectiveConfigResolver(services.EffectiveConfig)
	}
	if services.ConfigMutator != nil {
		g.SetConfigMutationService(services.ConfigMutator)
	}
	if services.ConfigReloader != nil {
		g.SetConfigMutationReloader(services.ConfigReloader)
	}
	if services.OperationalWarnings != nil {
		g.SetOperationalWarningSource(services.OperationalWarnings)
	}
}

// ApplyIntegrationServices installs integration-facing services onto the
// gateway.
func (g *Gateway) ApplyIntegrationServices(services IntegrationServices) {
	if g == nil {
		return
	}
	g.SetChannels(services.Channels)
	g.SetExtensionRegistry(services.Extensions)
	g.SetModuleCatalog(services.ModuleCatalog)
	g.SetWebhooks(services.Webhooks)
	g.SetSkillService(services.SkillService)
	g.SetSkillHub(services.SkillHub)
	g.SetPluginInstaller(services.PluginInstaller)
}

// ApplyHostServices installs host-helper services onto the gateway.
func (g *Gateway) ApplyHostServices(services HostServices) {
	if g == nil {
		return
	}
	g.SetBrowserClient(services.BrowserClient)
	g.SetDesktopClient(services.DesktopClient)
	g.SetCapabilities(services.Capabilities)
	g.SetManagedHelpers(services.ManagedHelpers)
}

// ApplyKnowledgeServices installs knowledge services onto the gateway.
func (g *Gateway) ApplyKnowledgeServices(services KnowledgeServices) {
	if g == nil {
		return
	}
	g.SetKnowledgeService(services.Knowledge)
}

// ApplyConfigServices installs file-backed config watcher state onto the
// gateway.
func (g *Gateway) ApplyConfigServices(services ConfigServices) {
	if g == nil {
		return
	}
	g.SetConfigWatcher(services.Watcher, services.Path)
}

// SetCron updates the cron service after construction.
func (g *Gateway) SetCron(svc *cron.Service) {
	g.cron = svc
}

// SetWatch updates the watch service after construction.
func (g *Gateway) SetWatch(svc *watch.Service) {
	g.watch = svc
}

// SetWebhooks updates the webhook adapters after construction.
func (g *Gateway) SetWebhooks(wh map[string]*webhook.Adapter) {
	g.webhooks = wh
}

// SetHeartbeat updates the heartbeat service after construction.
func (g *Gateway) SetHeartbeat(svc *heartbeat.Service) {
	g.heartbeat = svc
}

// SetWire updates the wire logger after construction.
func (g *Gateway) SetWire(l *wire.Logger) {
	g.wire = l
}

// SetWakeup updates the wakeup service after construction.
func (g *Gateway) SetWakeup(svc *wakeup.Service) {
	g.wakeup = svc
}

// SetThreadBindings updates the thread binding store after construction.
func (g *Gateway) SetThreadBindings(bindings *channels.ThreadBinding) {
	g.threadBindings = bindings
}

// SetAllowlist updates the allowlist manager after construction.
func (g *Gateway) SetAllowlist(m *allowlist.Manager) {
	g.allowlist = m
}

// SetSandbox updates the sandbox runner after construction.
func (g *Gateway) SetSandbox(r *sandbox.Runner) {
	g.sandbox = r
}

// SetApprovals updates the approval store after construction.
func (g *Gateway) SetApprovals(s approval.Store) {
	g.approvals = s
	if g.runtime != nil {
		g.runtime.WithApprovals(s)
	}
}

// SetApprovalProviderRegistry updates the approval provider registry after construction.
func (g *Gateway) SetApprovalProviderRegistry(registry controlplane.ApprovalProviderCatalog) {
	g.approvalProviders = registry
}

// SetGovernanceAdapterRegistry updates the governance adapter registry after construction.
func (g *Gateway) SetGovernanceAdapterRegistry(registry controlplane.GovernanceAdapterCatalog) {
	g.governanceAdapters = registry
}

// SetAuditSinkRegistry updates the audit sink registry after construction.
func (g *Gateway) SetAuditSinkRegistry(registry controlplane.AuditSinkCatalog) {
	g.auditSinks = registry
}

// SetAuditDeliveryController updates the audit delivery controller after construction.
func (g *Gateway) SetAuditDeliveryController(controller audit.DeliveryController) {
	g.auditDelivery = controller
}

// SetGrantStore updates the grant store after construction.
func (g *Gateway) SetGrantStore(gs *approval.GrantStore) {
	g.grantStore = gs
}

// SetPolicyEngine updates the control-plane policy engine after construction.
func (g *Gateway) SetPolicyEngine(engine policy.Engine) {
	g.policyEngine = engine
}

// SetAuthorizationDecider updates the authorization backend after construction.
func (g *Gateway) SetAuthorizationDecider(decider authz.AuthorizationDecider) {
	if decider == nil {
		g.authzDecider = authz.OpenDecider{}
		return
	}
	g.authzDecider = decider
}

// SetCredentialInventory updates the sanitized credential inventory after construction.
func (g *Gateway) SetCredentialInventory(inventory config.SecretRefInventory) {
	g.credentials = inventory
}

// SetChannelHealth updates the channel health monitor after construction.
func (g *Gateway) SetChannelHealth(m *health.Monitor) {
	g.channelHealth = m
	if g.extensions != nil {
		g.extensions.SetChannelHealth(m)
	}
}

// SetChannels updates the channel manager after construction.
func (g *Gateway) SetChannels(mgr *channelmgr.Manager) {
	g.channels = mgr
	if g.extensions != nil {
		g.extensions.SetChannels(mgr)
	}
}

// SetExtensionRegistry updates the unified extension registry after construction.
func (g *Gateway) SetExtensionRegistry(reg *extregistry.Registry) {
	g.extensions = reg
	if g.extensions != nil && g.channelHealth != nil {
		g.extensions.SetChannelHealth(g.channelHealth)
	}
	if g.extensions != nil && g.moduleCatalog != nil {
		g.extensions.SetModules(g.moduleCatalog)
	}
}

// SetHooks updates the hook executor after construction.
func (g *Gateway) SetHooks(exec *hooks.Executor) {
	g.hooks = exec
}

// SetUsageStore updates the usage store after construction.
func (g *Gateway) SetUsageStore(s usage.Store) {
	g.usageStore = s
}

// SetDiscovery updates the discovery resolver after construction.
func (g *Gateway) SetDiscovery(r discovery.Resolver) {
	g.discovery = r
}

// SetWSHandler updates the WebSocket handler after construction.
func (g *Gateway) SetWSHandler(h *WSHandler) {
	g.wsHandler = h
}

// SetConfigWatcher updates the config watcher and path after construction.
func (g *Gateway) SetConfigWatcher(w *config.Watcher, path string) {
	g.configWatcher = w
	g.configPath = path
}

// SetSkillService updates the skill service after construction.
func (g *Gateway) SetSkillService(s *skill.Service) {
	g.skillService = s
}

// SetSkillHub updates the ClawHub client after construction.
func (g *Gateway) SetSkillHub(h skill.ClawHubClient) {
	g.skillHub = h
}

// SetKnowledgeService updates the knowledge source service after construction.
func (g *Gateway) SetKnowledgeService(svc *knowledge.Service) {
	g.knowledge = svc
}

// SetModuleCatalog updates the unified module catalog after construction.
func (g *Gateway) SetModuleCatalog(store *modules.Store) {
	g.moduleCatalog = store
	if g.extensions != nil {
		g.extensions.SetModules(store)
	}
}

// SetPluginInstaller updates the plugin installer after construction.
func (g *Gateway) SetPluginInstaller(inst *plugin.Installer) {
	g.pluginInstaller = inst
}

// SetPluginRuntime updates the plugin runtime refresher after construction.
func (g *Gateway) SetPluginRuntime(runtime PluginRuntimeController) {
	g.pluginRuntime = runtime
}

// WSHandler returns the currently installed WebSocket handler.
func (g *Gateway) WSHandler() *WSHandler {
	if g == nil {
		return nil
	}
	return g.wsHandler
}

// SetConfigStore updates the config store after construction.
func (g *Gateway) SetConfigStore(s *store.ConfigStore) {
	g.configStore = s
}

// SetOperationalWarningSource updates the source of actionable degraded runtime conditions.
func (g *Gateway) SetOperationalWarningSource(source controlplane.OperationalWarningSource) {
	g.operationalWarning = source
}

// SetEffectiveConfigResolver updates the merged effective-config provider after construction.
func (g *Gateway) SetEffectiveConfigResolver(resolver controlplane.EffectiveConfigProvider) {
	g.effectiveCfg = resolver
	if g.configMutator != nil {
		g.configMutator.SetProvider(resolver)
	}
}

// SetConfigMutationService installs the canonical overlay-backed config
// mutation entry point used by operator config/model/channel handlers.
func (g *Gateway) SetConfigMutationService(service controlplane.ConfigMutator) {
	g.configMutator = service
	if g.configMutator != nil && g.effectiveCfg != nil {
		g.configMutator.SetProvider(g.effectiveCfg)
	}
}

// SetConfigMutationReloader installs the callback used after config-store or
// file-backed config mutations so runtime state can refresh from the effective
// config path.
func (g *Gateway) SetConfigMutationReloader(fn func(context.Context) error) {
	g.configReload = fn
}

// SetBrowserClient updates the browser daemon client after construction.
func (g *Gateway) SetBrowserClient(c *browserclient.Client) {
	g.browserClient = c
}

// SetDesktopClient updates the desktop daemon client after construction.
func (g *Gateway) SetDesktopClient(c *desktopclient.Client) {
	g.desktopClient = c
}

// SetCapabilities updates the capability registry after construction.
func (g *Gateway) SetCapabilities(reg *capregistry.Registry) {
	g.capabilities = reg
	if g.extensions != nil {
		g.extensions.SetCapabilities(reg)
	}
}

// SetDeviceAuth updates the device auth store and pairing manager.
func (g *Gateway) SetDeviceAuth(store *deviceauth.Store, pairing *deviceauth.PairingManager) {
	g.deviceStore = store
	g.devicePairing = pairing
}

// SetManagedHelpers sets the controller for managed Browser/Desktop helpers (operator UI).
func (g *Gateway) SetManagedHelpers(c ManagedHelpersController) {
	g.managedHelpers = c
}

// Handler returns the composed HTTP handler with production middleware.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	g.registerAuthSessionRoutes(mux)
	g.registerConsoleUIRoutes(mux)
	g.registerOperatorAPIRoutes(mux)
	g.registerTransportRoutes(mux)
	g.registerChannelRoutes(mux)
	g.registerRuntimeAPIRoutes(mux)
	g.registerDiagnosticsRoutes(mux)
	g.registerTelemetryRoutes(mux)
	g.registerPublicServerRoutes(mux)
	return g.wrapHTTPAppMiddleware(mux)
}

// handleWebhookInbound accepts inbound messages from external systems.
//
//	POST /channels/webhook/{id}/inbound
//	{"sender_id":"user1","content":"hello","metadata":{...}}
func (g *Gateway) handleWebhookInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing webhook id")
		return
	}
	adapter, ok := g.webhooks[id]
	if !ok {
		gwError(w, http.StatusNotFound, "unknown webhook id")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gwError(w, http.StatusBadRequest, "read body failed")
		return
	}

	// Verify signature if secret is configured.
	sig := r.Header.Get("X-HopClaw-Signature")
	if !adapter.VerifySignature(body, sig) {
		gwError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	var payload webhook.InboundPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		gwErrorCode(w, http.StatusBadRequest, apiresponse.ErrorCodeInvalidJSON, "invalid json")
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		gwError(w, http.StatusBadRequest, "content is required")
		return
	}

	adapter.HandleInbound(payload)
	gwJSON(w, http.StatusOK, okResponse{OK: true})
}

// ---------------------------------------------------------------------------
// Channel list
// ---------------------------------------------------------------------------

type channelListItem struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

func (g *Gateway) handleListChannels(w http.ResponseWriter, r *http.Request) {
	if g.channels == nil {
		gwJSON(w, http.StatusOK, countedItemsResponse{Items: []any{}, Count: 0})
		return
	}
	names := g.channels.Names()
	items := make([]channelListItem, len(names))
	for i, name := range names {
		_, connected := g.channels.Get(name)
		items[i] = channelListItem{Name: name, Connected: connected}
	}
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleChannelInbound(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/channels/") || !strings.HasSuffix(r.URL.Path, "/inbound") {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/channels/webhook/") {
		http.NotFound(w, r)
		return
	}
	if g.channels == nil {
		gwError(w, http.StatusServiceUnavailable, "channels not available")
		return
	}

	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/channels/"), "/inbound")
	name = strings.Trim(name, "/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	adapter, ok := g.channels.Get(name)
	if !ok {
		gwError(w, http.StatusNotFound, "unknown channel")
		return
	}
	inbound, ok := adapter.(channels.HTTPInboundAdapter)
	if !ok {
		gwError(w, http.StatusNotFound, "channel does not expose HTTP inbound")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gwError(w, http.StatusBadRequest, "read body failed")
		return
	}
	resp, err := inbound.HandleHTTPInbound(r.Context(), channels.HTTPInboundRequest{
		Method: r.Method,
		Header: r.Header.Clone(),
		Query:  r.URL.Query(),
		Body:   body,
	})
	if err != nil {
		var inboundErr *channels.HTTPInboundError
		if errors.As(err, &inboundErr) {
			gwError(w, inboundErr.StatusCode, inboundErr.Message)
			return
		}
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if resp == nil {
		resp = &channels.HTTPInboundResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       []byte(`{"ok":true}`),
		}
	}
	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}
	status := resp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(resp.Body) > 0 {
		if _, err := w.Write(resp.Body); err != nil {
			logging.FromContext(r.Context()).Warn("write http response body failed", "error", err)
		}
	}
}
