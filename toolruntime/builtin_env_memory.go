package toolruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func handleSessionList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	sessions, err := agent.ListSessions(ctx, b.sessions, agent.SessionListFilter{})
	if errors.Is(err, agent.ErrSessionListUnsupported) {
		return b.jsonResult(call, map[string]any{
			"sessions": []any{},
			"message":  "session store does not support listing",
		})
	}
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"sessions": []any{},
			"message":  fmt.Sprintf("failed to list sessions: %v", err),
		})
	}

	var items []map[string]any
	for _, sess := range sessions {
		summary := sess.ToSummary()
		items = append(items, map[string]any{
			"id":         sess.ID,
			"key":        sess.Key,
			"model":      sess.Model,
			"messages":   summary.MessageCount,
			"created_at": sess.CreatedAt.Format(time.RFC3339),
			"updated_at": sess.UpdatedAt.Format(time.RFC3339),
		})
	}
	return b.jsonResult(call, map[string]any{
		"sessions": items,
		"count":    len(items),
	})
}

// ---------------------------------------------------------------------------
// session.history
// ---------------------------------------------------------------------------

func handleSessionHistory(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	sess, err := agent.LoadSessionMetadata(ctx, b.sessions, sessionID, agent.ScopeFilter{})
	if errors.Is(err, agent.ErrSessionReadUnsupported) {
		return b.jsonResult(call, map[string]any{
			"session_id": sessionID,
			"messages":   []any{},
			"message":    "session store does not support reading",
		})
	}
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"session_id": sessionID,
			"messages":   []any{},
			"message":    fmt.Sprintf("session not found: %v", err),
		})
	}

	limit := 50
	if v, ok := call.Input["limit"]; ok {
		if n, ok := v.(float64); ok && n > 0 {
			limit = int(n)
		}
	}

	msgs, err := agent.LoadRecentMessages(ctx, b.sessions, sessionID, limit)
	if errors.Is(err, agent.ErrSessionReadUnsupported) {
		return b.jsonResult(call, map[string]any{
			"session_id": sessionID,
			"messages":   []any{},
			"message":    "session store does not support reading",
		})
	}
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"session_id": sessionID,
			"messages":   []any{},
			"message":    fmt.Sprintf("session not found: %v", err),
		})
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	var items []map[string]any
	for _, msg := range msgs {
		items = append(items, map[string]any{
			"role":       string(msg.Role),
			"content":    msg.Content,
			"created_at": msg.CreatedAt.Format(time.RFC3339),
		})
	}
	return b.jsonResult(call, map[string]any{
		"session_id":     sessionID,
		"messages":       items,
		"total_messages": sess.TotalMessageCount(),
	})
}

// ---------------------------------------------------------------------------
// memory.get
// ---------------------------------------------------------------------------

func memoryEntryPayload(entry agent.MemoryEntry) map[string]any {
	item := map[string]any{
		"key":             entry.Key,
		"value":           entry.Value,
		"fact_class":      entry.FactClass,
		"namespace":       entry.Namespace,
		"scope_key":       entry.ScopeKey,
		"field":           entry.Field,
		"label":           entry.Label,
		"managed":         entry.Managed,
		"source":          entry.Source,
		"tags":            entry.Tags,
		"previous_values": entry.PreviousValues,
		"evidence_count":  entry.EvidenceCount,
		"score":           entry.Score,
		"state":           entry.State,
		"created_at":      entry.CreatedAt.Format(time.RFC3339),
		"updated_at":      entry.UpdatedAt.Format(time.RFC3339),
	}
	if entry.SupersededBy != "" {
		item["superseded_by"] = entry.SupersededBy
	}
	if entry.SessionKey != "" {
		item["session_key"] = entry.SessionKey
	}
	if entry.ProjectID != "" {
		item["project_id"] = entry.ProjectID
	}
	if len(entry.MediaRefs) > 0 {
		item["media_refs"] = entry.MediaRefs
	}
	if entry.UsedCount > 0 {
		item["used_count"] = entry.UsedCount
	}
	if !entry.LastUsedAt.IsZero() {
		item["last_used_at"] = entry.LastUsedAt.Format(time.RFC3339)
	}
	if entry.CorrectionCount > 0 {
		item["correction_count"] = entry.CorrectionCount
	}
	return item
}

func handleMemoryGet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.memoryStore == nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"found":   false,
			"message": "memory store not configured",
		})
	}
	entry, err := b.memoryStore.Get(ctx, key)
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"found":   false,
			"message": fmt.Sprintf("get failed: %v", err),
		})
	}
	if entry == nil {
		return b.jsonResult(call, map[string]any{
			"key":   key,
			"found": false,
		})
	}
	item := memoryEntryPayload(*entry)
	item["found"] = true
	return b.jsonResult(call, item)
}

// ---------------------------------------------------------------------------
// memory.set
// ---------------------------------------------------------------------------

func handleMemorySet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.memoryStore == nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"success": false,
			"message": "memory store not configured",
		})
	}

	if upserter, ok := b.memoryStore.(agent.AgentUpserter); ok {
		record := agent.MemoryRecord{
			Key:    key,
			Value:  value,
			Source: agent.MemorySourceAgent,
		}
		if session := builtinSessionFromContext(ctx); session != nil {
			record.SessionKey = strings.TrimSpace(session.Key)
		}
		if strings.TrimSpace(b.rootAbs) != "" {
			record.ProjectID = agent.ProjectID(b.rootAbs)
		}

		entry, mutation, err := upserter.AgentUpsert(ctx, record)
		if err != nil {
			return b.jsonResult(call, map[string]any{
				"key":     key,
				"success": false,
				"message": fmt.Sprintf("set failed: %v", err),
			})
		}
		if mutation.Action == agent.MutationBlocked {
			return b.jsonResult(call, map[string]any{
				"key":     key,
				"blocked": true,
				"reason":  mutation.Reason,
				"hint":    "This memory was set by the user and cannot be overwritten. Ask the user if they want to update it.",
			})
		}
		if entry == nil {
			return b.jsonResult(call, map[string]any{
				"key":     key,
				"success": true,
			})
		}
		item := memoryEntryPayload(*entry)
		item["success"] = true
		return b.jsonResult(call, item)
	}

	if err := b.memoryStore.Set(ctx, key, value); err != nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"success": false,
			"message": fmt.Sprintf("set failed: %v", err),
		})
	}
	return b.jsonResult(call, map[string]any{
		"key":     key,
		"success": true,
		"message": fmt.Sprintf("stored %q", key),
	})
}

// ---------------------------------------------------------------------------
// memory.delete
// ---------------------------------------------------------------------------

func handleMemoryDelete(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.memoryStore == nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"success": false,
			"message": "memory store not configured",
		})
	}
	if err := b.memoryStore.Delete(ctx, key); err != nil {
		return b.jsonResult(call, map[string]any{
			"key":     key,
			"success": false,
			"message": fmt.Sprintf("delete failed: %v", err),
		})
	}
	return b.jsonResult(call, map[string]any{
		"key":     key,
		"success": true,
		"message": fmt.Sprintf("deleted %q", key),
	})
}

// ---------------------------------------------------------------------------
// memory.search
// ---------------------------------------------------------------------------

func handleMemoryList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.memoryStore == nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"count":   0,
			"message": "memory store not configured",
		})
	}
	prefix, _ := stringFrom(call.Input["prefix"])
	limit, _ := intFrom(call.Input["limit"], defaultMemorySearchLimit)
	if limit <= 0 {
		limit = defaultMemorySearchLimit
	}
	entries, err := b.memoryStore.List(ctx)
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"count":   0,
			"message": fmt.Sprintf("list failed: %v", err),
		})
	}
	capHint := len(entries)
	if capHint > limit {
		capHint = limit
	}
	items := make([]map[string]any, 0, capHint)
	for _, entry := range entries {
		if prefix != "" && !strings.HasPrefix(entry.Key, prefix) {
			continue
		}
		items = append(items, memoryEntryPayload(entry))
		if len(items) >= limit {
			break
		}
	}
	return b.jsonResult(call, map[string]any{
		"results": items,
		"count":   len(items),
		"prefix":  prefix,
	})
}

const (
	memorySearchModeKeyword  = "keyword"
	memorySearchModeSemantic = "semantic"
	memorySearchModeHybrid   = "hybrid"
	memorySearchModeMMR      = "mmr"
	defaultMemorySearchLimit = 10
	defaultMemoryMMRLambda   = 0.5
)

func handleMemorySearch(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if b.memoryStore == nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"message": "memory store not configured",
		})
	}

	mode, _ := stringFrom(call.Input["mode"])
	if mode == "" {
		mode = memorySearchModeKeyword
	}
	limit, _ := intFrom(call.Input["limit"], 0)

	var entries []agent.MemoryEntry
	switch mode {
	case memorySearchModeKeyword:
		entries, err = keywordSearchMemoryEntries(ctx, b.memoryStore, query, limit)
	case memorySearchModeSemantic:
		if limit <= 0 {
			limit = defaultMemorySearchLimit
		}
		entries, err = b.memoryStore.SemanticSearch(ctx, query, limit)
	case memorySearchModeMMR:
		if limit <= 0 {
			limit = defaultMemorySearchLimit
		}
		lambda, _ := floatFrom(call.Input["lambda"], defaultMemoryMMRLambda)
		entries, err = b.memoryStore.SemanticSearchMMR(ctx, query, limit, lambda)
	case memorySearchModeHybrid:
		entries, err = b.memoryStore.Search(ctx, query)
		if err == nil && limit > 0 && len(entries) > limit {
			entries = entries[:limit]
		}
	default:
		return contextengine.ToolResult{}, fmt.Errorf("memory.search: unsupported mode %q", mode)
	}

	if err != nil {
		return b.jsonResult(call, map[string]any{
			"results": []any{},
			"message": fmt.Sprintf("search failed: %v", err),
		})
	}
	var items []map[string]any
	for _, e := range entries {
		items = append(items, memoryEntryPayload(e))
	}
	return b.jsonResult(call, map[string]any{
		"results": items,
		"count":   len(items),
		"mode":    mode,
	})
}

func keywordSearchMemoryEntries(ctx context.Context, store agent.MemoryStore, query string, limit int) ([]agent.MemoryEntry, error) {
	entries, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return nil, nil
	}
	results := make([]agent.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Key), lowerQuery) ||
			strings.Contains(strings.ToLower(entry.Value), lowerQuery) {
			results = append(results, entry)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Session / Memory input/output schemas
// ---------------------------------------------------------------------------
