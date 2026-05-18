package bootstrap

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/triage"
)

type preparedRuntimeAgentComponent struct {
	component         *agent.AgentComponent
	sessionDirectives agent.SessionDirectiveStore
	triageEngine      *triage.ModelTriage
}

func prepareRuntimeAgentComponent(
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	primitives *preparedRuntimeCorePrimitives,
) *preparedRuntimeAgentComponent {
	component := agent.NewComponent(agent.AgentConfig{
		SystemPrompt:            cfg.Agent.SystemPrompt,
		DefaultModel:            cfg.Agent.DefaultModel,
		MaxRunDuration:          cfg.Agent.MaxRunDuration,
		MaxToolRounds:           cfg.Agent.MaxToolRounds,
		MaxToolRecoveryAttempts: cfg.Agent.MaxToolRecoveryAttempts,
		QueueMode:               parseQueueMode(cfg.Agent.QueueMode),
		DedupeWindow:            cfg.Agent.DedupeWindow,
	}, foundation.sessions, foundation.runs, agent.NewInMemoryCoordinator(), primitives.contextEngine, primitives.modelRuntime, primitives.toolRuntime, primitives.runtimeProvider)

	sessionDirectives := agent.NewInMemorySessionDirectiveStore()
	component.WithSessionDirectives(sessionDirectives)

	triageModel := strings.TrimSpace(cfg.Agent.TriageModel)
	triageEngine := triage.NewModelTriage(func(ctx context.Context, req triage.ChatRequest) (string, error) {
		resp, err := primitives.modelRuntime.Chat(ctx, agent.ChatRequest{
			Model:        req.Model,
			SystemPrompt: req.SystemPrompt,
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: req.Payload,
			}},
			Budget: req.Budget,
		})
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", agent.ErrModelClientNil
		}
		return resp.Message.Content, nil
	}, 0)
	if triageModel != "" {
		triageEngine = triageEngine.WithDefaultModel(triageModel)
	}

	planner := agent.NewModelPlanner(primitives.modelRuntime, 0)
	if triageModel != "" {
		planner = planner.WithDefaultModel(triageModel)
	}
	component.WithPlanner(planner)
	component.WithRunTriage(triageEngine)

	modeSelector := agent.NewModelExecutionModeSelector(primitives.modelRuntime, 0)
	if triageModel != "" {
		modeSelector = modeSelector.WithDefaultModel(triageModel)
	}
	component.WithExecutionModeSelector(modeSelector)

	preflightAnalyzer := agent.NewModelPreflightAnalyzer(primitives.modelRuntime, 0)
	if triageModel != "" {
		preflightAnalyzer = preflightAnalyzer.WithDefaultModel(triageModel)
	}
	component.WithPreflightAnalyzer(preflightAnalyzer)

	taskContractAnalyzer := agent.NewModelTaskContractAnalyzer(primitives.modelRuntime, 0)
	if triageModel != "" {
		taskContractAnalyzer = taskContractAnalyzer.WithDefaultModel(triageModel)
	}
	component.WithTaskContractAnalyzer(taskContractAnalyzer)
	component.WithRouter(primitives.routerRuntime)

	return &preparedRuntimeAgentComponent{
		component:         component,
		sessionDirectives: sessionDirectives,
		triageEngine:      triageEngine,
	}
}
