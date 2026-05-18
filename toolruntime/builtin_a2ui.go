package toolruntime

import (
	"context"
	"encoding/json"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/canvas"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func a2uiToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "a2ui.push",
				Description:     "Push A2UI components to the canvas for the current session.",
				InputSchema:     a2uiPushInputSchema(),
				OutputSchema:    a2uiPushOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "a2ui:push:{session_id}",
			},
			Handler: handleA2UIPush,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "a2ui.reset",
				Description:     "Clear all A2UI components for the current session.",
				InputSchema:     a2uiResetInputSchema(),
				OutputSchema:    a2uiResetOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "a2ui:reset:{session_id}",
			},
			Handler: handleA2UIReset,
		},
	}
}

func handleA2UIPush(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.canvasHost == nil {
		return contextengine.ToolResult{Content: "canvas host not configured"}, nil
	}

	sessionID, _ := stringFrom(call.Input["session_id"])
	if sessionID == "" {
		return contextengine.ToolResult{Content: "session_id is required"}, nil
	}

	rawComponents, ok := call.Input["components"]
	if !ok {
		return contextengine.ToolResult{Content: "at least one component is required"}, nil
	}

	// Convert the components from map[string]any to []canvas.Component via JSON round-trip.
	compJSON, err := json.Marshal(rawComponents)
	if err != nil {
		return contextengine.ToolResult{Content: "invalid components format"}, nil
	}
	var components []canvas.Component
	if err := json.Unmarshal(compJSON, &components); err != nil {
		return contextengine.ToolResult{Content: "invalid components format"}, nil
	}
	if len(components) == 0 {
		return contextengine.ToolResult{Content: "at least one component is required"}, nil
	}

	replace, _ := boolFrom(call.Input["replace"])

	version := b.canvasHost.PushComponents(sessionID, components, replace)

	result, _ := json.Marshal(map[string]any{
		"ok":              true,
		"version":         version,
		"component_count": len(components),
	})
	return contextengine.ToolResult{Content: string(result)}, nil
}

func handleA2UIReset(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.canvasHost == nil {
		return contextengine.ToolResult{Content: "canvas host not configured"}, nil
	}

	sessionID, _ := stringFrom(call.Input["session_id"])
	if sessionID == "" {
		return contextengine.ToolResult{Content: "session_id is required"}, nil
	}

	b.canvasHost.ResetComponents(sessionID)

	result, _ := json.Marshal(map[string]any{
		"ok": true,
	})
	return contextengine.ToolResult{Content: string(result)}, nil
}

// ---------------------------------------------------------------------------
// Input/Output schemas
// ---------------------------------------------------------------------------

func a2uiPushInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "The session to push components to.",
			},
			"components": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   map[string]any{"type": "string", "description": "Unique component identifier."},
						"type": map[string]any{"type": "string", "description": "Component type: chart, table, form, markdown, custom."},
						"props": map[string]any{
							"type":        "object",
							"description": "Component-specific properties.",
						},
						"children": map[string]any{
							"type":        "array",
							"description": "Nested child components.",
						},
					},
					"required": []string{"id", "type"},
				},
				"description": "Components to push.",
			},
			"replace": map[string]any{
				"type":        "boolean",
				"description": "If true, replaces all existing components instead of appending.",
			},
		},
		"required": []string{"session_id", "components"},
	}
}

func a2uiPushOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok":              map[string]any{"type": "boolean"},
			"version":         map[string]any{"type": "integer"},
			"component_count": map[string]any{"type": "integer"},
		},
	}
}

func a2uiResetInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "The session to reset.",
			},
		},
		"required": []string{"session_id"},
	}
}

func a2uiResetOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
	}
}
