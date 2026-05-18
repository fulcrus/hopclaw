package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/skill"
)

// TestTool executes a single read-only tool call through the real runtime
// preparation and policy path without appending messages to a session.
func (a *AgentComponent) TestTool(ctx context.Context, sessionKey string, call ToolCall) (ToolDefinition, contextengine.ToolResult, error) {
	if a.context == nil {
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("%w", ErrContextEngineNil)
	}
	if a.tools == nil {
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("%w", ErrToolExecutorNil)
	}
	call.Name = strings.TrimSpace(call.Name)
	if call.Name == "" {
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("tool name is required")
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool-test-%d", time.Now().UTC().UnixNano())
	}

	session, err := a.sessionForToolTest(ctx, sessionKey)
	if err != nil {
		return ToolDefinition{}, contextengine.ToolResult{}, err
	}

	runtimeCtx, err := a.runtime.Current(ctx, session, nil)
	if err != nil {
		return ToolDefinition{}, contextengine.ToolResult{}, err
	}
	a.injectPromptMemoryFacts(ctx, session, runtimeCtx)
	prepared, _, err := a.context.Prepare(ctx, &session.Session, toContextRun(nil, a.systemPromptFor(nil, session, runtimeCtx)), runtimeCtx)
	if err != nil {
		return ToolDefinition{}, contextengine.ToolResult{}, err
	}

	definition, ok := findToolDefinition(buildAllowedToolDefinitions(prepared.Skills, session, a.tools, nil), call.Name)
	if !ok {
		if bound, ok := findBoundTool(prepared.Skills, call.Name); ok && !bound.Eligibility.Eligible {
			return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("tool %q is unavailable: %s", call.Name, strings.Join(bound.Eligibility.Reasons, "; "))
		}
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("tool %q is not available", call.Name)
	}
	if !strings.EqualFold(strings.TrimSpace(definition.SideEffectClass), "read") || definition.RequiresApproval {
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("tool %q is not testable because it is not read-only", call.Name)
	}

	previewRun := &Run{
		ID:        call.ID + "-run",
		SessionID: session.ID,
		Status:    RunRunning,
		Phase:     PhaseExecutingTools,
		Model:     session.Model,
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.checkToolTestPolicy(ctx, previewRun, session, prepared.Skills, call); err != nil {
		return ToolDefinition{}, contextengine.ToolResult{}, err
	}

	results, err := a.tools.ExecuteBatch(ctx, previewRun, session, []ToolCall{call})
	if err != nil {
		return ToolDefinition{}, contextengine.ToolResult{}, err
	}
	if len(results) != 1 {
		return ToolDefinition{}, contextengine.ToolResult{}, fmt.Errorf("tool %q returned %d results, want 1", call.Name, len(results))
	}
	return definition, results[0], nil
}

func (a *AgentComponent) sessionForToolTest(ctx context.Context, sessionKey string) (*Session, error) {
	key := strings.TrimSpace(sessionKey)
	if key != "" && a.sessions != nil {
		return a.sessions.GetOrCreate(ctx, key, defaultString(a.config.DefaultModel, "tool-test-model"))
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("tool-test-%d", now.UnixNano())
	return &Session{
		ID:        id,
		Key:       defaultString(key, id),
		Model:     defaultString(a.config.DefaultModel, "tool-test-model"),
		CreatedAt: now,
		UpdatedAt: now,
		Session: contextengine.Session{
			ID: id,
		},
	}, nil
}

func (a *AgentComponent) checkToolTestPolicy(ctx context.Context, run *Run, session *Session, snapshot skill.SessionSkillSnapshot, call ToolCall) error {
	if a.policy == nil {
		return nil
	}
	bound := a.resolveTool(session, snapshot, call.Name)
	decision, err := a.policy.EvaluateTool(ctx, policy.ToolContext{
		RunID:     run.ID,
		SessionID: session.ID,
		ToolName:  call.Name,
		Input:     cloneMap(call.Input),
		Tool:      bound,
	})
	if err != nil {
		return err
	}
	switch decision.Action {
	case policy.ActionAllow:
		return nil
	case policy.ActionRequireApproval:
		return fmt.Errorf("tool %q requires approval and cannot be tested from the operator API", call.Name)
	case policy.ActionDeny:
		if len(decision.Reasons) == 0 {
			return fmt.Errorf("tool %q is denied by policy", call.Name)
		}
		return fmt.Errorf("tool %q is denied by policy: %s", call.Name, strings.Join(decision.Reasons, "; "))
	default:
		return fmt.Errorf("tool %q is blocked by unsupported policy action %q", call.Name, decision.Action)
	}
}

func findToolDefinition(definitions []ToolDefinition, name string) (ToolDefinition, bool) {
	trimmed := strings.TrimSpace(name)
	for _, definition := range definitions {
		if strings.EqualFold(strings.TrimSpace(definition.Name), trimmed) {
			return definition, true
		}
	}
	return ToolDefinition{}, false
}

func findBoundTool(snapshot skill.SessionSkillSnapshot, name string) (*skill.BoundTool, bool) {
	trimmed := strings.TrimSpace(name)
	for _, bound := range snapshot.Ordered {
		for _, manifest := range bound.Package.ToolManifests {
			if manifestMatchesToolName(manifest, trimmed) {
				copied := skill.BoundTool{
					Package:     bound.Package,
					Manifest:    manifest,
					Eligibility: bound.Eligibility,
				}
				return &copied, true
			}
		}
	}
	return nil, false
}

func manifestMatchesToolName(manifest skill.ToolManifest, name string) bool {
	if strings.EqualFold(strings.TrimSpace(manifest.Name), strings.TrimSpace(name)) {
		return true
	}
	for _, alias := range manifest.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
