package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// cronToolDefs returns all 6 cron.* tool definitions.
// ---------------------------------------------------------------------------

func cronToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "cron.list",
				Description:     "List all cron jobs.",
				InputSchema:     cronListInputSchema(),
				OutputSchema:    cronListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "cron:list",
			},
			Handler: handleCronList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "cron.add",
				Description:     "Create a new cron job.",
				InputSchema:     cronAddInputSchema(),
				OutputSchema:    cronAddOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "cron:add:{name}",
			},
			Handler: handleCronAdd,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "cron.update",
				Description:     "Update an existing cron job.",
				InputSchema:     cronUpdateInputSchema(),
				OutputSchema:    cronUpdateOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "cron:update:{id}",
			},
			Handler: handleCronUpdate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "cron.remove",
				Description:     "Remove a cron job.",
				InputSchema:     cronRemoveInputSchema(),
				OutputSchema:    cronRemoveOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "cron:remove:{id}",
			},
			Handler: handleCronRemove,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "cron.run",
				Description:      "Manually trigger a cron job, bypassing its schedule.",
				InputSchema:      cronRunInputSchema(),
				OutputSchema:     cronRunOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "cron:run:{id}",
			},
			Handler: handleCronRun,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "cron.status",
				Description:     "Get detailed status and run history for a cron job.",
				InputSchema:     cronStatusInputSchema(),
				OutputSchema:    cronStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "cron:status:{id}",
			},
			Handler: handleCronStatus,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func cronListInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func cronAddInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Human-readable name for the job.",
			},
			"schedule_kind": map[string]any{
				"type":        "string",
				"description": "Schedule type: cron, every, or at.",
				"enum":        []string{"cron", "every", "at"},
			},
			"schedule_expression": map[string]any{
				"type":        "string",
				"description": "Schedule expression: cron expression, Go duration, or RFC3339 timestamp.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Message content to submit when the job fires.",
			},
			"session_key": map[string]any{
				"type":        "string",
				"description": "Optional session key for the agent run.",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Optional delivery channel adapter name.",
			},
			"account_id": map[string]any{
				"type":        "string",
				"description": "Optional provider account ID for multi-account channels.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Optional delivery target (user/group ID).",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "Optional IANA timezone for the schedule.",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Whether the cron job should start enabled.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override for the triggered run.",
			},
			"automation_id": map[string]any{
				"type":        "string",
				"description": "Optional automation identifier.",
			},
		},
		"required":             []string{"name", "schedule_kind", "schedule_expression", "content"},
		"additionalProperties": false,
	}
}

func cronUpdateInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The job ID to update.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "New name for the job.",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Whether the job is enabled.",
			},
			"schedule_kind": map[string]any{
				"type":        "string",
				"description": "New schedule type: cron, every, or at.",
				"enum":        []string{"cron", "every", "at"},
			},
			"schedule_expression": map[string]any{
				"type":        "string",
				"description": "New schedule expression.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "New message content.",
			},
			"session_key": map[string]any{
				"type":        "string",
				"description": "Updated session key.",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Updated delivery channel adapter name.",
			},
			"account_id": map[string]any{
				"type":        "string",
				"description": "Updated provider account ID for multi-account channels.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Updated delivery target.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Updated model override.",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "New IANA timezone for the schedule.",
			},
			"automation_id": map[string]any{
				"type":        "string",
				"description": "Updated automation identifier.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func cronRemoveInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The job ID to remove.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func cronRunInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The job ID to trigger.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func cronStatusInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The job ID to inspect.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func cronListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":                             stringSchema("Job ID."),
		"name":                           stringSchema("Job name."),
		"enabled":                        booleanSchema("Whether the job is enabled."),
		"schedule":                       stringSchema("Schedule summary."),
		"next_run_at":                    stringSchema("Next run time in RFC3339 format."),
		"last_status":                    stringSchema("Status of the last run."),
		"notification_total_count":       integerSchema("Total successful notifications delivered by this job."),
		"notification_failure_count":     integerSchema("Total failed notification deliveries for this job."),
		"notification_today_count":       integerSchema("Successful notifications delivered today by this job."),
		"notification_today_date":        stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
		"notification_last_attempt_at":   stringSchema("Most recent notification delivery attempt time in RFC3339 format."),
		"notification_last_delivered_at": stringSchema("Most recent successful notification delivery time in RFC3339 format."),
		"notification_last_status":       stringSchema("Latest notification delivery status."),
		"notification_last_error":        stringSchema("Latest notification delivery error."),
		"delivery_channel":               stringSchema("Configured delivery channel."),
		"delivery_account_id":            stringSchema("Configured delivery account ID."),
		"delivery_target":                stringSchema("Configured delivery target."),
	}, "id", "name", "enabled")
	return objectSchema(map[string]any{
		"jobs":  arraySchema(entry, "Cron jobs."),
		"count": integerSchema("Number of cron jobs."),
	}, "jobs", "count")
}

func cronAddOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                  stringSchema("Assigned job ID."),
		"name":                stringSchema("Job name."),
		"schedule":            stringSchema("Schedule summary."),
		"delivery_channel":    stringSchema("Configured delivery channel."),
		"delivery_account_id": stringSchema("Configured delivery account ID."),
		"delivery_target":     stringSchema("Configured delivery target."),
	}, "id", "name", "schedule")
}

func cronUpdateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Job ID."),
		"updated": booleanSchema("Whether the update succeeded."),
	}, "id", "updated")
}

func cronRemoveOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Job ID."),
		"removed": booleanSchema("Whether the removal succeeded."),
	}, "id", "removed")
}

func cronRunOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":        stringSchema("Job ID."),
		"triggered": booleanSchema("Whether the job was triggered."),
	}, "id", "triggered")
}

func cronStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                             stringSchema("Job ID."),
		"name":                           stringSchema("Job name."),
		"enabled":                        booleanSchema("Whether the job is enabled."),
		"schedule":                       stringSchema("Schedule summary."),
		"content":                        stringSchema("Payload content."),
		"session_key":                    stringSchema("Session key."),
		"model":                          stringSchema("Model override."),
		"delivery_channel":               stringSchema("Configured delivery channel."),
		"delivery_account_id":            stringSchema("Configured delivery account ID."),
		"delivery_target":                stringSchema("Configured delivery target."),
		"last_run_at":                    stringSchema("Last run time in RFC3339 format."),
		"next_run_at":                    stringSchema("Next run time in RFC3339 format."),
		"last_status":                    stringSchema("Status of the last run."),
		"last_error":                     stringSchema("Error from the last run."),
		"notification_total_count":       integerSchema("Total successful notifications delivered by this job."),
		"notification_failure_count":     integerSchema("Total failed notification deliveries for this job."),
		"notification_today_count":       integerSchema("Successful notifications delivered today by this job."),
		"notification_today_date":        stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
		"notification_last_attempt_at":   stringSchema("Most recent notification delivery attempt time in RFC3339 format."),
		"notification_last_delivered_at": stringSchema("Most recent successful notification delivery time in RFC3339 format."),
		"notification_last_status":       stringSchema("Latest notification delivery status."),
		"notification_last_error":        stringSchema("Latest notification delivery error."),
		"created_at":                     stringSchema("Creation time in RFC3339 format."),
		"updated_at":                     stringSchema("Last update time in RFC3339 format."),
	}, "id", "name", "enabled", "schedule")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func cronScheduleSummary(sched cronsvc.Schedule) string {
	switch sched.Kind {
	case cronsvc.ScheduleKindCron:
		return "cron: " + sched.Expression
	case cronsvc.ScheduleKindEvery:
		return "every " + sched.Every
	case cronsvc.ScheduleKindAt:
		return "at " + sched.At
	default:
		return sched.Kind
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleCronList(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.list: cron service not available")
	}

	jobs := b.cronService.Store().List()
	entries := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		entries = append(entries, map[string]any{
			"id":                             job.ID,
			"name":                           job.Name,
			"enabled":                        job.Enabled,
			"schedule":                       cronScheduleSummary(job.Schedule),
			"delivery_channel":               cronDeliveryChannel(job.Delivery),
			"delivery_account_id":            cronDeliveryAccountID(job.Delivery),
			"delivery_target":                cronDeliveryTarget(job.Delivery),
			"next_run_at":                    formatTimeOrEmpty(job.NextRunAt),
			"last_status":                    job.LastStatus,
			"notification_total_count":       job.Notifications.TotalCount,
			"notification_failure_count":     job.Notifications.FailureCount,
			"notification_today_count":       job.Notifications.TodayCount,
			"notification_today_date":        job.Notifications.TodayDate,
			"notification_last_attempt_at":   formatTimeOrEmpty(job.Notifications.LastAttemptAt),
			"notification_last_delivered_at": formatTimeOrEmpty(job.Notifications.LastDeliveredAt),
			"notification_last_status":       job.Notifications.LastStatus,
			"notification_last_error":        job.Notifications.LastError,
		})
	}

	return b.jsonResult(call, map[string]any{
		"jobs":  entries,
		"count": len(entries),
	})
}

func handleCronAdd(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: cron service not available")
	}

	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}
	schedKind, err := requiredString(call.Input, "schedule_kind")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}
	schedExpr, err := requiredString(call.Input, "schedule_expression")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}

	sessionKey, _ := stringFrom(call.Input["session_key"])
	channel, _ := stringFrom(call.Input["channel"])
	accountID, _ := stringFrom(call.Input["account_id"])
	target, _ := stringFrom(call.Input["target"])
	timezone, _ := stringFrom(call.Input["timezone"])
	model, _ := stringFrom(call.Input["model"])
	automationID, _ := stringFrom(call.Input["automation_id"])
	enabled := true
	if call.Input["enabled"] != nil {
		enabled, err = boolFrom(call.Input["enabled"])
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("cron.add: enabled: %w", err)
		}
	}

	sched := cronsvc.Schedule{
		Kind:     schedKind,
		Timezone: timezone,
	}
	switch schedKind {
	case cronsvc.ScheduleKindCron:
		sched.Expression = schedExpr
	case cronsvc.ScheduleKindEvery:
		sched.Every = schedExpr
	case cronsvc.ScheduleKindAt:
		sched.At = schedExpr
	}
	if enabled {
		if _, err := cronsvc.NextRunTime(sched, time.Now().UTC()); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
		}
	}

	now := time.Now().UTC()
	id := fmt.Sprintf("cron-%d", now.UnixNano())

	delivery := buildCronDelivery(strings.TrimSpace(channel), strings.TrimSpace(accountID), strings.TrimSpace(target))

	job := cronsvc.Job{
		ID:           id,
		Name:         name,
		Enabled:      enabled,
		Schedule:     sched,
		Payload:      cronsvc.Payload{Content: content},
		Delivery:     delivery,
		SessionKey:   sessionKey,
		Model:        strings.TrimSpace(model),
		AutomationID: strings.TrimSpace(automationID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if enabled {
		if nextRun, nextErr := cronsvc.NextRunTime(sched, now); nextErr == nil {
			job.NextRunAt = nextRun
		}
	}

	store := b.cronService.Store()
	if err := store.Add(job); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}
	if err := store.Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.add: %w", err)
	}
	b.cronService.Rearm()

	return b.jsonResult(call, map[string]any{
		"id":                  id,
		"name":                name,
		"schedule":            cronScheduleSummary(sched),
		"delivery_channel":    cronDeliveryChannel(delivery),
		"delivery_account_id": cronDeliveryAccountID(delivery),
		"delivery_target":     cronDeliveryTarget(delivery),
	})
}

func handleCronUpdate(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.update: cron service not available")
	}

	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.update: %w", err)
	}

	current, err := b.cronService.Store().Get(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.update: %w", err)
	}
	updated := *current
	now := time.Now().UTC()

	if call.Input["name"] != nil {
		updated.Name = optionalString(call.Input, "name")
	}
	if call.Input["enabled"] != nil {
		updated.Enabled, err = boolFrom(call.Input["enabled"])
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("cron.update: enabled: %w", err)
		}
	}
	if call.Input["schedule_kind"] != nil {
		updated.Schedule.Kind = optionalString(call.Input, "schedule_kind")
	}
	if call.Input["schedule_expression"] != nil {
		expr := optionalString(call.Input, "schedule_expression")
		updated.Schedule.At = ""
		updated.Schedule.Every = ""
		updated.Schedule.Expression = ""
		switch updated.Schedule.Kind {
		case cronsvc.ScheduleKindCron:
			updated.Schedule.Expression = expr
		case cronsvc.ScheduleKindEvery:
			updated.Schedule.Every = expr
		case cronsvc.ScheduleKindAt:
			updated.Schedule.At = expr
		}
	}
	if call.Input["content"] != nil {
		updated.Payload.Content = optionalString(call.Input, "content")
	}
	if call.Input["session_key"] != nil {
		updated.SessionKey = optionalString(call.Input, "session_key")
	}
	if call.Input["model"] != nil {
		updated.Model = optionalString(call.Input, "model")
	}
	if call.Input["timezone"] != nil {
		updated.Schedule.Timezone = optionalString(call.Input, "timezone")
	}
	if call.Input["channel"] != nil || call.Input["account_id"] != nil || call.Input["target"] != nil {
		channel := cronDeliveryChannel(updated.Delivery)
		accountID := cronDeliveryAccountID(updated.Delivery)
		target := cronDeliveryTarget(updated.Delivery)
		if call.Input["channel"] != nil {
			channel = optionalString(call.Input, "channel")
		}
		if call.Input["account_id"] != nil {
			accountID = optionalString(call.Input, "account_id")
		}
		if call.Input["target"] != nil {
			target = optionalString(call.Input, "target")
		}
		updated.Delivery = buildCronDelivery(channel, accountID, target)
	}
	if call.Input["automation_id"] != nil {
		updated.AutomationID = optionalString(call.Input, "automation_id")
	}
	updated.UpdatedAt = now
	if updated.Enabled {
		nextRun, nextErr := cronsvc.NextRunTime(updated.Schedule, now)
		if nextErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("cron.update: %w", nextErr)
		}
		updated.NextRunAt = nextRun
	} else {
		updated.NextRunAt = time.Time{}
	}
	if err := b.cronService.Store().Update(id, func(job *cronsvc.Job) {
		*job = updated
	}); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.update: %w", err)
	}

	if err := b.cronService.Store().Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.update: %w", err)
	}
	b.cronService.Rearm()

	return b.jsonResult(call, map[string]any{
		"id":      id,
		"updated": true,
	})
}

func handleCronRemove(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.remove: cron service not available")
	}

	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.remove: %w", err)
	}

	store := b.cronService.Store()
	if err := store.Remove(id); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.remove: %w", err)
	}
	if err := store.Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.remove: %w", err)
	}
	b.cronService.Rearm()

	return b.jsonResult(call, map[string]any{
		"id":      id,
		"removed": true,
	})
}

func handleCronRun(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.run: cron service not available")
	}

	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.run: %w", err)
	}

	if err := b.cronService.TriggerJob(ctx, id); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.run: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"id":        id,
		"triggered": true,
	})
}

func handleCronStatus(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.cronService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.status: cron service not available")
	}

	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.status: %w", err)
	}

	job, err := b.cronService.Store().Get(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("cron.status: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"id":                             job.ID,
		"name":                           job.Name,
		"enabled":                        job.Enabled,
		"schedule":                       cronScheduleSummary(job.Schedule),
		"content":                        job.Payload.Content,
		"session_key":                    job.SessionKey,
		"model":                          job.Model,
		"delivery_channel":               cronDeliveryChannel(job.Delivery),
		"delivery_account_id":            cronDeliveryAccountID(job.Delivery),
		"delivery_target":                cronDeliveryTarget(job.Delivery),
		"last_run_at":                    formatTimeOrEmpty(job.LastRunAt),
		"next_run_at":                    formatTimeOrEmpty(job.NextRunAt),
		"last_status":                    job.LastStatus,
		"last_error":                     job.LastError,
		"notification_total_count":       job.Notifications.TotalCount,
		"notification_failure_count":     job.Notifications.FailureCount,
		"notification_today_count":       job.Notifications.TodayCount,
		"notification_today_date":        job.Notifications.TodayDate,
		"notification_last_attempt_at":   formatTimeOrEmpty(job.Notifications.LastAttemptAt),
		"notification_last_delivered_at": formatTimeOrEmpty(job.Notifications.LastDeliveredAt),
		"notification_last_status":       job.Notifications.LastStatus,
		"notification_last_error":        job.Notifications.LastError,
		"created_at":                     formatTimeOrEmpty(job.CreatedAt),
		"updated_at":                     formatTimeOrEmpty(job.UpdatedAt),
	})
}

func buildCronDelivery(channel, accountID, target string) *cronsvc.Delivery {
	channel = strings.TrimSpace(channel)
	accountID = strings.TrimSpace(accountID)
	target = strings.TrimSpace(target)
	if channel == "" && accountID == "" && target == "" {
		return nil
	}
	return &cronsvc.Delivery{
		Kind:       "channel",
		Provider:   strings.ToLower(channel),
		Channel:    channel,
		AccountID:  accountID,
		TargetType: destinationTargetType(channel),
		Target:     target,
	}
}

func cronDeliveryChannel(delivery *cronsvc.Delivery) string {
	if delivery == nil {
		return ""
	}
	return strings.TrimSpace(delivery.Channel)
}

func cronDeliveryAccountID(delivery *cronsvc.Delivery) string {
	if delivery == nil {
		return ""
	}
	return strings.TrimSpace(delivery.AccountID)
}

func cronDeliveryTarget(delivery *cronsvc.Delivery) string {
	if delivery == nil {
		return ""
	}
	return strings.TrimSpace(delivery.Target)
}
