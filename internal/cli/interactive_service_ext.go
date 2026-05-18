package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

const (
	interactiveSupervisorLimit       = 32
	interactiveMemoryListLimit       = 256
	interactiveAutomationDetailLimit = 10
)

func (g *externalInteractiveGateway) SupervisorSnapshot(ctx context.Context) (*replpkg.SupervisorSnapshot, error) {
	items, err := loadRunViewsFiltered(ctx, g.client, agent.RunListFilter{Limit: interactiveSupervisorLimit}, runtimesvc.RunListViewOptions{
		IncludeVerification: true,
	})
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return buildSupervisorSnapshot(mapRunSummaries(items), ""), nil
}

func (g *externalInteractiveGateway) GetRunDelivery(ctx context.Context, runID string) (*replpkg.RunDeliveryDetail, error) {
	completion, err := loadRunCompletion(ctx, g.client, runID)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapRunDeliveryDetail(completion), nil
}

func (g *externalInteractiveGateway) ListGovernanceDeliveries(ctx context.Context, query string, limit int) ([]replpkg.DeliveryListItem, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		values.Set("q", trimmed)
	}
	var resp struct {
		Items []*runtimesvc.GovernanceDeliveryView `json:"items"`
		Count int                                  `json:"count"`
	}
	if err := g.client.Get(ctx, "/runtime/governance/deliveries?"+values.Encode(), &resp); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return mapGovernanceDeliveryItems(resp.Items), nil
}

func (g *externalInteractiveGateway) RedriveDelivery(ctx context.Context, id string) (*replpkg.RedriveResult, error) {
	if g == nil || g.client == nil || strings.TrimSpace(id) == "" {
		return nil, nil
	}
	var resp runtimesvc.GovernanceRedriveResult
	path := "/runtime/governance/deliveries/" + url.PathEscape(strings.TrimSpace(id)) + "/redrive"
	if err := g.client.Post(ctx, path, nil, &resp); gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return mapGovernanceRedriveResult(&resp), nil
}

func (g *externalInteractiveGateway) ListMemoryConflicts(ctx context.Context) ([]agent.MemoryEntry, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	items, err := listMemoryEntries(ctx, g.client, "", interactiveMemoryListLimit)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return filterMemoryEntries(items, func(entry agent.MemoryEntry) bool {
		return strings.TrimSpace(entry.ConflictWith) != ""
	}), nil
}

func (g *externalInteractiveGateway) ListPendingMemoryWrites(ctx context.Context) ([]agent.MemoryEntry, error) {
	if g == nil || g.client == nil {
		return nil, nil
	}
	items, err := listMemoryEntries(ctx, g.client, "", interactiveMemoryListLimit)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return filterMemoryEntries(items, func(entry agent.MemoryEntry) bool {
		return entry.PendingWrite
	}), nil
}

func (g *externalInteractiveGateway) ResolveMemoryConflict(context.Context, string, string) error {
	return fmt.Errorf("memory conflict resolution is not supported on this runtime yet")
}

func (g *externalInteractiveGateway) RecoveryCandidates(ctx context.Context) ([]replpkg.RecoveryCandidate, error) {
	items, err := loadRunViewsFiltered(ctx, g.client, agent.RunListFilter{
		Status: agent.RunCancelled,
		Limit:  interactiveSupervisorLimit,
	}, runtimesvc.RunListViewOptions{IncludeVerification: true})
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mapRecoveryCandidates(items), nil
}

func (g *externalInteractiveGateway) MemoryUsedInContext(ctx context.Context, sessionID string) ([]replpkg.MemoryUsageItem, error) {
	if g == nil || g.client == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	detail, err := fetchSessionDetail(ctx, g.client, strings.TrimSpace(sessionID))
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil || detail == nil {
		return nil, err
	}
	entries, err := listMemoryEntries(ctx, g.client, "", interactiveMemoryListLimit)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items, _ := buildMemoryUsageItems(entries, detail.Summary.Key, "")
	return items, nil
}

func (g *externalInteractiveGateway) ContextPressure(ctx context.Context, sessionID string) (*replpkg.ContextPressureInfo, error) {
	if g == nil || g.client == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	detail, err := fetchSessionDetail(ctx, g.client, strings.TrimSpace(sessionID))
	if gatewayErrorStatus(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil || detail == nil {
		return nil, err
	}
	models, err := fetchModelInfo(ctx, g.client)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		models = nil
		err = nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := listMemoryEntries(ctx, g.client, "", interactiveMemoryListLimit)
	if gatewayErrorStatus(err) == http.StatusNotFound {
		entries = nil
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return buildContextPressure(detail, models, entries), nil
}

func (g *embeddedInteractiveGateway) SupervisorSnapshot(ctx context.Context) (*replpkg.SupervisorSnapshot, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	items, err := g.app.Runtime.ListRunViews(ctx, agent.RunListFilter{
		Limit: interactiveSupervisorLimit,
	}, runtimesvc.RunListViewOptions{IncludeVerification: true})
	if err != nil {
		return nil, err
	}
	return buildSupervisorSnapshot(mapRunSummaries(items), ""), nil
}

func (g *embeddedInteractiveGateway) GetRunDelivery(ctx context.Context, runID string) (*replpkg.RunDeliveryDetail, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	completion, err := g.app.Runtime.GetRunCompletion(ctx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	return mapRunDeliveryDetail(completion), nil
}

func (g *embeddedInteractiveGateway) ListGovernanceDeliveries(ctx context.Context, query string, limit int) ([]replpkg.DeliveryListItem, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	items, err := g.app.Runtime.ListGovernanceDeliveries(ctx, runtimesvc.GovernanceDeliveryFilter{
		Query: strings.TrimSpace(query),
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return mapGovernanceDeliveryItems(items), nil
}

func (g *embeddedInteractiveGateway) RedriveDelivery(ctx context.Context, id string) (*replpkg.RedriveResult, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil || strings.TrimSpace(id) == "" {
		return nil, nil
	}
	result, err := g.app.Runtime.RedriveGovernanceDeliveries(ctx, runtimesvc.GovernanceRedriveRequest{
		IDs: []string{strings.TrimSpace(id)},
	})
	if err != nil {
		return nil, err
	}
	return mapGovernanceRedriveResult(result), nil
}

func (g *embeddedInteractiveGateway) ListMemoryConflicts(ctx context.Context) ([]agent.MemoryEntry, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	items, err := g.app.Runtime.ListMemory(ctx)
	if err != nil {
		return nil, err
	}
	return filterMemoryEntries(items, func(entry agent.MemoryEntry) bool {
		return strings.TrimSpace(entry.ConflictWith) != ""
	}), nil
}

func (g *embeddedInteractiveGateway) ListPendingMemoryWrites(ctx context.Context) ([]agent.MemoryEntry, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	items, err := g.app.Runtime.ListMemory(ctx)
	if err != nil {
		return nil, err
	}
	return filterMemoryEntries(items, func(entry agent.MemoryEntry) bool {
		return entry.PendingWrite
	}), nil
}

func (g *embeddedInteractiveGateway) ResolveMemoryConflict(context.Context, string, string) error {
	return fmt.Errorf("memory conflict resolution is not supported on this runtime yet")
}

func (g *embeddedInteractiveGateway) RecoveryCandidates(ctx context.Context) ([]replpkg.RecoveryCandidate, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil {
		return nil, nil
	}
	items, err := g.app.Runtime.ListRunViews(ctx, agent.RunListFilter{
		Status: agent.RunCancelled,
		Limit:  interactiveSupervisorLimit,
	}, runtimesvc.RunListViewOptions{IncludeVerification: true})
	if err != nil {
		return nil, err
	}
	return mapRecoveryCandidates(items), nil
}

func (g *embeddedInteractiveGateway) MemoryUsedInContext(ctx context.Context, sessionID string) ([]replpkg.MemoryUsageItem, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	session, err := g.app.Runtime.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil || session == nil {
		return nil, err
	}
	entries, err := g.app.Runtime.ListMemory(ctx)
	if err != nil {
		return nil, err
	}
	items, _ := buildMemoryUsageItems(entries, session.Key, "")
	return items, nil
}

func (g *embeddedInteractiveGateway) ContextPressure(ctx context.Context, sessionID string) (*replpkg.ContextPressureInfo, error) {
	if g == nil || g.app == nil || g.app.Runtime == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	session, err := g.app.Runtime.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil || session == nil {
		return nil, err
	}
	detail := mapSessionDetail(session)
	if detail == nil {
		return nil, nil
	}
	entries, err := g.app.Runtime.ListMemory(ctx)
	if err != nil {
		return nil, err
	}
	return buildContextPressure(detail, embeddedModelInfo(g.app), entries), nil
}

func loadRunViewsFiltered(ctx context.Context, client *GatewayClient, filter agent.RunListFilter, options runtimesvc.RunListViewOptions) ([]*runtimesvc.RunListView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	if filter.Limit <= 0 {
		filter.Limit = messageDefaultLimit
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(filter.Limit))
	if filter.SessionID = strings.TrimSpace(filter.SessionID); filter.SessionID != "" {
		values.Set("session_id", filter.SessionID)
	}
	if filter.Status = agent.RunStatus(strings.TrimSpace(string(filter.Status))); filter.Status != "" {
		values.Set("status", string(filter.Status))
	}
	include := make([]string, 0, 2)
	if options.IncludeVerification {
		include = append(include, "verification")
	}
	if options.IncludeExecutionGraph {
		include = append(include, "execution_graph")
	}
	if len(include) > 0 {
		values.Set("include", strings.Join(include, ","))
	}
	var resp struct {
		Items []*runtimesvc.RunListView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := client.Get(ctx, "/runtime/runs?"+values.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func loadRunCompletion(ctx context.Context, client *GatewayClient, runID string) (*runtimesvc.RunCompletion, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var completion runtimesvc.RunCompletion
	if err := client.Get(ctx, "/runtime/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/completion", &completion); err != nil {
		return nil, err
	}
	return &completion, nil
}

func buildSupervisorSnapshot(items []replpkg.RunSummary, foregroundRunID string) *replpkg.SupervisorSnapshot {
	if len(items) == 0 {
		return &replpkg.SupervisorSnapshot{}
	}
	out := append([]replpkg.RunSummary(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if rankI, rankJ := supervisorItemRank(out[i]), supervisorItemRank(out[j]); rankI != rankJ {
			return rankI < rankJ
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})

	activeCount := 0
	pausedCount := 0
	attentionCount := 0
	for _, item := range out {
		if supervisorIsActive(item) {
			activeCount++
		}
		if supervisorIsPaused(item) {
			pausedCount++
		}
		if supervisorNeedsAttention(item) {
			attentionCount++
		}
	}
	foregroundRunID = strings.TrimSpace(foregroundRunID)
	if foregroundRunID == "" {
		for _, item := range out {
			if supervisorIsActive(item) {
				foregroundRunID = item.ID
				break
			}
		}
	}
	backgroundCount := activeCount
	if foregroundRunID != "" && backgroundCount > 0 {
		backgroundCount--
	}
	return &replpkg.SupervisorSnapshot{
		ForegroundRunID:    foregroundRunID,
		ActiveRunCount:     activeCount,
		BackgroundRunCount: max(backgroundCount, 0),
		PausedRunCount:     pausedCount,
		AttentionCount:     attentionCount,
		Items:              out,
	}
}

func supervisorIsActive(item replpkg.RunSummary) bool {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	switch status {
	case "queued", "waiting_input", "waiting approval", "waiting_approval", "running", "streaming":
		return true
	}
	phase := strings.ToLower(strings.TrimSpace(item.Phase))
	switch phase {
	case "thinking", "planning", "executing", "executing tools", "processing results", "delivering", "waiting approval":
		return true
	}
	return false
}

func supervisorIsPaused(item replpkg.RunSummary) bool {
	if item.Resumable {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	phase := strings.ToLower(strings.TrimSpace(item.Phase))
	return status == "paused" || phase == "paused"
}

func supervisorNeedsAttention(item replpkg.RunSummary) bool {
	return strings.TrimSpace(item.Attention) != ""
}

func supervisorItemRank(item replpkg.RunSummary) int {
	switch {
	case supervisorIsActive(item) && supervisorNeedsAttention(item):
		return 0
	case supervisorIsActive(item):
		return 1
	case supervisorIsPaused(item) && supervisorNeedsAttention(item):
		return 2
	case supervisorIsPaused(item):
		return 3
	case supervisorNeedsAttention(item):
		return 4
	default:
		return 5
	}
}

func mapRecoveryCandidates(items []*runtimesvc.RunListView) []replpkg.RecoveryCandidate {
	out := make([]replpkg.RecoveryCandidate, 0, len(items))
	for _, item := range items {
		if item == nil || item.WorkflowState == nil || !item.WorkflowState.NeedsContinuation() {
			continue
		}
		summary := strings.TrimSpace(item.Error)
		if summary == "" {
			summary = strings.TrimSpace(string(item.Phase))
		}
		if summary == "" {
			summary = "paused work is resumable"
		}
		out = append(out, replpkg.RecoveryCandidate{
			Type:    "paused_run",
			ID:      strings.TrimSpace(item.ID),
			Summary: summary,
			Action:  "continue",
		})
	}
	return out
}

func mapRunDeliveryDetail(completion *runtimesvc.RunCompletion) *replpkg.RunDeliveryDetail {
	if completion == nil {
		return nil
	}
	detail := &replpkg.RunDeliveryDetail{
		Status:  strings.TrimSpace(string(completion.Status)),
		Summary: "",
	}
	if completion.Delivery != nil {
		detail.Summary = strings.TrimSpace(completion.Delivery.Summary)
	}
	if detail.Summary == "" && completion.Result != nil {
		detail.Summary = strings.TrimSpace(completion.Result.Summary)
	}
	if detail.Status == "" && completion.Result != nil {
		detail.Status = strings.TrimSpace(string(completion.Result.Status))
	}
	if detail.Status == "" {
		detail.Status = "unknown"
	}

	targetByKey := map[string]int{}
	for _, receipt := range completion.Receipts {
		label := firstNonEmpty(strings.TrimSpace(receipt.AdapterName), strings.TrimSpace(receipt.Kind), "terminal inline")
		kind := firstNonEmpty(strings.TrimSpace(receipt.Kind), "inline")
		key := kind + "\x00" + label
		target := replpkg.DeliveryTarget{
			Kind:     kind,
			Label:    label,
			Status:   firstNonEmpty(strings.TrimSpace(receipt.Status), detail.Status),
			Attempts: receipt.Attempts,
		}
		if !receipt.NextAttemptAt.IsZero() {
			target.NextAt = receipt.NextAttemptAt.Local().Format(time.RFC3339)
		}
		if idx, ok := targetByKey[key]; ok {
			detail.Targets[idx] = target
		} else {
			targetByKey[key] = len(detail.Targets)
			detail.Targets = append(detail.Targets, target)
		}
		detail.Receipts = append(detail.Receipts, replpkg.DeliveryReceipt{
			TargetLabel: label,
			Adapter:     strings.TrimSpace(receipt.AdapterName),
			Status:      strings.TrimSpace(receipt.Status),
			At:          deliveryReceiptAt(receipt),
			Error:       strings.TrimSpace(receipt.Error),
		})
	}

	if len(detail.Targets) == 0 && completion.Delivery != nil {
		status := "delivered"
		if completion.Delivery.Verification != nil && !strings.EqualFold(strings.TrimSpace(completion.Delivery.Verification.Status), "passed") {
			status = "blocked"
		}
		label := "terminal inline"
		if completion.Delivery.Conversation != nil && strings.TrimSpace(completion.Delivery.Conversation.Channel) != "" {
			label = completion.Delivery.Conversation.Channel
		}
		detail.Targets = append(detail.Targets, replpkg.DeliveryTarget{
			Kind:   "inline",
			Label:  label,
			Status: status,
		})
	}

	sort.SliceStable(detail.Receipts, func(i, j int) bool {
		return detail.Receipts[i].At > detail.Receipts[j].At
	})
	return detail
}

func mapGovernanceDeliveryItems(items []*runtimesvc.GovernanceDeliveryView) []replpkg.DeliveryListItem {
	out := make([]replpkg.DeliveryListItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, replpkg.DeliveryListItem{
			ID:          strings.TrimSpace(item.ID),
			RunID:       strings.TrimSpace(item.Record.RunID),
			AdapterName: strings.TrimSpace(item.AdapterName),
			Status:      strings.TrimSpace(string(item.Status)),
			Attempts:    item.Attempts,
			MaxAttempts: item.MaxAttempts,
			LastError:   strings.TrimSpace(item.LastError),
			NextAt:      automationTimeLabel(item.NextAttemptAt),
			CanRedrive:  item.CanRedrive,
			Summary:     firstNonEmpty(strings.TrimSpace(item.Record.Summary), strings.TrimSpace(string(item.Record.EventType)), strings.TrimSpace(item.Record.EventID)),
		})
	}
	return out
}

func mapGovernanceRedriveResult(result *runtimesvc.GovernanceRedriveResult) *replpkg.RedriveResult {
	if result == nil {
		return nil
	}
	return &replpkg.RedriveResult{
		Redriven: result.Updated,
		Failed:   result.Skipped,
	}
}

func deliveryReceiptAt(receipt runtimesvc.DeliveryReceipt) string {
	switch {
	case !receipt.DeliveredAt.IsZero():
		return receipt.DeliveredAt.Local().Format(time.RFC3339)
	case !receipt.LastAttemptAt.IsZero():
		return receipt.LastAttemptAt.Local().Format(time.RFC3339)
	case !receipt.UpdatedAt.IsZero():
		return receipt.UpdatedAt.Local().Format(time.RFC3339)
	default:
		return ""
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func buildMemoryUsageItems(entries []agent.MemoryEntry, sessionKey, projectID string) ([]replpkg.MemoryUsageItem, int) {
	matching := matchingMemoryEntries(entries, sessionKey, projectID)
	recalled := agent.RecallForContext(matching, sessionKey, projectID).Memories
	out := make([]replpkg.MemoryUsageItem, 0, len(recalled))
	for _, item := range recalled {
		scope, reason := memoryUsageProjection(item)
		out = append(out, replpkg.MemoryUsageItem{
			Key:       strings.TrimSpace(item.Key),
			Namespace: strings.TrimSpace(item.Namespace),
			Scope:     scope,
			Source:    strings.TrimSpace(item.Source),
			Reason:    reason,
		})
	}
	return out, max(len(matching)-len(recalled), 0)
}

func matchingMemoryEntries(entries []agent.MemoryEntry, sessionKey, projectID string) []agent.MemoryEntry {
	out := make([]agent.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.State == agent.MemorySuperseded {
			continue
		}
		if entry.SessionKey != "" && entry.SessionKey != sessionKey {
			continue
		}
		if entry.ProjectID != "" && projectID != "" && entry.ProjectID != projectID {
			continue
		}
		if entry.ProjectID != "" && projectID == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func memoryUsageProjection(entry agent.MemoryEntry) (scope string, reason string) {
	switch {
	case entry.SessionKey != "":
		scope = "conversation"
		reason = "recent"
	case entry.ProjectID != "":
		scope = "project"
		reason = "project context"
	case strings.TrimSpace(entry.ScopeKey) != "":
		scope = "task"
		reason = "task context"
	case strings.EqualFold(strings.TrimSpace(entry.Source), "user"):
		scope = "saved"
		reason = "pinned"
	default:
		namespace := strings.ToLower(strings.TrimSpace(entry.Namespace))
		if strings.Contains(namespace, "task") {
			scope = "task"
			reason = "task context"
			return scope, reason
		}
		scope = "saved"
		reason = "recalled"
	}
	return scope, reason
}

func buildContextPressure(detail *replpkg.SessionDetail, models []replpkg.ModelInfo, entries []agent.MemoryEntry) *replpkg.ContextPressureInfo {
	if detail == nil {
		return nil
	}
	messages := make([]contextengine.Message, 0, len(detail.Messages))
	for _, item := range detail.Messages {
		role := contextengine.RoleAssistant
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "user":
			role = contextengine.RoleUser
		case "tool":
			role = contextengine.RoleTool
		}
		messages = append(messages, contextengine.NewTextMessage(role, item.Content))
	}
	used := contextengine.CharRatioEstimator{}.EstimateMessages(messages)
	window := 0
	for _, item := range models {
		if item.ID == detail.Summary.Model {
			window = item.ContextWindow
			break
		}
	}
	memoryItems, trimmedMemory := buildMemoryUsageItems(entries, detail.Summary.Key, "")
	percent := 0
	if window > 0 {
		percent = int(float64(used) / float64(window) * 100)
	}
	recommendation := "no action needed"
	switch {
	case percent >= 90:
		recommendation = "compact now"
	case percent >= 75:
		recommendation = "consider /compact soon"
	}
	return &replpkg.ContextPressureInfo{
		WindowSize:     window,
		UsedTokens:     used,
		UsedPercent:    percent,
		KeptItems:      len(detail.Messages) + len(memoryItems),
		TrimmedItems:   trimmedMemory,
		Recommendation: recommendation,
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func filterMemoryEntries(items []agent.MemoryEntry, keep func(agent.MemoryEntry) bool) []agent.MemoryEntry {
	out := make([]agent.MemoryEntry, 0, len(items))
	for _, item := range items {
		if keep != nil && !keep(item) {
			continue
		}
		out = append(out, item)
	}
	return out
}
