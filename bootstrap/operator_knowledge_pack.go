package bootstrap

import (
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type knowledgeGatewayTarget interface {
	ApplyKnowledgeServices(gateway.KnowledgeServices)
}

type preparedOperatorKnowledgePack struct {
	knowledgeService *knowledge.Service
}

func newPreparedOperatorKnowledgePack(runtimeCore *preparedBootstrapRuntimeCore) *preparedOperatorKnowledgePack {
	if runtimeCore == nil {
		return nil
	}
	return &preparedOperatorKnowledgePack{knowledgeService: runtimeCore.knowledgeService}
}

func (a *App) knowledgePackForState() *preparedOperatorKnowledgePack {
	if a == nil {
		return nil
	}
	return &preparedOperatorKnowledgePack{knowledgeService: a.Knowledge}
}

func (p *preparedOperatorKnowledgePack) packID() string {
	if p == nil {
		return ""
	}
	return builtinBindingPackKnowledge
}

func (p *preparedOperatorKnowledgePack) moduleExposed() bool {
	return p != nil && p.knowledgeService != nil
}

func (p *preparedOperatorKnowledgePack) module() modules.StaticModule {
	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: "Knowledge retrieval and ingestion services are ready.",
		Details: map[string]any{
			"knowledge_service": p != nil && p.knowledgeService != nil,
		},
	}
	if p == nil || p.knowledgeService == nil {
		health.Status = modules.HealthDegraded
		health.Summary = "Knowledge service is not wired."
	}
	return staticFirstPartyPackModule(
		builtinBindingPackKnowledge,
		"knowledge-pack",
		"First-party knowledge service pack for retrieval, ingestion, and runtime knowledge tooling.",
		health,
	)
}

func (p *preparedOperatorKnowledgePack) applySurface(*preparedBootstrapSurface) {}

func (p *preparedOperatorKnowledgePack) applyGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	gw.ApplyKnowledgeServices(gateway.KnowledgeServices{Knowledge: p.knowledgeService})
}

func (p *preparedOperatorKnowledgePack) applyKnowledgeGateway(target knowledgeGatewayTarget) {
	if p == nil || target == nil {
		return
	}
	target.ApplyKnowledgeServices(gateway.KnowledgeServices{Knowledge: p.knowledgeService})
}

func (p *preparedOperatorKnowledgePack) applyBuiltins(bindings *toolruntime.BuiltinsBindings) {
	if p == nil || bindings == nil {
		return
	}
	bindings.KnowledgeService = p.knowledgeService
}
