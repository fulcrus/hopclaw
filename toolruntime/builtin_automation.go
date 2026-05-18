package toolruntime

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
	watchsvc "github.com/fulcrus/hopclaw/watch"
)

func automationToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "automation.search",
				Description:     "Search cron, wakeup, and watch automations using a unified inventory view.",
				InputSchema:     automationSearchInputSchema(),
				OutputSchema:    automationSearchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "automation:search",
			},
			Handler: handleAutomationSearch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "automation.stats",
				Description:     "Return a unified automation summary including counts and notification totals.",
				InputSchema:     automationStatsInputSchema(),
				OutputSchema:    automationStatsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "automation:stats",
			},
			Handler: handleAutomationStats,
		},
	}
}

func automationSearchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"query":   stringSchema("Optional search text. Blank returns all visible automations."),
		"kind":    stringSchema("Optional automation kind filter: cron, wakeup, or watch."),
		"enabled": booleanSchema("Optional enabled filter."),
		"limit":   integerSchema("Maximum number of items to return."),
	})
}

func automationStatsInputSchema() map[string]any {
	return automationSearchInputSchema()
}

func automationSearchOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":                             stringSchema("Automation ID."),
		"kind":                           stringSchema("Automation kind."),
		"name":                           stringSchema("Automation name."),
		"enabled":                        booleanSchema("Whether the automation is enabled."),
		"schedule":                       stringSchema("Schedule or interval summary."),
		"message":                        stringSchema("Wakeup message, when applicable."),
		"prompt_preview":                 stringSchema("Prompt preview, when applicable."),
		"session_key":                    stringSchema("Session key."),
		"model":                          stringSchema("Model override."),
		"channel":                        stringSchema("Channel associated with the automation."),
		"delivery_channel":               stringSchema("Delivery channel."),
		"delivery_account_id":            stringSchema("Delivery account ID."),
		"delivery_target":                stringSchema("Delivery target."),
		"source_kind":                    stringSchema("Watch source kind."),
		"source_label":                   stringSchema("Watch source summary."),
		"next_run_at":                    stringSchema("Next run/check time in RFC3339 format."),
		"last_run_at":                    stringSchema("Last execution time in RFC3339 format."),
		"last_status":                    stringSchema("Last execution status."),
		"notification_total_count":       integerSchema("Total successful notifications delivered."),
		"notification_failure_count":     integerSchema("Total failed notifications."),
		"notification_today_count":       integerSchema("Notifications delivered today."),
		"notification_today_date":        stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
		"notification_last_attempt_at":   stringSchema("Latest notification attempt time in RFC3339 format."),
		"notification_last_delivered_at": stringSchema("Latest successful notification delivery time in RFC3339 format."),
		"notification_last_status":       stringSchema("Latest notification status."),
		"notification_last_error":        stringSchema("Latest notification error."),
	}, "id", "kind", "name", "enabled")
	return objectSchema(map[string]any{
		"items": arraySchema(entry, "Matching automation items."),
		"count": integerSchema("Number of returned items."),
	}, "items", "count")
}

func automationStatsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"total_count":                integerSchema("Total number of matching automations."),
		"enabled_count":              integerSchema("Number of enabled automations."),
		"disabled_count":             integerSchema("Number of disabled automations."),
		"cron_count":                 integerSchema("Number of cron automations."),
		"wakeup_count":               integerSchema("Number of wakeup automations."),
		"watch_count":                integerSchema("Number of watch automations."),
		"notification_total_count":   integerSchema("Total successful notifications delivered."),
		"notification_failure_count": integerSchema("Total failed notifications."),
		"notification_today_count":   integerSchema("Notifications delivered today."),
		"notification_today_date":    stringSchema("UTC date for notification_today_count in YYYY-MM-DD format."),
	}, "total_count", "enabled_count", "disabled_count")
}

type automationInventoryEntry struct {
	ID            string
	Kind          string
	Name          string
	Enabled       bool
	Schedule      string
	Message       string
	PromptPreview string
	SessionKey    string
	Model         string
	Channel       string
	DeliveryChan  string
	DeliveryAcct  string
	DeliveryTgt   string
	SourceKind    string
	SourceLabel   string
	NextRunAt     time.Time
	LastRunAt     time.Time
	LastStatus    string

	NotificationTotal     int
	NotificationFailure   int
	NotificationToday     int
	NotificationTodayDate string
	NotificationLastTry   time.Time
	NotificationLastOK    time.Time
	NotificationLastState string
	NotificationLastError string
}

func handleAutomationSearch(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	items, err := automationInventoryForCall(b, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, automationInventoryJSON(item))
	}
	return b.jsonResult(call, map[string]any{
		"items": payload,
		"count": len(payload),
	})
}

func handleAutomationStats(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	items, err := automationInventoryForCall(b, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	day := time.Now().UTC().Format("2006-01-02")
	result := map[string]any{
		"total_count":                len(items),
		"enabled_count":              0,
		"disabled_count":             0,
		"cron_count":                 0,
		"wakeup_count":               0,
		"watch_count":                0,
		"notification_total_count":   0,
		"notification_failure_count": 0,
		"notification_today_count":   0,
		"notification_today_date":    day,
	}
	for _, item := range items {
		if item.Enabled {
			result["enabled_count"] = intValue(result["enabled_count"]) + 1
		} else {
			result["disabled_count"] = intValue(result["disabled_count"]) + 1
		}
		switch item.Kind {
		case "cron":
			result["cron_count"] = intValue(result["cron_count"]) + 1
		case "wakeup":
			result["wakeup_count"] = intValue(result["wakeup_count"]) + 1
		case "watch":
			result["watch_count"] = intValue(result["watch_count"]) + 1
		}
		result["notification_total_count"] = intValue(result["notification_total_count"]) + item.NotificationTotal
		result["notification_failure_count"] = intValue(result["notification_failure_count"]) + item.NotificationFailure
		if item.NotificationTodayDate == day {
			result["notification_today_count"] = intValue(result["notification_today_count"]) + item.NotificationToday
		}
	}
	return b.jsonResult(call, result)
}

func automationInventoryForCall(b *Builtins, input map[string]any) ([]automationInventoryEntry, error) {
	query := strings.TrimSpace(strings.ToLower(optionalString(input, "query")))
	kind := strings.TrimSpace(strings.ToLower(optionalString(input, "kind")))
	limit, err := intFrom(input["limit"], 20)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	enabledFilterSet := input["enabled"] != nil
	enabledFilter := false
	if enabledFilterSet {
		enabledFilter, err = boolFrom(input["enabled"])
		if err != nil {
			return nil, err
		}
	}
	items := automationInventory(b)
	filtered := make([]automationInventoryEntry, 0, len(items))
	for _, item := range items {
		if kind != "" && item.Kind != kind {
			continue
		}
		if enabledFilterSet && item.Enabled != enabledFilter {
			continue
		}
		score := automationMatchScore(item, query)
		if query != "" && score == 0 {
			continue
		}
		filtered = append(filtered, item)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		scoreI := automationMatchScore(filtered[i], query)
		scoreJ := automationMatchScore(filtered[j], query)
		switch {
		case scoreI != scoreJ:
			return scoreI > scoreJ
		case filtered[i].Enabled != filtered[j].Enabled:
			return filtered[i].Enabled
		case !filtered[i].NextRunAt.Equal(filtered[j].NextRunAt):
			if filtered[i].NextRunAt.IsZero() {
				return false
			}
			if filtered[j].NextRunAt.IsZero() {
				return true
			}
			return filtered[i].NextRunAt.Before(filtered[j].NextRunAt)
		case filtered[i].Kind != filtered[j].Kind:
			return filtered[i].Kind < filtered[j].Kind
		default:
			return filtered[i].Name < filtered[j].Name
		}
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func automationInventory(b *Builtins) []automationInventoryEntry {
	out := make([]automationInventoryEntry, 0)
	if b != nil && b.cronService != nil {
		for _, job := range b.cronService.Store().List() {
			out = append(out, automationInventoryEntry{
				ID:                    strings.TrimSpace(job.ID),
				Kind:                  "cron",
				Name:                  strings.TrimSpace(job.Name),
				Enabled:               job.Enabled,
				Schedule:              cronScheduleSummary(job.Schedule),
				PromptPreview:         strings.TrimSpace(job.Payload.Content),
				SessionKey:            strings.TrimSpace(job.SessionKey),
				Model:                 strings.TrimSpace(job.Model),
				Channel:               cronDeliveryChannel(job.Delivery),
				DeliveryChan:          cronDeliveryChannel(job.Delivery),
				DeliveryAcct:          cronDeliveryAccountID(job.Delivery),
				DeliveryTgt:           cronDeliveryTarget(job.Delivery),
				NextRunAt:             job.NextRunAt,
				LastRunAt:             job.LastRunAt,
				LastStatus:            strings.TrimSpace(job.LastStatus),
				NotificationTotal:     job.Notifications.TotalCount,
				NotificationFailure:   job.Notifications.FailureCount,
				NotificationToday:     job.Notifications.TodayCount,
				NotificationTodayDate: strings.TrimSpace(job.Notifications.TodayDate),
				NotificationLastTry:   job.Notifications.LastAttemptAt,
				NotificationLastOK:    job.Notifications.LastDeliveredAt,
				NotificationLastState: strings.TrimSpace(job.Notifications.LastStatus),
				NotificationLastError: strings.TrimSpace(job.Notifications.LastError),
			})
		}
	}
	if b != nil && b.wakeupService != nil {
		for _, trigger := range b.wakeupService.List() {
			out = append(out, automationInventoryEntry{
				ID:            strings.TrimSpace(trigger.ID),
				Kind:          "wakeup",
				Name:          strings.TrimSpace(trigger.Name),
				Enabled:       trigger.Enabled,
				Schedule:      strings.TrimSpace(trigger.Schedule),
				Message:       strings.TrimSpace(trigger.Message),
				PromptPreview: strings.TrimSpace(trigger.Message),
				SessionKey:    strings.TrimSpace(trigger.SessionKey),
				Model:         strings.TrimSpace(trigger.Model),
				Channel:       strings.TrimSpace(trigger.Channel),
				NextRunAt:     trigger.NextRunAt,
				LastRunAt:     trigger.LastRunAt,
				LastStatus:    strings.TrimSpace(trigger.LastStatus),
			})
		}
	}
	if b != nil && b.watchService != nil {
		for _, item := range b.watchService.Store().List() {
			out = append(out, automationInventoryEntry{
				ID:                    strings.TrimSpace(item.ID),
				Kind:                  "watch",
				Name:                  strings.TrimSpace(item.Name),
				Enabled:               item.Enabled,
				Schedule:              strings.TrimSpace(item.Interval),
				PromptPreview:         strings.TrimSpace(item.Prompt),
				SessionKey:            strings.TrimSpace(item.SessionKey),
				Model:                 strings.TrimSpace(item.Model),
				Channel:               watchDeliveryChannel(item),
				DeliveryChan:          watchDeliveryChannel(item),
				DeliveryAcct:          watchDeliveryAccountID(item),
				DeliveryTgt:           watchDeliveryTarget(item),
				SourceKind:            strings.TrimSpace(item.Source.Kind),
				SourceLabel:           automationWatchSourceLabel(item.Source),
				NextRunAt:             item.NextCheckAt,
				LastRunAt:             firstNonZeroAutomationTime(item.LastTriggeredAt, item.LastCheckedAt),
				LastStatus:            strings.TrimSpace(item.LastStatus),
				NotificationTotal:     item.Notifications.TotalCount,
				NotificationFailure:   item.Notifications.FailureCount,
				NotificationToday:     item.Notifications.TodayCount,
				NotificationTodayDate: strings.TrimSpace(item.Notifications.TodayDate),
				NotificationLastTry:   item.Notifications.LastAttemptAt,
				NotificationLastOK:    item.Notifications.LastDeliveredAt,
				NotificationLastState: strings.TrimSpace(item.Notifications.LastStatus),
				NotificationLastError: strings.TrimSpace(item.Notifications.LastError),
			})
		}
	}
	return out
}

func automationInventoryJSON(item automationInventoryEntry) map[string]any {
	return map[string]any{
		"id":                             item.ID,
		"kind":                           item.Kind,
		"name":                           item.Name,
		"enabled":                        item.Enabled,
		"schedule":                       item.Schedule,
		"message":                        item.Message,
		"prompt_preview":                 item.PromptPreview,
		"session_key":                    item.SessionKey,
		"model":                          item.Model,
		"channel":                        item.Channel,
		"delivery_channel":               item.DeliveryChan,
		"delivery_account_id":            item.DeliveryAcct,
		"delivery_target":                item.DeliveryTgt,
		"source_kind":                    item.SourceKind,
		"source_label":                   item.SourceLabel,
		"next_run_at":                    formatTimeOrEmpty(item.NextRunAt),
		"last_run_at":                    formatTimeOrEmpty(item.LastRunAt),
		"last_status":                    item.LastStatus,
		"notification_total_count":       item.NotificationTotal,
		"notification_failure_count":     item.NotificationFailure,
		"notification_today_count":       item.NotificationToday,
		"notification_today_date":        item.NotificationTodayDate,
		"notification_last_attempt_at":   formatTimeOrEmpty(item.NotificationLastTry),
		"notification_last_delivered_at": formatTimeOrEmpty(item.NotificationLastOK),
		"notification_last_status":       item.NotificationLastState,
		"notification_last_error":        item.NotificationLastError,
	}
}

func automationMatchScore(item automationInventoryEntry, query string) int {
	if query == "" {
		return 1
	}
	score := 0
	fields := []string{
		strings.ToLower(item.ID),
		strings.ToLower(item.Name),
		strings.ToLower(item.Kind),
		strings.ToLower(item.Schedule),
		strings.ToLower(item.Message),
		strings.ToLower(item.PromptPreview),
		strings.ToLower(item.SourceKind),
		strings.ToLower(item.SourceLabel),
		strings.ToLower(item.Channel),
		strings.ToLower(item.DeliveryChan),
		strings.ToLower(item.DeliveryTgt),
	}
	for _, field := range fields {
		switch {
		case field == query:
			score += 80
		case strings.Contains(field, query):
			score += 24
		}
	}
	for _, token := range strings.Fields(query) {
		for _, field := range fields {
			if strings.Contains(field, token) {
				score += 5
			}
		}
	}
	return score
}

func automationWatchSourceLabel(source watchsvc.Source) string {
	switch source.Kind {
	case watchsvc.SourceKindHTTP:
		if source.HTTP != nil {
			return strings.TrimSpace(source.HTTP.URL)
		}
	case watchsvc.SourceKindFeed:
		if source.Feed != nil {
			return strings.TrimSpace(source.Feed.URL)
		}
	case watchsvc.SourceKindFile:
		if source.File != nil {
			return strings.TrimSpace(source.File.Path)
		}
	case watchsvc.SourceKindBrowserSnapshot:
		if source.BrowserSnapshot != nil {
			return strings.TrimSpace(source.BrowserSnapshot.URL)
		}
	case watchsvc.SourceKindCalendar:
		if source.Calendar != nil {
			return strings.TrimSpace(source.Calendar.Query)
		}
	case watchsvc.SourceKindMailbox:
		if source.Mailbox != nil {
			return strings.TrimSpace(strings.TrimSpace(source.Mailbox.Folder) + " " + strings.TrimSpace(source.Mailbox.Query))
		}
	case watchsvc.SourceKindWebhook:
		if source.Webhook != nil {
			return strings.TrimSpace(firstNonEmptyString(source.Webhook.SessionKey, source.Webhook.WebhookID))
		}
	case watchsvc.SourceKindStructuredInbox:
		if source.StructuredInbox != nil {
			return strings.TrimSpace(source.StructuredInbox.SessionKey)
		}
	}
	return ""
}

func firstNonZeroAutomationTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
