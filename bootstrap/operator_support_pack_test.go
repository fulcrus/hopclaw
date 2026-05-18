package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/usage"
)

type supportGatewayTargetStub struct {
	approvals     approval.Store
	grantStore    *approval.GrantStore
	allowlist     *allowlist.Manager
	sandbox       *sandbox.Runner
	discovery     discovery.Resolver
	channelHealth *health.Monitor
	hooks         *hooks.Executor
	usageStore    usage.Store
}

func (s *supportGatewayTargetStub) ApplySupportServices(services gateway.SupportServices) {
	s.approvals = services.Approvals
	s.grantStore = services.GrantStore
	s.allowlist = services.Allowlist
	s.sandbox = services.Sandbox
	s.discovery = services.Discovery
	s.channelHealth = services.ChannelHealth
	s.hooks = services.Hooks
	s.usageStore = services.UsageStore
}

type stubDiscoveryResolver struct{}

func (stubDiscoveryResolver) Discover(context.Context) ([]discovery.Peer, error) {
	return nil, nil
}

func (stubDiscoveryResolver) Announce(context.Context, discovery.Peer) error {
	return nil
}

func (stubDiscoveryResolver) Stop() error {
	return nil
}

func TestPreparedOperatorSupportPackAppliesAcrossTargets(t *testing.T) {
	t.Parallel()

	approvals := approval.NewInMemoryStore()
	grantStore := approval.NewGrantStore()
	allowlistManager := allowlist.NewManager(nil)
	sandboxRunner := &sandbox.Runner{}
	isolationManager := &isolation.Manager{}
	healthMonitor := &health.Monitor{}
	hookExecutor := hooks.NewExecutor(hooks.NewInMemoryStore())
	discoveryResolver := stubDiscoveryResolver{}
	keychainWatcher := &keychain.Watcher{}
	usageStore := usage.NewInMemoryStore()
	pack := &preparedOperatorSupportPack{
		approvals:        approvals,
		grantStore:       grantStore,
		allowlistManager: allowlistManager,
		sandboxRunner:    sandboxRunner,
		isolationManager: isolationManager,
		healthMonitor:    healthMonitor,
		hookExecutor:     hookExecutor,
		discovery:        discoveryResolver,
		keychainWatcher:  keychainWatcher,
		usageStore:       usageStore,
	}

	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{firstPartyPackOperatorSupport}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}

	surface := &preparedBootstrapSurface{}
	pack.applySurface(surface)
	if surface.allowlistManager != allowlistManager || surface.sandboxRunner != sandboxRunner || surface.isolationManager != isolationManager || surface.healthMonitor != healthMonitor || surface.hookExecutor != hookExecutor || surface.keychainWatcher != keychainWatcher {
		t.Fatalf("surface wiring = %#v", surface)
	}
	if surface.discoveryResolver != discoveryResolver {
		t.Fatalf("discovery resolver wiring = %#v", surface.discoveryResolver)
	}

	target := &supportGatewayTargetStub{}
	pack.applySupportGateway(target)
	if target.approvals != approvals || target.grantStore != grantStore || target.allowlist != allowlistManager || target.sandbox != sandboxRunner || target.channelHealth != healthMonitor || target.hooks != hookExecutor || target.usageStore != usageStore {
		t.Fatalf("gateway wiring = %#v", target)
	}
	if target.discovery != discoveryResolver {
		t.Fatalf("discovery gateway wiring = %#v", target.discovery)
	}

	module := pack.module()
	if module.Manifest().ID != firstPartyPackOperatorSupport || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}
}
