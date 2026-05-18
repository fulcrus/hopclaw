package toolruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

type sideEffectBoundaryExecutor struct {
	inner agent.ToolExecutor
}

type observedSideEffects struct {
	filesystemWrite  bool
	processSpawn     bool
	externalNetWrite bool
	classified       bool
}

func WithSideEffectBoundary() ToolMiddleware {
	return func(next agent.ToolExecutor) agent.ToolExecutor {
		return &sideEffectBoundaryExecutor{inner: next}
	}
}

func (e *sideEffectBoundaryExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if e.inner == nil {
		return nil, agent.ErrToolExecutorNil
	}
	results := make([]contextengine.ToolResult, len(calls))
	delegateCalls := make([]agent.ToolCall, 0, len(calls))
	delegateIndexes := make([]int, 0, len(calls))
	for i, call := range calls {
		resolved, ok := e.ResolveTool(session, call.Name)
		if !ok || resolved == nil {
			results[i] = batchErrorResult(call, fmt.Errorf("tool %q cannot run because side-effect metadata could not be resolved", call.Name))
			continue
		}
		if err := validateObservedSideEffects(resolved, call); err != nil {
			results[i] = batchErrorResult(call, err)
			continue
		}
		delegateCalls = append(delegateCalls, call)
		delegateIndexes = append(delegateIndexes, i)
	}
	if len(delegateCalls) == 0 {
		return results, nil
	}
	delegateResults, err := e.inner.ExecuteBatch(ctx, run, session, delegateCalls)
	for i, result := range delegateResults {
		if i >= len(delegateIndexes) {
			break
		}
		results[delegateIndexes[i]] = result
	}
	return results, err
}

func (e *sideEffectBoundaryExecutor) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	provider, ok := e.inner.(agent.ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return provider.ToolDefinitions(session)
}

func (e *sideEffectBoundaryExecutor) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	resolver, ok := e.inner.(agent.ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}

func validateObservedSideEffects(resolved *agent.ResolvedTool, call agent.ToolCall) error {
	if resolved == nil {
		return nil
	}
	declared := declaredRuntimeBoundary(resolved)
	observed := inferObservedSideEffects(call)
	if !observed.classified && declared.Class == skill.SideEffectRead {
		return fmt.Errorf("tool %q declares side_effect_class %q but runtime could not verify it as read-only", call.Name, declared.Class)
	}
	switch {
	case observed.processSpawn && !declared.AllowsProcessSpawn:
		return fmt.Errorf("tool %q violates side_effect_class %q: process execution is not allowed", call.Name, declared.Class)
	case observed.filesystemWrite && !declared.AllowsFilesystemWrite:
		return fmt.Errorf("tool %q violates side_effect_class %q: filesystem writes are not allowed", call.Name, declared.Class)
	case observed.externalNetWrite && !declared.AllowsExternalNetworkWrite:
		return fmt.Errorf("tool %q violates side_effect_class %q: external network writes are not allowed", call.Name, declared.Class)
	default:
		return nil
	}
}

func declaredRuntimeBoundary(resolved *agent.ResolvedTool) skill.RuntimeBoundaryPolicy {
	if resolved == nil {
		return skill.RuntimeBoundaryForSideEffect("")
	}
	if sideEffect := strings.TrimSpace(resolved.Manifest.SideEffectClass); sideEffect != "" {
		return skill.RuntimeBoundaryForSideEffect(sideEffect)
	}
	return skill.RuntimeBoundaryForSideEffect(resolved.Descriptor.SideEffectClass)
}

func inferObservedSideEffects(call agent.ToolCall) observedSideEffects {
	domain, action := splitToolName(call.Name)
	switch domain {
	case "fs":
		if !fsReadActions[action] {
			return observedSideEffects{filesystemWrite: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "exec":
		if action != "which" {
			return observedSideEffects{processSpawn: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "proc":
		if action == "start" || action == "stop" {
			return observedSideEffects{processSpawn: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "net":
		return inferNetObservedSideEffects(action, call.Input)
	case "web", "search", "news":
		return observedSideEffects{classified: true}
	case "email":
		switch action {
		case "send":
			return observedSideEffects{externalNetWrite: true, classified: true}
		case "download_attachment":
			return observedSideEffects{filesystemWrite: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "calendar":
		if action != "list_events" {
			return observedSideEffects{externalNetWrite: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "channel":
		if !channelReadActions[action] {
			return observedSideEffects{externalNetWrite: true, classified: true}
		}
		return observedSideEffects{classified: true}
	case "browser":
		switch {
		case browserReadActions[action]:
			return observedSideEffects{classified: true}
		case action == "open" || action == "close":
			return observedSideEffects{filesystemWrite: true, classified: true}
		default:
			return observedSideEffects{externalNetWrite: true, classified: true}
		}
	case "watch", "cron", "wakeup":
		if lifecycleReadActions[action] {
			return observedSideEffects{classified: true}
		}
		if action == "run" {
			return observedSideEffects{externalNetWrite: true, classified: true}
		}
		return observedSideEffects{filesystemWrite: true, classified: true}
	case "semantic":
		if action == "catalog" || action == "inspect_context" {
			return observedSideEffects{classified: true}
		}
		return observedSideEffects{externalNetWrite: true, classified: true}
	}
	return inferGenericObservedSideEffects(action, call.Input)
}

var fsReadActions = map[string]bool{
	"list":    true,
	"tree":    true,
	"find":    true,
	"grep":    true,
	"read":    true,
	"stat":    true,
	"hash":    true,
	"diff":    true,
	"changes": true,
}

var channelReadActions = map[string]bool{
	"list":    true,
	"status":  true,
	"history": true,
}

var browserReadActions = map[string]bool{
	"screenshot":         true,
	"screenshot_labeled": true,
	"snapshot":           true,
	"snapshot_aria":      true,
	"wait":               true,
	"tabs":               true,
	"cookies":            true,
	"page_info":          true,
	"downloads":          true,
	"storage_get":        true,
	"location":           true,
	"title":              true,
	"selection":          true,
	"network_events":     true,
	"console_logs":       true,
	"requests":           true,
	"download_status":    true,
	"pdf_text":           true,
}

var lifecycleReadActions = map[string]bool{
	"list":   true,
	"status": true,
	"get":    true,
}

func inferNetObservedSideEffects(action string, input map[string]any) observedSideEffects {
	switch action {
	case "http":
		if isSafeHTTPMethod(fmt.Sprint(input["method"])) {
			return observedSideEffects{classified: true}
		}
		return observedSideEffects{externalNetWrite: true, classified: true}
	case "download":
		return observedSideEffects{filesystemWrite: true, classified: true}
	case "upload":
		return observedSideEffects{externalNetWrite: true, classified: true}
	case "serve":
		return observedSideEffects{filesystemWrite: true, classified: true}
	default:
		return inferGenericObservedSideEffects(action, input)
	}
}

func inferGenericObservedSideEffects(action string, input map[string]any) observedSideEffects {
	action = strings.ToLower(strings.TrimSpace(action))
	switch {
	case action == "":
		return inferObservedSideEffectsFromInput(input)
	case genericReadActions[action]:
		return observedSideEffects{classified: true}
	case action == "run" || action == "shell" || action == "script" || action == "spawn" || action == "start" || action == "stop":
		return observedSideEffects{processSpawn: true, classified: true}
	case action == "send" || action == "upload" || action == "deliver" || action == "notify" || action == "react":
		return observedSideEffects{externalNetWrite: true, classified: true}
	case strings.HasPrefix(action, "write"),
		strings.HasPrefix(action, "edit"),
		strings.HasPrefix(action, "patch"),
		strings.HasPrefix(action, "delete"),
		strings.HasPrefix(action, "remove"),
		strings.HasPrefix(action, "append"),
		strings.HasPrefix(action, "move"),
		strings.HasPrefix(action, "copy"),
		strings.HasPrefix(action, "mkdir"),
		strings.HasPrefix(action, "create"),
		strings.HasPrefix(action, "update"),
		strings.HasPrefix(action, "set"):
		if strings.TrimSpace(fmt.Sprint(input["url"])) != "" || strings.TrimSpace(fmt.Sprint(input["endpoint"])) != "" {
			return observedSideEffects{externalNetWrite: true, classified: true}
		}
		return observedSideEffects{filesystemWrite: true, classified: true}
	default:
		return inferObservedSideEffectsFromInput(input)
	}
}

var genericReadActions = map[string]bool{
	"cat": true, "check": true, "describe": true, "details": true,
	"diff": true, "find": true, "fetch": true, "get": true,
	"grep": true, "hash": true, "head": true, "history": true,
	"info": true, "inspect": true, "list": true, "lookup": true,
	"ls": true, "preview": true, "query": true, "read": true,
	"resolve": true, "scan": true, "search": true, "show": true,
	"snapshot": true, "stat": true, "status": true, "tail": true,
	"test": true, "view": true, "wait": true, "which": true,
}

func inferObservedSideEffectsFromInput(input map[string]any) observedSideEffects {
	if len(input) == 0 {
		return observedSideEffects{}
	}
	for _, key := range []string{"command", "cmd", "script", "shell", "interpreter"} {
		if strings.TrimSpace(fmt.Sprint(input[key])) != "" {
			return observedSideEffects{processSpawn: true, classified: true}
		}
	}
	if args, ok := input["args"]; ok {
		switch typed := args.(type) {
		case []any:
			if len(typed) > 0 {
				return observedSideEffects{processSpawn: true, classified: true}
			}
		case []string:
			if len(typed) > 0 {
				return observedSideEffects{processSpawn: true, classified: true}
			}
		}
	}
	if !isSafeHTTPMethod(fmt.Sprint(input["method"])) && (strings.TrimSpace(fmt.Sprint(input["url"])) != "" || strings.TrimSpace(fmt.Sprint(input["endpoint"])) != "") {
		return observedSideEffects{externalNetWrite: true, classified: true}
	}
	return observedSideEffects{}
}

func splitToolName(name string) (string, string) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(name)), ".")
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[len(parts)-1]
}

func isSafeHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", "GET", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}
