package bootstrap

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/canvas"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/hooks"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

var log = logging.WithSubsystem("bootstrap")

type Dependencies struct {
	Model              agent.ModelClient
	Tools              agent.ToolExecutor
	Artifacts          artifact.Store
	Router             agent.ModelRouter
	Policy             agent.PolicyEngine
	ApprovalProviders  []controlapproval.Provider
	GovernanceAdapters []controlgov.Adapter
	Runtime            agent.RuntimeContextProvider
	ConfigPath         string // path to YAML config file for hot-reload watcher
}

type AppConfigState struct {
	BaseConfig  config.Config
	Config      config.Config
	ConfigStore *store.ConfigStore
}

type AppStoreState struct {
	Sessions   agent.SessionStore
	Runs       agent.RunStore
	Approvals  approval.Store
	Artifacts  artifact.Store
	GrantStore *approval.GrantStore
}

type AppRuntimeState struct {
	Bus               *eventbus.InMemoryBus
	Capabilities      *capregistry.Registry
	ExtensionRegistry *extregistry.Registry
	ModuleCatalog     *modules.Store
	SkillService      *skill.Service
	Knowledge         *knowledge.Service
	Plugins           *plugin.Manager
	Runtime           *runtimesvc.Service
	ManagedHelpers    *managedHelpers
	ApprovalTimeout   *approval.TimeoutService
	ArtifactPruner    *runtimesvc.ArtifactPruner
	StatePruner       *runtimesvc.StatePruner
}

type AppSurfaceState struct {
	Gateway           *gateway.Gateway
	Channels          *channelmgr.Manager
	Webhooks          map[string]*webhook.Adapter // webhook ID → adapter
	PluginInstaller   *plugin.Installer
	CronService       *cronsvc.Service
	WatchService      *watch.Service
	HeartbeatService  *heartbeat.Service
	WireLogger        *wire.Logger
	WakeupService     *wakeup.Service
	AllowlistManager  *allowlist.Manager
	SandboxRunner     *sandbox.Runner
	IsolationManager  *isolation.Manager
	HealthMonitor     *health.Monitor
	HookExecutor      *hooks.Executor
	CanvasHost        *canvas.Host
	DiscoveryResolver discovery.Resolver
	NodeRegistry      *gatewaynodes.Registry
	DeviceStore       *deviceauth.Store
	DevicePairing     *deviceauth.PairingManager
	Handler           http.Handler
}

type appInternalState struct {
	runtimeDB                *sql.DB
	storeDB                  *sql.DB // non-nil when backend is "sqlite"
	knowledgeDB              *sql.DB
	auditDB                  *sql.DB
	governanceDeliveryDB     *sql.DB
	governanceDispatcher     *controlgov.ReliableDispatcher
	effectiveConfig          *controloverlay.Resolver
	effectiveLayers          []controlsnapshot.Layer
	snapshotBuilder          func(config.Config, []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot
	snapshotState            *effectiveSnapshotState
	runtimeRoutes            *server.Server
	channelBridges           []namedChannelBridge
	processManager           *plugin.ProcessManager
	mcpRuntime               pluginMCPRuntime
	skillWatchStop           context.CancelFunc
	pluginWatchStop          context.CancelFunc
	keychainWatcher          *keychain.Watcher
	configWatcher            *config.Watcher
	configWatcherStop        context.CancelFunc
	memoryStore              agent.MemoryStore
	baseTools                agent.ToolExecutor
	builtins                 *toolruntime.Builtins
	modelRuntime             *dynamicModelClient
	routerRuntime            *dynamicModelRouter
	toolRuntime              *dynamicToolExecutor
	skillBinder              *dynamicSkillBinder
	sessionDirectives        agent.SessionDirectiveStore
	threadBindings           *channels.ThreadBinding
	pairingManager           *pairing.Manager
	statusDelay              time.Duration
	skillHub                 skill.ClawHubClient
	browserClient            *browserclient.Client
	desktopClient            *desktopclient.Client
	spawner                  *isolation.Spawner
	policyOverlay            agent.PolicyEngine
	customApprovals          []controlapproval.Provider
	customGovernance         []controlgov.Adapter
	approvalSyncer           *dynamicApprovalDispatcher
	governanceControl        *dynamicGovernanceDispatcher
	auditSink                *dynamicAuditSink
	usageStore               usage.Store
	customModel              bool
	customRouter             bool
	customTools              bool
	startupWarnings          *startupWarningCollector
	operationalWarnings      controlplane.OperationalWarningSource
	automationDeliverer      *channelCronDeliverer
	closed                   bool
	pluginWatchGen           uint64
	pluginWatchRestartQueued bool
	refreshMu                sync.Mutex
}

type App struct {
	AppConfigState
	AppStoreState
	AppRuntimeState
	AppSurfaceState
	appInternalState
}

type channelBridge interface {
	Start(ctx context.Context)
	Stop()
}

type namedChannelBridge struct {
	name   string
	bridge channelBridge
}
