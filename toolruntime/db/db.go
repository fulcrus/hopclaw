// Package db implements in-memory key-value store tool handlers
// (db.kv.get, db.kv.set, db.kv.delete, db.kv.list) for the toolruntime registry.
package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that db handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
}

// Handler is the tool handler signature for db tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a db handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// kvStore is a package-level in-memory key-value store shared across all
// runtime instances. It serves as a lightweight scratchpad the agent can
// use to persist small pieces of data during a session.
var kvStore sync.Map

// ResetKVStore clears the in-memory KV store. Exported for testing.
func ResetKVStore() {
	kvStore.Range(func(key, _ any) bool {
		kvStore.Delete(key)
		return true
	})
}

// ToolDefs returns all db domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "db.kv.get",
				Description:     "Get a value from the in-memory key-value store.",
				InputSchema:     kvGetInputSchema(),
				OutputSchema:    kvGetOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "db:kv:{key}",
			},
			Handler: handleKVGet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "db.kv.set",
				Description:     "Set a key-value pair in the in-memory store.",
				InputSchema:     kvSetInputSchema(),
				OutputSchema:    kvSetOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "db:kv:{key}",
			},
			Handler: handleKVSet,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "db.kv.delete",
				Description:     "Delete a key from the in-memory store.",
				InputSchema:     kvDeleteInputSchema(),
				OutputSchema:    kvDeleteOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "db:kv:{key}",
			},
			Handler: handleKVDelete,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "db.kv.list",
				Description:     "List keys in the in-memory store, optionally filtered by prefix.",
				InputSchema:     kvListInputSchema(),
				OutputSchema:    kvListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "db:kv:list",
			},
			Handler: handleKVList,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func kvGetInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The key to look up.",
			},
		},
		"required":             []string{"key"},
		"additionalProperties": false,
	}
}

func kvSetInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The key to set.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "The value to store.",
			},
		},
		"required":             []string{"key", "value"},
		"additionalProperties": false,
	}
}

func kvDeleteInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The key to delete.",
			},
		},
		"required":             []string{"key"},
		"additionalProperties": false,
	}
}

func kvListInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prefix": map[string]any{
				"type":        "string",
				"description": "Optional prefix to filter keys.",
			},
		},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringArraySchema(description string) map[string]any {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
		},
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func kvGetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":    stringSchema("The requested key."),
		"value":  stringSchema("The stored value, or null if not found."),
		"exists": booleanSchema("Whether the key exists."),
	}, "key", "exists")
}

func kvSetOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":     stringSchema("The key that was set."),
		"message": stringSchema("Human-readable confirmation."),
	}, "key", "message")
}

func kvDeleteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":     stringSchema("The key that was targeted for deletion."),
		"deleted": booleanSchema("Whether the key existed and was deleted."),
		"message": stringSchema("Human-readable result."),
	}, "key", "deleted", "message")
}

func kvListOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"keys":  stringArraySchema("Matching keys."),
		"count": integerSchema("Number of matching keys."),
	}, "keys", "count")
}

// ---------------------------------------------------------------------------
// Param helpers
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleKVGet(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("db.kv.get: %w", err)
	}

	val, ok := kvStore.Load(key)
	result := map[string]any{
		"key":    key,
		"exists": ok,
	}
	if ok {
		result["value"] = val
	} else {
		result["value"] = nil
	}
	return rt.JSONResult(call, result)
}

func handleKVSet(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("db.kv.set: %w", err)
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("db.kv.set: %w", err)
	}

	kvStore.Store(key, value)

	return rt.JSONResult(call, map[string]any{
		"key":     key,
		"message": fmt.Sprintf("stored value for key %q", key),
	})
}

func handleKVDelete(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("db.kv.delete: %w", err)
	}

	_, existed := kvStore.LoadAndDelete(key)

	msg := fmt.Sprintf("key %q not found", key)
	if existed {
		msg = fmt.Sprintf("deleted key %q", key)
	}

	return rt.JSONResult(call, map[string]any{
		"key":     key,
		"deleted": existed,
		"message": msg,
	})
}

func handleKVList(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	prefix, _ := stringFrom(call.Input["prefix"])

	var keys []string
	kvStore.Range(func(k, v any) bool {
		keyStr, ok := k.(string)
		if !ok {
			return true
		}
		if prefix == "" || strings.HasPrefix(keyStr, prefix) {
			keys = append(keys, keyStr)
		}
		return true
	})
	sort.Strings(keys)

	return rt.JSONResult(call, map[string]any{
		"keys":  keys,
		"count": len(keys),
	})
}
