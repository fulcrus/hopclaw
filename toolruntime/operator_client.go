package toolruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

type operatorClientHandler func(context.Context, agent.ToolCall) (contextengine.ToolResult, error)

type operatorClientToolDef struct {
	Manifest skill.ToolManifest
	Handler  operatorClientHandler
}

type OperatorClient struct {
	tools       map[string]skill.BoundTool
	definitions []agent.ToolDefinition
	handlers    map[string]operatorClientHandler
}

func NewOperatorClient() *OperatorClient {
	pkg := &skill.SkillPackage{
		ID:     "operator-client",
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusReady,
		Prompt: skill.PromptSkill{
			Name:        "operator-client",
			Description: "Operator gateway client tools",
			Location:    "operator:client",
		},
		Source: skill.SkillSource{
			Kind: skill.SourceBundled,
			Dir:  "operator",
			Root: "operator",
		},
		Trust: skill.TrustBundled,
	}

	client := &OperatorClient{
		tools:    make(map[string]skill.BoundTool),
		handlers: make(map[string]operatorClientHandler),
	}
	for _, def := range operatorClientToolDefs() {
		client.addTool(pkg, def)
	}
	return client
}

func operatorClientToolDefs() []operatorClientToolDef {
	return []operatorClientToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "gateway.status",
				Description:     "Query the operator gateway for its current status.",
				InputSchema:     gatewayStatusInputSchema(),
				OutputSchema:    gatewayStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "gateway:status",
			},
			Handler: handleGatewayStatus,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "gateway.capabilities",
				Description:     "List the capabilities registered with the operator gateway.",
				InputSchema:     gatewayCapabilitiesInputSchema(),
				OutputSchema:    gatewayCapabilitiesOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "gateway:capabilities",
			},
			Handler: handleGatewayCapabilities,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "gateway.health",
				Description:     "Check the health of all operator gateway channels.",
				InputSchema:     gatewayHealthInputSchema(),
				OutputSchema:    gatewayHealthOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "gateway:health",
			},
			Handler: handleGatewayHealth,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "gateway.reload",
				Description:     "Trigger a configuration reload on the operator gateway.",
				InputSchema:     gatewayReloadInputSchema(),
				OutputSchema:    gatewayReloadOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "gateway:reload",
			},
			Handler: handleGatewayReload,
		},
	}
}

func (c *OperatorClient) addTool(pkg *skill.SkillPackage, def operatorClientToolDef) {
	name := strings.TrimSpace(def.Manifest.Name)
	if name == "" {
		return
	}
	pkg.ToolManifests = append(pkg.ToolManifests, skill.ToolManifest{
		Name:             name,
		Description:      def.Manifest.Description,
		InputSchema:      cloneSchema(def.Manifest.InputSchema),
		OutputSchema:     cloneSchema(def.Manifest.OutputSchema),
		SideEffectClass:  def.Manifest.SideEffectClass,
		Idempotent:       def.Manifest.Idempotent,
		RequiresApproval: def.Manifest.RequiresApproval,
		ExecutionKey:     def.Manifest.ExecutionKey,
	})

	bound := skill.BoundTool{
		Package: pkg,
		Manifest: skill.ToolManifest{
			Name:             name,
			Description:      def.Manifest.Description,
			InputSchema:      cloneSchema(def.Manifest.InputSchema),
			OutputSchema:     cloneSchema(def.Manifest.OutputSchema),
			SideEffectClass:  def.Manifest.SideEffectClass,
			Idempotent:       def.Manifest.Idempotent,
			RequiresApproval: def.Manifest.RequiresApproval,
			ExecutionKey:     def.Manifest.ExecutionKey,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	c.tools[name] = bound
	c.handlers[name] = def.Handler
	c.definitions = append(c.definitions, agent.ToolDefinition{
		Name:             name,
		Description:      def.Manifest.Description,
		InputSchema:      cloneSchema(def.Manifest.InputSchema),
		OutputSchema:     cloneSchema(def.Manifest.OutputSchema),
		SideEffectClass:  def.Manifest.SideEffectClass,
		Idempotent:       def.Manifest.Idempotent,
		RequiresApproval: def.Manifest.RequiresApproval,
		ExecutionKey:     def.Manifest.ExecutionKey,
		Source:           "operator",
		SourceRef:        "operator:gateway",
		Trust:            string(skill.TrustBundled),
		Eligible:         true,
		Availability: agent.ToolAvailability{
			Status: agent.AvailabilityReady,
		},
	})
}

func (c *OperatorClient) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if c == nil || len(c.definitions) == 0 {
		return nil
	}
	out := make([]agent.ToolDefinition, 0, len(c.definitions))
	for _, def := range c.definitions {
		out = append(out, copyToolDefinition(def))
	}
	return out
}

func (c *OperatorClient) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if c == nil {
		return nil, false
	}
	bound, ok := c.tools[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}
	copied := bound
	return resolvedToolFromBinding(&copied, agent.ToolDefinition{
		Name:             copied.Manifest.Name,
		Description:      copied.Manifest.Description,
		InputSchema:      cloneSchema(copied.Manifest.InputSchema),
		OutputSchema:     cloneSchema(copied.Manifest.OutputSchema),
		SideEffectClass:  copied.Manifest.SideEffectClass,
		Idempotent:       copied.Manifest.Idempotent,
		RequiresApproval: copied.Manifest.RequiresApproval,
		ExecutionKey:     copied.Manifest.ExecutionKey,
		Source:           "operator",
		SourceRef:        "operator:gateway",
		Trust:            string(copied.Package.Trust),
		Eligible:         true,
		Availability: agent.ToolAvailability{
			Status: agent.AvailabilityReady,
		},
	}, "operator_client"), true
}

func (c *OperatorClient) ExecuteBatch(ctx context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		handler, ok := c.handlers[strings.TrimSpace(call.Name)]
		if !ok {
			return nil, fmt.Errorf("tool %q is not registered", call.Name)
		}
		result, err := handler(ctx, call)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
