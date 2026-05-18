package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/canvas"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func TestPreparedOperatorUIPackAppliesToSurfaceAndBuiltins(t *testing.T) {
	t.Parallel()

	canvasHost := &canvas.Host{}
	pack := &preparedOperatorUIPack{canvasHost: canvasHost}
	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{builtinBindingPackUI}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}

	surface := &preparedBootstrapSurface{}
	pack.applySurface(surface)
	if surface.canvasHost != canvasHost {
		t.Fatal("expected canvas host on surface")
	}

	bindings := toolruntime.BuiltinsBindings{}
	pack.applyBuiltins(&bindings)
	if bindings.CanvasHost != canvasHost {
		t.Fatalf("builtin canvas binding = %#v", bindings.CanvasHost)
	}

	module := pack.module()
	if module.Manifest().ID != builtinBindingPackUI || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}
}
