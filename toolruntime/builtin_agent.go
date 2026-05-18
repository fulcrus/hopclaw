package toolruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// agentToolDefs returns all 4 agent.* tool definitions.
// ---------------------------------------------------------------------------

func agentToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "agent.spawn",
				Description:     "Spawn a sub-agent session to handle a task concurrently.",
				InputSchema:     agentSpawnInputSchema(),
				OutputSchema:    agentSpawnOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "agent:spawn:{agent_name}",
			},
			Handler: handleAgentSpawn,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "agent.send",
				Description:     "Send a message to a running sub-agent session.",
				InputSchema:     agentSendInputSchema(),
				OutputSchema:    agentSendOutputSchema(),
				SideEffectClass: "remote_write",
				ExecutionKey:    "agent:send:{child_id}",
			},
			Handler: handleAgentSend,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "agent.yield",
				Description:     "Wait for all sub-agents of a parent session to complete and return their results.",
				InputSchema:     agentYieldInputSchema(),
				OutputSchema:    agentYieldOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "agent:yield:{session_id}",
			},
			Handler: handleAgentYield,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "agent.list",
				Description:     "List sub-agent sessions for a parent session.",
				InputSchema:     agentListInputSchema(),
				OutputSchema:    agentListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "agent:list:{session_id}",
			},
			Handler: handleAgentList,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func agentSpawnInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_name": map[string]any{
				"type":        "string",
				"description": "Name of the agent to spawn.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Initial message to send to the sub-agent.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Parent session ID that owns this sub-agent.",
			},
		},
		"required":             []string{"agent_name", "message", "session_id"},
		"additionalProperties": false,
	}
}

func agentSendInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Parent session ID.",
			},
			"child_id": map[string]any{
				"type":        "string",
				"description": "Child sub-agent session ID.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to deliver to the sub-agent.",
			},
		},
		"required":             []string{"session_id", "child_id", "message"},
		"additionalProperties": false,
	}
}

func agentYieldInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Parent session ID whose sub-agents to wait for.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func agentListInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Parent session ID whose sub-agents to list.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func agentSpawnOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                 stringSchema("Child session ID."),
		"agent_name":         stringSchema("Name of the spawned agent."),
		"session_key":        stringSchema("Session key for the child."),
		"status":             stringSchema("Current status of the child session."),
		"delegation_applied": booleanSchema("Whether the parent run delegation contract was attached to the child task."),
		"delegation_scope":   stringSchema("Compact summary of the applied delegation contract."),
	}, "id", "agent_name", "session_key", "status")
}

func agentSendOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"child_id": stringSchema("Child session ID."),
		"status":   stringSchema("Current status of the child session."),
		"message":  stringSchema("Delivery acknowledgement."),
	}, "child_id", "status", "message")
}

func agentYieldOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":         stringSchema("Child session ID."),
		"agent_name": stringSchema("Name of the agent."),
		"status":     stringSchema("Terminal status of the child."),
		"result":     stringSchema("Result from the child session."),
	}, "id", "agent_name", "status")
	return objectSchema(map[string]any{
		"children": arraySchema(entry, "Completed child sessions."),
		"count":    integerSchema("Number of completed children."),
	}, "children", "count")
}

func agentListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":          stringSchema("Child session ID."),
		"agent_name":  stringSchema("Name of the agent."),
		"session_key": stringSchema("Session key for the child."),
		"status":      stringSchema("Current status of the child."),
		"created_at":  stringSchema("Creation time in RFC3339 format."),
	}, "id", "agent_name", "session_key", "status", "created_at")
	return objectSchema(map[string]any{
		"children": arraySchema(entry, "Sub-agent sessions."),
		"count":    integerSchema("Number of sub-agents."),
	}, "children", "count")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findChild looks up a specific child session by parent and child ID.
func findChild(children []isolation.ChildSession, childID string) *isolation.ChildSession {
	for i := range children {
		if children[i].ID == childID {
			return &children[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleAgentSpawn(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.spawner == nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: spawner not available")
	}

	agentName, err := requiredString(call.Input, "agent_name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: %w", err)
	}
	message, err := requiredString(call.Input, "message")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: %w", err)
	}

	run := builtinRunFromContext(ctx)
	if run != nil {
		if strings.TrimSpace(run.SessionID) != "" && run.SessionID != sessionID {
			return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: session_id %q does not match current run session %q", sessionID, run.SessionID)
		}
		if run.Delegation == nil {
			return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: delegation is not authorized for this run")
		}
		message = buildDelegatedChildMessage(message, run.Delegation)
	}

	child, err := b.spawner.Spawn(ctx, isolation.SpawnRequest{
		ParentSessionID: sessionID,
		AgentName:       agentName,
		Message:         message,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.spawn: %w", err)
	}

	scope := ""
	if run != nil && run.Delegation != nil {
		scope = renderDelegationScope(run.Delegation)
	}
	return b.jsonResult(call, map[string]any{
		"id":                 child.ID,
		"agent_name":         child.AgentName,
		"session_key":        child.SessionKey,
		"status":             child.Status,
		"delegation_applied": run != nil && run.Delegation != nil,
		"delegation_scope":   scope,
	})
}

func handleAgentSend(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.spawner == nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: spawner not available")
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: %w", err)
	}
	childID, err := requiredString(call.Input, "child_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: %w", err)
	}
	message, err := requiredString(call.Input, "message")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: %w", err)
	}

	if run := builtinRunFromContext(ctx); run != nil && strings.TrimSpace(run.SessionID) != "" && run.SessionID != sessionID {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: session_id %q does not match current run session %q", sessionID, run.SessionID)
	}

	if err := b.spawner.SendMessage(ctx, sessionID, childID, message); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.send: %w", err)
	}

	// Re-fetch child status after dispatching.
	children := b.spawner.ListChildren(sessionID)
	child := findChild(children, childID)
	status := "unknown"
	if child != nil {
		status = child.Status
	}

	return b.jsonResult(call, map[string]any{
		"child_id": childID,
		"status":   status,
		"message":  "message dispatched to child session",
	})
}

func handleAgentYield(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.spawner == nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.yield: spawner not available")
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.yield: %w", err)
	}
	if session := builtinSessionFromContext(ctx); session != nil && strings.TrimSpace(session.ID) != "" && session.ID != sessionID {
		return contextengine.ToolResult{}, fmt.Errorf("agent.yield: session_id %q does not match current session %q", sessionID, session.ID)
	}

	completed, err := b.spawner.Yield(sessionID)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.yield: %w", err)
	}

	entries := make([]map[string]any, 0, len(completed))
	for _, child := range completed {
		entries = append(entries, map[string]any{
			"id":         child.ID,
			"agent_name": child.AgentName,
			"status":     child.Status,
			"result":     child.Result,
		})
	}

	return b.jsonResult(call, map[string]any{
		"children": entries,
		"count":    len(entries),
	})
}

func handleAgentList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.spawner == nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.list: spawner not available")
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("agent.list: %w", err)
	}
	if session := builtinSessionFromContext(ctx); session != nil && strings.TrimSpace(session.ID) != "" && session.ID != sessionID {
		return contextengine.ToolResult{}, fmt.Errorf("agent.list: session_id %q does not match current session %q", sessionID, session.ID)
	}

	children := b.spawner.ListChildren(sessionID)
	entries := make([]map[string]any, 0, len(children))
	for _, child := range children {
		entries = append(entries, map[string]any{
			"id":          child.ID,
			"agent_name":  child.AgentName,
			"session_key": child.SessionKey,
			"status":      child.Status,
			"created_at":  formatTimeOrEmpty(child.CreatedAt),
		})
	}

	return b.jsonResult(call, map[string]any{
		"children": entries,
		"count":    len(entries),
	})
}

func buildDelegatedChildMessage(message string, contract *agent.DelegationContract) string {
	if contract == nil {
		return strings.TrimSpace(message)
	}
	lines := []string{
		"<delegation_contract>",
		"You are a child agent operating under the parent run's bounded delegation contract.",
	}
	if goal := strings.TrimSpace(contract.Goal); goal != "" {
		lines = append(lines, "Goal: "+goal)
	}
	if len(contract.AllowedDomains) > 0 {
		lines = append(lines, "Allowed domains: "+strings.Join(contract.AllowedDomains, ", "))
	}
	if len(contract.AllowedTools) > 0 {
		lines = append(lines, "Allowed tools: "+strings.Join(contract.AllowedTools, ", "))
	}
	if sideEffect := strings.TrimSpace(contract.SideEffectClass); sideEffect != "" {
		lines = append(lines, "Side-effect ceiling: "+sideEffect)
	}
	if contract.MaxTurns > 0 {
		lines = append(lines, fmt.Sprintf("Max turns: %d", contract.MaxTurns))
	}
	if contract.MaxBudgetTokens > 0 {
		lines = append(lines, fmt.Sprintf("Max budget tokens: %d", contract.MaxBudgetTokens))
	}
	lines = append(lines, fmt.Sprintf("Requires approval before privileged side effects: %t", contract.RequiresApproval))
	if ref := strings.TrimSpace(contract.VerificationPlanRef); ref != "" {
		lines = append(lines, "Verification plan: "+ref)
	}
	lines = append(lines,
		"Rules: do not expand privileges or spawn further sub-agents unless explicitly re-authorized by the parent run.",
		"</delegation_contract>",
		"",
		strings.TrimSpace(message),
	)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderDelegationScope(contract *agent.DelegationContract) string {
	if contract == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if len(contract.AllowedDomains) > 0 {
		parts = append(parts, "domains="+strings.Join(contract.AllowedDomains, ","))
	}
	if sideEffect := strings.TrimSpace(contract.SideEffectClass); sideEffect != "" {
		parts = append(parts, "side_effect="+sideEffect)
	}
	if contract.MaxTurns > 0 {
		parts = append(parts, fmt.Sprintf("max_turns=%d", contract.MaxTurns))
	}
	if contract.MaxBudgetTokens > 0 {
		parts = append(parts, fmt.Sprintf("max_budget_tokens=%d", contract.MaxBudgetTokens))
	}
	return strings.Join(parts, " ")
}
