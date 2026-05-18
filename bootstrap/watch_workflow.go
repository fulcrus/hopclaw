package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/watch"
)

type bootstrapWatchWorkflow struct {
	service *watch.Service
	nextID  atomic.Uint64
}

func requestWatchWorkflowServiceRearm(service *watch.Service) {
	if service == nil {
		return
	}
	service.Rearm()
}

func runWatchWorkflowPostCommitActions(actions ...func()) {
	for _, action := range actions {
		if action != nil {
			action()
		}
	}
}

var bootstrapWatchCancelTargetPattern = regexp.MustCompile(`(?:https?://[^\s]+)|(?:[A-Za-z0-9.-]+\.[A-Za-z]{2,})|(?:/[\w.\-~/]+)`)

func newBootstrapWatchWorkflow(service *watch.Service) *bootstrapWatchWorkflow {
	if service == nil {
		return nil
	}
	workflow := &bootstrapWatchWorkflow{service: service}
	workflow.nextID.Store(maxExistingBootstrapWatchID(service.Store()))
	return workflow
}

func (w *bootstrapWatchWorkflow) Create(ctx context.Context, req agent.WatchWorkflowRequest) (*agent.WatchWorkflowResult, error) {
	if w == nil || w.service == nil || w.service.Store() == nil {
		return nil, fmt.Errorf("watch workflow is not configured")
	}
	sourceKind := strings.TrimSpace(req.SourceKind)
	sourceURL := strings.TrimSpace(req.SourceURL)
	sourcePath := strings.TrimSpace(req.SourcePath)
	sourceSessionKey := strings.TrimSpace(req.SourceSessionKey)
	calendarQuery := strings.TrimSpace(req.CalendarQuery)
	mailboxFolder := strings.TrimSpace(req.MailboxFolder)
	mailboxQuery := strings.TrimSpace(req.MailboxQuery)
	webhookID := strings.TrimSpace(req.WebhookID)
	webhookSenderID := strings.TrimSpace(req.WebhookSenderID)
	if sourceKind == "" {
		switch {
		case sourceURL != "":
			sourceKind = watch.SourceKindHTTP
		case sourcePath != "":
			sourceKind = watch.SourceKindFile
		case sourceSessionKey != "":
			sourceKind = watch.SourceKindStructuredInbox
		case calendarQuery != "":
			sourceKind = watch.SourceKindCalendar
		case webhookID != "" && webhookSenderID != "":
			sourceKind = watch.SourceKindWebhook
		case mailboxFolder != "":
			sourceKind = watch.SourceKindMailbox
		}
	}
	if sourceKind == "" {
		return nil, fmt.Errorf("watch source is required")
	}
	now := time.Now().UTC()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Monitoring Job"
	}
	interval := strings.TrimSpace(req.Interval)
	if interval == "" {
		interval = "1h"
	}
	item := watch.Watch{
		Name:        name,
		Enabled:     true,
		Interval:    interval,
		SessionKey:  strings.TrimSpace(req.SessionKey),
		Model:       strings.TrimSpace(req.Model),
		Prompt:      strings.TrimSpace(req.Prompt),
		FireOnStart: req.FireOnStart,
		Source:      watch.Source{Kind: sourceKind},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	switch sourceKind {
	case watch.SourceKindHTTP:
		item.Source.HTTP = &watch.HTTPSource{URL: sourceURL}
	case watch.SourceKindFile:
		item.Source.File = &watch.FileSource{Path: sourcePath}
	case watch.SourceKindFeed:
		item.Source.Feed = &watch.FeedSource{URL: sourceURL}
	case watch.SourceKindCalendar:
		item.Source.Calendar = &watch.CalendarSource{Query: calendarQuery}
	case watch.SourceKindMailbox:
		item.Source.Mailbox = &watch.MailboxSource{Folder: mailboxFolder, Query: mailboxQuery}
	case watch.SourceKindWebhook:
		item.Source.Webhook = &watch.WebhookSource{WebhookID: webhookID, SenderID: webhookSenderID, SessionKey: sourceSessionKey, Limit: req.InboxLimit}
	case watch.SourceKindStructuredInbox:
		item.Source.StructuredInbox = &watch.StructuredInboxSource{SessionKey: sourceSessionKey, Limit: req.InboxLimit}
	case watch.SourceKindBrowserSnapshot:
		item.Source.BrowserSnapshot = &watch.BrowserSnapshotSource{URL: sourceURL}
	default:
		return nil, fmt.Errorf("unsupported watch source kind: %s", sourceKind)
	}
	if err := watch.Validate(item); err != nil {
		return nil, err
	}
	for {
		item.ID = fmt.Sprintf("watch-%06d", w.nextID.Add(1))
		if err := w.service.Store().Add(item); err != nil {
			if errors.Is(err, watch.ErrDuplicateID) {
				continue
			}
			return nil, err
		}
		break
	}
	if err := w.service.Store().Save(); err != nil {
		return nil, err
	}
	runWatchWorkflowPostCommitActions(func() {
		requestWatchWorkflowServiceRearm(w.service)
	})
	return &agent.WatchWorkflowResult{
		WatchID:          item.ID,
		Name:             name,
		SourceKind:       sourceKind,
		SourceURL:        sourceURL,
		SourcePath:       sourcePath,
		SourceSessionKey: sourceSessionKey,
		CalendarQuery:    calendarQuery,
		MailboxFolder:    mailboxFolder,
		MailboxQuery:     mailboxQuery,
		WebhookID:        webhookID,
		WebhookSenderID:  webhookSenderID,
		InboxLimit:       req.InboxLimit,
		Interval:         interval,
		Summary:          fmt.Sprintf("Monitoring is set up for %s. I will check %s every %s.", watchSourceSummaryLabel(item.Source), name, interval),
	}, nil
}

func (w *bootstrapWatchWorkflow) Cancel(_ context.Context, req agent.WatchWorkflowCancelRequest) (*agent.WatchWorkflowCancelResult, error) {
	if w == nil || w.service == nil || w.service.Store() == nil {
		return nil, fmt.Errorf("watch workflow is not configured")
	}
	query := strings.TrimSpace(req.Query)
	sessionKey := strings.TrimSpace(req.SessionKey)
	target := strings.TrimSpace(req.TargetRef)
	if target == "" {
		target = bootstrapWatchCancelTarget(query)
	}
	removeAll := req.RemoveAll

	candidates := bootstrapWatchCancelCandidates(w.service.Store().List(), sessionKey, target)
	if len(candidates) == 0 {
		return &agent.WatchWorkflowCancelResult{
			Summary: bootstrapWatchCancelSummary(query, nil),
		}, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})

	removed := make([]string, 0, len(candidates))
	for idx, item := range candidates {
		if !removeAll && idx > 0 {
			break
		}
		if err := w.service.Store().Remove(item.ID); err != nil {
			return nil, err
		}
		removed = append(removed, item.ID)
	}
	if len(removed) > 0 {
		if err := w.service.Store().Save(); err != nil {
			return nil, err
		}
		runWatchWorkflowPostCommitActions(func() {
			requestWatchWorkflowServiceRearm(w.service)
		})
	}
	return &agent.WatchWorkflowCancelResult{
		RemovedWatchIDs: removed,
		Summary:         bootstrapWatchCancelSummary(query, removed),
	}, nil
}

func watchSourceSummaryLabel(source watch.Source) string {
	switch strings.TrimSpace(source.Kind) {
	case watch.SourceKindHTTP:
		if source.HTTP != nil {
			return strings.TrimSpace(source.HTTP.URL)
		}
	case watch.SourceKindFile:
		if source.File != nil {
			if abs, err := filepath.Abs(strings.TrimSpace(source.File.Path)); err == nil {
				return abs
			}
			return strings.TrimSpace(source.File.Path)
		}
	case watch.SourceKindFeed:
		if source.Feed != nil {
			return strings.TrimSpace(source.Feed.URL)
		}
	case watch.SourceKindCalendar:
		if source.Calendar != nil && strings.TrimSpace(source.Calendar.Query) != "" {
			return "calendar query=" + strings.TrimSpace(source.Calendar.Query)
		}
		return "calendar"
	case watch.SourceKindMailbox:
		if source.Mailbox != nil {
			folder := strings.TrimSpace(source.Mailbox.Folder)
			if folder == "" {
				folder = "INBOX"
			}
			if query := strings.TrimSpace(source.Mailbox.Query); query != "" {
				return folder + " query=" + query
			}
			return folder
		}
	case watch.SourceKindBrowserSnapshot:
		if source.BrowserSnapshot != nil {
			return strings.TrimSpace(source.BrowserSnapshot.URL)
		}
	case watch.SourceKindWebhook:
		if source.Webhook != nil {
			if sessionKey := strings.TrimSpace(source.Webhook.SessionKey); sessionKey != "" {
				return sessionKey
			}
			if webhookID := strings.TrimSpace(source.Webhook.WebhookID); webhookID != "" {
				if senderID := strings.TrimSpace(source.Webhook.SenderID); senderID != "" {
					return "webhook:" + webhookID + ":" + senderID
				}
				return "webhook:" + webhookID
			}
		}
	case watch.SourceKindStructuredInbox:
		if source.StructuredInbox != nil {
			return strings.TrimSpace(source.StructuredInbox.SessionKey)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "the selected source"
}

func maxExistingBootstrapWatchID(store *watch.Store) uint64 {
	if store == nil {
		return 0
	}
	var maxID uint64
	for _, item := range store.List() {
		if id := parseBootstrapWatchID(item.ID); id > maxID {
			maxID = id
		}
	}
	return maxID
}

func parseBootstrapWatchID(raw string) uint64 {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "watch-") {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(raw, "watch-"), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func bootstrapWatchCancelCandidates(items []watch.Watch, sessionKey, target string) []watch.Watch {
	sessionKey = strings.TrimSpace(sessionKey)
	target = strings.ToLower(strings.TrimSpace(target))
	if sessionKey != "" {
		sessionScoped := make([]watch.Watch, 0, len(items))
		for _, item := range items {
			if !strings.EqualFold(strings.TrimSpace(item.SessionKey), sessionKey) {
				continue
			}
			if target != "" && !bootstrapWatchMatchesCancelTarget(item, target) {
				continue
			}
			sessionScoped = append(sessionScoped, item)
		}
		if len(sessionScoped) > 0 || target == "" {
			return sessionScoped
		}
	}

	if target == "" {
		return nil
	}

	out := make([]watch.Watch, 0, len(items))
	for _, item := range items {
		if bootstrapWatchMatchesCancelTarget(item, target) {
			out = append(out, item)
		}
	}
	return out
}

func bootstrapWatchMatchesCancelTarget(item watch.Watch, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	values := []string{
		item.ID,
		item.Name,
		item.SessionKey,
		strings.TrimSpace(watchSourceSummaryLabel(item.Source)),
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), target) {
			return true
		}
	}
	return false
}

func bootstrapWatchCancelTarget(query string) string {
	match := strings.TrimSpace(bootstrapWatchCancelTargetPattern.FindString(strings.TrimSpace(query)))
	if match == "" {
		return ""
	}
	if parsed, err := url.Parse(match); err == nil && parsed != nil && parsed.Host != "" {
		return strings.ToLower(strings.TrimSpace(parsed.Host))
	}
	return strings.ToLower(match)
}

func bootstrapWatchCancelSummary(query string, removed []string) string {
	if len(removed) == 0 {
		if containsChinese(query) {
			return "我没有找到可取消的监控任务。"
		}
		return "I couldn't find a matching monitoring job to cancel."
	}
	if containsChinese(query) {
		if len(removed) == 1 {
			return fmt.Sprintf("已取消监控任务 `%s`。", removed[0])
		}
		return fmt.Sprintf("已取消 %d 个监控任务：%s。", len(removed), strings.Join(removed, "、"))
	}
	if len(removed) == 1 {
		return fmt.Sprintf("Cancelled monitoring job `%s`.", removed[0])
	}
	return fmt.Sprintf("Cancelled %d monitoring jobs: %s.", len(removed), strings.Join(removed, ", "))
}

func containsChinese(text string) bool {
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
