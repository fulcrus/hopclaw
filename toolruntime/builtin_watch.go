package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
	watchsvc "github.com/fulcrus/hopclaw/watch"
)

func watchToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "watch.list",
				Description:     "List all watch jobs.",
				InputSchema:     watchListInputSchema(),
				OutputSchema:    watchListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "watch:list",
			},
			Handler: handleWatchList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "watch.add",
				Description:     "Create a new watch job that monitors a source and triggers a run when it changes.",
				InputSchema:     watchAddInputSchema(),
				OutputSchema:    watchAddOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "watch:add:{name}",
			},
			Handler: handleWatchAdd,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "watch.update",
				Description:     "Update an existing watch job.",
				InputSchema:     watchUpdateInputSchema(),
				OutputSchema:    watchUpdateOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "watch:update:{id}",
			},
			Handler: handleWatchUpdate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "watch.remove",
				Description:     "Remove a watch job.",
				InputSchema:     watchRemoveInputSchema(),
				OutputSchema:    watchRemoveOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "watch:remove:{id}",
			},
			Handler: handleWatchRemove,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "watch.run",
				Description:      "Run a watch job immediately, bypassing its schedule.",
				InputSchema:      watchRunInputSchema(),
				OutputSchema:     watchRunOutputSchema(),
				SideEffectClass:  "remote_write",
				RequiresApproval: true,
				ExecutionKey:     "watch:run:{id}",
			},
			Handler: handleWatchRun,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "watch.status",
				Description:     "Get detailed status for a watch job.",
				InputSchema:     watchStatusInputSchema(),
				OutputSchema:    watchStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "watch:status:{id}",
			},
			Handler: handleWatchStatus,
		},
	}
}

func watchListInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func watchAddInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":                stringSchema("Human-readable watch name."),
			"enabled":             booleanSchema("Whether the watch is enabled."),
			"interval":            stringSchema("Polling interval as a Go duration, for example 5m or 1h."),
			"source_kind":         stringSchema("Source type: http, file, feed, mailbox, browser_snapshot, calendar, webhook, or structured_app_inbox."),
			"source_url":          stringSchema("HTTP/feed/browser snapshot URL to poll."),
			"source_path":         stringSchema("Local file path for file watches."),
			"source_session_key":  stringSchema("Session key for inbox-backed watches."),
			"calendar_query":      stringSchema("Optional calendar query for calendar watches."),
			"mailbox_folder":      stringSchema("Mailbox folder for mailbox watches."),
			"mailbox_query":       stringSchema("Optional mailbox search query."),
			"mailbox_limit":       integerSchema("Optional mailbox message limit."),
			"webhook_id":          stringSchema("Webhook adapter ID for webhook inbox watches."),
			"webhook_sender_id":   stringSchema("Webhook sender ID within the webhook inbox."),
			"inbox_limit":         integerSchema("Optional event/message limit for inbox-backed watches."),
			"prompt":              stringSchema("Optional prompt used when a change is detected."),
			"session_key":         stringSchema("Optional session key for triggered runs."),
			"model":               stringSchema("Optional model override for triggered runs."),
			"delivery_channel":    stringSchema("Optional channel adapter used for outbound notifications."),
			"delivery_account_id": stringSchema("Optional provider account ID for multi-account channels."),
			"delivery_target":     stringSchema("Optional channel-specific target (user, thread, or group ID)."),
			"fire_on_start":       booleanSchema("Whether the first observation should immediately trigger a run."),
			"automation_id":       stringSchema("Optional automation identifier."),
		},
		"required":             []string{"name", "interval", "source_kind"},
		"additionalProperties": false,
	}
}

func watchUpdateInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":                  stringSchema("Watch job ID."),
			"name":                stringSchema("Updated watch name."),
			"enabled":             booleanSchema("Whether the watch is enabled."),
			"interval":            stringSchema("Updated polling interval."),
			"source_kind":         stringSchema("Updated source kind."),
			"source_url":          stringSchema("Updated source URL."),
			"source_path":         stringSchema("Updated source file path."),
			"source_session_key":  stringSchema("Updated session key for inbox-backed watches."),
			"calendar_query":      stringSchema("Updated calendar query."),
			"mailbox_folder":      stringSchema("Updated mailbox folder."),
			"mailbox_query":       stringSchema("Updated mailbox query."),
			"mailbox_limit":       integerSchema("Updated mailbox limit."),
			"webhook_id":          stringSchema("Updated webhook adapter ID."),
			"webhook_sender_id":   stringSchema("Updated webhook sender ID."),
			"inbox_limit":         integerSchema("Updated inbox event limit."),
			"prompt":              stringSchema("Updated prompt."),
			"session_key":         stringSchema("Updated session key."),
			"model":               stringSchema("Updated model override."),
			"delivery_channel":    stringSchema("Updated delivery channel adapter."),
			"delivery_account_id": stringSchema("Updated provider account ID for multi-account channels."),
			"delivery_target":     stringSchema("Updated delivery target."),
			"fire_on_start":       booleanSchema("Updated fire-on-start behavior."),
			"automation_id":       stringSchema("Updated automation identifier."),
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func watchRemoveInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id": stringSchema("Watch job ID."),
	}, "id")
}

func watchRunInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id": stringSchema("Watch job ID."),
	}, "id")
}

func watchStatusInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id": stringSchema("Watch job ID."),
	}, "id")
}

func watchListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":                             stringSchema("Watch job ID."),
		"name":                           stringSchema("Watch name."),
		"enabled":                        booleanSchema("Whether the watch is enabled."),
		"interval":                       stringSchema("Polling interval."),
		"source_kind":                    stringSchema("Source type."),
		"source_url":                     stringSchema("Source URL."),
		"source_path":                    stringSchema("Source path."),
		"source_session_key":             stringSchema("Source session key."),
		"calendar_query":                 stringSchema("Calendar query."),
		"mailbox_folder":                 stringSchema("Mailbox folder."),
		"mailbox_query":                  stringSchema("Mailbox query."),
		"mailbox_limit":                  integerSchema("Mailbox message limit."),
		"webhook_id":                     stringSchema("Webhook adapter ID."),
		"webhook_sender_id":              stringSchema("Webhook sender ID."),
		"inbox_limit":                    integerSchema("Inbox event limit."),
		"prompt":                         stringSchema("Prompt used for triggered runs."),
		"session_key":                    stringSchema("Session key."),
		"model":                          stringSchema("Model override."),
		"delivery_channel":               stringSchema("Configured delivery channel."),
		"delivery_account_id":            stringSchema("Configured delivery account ID."),
		"delivery_target":                stringSchema("Configured delivery target."),
		"notification_total_count":       integerSchema("Total successful notifications delivered by this watch."),
		"notification_failure_count":     integerSchema("Total failed notification deliveries for this watch."),
		"notification_today_count":       integerSchema("Successful notifications delivered today by this watch."),
		"notification_today_date":        stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
		"notification_last_attempt_at":   stringSchema("Most recent notification delivery attempt time in RFC3339 format."),
		"notification_last_delivered_at": stringSchema("Most recent successful notification delivery time in RFC3339 format."),
		"notification_last_status":       stringSchema("Latest notification delivery status."),
		"notification_last_error":        stringSchema("Latest notification delivery error."),
		"fire_on_start":                  booleanSchema("Whether the first check should trigger a run."),
		"last_run_id":                    stringSchema("Last triggered run ID."),
		"last_status":                    stringSchema("Status of the last check."),
		"last_error":                     stringSchema("Error from the last check."),
		"last_summary":                   stringSchema("Summary from the last check."),
		"last_verification_status":       stringSchema("Verification status of the last triggered run."),
		"last_verification_summary":      stringSchema("Verification summary of the last triggered run."),
		"last_checked_at":                stringSchema("Time of the last check in RFC3339 format."),
		"last_triggered_at":              stringSchema("Time of the last trigger in RFC3339 format."),
		"next_check_at":                  stringSchema("Time of the next check in RFC3339 format."),
		"created_at":                     stringSchema("Creation time in RFC3339 format."),
		"updated_at":                     stringSchema("Last update time in RFC3339 format."),
	}, "id", "name", "enabled", "interval", "source_kind")
	return objectSchema(map[string]any{
		"watches": arraySchema(entry, "Watch jobs."),
		"count":   integerSchema("Number of watch jobs."),
	}, "watches", "count")
}

func watchAddOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                  stringSchema("Assigned watch job ID."),
		"name":                stringSchema("Watch name."),
		"interval":            stringSchema("Polling interval."),
		"source_kind":         stringSchema("Source type."),
		"source_url":          stringSchema("Source URL."),
		"source_path":         stringSchema("Source path."),
		"source_session_key":  stringSchema("Source session key."),
		"calendar_query":      stringSchema("Calendar query."),
		"webhook_id":          stringSchema("Webhook adapter ID."),
		"webhook_sender_id":   stringSchema("Webhook sender ID."),
		"inbox_limit":         integerSchema("Inbox event limit."),
		"delivery_channel":    stringSchema("Configured delivery channel."),
		"delivery_account_id": stringSchema("Configured delivery account ID."),
		"delivery_target":     stringSchema("Configured delivery target."),
	}, "id", "name", "interval", "source_kind", "source_url")
}

func watchUpdateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Watch job ID."),
		"updated": booleanSchema("Whether the update succeeded."),
	}, "id", "updated")
}

func watchRemoveOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Watch job ID."),
		"removed": booleanSchema("Whether the removal succeeded."),
	}, "id", "removed")
}

func watchRunOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":        stringSchema("Watch job ID."),
		"triggered": booleanSchema("Whether the watch job was triggered."),
	}, "id", "triggered")
}

func watchStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":                             stringSchema("Watch job ID."),
		"name":                           stringSchema("Watch name."),
		"enabled":                        booleanSchema("Whether the watch is enabled."),
		"interval":                       stringSchema("Polling interval."),
		"source_kind":                    stringSchema("Source type."),
		"source_url":                     stringSchema("Source URL."),
		"source_path":                    stringSchema("Source path."),
		"source_session_key":             stringSchema("Source session key."),
		"calendar_query":                 stringSchema("Calendar query."),
		"mailbox_folder":                 stringSchema("Mailbox folder."),
		"mailbox_query":                  stringSchema("Mailbox query."),
		"mailbox_limit":                  integerSchema("Mailbox message limit."),
		"webhook_id":                     stringSchema("Webhook adapter ID."),
		"webhook_sender_id":              stringSchema("Webhook sender ID."),
		"inbox_limit":                    integerSchema("Inbox event limit."),
		"prompt":                         stringSchema("Prompt used for triggered runs."),
		"session_key":                    stringSchema("Session key."),
		"model":                          stringSchema("Model override."),
		"delivery_channel":               stringSchema("Configured delivery channel."),
		"delivery_account_id":            stringSchema("Configured delivery account ID."),
		"delivery_target":                stringSchema("Configured delivery target."),
		"notification_total_count":       integerSchema("Total successful notifications delivered by this watch."),
		"notification_failure_count":     integerSchema("Total failed notification deliveries for this watch."),
		"notification_today_count":       integerSchema("Successful notifications delivered today by this watch."),
		"notification_today_date":        stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
		"notification_last_attempt_at":   stringSchema("Most recent notification delivery attempt time in RFC3339 format."),
		"notification_last_delivered_at": stringSchema("Most recent successful notification delivery time in RFC3339 format."),
		"notification_last_status":       stringSchema("Latest notification delivery status."),
		"notification_last_error":        stringSchema("Latest notification delivery error."),
		"fire_on_start":                  booleanSchema("Whether the first check should trigger a run."),
		"last_run_id":                    stringSchema("Last triggered run ID."),
		"last_status":                    stringSchema("Status of the last check."),
		"last_error":                     stringSchema("Error from the last check."),
		"last_summary":                   stringSchema("Summary from the last check."),
		"last_verification_status":       stringSchema("Verification status of the last triggered run."),
		"last_verification_summary":      stringSchema("Verification summary of the last triggered run."),
		"last_checked_at":                stringSchema("Time of the last check in RFC3339 format."),
		"last_triggered_at":              stringSchema("Time of the last trigger in RFC3339 format."),
		"next_check_at":                  stringSchema("Time of the next check in RFC3339 format."),
		"created_at":                     stringSchema("Creation time in RFC3339 format."),
		"updated_at":                     stringSchema("Last update time in RFC3339 format."),
	}, "id", "name", "enabled", "interval", "source_kind")
}

func handleWatchList(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.list: watch service not available")
	}
	items := b.watchService.Store().List()
	entries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entries = append(entries, watchJSON(item))
	}
	return b.jsonResult(call, map[string]any{
		"watches": entries,
		"count":   len(entries),
	})
}

func handleWatchAdd(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: watch service not available")
	}
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	interval, err := requiredString(call.Input, "interval")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	sourceKind, err := requiredString(call.Input, "source_kind")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	enabled := true
	if call.Input["enabled"] != nil {
		enabled, err = boolFrom(call.Input["enabled"])
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("watch.add: enabled: %w", err)
		}
	}
	fireOnStart := false
	if call.Input["fire_on_start"] != nil {
		fireOnStart, err = boolFrom(call.Input["fire_on_start"])
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("watch.add: fire_on_start: %w", err)
		}
	}

	now := time.Now().UTC()
	item := watchsvc.Watch{
		ID:           fmt.Sprintf("watch-%d", now.UnixNano()),
		Name:         strings.TrimSpace(name),
		Enabled:      enabled,
		Interval:     strings.TrimSpace(interval),
		Source:       buildWatchSource(call.Input, strings.TrimSpace(sourceKind), watchsvc.Source{}),
		Delivery:     buildWatchDelivery(call.Input, nil),
		Prompt:       optionalString(call.Input, "prompt"),
		SessionKey:   optionalString(call.Input, "session_key"),
		Model:        optionalString(call.Input, "model"),
		AutomationID: optionalString(call.Input, "automation_id"),
		FireOnStart:  fireOnStart,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if item.Enabled {
		item.NextCheckAt = now
	}
	if err := watchsvc.Validate(item); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	store := b.watchService.Store()
	if err := store.Add(item); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	if err := store.Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.add: %w", err)
	}
	b.watchService.Rearm()
	return b.jsonResult(call, map[string]any{
		"id":                  item.ID,
		"name":                item.Name,
		"interval":            item.Interval,
		"source_kind":         item.Source.Kind,
		"source_url":          watchSourceURL(item),
		"source_path":         watchSourcePath(item),
		"source_session_key":  watchSourceSessionKey(item),
		"calendar_query":      watchCalendarQuery(item),
		"webhook_id":          watchWebhookID(item),
		"webhook_sender_id":   watchWebhookSenderID(item),
		"inbox_limit":         watchInboxLimit(item),
		"delivery_channel":    watchDeliveryChannel(item),
		"delivery_account_id": watchDeliveryAccountID(item),
		"delivery_target":     watchDeliveryTarget(item),
	})
}

func handleWatchUpdate(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: watch service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: %w", err)
	}
	current, err := b.watchService.Store().Get(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: %w", err)
	}
	updated := *current
	now := time.Now().UTC()

	if call.Input["name"] != nil {
		updated.Name = optionalString(call.Input, "name")
	}
	if call.Input["enabled"] != nil {
		value, boolErr := boolFrom(call.Input["enabled"])
		if boolErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("watch.update: enabled: %w", boolErr)
		}
		updated.Enabled = value
		if updated.Enabled {
			updated.NextCheckAt = now
		} else {
			updated.NextCheckAt = time.Time{}
		}
	}
	if call.Input["interval"] != nil {
		updated.Interval = optionalString(call.Input, "interval")
		if updated.Enabled {
			updated.NextCheckAt = now
		}
	}
	if call.Input["source_kind"] != nil || call.Input["source_url"] != nil || call.Input["source_path"] != nil || call.Input["source_session_key"] != nil || call.Input["calendar_query"] != nil || call.Input["mailbox_folder"] != nil || call.Input["mailbox_query"] != nil || call.Input["mailbox_limit"] != nil || call.Input["webhook_id"] != nil || call.Input["webhook_sender_id"] != nil || call.Input["inbox_limit"] != nil {
		sourceKind := updated.Source.Kind
		if call.Input["source_kind"] != nil {
			sourceKind = optionalString(call.Input, "source_kind")
		}
		updated.Source = buildWatchSource(call.Input, sourceKind, updated.Source)
	}
	if call.Input["prompt"] != nil {
		updated.Prompt = optionalString(call.Input, "prompt")
	}
	if call.Input["session_key"] != nil {
		updated.SessionKey = optionalString(call.Input, "session_key")
	}
	if call.Input["model"] != nil {
		updated.Model = optionalString(call.Input, "model")
	}
	if call.Input["automation_id"] != nil {
		updated.AutomationID = optionalString(call.Input, "automation_id")
	}
	if call.Input["delivery_channel"] != nil || call.Input["delivery_account_id"] != nil || call.Input["delivery_target"] != nil {
		updated.Delivery = buildWatchDelivery(call.Input, updated.Delivery)
	}
	if call.Input["fire_on_start"] != nil {
		value, boolErr := boolFrom(call.Input["fire_on_start"])
		if boolErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("watch.update: fire_on_start: %w", boolErr)
		}
		updated.FireOnStart = value
	}
	updated.UpdatedAt = now

	if err := watchsvc.Validate(updated); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: %w", err)
	}
	if err := b.watchService.Store().Update(id, func(item *watchsvc.Watch) {
		*item = updated
	}); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: %w", err)
	}
	if err := b.watchService.Store().Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.update: %w", err)
	}
	b.watchService.Rearm()
	return b.jsonResult(call, map[string]any{"id": id, "updated": true})
}

func handleWatchRemove(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.remove: watch service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.remove: %w", err)
	}
	if err := b.watchService.Store().Remove(id); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.remove: %w", err)
	}
	if err := b.watchService.Store().Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.remove: %w", err)
	}
	b.watchService.Rearm()
	return b.jsonResult(call, map[string]any{"id": id, "removed": true})
}

func handleWatchRun(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.run: watch service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.run: %w", err)
	}
	if err := b.watchService.Trigger(ctx, id); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.run: %w", err)
	}
	return b.jsonResult(call, map[string]any{"id": id, "triggered": true})
}

func handleWatchStatus(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	if b.watchService == nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.status: watch service not available")
	}
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.status: %w", err)
	}
	item, err := b.watchService.Store().Get(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("watch.status: %w", err)
	}
	return b.jsonResult(call, watchJSON(*item))
}

func watchJSON(item watchsvc.Watch) map[string]any {
	return map[string]any{
		"id":                             item.ID,
		"name":                           item.Name,
		"enabled":                        item.Enabled,
		"interval":                       item.Interval,
		"source_kind":                    item.Source.Kind,
		"source_url":                     watchSourceURL(item),
		"source_path":                    watchSourcePath(item),
		"source_session_key":             watchSourceSessionKey(item),
		"calendar_query":                 watchCalendarQuery(item),
		"mailbox_folder":                 watchMailboxFolder(item),
		"mailbox_query":                  watchMailboxQuery(item),
		"webhook_id":                     watchWebhookID(item),
		"webhook_sender_id":              watchWebhookSenderID(item),
		"inbox_limit":                    watchInboxLimit(item),
		"prompt":                         item.Prompt,
		"session_key":                    item.SessionKey,
		"model":                          item.Model,
		"delivery_channel":               watchDeliveryChannel(item),
		"delivery_account_id":            watchDeliveryAccountID(item),
		"delivery_target":                watchDeliveryTarget(item),
		"notification_total_count":       item.Notifications.TotalCount,
		"notification_failure_count":     item.Notifications.FailureCount,
		"notification_today_count":       item.Notifications.TodayCount,
		"notification_today_date":        item.Notifications.TodayDate,
		"notification_last_attempt_at":   formatTimeOrEmpty(item.Notifications.LastAttemptAt),
		"notification_last_delivered_at": formatTimeOrEmpty(item.Notifications.LastDeliveredAt),
		"notification_last_status":       item.Notifications.LastStatus,
		"notification_last_error":        item.Notifications.LastError,
		"fire_on_start":                  item.FireOnStart,
		"last_run_id":                    item.LastRunID,
		"last_status":                    item.LastStatus,
		"last_error":                     item.LastError,
		"last_summary":                   item.LastSummary,
		"last_verification_status":       item.LastVerificationStatus,
		"last_verification_summary":      item.LastVerificationSummary,
		"last_checked_at":                formatTimeOrEmpty(item.LastCheckedAt),
		"last_triggered_at":              formatTimeOrEmpty(item.LastTriggeredAt),
		"next_check_at":                  formatTimeOrEmpty(item.NextCheckAt),
		"created_at":                     formatTimeOrEmpty(item.CreatedAt),
		"updated_at":                     formatTimeOrEmpty(item.UpdatedAt),
	}
}

func buildWatchDelivery(input map[string]any, current *watchsvc.DeliveryTarget) *watchsvc.DeliveryTarget {
	hasChannel := input["delivery_channel"] != nil
	hasAccountID := input["delivery_account_id"] != nil
	hasTarget := input["delivery_target"] != nil
	channel := ""
	accountID := ""
	target := ""
	if current != nil {
		channel = strings.TrimSpace(current.Channel)
		accountID = strings.TrimSpace(current.AccountID)
		target = strings.TrimSpace(current.Target)
	}
	if hasChannel {
		channel = optionalString(input, "delivery_channel")
	}
	if hasAccountID {
		accountID = optionalString(input, "delivery_account_id")
	}
	if hasTarget {
		target = optionalString(input, "delivery_target")
	}
	if channel == "" && accountID == "" && target == "" {
		return nil
	}
	return &watchsvc.DeliveryTarget{
		Kind:       "channel",
		Provider:   strings.ToLower(channel),
		Channel:    channel,
		AccountID:  accountID,
		TargetType: destinationTargetType(channel),
		Target:     target,
	}
}

func buildWatchSource(input map[string]any, kind string, current watchsvc.Source) watchsvc.Source {
	source := watchsvc.Source{Kind: strings.TrimSpace(kind)}
	if source.Kind == "" {
		source.Kind = strings.TrimSpace(current.Kind)
	}
	switch source.Kind {
	case watchsvc.SourceKindHTTP:
		urlValue := optionalString(input, "source_url")
		if urlValue == "" && current.HTTP != nil {
			urlValue = strings.TrimSpace(current.HTTP.URL)
		}
		source.HTTP = &watchsvc.HTTPSource{URL: urlValue}
	case watchsvc.SourceKindFile:
		pathValue := optionalString(input, "source_path")
		if pathValue == "" && current.File != nil {
			pathValue = strings.TrimSpace(current.File.Path)
		}
		source.File = &watchsvc.FileSource{Path: pathValue}
	case watchsvc.SourceKindFeed:
		urlValue := optionalString(input, "source_url")
		if urlValue == "" && current.Feed != nil {
			urlValue = strings.TrimSpace(current.Feed.URL)
		}
		source.Feed = &watchsvc.FeedSource{URL: urlValue}
	case watchsvc.SourceKindMailbox:
		folder := optionalString(input, "mailbox_folder")
		if folder == "" && current.Mailbox != nil {
			folder = strings.TrimSpace(current.Mailbox.Folder)
		}
		query := optionalString(input, "mailbox_query")
		if query == "" && current.Mailbox != nil {
			query = strings.TrimSpace(current.Mailbox.Query)
		}
		limit := 0
		if input["mailbox_limit"] != nil {
			if value, err := intFrom(input["mailbox_limit"], 0); err == nil {
				limit = value
			}
		} else if current.Mailbox != nil {
			limit = current.Mailbox.Limit
		}
		source.Mailbox = &watchsvc.MailboxSource{Folder: folder, Query: query, Limit: limit}
	case watchsvc.SourceKindCalendar:
		query := optionalString(input, "calendar_query")
		if query == "" && current.Calendar != nil {
			query = strings.TrimSpace(current.Calendar.Query)
		}
		limit := 0
		if input["inbox_limit"] != nil {
			if value, err := intFrom(input["inbox_limit"], 0); err == nil {
				limit = value
			}
		} else if current.Calendar != nil {
			limit = current.Calendar.Limit
		}
		source.Calendar = &watchsvc.CalendarSource{Query: query, Limit: limit}
	case watchsvc.SourceKindWebhook:
		sessionKey := optionalString(input, "source_session_key")
		if sessionKey == "" && current.Webhook != nil {
			sessionKey = strings.TrimSpace(current.Webhook.SessionKey)
		}
		webhookID := optionalString(input, "webhook_id")
		if webhookID == "" && current.Webhook != nil {
			webhookID = strings.TrimSpace(current.Webhook.WebhookID)
		}
		senderID := optionalString(input, "webhook_sender_id")
		if senderID == "" && current.Webhook != nil {
			senderID = strings.TrimSpace(current.Webhook.SenderID)
		}
		limit := 0
		if input["inbox_limit"] != nil {
			if value, err := intFrom(input["inbox_limit"], 0); err == nil {
				limit = value
			}
		} else if current.Webhook != nil {
			limit = current.Webhook.Limit
		}
		source.Webhook = &watchsvc.WebhookSource{WebhookID: webhookID, SenderID: senderID, SessionKey: sessionKey, Limit: limit}
	case watchsvc.SourceKindStructuredInbox:
		sessionKey := optionalString(input, "source_session_key")
		if sessionKey == "" && current.StructuredInbox != nil {
			sessionKey = strings.TrimSpace(current.StructuredInbox.SessionKey)
		}
		limit := 0
		if input["inbox_limit"] != nil {
			if value, err := intFrom(input["inbox_limit"], 0); err == nil {
				limit = value
			}
		} else if current.StructuredInbox != nil {
			limit = current.StructuredInbox.Limit
		}
		source.StructuredInbox = &watchsvc.StructuredInboxSource{SessionKey: sessionKey, Limit: limit}
	case watchsvc.SourceKindBrowserSnapshot:
		urlValue := optionalString(input, "source_url")
		if urlValue == "" && current.BrowserSnapshot != nil {
			urlValue = strings.TrimSpace(current.BrowserSnapshot.URL)
		}
		source.BrowserSnapshot = &watchsvc.BrowserSnapshotSource{URL: urlValue}
	default:
		if source.Kind == "" {
			source.Kind = watchsvc.SourceKindHTTP
			source.HTTP = &watchsvc.HTTPSource{URL: optionalString(input, "source_url")}
		}
	}
	return source
}

func watchSourceURL(item watchsvc.Watch) string {
	switch item.Source.Kind {
	case watchsvc.SourceKindHTTP:
		if item.Source.HTTP != nil {
			return strings.TrimSpace(item.Source.HTTP.URL)
		}
	case watchsvc.SourceKindFeed:
		if item.Source.Feed != nil {
			return strings.TrimSpace(item.Source.Feed.URL)
		}
	case watchsvc.SourceKindBrowserSnapshot:
		if item.Source.BrowserSnapshot != nil {
			return strings.TrimSpace(item.Source.BrowserSnapshot.URL)
		}
	case watchsvc.SourceKindWebhook:
		if item.Source.Webhook != nil && strings.TrimSpace(item.Source.Webhook.SessionKey) != "" {
			return strings.TrimSpace(item.Source.Webhook.SessionKey)
		}
	}
	return ""
}

func watchSourcePath(item watchsvc.Watch) string {
	if item.Source.File == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.File.Path)
}

func watchMailboxFolder(item watchsvc.Watch) string {
	if item.Source.Mailbox == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.Mailbox.Folder)
}

func watchMailboxQuery(item watchsvc.Watch) string {
	if item.Source.Mailbox == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.Mailbox.Query)
}

func watchCalendarQuery(item watchsvc.Watch) string {
	if item.Source.Calendar == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.Calendar.Query)
}

func watchSourceSessionKey(item watchsvc.Watch) string {
	switch item.Source.Kind {
	case watchsvc.SourceKindWebhook:
		if item.Source.Webhook != nil {
			return strings.TrimSpace(item.Source.Webhook.SessionKey)
		}
	case watchsvc.SourceKindStructuredInbox:
		if item.Source.StructuredInbox != nil {
			return strings.TrimSpace(item.Source.StructuredInbox.SessionKey)
		}
	}
	return ""
}

func watchWebhookID(item watchsvc.Watch) string {
	if item.Source.Webhook == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.Webhook.WebhookID)
}

func watchWebhookSenderID(item watchsvc.Watch) string {
	if item.Source.Webhook == nil {
		return ""
	}
	return strings.TrimSpace(item.Source.Webhook.SenderID)
}

func watchInboxLimit(item watchsvc.Watch) int {
	switch item.Source.Kind {
	case watchsvc.SourceKindWebhook:
		if item.Source.Webhook != nil {
			return item.Source.Webhook.Limit
		}
	case watchsvc.SourceKindStructuredInbox:
		if item.Source.StructuredInbox != nil {
			return item.Source.StructuredInbox.Limit
		}
	case watchsvc.SourceKindCalendar:
		if item.Source.Calendar != nil {
			return item.Source.Calendar.Limit
		}
	}
	return 0
}

func watchDeliveryChannel(item watchsvc.Watch) string {
	if item.Delivery == nil {
		return ""
	}
	return strings.TrimSpace(item.Delivery.Channel)
}

func watchDeliveryTarget(item watchsvc.Watch) string {
	if item.Delivery == nil {
		return ""
	}
	return strings.TrimSpace(item.Delivery.Target)
}

func watchDeliveryAccountID(item watchsvc.Watch) string {
	if item.Delivery == nil {
		return ""
	}
	return strings.TrimSpace(item.Delivery.AccountID)
}

// optionalString is now defined in helpers_exported.go
