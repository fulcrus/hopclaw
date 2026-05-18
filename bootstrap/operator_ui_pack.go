package bootstrap

import (
	"github.com/fulcrus/hopclaw/canvas"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type preparedOperatorUIPack struct {
	canvasHost *canvas.Host
}

func newPreparedOperatorUIPack(addons *preparedOperatorAddons) *preparedOperatorUIPack {
	if addons == nil {
		return nil
	}
	return &preparedOperatorUIPack{canvasHost: addons.canvasHost}
}

func (a *App) uiPackForState() *preparedOperatorUIPack {
	if a == nil {
		return nil
	}
	return &preparedOperatorUIPack{canvasHost: a.CanvasHost}
}

func (p *preparedOperatorUIPack) packID() string {
	if p == nil {
		return ""
	}
	return builtinBindingPackUI
}

func (p *preparedOperatorUIPack) moduleExposed() bool {
	return p != nil && p.canvasHost != nil
}

func (p *preparedOperatorUIPack) module() modules.StaticModule {
	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: "Canvas UI surfaces are ready.",
		Details: map[string]any{
			"canvas_host": p != nil && p.canvasHost != nil,
		},
	}
	if p == nil || p.canvasHost == nil {
		health.Status = modules.HealthDegraded
		health.Summary = "Canvas host is not wired."
	}
	return staticFirstPartyPackModule(
		builtinBindingPackUI,
		"ui-pack",
		"First-party UI surface pack for canvas-backed runtime experiences.",
		health,
	)
}

func (p *preparedOperatorUIPack) applySurface(surface *preparedBootstrapSurface) {
	if p == nil || surface == nil {
		return
	}
	surface.canvasHost = p.canvasHost
}

func (p *preparedOperatorUIPack) applyGateway(*gateway.Gateway) {}

func (p *preparedOperatorUIPack) applyBuiltins(bindings *toolruntime.BuiltinsBindings) {
	if p == nil || bindings == nil {
		return
	}
	bindings.CanvasHost = p.canvasHost
}
