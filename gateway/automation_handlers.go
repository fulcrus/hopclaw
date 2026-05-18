package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/hooks"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
)

const automationDetailRecentExecutionLimit = 10

var (
	automationErrorURLPattern    = regexp.MustCompile(`https?://\S+`)
	automationErrorHexPattern    = regexp.MustCompile(`\b[0-9a-f]{8,}\b`)
	automationErrorNumberPattern = regexp.MustCompile(`\b\d+\b`)
)

type automationItemsResponse struct {
	Items         []autopkg.Item                   `json:"items"`
	Count         int                              `json:"count"`
	Services      map[string]autopkg.ServiceStatus `json:"services"`
	Notifications autopkg.NotificationSummary      `json:"notifications"`
}

type automationTemplatesResponse struct {
	Items []autopkg.StarterTemplate `json:"items"`
	Count int                       `json:"count"`
}

type automationItemDetailResponse struct {
	Item                 autopkg.Item               `json:"item"`
	RecentExecutions     []autopkg.ExecutionRecord  `json:"recent_executions,omitempty"`
	LatestCompletion     *runtimesvc.RunCompletion  `json:"latest_completion,omitempty"`
	LatestResult         *runtimesvc.RunResult      `json:"latest_result,omitempty"`
	LatestVerification   *verifyrt.RunVerification  `json:"latest_verification,omitempty"`
	RunPath              string                     `json:"run_path,omitempty"`
	CanReplay            bool                       `json:"can_replay,omitempty"`
	LatestPayloadPreview map[string]any             `json:"latest_payload_preview,omitempty"`
	ErrorSignatures      []automationErrorSignature `json:"error_signatures,omitempty"`
}

type automationErrorSignature struct {
	Signature      string    `json:"signature"`
	Count          int       `json:"count"`
	LastOccurredAt time.Time `json:"last_occurred_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

func (g *Gateway) handleAutomationTemplates(w http.ResponseWriter, r *http.Request) {
	items := autopkg.StarterTemplates()
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" {
		filtered := make([]autopkg.StarterTemplate, 0, len(items))
		for _, item := range items {
			if string(item.Kind) == kind {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	gwJSON(w, http.StatusOK, automationTemplatesResponse{
		Items: items,
		Count: len(items),
	})
}

func (g *Gateway) handleAutomationItems(w http.ResponseWriter, r *http.Request) {
	kinds := parseAutomationKinds(r.URL.Query().Get("kinds"))
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))

	items := make([]autopkg.Item, 0)
	services := map[string]autopkg.ServiceStatus{
		string(autopkg.KindCron):   automationCronStatus(g, authScope),
		string(autopkg.KindWakeup): automationWakeupStatus(g, authScope),
		string(autopkg.KindWatch):  automationWatchStatus(g, authScope),
		string(autopkg.KindHook):   automationHookStatus(r.Context(), g, authScope),
	}

	if wantsAutomationKind(kinds, autopkg.KindCron) && g.cron != nil {
		for _, job := range g.cron.Store().List() {
			items = append(items, cronAutomationItem(job))
		}
	}
	if wantsAutomationKind(kinds, autopkg.KindWakeup) && g.wakeup != nil {
		for _, trigger := range g.wakeup.List() {
			items = append(items, wakeupAutomationItem(trigger))
		}
	}
	if wantsAutomationKind(kinds, autopkg.KindWatch) && g.watch != nil {
		for _, item := range g.watch.Store().List() {
			items = append(items, watchAutomationItem(item))
		}
	}
	if wantsAutomationKind(kinds, autopkg.KindHook) && g.hooks != nil {
		hookItems, err := g.hooks.Store().List(r.Context())
		if err == nil {
			for _, item := range hookItems {
				if item == nil || !hookMatchesAuthScope(authScope, item) {
					continue
				}
				items = append(items, hookAutomationItem(*item, g.filterHookResultsByAuthScope(r.Context(), authScope, item, g.hooks.RecentResultsByHook(item.ID, 1))))
			}
		}
	}

	gwJSON(w, http.StatusOK, automationItemsResponse{
		Items:         items,
		Count:         len(items),
		Services:      services,
		Notifications: autopkg.AggregateNotifications(items, time.Now().UTC()),
	})
}

func (g *Gateway) handleAutomationItemDetail(w http.ResponseWriter, r *http.Request) {
	kind := autopkg.Kind(strings.TrimSpace(r.PathValue("kind")))
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		gwError(w, http.StatusBadRequest, "id is required")
		return
	}
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	item, executions, status, err := g.lookupAutomationItem(r.Context(), kind, id, authScope)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}

	response := automationItemDetailResponse{
		Item:             item,
		RecentExecutions: executions,
	}
	if kind == autopkg.KindHook && g.hooks != nil {
		var hookDef *hooks.Hook
		if current, err := g.hooks.Store().Get(r.Context(), id); err == nil {
			hookDef = current
		}
		results := g.filterHookResultsByAuthScope(r.Context(), authScope, hookDef, g.hooks.RecentResultsByHook(id, automationDetailRecentExecutionLimit))
		response.CanReplay = len(results) > 0
		if len(results) > 0 && len(results[0].PayloadPreview) > 0 {
			response.LatestPayloadPreview = results[0].PayloadPreview
		}
		response.ErrorSignatures = hookErrorSignatures(results)
	}
	if item.LastExecution != nil {
		runID := strings.TrimSpace(item.LastExecution.RunID)
		if runID != "" {
			response.RunPath = "#/runs/" + url.PathEscape(runID)
		}
		if g.runtime != nil && runID != "" {
			if completion, err := g.getRunCompletionScoped(r.Context(), runID, authScope); err == nil {
				response.LatestCompletion = completion
				response.LatestResult = completion.Result
				response.LatestVerification = completion.Verification
			} else if !authScope.isZero() {
				response.RunPath = ""
			}
		}
	}
	gwJSON(w, http.StatusOK, response)
}

func parseAutomationKinds(raw string) []autopkg.Kind {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]autopkg.Kind, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, autopkg.Kind(part))
	}
	return out
}

func wantsAutomationKind(kinds []autopkg.Kind, kind autopkg.Kind) bool {
	if len(kinds) == 0 {
		return true
	}
	return slices.Contains(kinds, kind)
}

func automationCronStatus(g *Gateway, _ authScope) autopkg.ServiceStatus {
	if g.cron == nil {
		return autopkg.ServiceStatus{}
	}
	count := 0
	for range g.cron.Store().List() {
		count++
	}
	return autopkg.ServiceStatus{
		Available: true,
		Running:   g.cron.IsRunning(),
		Count:     count,
	}
}

func automationWakeupStatus(g *Gateway, _ authScope) autopkg.ServiceStatus {
	if g.wakeup == nil {
		return autopkg.ServiceStatus{}
	}
	count := 0
	for range g.wakeup.List() {
		count++
	}
	return autopkg.ServiceStatus{
		Available: true,
		Running:   g.wakeup.IsRunning(),
		Count:     count,
	}
}

func automationWatchStatus(g *Gateway, _ authScope) autopkg.ServiceStatus {
	if g.watch == nil {
		return autopkg.ServiceStatus{}
	}
	count := 0
	for range g.watch.Store().List() {
		count++
	}
	return autopkg.ServiceStatus{
		Available: true,
		Running:   g.watch.IsRunning(),
		Count:     count,
	}
}

func automationHookStatus(ctx context.Context, g *Gateway, scope authScope) autopkg.ServiceStatus {
	if g.hooks == nil {
		return autopkg.ServiceStatus{}
	}
	count := 0
	if hookItems, err := g.hooks.Store().List(ctx); err == nil {
		for _, item := range hookItems {
			if hookMatchesAuthScope(scope, item) {
				count++
			}
		}
	}
	return autopkg.ServiceStatus{
		Available: true,
		Running:   true,
		Count:     count,
	}
}

func (g *Gateway) lookupAutomationItem(ctx context.Context, kind autopkg.Kind, id string, scope authScope) (autopkg.Item, []autopkg.ExecutionRecord, int, error) {
	switch kind {
	case autopkg.KindCron:
		if g.cron == nil {
			return autopkg.Item{}, nil, http.StatusServiceUnavailable, errors.New("cron service not available")
		}
		job, err := g.cron.Store().Get(id)
		if err != nil {
			return autopkg.Item{}, nil, http.StatusNotFound, err
		}
		item := cronAutomationItem(*job)
		return item, automationRecentExecutions(item), http.StatusOK, nil
	case autopkg.KindWakeup:
		if g.wakeup == nil {
			return autopkg.Item{}, nil, http.StatusServiceUnavailable, errors.New("wakeup service not available")
		}
		trigger, err := g.wakeup.Get(id)
		if err != nil {
			return autopkg.Item{}, nil, http.StatusNotFound, err
		}
		item := wakeupAutomationItem(*trigger)
		return item, automationRecentExecutions(item), http.StatusOK, nil
	case autopkg.KindWatch:
		if g.watch == nil {
			return autopkg.Item{}, nil, http.StatusServiceUnavailable, errors.New("watch service not available")
		}
		item, err := g.watch.Store().Get(id)
		if err != nil {
			return autopkg.Item{}, nil, http.StatusNotFound, err
		}
		projection := watchAutomationItem(*item)
		return projection, automationRecentExecutions(projection), http.StatusOK, nil
	case autopkg.KindHook:
		if g.hooks == nil {
			return autopkg.Item{}, nil, http.StatusServiceUnavailable, errors.New("hooks not available")
		}
		item, err := g.hooks.Store().Get(ctx, id)
		if err != nil {
			return autopkg.Item{}, nil, http.StatusNotFound, err
		}
		if !hookMatchesAuthScope(scope, item) {
			return autopkg.Item{}, nil, http.StatusNotFound, errors.New("hook not found")
		}
		results := g.filterHookResultsByAuthScope(ctx, scope, item, g.hooks.RecentResultsByHook(id, automationDetailRecentExecutionLimit))
		projection := hookAutomationItem(*item, results)
		return projection, hookExecutionRecords(results), http.StatusOK, nil
	default:
		return autopkg.Item{}, nil, http.StatusBadRequest, errors.New("unsupported automation kind")
	}
}

func automationRecentExecutions(item autopkg.Item) []autopkg.ExecutionRecord {
	if item.LastExecution == nil {
		return nil
	}
	return []autopkg.ExecutionRecord{*item.LastExecution}
}

func hookAutomationItem(hook hooks.Hook, results []hooks.HookResult) autopkg.Item {
	item := autopkg.Item{
		ID:          hook.ID,
		Kind:        autopkg.KindHook,
		Name:        firstAutomationName(hook.Name, hook.ID),
		Enabled:     hook.Enabled,
		Schedule:    hookTriggerSummary(hook),
		SourceKind:  strings.TrimSpace(string(hook.Kind)),
		SourceLabel: hookTargetLabel(hook),
	}
	if latest := hookLatestExecution(results); latest != nil {
		item.LastRunAt = latest.OccurredAt
		item.LastExecution = latest
	}
	return item
}

func hookLatestExecution(results []hooks.HookResult) *autopkg.ExecutionRecord {
	executions := hookExecutionRecords(results)
	if len(executions) == 0 {
		return nil
	}
	return &executions[0]
}

func hookExecutionRecords(results []hooks.HookResult) []autopkg.ExecutionRecord {
	if len(results) == 0 {
		return nil
	}
	out := make([]autopkg.ExecutionRecord, 0, len(results))
	for _, result := range results {
		summary := strings.TrimSpace(result.Summary)
		if summary == "" {
			summary = strings.TrimSpace(string(result.Trigger))
			if phase := strings.TrimSpace(string(result.Phase)); phase != "" {
				summary = firstAutomationName(summary+" · "+phase, summary)
			}
			if result.Duration > 0 {
				summary = firstAutomationName(summary+" · "+result.Duration.String(), summary)
			}
		}
		out = append(out, autopkg.ExecutionRecord{
			OccurredAt:  result.ExecutedAt,
			Status:      strings.TrimSpace(result.Status),
			RunID:       strings.TrimSpace(result.RunID),
			SessionID:   strings.TrimSpace(result.SessionID),
			ToolName:    strings.TrimSpace(result.ToolName),
			TargetLabel: strings.TrimSpace(result.TargetLabel),
			Summary:     summary,
			Error:       strings.TrimSpace(result.Error),
		})
	}
	return out
}

func hookTriggerSummary(hook hooks.Hook) string {
	trigger := strings.TrimSpace(string(hook.Trigger))
	phase := strings.TrimSpace(string(hook.EffectivePhase()))
	switch {
	case trigger != "" && phase != "":
		return trigger + " · " + phase
	case trigger != "":
		return trigger
	default:
		return phase
	}
}

func hookTargetLabel(hook hooks.Hook) string {
	switch hook.Kind {
	case hooks.KindHTTP:
		return strings.TrimSpace(hook.URL)
	case hooks.KindCommand:
		return strings.TrimSpace(hook.Command)
	default:
		return ""
	}
}

func hookErrorSignatures(results []hooks.HookResult) []automationErrorSignature {
	if len(results) == 0 {
		return nil
	}
	type aggregate struct {
		signature automationErrorSignature
	}
	bySignature := make(map[string]*aggregate)
	order := make([]string, 0)
	for _, result := range results {
		errText := strings.TrimSpace(result.Error)
		if errText == "" {
			continue
		}
		signature := normalizeAutomationErrorSignature(errText)
		entry := bySignature[signature]
		if entry == nil {
			entry = &aggregate{
				signature: automationErrorSignature{
					Signature: signature,
					LastError: errText,
				},
			}
			bySignature[signature] = entry
			order = append(order, signature)
		}
		entry.signature.Count++
		if result.ExecutedAt.After(entry.signature.LastOccurredAt) {
			entry.signature.LastOccurredAt = result.ExecutedAt
			entry.signature.LastError = errText
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]automationErrorSignature, 0, len(order))
	for _, signature := range order {
		out = append(out, bySignature[signature].signature)
	}
	slices.SortFunc(out, func(a, b automationErrorSignature) int {
		switch {
		case a.Count != b.Count:
			if a.Count > b.Count {
				return -1
			}
			return 1
		case a.LastOccurredAt.After(b.LastOccurredAt):
			return -1
		case a.LastOccurredAt.Before(b.LastOccurredAt):
			return 1
		default:
			return strings.Compare(a.Signature, b.Signature)
		}
	})
	return out
}

func normalizeAutomationErrorSignature(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = automationErrorURLPattern.ReplaceAllString(value, "<url>")
	value = automationErrorHexPattern.ReplaceAllString(value, "<id>")
	value = automationErrorNumberPattern.ReplaceAllString(value, "<num>")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func cronAutomationItem(job cron.Job) autopkg.Item {
	item := autopkg.Item{
		ID:            job.ID,
		Kind:          autopkg.KindCron,
		Name:          firstAutomationName(job.Name, job.ID),
		Enabled:       job.Enabled,
		Schedule:      cronScheduleSummary(job.Schedule),
		Channel:       deliveryChannel(job.Delivery),
		SessionKey:    strings.TrimSpace(job.SessionKey),
		Model:         strings.TrimSpace(job.Model),
		PromptPreview: strings.TrimSpace(job.Payload.Content),
		NextRunAt:     job.NextRunAt,
		LastRunAt:     job.LastRunAt,
	}
	if job.Delivery != nil {
		item.Delivery = &autopkg.DeliveryTarget{
			Channel: strings.TrimSpace(job.Delivery.Channel),
			Target:  strings.TrimSpace(job.Delivery.Target),
		}
	}
	if job.Notifications.Populated() {
		stats := job.Notifications
		item.Notifications = &stats
	}
	if !job.LastRunAt.IsZero() || strings.TrimSpace(job.LastStatus) != "" || strings.TrimSpace(job.LastError) != "" {
		item.LastExecution = &autopkg.ExecutionRecord{
			OccurredAt:          job.LastRunAt,
			Status:              strings.TrimSpace(job.LastStatus),
			RunID:               strings.TrimSpace(job.LastRunID),
			Summary:             strings.TrimSpace(job.LastSummary),
			Error:               strings.TrimSpace(job.LastError),
			VerificationStatus:  strings.TrimSpace(job.LastVerificationStatus),
			VerificationSummary: strings.TrimSpace(job.LastVerificationSummary),
		}
	}
	return item
}

func wakeupAutomationItem(trigger wakeup.Trigger) autopkg.Item {
	item := autopkg.Item{
		ID:            trigger.ID,
		Kind:          autopkg.KindWakeup,
		Name:          firstAutomationName(trigger.Name, trigger.ID),
		Enabled:       trigger.Enabled,
		Schedule:      strings.TrimSpace(trigger.Schedule),
		Channel:       strings.TrimSpace(trigger.Channel),
		Message:       strings.TrimSpace(trigger.Message),
		SessionKey:    strings.TrimSpace(trigger.SessionKey),
		Model:         strings.TrimSpace(trigger.Model),
		PromptPreview: strings.TrimSpace(trigger.Message),
		NextRunAt:     trigger.NextRunAt,
		LastRunAt:     trigger.LastRunAt,
	}
	if !trigger.LastRunAt.IsZero() || strings.TrimSpace(trigger.LastStatus) != "" || strings.TrimSpace(trigger.LastError) != "" || strings.TrimSpace(trigger.LastRunID) != "" {
		item.LastExecution = &autopkg.ExecutionRecord{
			OccurredAt:          trigger.LastRunAt,
			Status:              strings.TrimSpace(trigger.LastStatus),
			RunID:               strings.TrimSpace(trigger.LastRunID),
			Error:               strings.TrimSpace(trigger.LastError),
			Summary:             wakeupSummary(trigger),
			VerificationStatus:  strings.TrimSpace(trigger.LastVerificationStatus),
			VerificationSummary: strings.TrimSpace(trigger.LastVerificationSummary),
		}
	}
	return item
}

func watchAutomationItem(item watch.Watch) autopkg.Item {
	result := autopkg.Item{
		ID:            item.ID,
		Kind:          autopkg.KindWatch,
		Name:          firstAutomationName(item.Name, item.ID),
		Enabled:       item.Enabled,
		Schedule:      strings.TrimSpace(item.Interval),
		Channel:       watchDeliveryChannel(item.Delivery),
		SessionKey:    strings.TrimSpace(item.SessionKey),
		Model:         strings.TrimSpace(item.Model),
		SourceKind:    strings.TrimSpace(item.Source.Kind),
		SourceLabel:   watchSourceLabel(item.Source),
		PromptPreview: strings.TrimSpace(item.Prompt),
		NextRunAt:     item.NextCheckAt,
		LastRunAt:     firstNonZeroTime(item.LastTriggeredAt, item.LastCheckedAt),
	}
	if item.Delivery != nil {
		result.Delivery = &autopkg.DeliveryTarget{
			Channel: strings.TrimSpace(item.Delivery.Channel),
			Target:  strings.TrimSpace(item.Delivery.Target),
		}
	}
	if item.Notifications.Populated() {
		stats := item.Notifications
		result.Notifications = &stats
	}
	if !item.LastCheckedAt.IsZero() || strings.TrimSpace(item.LastStatus) != "" || strings.TrimSpace(item.LastError) != "" {
		result.LastExecution = &autopkg.ExecutionRecord{
			OccurredAt:          firstNonZeroTime(item.LastTriggeredAt, item.LastCheckedAt),
			Status:              strings.TrimSpace(item.LastStatus),
			RunID:               strings.TrimSpace(item.LastRunID),
			Summary:             strings.TrimSpace(item.LastSummary),
			Error:               strings.TrimSpace(item.LastError),
			VerificationStatus:  strings.TrimSpace(item.LastVerificationStatus),
			VerificationSummary: strings.TrimSpace(item.LastVerificationSummary),
		}
	}
	return result
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
		if expr := strings.TrimSpace(schedule.Expression); expr != "" {
			return expr
		}
		if every := strings.TrimSpace(schedule.Every); every != "" {
			return every
		}
		return strings.TrimSpace(schedule.At)
	}
}

func watchSourceLabel(source watch.Source) string {
	switch source.Kind {
	case watch.SourceKindHTTP:
		if source.HTTP != nil {
			return strings.TrimSpace(source.HTTP.URL)
		}
	case watch.SourceKindFile:
		if source.File != nil {
			return strings.TrimSpace(source.File.Path)
		}
	case watch.SourceKindFeed:
		if source.Feed != nil {
			return strings.TrimSpace(source.Feed.URL)
		}
	case watch.SourceKindBrowserSnapshot:
		if source.BrowserSnapshot != nil {
			return strings.TrimSpace(source.BrowserSnapshot.URL)
		}
	case watch.SourceKindCalendar:
		if source.Calendar != nil {
			return strings.TrimSpace(source.Calendar.Query)
		}
	case watch.SourceKindMailbox:
		if source.Mailbox != nil {
			parts := make([]string, 0, 2)
			if folder := strings.TrimSpace(source.Mailbox.Folder); folder != "" {
				parts = append(parts, folder)
			}
			if query := strings.TrimSpace(source.Mailbox.Query); query != "" {
				parts = append(parts, query)
			}
			return strings.Join(parts, " ")
		}
	case watch.SourceKindWebhook:
		if source.Webhook != nil {
			if key := strings.TrimSpace(source.Webhook.SessionKey); key != "" {
				return key
			}
			if id := strings.TrimSpace(source.Webhook.WebhookID); id != "" {
				return id
			}
		}
	case watch.SourceKindStructuredInbox:
		if source.StructuredInbox != nil {
			return strings.TrimSpace(source.StructuredInbox.SessionKey)
		}
	}
	return ""
}

func firstAutomationName(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "-"
}

func deliveryChannel(delivery *cron.Delivery) string {
	if delivery == nil {
		return ""
	}
	return strings.TrimSpace(delivery.Channel)
}

func watchDeliveryChannel(delivery *watch.DeliveryTarget) string {
	if delivery == nil {
		return ""
	}
	return strings.TrimSpace(delivery.Channel)
}

func wakeupSummary(trigger wakeup.Trigger) string {
	if strings.TrimSpace(trigger.LastStatus) == "error" {
		return ""
	}
	if text := strings.TrimSpace(trigger.LastSummary); text != "" {
		return text
	}
	if text := strings.TrimSpace(trigger.Message); text != "" {
		return text
	}
	return "wakeup trigger submitted"
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (g *Gateway) getRunCompletionScoped(ctx context.Context, runID string, scope authScope) (*runtimesvc.RunCompletion, error) {
	if g == nil || g.runtime == nil {
		return nil, errors.New("runtime not available")
	}
	if scope.isZero() {
		return g.runtime.GetRunCompletion(ctx, runID)
	}
	run, err := g.runtime.GetRunScoped(ctx, runID, scope.scopeFilter())
	if err != nil || run == nil {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	return g.runtime.GetRunCompletion(ctx, runID)
}
