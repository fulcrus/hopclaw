package bootstrap

import (
	"context"
	"reflect"
	"testing"

	cronsvc "github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

type automationGatewayTargetStub struct {
	cron      *cronsvc.Service
	watch     *watch.Service
	heartbeat *heartbeat.Service
	wire      *wire.Logger
	wakeup    *wakeup.Service
}

func (s *automationGatewayTargetStub) ApplyAutomationServices(services gateway.AutomationServices) {
	s.cron = services.Cron
	s.watch = services.Watch
	s.heartbeat = services.Heartbeat
	s.wire = services.Wire
	s.wakeup = services.Wakeup
}

func TestPreparedOperatorAutomationPackContributionMetadata(t *testing.T) {
	t.Parallel()

	pack := &preparedOperatorAutomationPack{}
	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{builtinBindingPackAutomation}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}
}

func TestPreparedOperatorAutomationPackAppliesAcrossTargets(t *testing.T) {
	t.Parallel()

	cronService := &cronsvc.Service{}
	watchService := &watch.Service{}
	heartbeatService := &heartbeat.Service{}
	wireLogger := &wire.Logger{}
	wakeupService := &wakeup.Service{}
	pack := &preparedOperatorAutomationPack{
		cronService:      cronService,
		watchService:     watchService,
		heartbeatService: heartbeatService,
		wireLogger:       wireLogger,
		wakeupService:    wakeupService,
	}

	surface := &preparedBootstrapSurface{}
	pack.applySurface(surface)
	if surface.cronService != cronService || surface.watchService != watchService || surface.heartbeatService != heartbeatService || surface.wireLogger != wireLogger || surface.wakeupService != wakeupService {
		t.Fatalf("surface wiring = %#v", surface)
	}

	bindings := toolruntime.BuiltinsBindings{}
	pack.applyBuiltins(&bindings)
	if bindings.CronService != cronService || bindings.WatchService != watchService || bindings.WakeupService != wakeupService {
		t.Fatalf("builtin wiring = %#v", bindings)
	}

	target := &automationGatewayTargetStub{}
	pack.applyAutomationGateway(target)
	if target.cron != cronService || target.watch != watchService || target.heartbeat != heartbeatService || target.wire != wireLogger || target.wakeup != wakeupService {
		t.Fatalf("gateway wiring = %#v", target)
	}

	module := pack.module()
	if module.Manifest().ID != builtinBindingPackAutomation || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}
}

func TestApplyFirstPartyPackContributionUpdatesSurfaceAndBuiltins(t *testing.T) {
	t.Parallel()

	cronService := &cronsvc.Service{}
	pack := &preparedOperatorAutomationPack{cronService: cronService}
	surface := &preparedBootstrapSurface{}
	builtins := toolruntime.NewBuiltins(toolruntime.BuiltinsConfig{Root: t.TempDir()})

	applyFirstPartyPackContribution(surface, nil, builtins, pack)

	if surface.cronService != cronService {
		t.Fatal("expected surface cron service to be populated")
	}
	if bindings := builtins.Bindings(); bindings.CronService != cronService {
		t.Fatalf("builtin bindings = %#v", bindings)
	}
}
