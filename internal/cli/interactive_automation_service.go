package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/hooks"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
)

func (g *externalInteractiveGateway) GetAutomationDetail(ctx context.Context, kind, id string) (*replpkg.AutomationItem, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	var resp automationCLIItemDetailResponse
	path := automationPath + "/" + url.PathEscape(strings.TrimSpace(kind)) + "/" + url.PathEscape(strings.TrimSpace(id))
	if err := g.client.Get(ctx, path, &resp); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if resp.Item.LastExecution == nil && len(resp.RecentExecutions) > 0 {
		latest := resp.RecentExecutions[0]
		resp.Item.LastExecution = &latest
	}
	return mapSingleAutomationItem(resp.Item), nil
}

func (g *externalInteractiveGateway) ListAutomations(ctx context.Context, limit int) ([]replpkg.AutomationItem, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var resp automationCLIItemsResponse
	err := g.client.Get(ctx, automationPath, &resp)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := mapAutomationItems(resp.Items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (g *externalInteractiveGateway) CreateAutomation(ctx context.Context, req replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(firstNonEmpty(req.Kind, "cron")), "cron") {
		return nil, nil
	}
	body := map[string]any{
		"name":    strings.TrimSpace(req.Name),
		"enabled": req.Enabled,
		"schedule": map[string]any{
			"kind":       firstNonEmpty(strings.TrimSpace(req.ScheduleKind), cron.ScheduleKindCron),
			"at":         strings.TrimSpace(req.At),
			"every":      strings.TrimSpace(req.Every),
			"expression": strings.TrimSpace(req.Expression),
		},
		"payload": map[string]any{
			"content": strings.TrimSpace(req.Prompt),
		},
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		body["model"] = model
	}
	if sessionKey := strings.TrimSpace(req.SessionKey); sessionKey != "" {
		body["session_key"] = sessionKey
	}
	var resp cronCLIJobResponse
	if err := g.client.Post(ctx, cronJobsPath, body, &resp); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	item := mapCronCLIJobAutomationItem(resp.Job)
	return &item, nil
}

func (g *externalInteractiveGateway) PauseAutomation(ctx context.Context, kind, id string) error {
	return updateGatewayAutomationEnabled(ctx, g.client, kind, id, false)
}

func (g *externalInteractiveGateway) ResumeAutomation(ctx context.Context, kind, id string) error {
	return updateGatewayAutomationEnabled(ctx, g.client, kind, id, true)
}

func (g *externalInteractiveGateway) RunAutomationNow(ctx context.Context, kind, id string) error {
	if g == nil || g.client == nil {
		return nil
	}
	path := automationRunNowPath(kind, id)
	if path == "" {
		return nil
	}
	if err := g.client.Post(ctx, path, map[string]any{}, nil); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil
	} else {
		return err
	}
}

func (g *embeddedInteractiveGateway) GetAutomationDetail(ctx context.Context, kind, id string) (*replpkg.AutomationItem, error) {
	if g == nil || g.app == nil {
		return nil, nil
	}
	item, err := embeddedAutomationDetailItem(ctx, g.app, kind, id)
	if err != nil || item == nil {
		return nil, err
	}
	return mapSingleAutomationItem(*item), nil
}

func (g *embeddedInteractiveGateway) ListAutomations(ctx context.Context, limit int) ([]replpkg.AutomationItem, error) {
	if g == nil || g.app == nil {
		return nil, nil
	}
	items := embeddedAutomationItems(ctx, g.app)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Status != items[j].Status {
			return items[i].Status < items[j].Status
		}
		return items[i].Name < items[j].Name
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (g *embeddedInteractiveGateway) CreateAutomation(ctx context.Context, req replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error) {
	if g == nil || g.app == nil || g.app.CronService == nil {
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(firstNonEmpty(req.Kind, "cron")), "cron") {
		return nil, nil
	}
	now := time.Now().UTC()
	schedule := cron.Schedule{
		Kind:       firstNonEmpty(strings.TrimSpace(req.ScheduleKind), cron.ScheduleKindCron),
		At:         strings.TrimSpace(req.At),
		Every:      strings.TrimSpace(req.Every),
		Expression: strings.TrimSpace(req.Expression),
	}
	nextRun, err := cron.NextRunTimeAnchored(schedule, now, now)
	if err != nil {
		return nil, err
	}
	job := cron.Job{
		ID:         promotedCronJobID(req.Name, now),
		Name:       strings.TrimSpace(req.Name),
		Enabled:    req.Enabled,
		Schedule:   schedule,
		Payload:    cron.Payload{Content: strings.TrimSpace(req.Prompt)},
		SessionKey: strings.TrimSpace(req.SessionKey),
		Model:      strings.TrimSpace(req.Model),
		NextRunAt:  nextRun,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := g.app.CronService.Store().Add(job); err != nil {
		return nil, err
	}
	if err := g.app.CronService.Store().Save(); err != nil {
		return nil, err
	}
	g.app.CronService.Rearm()
	item := mapCronAutomationItem(job)
	return &item, nil
}

func (g *embeddedInteractiveGateway) PauseAutomation(ctx context.Context, kind, id string) error {
	return updateEmbeddedAutomationEnabled(ctx, g.app, kind, id, false)
}

func (g *embeddedInteractiveGateway) ResumeAutomation(ctx context.Context, kind, id string) error {
	return updateEmbeddedAutomationEnabled(ctx, g.app, kind, id, true)
}

func (g *embeddedInteractiveGateway) RunAutomationNow(ctx context.Context, kind, id string) error {
	if g == nil || g.app == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	switch autopkg.Kind(strings.TrimSpace(kind)) {
	case autopkg.KindCron:
		if g.app.CronService == nil {
			return nil
		}
		return g.app.CronService.TriggerJob(ctx, id)
	case autopkg.KindWatch:
		if g.app.WatchService == nil {
			return nil
		}
		return g.app.WatchService.Trigger(ctx, id)
	case autopkg.KindHook:
		if g.app.HookExecutor == nil {
			return nil
		}
		hook, err := g.app.HookExecutor.Store().Get(ctx, id)
		if err != nil {
			return err
		}
		_, err = g.app.HookExecutor.FireHook(ctx, id, hook.Trigger, hook.EffectivePhase(), map[string]any{})
		return err
	default:
		return nil
	}
}

func updateGatewayAutomationEnabled(ctx context.Context, client *GatewayClient, kind, id string, enabled bool) error {
	if client == nil {
		return nil
	}
	path, err := automationUpdatePath(kind, id)
	if err != nil {
		return nil
	}
	body := map[string]any{"enabled": enabled}
	if err := client.Patch(ctx, path, body, nil); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil
	} else {
		return err
	}
}

func automationRunNowPath(kind, id string) string {
	id = url.PathEscape(strings.TrimSpace(id))
	switch autopkg.Kind(strings.TrimSpace(kind)) {
	case autopkg.KindCron:
		return cronJobsPath + "/" + id + "/run"
	case autopkg.KindWatch:
		return "/operator/watch/items/" + id + "/run"
	default:
		return ""
	}
}

func mapAutomationItems(items []autopkg.Item) []replpkg.AutomationItem {
	out := make([]replpkg.AutomationItem, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.AutomationItem{
			ID:            strings.TrimSpace(item.ID),
			Name:          firstNonEmpty(strings.TrimSpace(item.Name), strings.TrimSpace(item.ID)),
			Kind:          strings.TrimSpace(string(item.Kind)),
			Status:        automationItemStatus(item),
			Schedule:      automationItemSchedule(item),
			Delivery:      automationItemDelivery(item),
			NextRun:       automationTimeLabel(item.NextRunAt),
			Health:        automationItemHealth(item),
			SetupContract: automationItemSetupContract(item),
		})
	}
	return out
}

func automationItemStatus(item autopkg.Item) string {
	if !item.Enabled {
		return "paused"
	}
	if automationDeliveryNeedsInput(item) {
		return "needs_input"
	}
	if item.LastExecution != nil {
		status := strings.ToLower(strings.TrimSpace(item.LastExecution.Status))
		switch status {
		case "running", "triggered":
			return "running"
		case "error", "failed":
			return "degraded"
		}
	}
	return "ready"
}

func automationItemSchedule(item autopkg.Item) string {
	switch {
	case strings.TrimSpace(item.Schedule) != "":
		return strings.TrimSpace(item.Schedule)
	case !item.NextRunAt.IsZero():
		return "next " + automationTimeLabel(item.NextRunAt)
	default:
		return "-"
	}
}

func automationItemDelivery(item autopkg.Item) string {
	if item.Delivery == nil {
		return "-"
	}
	label := strings.TrimSpace(item.Delivery.Label)
	if label != "" {
		return label
	}
	parts := []string{
		strings.TrimSpace(item.Delivery.Channel),
		strings.TrimSpace(item.Delivery.Target),
	}
	return firstNonEmpty(strings.TrimSpace(strings.Join(nonEmptyStrings(parts...), "/")), "-")
}

func automationItemHealth(item autopkg.Item) string {
	if item.LastExecution != nil {
		if summary := firstNonEmpty(strings.TrimSpace(item.LastExecution.Summary), strings.TrimSpace(item.LastExecution.Error)); summary != "" {
			return summary
		}
	}
	if item.Notifications != nil {
		if item.Notifications.FailureCount > 0 {
			return fmt.Sprintf("%d delivery failures", item.Notifications.FailureCount)
		}
		if item.Notifications.TotalCount > 0 {
			return fmt.Sprintf("%d delivered", item.Notifications.TotalCount)
		}
	}
	return "-"
}

func automationDeliveryNeedsInput(item autopkg.Item) bool {
	if item.Delivery == nil {
		return false
	}
	return strings.TrimSpace(item.Delivery.Target) == ""
}

func automationItemSetupContract(item autopkg.Item) *replpkg.AutomationSetupInfo {
	if !automationDeliveryNeedsInput(item) {
		return nil
	}
	channel := ""
	if item.Delivery != nil {
		channel = firstNonEmpty(strings.TrimSpace(item.Delivery.Label), strings.TrimSpace(item.Delivery.Channel), strings.TrimSpace(item.Delivery.Provider))
	}
	summary := "Delivery target is missing."
	if channel != "" {
		summary = "Delivery target is missing for " + channel + "."
	}
	question := "Provide a delivery target for this automation."
	if channel != "" {
		question = "Provide the delivery target for " + channel + "."
	}
	return &replpkg.AutomationSetupInfo{
		Status:  "needs_input",
		Summary: summary,
		Slots: []replpkg.AutomationSetupSlot{{
			Field:    "delivery_target",
			Question: question,
			Example:  automationDeliveryTargetExample(item),
			Required: true,
		}},
	}
}

func automationDeliveryTargetExample(item autopkg.Item) string {
	if item.Delivery == nil {
		return "#ops-alerts"
	}
	channel := strings.ToLower(strings.TrimSpace(firstNonEmpty(item.Delivery.Channel, item.Delivery.Provider, item.Delivery.Kind)))
	switch channel {
	case "email", "smtp":
		return "ops@example.com"
	case "sms":
		return "+15551234567"
	default:
		return "#ops-alerts"
	}
}

func automationTimeLabel(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	local := at.Local()
	if sameDay(local, time.Now()) {
		return local.Format("15:04")
	}
	return local.Format("2006-01-02 15:04")
}

func embeddedAutomationItems(ctx context.Context, app *bootstrap.App) []replpkg.AutomationItem {
	if app == nil {
		return nil
	}
	out := make([]replpkg.AutomationItem, 0)
	if app.CronService != nil {
		for _, job := range app.CronService.Store().List() {
			out = append(out, mapCronAutomationItem(job))
		}
	}
	if app.WatchService != nil {
		for _, item := range app.WatchService.Store().List() {
			out = append(out, mapWatchAutomationItem(item))
		}
	}
	if app.WakeupService != nil {
		for _, item := range app.WakeupService.List() {
			out = append(out, mapWakeupAutomationItem(item))
		}
	}
	if app.HookExecutor != nil {
		hookItems, err := app.HookExecutor.Store().List(ctx)
		if err == nil {
			for _, item := range hookItems {
				if item == nil {
					continue
				}
				out = append(out, mapHookAutomationItem(*item, app.HookExecutor.RecentResultsByHook(item.ID, 1)))
			}
		}
	}
	return out
}

func embeddedAutomationDetailItem(ctx context.Context, app *bootstrap.App, kind, id string) (*autopkg.Item, error) {
	if app == nil {
		return nil, nil
	}
	id = strings.TrimSpace(id)
	switch autopkg.Kind(strings.TrimSpace(kind)) {
	case autopkg.KindCron:
		if app.CronService == nil {
			return nil, nil
		}
		job, err := app.CronService.Store().Get(id)
		if err != nil {
			return nil, err
		}
		item := cronAutomationProjection(*job)
		return &item, nil
	case autopkg.KindWatch:
		if app.WatchService == nil {
			return nil, nil
		}
		watchItem, err := app.WatchService.Store().Get(id)
		if err != nil {
			return nil, err
		}
		item := watchAutomationProjection(*watchItem)
		return &item, nil
	case autopkg.KindWakeup:
		if app.WakeupService == nil {
			return nil, nil
		}
		trigger, err := app.WakeupService.Get(id)
		if err != nil {
			return nil, err
		}
		item := wakeupAutomationProjection(*trigger)
		return &item, nil
	case autopkg.KindHook:
		if app.HookExecutor == nil {
			return nil, nil
		}
		hookItem, err := app.HookExecutor.Store().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		item := hookAutomationProjection(*hookItem, app.HookExecutor.RecentResultsByHook(id, interactiveAutomationDetailLimit))
		return &item, nil
	default:
		return nil, fmt.Errorf("unsupported automation kind %q", kind)
	}
}

func updateEmbeddedAutomationEnabled(ctx context.Context, app *bootstrap.App, kind, id string, enabled bool) error {
	if app == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	switch autopkg.Kind(strings.TrimSpace(kind)) {
	case autopkg.KindCron:
		if app.CronService == nil {
			return nil
		}
		now := time.Now().UTC()
		return updateCronAutomation(app.CronService, id, enabled, now)
	case autopkg.KindWatch:
		if app.WatchService == nil {
			return nil
		}
		now := time.Now().UTC()
		if err := app.WatchService.Store().Update(id, func(item *watch.Watch) {
			item.Enabled = enabled
			item.UpdatedAt = now
			if enabled && item.NextCheckAt.IsZero() {
				item.NextCheckAt = now
			}
			if !enabled {
				item.NextCheckAt = time.Time{}
			}
		}); err != nil {
			return err
		}
		if err := app.WatchService.Store().Save(); err != nil {
			return err
		}
		app.WatchService.Rearm()
		return nil
	case autopkg.KindWakeup:
		if app.WakeupService == nil {
			return nil
		}
		if enabled {
			return app.WakeupService.Enable(id)
		}
		return app.WakeupService.Disable(id)
	case autopkg.KindHook:
		if app.HookExecutor == nil {
			return nil
		}
		hook, err := app.HookExecutor.Store().Get(ctx, id)
		if err != nil {
			return err
		}
		hook.Enabled = enabled
		_, err = app.HookExecutor.Store().Update(ctx, *hook)
		return err
	default:
		return nil
	}
}

func updateCronAutomation(service *cron.Service, id string, enabled bool, now time.Time) error {
	if service == nil {
		return nil
	}
	var updateErr error
	err := service.Store().Update(id, func(job *cron.Job) {
		job.Enabled = enabled
		job.UpdatedAt = now
		if !enabled {
			job.NextRunAt = time.Time{}
			return
		}
		anchor := job.LastRunAt
		if anchor.IsZero() {
			anchor = job.CreatedAt
		}
		nextRun, err := cron.NextRunTimeAnchored(job.Schedule, now, anchor)
		if err != nil {
			updateErr = err
			return
		}
		job.NextRunAt = nextRun
	})
	if err != nil {
		return err
	}
	if updateErr != nil {
		return updateErr
	}
	if err := service.Store().Save(); err != nil {
		return err
	}
	service.Rearm()
	return nil
}

func promotedCronJobID(name string, now time.Time) string {
	base := strings.ToLower(strings.TrimSpace(name))
	if base == "" {
		base = "promoted"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "promoted"
	}
	return fmt.Sprintf("%s-%d", slug, now.Unix())
}

func mapCronCLIJobAutomationItem(job cronCLIJob) replpkg.AutomationItem {
	return mapCronAutomationItem(cron.Job{
		ID:         job.ID,
		Name:       job.Name,
		Enabled:    job.Enabled,
		Schedule:   cron.Schedule(job.Schedule),
		Payload:    cron.Payload(job.Payload),
		SessionKey: job.SessionKey,
		Model:      job.Model,
		LastRunAt:  job.LastRunAt,
		NextRunAt:  job.NextRunAt,
		LastStatus: job.LastStatus,
		LastError:  job.LastError,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
	})
}

func mapCronAutomationItem(job cron.Job) replpkg.AutomationItem {
	return *mapSingleAutomationItem(cronAutomationProjection(job))
}

func cronAutomationProjection(job cron.Job) autopkg.Item {
	item := autopkg.Item{
		ID:            job.ID,
		Kind:          autopkg.KindCron,
		Name:          job.Name,
		Enabled:       job.Enabled,
		Schedule:      cronScheduleSummary(job.Schedule),
		SessionKey:    job.SessionKey,
		Model:         job.Model,
		NextRunAt:     job.NextRunAt,
		LastRunAt:     job.LastRunAt,
		Delivery:      job.Delivery,
		Notifications: &job.Notifications,
	}
	if job.LastRunAt.IsZero() && item.LastExecution == nil && job.LastStatus == "" && job.LastSummary == "" && job.LastError == "" {
		return item
	}
	item.LastExecution = &autopkg.ExecutionRecord{
		OccurredAt:          job.LastRunAt,
		Status:              job.LastStatus,
		RunID:               job.LastRunID,
		Summary:             job.LastSummary,
		Error:               job.LastError,
		VerificationStatus:  job.LastVerificationStatus,
		VerificationSummary: job.LastVerificationSummary,
	}
	return item
}

func cronScheduleSummary(schedule cron.Schedule) string {
	switch strings.TrimSpace(schedule.Kind) {
	case cron.ScheduleKindCron:
		return strings.TrimSpace(schedule.Expression)
	case cron.ScheduleKindEvery:
		return strings.TrimSpace(schedule.Every)
	case cron.ScheduleKindAt:
		return strings.TrimSpace(schedule.At)
	default:
		return "-"
	}
}

func mapWatchAutomationItem(item watch.Watch) replpkg.AutomationItem {
	return *mapSingleAutomationItem(watchAutomationProjection(item))
}

func watchAutomationProjection(item watch.Watch) autopkg.Item {
	auto := autopkg.Item{
		ID:            item.ID,
		Kind:          autopkg.KindWatch,
		Name:          item.Name,
		Enabled:       item.Enabled,
		Schedule:      strings.TrimSpace(item.Interval),
		SessionKey:    item.SessionKey,
		Model:         item.Model,
		NextRunAt:     item.NextCheckAt,
		LastRunAt:     item.LastTriggeredAt,
		Delivery:      item.Delivery,
		Notifications: &item.Notifications,
		LastExecution: &autopkg.ExecutionRecord{
			OccurredAt:          firstNonZeroTime(item.LastTriggeredAt, item.LastCheckedAt),
			Status:              item.LastStatus,
			RunID:               item.LastRunID,
			Summary:             item.LastSummary,
			Error:               item.LastError,
			VerificationStatus:  item.LastVerificationStatus,
			VerificationSummary: item.LastVerificationSummary,
		},
	}
	return auto
}

func mapWakeupAutomationItem(item wakeup.Trigger) replpkg.AutomationItem {
	return *mapSingleAutomationItem(wakeupAutomationProjection(item))
}

func wakeupAutomationProjection(item wakeup.Trigger) autopkg.Item {
	auto := autopkg.Item{
		ID:         item.ID,
		Kind:       autopkg.KindWakeup,
		Name:       item.Name,
		Enabled:    item.Enabled,
		Schedule:   strings.TrimSpace(item.Schedule),
		SessionKey: item.SessionKey,
		Model:      item.Model,
		NextRunAt:  item.NextRunAt,
		LastRunAt:  item.LastRunAt,
		LastExecution: &autopkg.ExecutionRecord{
			OccurredAt:          item.LastRunAt,
			Status:              item.LastStatus,
			RunID:               item.LastRunID,
			Summary:             item.LastSummary,
			Error:               item.LastError,
			VerificationStatus:  item.LastVerificationStatus,
			VerificationSummary: item.LastVerificationSummary,
		},
	}
	if strings.TrimSpace(item.Channel) != "" {
		auto.Delivery = &autopkg.DeliveryTarget{Channel: item.Channel, Label: item.Channel}
	}
	return auto
}

func mapHookAutomationItem(item hooks.Hook, recent []hooks.HookResult) replpkg.AutomationItem {
	return *mapSingleAutomationItem(hookAutomationProjection(item, recent))
}

func hookAutomationProjection(item hooks.Hook, recent []hooks.HookResult) autopkg.Item {
	auto := autopkg.Item{
		ID:       item.ID,
		Kind:     autopkg.KindHook,
		Name:     item.Name,
		Enabled:  item.Enabled,
		Schedule: strings.TrimSpace(string(item.Trigger)),
	}
	if len(recent) > 0 {
		latest := recent[0]
		auto.LastExecution = &autopkg.ExecutionRecord{
			OccurredAt: latest.ExecutedAt,
			Status:     latest.Status,
			RunID:      latest.RunID,
			Summary:    latest.Summary,
			Error:      latest.Error,
		}
	}
	return auto
}

func mapSingleAutomationItem(item autopkg.Item) *replpkg.AutomationItem {
	items := mapAutomationItems([]autopkg.Item{item})
	if len(items) == 0 {
		return nil
	}
	out := items[0]
	return &out
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
