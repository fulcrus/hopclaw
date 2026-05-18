package toolruntime

import (
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/canvas"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/media"
	"github.com/fulcrus/hopclaw/skill"
	wakeupsvc "github.com/fulcrus/hopclaw/wakeup"
	watchsvc "github.com/fulcrus/hopclaw/watch"
)

// BuiltinsBindings captures the external services and hosts wired into the
// builtin tool runtime. It is the preferred wiring surface for Builtins so
// bootstrap, builders, and tests can update dependencies through one bundle
// instead of a long tail of per-service setter methods.
type BuiltinsBindings struct {
	Layer2             *Layer2Registry
	SkillService       *skill.Service
	ModuleCatalog      *modules.Store
	ClawHub            skill.ClawHubClient
	Sessions           agent.SessionStore
	MemoryStore        agent.MemoryStore
	KnowledgeService   *knowledge.Service
	ArtifactStore      artifact.Store
	ChannelManager     *channelmgr.Manager
	DestinationCatalog DestinationCatalog
	ExtensionRegistry  *extregistry.Registry
	CronService        *cronsvc.Service
	WatchService       *watchsvc.Service
	WakeupService      *wakeupsvc.Service
	Spawner            *isolation.Spawner
	BrowserClient      *browserclient.Client
	DesktopClient      *desktopclient.Client
	CanvasHost         *canvas.Host
	MediaRegistry      *media.Registry
}

// Bindings returns a point-in-time snapshot of the builtin runtime bindings.
func (b *Builtins) Bindings() BuiltinsBindings {
	if b == nil {
		return BuiltinsBindings{}
	}
	return BuiltinsBindings{
		Layer2:             b.layer2,
		SkillService:       b.skillService,
		ModuleCatalog:      b.moduleCatalog,
		ClawHub:            b.clawHub,
		Sessions:           b.sessions,
		MemoryStore:        b.memoryStore,
		KnowledgeService:   b.knowledge,
		ArtifactStore:      b.artifactStore,
		ChannelManager:     b.channelManager,
		DestinationCatalog: b.destinations.Clone(),
		ExtensionRegistry:  b.extensions,
		CronService:        b.cronService,
		WatchService:       b.watchService,
		WakeupService:      b.wakeupService,
		Spawner:            b.spawner,
		BrowserClient:      b.browserClient,
		DesktopClient:      b.desktopClient,
		CanvasHost:         b.canvasHost,
		MediaRegistry:      b.mediaRegistry,
	}
}

// ApplyBindings replaces the builtin runtime bindings with one coherent
// snapshot. Destination catalogs are cloned so callers can safely reuse or
// mutate their input after the call returns.
func (b *Builtins) ApplyBindings(bindings BuiltinsBindings) {
	if b == nil {
		return
	}
	b.layer2 = bindings.Layer2
	b.skillService = bindings.SkillService
	b.moduleCatalog = bindings.ModuleCatalog
	b.clawHub = bindings.ClawHub
	b.sessions = bindings.Sessions
	b.memoryStore = bindings.MemoryStore
	b.knowledge = bindings.KnowledgeService
	b.artifactStore = bindings.ArtifactStore
	b.channelManager = bindings.ChannelManager
	b.destinations = bindings.DestinationCatalog.Clone()
	b.extensions = bindings.ExtensionRegistry
	b.cronService = bindings.CronService
	b.watchService = bindings.WatchService
	b.wakeupService = bindings.WakeupService
	b.spawner = bindings.Spawner
	b.browserClient = bindings.BrowserClient
	b.desktopClient = bindings.DesktopClient
	b.canvasHost = bindings.CanvasHost
	b.mediaRegistry = bindings.MediaRegistry
}

// UpdateBindings loads the current snapshot, applies a caller-provided update,
// and writes the result back atomically from the caller's point of view.
func (b *Builtins) UpdateBindings(update func(*BuiltinsBindings)) {
	if b == nil || update == nil {
		return
	}
	bindings := b.Bindings()
	update(&bindings)
	b.ApplyBindings(bindings)
}
