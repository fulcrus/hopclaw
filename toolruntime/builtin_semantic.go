package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime/document"
)

const (
	semanticActionSendMessage      = "send_message"
	semanticActionSendCard         = "send_card"
	semanticActionGroupNotify      = "group_notify"
	semanticActionUploadAttachment = "upload_attachment"
	semanticActionCreateDocument   = "create_document"
	semanticActionCreateSchedule   = "create_schedule"
	semanticActionSearch           = "search"
	semanticActionSimilarity       = "similarity"

	semanticInspectThread       = "thread"
	semanticInspectParticipants = "participants"
	semanticInspectMentions     = "mentions"
	semanticInspectReplyContext = "reply_context"
	semanticInspectEmbedding    = "embedding"

	semanticTargetChannel    = "channel"
	semanticTargetDocument   = "document"
	semanticTargetCalendar   = "calendar_file"
	semanticTargetHTTPUpload = "http_upload"
	semanticTargetMemory     = "memory"

	semanticStatusOK          = "ok"
	semanticStatusUnsupported = "unsupported"

	defaultSemanticInspectLimit = 20
)

func semanticToolDefs(_ BuiltinsConfig) []builtinToolDef {
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "semantic.catalog",
				Description:     "List stable semantic automation actions and inspect operations with their target contracts.",
				InputSchema:     semanticCatalogInputSchema(),
				OutputSchema:    semanticCatalogOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "semantic:catalog",
			},
			Handler: handleSemanticCatalog,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "semantic.deliver",
				Description:      "Execute a stable semantic delivery action or embedding-oriented semantic operation.",
				InputSchema:      semanticDeliverInputSchema(),
				OutputSchema:     semanticDeliverOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "semantic:deliver:{action}",
			},
			Handler: handleSemanticDeliver,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "semantic.inspect_context",
				Description:     "Inspect thread, participants, mentions, reply context, or embedding state using a stable semantic contract.",
				InputSchema:     semanticInspectInputSchema(),
				OutputSchema:    semanticInspectOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "semantic:inspect:{kind}",
			},
			Handler: handleSemanticInspect,
		},
	}
}

func semanticCatalogInputSchema() map[string]any {
	return objectSchema(map[string]any{})
}

func semanticDeliverInputSchema() map[string]any {
	blockSchema := objectSchema(map[string]any{
		"kind":    stringSchema("Semantic block kind."),
		"title":   stringSchema("Optional block title."),
		"content": stringSchema("Block content."),
	})
	attachmentSchema := objectSchema(map[string]any{
		"kind":         stringSchema("Attachment kind."),
		"label":        stringSchema("Display label."),
		"uri":          stringSchema("Attachment URI."),
		"path":         stringSchema("Local file path to attach or upload."),
		"content_type": stringSchema("Attachment content type."),
	})
	targetSchema := objectSchema(map[string]any{
		"kind":       stringSchema("Target kind such as channel, document, calendar_file, or http_upload."),
		"channel":    stringSchema("Registered channel name."),
		"channel_id": stringSchema("Channel/thread identifier."),
		"target_id":  stringSchema("Message target identifier."),
		"url":        stringSchema("External upload URL."),
		"field":      stringSchema("Multipart field name for uploads."),
		"method":     stringSchema("Upload method, defaults to POST."),
	})
	documentParagraphSchema := objectSchema(map[string]any{
		"text":  stringSchema("Paragraph text."),
		"style": stringSchema("Paragraph style."),
	}, "text")
	documentSchema := objectSchema(map[string]any{
		"path":    stringSchema("Document output path."),
		"title":   stringSchema("Document title."),
		"author":  stringSchema("Document author."),
		"content": arraySchema(documentParagraphSchema, "Document paragraphs."),
	}, "path", "content")
	scheduleSchema := objectSchema(map[string]any{
		"path":        stringSchema("Calendar file output path."),
		"summary":     stringSchema("Event summary."),
		"start":       stringSchema("Event start in RFC3339."),
		"end":         stringSchema("Event end in RFC3339."),
		"description": stringSchema("Optional event description."),
		"location":    stringSchema("Optional location."),
		"status":      stringSchema("Optional ICS status."),
		"organizer":   stringSchema("Optional organizer."),
		"attendees":   arraySchema(stringSchema("Attendee identifier."), "Event attendees."),
	}, "path", "summary", "start")
	optionsSchema := objectSchema(map[string]any{
		"reply_to_id": stringSchema("Optional reply target."),
		"limit":       integerSchema("Optional inspect limit."),
		"before":      stringSchema("Optional pagination cursor."),
		"mention_all": booleanSchema("Whether the delivery should mention all participants."),
	})
	return objectSchema(map[string]any{
		"action":      stringSchema("Semantic action."),
		"query":       stringSchema("Semantic query for search-style actions."),
		"candidate":   stringSchema("Comparison text for similarity actions."),
		"mode":        stringSchema("Search mode for semantic actions: semantic, hybrid, keyword, or mmr."),
		"lambda":      numberSchema("MMR lambda when mode is mmr."),
		"target":      targetSchema,
		"content":     stringSchema("Primary text or markdown payload."),
		"format":      stringSchema("Semantic format override."),
		"blocks":      arraySchema(blockSchema, "Structured card blocks."),
		"attachments": arraySchema(attachmentSchema, "Semantic attachments."),
		"document":    documentSchema,
		"schedule":    scheduleSchema,
		"options":     optionsSchema,
	}, "action")
}

func semanticInspectInputSchema() map[string]any {
	targetSchema := objectSchema(map[string]any{
		"kind":       stringSchema("Target kind. Currently channel is supported."),
		"channel":    stringSchema("Registered channel name."),
		"channel_id": stringSchema("Channel/thread identifier."),
		"target_id":  stringSchema("Optional target identifier fallback."),
	})
	return objectSchema(map[string]any{
		"kind":    stringSchema("Inspect kind such as thread, participants, mentions, or reply_context."),
		"target":  targetSchema,
		"limit":   integerSchema("Maximum thread messages to read."),
		"before":  stringSchema("Optional thread pagination cursor."),
		"message": stringSchema("Optional message identifier for reply context."),
	}, "kind")
}

func semanticCatalogOutputSchema() map[string]any {
	actionSchema := objectSchema(map[string]any{
		"name":              stringSchema("Semantic action name."),
		"summary":           stringSchema("Operator-facing action summary."),
		"target_kinds":      arraySchema(stringSchema("Supported target kinds."), "Supported target kinds."),
		"requires_approval": booleanSchema("Whether the action requires approval."),
	}, "name", "summary", "target_kinds", "requires_approval")
	inspectSchema := objectSchema(map[string]any{
		"name":         stringSchema("Inspect operation name."),
		"summary":      stringSchema("Inspect operation summary."),
		"target_kinds": arraySchema(stringSchema("Supported target kinds."), "Supported target kinds."),
	}, "name", "summary", "target_kinds")
	return objectSchema(map[string]any{
		"actions":  arraySchema(actionSchema, "Stable semantic delivery actions."),
		"inspects": arraySchema(inspectSchema, "Stable semantic inspect operations."),
	}, "actions", "inspects")
}

func semanticDeliverOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"action":      stringSchema("Semantic action name."),
		"status":      stringSchema("Execution status."),
		"supported":   booleanSchema("Whether the runtime supports this semantic action."),
		"target_kind": stringSchema("Resolved target kind."),
		"message":     stringSchema("Operator-facing status message."),
		"result":      objectSchema(map[string]any{}),
		"hints":       arraySchema(stringSchema("Hints when an action is unsupported."), "Operator hints."),
	}, "action", "status", "supported", "target_kind", "message", "result", "hints")
}

func semanticInspectOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":        stringSchema("Inspect kind."),
		"status":      stringSchema("Execution status."),
		"supported":   booleanSchema("Whether the runtime supports this inspect operation."),
		"target_kind": stringSchema("Resolved target kind."),
		"message":     stringSchema("Operator-facing status message."),
		"result":      objectSchema(map[string]any{}),
		"hints":       arraySchema(stringSchema("Hints when an inspect operation is unsupported."), "Operator hints."),
	}, "kind", "status", "supported", "target_kind", "message", "result", "hints")
}

func handleSemanticCatalog(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return b.jsonResult(call, map[string]any{
		"actions": []any{
			map[string]any{"name": semanticActionSendMessage, "summary": "Send text or markdown to a channel target.", "target_kinds": []any{semanticTargetChannel}, "requires_approval": true},
			map[string]any{"name": semanticActionSendCard, "summary": "Send structured card blocks to a channel target.", "target_kinds": []any{semanticTargetChannel}, "requires_approval": true},
			map[string]any{"name": semanticActionGroupNotify, "summary": "Send a broadcast-style notification to a channel target.", "target_kinds": []any{semanticTargetChannel}, "requires_approval": true},
			map[string]any{"name": semanticActionUploadAttachment, "summary": "Upload an attachment to a channel or external multipart endpoint.", "target_kinds": []any{semanticTargetChannel, semanticTargetHTTPUpload}, "requires_approval": true},
			map[string]any{"name": semanticActionCreateDocument, "summary": "Create a local office document artifact from structured paragraphs.", "target_kinds": []any{semanticTargetDocument}, "requires_approval": true},
			map[string]any{"name": semanticActionCreateSchedule, "summary": "Create a local calendar file artifact from event semantics.", "target_kinds": []any{semanticTargetCalendar}, "requires_approval": true},
			map[string]any{"name": semanticActionSearch, "summary": "Search memory with semantic, hybrid, keyword, or MMR retrieval semantics.", "target_kinds": []any{semanticTargetMemory}, "requires_approval": false},
			map[string]any{"name": semanticActionSimilarity, "summary": "Compute embedding-based similarity between two texts.", "target_kinds": []any{semanticTargetMemory}, "requires_approval": false},
		},
		"inspects": []any{
			map[string]any{"name": semanticInspectThread, "summary": "Read recent thread history through a channel adapter.", "target_kinds": []any{semanticTargetChannel}},
			map[string]any{"name": semanticInspectParticipants, "summary": "Inspect channel participants via a stable semantic action.", "target_kinds": []any{semanticTargetChannel}},
			map[string]any{"name": semanticInspectMentions, "summary": "Inspect mentions via a stable semantic action.", "target_kinds": []any{semanticTargetChannel}},
			map[string]any{"name": semanticInspectReplyContext, "summary": "Inspect reply context via a stable semantic action.", "target_kinds": []any{semanticTargetChannel}},
			map[string]any{"name": semanticInspectEmbedding, "summary": "Inspect whether semantic embeddings are configured and indexed.", "target_kinds": []any{semanticTargetMemory}},
		},
	})
}

func handleSemanticDeliver(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	action, err := requiredString(call.Input, "action")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	target, err := mapFrom(call.Input["target"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	switch strings.TrimSpace(action) {
	case semanticActionSendMessage, semanticActionSendCard, semanticActionGroupNotify, semanticActionUploadAttachment:
		return handleSemanticChannelDelivery(ctx, b, call, action, target)
	case semanticActionCreateDocument:
		return handleSemanticDocumentCreate(ctx, b, call, target)
	case semanticActionCreateSchedule:
		return handleSemanticScheduleCreate(ctx, b, call, target)
	case semanticActionSearch:
		return handleSemanticSearchAction(ctx, b, call, target)
	case semanticActionSimilarity:
		return handleSemanticSimilarityAction(ctx, b, call, target)
	default:
		supported := []string{
			semanticActionSendMessage, semanticActionSendCard, semanticActionGroupNotify,
			semanticActionUploadAttachment, semanticActionCreateDocument,
			semanticActionCreateSchedule, semanticActionSearch, semanticActionSimilarity,
		}
		return semanticUnsupportedResult(b, call, action, semanticTargetChannel,
			fmt.Sprintf("unknown semantic action %q; supported actions: %s", action, strings.Join(supported, ", ")),
			[]string{"Use semantic.catalog to discover the currently supported delivery contracts."},
		)
	}
}

func handleSemanticInspect(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	kind, err := requiredString(call.Input, "kind")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
	}
	target, err := mapFrom(call.Input["target"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
	}
	targetKind := semanticTargetValue(target)
	switch strings.TrimSpace(kind) {
	case semanticInspectEmbedding:
		if targetKind == "" {
			targetKind = semanticTargetMemory
		}
		return handleSemanticEmbeddingInspect(ctx, b, call, targetKind)
	}
	if targetKind == "" {
		targetKind = semanticTargetChannel
	}
	channelName, err := semanticTargetRequiredString(target, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
	}
	channelID := semanticTargetOptionalString(target, "channel_id")
	if channelID == "" {
		channelID = semanticTargetOptionalString(target, "target_id")
	}
	if channelID == "" {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: channel target requires channel_id or target_id")
	}
	switch strings.TrimSpace(kind) {
	case semanticInspectThread:
		limit, err := intFrom(call.Input["limit"], defaultSemanticInspectLimit)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
		}
		before, _ := stringFrom(call.Input["before"])
		payload, err := semanticNestedPayload(ctx, b, "channel.history", handleChannelHistory, map[string]any{
			"channel":    channelName,
			"channel_id": channelID,
			"limit":      limit,
			"before":     strings.TrimSpace(before),
		})
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
		}
		return semanticWrappedResult(b, call, "kind", kind, semanticStatusOK, true, targetKind, "thread context loaded", payload, nil)
	case semanticInspectParticipants, semanticInspectMentions, semanticInspectReplyContext:
		payload, err := semanticNestedPayload(ctx, b, "channel.action", handleChannelAction, map[string]any{
			"channel":     channelName,
			"channel_id":  channelID,
			"action_type": kind,
			"params":      map[string]any{},
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "does not support") {
				return semanticUnsupportedResult(b, call, kind, targetKind, err.Error(), []string{
					"Use a channel adapter that implements semantic context actions or custom channel.action support.",
				})
			}
			return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
		}
		return semanticWrappedResult(b, call, "kind", kind, semanticStatusOK, true, targetKind, "semantic context loaded", payload, nil)
	default:
		supported := []string{
			semanticInspectEmbedding, semanticInspectThread,
			semanticInspectParticipants, semanticInspectMentions, semanticInspectReplyContext,
		}
		return semanticUnsupportedResult(b, call, kind, targetKind,
			fmt.Sprintf("unknown semantic inspect kind %q; supported kinds: %s", kind, strings.Join(supported, ", ")),
			[]string{"Use semantic.catalog to discover the currently supported inspect operations."},
		)
	}
}

type semanticEmbeddingAccessor interface {
	HasEmbedding() bool
	EmbeddingClient() agent.EmbeddingClient
	VectorStats() (int, int)
}

func handleSemanticSearchAction(ctx context.Context, b *Builtins, call agent.ToolCall, target map[string]any) (contextengine.ToolResult, error) {
	targetKind := semanticTargetValue(target)
	if targetKind == "" {
		targetKind = semanticTargetMemory
	}
	query := semanticQueryValue(call.Input)
	if query == "" {
		return semanticUnsupportedResult(b, call, semanticActionSearch, targetKind, "query or content is required for semantic search", []string{
			"Provide query (or content) when calling semantic.deliver with action search.",
		})
	}
	if b.memoryStore == nil {
		return semanticUnsupportedResult(b, call, semanticActionSearch, targetKind, "memory store not configured", []string{
			"Configure a memory store before using semantic search actions.",
		})
	}

	mode := semanticSearchModeValue(call.Input)
	limit, err := semanticLimitValue(call.Input, defaultMemorySearchLimit)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	lambda, err := semanticLambdaValue(call.Input, defaultMemoryMMRLambda)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}

	var entries []agent.MemoryEntry
	switch mode {
	case memorySearchModeSemantic:
		entries, err = b.memoryStore.SemanticSearch(ctx, query, limit)
	case memorySearchModeMMR:
		entries, err = b.memoryStore.SemanticSearchMMR(ctx, query, limit, lambda)
	case memorySearchModeHybrid, memorySearchModeKeyword:
		entries, err = b.memoryStore.Search(ctx, query)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: unsupported semantic search mode %q", mode)
	}
	if err != nil {
		if semanticEmbeddingNotConfigured(err) {
			return semanticUnsupportedResult(b, call, semanticActionSearch, targetKind, err.Error(), []string{
				"Enable embeddings for semantic/mmr search or switch mode to hybrid.",
			})
		}
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}

	payload := map[string]any{
		"query":   query,
		"mode":    mode,
		"count":   len(entries),
		"entries": semanticMemoryEntries(entries),
	}
	if mode == memorySearchModeMMR {
		payload["lambda"] = lambda
	}
	return semanticWrappedResult(b, call, "action", semanticActionSearch, semanticStatusOK, true, targetKind, "semantic search completed", payload, nil)
}

func handleSemanticSimilarityAction(ctx context.Context, b *Builtins, call agent.ToolCall, target map[string]any) (contextengine.ToolResult, error) {
	targetKind := semanticTargetValue(target)
	if targetKind == "" {
		targetKind = semanticTargetMemory
	}
	left := semanticQueryValue(call.Input)
	if left == "" {
		return semanticUnsupportedResult(b, call, semanticActionSimilarity, targetKind, "query or content is required for similarity", []string{
			"Provide query (or content) and candidate when calling semantic.deliver with action similarity.",
		})
	}
	right := strings.TrimSpace(semanticMapOptionalString(call.Input, "candidate"))
	if right == "" {
		return semanticUnsupportedResult(b, call, semanticActionSimilarity, targetKind, "candidate is required for similarity", []string{
			"Set candidate to the text you want to compare against the semantic query.",
		})
	}

	accessor := semanticEmbeddingSource(b.memoryStore)
	if accessor == nil || accessor.EmbeddingClient() == nil || !accessor.HasEmbedding() {
		return semanticUnsupportedResult(b, call, semanticActionSimilarity, targetKind, "embedding is not configured", []string{
			"Enable embeddings on the memory store before using semantic similarity actions.",
		})
	}
	vectors, err := accessor.EmbeddingClient().Embed(ctx, []string{left, right})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: embed similarity texts: %w", err)
	}
	if len(vectors) != 2 {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: embedding client returned %d vectors, want 2", len(vectors))
	}

	score := semanticCosineSimilarity(vectors[0], vectors[1])
	return semanticWrappedResult(b, call, "action", semanticActionSimilarity, semanticStatusOK, true, targetKind, "semantic similarity computed", map[string]any{
		"query":     left,
		"candidate": right,
		"score":     score,
	}, nil)
}

func handleSemanticEmbeddingInspect(ctx context.Context, b *Builtins, call agent.ToolCall, targetKind string) (contextengine.ToolResult, error) {
	payload := map[string]any{
		"store_configured":     b.memoryStore != nil,
		"embedding_configured": false,
		"entry_count":          0,
		"vector_count":         0,
		"vector_dimension":     0,
	}
	if b.memoryStore == nil {
		return semanticWrappedResult(b, call, "kind", semanticInspectEmbedding, semanticStatusOK, true, targetKind, "memory store not configured", payload, []string{
			"Configure a memory store and embedding provider to enable semantic retrieval.",
		})
	}

	entries, err := b.memoryStore.List(ctx)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.inspect_context: %w", err)
	}
	payload["entry_count"] = len(entries)

	accessor := semanticEmbeddingSource(b.memoryStore)
	if accessor == nil {
		return semanticWrappedResult(b, call, "kind", semanticInspectEmbedding, semanticStatusOK, true, targetKind, "memory store does not expose embedding diagnostics", payload, []string{
			"Use an embedding-aware memory store implementation to inspect vector state.",
		})
	}

	payload["embedding_configured"] = accessor.HasEmbedding()
	if client := accessor.EmbeddingClient(); client != nil {
		payload["embedding_client"] = fmt.Sprintf("%T", client)
	}
	vectorCount, vectorDimension := accessor.VectorStats()
	payload["vector_count"] = vectorCount
	payload["vector_dimension"] = vectorDimension

	message := "embedding status loaded"
	hints := []string(nil)
	if !accessor.HasEmbedding() {
		message = "embedding is not configured"
		hints = []string{
			"Enable embeddings on the configured memory store to unlock semantic search and similarity actions.",
		}
	}
	return semanticWrappedResult(b, call, "kind", semanticInspectEmbedding, semanticStatusOK, true, targetKind, message, payload, hints)
}

func handleSemanticChannelDelivery(ctx context.Context, b *Builtins, call agent.ToolCall, action string, target map[string]any) (contextengine.ToolResult, error) {
	targetKind := semanticTargetValue(target)
	if targetKind == "" {
		targetKind = semanticTargetChannel
	}
	if action == semanticActionUploadAttachment && targetKind == semanticTargetHTTPUpload {
		return handleSemanticHTTPUpload(ctx, b, call, action, target)
	}
	channelName, err := semanticTargetRequiredString(target, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	targetID, err := semanticTargetRequiredString(target, "target_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	content, _ := stringFrom(call.Input["content"])
	format, _ := stringFrom(call.Input["format"])
	replyToID := semanticReplyTarget(call.Input)
	blocks, err := semanticBlocks(call.Input["blocks"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	attachments, err := semanticAttachments(b, call.Input["attachments"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	if action == semanticActionSendCard && strings.TrimSpace(format) == "" {
		format = "rich"
	}
	if action == semanticActionGroupNotify && strings.TrimSpace(format) == "" {
		format = "markdown"
	}
	if strings.TrimSpace(content) == "" && len(blocks) == 0 && len(attachments) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: content, blocks, or attachments are required")
	}
	caps := adapter.Capabilities()
	if len(blocks) > 0 && !caps.SendRichText {
		return semanticUnsupportedResult(b, call, action, targetKind, fmt.Sprintf("channel %q does not support structured card delivery", channelName), []string{
			"Fallback to send_message or choose a channel with rich-text/card support.",
		})
	}
	if len(attachments) > 0 && !caps.SendFile {
		return semanticUnsupportedResult(b, call, action, targetKind, fmt.Sprintf("channel %q does not support file attachments", channelName), []string{
			"Fallback to a plain message with a link or switch to an adapter with file delivery support.",
		})
	}
	metadata := map[string]any{
		"semantic_action": action,
	}
	if action == semanticActionGroupNotify {
		metadata["mention_all"] = true
	}
	msg := channels.OutboundMessage{
		ChannelID:   strings.TrimSpace(semanticTargetOptionalString(target, "channel_id")),
		TargetID:    targetID,
		ReplyToID:   replyToID,
		Content:     strings.TrimSpace(content),
		Format:      strings.TrimSpace(format),
		Blocks:      blocks,
		Attachments: attachments,
		Metadata:    metadata,
	}
	if err := adapter.Send(ctx, msg); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	return semanticWrappedResult(b, call, "action", action, semanticStatusOK, true, targetKind, "semantic delivery completed", map[string]any{
		"channel":          channelName,
		"target_id":        targetID,
		"channel_id":       msg.ChannelID,
		"format":           msg.Format,
		"block_count":      len(blocks),
		"attachment_count": len(attachments),
		"reply_to_id":      msg.ReplyToID,
		"mention_all":      action == semanticActionGroupNotify,
	}, nil)
}

func handleSemanticHTTPUpload(ctx context.Context, b *Builtins, call agent.ToolCall, action string, target map[string]any) (contextengine.ToolResult, error) {
	rawURL, err := semanticTargetRequiredString(target, "url")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	path, err := semanticFirstAttachmentPath(call.Input["attachments"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	input := map[string]any{
		"url":    rawURL,
		"path":   path,
		"field":  strings.TrimSpace(semanticTargetOptionalString(target, "field")),
		"method": strings.TrimSpace(semanticTargetOptionalString(target, "method")),
	}
	payload, err := semanticNestedPayload(ctx, b, "net.upload", handleNetUpload, input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	return semanticWrappedResult(b, call, "action", action, semanticStatusOK, true, semanticTargetHTTPUpload, "attachment upload completed", payload, nil)
}

func handleSemanticDocumentCreate(ctx context.Context, b *Builtins, call agent.ToolCall, _ map[string]any) (contextengine.ToolResult, error) {
	document, err := mapFrom(call.Input["document"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	if len(document) == 0 {
		return semanticUnsupportedResult(b, call, semanticActionCreateDocument, semanticTargetDocument, "document payload is required", []string{
			"Provide document.path and document.content paragraphs when calling semantic.deliver.",
		})
	}
	payload, err := semanticNestedPayload(ctx, b, "document.create", semanticDocumentCreateHandler, document)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	return semanticWrappedResult(b, call, "action", semanticActionCreateDocument, semanticStatusOK, true, semanticTargetDocument, "document artifact created", payload, nil)
}

func handleSemanticScheduleCreate(ctx context.Context, b *Builtins, call agent.ToolCall, target map[string]any) (contextengine.ToolResult, error) {
	schedule, err := mapFrom(call.Input["schedule"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	if len(schedule) == 0 {
		return semanticUnsupportedResult(b, call, semanticActionCreateSchedule, semanticTargetCalendar, "schedule payload is required", []string{
			"Provide schedule.path, schedule.summary, and schedule.start when calling semantic.deliver.",
		})
	}
	path, _ := stringFrom(schedule["path"])
	if strings.TrimSpace(path) == "" {
		if targetPath := semanticTargetOptionalString(target, "path"); targetPath != "" {
			path = targetPath
		}
	}
	if strings.TrimSpace(path) == "" {
		return semanticUnsupportedResult(b, call, semanticActionCreateSchedule, semanticTargetCalendar, "calendar file output path is required", []string{
			"Set schedule.path so the semantic layer can materialize a deliverable ICS artifact.",
		})
	}
	event := map[string]any{
		"summary":     strings.TrimSpace(semanticMapOptionalString(schedule, "summary")),
		"start":       strings.TrimSpace(semanticMapOptionalString(schedule, "start")),
		"end":         strings.TrimSpace(semanticMapOptionalString(schedule, "end")),
		"description": strings.TrimSpace(semanticMapOptionalString(schedule, "description")),
		"location":    strings.TrimSpace(semanticMapOptionalString(schedule, "location")),
		"status":      strings.TrimSpace(semanticMapOptionalString(schedule, "status")),
		"organizer":   strings.TrimSpace(semanticMapOptionalString(schedule, "organizer")),
		"attendees":   schedule["attendees"],
	}
	payload, err := semanticNestedPayload(ctx, b, "calendar.create_ics", handleCalendarCreateICS, map[string]any{
		"path":   path,
		"events": []any{event},
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("semantic.deliver: %w", err)
	}
	return semanticWrappedResult(b, call, "action", semanticActionCreateSchedule, semanticStatusOK, true, semanticTargetCalendar, "calendar artifact created", payload, nil)
}

func semanticNestedPayload(ctx context.Context, b *Builtins, name string, handler builtinHandler, input map[string]any) (map[string]any, error) {
	result, err := handler(ctx, b, agent.ToolCall{
		ID:    "semantic-nested-" + name,
		Name:  name,
		Input: semanticPruneEmpty(input),
	})
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return nil, fmt.Errorf("decode nested payload: %w", err)
	}
	return payload, nil
}

func semanticWrappedResult(b *Builtins, call agent.ToolCall, key, value, status string, supported bool, targetKind, message string, result map[string]any, hints []string) (contextengine.ToolResult, error) {
	payload := map[string]any{
		key:           value,
		"status":      status,
		"supported":   supported,
		"target_kind": targetKind,
		"message":     message,
		"result":      result,
		"hints":       semanticStringSlice(hints),
	}
	return b.jsonResult(call, payload)
}

func semanticUnsupportedResult(b *Builtins, call agent.ToolCall, value, targetKind, message string, hints []string) (contextengine.ToolResult, error) {
	key := "action"
	if call.Name == "semantic.inspect_context" {
		key = "kind"
	}
	return semanticWrappedResult(b, call, key, value, semanticStatusUnsupported, false, targetKind, message, map[string]any{}, hints)
}

func semanticTargetValue(target map[string]any) string {
	kind, _ := stringFrom(target["kind"])
	return strings.TrimSpace(kind)
}

func semanticTargetRequiredString(target map[string]any, key string) (string, error) {
	value, _ := stringFrom(target[key])
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("target.%s is required", key)
	}
	return value, nil
}

func semanticTargetOptionalString(target map[string]any, key string) string {
	value, _ := stringFrom(target[key])
	return strings.TrimSpace(value)
}

func semanticQueryValue(input map[string]any) string {
	query := strings.TrimSpace(semanticMapOptionalString(input, "query"))
	if query != "" {
		return query
	}
	content, _ := stringFrom(input["content"])
	return strings.TrimSpace(content)
}

func semanticSearchModeValue(input map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(semanticMapOptionalString(input, "mode")))
	if mode != "" {
		return mode
	}
	options, _ := mapFrom(input["options"])
	if options != nil {
		mode = strings.ToLower(strings.TrimSpace(semanticMapOptionalString(options, "mode")))
	}
	if mode == "" {
		mode = memorySearchModeSemantic
	}
	return mode
}

func semanticLimitValue(input map[string]any, fallback int) (int, error) {
	options, _ := mapFrom(input["options"])
	if options != nil && options["limit"] != nil {
		return intFrom(options["limit"], fallback)
	}
	return intFrom(input["limit"], fallback)
}

func semanticLambdaValue(input map[string]any, fallback float64) (float64, error) {
	if input["lambda"] != nil {
		return floatFrom(input["lambda"], fallback)
	}
	options, _ := mapFrom(input["options"])
	if options != nil && options["lambda"] != nil {
		return floatFrom(options["lambda"], fallback)
	}
	return fallback, nil
}

func semanticMemoryEntries(entries []agent.MemoryEntry) []any {
	items := make([]any, 0, len(entries))
	for _, entry := range entries {
		item := memoryEntryPayload(entry)
		item["created_at"] = entry.CreatedAt.Format(timeFormatRFC3339())
		item["updated_at"] = entry.UpdatedAt.Format(timeFormatRFC3339())
		if !entry.LastUsedAt.IsZero() {
			item["last_used_at"] = entry.LastUsedAt.Format(timeFormatRFC3339())
		}
		items = append(items, item)
	}
	return items
}

func semanticEmbeddingSource(store agent.MemoryStore) semanticEmbeddingAccessor {
	if store == nil {
		return nil
	}
	accessor, _ := store.(semanticEmbeddingAccessor)
	return accessor
}

func semanticEmbeddingNotConfigured(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "embedding client not configured")
}

func semanticCosineSimilarity(left, right []float32) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var dot float64
	var leftNorm float64
	var rightNorm float64
	for index := range left {
		lv := float64(left[index])
		rv := float64(right[index])
		dot += lv * rv
		leftNorm += lv * lv
		rightNorm += rv * rv
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func timeFormatRFC3339() string {
	return "2006-01-02T15:04:05Z07:00"
}

// semanticDocumentCreateHandler adapts the document sub-package handler for use
// by semanticNestedPayload, which expects a builtinHandler signature.
func semanticDocumentCreateHandler(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	defs := document.ToolDefs()
	for _, d := range defs {
		if d.Manifest.Name == "document.create" {
			return d.Handler(ctx, b, call)
		}
	}
	return contextengine.ToolResult{}, fmt.Errorf("document.create handler not found")
}

func semanticMapOptionalString(value map[string]any, key string) string {
	raw, _ := stringFrom(value[key])
	return strings.TrimSpace(raw)
}

func semanticReplyTarget(input map[string]any) string {
	options, _ := mapFrom(input["options"])
	if reply := semanticMapOptionalString(options, "reply_to_id"); reply != "" {
		return reply
	}
	reply, _ := stringFrom(input["reply_to_id"])
	return strings.TrimSpace(reply)
}

func semanticBlocks(raw any) ([]channels.OutboundBlock, error) {
	items, ok := raw.([]any)
	if raw == nil {
		return nil, nil
	}
	if !ok {
		return nil, fmt.Errorf("blocks must be an array")
	}
	blocks := make([]channels.OutboundBlock, 0, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("blocks[%d] must be an object", index)
		}
		block := channels.OutboundBlock{
			Kind:    semanticMapOptionalString(entry, "kind"),
			Title:   semanticMapOptionalString(entry, "title"),
			Content: semanticMapOptionalString(entry, "content"),
		}
		if block.Kind == "" {
			block.Kind = "section"
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func semanticAttachments(b *Builtins, raw any) ([]channels.OutboundAttachment, error) {
	items, ok := raw.([]any)
	if raw == nil {
		return nil, nil
	}
	if !ok {
		return nil, fmt.Errorf("attachments must be an array")
	}
	attachments := make([]channels.OutboundAttachment, 0, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("attachments[%d] must be an object", index)
		}
		path := semanticMapOptionalString(entry, "path")
		uri := semanticMapOptionalString(entry, "uri")
		if path != "" {
			resolved, err := b.resolvePath(path)
			if err != nil {
				return nil, fmt.Errorf("attachments[%d]: %w", index, err)
			}
			uri = "file://" + filepath.ToSlash(resolved)
		}
		attachment := channels.OutboundAttachment{
			Kind:        semanticMapOptionalString(entry, "kind"),
			Label:       semanticMapOptionalString(entry, "label"),
			URI:         uri,
			ContentType: semanticMapOptionalString(entry, "content_type"),
		}
		if attachment.Kind == "" {
			attachment.Kind = "file"
		}
		if attachment.Label == "" && path != "" {
			attachment.Label = filepath.Base(path)
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func semanticFirstAttachmentPath(raw any) (string, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return "", fmt.Errorf("at least one attachment with path is required")
	}
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("attachments[%d] must be an object", index)
		}
		path := semanticMapOptionalString(entry, "path")
		if path != "" {
			return path, nil
		}
	}
	return "", fmt.Errorf("no attachment path provided")
}

func semanticPruneEmpty(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			out[key] = typed
		default:
			if value == nil {
				continue
			}
			out[key] = value
		}
	}
	return out
}

func semanticStringSlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
