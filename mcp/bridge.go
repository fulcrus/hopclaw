package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

// defaultSideEffectClass is the conservative default assigned to MCP tools
// since we cannot know whether they perform writes.
const defaultSideEffectClass = "external_write"

// Bridge converts MCP tools into HopClaw ToolManifests and routes execution
// calls through the Manager.
type Bridge struct {
	manager *Manager
}

// NewBridge creates a Bridge backed by the given Manager.
func NewBridge(manager *Manager) *Bridge {
	return &Bridge{manager: manager}
}

// ToolManifests converts all tools from connected MCP servers into HopClaw
// ToolManifest entries suitable for the skill system.
func (b *Bridge) ToolManifests() []skill.ToolManifest {
	tools := b.manager.Tools()
	manifests := make([]skill.ToolManifest, 0, len(tools))
	for _, tool := range tools {
		manifests = append(manifests, skill.ToolManifest{
			Name:            tool.Name,
			Description:     tool.Description,
			InputSchema:     tool.InputSchema,
			SideEffectClass: defaultSideEffectClass,
		})
	}
	return manifests
}

// Execute calls the named MCP tool through the Manager and returns the
// concatenated text content blocks as a string.
func (b *Bridge) Execute(ctx context.Context, name string, input map[string]any) (string, error) {
	result, err := b.manager.CallTool(ctx, name, input)
	if err != nil {
		return "", fmt.Errorf("mcp execute %q: %w", name, err)
	}
	return formatCallToolResult(result), nil
}

// formatCallToolResult concatenates all text content blocks from a tool result
// into a single string. Non-text blocks are represented by a placeholder.
func formatCallToolResult(result *CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, "[image: "+block.MIMEType+"]")
		case "resource":
			parts = append(parts, "[resource: "+block.URI+"]")
		default:
			parts = append(parts, "["+block.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}
