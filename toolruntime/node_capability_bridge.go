package toolruntime

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	registry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

type nodeCapabilityBridge struct {
	sessionCap registry.SessionCapability
	entries    map[string]nodeCapabilityBridgeEntry
	order      []string
}

type nodeCapabilityBridgeEntry struct {
	toolName         string
	desktopOperation string
	definition       agent.ToolDefinition
	bound            skill.BoundTool
}

func NewNodeCapabilityBridge(reg *registry.Registry) agent.ToolExecutor {
	if reg == nil {
		return nil
	}
	capability, ok := reg.Get("desktop")
	if !ok {
		return nil
	}
	sessionCap, ok := capability.(registry.SessionCapability)
	if !ok {
		return nil
	}

	supportedOps := make(map[string]struct{})
	for _, op := range sessionCap.Manifest().Operations {
		name := strings.TrimSpace(op.Name)
		if name != "" {
			supportedOps[name] = struct{}{}
		}
	}

	pkg := &skill.SkillPackage{
		ID:     "capability-bridge-nodes",
		Kind:   skill.SkillKindExecutable,
		Status: skill.StatusReady,
		Prompt: skill.PromptSkill{
			Name:        "nodes-capability-bridge",
			Description: "Legacy nodes tools backed by desktop capability",
			Location:    "capability-bridge:nodes",
		},
		Source: skill.SkillSource{
			Kind: skill.SourceBundled,
			Dir:  "capability-bridge",
			Root: "capability-bridge",
		},
		Trust: skill.TrustInternal,
	}

	bridge := &nodeCapabilityBridge{
		sessionCap: sessionCap,
		entries:    make(map[string]nodeCapabilityBridgeEntry),
	}
	for _, item := range []struct {
		toolName         string
		description      string
		desktopOperation string
		inputSchema      map[string]any
		outputSchema     map[string]any
		sideEffectClass  string
		idempotent       bool
		requiresApproval bool
		executionKey     string
	}{
		{
			toolName:         "nodes.screen_capture",
			description:      "Capture a screenshot and save it to disk.",
			desktopOperation: "screenshot",
			inputSchema:      nodesScreenCaptureInputSchema(),
			outputSchema:     nodesScreenCaptureOutputSchema(),
			sideEffectClass:  "local_write",
			executionKey:     "nodes:screen_capture",
		},
		{
			toolName:         "nodes.screen_record",
			description:      "Record the screen for a given duration and save to a video file.",
			desktopOperation: "screen_record",
			inputSchema:      nodesScreenRecordInputSchema(),
			outputSchema:     nodesScreenRecordOutputSchema(),
			sideEffectClass:  "local_write",
			executionKey:     "nodes:screen_record",
		},
		{
			toolName:         "nodes.clipboard_read",
			description:      "Read the current contents of the system clipboard.",
			desktopOperation: "clipboard_read",
			inputSchema:      nodesClipboardReadInputSchema(),
			outputSchema:     nodesClipboardReadOutputSchema(),
			sideEffectClass:  "read",
			idempotent:       true,
			executionKey:     "nodes:clipboard_read",
		},
		{
			toolName:         "nodes.clipboard_write",
			description:      "Write text content to the system clipboard.",
			desktopOperation: "clipboard_write",
			inputSchema:      nodesClipboardWriteInputSchema(),
			outputSchema:     nodesClipboardWriteOutputSchema(),
			sideEffectClass:  "local_write",
			executionKey:     "nodes:clipboard_write",
		},
	} {
		if _, ok := supportedOps[item.desktopOperation]; !ok {
			continue
		}
		definition := agent.ToolDefinition{
			Name:             item.toolName,
			Description:      item.description,
			InputSchema:      cloneSchema(item.inputSchema),
			OutputSchema:     cloneSchema(item.outputSchema),
			SideEffectClass:  item.sideEffectClass,
			Idempotent:       item.idempotent,
			RequiresApproval: item.requiresApproval,
			ExecutionKey:     item.executionKey,
			Source:           "capability_bridge",
			SourceRef:        "desktop:" + item.desktopOperation,
			Trust:            string(skill.TrustInternal),
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		}
		bound := skill.BoundTool{
			Package: pkg,
			Manifest: skill.ToolManifest{
				Name:             item.toolName,
				Description:      item.description,
				InputSchema:      cloneSchema(item.inputSchema),
				OutputSchema:     cloneSchema(item.outputSchema),
				SideEffectClass:  item.sideEffectClass,
				Idempotent:       item.idempotent,
				RequiresApproval: item.requiresApproval,
				ExecutionKey:     item.executionKey,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		}
		bridge.entries[item.toolName] = nodeCapabilityBridgeEntry{
			toolName:         item.toolName,
			desktopOperation: item.desktopOperation,
			definition:       definition,
			bound:            bound,
		}
		bridge.order = append(bridge.order, item.toolName)
	}
	if len(bridge.order) == 0 {
		return nil
	}
	return bridge
}

func (b *nodeCapabilityBridge) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if b == nil || len(b.order) == 0 {
		return nil
	}
	health := b.sessionCap.Health(context.Background())
	out := make([]agent.ToolDefinition, 0, len(b.order))
	for _, name := range b.order {
		entry := b.entries[name]
		definition := copyToolDefinition(entry.definition)
		if health.Status != captypes.StatusReady {
			definition.Eligible = false
			definition.EligibilityReasons = availabilityReasons(health)
			definition.Availability = availabilityForHealth(health, definition.EligibilityReasons)
		}
		out = append(out, definition)
	}
	return out
}

func (b *nodeCapabilityBridge) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if b == nil {
		return nil, false
	}
	entry, ok := b.entries[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}
	definition := copyToolDefinition(entry.definition)
	health := b.sessionCap.Health(context.Background())
	bound := entry.bound
	if health.Status != captypes.StatusReady {
		definition.Eligible = false
		definition.EligibilityReasons = availabilityReasons(health)
		definition.Availability = availabilityForHealth(health, definition.EligibilityReasons)
		bound.Eligibility = skill.EligibilityResult{
			Eligible: false,
			Reasons:  append([]string(nil), definition.EligibilityReasons...),
		}
	}
	return resolvedToolFromBinding(&bound, definition, "capability_bridge:desktop"), true
}

func (b *nodeCapabilityBridge) ExecuteBatch(ctx context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if b == nil {
		return nil, agent.ErrToolExecutorNil
	}
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		entry, ok := b.entries[strings.TrimSpace(call.Name)]
		if !ok {
			results = append(results, batchErrorResult(call, fmt.Errorf("tool %q is not registered", call.Name)))
			continue
		}
		result, err := b.executeOne(ctx, entry, call)
		if err != nil {
			results = append(results, batchErrorResult(call, err))
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

func (b *nodeCapabilityBridge) executeOne(ctx context.Context, entry nodeCapabilityBridgeEntry, call agent.ToolCall) (contextengine.ToolResult, error) {
	health := b.sessionCap.Health(ctx)
	if health.Status != captypes.StatusReady {
		if strings.TrimSpace(health.Message) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s is unavailable: %s", call.Name, health.Message)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s is unavailable", call.Name)
	}

	handle, err := b.sessionCap.OpenSession(ctx, nil)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer func() {
		_ = b.sessionCap.CloseSession(context.Background(), handle.ID)
	}()

	switch entry.desktopOperation {
	case "screenshot":
		return b.executeScreenCapture(ctx, handle.ID, call)
	case "screen_record":
		return b.executeScreenRecord(ctx, handle.ID, call)
	case "clipboard_read":
		return b.executeClipboardRead(ctx, handle.ID, call)
	case "clipboard_write":
		return b.executeClipboardWrite(ctx, handle.ID, call)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("%s bridge operation %q is not supported", call.Name, entry.desktopOperation)
	}
}

func (b *nodeCapabilityBridge) executeScreenCapture(ctx context.Context, sessionID string, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	result, err := b.sessionCap.Invoke(ctx, captypes.InvokeRequest{
		Operation: "screenshot",
		SessionID: sessionID,
		Params:    map[string]any{},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if result == nil || !result.OK {
		if result != nil && strings.TrimSpace(result.Error) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s: %s", call.Name, result.Error)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s failed", call.Name)
	}
	bodyBase64 := strings.TrimSpace(mapStringValue(result.Data, "content_base64"))
	if bodyBase64 == "" {
		return contextengine.ToolResult{}, fmt.Errorf("%s did not return screenshot data", call.Name)
	}
	body, err := base64.StdEncoding.DecodeString(bodyBase64)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s decode screenshot payload: %w", call.Name, err)
	}
	if err := os.WriteFile(absPath, body, 0o644); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s write screenshot: %w", call.Name, err)
	}
	return gatewayJSONResult(call, map[string]any{
		"ok":         true,
		"path":       absPath,
		"size_bytes": len(body),
	})
}

func (b *nodeCapabilityBridge) executeScreenRecord(ctx context.Context, sessionID string, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	durationSec, err := intFrom(call.Input["duration_sec"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	if durationSec <= 0 {
		return contextengine.ToolResult{}, fmt.Errorf("%s: duration_sec must be positive", call.Name)
	}
	if durationSec > nodesScreenRecordMaxDuration {
		return contextengine.ToolResult{}, fmt.Errorf("%s: duration_sec exceeds maximum of %d", call.Name, nodesScreenRecordMaxDuration)
	}
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	fps, _ := intFrom(call.Input["fps"], nodesScreenRecordDefaultFPS)
	if fps <= 0 || fps > nodesScreenRecordMaxFPS {
		fps = nodesScreenRecordDefaultFPS
	}
	quality := stringFromDefault(call.Input["quality"], "medium")
	switch quality {
	case "low", "medium", "high":
	default:
		quality = "medium"
	}
	params := map[string]any{
		"output_path":  absPath,
		"duration_sec": durationSec,
	}
	if audio, err := boolFrom(call.Input["audio"]); err == nil {
		params["audio"] = audio
	}
	if display, err := stringFrom(call.Input["display"]); err == nil && strings.TrimSpace(display) != "" {
		params["display"] = display
	}
	result, err := b.sessionCap.Invoke(ctx, captypes.InvokeRequest{
		Operation: "screen_record",
		SessionID: sessionID,
		Params:    params,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if result == nil || !result.OK {
		if result != nil && strings.TrimSpace(result.Error) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s: %s", call.Name, result.Error)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s failed", call.Name)
	}
	pathValue := strings.TrimSpace(mapStringValue(result.Data, "path"))
	if pathValue == "" {
		pathValue = absPath
	}
	sizeBytes := int64Value(result.Data, "size_bytes")
	if sizeBytes == 0 {
		if info, statErr := os.Stat(pathValue); statErr == nil {
			sizeBytes = info.Size()
		}
	}
	actualDurationSec := intValue(result.Data["duration_sec"])
	if actualDurationSec <= 0 {
		actualDurationSec = durationSec
	}
	okValue := boolValue(result.Data, "ok")
	if !okValue {
		okValue = result.OK
	}
	return gatewayJSONResult(call, map[string]any{
		"ok":           okValue,
		"path":         pathValue,
		"size_bytes":   sizeBytes,
		"duration_sec": actualDurationSec,
		"fps":          fps,
		"quality":      quality,
	})
}

func (b *nodeCapabilityBridge) executeClipboardRead(ctx context.Context, sessionID string, call agent.ToolCall) (contextengine.ToolResult, error) {
	result, err := b.sessionCap.Invoke(ctx, captypes.InvokeRequest{
		Operation: "clipboard_read",
		SessionID: sessionID,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if result == nil || !result.OK {
		if result != nil && strings.TrimSpace(result.Error) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s: %s", call.Name, result.Error)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s failed", call.Name)
	}
	return gatewayJSONResult(call, map[string]any{
		"content": strings.TrimSpace(mapStringValue(result.Data, "text")),
		"format":  "text",
	})
}

func (b *nodeCapabilityBridge) executeClipboardWrite(ctx context.Context, sessionID string, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	result, err := b.sessionCap.Invoke(ctx, captypes.InvokeRequest{
		Operation: "clipboard_write",
		SessionID: sessionID,
		Params: map[string]any{
			"text": content,
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if result == nil || !result.OK {
		if result != nil && strings.TrimSpace(result.Error) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s: %s", call.Name, result.Error)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s failed", call.Name)
	}
	okValue := boolValue(result.Data, "written")
	if !okValue {
		okValue = result.OK
	}
	return gatewayJSONResult(call, map[string]any{
		"ok": okValue,
	})
}

func int64Value(data map[string]any, key string) int64 {
	if value, err := intFrom(data[key], 0); err == nil {
		return int64(value)
	}
	return 0
}
