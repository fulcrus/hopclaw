package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
	wakeupsvc "github.com/fulcrus/hopclaw/wakeup"
)

func wakeupToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "wakeup.list",
				Description:     "List all wakeup triggers.",
				InputSchema:     wakeupListInputSchema(),
				OutputSchema:    wakeupListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "wakeup:list",
			},
			Handler: handleWakeupList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "wakeup.add",
				Description:     "Create a new wakeup trigger.",
				InputSchema:     wakeupAddInputSchema(),
				OutputSchema:    wakeupAddOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "wakeup:add:{name}",
			},
			Handler: handleWakeupAdd,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "wakeup.update",
				Description:     "Update an existing wakeup trigger.",
				InputSchema:     wakeupUpdateInputSchema(),
				OutputSchema:    wakeupUpdateOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "wakeup:update:{id}",
			},
			Handler: handleWakeupUpdate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "wakeup.remove",
				Description:     "Remove a wakeup trigger.",
				InputSchema:     wakeupRemoveInputSchema(),
				OutputSchema:    wakeupRemoveOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "wakeup:remove:{id}",
			},
			Handler: handleWakeupRemove,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "wakeup.status",
				Description:     "Get detailed status for a wakeup trigger.",
				InputSchema:     wakeupStatusInputSchema(),
				OutputSchema:    wakeupStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "wakeup:status:{id}",
			},
			Handler: handleWakeupStatus,
		},
	}
}

func wakeupListInputSchema() map[string]any {
	return objectSchema(map[string]any{}, []string{}...)
}

func wakeupAddInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":          stringSchema("Human-readable wakeup name."),
		"schedule":      stringSchema("Cron expression or every-duration syntax like `every 1h`."),
		"channel":       stringSchema("Optional originating channel name."),
		"session_key":   stringSchema("Optional session key for the submitted run."),
		"message":       stringSchema("Message content submitted when the trigger fires."),
		"model":         stringSchema("Optional model override."),
		"enabled":       booleanSchema("Whether the wakeup trigger is enabled."),
		"timezone":      stringSchema("Optional IANA timezone."),
		"automation_id": stringSchema("Optional automation identifier."),
	}, "name", "schedule", "message")
}

func wakeupUpdateInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":            stringSchema("Wakeup trigger ID."),
		"name":          stringSchema("Updated wakeup name."),
		"schedule":      stringSchema("Updated cron expression or every-duration syntax."),
		"channel":       stringSchema("Updated originating channel name."),
		"session_key":   stringSchema("Updated session key."),
		"message":       stringSchema("Updated message content."),
		"model":         stringSchema("Updated model override."),
		"enabled":       booleanSchema("Whether the wakeup trigger is enabled."),
		"timezone":      stringSchema("Updated IANA timezone."),
		"automation_id": stringSchema("Updated automation identifier."),
	}, "id")
}

func wakeupRemoveInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id": stringSchema("Wakeup trigger ID."),
	}, "id")
}

func wakeupStatusInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id": stringSchema("Wakeup trigger ID."),
	}, "id")
}

func wakeupListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":                        stringSchema("Wakeup trigger ID."),
		"name":                      stringSchema("Wakeup trigger name."),
		"enabled":                   booleanSchema("Whether the trigger is enabled."),
		"schedule":                  stringSchema("Schedule summary."),
		"channel":                   stringSchema("Originating channel."),
		"session_key":               stringSchema("Session key."),
		"message":                   stringSchema("Wakeup message."),
		"model":                     stringSchema("Model override."),
		"timezone":                  stringSchema("IANA timezone."),
		"automation_id":             stringSchema("Automation identifier."),
		"next_run_at":               stringSchema("Next run time in RFC3339 format."),
		"last_run_at":               stringSchema("Last run time in RFC3339 format."),
		"last_run_id":               stringSchema("Last run ID."),
		"last_status":               stringSchema("Last execution status."),
		"last_error":                stringSchema("Last execution error."),
		"last_summary":              stringSchema("Last execution summary."),
		"last_verification_status":  stringSchema("Last verification status."),
		"last_verification_summary": stringSchema("Last verification summary."),
		"created_at":                stringSchema("Creation time in RFC3339 format."),
	}, "id", "name", "enabled", "schedule", "message")
	return objectSchema(map[string]any{
		"triggers": arraySchema(entry, "Wakeup triggers."),
		"count":    integerSchema("Number of wakeup triggers."),
		"running":  booleanSchema("Whether the wakeup service is running."),
	}, "triggers", "count", "running")
}

func wakeupAddOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":       stringSchema("Assigned wakeup trigger ID."),
		"name":     stringSchema("Wakeup trigger name."),
		"schedule": stringSchema("Schedule summary."),
	}, "id", "name", "schedule")
}

func wakeupUpdateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Wakeup trigger ID."),
		"updated": booleanSchema("Whether the update succeeded."),
	}, "id", "updated")
}

func wakeupRemoveOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Wakeup trigger ID."),
		"removed": booleanSchema("Whether the removal succeeded."),
	}, "id", "removed")
}

func wakeupStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                        stringSchema("Wakeup trigger ID."),
		"name":                      stringSchema("Wakeup trigger name."),
		"enabled":                   booleanSchema("Whether the trigger is enabled."),
		"schedule":                  stringSchema("Schedule summary."),
		"channel":                   stringSchema("Originating channel."),
		"session_key":               stringSchema("Session key."),
		"message":                   stringSchema("Wakeup message."),
		"model":                     stringSchema("Model override."),
		"timezone":                  stringSchema("IANA timezone."),
		"automation_id":             stringSchema("Automation identifier."),
		"next_run_at":               stringSchema("Next run time in RFC3339 format."),
		"last_run_at":               stringSchema("Last run time in RFC3339 format."),
		"last_run_id":               stringSchema("Last run ID."),
		"last_status":               stringSchema("Last execution status."),
		"last_error":                stringSchema("Last execution error."),
		"last_summary":              stringSchema("Last execution summary."),
		"last_verification_status":  stringSchema("Last verification status."),
		"last_verification_summary": stringSchema("Last verification summary."),
		"created_at":                stringSchema("Creation time in RFC3339 format."),
	}, "id", "name", "enabled", "schedule", "message")
}

func handleWakeupList(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.wakeupService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.list: wakeup service not available")
	}
	triggers := b.wakeupService.List()
	items := make([]map[string]any, 0, len(triggers))
	for _, trigger := range triggers {
		items = append(items, wakeupJSON(trigger))
	}
	return b.jsonResult(call, map[string]any{
		"triggers": items,
		"count":    len(items),
		"running":  b.wakeupService.IsRunning(),
	})
}

func handleWakeupAdd(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.wakeupService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: wakeup service not available")
	}
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: %w", err)
	}
	schedule, err := requiredString(call.Input, "schedule")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: %w", err)
	}
	message, err := requiredString(call.Input, "message")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: %w", err)
	}
	now := time.Now().UTC()
	trigger := wakeupsvc.Trigger{
		ID:           fmt.Sprintf("wakeup-%d", now.UnixNano()),
		Name:         strings.TrimSpace(name),
		Schedule:     strings.TrimSpace(schedule),
		Channel:      optionalString(call.Input, "channel"),
		SessionKey:   optionalString(call.Input, "session_key"),
		Message:      strings.TrimSpace(message),
		Model:        optionalString(call.Input, "model"),
		Enabled:      true,
		Timezone:     optionalString(call.Input, "timezone"),
		AutomationID: optionalString(call.Input, "automation_id"),
		CreatedAt:    now,
	}
	if call.Input["enabled"] != nil {
		enabled, boolErr := boolFrom(call.Input["enabled"])
		if boolErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: enabled: %w", boolErr)
		}
		trigger.Enabled = enabled
	}
	if err := b.wakeupService.Add(trigger); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.add: %w", err)
	}
	return b.jsonResult(call, map[string]any{
		"id":       trigger.ID,
		"name":     trigger.Name,
		"schedule": trigger.Schedule,
	})
}

func handleWakeupUpdate(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.wakeupService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.update: wakeup service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.update: %w", err)
	}
	if err := b.wakeupService.Update(id, func(trigger *wakeupsvc.Trigger) {
		if call.Input["name"] != nil {
			trigger.Name = optionalString(call.Input, "name")
		}
		if call.Input["schedule"] != nil {
			trigger.Schedule = optionalString(call.Input, "schedule")
		}
		if call.Input["channel"] != nil {
			trigger.Channel = optionalString(call.Input, "channel")
		}
		if call.Input["session_key"] != nil {
			trigger.SessionKey = optionalString(call.Input, "session_key")
		}
		if call.Input["message"] != nil {
			trigger.Message = optionalString(call.Input, "message")
		}
		if call.Input["model"] != nil {
			trigger.Model = optionalString(call.Input, "model")
		}
		if call.Input["enabled"] != nil {
			if enabled, boolErr := boolFrom(call.Input["enabled"]); boolErr == nil {
				trigger.Enabled = enabled
			}
		}
		if call.Input["timezone"] != nil {
			trigger.Timezone = optionalString(call.Input, "timezone")
		}
		if call.Input["automation_id"] != nil {
			trigger.AutomationID = optionalString(call.Input, "automation_id")
		}
	}); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.update: %w", err)
	}
	return b.jsonResult(call, map[string]any{"id": id, "updated": true})
}

func handleWakeupRemove(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.wakeupService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.remove: wakeup service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.remove: %w", err)
	}
	if err := b.wakeupService.Remove(id); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.remove: %w", err)
	}
	return b.jsonResult(call, map[string]any{"id": id, "removed": true})
}

func handleWakeupStatus(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.wakeupService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.status: wakeup service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.status: %w", err)
	}
	trigger, err := b.wakeupService.Get(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("wakeup.status: %w", err)
	}
	return b.jsonResult(call, wakeupJSON(*trigger))
}

func wakeupJSON(trigger wakeupsvc.Trigger) map[string]any {
	return map[string]any{
		"id":                        trigger.ID,
		"name":                      trigger.Name,
		"enabled":                   trigger.Enabled,
		"schedule":                  strings.TrimSpace(trigger.Schedule),
		"channel":                   strings.TrimSpace(trigger.Channel),
		"session_key":               strings.TrimSpace(trigger.SessionKey),
		"message":                   strings.TrimSpace(trigger.Message),
		"model":                     strings.TrimSpace(trigger.Model),
		"timezone":                  strings.TrimSpace(trigger.Timezone),
		"automation_id":             strings.TrimSpace(trigger.AutomationID),
		"next_run_at":               formatTimeOrEmpty(trigger.NextRunAt),
		"last_run_at":               formatTimeOrEmpty(trigger.LastRunAt),
		"last_run_id":               strings.TrimSpace(trigger.LastRunID),
		"last_status":               strings.TrimSpace(trigger.LastStatus),
		"last_error":                strings.TrimSpace(trigger.LastError),
		"last_summary":              strings.TrimSpace(trigger.LastSummary),
		"last_verification_status":  strings.TrimSpace(trigger.LastVerificationStatus),
		"last_verification_summary": strings.TrimSpace(trigger.LastVerificationSummary),
		"created_at":                formatTimeOrEmpty(trigger.CreatedAt),
	}
}
