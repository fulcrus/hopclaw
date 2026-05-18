package toolruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// defaultHistoryLimit is the default number of messages returned by channel.history.
const defaultHistoryLimit = 20

func channelToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "channel.list",
				Description:     "List all registered channels with their connection status and capabilities.",
				InputSchema:     channelListInputSchema(),
				OutputSchema:    channelListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "channel:list",
			},
			Handler: handleChannelList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "channel.status",
				Description:     "Get detailed status and capability information for a specific channel.",
				InputSchema:     channelStatusInputSchema(),
				OutputSchema:    channelStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "channel:status:{name}",
			},
			Handler: handleChannelStatus,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "channel.send",
				Description:      "Send a message to a channel target.",
				InputSchema:      channelSendInputSchema(),
				OutputSchema:     channelSendOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "channel:send:{channel}:{target_id}",
			},
			Handler: handleChannelSend,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "channel.edit",
				Description:      "Edit a previously sent message in a channel.",
				InputSchema:      channelEditInputSchema(),
				OutputSchema:     channelEditOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "channel:edit:{channel}:{message_id}",
			},
			Handler: handleChannelEdit,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "channel.delete",
				Description:      "Delete a message from a channel.",
				InputSchema:      channelDeleteInputSchema(),
				OutputSchema:     channelDeleteOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "channel:delete:{channel}:{message_id}",
			},
			Handler: handleChannelDelete,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "channel.react",
				Description:      "Add or remove an emoji reaction on a channel message.",
				InputSchema:      channelReactInputSchema(),
				OutputSchema:     channelReactOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "channel:react:{channel}:{message_id}",
			},
			Handler: handleChannelReact,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "channel.history",
				Description:     "Read message history from a channel.",
				InputSchema:     channelHistoryInputSchema(),
				OutputSchema:    channelHistoryOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "channel:history:{channel}:{channel_id}",
			},
			Handler: handleChannelHistory,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "channel.action",
				Description:      "Execute a custom action on a channel.",
				InputSchema:      channelActionInputSchema(),
				OutputSchema:     channelActionOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "channel:action:{channel}:{action_type}",
			},
			Handler: handleChannelAction,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func channelListInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func channelStatusInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func channelSendInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"target_id": map[string]any{
				"type":        "string",
				"description": "Target identifier (e.g. user ID, group ID) within the channel.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Message content to send.",
			},
			"reply_to_id": map[string]any{
				"type":        "string",
				"description": "Optional message ID to reply to.",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Message format: text, markdown, or rich. Defaults to text.",
			},
		},
		"required":             []string{"channel", "target_id", "content"},
		"additionalProperties": false,
	}
}

func channelEditInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Channel identifier where the message resides.",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "ID of the message to edit.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "New message content.",
			},
		},
		"required":             []string{"channel", "channel_id", "message_id", "content"},
		"additionalProperties": false,
	}
}

func channelDeleteInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Channel identifier where the message resides.",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "ID of the message to delete.",
			},
		},
		"required":             []string{"channel", "channel_id", "message_id"},
		"additionalProperties": false,
	}
}

func channelReactInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Channel identifier where the message resides.",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "ID of the message to react to.",
			},
			"emoji": map[string]any{
				"type":        "string",
				"description": "Emoji name or symbol for the reaction.",
			},
			"remove": map[string]any{
				"type":        "boolean",
				"description": "If true, remove the reaction instead of adding it.",
			},
		},
		"required":             []string{"channel", "channel_id", "message_id", "emoji"},
		"additionalProperties": false,
	}
}

func channelHistoryInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Channel identifier to read history from.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of messages to return. Defaults to 20.",
			},
			"before": map[string]any{
				"type":        "string",
				"description": "Return messages before this message ID for pagination.",
			},
		},
		"required":             []string{"channel", "channel_id"},
		"additionalProperties": false,
	}
}

func channelActionInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Name of the registered channel.",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Channel identifier to execute the action on.",
			},
			"action_type": map[string]any{
				"type":        "string",
				"description": "Type of the custom action to execute.",
			},
			"params": map[string]any{
				"type":        "object",
				"description": "Optional parameters for the action.",
			},
		},
		"required":             []string{"channel", "channel_id", "action_type"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func channelListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"name":         stringSchema("Registered channel name."),
		"status":       stringSchema("Connection status."),
		"capabilities": channelCapabilitiesSchema(),
	}, "name", "status", "capabilities")
	return objectSchema(map[string]any{
		"channels": arraySchema(entry, "Registered channels."),
		"count":    integerSchema("Number of registered channels."),
	}, "channels", "count")
}

func channelStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":             stringSchema("Channel name."),
		"status":           stringSchema("Connection status."),
		"capabilities":     channelCapabilitiesSchema(),
		"supports_edit":    booleanSchema("Whether the channel supports editing messages."),
		"supports_delete":  booleanSchema("Whether the channel supports deleting messages."),
		"supports_react":   booleanSchema("Whether the channel supports reactions."),
		"supports_history": booleanSchema("Whether the channel supports reading history."),
		"supports_action":  booleanSchema("Whether the channel supports custom actions."),
	}, "name", "status", "capabilities")
}

func channelSendOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the send was successful."),
		"channel":   stringSchema("Channel name."),
		"target_id": stringSchema("Target the message was sent to."),
	}, "ok", "channel", "target_id")
}

func channelEditOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the edit was successful."),
		"channel":    stringSchema("Channel name."),
		"message_id": stringSchema("ID of the edited message."),
	}, "ok", "channel", "message_id")
}

func channelDeleteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the delete was successful."),
		"channel":    stringSchema("Channel name."),
		"message_id": stringSchema("ID of the deleted message."),
	}, "ok", "channel", "message_id")
}

func channelReactOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the reaction operation was successful."),
		"channel":    stringSchema("Channel name."),
		"message_id": stringSchema("ID of the reacted message."),
		"emoji":      stringSchema("Emoji used for the reaction."),
		"action":     stringSchema("Action performed: added or removed."),
	}, "ok", "channel", "message_id", "emoji", "action")
}

func channelHistoryOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":        stringSchema("Message ID."),
		"sender_id": stringSchema("Sender identifier."),
		"content":   stringSchema("Message content."),
		"timestamp": stringSchema("Message timestamp."),
	}, "id", "sender_id", "content", "timestamp")
	return objectSchema(map[string]any{
		"messages": arraySchema(entry, "History messages."),
		"count":    integerSchema("Number of messages returned."),
	}, "messages", "count")
}

func channelActionOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success": booleanSchema("Whether the action succeeded."),
		"data":    objectSchema(map[string]any{}),
		"error":   stringSchema("Error message if the action failed."),
	}, "success")
}

// channelCapabilitiesSchema returns the JSON Schema for a capabilities object.
func channelCapabilitiesSchema() map[string]any {
	return objectSchema(map[string]any{
		"send_text":       booleanSchema("Can send plain text."),
		"send_rich_text":  booleanSchema("Can send rich text / markdown."),
		"send_file":       booleanSchema("Can send file attachments."),
		"receive_message": booleanSchema("Can receive inbound messages."),
		"receive_event":   booleanSchema("Can receive inbound events."),
	}, "send_text", "send_rich_text", "send_file", "receive_message", "receive_event")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireChannelManager returns an error if the channel manager is not wired.
func requireChannelManager(b *Builtins) error {
	if b.channelManager == nil {
		return fmt.Errorf("channel manager not available")
	}
	return nil
}

// lookupAdapter resolves a named adapter from the channel manager.
func lookupAdapter(b *Builtins, name string) (channels.Adapter, error) {
	if err := requireChannelManager(b); err != nil {
		return nil, err
	}
	adapter, ok := b.channelManager.Get(name)
	if !ok {
		return nil, fmt.Errorf("channel %q not found", name)
	}
	return adapter, nil
}

// capabilitiesMap converts a Capabilities struct to a map for JSON output.
func capabilitiesMap(caps channels.Capabilities) map[string]any {
	return map[string]any{
		"send_text":       caps.SendText,
		"send_rich_text":  caps.SendRichText,
		"send_file":       caps.SendFile,
		"receive_message": caps.ReceiveMessage,
		"receive_event":   caps.ReceiveEvent,
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleChannelList(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.extensions != nil {
		items := b.extensions.Channels()
		entries := make([]map[string]any, 0, len(items))
		for _, item := range items {
			entries = append(entries, map[string]any{
				"name":         item.Name,
				"status":       item.Status,
				"capabilities": capabilitiesMap(item.Capabilities),
			})
		}
		return b.jsonResult(call, map[string]any{
			"channels": entries,
			"count":    len(entries),
		})
	}
	if err := requireChannelManager(b); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.list: %w", err)
	}

	names := b.channelManager.Names()
	sort.Strings(names)

	entries := make([]map[string]any, 0, len(names))
	for _, name := range names {
		adapter, ok := b.channelManager.Get(name)
		if !ok {
			continue
		}
		entries = append(entries, map[string]any{
			"name":         name,
			"status":       string(adapter.Status()),
			"capabilities": capabilitiesMap(adapter.Capabilities()),
		})
	}

	return b.jsonResult(call, map[string]any{
		"channels": entries,
		"count":    len(entries),
	})
}

func handleChannelStatus(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.status: %w", err)
	}
	if b.extensions != nil {
		if item, ok := b.extensions.Channel(name); ok {
			return b.jsonResult(call, map[string]any{
				"name":             item.Name,
				"status":           item.Status,
				"capabilities":     capabilitiesMap(item.Capabilities),
				"supports_edit":    item.CapabilityMatrix.EditMessage,
				"supports_delete":  item.CapabilityMatrix.DeleteMessage,
				"supports_react":   item.CapabilityMatrix.Reactions,
				"supports_history": item.CapabilityMatrix.History,
				"supports_action":  item.SupportsAction,
			})
		}
	}

	adapter, err := lookupAdapter(b, name)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.status: %w", err)
	}

	_, supportsEdit := adapter.(channels.MessageEditor)
	_, supportsDelete := adapter.(channels.MessageDeleter)
	_, supportsReact := adapter.(channels.MessageReactor)
	_, supportsHistory := adapter.(channels.HistoryReader)
	_, supportsAction := adapter.(channels.ActionExecutor)

	return b.jsonResult(call, map[string]any{
		"name":             name,
		"status":           string(adapter.Status()),
		"capabilities":     capabilitiesMap(adapter.Capabilities()),
		"supports_edit":    supportsEdit,
		"supports_delete":  supportsDelete,
		"supports_react":   supportsReact,
		"supports_history": supportsHistory,
		"supports_action":  supportsAction,
	})
}

func handleChannelSend(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.send: %w", err)
	}
	targetID, err := requiredString(call.Input, "target_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.send: %w", err)
	}
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.send: %w", err)
	}
	replyToID, _ := stringFrom(call.Input["reply_to_id"])
	format, _ := stringFrom(call.Input["format"])

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.send: %w", err)
	}

	msg := channels.OutboundMessage{
		TargetID:  targetID,
		ReplyToID: strings.TrimSpace(replyToID),
		Content:   content,
		Format:    strings.TrimSpace(format),
	}

	if err := adapter.Send(ctx, msg); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.send: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":        true,
		"channel":   channelName,
		"target_id": targetID,
	})
}

func handleChannelEdit(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}
	channelID, err := requiredString(call.Input, "channel_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}
	messageID, err := requiredString(call.Input, "message_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}

	editor, ok := adapter.(channels.MessageEditor)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: channel %q does not support editing messages", channelName)
	}

	if err := editor.EditMessage(ctx, channelID, messageID, content); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.edit: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":         true,
		"channel":    channelName,
		"message_id": messageID,
	})
}

func handleChannelDelete(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: %w", err)
	}
	channelID, err := requiredString(call.Input, "channel_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: %w", err)
	}
	messageID, err := requiredString(call.Input, "message_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: %w", err)
	}

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: %w", err)
	}

	deleter, ok := adapter.(channels.MessageDeleter)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: channel %q does not support deleting messages", channelName)
	}

	if err := deleter.DeleteMessage(ctx, channelID, messageID); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.delete: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":         true,
		"channel":    channelName,
		"message_id": messageID,
	})
}

func handleChannelReact(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}
	channelID, err := requiredString(call.Input, "channel_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}
	messageID, err := requiredString(call.Input, "message_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}
	emoji, err := requiredString(call.Input, "emoji")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}
	remove, err := boolFromDefault(call.Input["remove"], false)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
	}

	reactor, ok := adapter.(channels.MessageReactor)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("channel.react: channel %q does not support reactions", channelName)
	}

	action := "added"
	if remove {
		action = "removed"
		if err := reactor.RemoveReaction(ctx, channelID, messageID, emoji); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
		}
	} else {
		if err := reactor.AddReaction(ctx, channelID, messageID, emoji); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("channel.react: %w", err)
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":         true,
		"channel":    channelName,
		"message_id": messageID,
		"emoji":      emoji,
		"action":     action,
	})
}

func handleChannelHistory(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: %w", err)
	}
	channelID, err := requiredString(call.Input, "channel_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: %w", err)
	}
	limit, err := intFrom(call.Input["limit"], defaultHistoryLimit)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: %w", err)
	}
	before, _ := stringFrom(call.Input["before"])

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: %w", err)
	}

	reader, ok := adapter.(channels.HistoryReader)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: channel %q does not support reading history", channelName)
	}

	messages, err := reader.ReadHistory(ctx, channelID, limit, strings.TrimSpace(before))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.history: %w", err)
	}

	entries := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		entries = append(entries, map[string]any{
			"id":        msg.ID,
			"sender_id": msg.SenderID,
			"content":   msg.Content,
			"timestamp": msg.Timestamp,
		})
	}

	return b.jsonResult(call, map[string]any{
		"messages": entries,
		"count":    len(entries),
	})
}

func handleChannelAction(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	channelName, err := requiredString(call.Input, "channel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}
	channelID, err := requiredString(call.Input, "channel_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}
	actionType, err := requiredString(call.Input, "action_type")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}
	params, err := mapFrom(call.Input["params"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}

	adapter, err := lookupAdapter(b, channelName)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}

	executor, ok := adapter.(channels.ActionExecutor)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: channel %q does not support custom actions", channelName)
	}

	action := channels.ChannelAction{
		Type:   actionType,
		Params: params,
	}

	result, err := executor.ExecuteAction(ctx, channelID, action)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("channel.action: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"success": result.Success,
		"data":    result.Data,
		"error":   result.Error,
	})
}
