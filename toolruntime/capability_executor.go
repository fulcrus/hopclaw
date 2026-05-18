package toolruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	registry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

type CapabilityExecutor struct {
	artifacts artifact.Store
	entries   map[string]*capabilityToolEntry
	order     []string
}

type capabilityToolEntry struct {
	name            string
	capability      string
	operation       string
	sessionOptional bool
	sessionCap      registry.SessionCapability
	definition      agent.ToolDefinition
	bound           skill.BoundTool
}

type capabilityToolProjection struct {
	name    string
	aliases []string
}

func NewCapabilityExecutor(reg *registry.Registry, artifacts artifact.Store) *CapabilityExecutor {
	if reg == nil {
		return nil
	}
	exec := &CapabilityExecutor{
		artifacts: artifacts,
		entries:   make(map[string]*capabilityToolEntry),
	}
	exec.registerSessionCapabilities(reg)
	if len(exec.entries) == 0 {
		return nil
	}
	sort.Strings(exec.order)
	return exec
}

func (e *CapabilityExecutor) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if e == nil || len(e.order) == 0 {
		return nil
	}
	availability := e.availabilitySnapshot(context.Background())
	out := make([]agent.ToolDefinition, 0, len(e.order))
	for _, name := range e.order {
		entry := e.entries[name]
		if !entryAvailable(availability, entry) {
			definition := copyToolDefinition(entry.definition)
			definition.Eligible = false
			definition.EligibilityReasons = availabilityReasons(availability[entry.capability])
			definition.Availability = availabilityForHealth(availability[entry.capability], definition.EligibilityReasons)
			out = append(out, definition)
			continue
		}
		out = append(out, copyToolDefinition(entry.definition))
	}
	return out
}

func (e *CapabilityExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if e == nil {
		return nil, false
	}
	entry, ok := e.entries[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}
	bound := entry.bound
	definition := copyToolDefinition(entry.definition)
	if !entryAvailable(e.availabilitySnapshot(context.Background()), entry) {
		definition.Eligible = false
		definition.EligibilityReasons = availabilityReasons(e.availabilitySnapshot(context.Background())[entry.capability])
		definition.Availability = availabilityForHealth(e.availabilitySnapshot(context.Background())[entry.capability], definition.EligibilityReasons)
		bound.Eligibility = skill.EligibilityResult{
			Eligible: false,
			Reasons:  append([]string(nil), definition.EligibilityReasons...),
		}
	}
	return resolvedToolFromBinding(&bound, definition, "capability:"+entry.capability), true
}

func (e *CapabilityExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if e == nil {
		return nil, agent.ErrToolExecutorNil
	}
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		entry, ok := e.entries[strings.TrimSpace(call.Name)]
		if !ok {
			results = append(results, batchErrorResult(call, fmt.Errorf("tool %q is not registered", call.Name)))
			continue
		}
		result, err := e.executeCall(ctx, run, session, entry, call)
		if err != nil {
			results = append(results, batchErrorResult(call, err))
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

func (e *CapabilityExecutor) executeCall(ctx context.Context, run *agent.Run, session *agent.Session, entry *capabilityToolEntry, call agent.ToolCall) (contextengine.ToolResult, error) {
	if err := e.ensureEntryAvailable(ctx, entry, call.Name); err != nil {
		return contextengine.ToolResult{}, err
	}
	params := supportmaps.Clone(call.Input)
	switch entry.operation {
	case "create_session":
		handle, err := entry.sessionCap.OpenSession(ctx, params)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		body, err := json.MarshalIndent(map[string]any{
			"session_id": handle.ID,
			"capability": handle.Capability,
			"created_at": handle.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"metadata":   supportmaps.Clone(handle.Metadata),
		}, "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    string(body),
		}, nil
	case "close_session":
		sessionID := strings.TrimSpace(mapStringValue(params, "session_id"))
		if sessionID == "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s requires input.session_id", call.Name)
		}
		if err := entry.sessionCap.CloseSession(ctx, sessionID); err != nil {
			return contextengine.ToolResult{}, err
		}
		body, err := json.MarshalIndent(map[string]any{
			"session_id": sessionID,
			"closed":     true,
		}, "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    string(body),
		}, nil
	default:
		sessionID := strings.TrimSpace(mapStringValue(params, "session_id"))
		if sessionID == "" && !entry.sessionOptional {
			return contextengine.ToolResult{}, fmt.Errorf("%s requires input.session_id", call.Name)
		}
		if sessionID != "" {
			delete(params, "session_id")
		}
		result, err := entry.sessionCap.Invoke(ctx, captypes.InvokeRequest{
			Operation: entry.operation,
			SessionID: sessionID,
			Params:    params,
		})
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return e.renderInvokeResult(ctx, run, session, call, entry, sessionID, result)
	}
}

func (e *CapabilityExecutor) renderInvokeResult(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall, entry *capabilityToolEntry, capabilitySessionID string, result *captypes.InvokeResult) (contextengine.ToolResult, error) {
	if result == nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s returned no result", call.Name)
	}
	if !result.OK {
		if strings.TrimSpace(result.Error) != "" {
			return contextengine.ToolResult{}, fmt.Errorf("%s: %s", call.Name, result.Error)
		}
		return contextengine.ToolResult{}, fmt.Errorf("%s failed", call.Name)
	}
	if direct, ok := e.directArtifactResult(call, result); ok {
		return direct, nil
	}

	switch {
	case entry.operation == "screenshot" && entry.capability == "browser":
		return e.renderScreenshot(ctx, run, session, call, capabilitySessionID, result)
	case entry.operation == "screenshot" && entry.capability == "desktop":
		return e.renderDesktopScreenshot(ctx, run, session, call, capabilitySessionID, result)
	case entry.operation == "capture_tree" && entry.capability == "desktop":
		return e.renderDesktopCaptureTree(ctx, run, session, call, capabilitySessionID, result)
	case entry.operation == "snapshot" && entry.capability == "browser":
		return e.renderSnapshot(ctx, run, session, call, capabilitySessionID, result)
	default:
		body, err := json.MarshalIndent(supportmaps.Clone(result.Data), "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			Status:         resultStatusFromCapability(result),
			TranscriptText: defaultCapabilityTranscript(result, string(body)),
			Summary:        strings.TrimSpace(result.Summary),
			Content:        string(body),
			Structured:     supportmaps.Clone(result.Data),
			Blocks:         capabilityBlocks(result.Blocks),
			Artifacts:      capabilityArtifacts(result.Artifacts),
			Actions:        capabilityActions(result.Actions),
			Metadata:       supportmaps.Clone(result.Metadata),
		}.Normalized(), nil
	}
}

func (e *CapabilityExecutor) renderScreenshot(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall, browserSessionID string, result *captypes.InvokeResult) (contextengine.ToolResult, error) {
	data := supportmaps.Clone(result.Data)
	base64Body := strings.TrimSpace(mapStringValue(data, "content_base64"))
	if base64Body == "" {
		return contextengine.ToolResult{}, fmt.Errorf("%s did not return screenshot data", call.Name)
	}
	if e.artifacts == nil {
		return contextengine.ToolResult{}, agent.ErrArtifactStoreNil
	}
	body, err := base64.StdEncoding.DecodeString(base64Body)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("decode screenshot payload: %w", err)
	}
	mimeType := strings.TrimSpace(mapStringValue(data, "mime_type"))
	if mimeType == "" {
		mimeType = "image/png"
	}
	blob, err := e.artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "browser.screenshot",
		ContentType: mimeType,
		Body:        body,
		Metadata: map[string]any{
			meta.KeyRunID:        safeRunID(run),
			meta.KeySessionID:    safeSessionID(session),
			"browser_session_id": browserSessionID,
			meta.KeyToolName:     call.Name,
			meta.KeyToolCallID:   call.ID,
			"full_page":          boolValue(data, "full_page"),
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	content, err := json.MarshalIndent(map[string]any{
		"mime_type": mimeType,
		"full_page": boolValue(data, "full_page"),
		"size":      blob.Size,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := supportmaps.Clone(data)
	delete(structured, "content_base64")
	structured["mime_type"] = mimeType
	structured["size"] = blob.Size
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: "screenshot captured",
		Summary:        "screenshot captured",
		Content:        string(content),
		Structured:     structured,
		Artifacts: []resultmodel.ResultArtifact{{
			Kind:        "browser.screenshot",
			Name:        "browser-screenshot",
			URI:         blob.URI,
			ContentType: mimeType,
			SizeBytes:   blob.Size,
		}},
		Metadata:    supportmaps.Clone(result.Metadata),
		ArtifactURI: blob.URI,
	}.Normalized(), nil
}

func (e *CapabilityExecutor) renderDesktopScreenshot(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall, desktopSessionID string, result *captypes.InvokeResult) (contextengine.ToolResult, error) {
	data := supportmaps.Clone(result.Data)
	base64Body := strings.TrimSpace(mapStringValue(data, "content_base64"))
	if base64Body == "" {
		return contextengine.ToolResult{}, fmt.Errorf("%s did not return screenshot data", call.Name)
	}
	if e.artifacts == nil {
		return contextengine.ToolResult{}, agent.ErrArtifactStoreNil
	}
	body, err := base64.StdEncoding.DecodeString(base64Body)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("decode screenshot payload: %w", err)
	}
	mimeType := strings.TrimSpace(mapStringValue(data, "mime_type"))
	if mimeType == "" {
		mimeType = "image/png"
	}
	blob, err := e.artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "desktop.screenshot",
		ContentType: mimeType,
		Body:        body,
		Metadata: map[string]any{
			meta.KeyRunID:        safeRunID(run),
			meta.KeySessionID:    safeSessionID(session),
			"desktop_session_id": desktopSessionID,
			meta.KeyToolName:     call.Name,
			meta.KeyToolCallID:   call.ID,
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	content, err := json.MarshalIndent(map[string]any{
		"mime_type":    mimeType,
		"size":         blob.Size,
		"scope":        mapStringValue(data, "scope"),
		"app":          mapStringValue(data, "app"),
		"bundle_id":    mapStringValue(data, "bundle_id"),
		"title":        mapStringValue(data, "title"),
		"window_index": mapIntValue(data, "window_index"),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := supportmaps.Clone(data)
	delete(structured, "content_base64")
	structured["mime_type"] = mimeType
	structured["size"] = blob.Size
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: "desktop screenshot captured",
		Summary:        "desktop screenshot captured",
		Content:        string(content),
		Structured:     structured,
		Artifacts: []resultmodel.ResultArtifact{{
			Kind:        "desktop.screenshot",
			Name:        "desktop-screenshot",
			URI:         blob.URI,
			ContentType: mimeType,
			SizeBytes:   blob.Size,
		}},
		Metadata:    supportmaps.Clone(result.Metadata),
		ArtifactURI: blob.URI,
	}.Normalized(), nil
}

func (e *CapabilityExecutor) renderDesktopCaptureTree(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall, desktopSessionID string, result *captypes.InvokeResult) (contextengine.ToolResult, error) {
	data := supportmaps.Clone(result.Data)
	body, err := json.MarshalIndent(supportmaps.Clone(data), "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if e.artifacts == nil {
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: string(body),
			Content:        string(body),
			Structured:     supportmaps.Clone(data),
			Metadata:       supportmaps.Clone(result.Metadata),
		}, nil
	}
	blob, err := e.artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "desktop.capture_tree",
		ContentType: "application/json",
		Body:        body,
		Metadata: map[string]any{
			meta.KeyRunID:        safeRunID(run),
			meta.KeySessionID:    safeSessionID(session),
			"desktop_session_id": desktopSessionID,
			meta.KeyToolName:     call.Name,
			meta.KeyToolCallID:   call.ID,
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	content, err := json.MarshalIndent(map[string]any{
		"frontmost_app": nestedMapValue(data, "frontmost_app", "name"),
		"app_count":     len(anySliceValue(data, "apps")),
		"size":          blob.Size,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: "desktop accessibility tree captured",
		Summary:        "desktop accessibility tree captured",
		Content:        string(content),
		Structured:     supportmaps.Clone(data),
		Artifacts: []resultmodel.ResultArtifact{{
			Kind:        "desktop.capture_tree",
			Name:        "desktop-capture-tree.json",
			URI:         blob.URI,
			ContentType: "application/json",
			SizeBytes:   blob.Size,
		}},
		Metadata:    supportmaps.Clone(result.Metadata),
		ArtifactURI: blob.URI,
	}.Normalized(), nil
}

func (e *CapabilityExecutor) renderSnapshot(ctx context.Context, run *agent.Run, session *agent.Session, call agent.ToolCall, browserSessionID string, result *captypes.InvokeResult) (contextengine.ToolResult, error) {
	data := supportmaps.Clone(result.Data)
	html := mapStringValue(data, "html")
	if html == "" {
		body, err := json.MarshalIndent(supportmaps.Clone(data), "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: string(body),
			Content:        string(body),
			Structured:     supportmaps.Clone(data),
			Metadata:       supportmaps.Clone(result.Metadata),
		}, nil
	}
	if e.artifacts == nil {
		body, err := json.MarshalIndent(supportmaps.Clone(data), "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: string(body),
			Content:        string(body),
			Structured:     supportmaps.Clone(data),
			Metadata:       supportmaps.Clone(result.Metadata),
		}, nil
	}
	contentType := strings.TrimSpace(mapStringValue(data, "content_type"))
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	blob, err := e.artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "browser.snapshot",
		ContentType: contentType,
		Body:        []byte(html),
		Metadata: map[string]any{
			meta.KeyRunID:        safeRunID(run),
			meta.KeySessionID:    safeSessionID(session),
			"browser_session_id": browserSessionID,
			meta.KeyToolName:     call.Name,
			meta.KeyToolCallID:   call.ID,
			"url":                mapStringValue(data, "url"),
			"title":              mapStringValue(data, "title"),
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := json.MarshalIndent(map[string]any{
		"url":   mapStringValue(data, "url"),
		"title": mapStringValue(data, "title"),
		"size":  blob.Size,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	structured := supportmaps.Clone(data)
	delete(structured, "html")
	structured["size"] = blob.Size
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultmodel.ToolResultOK,
		TranscriptText: "browser snapshot captured",
		Summary:        "browser snapshot captured",
		Content:        string(body),
		Structured:     structured,
		Artifacts: []resultmodel.ResultArtifact{{
			Kind:        "browser.snapshot",
			Name:        "browser-snapshot.html",
			URI:         blob.URI,
			ContentType: contentType,
			SizeBytes:   blob.Size,
		}},
		Metadata:    supportmaps.Clone(result.Metadata),
		ArtifactURI: blob.URI,
	}.Normalized(), nil
}

func (e *CapabilityExecutor) registerSessionCapabilities(reg *registry.Registry) {
	for _, name := range reg.Names() {
		capability, ok := reg.Get(name)
		if !ok {
			continue
		}
		manifest := capability.Manifest()
		if manifest.Kind != captypes.KindSession {
			continue
		}
		sessionCap, ok := capability.(registry.SessionCapability)
		if !ok {
			continue
		}

		pkg := &skill.SkillPackage{
			ID:     "capability-" + manifest.Name,
			Kind:   skill.SkillKindExecutable,
			Status: skill.StatusReady,
			Prompt: skill.PromptSkill{
				Name:        manifest.Name,
				Description: "Capability host backed automation tools",
				Location:    "capability:" + manifest.Name,
			},
			Source: skill.SkillSource{
				Kind: skill.SourceBundled,
				Dir:  manifest.Name,
				Root: manifest.Name,
			},
			Trust: skill.TrustInternal,
		}

		for _, op := range manifest.Operations {
			projection := projectCapabilityTool(manifest.Name, op)
			definition := agent.ToolDefinition{
				Name:             projection.name,
				Description:      op.Description,
				InputSchema:      cloneSchema(op.InputSchema),
				OutputSchema:     cloneSchema(op.OutputSchema),
				SideEffectClass:  op.SideEffectClass,
				Idempotent:       op.Idempotent,
				RequiresApproval: manifest.ApprovalPolicy == "always",
				ExecutionKey:     capabilityExecutionKey(manifest.Name, op),
				Source:           "capability",
				SourceRef:        manifest.Name + ":" + op.Name,
				Trust:            string(skill.TrustInternal),
				Eligible:         true,
				Availability: agent.ToolAvailability{
					Status: agent.AvailabilityReady,
				},
			}
			entry := &capabilityToolEntry{
				name:            projection.name,
				capability:      manifest.Name,
				operation:       op.Name,
				sessionOptional: op.SessionOptional,
				sessionCap:      sessionCap,
				definition:      definition,
				bound: skill.BoundTool{
					Package: pkg,
					Manifest: skill.ToolManifest{
						Name:             projection.name,
						Aliases:          append([]string(nil), projection.aliases...),
						Description:      op.Description,
						InputSchema:      cloneSchema(op.InputSchema),
						OutputSchema:     cloneSchema(op.OutputSchema),
						SideEffectClass:  op.SideEffectClass,
						Idempotent:       op.Idempotent,
						RequiresApproval: manifest.ApprovalPolicy == "always",
						ExecutionKey:     definition.ExecutionKey,
					},
					Eligibility: skill.EligibilityResult{Eligible: true},
				},
			}
			e.entries[projection.name] = entry
			for _, alias := range projection.aliases {
				trimmed := strings.TrimSpace(alias)
				if trimmed == "" {
					continue
				}
				e.entries[trimmed] = entry
			}
			e.order = append(e.order, projection.name)
		}
	}
}

func projectCapabilityTool(capabilityName string, op captypes.OperationSpec) capabilityToolProjection {
	name := strings.TrimSpace(capabilityName) + "." + strings.TrimSpace(op.Name)
	projection := capabilityToolProjection{name: name}

	switch strings.TrimSpace(capabilityName) {
	case "browser":
		switch strings.TrimSpace(op.Name) {
		case "create_session":
			projection.name = "browser.open"
			projection.aliases = []string{"browser.create_session"}
		case "close_session":
			projection.name = "browser.close"
			projection.aliases = []string{"browser.close_session"}
		case "wait_for":
			projection.name = "browser.wait"
			projection.aliases = []string{"browser.wait_for"}
		case "list_tabs":
			projection.name = "browser.tabs"
			projection.aliases = []string{"browser.list_tabs"}
		}
	}

	return projection
}

func capabilityExecutionKey(capabilityName string, op captypes.OperationSpec) string {
	switch {
	case op.Name == "create_session":
		return capabilityName + ":{session.id}"
	case op.SessionOptional:
		return capabilityName + ":global"
	default:
		return capabilityName + ":{input.session_id}"
	}
}

func (e *CapabilityExecutor) availabilitySnapshot(ctx context.Context) map[string]captypes.Health {
	if e == nil || len(e.entries) == 0 {
		return nil
	}
	out := make(map[string]captypes.Health)
	for _, name := range e.order {
		entry := e.entries[name]
		if entry == nil {
			continue
		}
		if _, ok := out[entry.capability]; ok {
			continue
		}
		if entry.sessionCap == nil {
			out[entry.capability] = captypes.Health{
				Status:  captypes.StatusUnavailable,
				Message: "capability is not configured",
			}
			continue
		}
		out[entry.capability] = entry.sessionCap.Health(ctx)
	}
	return out
}

func (e *CapabilityExecutor) ensureEntryAvailable(ctx context.Context, entry *capabilityToolEntry, toolName string) error {
	availability := e.availabilitySnapshot(ctx)
	if entryAvailable(availability, entry) {
		return nil
	}
	health, ok := availability[entry.capability]
	if !ok {
		return fmt.Errorf("%s is unavailable", toolName)
	}
	if strings.TrimSpace(health.Message) == "" {
		return fmt.Errorf("%s is unavailable", toolName)
	}
	return fmt.Errorf("%s is unavailable: %s", toolName, health.Message)
}

func entryAvailable(availability map[string]captypes.Health, entry *capabilityToolEntry) bool {
	if entry == nil {
		return false
	}
	health, ok := availability[entry.capability]
	if !ok {
		return false
	}
	return health.Status == captypes.StatusReady
}

func availabilityReasons(health captypes.Health) []string {
	message := strings.TrimSpace(health.Message)
	if message == "" {
		return nil
	}
	return []string{message}
}

func availabilityForHealth(health captypes.Health, reasons []string) agent.ToolAvailability {
	status := agent.AvailabilityReady
	switch health.Status {
	case captypes.StatusDegraded:
		status = agent.AvailabilityDegraded
	case captypes.StatusUnavailable:
		status = agent.AvailabilityBlocked
	}
	return agent.ToolAvailability{
		Status:  status,
		Reasons: append([]string(nil), reasons...),
	}
}

func resultStatusFromCapability(result *captypes.InvokeResult) resultmodel.ToolResultStatus {
	if result == nil {
		return resultmodel.ToolResultError
	}
	switch strings.TrimSpace(strings.ToLower(result.Status)) {
	case "error", "failed":
		return resultmodel.ToolResultError
	case "partial", "degraded":
		return resultmodel.ToolResultPartial
	default:
		if strings.TrimSpace(result.Error) != "" {
			return resultmodel.ToolResultError
		}
		return resultmodel.ToolResultOK
	}
}

func defaultCapabilityTranscript(result *captypes.InvokeResult, fallback string) string {
	if result == nil {
		return strings.TrimSpace(fallback)
	}
	for _, value := range []string{result.TranscriptText, result.Summary, fallback} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func capabilityBlocks(blocks []captypes.ResultBlock) []resultmodel.ResultBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultBlock, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, resultmodel.ResultBlock{
			Kind:    resultmodel.ResultBlockKind(strings.TrimSpace(block.Kind)),
			Title:   strings.TrimSpace(block.Title),
			Content: strings.TrimSpace(block.Content),
			Data:    supportmaps.Clone(block.Data),
		})
	}
	return out
}

func capabilityArtifacts(items []captypes.ArtifactRef) []resultmodel.ResultArtifact {
	if len(items) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultArtifact, 0, len(items))
	for _, item := range items {
		if uri := strings.TrimSpace(item.URI); uri != "" {
			out = append(out, resultmodel.ResultArtifact{
				Kind:        strings.TrimSpace(item.Kind),
				Name:        strings.TrimSpace(item.Name),
				URI:         uri,
				ContentType: strings.TrimSpace(item.ContentType),
				SizeBytes:   item.SizeBytes,
				PreviewText: strings.TrimSpace(item.PreviewText),
				Metadata:    supportmaps.Clone(item.Metadata),
			})
		}
	}
	return out
}

func capabilityActions(items []captypes.ResultAction) []resultmodel.ResultAction {
	if len(items) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultAction, 0, len(items))
	for _, item := range items {
		out = append(out, resultmodel.ResultAction{
			Kind:   resultmodel.ResultActionKind(strings.TrimSpace(item.Kind)),
			Label:  strings.TrimSpace(item.Label),
			Target: strings.TrimSpace(item.Target),
			Params: supportmaps.Clone(item.Params),
		})
	}
	return out
}

func (e *CapabilityExecutor) directArtifactResult(call agent.ToolCall, result *captypes.InvokeResult) (contextengine.ToolResult, bool) {
	if result == nil {
		return contextengine.ToolResult{}, false
	}
	artifacts := capabilityArtifacts(result.Artifacts)
	if len(artifacts) == 0 && strings.TrimSpace(result.ArtifactRef) != "" {
		artifacts = append(artifacts, resultmodel.ResultArtifact{
			Kind: "artifact",
			URI:  strings.TrimSpace(result.ArtifactRef),
		})
	}
	if len(artifacts) == 0 {
		return contextengine.ToolResult{}, false
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		Status:         resultStatusFromCapability(result),
		TranscriptText: defaultCapabilityTranscript(result, "artifact ready"),
		Summary:        strings.TrimSpace(result.Summary),
		Structured:     supportmaps.Clone(result.Data),
		Blocks:         capabilityBlocks(result.Blocks),
		Artifacts:      artifacts,
		Actions:        capabilityActions(result.Actions),
		Metadata:       supportmaps.Clone(result.Metadata),
		ArtifactURI:    artifacts[0].URI,
	}.Normalized(), true
}

func mapStringValue(in map[string]any, key string) string {
	if len(in) == 0 {
		return ""
	}
	value, ok := in[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func mapIntValue(in map[string]any, key string) int {
	if len(in) == 0 {
		return 0
	}
	value, ok := in[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func anySliceValue(in map[string]any, key string) []any {
	if len(in) == 0 {
		return nil
	}
	values, _ := in[key].([]any)
	return values
}

func nestedMapValue(in map[string]any, key string, nested string) string {
	if len(in) == 0 {
		return ""
	}
	child, _ := in[key].(map[string]any)
	if len(child) == 0 {
		return ""
	}
	value, _ := child[nested].(string)
	return strings.TrimSpace(value)
}

func boolValue(in map[string]any, key string) bool {
	if len(in) == 0 {
		return false
	}
	value, ok := in[key]
	if !ok || value == nil {
		return false
	}
	boolean, _ := value.(bool)
	return boolean
}
