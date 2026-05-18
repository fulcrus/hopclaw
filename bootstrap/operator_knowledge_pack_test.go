package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type knowledgeGatewayTargetStub struct {
	knowledgeService *knowledge.Service
}

func (s *knowledgeGatewayTargetStub) ApplyKnowledgeServices(services gateway.KnowledgeServices) {
	s.knowledgeService = services.Knowledge
}

func TestPreparedOperatorKnowledgePackAppliesAcrossTargets(t *testing.T) {
	t.Parallel()

	knowledgeService := &knowledge.Service{}
	pack := &preparedOperatorKnowledgePack{knowledgeService: knowledgeService}
	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{builtinBindingPackKnowledge}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}

	target := &knowledgeGatewayTargetStub{}
	pack.applyKnowledgeGateway(target)
	if target.knowledgeService != knowledgeService {
		t.Fatal("expected gateway knowledge service to be wired")
	}

	bindings := toolruntime.BuiltinsBindings{}
	pack.applyBuiltins(&bindings)
	if bindings.KnowledgeService != knowledgeService {
		t.Fatalf("builtin knowledge wiring = %#v", bindings.KnowledgeService)
	}

	module := pack.module()
	if module.Manifest().ID != builtinBindingPackKnowledge || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}
}
