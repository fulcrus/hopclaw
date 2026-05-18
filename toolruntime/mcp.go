package toolruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/mcp"
	"github.com/fulcrus/hopclaw/resultmodel"
)

const mcpToolDefaultSideEffect = "external_write"

type mcpRuntime interface {
	Tools() []mcp.Tool
	CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error)
}

// MCPExecutor exposes connected MCP tools through the standard runtime catalog.
type MCPExecutor struct {
	runtime mcpRuntime
}

func NewMCPExecutor(runtime mcpRuntime) *MCPExecutor {
	if runtime == nil {
		return nil
	}
	return &MCPExecutor{runtime: runtime}
}

func (e *MCPExecutor) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	if e == nil || e.runtime == nil {
		return nil
	}
	tools := append([]mcp.Tool(nil), e.runtime.Tools()...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	out := make([]agent.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		out = append(out, copyToolDefinition(agent.ToolDefinition{
			Name:             strings.TrimSpace(tool.Name),
			Description:      strings.TrimSpace(tool.Description),
			InputSchema:      cloneSchema(tool.InputSchema),
			SideEffectClass:  mcpToolDefaultSideEffect,
			RequiresApproval: true,
			Source:           "mcp",
			SourceRef:        strings.TrimSpace(tool.Name),
			Trust:            "plugin",
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		}))
	}
	return out
}

func (e *MCPExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if e == nil || e.runtime == nil {
		return nil, false
	}
	for _, tool := range e.runtime.Tools() {
		if strings.TrimSpace(tool.Name) != strings.TrimSpace(name) {
			continue
		}
		definition := copyToolDefinition(agent.ToolDefinition{
			Name:             strings.TrimSpace(tool.Name),
			Description:      strings.TrimSpace(tool.Description),
			InputSchema:      cloneSchema(tool.InputSchema),
			SideEffectClass:  mcpToolDefaultSideEffect,
			RequiresApproval: true,
			Source:           "mcp",
			SourceRef:        strings.TrimSpace(tool.Name),
			Trust:            "plugin",
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		})
		return resolvedToolFromBinding(nil, definition, "mcp:"+definition.Name), true
	}
	return nil, false
}

func (e *MCPExecutor) ExecuteBatch(ctx context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if e == nil || e.runtime == nil {
		return nil, agent.ErrToolExecutorNil
	}
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		result, err := e.runtime.CallTool(ctx, call.Name, supportmaps.Clone(call.Input))
		if err != nil {
			results = append(results, batchErrorResult(call, err))
			continue
		}
		results = append(results, renderMCPToolResult(call, result))
	}
	return results, nil
}

func renderMCPToolResult(call agent.ToolCall, result *mcp.CallToolResult) contextengine.ToolResult {
	if result == nil {
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			Status:         resultmodel.ToolResultError,
			TranscriptText: "error: empty mcp tool result",
			Error: &resultmodel.ResultError{
				Message: "empty mcp tool result",
			},
		}.Normalized()
	}

	out := contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Structured: map[string]any{
			"content": result.Content,
		},
	}

	var transcript []string
	for _, block := range result.Content {
		switch strings.TrimSpace(block.Type) {
		case "text":
			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			out.Blocks = append(out.Blocks, resultmodel.ResultBlock{
				Kind:    resultmodel.ResultBlockText,
				Content: text,
			})
			transcript = append(transcript, text)
		case "resource":
			uri := strings.TrimSpace(block.URI)
			if uri == "" {
				continue
			}
			out.Artifacts = append(out.Artifacts, resultmodel.ResultArtifact{
				Kind: "resource",
				Name: call.Name,
				URI:  uri,
			})
			out.Actions = append(out.Actions, resultmodel.ResultAction{
				Kind:   resultmodel.ResultActionOpenURL,
				Label:  "Open resource",
				Target: uri,
			})
			transcript = append(transcript, "[resource] "+uri)
		case "image":
			uri := ""
			if strings.TrimSpace(block.Data) != "" && strings.TrimSpace(block.MIMEType) != "" {
				uri = "data:" + strings.TrimSpace(block.MIMEType) + ";base64," + strings.TrimSpace(block.Data)
			}
			preview := ""
			if block.Data != "" {
				if data, err := base64.StdEncoding.DecodeString(block.Data); err == nil {
					preview = fmt.Sprintf("%d bytes", len(data))
				}
			}
			out.Artifacts = append(out.Artifacts, resultmodel.ResultArtifact{
				Kind:        "image",
				Name:        call.Name,
				URI:         uri,
				ContentType: strings.TrimSpace(block.MIMEType),
				PreviewText: preview,
			})
			transcript = append(transcript, "[image] "+strings.TrimSpace(block.MIMEType))
		default:
			if body, err := json.Marshal(block); err == nil {
				text := string(body)
				out.Blocks = append(out.Blocks, resultmodel.ResultBlock{
					Kind:    resultmodel.ResultBlockJSON,
					Content: text,
				})
				transcript = append(transcript, text)
			}
		}
	}

	out.TranscriptText = strings.Join(transcript, "\n")
	if result.IsError {
		out.Status = resultmodel.ToolResultError
		message := strings.TrimSpace(out.TranscriptText)
		if message == "" {
			message = "mcp tool returned an error"
		}
		out.Error = &resultmodel.ResultError{Message: message}
	}
	return out.Normalized()
}
