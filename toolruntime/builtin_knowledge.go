package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/skill"
)

const defaultKnowledgeSearchLimit = 8

func knowledgeToolDefs(_ BuiltinsConfig) []builtinToolDef {
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "knowledge.sources",
				Description:     "List configured external knowledge sources and their readiness.",
				InputSchema:     knowledgeSourcesInputSchema(),
				OutputSchema:    knowledgeSourcesOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "knowledge:sources",
			},
			Handler: handleKnowledgeSources,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "knowledge.search",
				Description:     "Search indexed knowledge maintained outside HopClaw, such as local docs, repositories, and URLs.",
				InputSchema:     knowledgeSearchInputSchema(),
				OutputSchema:    knowledgeSearchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "knowledge:search:{query}",
			},
			Handler: handleKnowledgeSearch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "knowledge.sync",
				Description:      "Refresh one configured knowledge source into the search index.",
				InputSchema:      knowledgeSyncInputSchema(),
				OutputSchema:     knowledgeSyncOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "knowledge:sync:{source_id}",
			},
			Handler: handleKnowledgeSync,
		},
	}
}

func handleKnowledgeSources(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.knowledge == nil {
		return b.jsonResult(call, map[string]any{
			"sources": []any{},
			"count":   0,
			"message": "knowledge service not configured",
		})
	}
	enabledOnly, _ := boolFromDefault(call.Input["enabled_only"], false)
	items, err := b.knowledge.ListSources(ctx)
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"sources": []any{},
			"count":   0,
			"message": fmt.Sprintf("list failed: %v", err),
		})
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if enabledOnly && !item.Enabled {
			continue
		}
		out = append(out, knowledgeSourcePayload(item))
	}
	return b.jsonResult(call, map[string]any{
		"sources": out,
		"count":   len(out),
	})
}

func handleKnowledgeSearch(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.knowledge == nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"count":   0,
			"message": "knowledge service not configured",
		})
	}
	limit, _ := intFrom(call.Input["limit"], defaultKnowledgeSearchLimit)
	if limit <= 0 {
		limit = defaultKnowledgeSearchLimit
	}
	sourceID, _ := stringFrom(call.Input["source_id"])
	results, err := b.knowledge.Search(ctx, knowledge.SearchFilter{
		Query:    query,
		SourceID: strings.TrimSpace(sourceID),
		Limit:    limit,
	})
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"count":   0,
			"message": fmt.Sprintf("search failed: %v", err),
		})
	}
	items := make([]map[string]any, 0, len(results))
	for _, item := range results {
		items = append(items, map[string]any{
			"chunk_id":      item.ChunkID,
			"source_id":     item.SourceID,
			"source_name":   item.SourceName,
			"source_kind":   string(item.SourceKind),
			"document_id":   item.DocumentID,
			"title":         item.Title,
			"path":          item.Path,
			"uri":           item.URI,
			"preview":       item.Preview,
			"score":         item.Score,
			"keyword_score": item.KeywordScore,
			"updated_at":    item.UpdatedAt.Format(time.RFC3339),
		})
	}
	return b.jsonResult(call, map[string]any{
		"query":     query,
		"source_id": strings.TrimSpace(sourceID),
		"results":   items,
		"count":     len(items),
	})
}

func handleKnowledgeSync(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	sourceID, err := requiredString(call.Input, "source_id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.knowledge == nil {
		return b.jsonResult(call, map[string]any{
			"source_id": sourceID,
			"success":   false,
			"message":   "knowledge service not configured",
		})
	}
	result, err := b.knowledge.SyncSource(ctx, sourceID)
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"source_id": sourceID,
			"success":   false,
			"message":   fmt.Sprintf("sync failed: %v", err),
		})
	}
	return b.jsonResult(call, map[string]any{
		"source_id": sourceID,
		"success":   true,
		"source":    knowledgeSourcePayload(result.Source),
		"stats": map[string]any{
			"documents": result.Stats.Documents,
			"chunks":    result.Stats.Chunks,
			"bytes":     result.Stats.Bytes,
		},
		"message": "knowledge source synced",
	})
}

func knowledgeSourcePayload(item knowledge.Source) map[string]any {
	return map[string]any{
		"id":             item.ID,
		"name":           item.Name,
		"kind":           string(item.Kind),
		"enabled":        item.Enabled,
		"path":           item.Path,
		"urls":           append([]string(nil), item.URLs...),
		"include_globs":  append([]string(nil), item.IncludeGlobs...),
		"exclude_globs":  append([]string(nil), item.ExcludeGlobs...),
		"status":         string(item.Status),
		"last_sync_at":   item.LastSyncAt.Format(time.RFC3339),
		"last_error":     item.LastError,
		"connector_note": item.ConnectorNote,
		"created_at":     item.CreatedAt.Format(time.RFC3339),
		"updated_at":     item.UpdatedAt.Format(time.RFC3339),
		"stats": map[string]any{
			"documents": item.Stats.Documents,
			"chunks":    item.Stats.Chunks,
			"bytes":     item.Stats.Bytes,
		},
	}
}

func knowledgeSourcesInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"enabled_only": booleanSchema("Whether to return only enabled sources."),
	})
}

func knowledgeSourcesOutputSchema() map[string]any {
	entrySchema := objectSchema(map[string]any{
		"id":             stringSchema("Source identifier."),
		"name":           stringSchema("Source display name."),
		"kind":           stringSchema("Source connector kind."),
		"enabled":        booleanSchema("Whether the source is enabled."),
		"path":           stringSchema("Local path or checked-out repository path."),
		"urls":           arraySchema(stringSchema("Source URL."), "Configured URLs."),
		"include_globs":  arraySchema(stringSchema("Include glob."), "Optional include globs."),
		"exclude_globs":  arraySchema(stringSchema("Exclude glob."), "Optional exclude globs."),
		"status":         stringSchema("Current source status."),
		"last_sync_at":   stringSchema("Last successful sync timestamp."),
		"last_error":     stringSchema("Last sync error, if any."),
		"connector_note": stringSchema("Operator hint for the connector."),
		"created_at":     stringSchema("Creation timestamp."),
		"updated_at":     stringSchema("Last update timestamp."),
		"stats": objectSchema(map[string]any{
			"documents": integerSchema("Indexed document count."),
			"chunks":    integerSchema("Indexed chunk count."),
			"bytes":     integerSchema("Indexed byte count."),
		}),
	})
	return objectSchema(map[string]any{
		"sources": arraySchema(entrySchema, "Configured knowledge sources."),
		"count":   integerSchema("Number of sources returned."),
	})
}

func knowledgeSearchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"query":     stringSchema("Search query."),
		"source_id": stringSchema("Optional source identifier to narrow the search."),
		"limit":     integerSchema("Maximum result count."),
	}, "query")
}

func knowledgeSearchOutputSchema() map[string]any {
	entrySchema := objectSchema(map[string]any{
		"chunk_id":      stringSchema("Chunk identifier."),
		"source_id":     stringSchema("Owning source identifier."),
		"source_name":   stringSchema("Owning source name."),
		"source_kind":   stringSchema("Owning source kind."),
		"document_id":   stringSchema("Document identifier inside the source."),
		"title":         stringSchema("Chunk title."),
		"path":          stringSchema("Source-relative path."),
		"uri":           stringSchema("Original URI."),
		"preview":       stringSchema("Preview snippet."),
		"score":         numberSchema("Merged retrieval score."),
		"keyword_score": numberSchema("Keyword-only score contribution."),
		"updated_at":    stringSchema("Chunk update timestamp."),
	})
	return objectSchema(map[string]any{
		"query":     stringSchema("Executed search query."),
		"source_id": stringSchema("Applied source filter."),
		"results":   arraySchema(entrySchema, "Search results."),
		"count":     integerSchema("Number of results returned."),
	})
}

func knowledgeSyncInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"source_id": stringSchema("Knowledge source identifier."),
	}, "source_id")
}

func knowledgeSyncOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"source_id": stringSchema("Knowledge source identifier."),
		"success":   booleanSchema("Whether the sync succeeded."),
		"message":   stringSchema("Human-readable sync summary."),
		"source":    objectSchema(map[string]any{}),
		"stats": objectSchema(map[string]any{
			"documents": integerSchema("Indexed document count."),
			"chunks":    integerSchema("Indexed chunk count."),
			"bytes":     integerSchema("Indexed byte count."),
		}),
	})
}
