package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/hooks"
)

var ErrHookRejected = errors.New("action rejected by hook")

type HookDispatcher interface {
	Fire(ctx context.Context, trigger hooks.TriggerEvent, phase hooks.HookPhase, payload map[string]any) []hooks.HookResult
}

type hookedToolExecutor struct {
	inner      ToolExecutor
	dispatcher HookDispatcher
}

func wrapToolExecutorWithHooks(inner ToolExecutor, dispatcher HookDispatcher) ToolExecutor {
	if inner == nil || dispatcher == nil {
		return inner
	}
	if wrapped, ok := inner.(*hookedToolExecutor); ok {
		wrapped.dispatcher = dispatcher
		return wrapped
	}
	return &hookedToolExecutor{inner: inner, dispatcher: dispatcher}
}

func (e *hookedToolExecutor) ExecuteBatch(ctx context.Context, run *Run, session *Session, calls []ToolCall) ([]contextengine.ToolResult, error) {
	if e == nil || e.inner == nil {
		return nil, ErrToolExecutorNil
	}
	for idx, call := range calls {
		if err := fireHookBlocking(ctx, e.dispatcher, hooks.TriggerBeforeToolCall, hooks.HookPhasePre, buildToolCallHookPayload(run, session, call, idx, len(calls))); err != nil {
			return nil, err
		}
	}

	results, err := e.inner.ExecuteBatch(ctx, run, session, calls)
	if err != nil {
		for idx, call := range calls {
			fireHookNonBlocking(ctx, e.dispatcher, hooks.TriggerAfterToolCall, hooks.HookPhaseError, buildToolCallErrorHookPayload(run, session, call, idx, len(calls), err))
		}
		return nil, err
	}

	for idx, call := range calls {
		fireHookNonBlocking(ctx, e.dispatcher, hooks.TriggerAfterToolCall, hooks.HookPhasePost, buildToolCallResultHookPayload(run, session, call, resultForToolCall(results, call.ID, call.Name), idx, len(calls)))
	}
	return results, nil
}

func (e *hookedToolExecutor) ToolDefinitions(session *Session) []ToolDefinition {
	if e == nil || e.inner == nil {
		return nil
	}
	provider, ok := e.inner.(ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return provider.ToolDefinitions(session)
}

func (e *hookedToolExecutor) ResolveTool(session *Session, name string) (*ResolvedTool, bool) {
	if e == nil || e.inner == nil {
		return nil, false
	}
	resolver, ok := e.inner.(ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}

func (a *AgentComponent) WithHooks(dispatcher HookDispatcher) *AgentComponent {
	if a == nil {
		return a
	}
	a.hooks = dispatcher
	a.tools = wrapToolExecutorWithHooks(a.tools, dispatcher)
	return a
}

func (a *AgentComponent) emitMessageReceivedHook(ctx context.Context, session *Session, msg IncomingMessage) {
	fireHookNonBlocking(ctx, a.hooks, hooks.TriggerMessageReceived, hooks.HookPhasePost, buildMessageReceivedHookPayload(session, msg))
}

func (a *AgentComponent) beforeAgentStartHook(ctx context.Context, run *Run) error {
	return fireHookBlocking(ctx, a.hooks, hooks.TriggerBeforeAgentStart, hooks.HookPhasePre, buildRunLifecycleHookPayload(run, nil, "", ""))
}

func (a *AgentComponent) afterAgentEndHook(ctx context.Context, run *Run, session *Session, phase hooks.HookPhase, summary string, runErr error) {
	fireHookNonBlocking(ctx, a.hooks, hooks.TriggerAfterAgentEnd, phase, buildRunLifecycleHookPayload(run, session, summary, runErrString(runErr)))
}

func fireHookBlocking(ctx context.Context, dispatcher HookDispatcher, trigger hooks.TriggerEvent, phase hooks.HookPhase, payload map[string]any) error {
	if dispatcher == nil {
		return nil
	}
	results := dispatcher.Fire(ctx, trigger, phase, payload)
	for _, result := range results {
		if strings.TrimSpace(result.Status) != "error" {
			continue
		}
		label := strings.TrimSpace(result.HookName)
		if label == "" {
			label = strings.TrimSpace(result.HookID)
		}
		if label == "" {
			label = string(trigger)
		}
		message := strings.TrimSpace(result.Error)
		if message == "" {
			message = "hook rejected action"
		}
		return fmt.Errorf("%w: %s: %s", ErrHookRejected, label, message)
	}
	return nil
}

func fireHookNonBlocking(ctx context.Context, dispatcher HookDispatcher, trigger hooks.TriggerEvent, phase hooks.HookPhase, payload map[string]any) {
	if dispatcher == nil {
		return
	}
	results := dispatcher.Fire(ctx, trigger, phase, payload)
	for _, result := range results {
		if strings.TrimSpace(result.Status) != "error" {
			continue
		}
		log.Warn("hook execution failed",
			"trigger", trigger,
			"phase", phase,
			"hook_id", result.HookID,
			"hook_name", result.HookName,
			"error", result.Error,
		)
	}
}

func buildMessageReceivedHookPayload(session *Session, msg IncomingMessage) map[string]any {
	payload := map[string]any{
		"session_id":         strings.TrimSpace(msg.SessionID),
		"session_key":        strings.TrimSpace(msg.SessionKey),
		"external_event_id":  strings.TrimSpace(msg.ExternalEventID),
		"content":            strings.TrimSpace(msg.Content),
		"model":              strings.TrimSpace(msg.Model),
		"scope":              scopeFromIncomingMessage(msg),
		"metadata":           cloneMap(msg.Metadata),
		"message_role":       string(contextengine.RoleUser),
		"message_created_at": time.Now().UTC(),
	}
	if session != nil {
		payload["session_id"] = strings.TrimSpace(session.ID)
		payload["session_key"] = strings.TrimSpace(session.Key)
		payload["session_model"] = strings.TrimSpace(session.Model)
		payload["session_revision"] = session.Revision
		payload["scope"] = session.Scope.Normalize()
	}
	return payload
}

func buildRunLifecycleHookPayload(run *Run, session *Session, summary string, errorText string) map[string]any {
	payload := map[string]any{
		"summary": strings.TrimSpace(summary),
		"error":   strings.TrimSpace(errorText),
	}
	if run != nil {
		payload["run_id"] = strings.TrimSpace(run.ID)
		payload["session_id"] = strings.TrimSpace(run.SessionID)
		payload["status"] = string(run.Status)
		payload["phase"] = string(run.Phase)
		payload["queue_mode"] = string(run.QueueMode)
		payload["execution_mode"] = string(run.ExecutionMode)
		payload["model"] = strings.TrimSpace(run.Model)
		payload["tool_rounds"] = run.ToolRounds
		payload["tool_recoveries"] = run.ToolRecoveryCount
		payload["approval_id"] = strings.TrimSpace(run.ApprovalID)
		if scope := run.Scope.Normalize(); !scope.IsZero() {
			payload["scope"] = scope
		}
		if run.TaskContract != nil {
			payload["task_contract"] = cloneTaskContract(run.TaskContract)
		}
		if run.EffectiveProfile != nil {
			payload["effective_profile"] = cloneEffectiveAgentProfile(run.EffectiveProfile)
		}
		payload = mergeEventAttrs(payload, buildGovernanceEventAttrs(run))
	}
	if session != nil {
		payload["session_id"] = strings.TrimSpace(session.ID)
		payload["session_key"] = strings.TrimSpace(session.Key)
		payload["session_model"] = strings.TrimSpace(session.Model)
		payload["session_revision"] = session.Revision
		if scope := session.Scope.Normalize(); !scope.IsZero() {
			payload["scope"] = scope
		}
	}
	return payload
}

func buildToolCallHookPayload(run *Run, session *Session, call ToolCall, index, total int) map[string]any {
	payload := buildRunLifecycleHookPayload(run, session, "", "")
	payload["tool_name"] = strings.TrimSpace(call.Name)
	payload["tool_call_id"] = strings.TrimSpace(call.ID)
	payload["tool_input"] = cloneMap(call.Input)
	payload["tool_index"] = index
	payload["tool_count"] = total
	return payload
}

func buildToolCallErrorHookPayload(run *Run, session *Session, call ToolCall, index, total int, err error) map[string]any {
	payload := buildToolCallHookPayload(run, session, call, index, total)
	if err != nil {
		payload["error"] = strings.TrimSpace(err.Error())
	}
	payload["result_status"] = "error"
	return payload
}

func buildToolCallResultHookPayload(run *Run, session *Session, call ToolCall, result *contextengine.ToolResult, index, total int) map[string]any {
	payload := buildToolCallHookPayload(run, session, call, index, total)
	if result == nil {
		return payload
	}
	normalized := result.Normalized()
	payload["result_status"] = string(normalized.Status)
	payload["result_summary"] = strings.TrimSpace(normalized.Summary)
	payload["result"] = normalized.MarshalMetadata()
	if normalized.Error != nil {
		payload["error"] = strings.TrimSpace(normalized.Error.Message)
	}
	if artifactURI := strings.TrimSpace(normalized.ArtifactURI); artifactURI != "" {
		payload["artifact_uri"] = artifactURI
	}
	return payload
}

func resultForToolCall(results []contextengine.ToolResult, toolCallID, toolName string) *contextengine.ToolResult {
	for _, result := range results {
		normalized := result.Normalized()
		if strings.TrimSpace(toolCallID) != "" && normalized.ToolCallID == strings.TrimSpace(toolCallID) {
			copy := normalized
			return &copy
		}
		if strings.TrimSpace(toolCallID) == "" && strings.TrimSpace(toolName) != "" && normalized.ToolName == strings.TrimSpace(toolName) {
			copy := normalized
			return &copy
		}
	}
	return nil
}

func scopeFromIncomingMessage(msg IncomingMessage) any {
	scope := map[string]any{
		"automation_id": strings.TrimSpace(msg.AutomationID),
	}
	if scope["automation_id"] == "" {
		return nil
	}
	return scope
}

func runErrString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
