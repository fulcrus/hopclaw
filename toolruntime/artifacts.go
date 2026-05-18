package toolruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type ArtifactingConfig struct {
	InlineMaxBytes int
	PreviewChars   int
}

type ArtifactingExecutor struct {
	inner  agent.ToolExecutor
	store  artifact.Store
	config ArtifactingConfig
}

func NewArtifactingExecutor(inner agent.ToolExecutor, store artifact.Store, cfg ArtifactingConfig) *ArtifactingExecutor {
	if cfg.InlineMaxBytes <= 0 {
		cfg.InlineMaxBytes = 8 * 1024
	}
	if cfg.PreviewChars <= 0 {
		cfg.PreviewChars = 512
	}
	return &ArtifactingExecutor{
		inner:  inner,
		store:  store,
		config: cfg,
	}
}

func (e *ArtifactingExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if e.inner == nil {
		return nil, agent.ErrToolExecutorNil
	}
	results, err := e.inner.ExecuteBatch(ctx, run, session, calls)
	if err != nil || e.store == nil {
		return results, err
	}
	out := make([]contextengine.ToolResult, 0, len(results))
	for _, result := range results {
		processed, err := e.maybeStore(ctx, run, session, result)
		if err != nil {
			return nil, err
		}
		out = append(out, processed)
	}
	return out, nil
}

func (e *ArtifactingExecutor) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	provider, ok := e.inner.(agent.ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return provider.ToolDefinitions(session)
}

func (e *ArtifactingExecutor) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	resolver, ok := e.inner.(agent.ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}

func (e *ArtifactingExecutor) maybeStore(ctx context.Context, run *agent.Run, session *agent.Session, result contextengine.ToolResult) (contextengine.ToolResult, error) {
	normalized := result.Normalized()
	if normalized.PrimaryArtifactURI() != "" {
		return normalized, nil
	}
	content := strings.TrimSpace(normalized.TranscriptText)
	if len(content) <= e.config.InlineMaxBytes {
		return normalized, nil
	}
	blob, err := e.store.Put(ctx, artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "text/plain; charset=utf-8",
		Body:        []byte(content),
		Metadata: map[string]any{
			meta.KeyRunID:     safeRunID(run),
			meta.KeySessionID: safeSessionID(session),
			meta.KeyToolName:  result.ToolName,
			"tool_call_id":    result.ToolCallID,
		},
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	normalized.Artifacts = append(normalized.Artifacts, resultmodel.ResultArtifact{
		Kind:        "tool_output",
		Name:        defaultArtifactName(normalized.ToolName, normalized.ToolCallID),
		URI:         blob.URI,
		ContentType: "text/plain; charset=utf-8",
		SizeBytes:   blob.Size,
		PreviewText: compactArtifactPreview(content, e.config.PreviewChars),
		Metadata: map[string]any{
			meta.KeyRunID:      safeRunID(run),
			meta.KeySessionID:  safeSessionID(session),
			meta.KeyToolName:   normalized.ToolName,
			meta.KeyToolCallID: normalized.ToolCallID,
			"result_kind":      "tool_output",
			"display_name":     defaultArtifactName(normalized.ToolName, normalized.ToolCallID),
		},
	})
	normalized.ArtifactURI = blob.URI
	normalized.Blocks = append(normalized.Blocks, resultmodel.ResultBlock{
		Kind:    resultmodel.ResultBlockPreview,
		Title:   "Preview",
		Content: compactArtifactPreview(content, e.config.PreviewChars),
	})
	normalized.TranscriptText = summarizeArtifactContent(content, e.config.PreviewChars, blob.Size)
	normalized.Content = normalized.TranscriptText
	if normalized.Summary == "" {
		normalized.Summary = normalized.TranscriptText
	}
	return normalized.Normalized(), nil
}

func summarizeArtifactContent(content string, previewChars int, size int64) string {
	content = strings.TrimSpace(content)
	if previewChars > 0 && len(content) > previewChars {
		content = strings.TrimSpace(content[:previewChars]) + "\n[artifact_preview_truncated]"
	}
	if content == "" {
		return fmt.Sprintf("[artifact stored] %d bytes", size)
	}
	return fmt.Sprintf("[artifact stored] %d bytes\n%s", size, content)
}

func compactArtifactPreview(content string, previewChars int) string {
	content = strings.TrimSpace(content)
	if previewChars <= 0 || len(content) <= previewChars {
		return content
	}
	return strings.TrimSpace(content[:previewChars]) + "\n[artifact_preview_truncated]"
}

func defaultArtifactName(toolName, toolCallID string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool-output"
	}
	callID := strings.TrimSpace(toolCallID)
	if callID == "" {
		return name + ".txt"
	}
	return name + "-" + callID + ".txt"
}

func safeRunID(run *agent.Run) string {
	if run == nil {
		return ""
	}
	return run.ID
}

func safeSessionID(session *agent.Session) string {
	if session == nil {
		return ""
	}
	return session.ID
}
