package repl

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/contextengine"
	updatepkg "github.com/fulcrus/hopclaw/internal/update"
	"golang.org/x/term"
)

func (r *REPL) refreshSupervisorProjection(ctx context.Context, force bool) {
	if r == nil || r.service == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.supervisorFetchedAt) < projectionRefreshTTL {
		return
	}
	snapshot, err := r.service.SupervisorSnapshot(ctx)
	if err != nil {
		r.supervisorFetchedAt = now
		return
	}
	r.supervisorSnapshot = r.applyLocalSupervisorFocus(snapshot)
	r.supervisorFetchedAt = now
}

func (r *REPL) refreshReadinessProjection(ctx context.Context, force bool) {
	if r == nil || r.service == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.readinessFetchedAt) < readinessRefreshTTL {
		return
	}
	snapshot, err := r.service.ReadinessSnapshot(ctx)
	if err == nil {
		r.readinessSnapshot = snapshot
	}
	r.readinessFetchedAt = now
}

func (r *REPL) refreshTransparencyProjection(ctx context.Context, force bool) {
	if r == nil || r.service == nil {
		return
	}
	sessionID, err := r.currentServiceSessionID(ctx)
	if err != nil || sessionID == "" {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.memoryUsageFetchedAt) < transparencyRefreshTTL && now.Sub(r.contextPressureFetchedAt) < transparencyRefreshTTL {
		return
	}
	if items, err := r.service.MemoryUsedInContext(ctx, sessionID); err == nil {
		r.memoryUsage = append([]MemoryUsageItem(nil), items...)
		r.memoryUsageFetchedAt = now
	}
	if pressure, err := r.service.ContextPressure(ctx, sessionID); err == nil {
		r.contextPressure = pressure
		r.contextPressureFetchedAt = now
	}
}

func (r *REPL) applyLocalSupervisorFocus(snapshot *SupervisorSnapshot) *SupervisorSnapshot {
	if snapshot == nil {
		return nil
	}
	copySnapshot := *snapshot
	if len(snapshot.Items) > 0 {
		copySnapshot.Items = append([]RunSummary(nil), snapshot.Items...)
	}
	r.pruneBackgroundRuns(&copySnapshot)
	foreground := strings.TrimSpace(r.foregroundRunID)
	if foreground == "" && r.running {
		foreground = strings.TrimSpace(r.currentRunID)
	}
	activeCount := 0
	backgroundCount := 0
	for _, item := range copySnapshot.Items {
		if !supervisorRunActive(item) {
			continue
		}
		activeCount++
		if r.isBackgroundRun(item.ID) {
			backgroundCount++
		}
	}
	if foreground != "" {
		copySnapshot.ForegroundRunID = foreground
		copySnapshot.ActiveRunCount = activeCount
		if backgroundCount > 0 {
			copySnapshot.BackgroundRunCount = backgroundCount
		} else if activeCount > 0 {
			copySnapshot.BackgroundRunCount = max(activeCount-1, 0)
		}
		return &copySnapshot
	}
	if len(r.backgroundRuns) > 0 && !r.running {
		copySnapshot.ForegroundRunID = ""
		copySnapshot.ActiveRunCount = activeCount
		if backgroundCount > 0 {
			copySnapshot.BackgroundRunCount = backgroundCount
		} else {
			copySnapshot.BackgroundRunCount = activeCount
		}
	}
	return &copySnapshot
}

func (r *REPL) pruneBackgroundRuns(snapshot *SupervisorSnapshot) {
	if r == nil || len(r.backgroundRuns) == 0 || snapshot == nil {
		return
	}
	if snapshot.ActiveRunCount == 0 && len(snapshot.Items) == 0 {
		r.backgroundRuns = nil
		r.backgroundRunSessions = nil
		return
	}
	itemsByID := make(map[string]RunSummary, len(snapshot.Items))
	for _, item := range snapshot.Items {
		itemsByID[strings.TrimSpace(item.ID)] = item
	}
	kept := r.backgroundRuns[:0]
	for _, runID := range r.backgroundRuns {
		item, ok := itemsByID[strings.TrimSpace(runID)]
		if !ok {
			delete(r.backgroundRunSessions, strings.TrimSpace(runID))
			continue
		}
		if !supervisorRunActive(item) && !supervisorRunPaused(item) {
			delete(r.backgroundRunSessions, strings.TrimSpace(runID))
			continue
		}
		kept = append(kept, runID)
	}
	r.backgroundRuns = kept
}

func (r *REPL) effectiveModel() string {
	if r.selectedModel != "" {
		return r.selectedModel
	}
	if r.thinking {
		for _, item := range r.modelCache {
			if item.SupportsThinking {
				return item.ID
			}
		}
	}
	if r.sessionModel != "" {
		return r.sessionModel
	}
	if len(r.modelCache) > 0 {
		return strings.TrimSpace(r.modelCache[0].ID)
	}
	return ""
}

func (r *REPL) switchSession(ctx context.Context, key string, create bool) error {
	state, err := openSession(ctx, r.client, r.service, key, create)
	if err != nil {
		return err
	}
	r.sessionID = state.ID
	r.serviceSessionID = state.ServiceID
	r.sessionKey = state.Key
	r.sessionModel = state.Model
	r.refreshViewState()
	return nil
}

type sessionState struct {
	ID        string
	ServiceID string
	Key       string
	Model     string
}

func openSession(ctx context.Context, client *acp.InProcessClient, service Service, key string, create bool) (sessionState, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return sessionState{}, fmt.Errorf("session key is required")
	}
	var (
		info *acp.SessionInfo
		err  error
	)
	if create {
		info, err = client.NewSession(ctx, acp.NewSessionParams{SessionKey: key})
	} else {
		info, err = client.LoadSession(ctx, acp.LoadSessionParams{SessionKey: key})
		if err != nil {
			info, err = client.NewSession(ctx, acp.NewSessionParams{SessionKey: key})
		}
	}
	if err != nil {
		return sessionState{}, err
	}
	state := sessionState{
		ID:  info.SessionID,
		Key: info.SessionKey,
	}
	if detail, serviceID, err := resolveSessionDetail(ctx, service, state.ID, state.Key); err == nil && detail != nil {
		state.ServiceID = serviceID
		state.Model = detail.Summary.Model
	}
	return state, nil
}

func resolveSessionDetail(ctx context.Context, service Service, preferredID, sessionKey string) (*SessionDetail, string, error) {
	if service == nil {
		return nil, "", fmt.Errorf("repl service is required")
	}
	preferredID = strings.TrimSpace(preferredID)
	sessionKey = strings.TrimSpace(sessionKey)

	if preferredID != "" {
		if detail, err := service.GetSession(ctx, preferredID); err == nil && detail != nil {
			return detail, strings.TrimSpace(firstNonEmpty(detail.Summary.ID, preferredID)), nil
		}
	}

	if sessionKey == "" {
		if preferredID != "" && !strings.HasPrefix(strings.ToLower(preferredID), "acp-") {
			return nil, preferredID, nil
		}
		return nil, "", nil
	}
	sessions, err := service.ListSessions(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, item := range sessions {
		if !strings.EqualFold(strings.TrimSpace(item.Key), sessionKey) {
			continue
		}
		sessionID := strings.TrimSpace(item.ID)
		if sessionID == "" {
			break
		}
		detail, err := service.GetSession(ctx, sessionID)
		if err != nil {
			return nil, "", err
		}
		if detail == nil {
			detail = &SessionDetail{Summary: item}
		} else if strings.TrimSpace(detail.Summary.ID) == "" {
			detail.Summary.ID = sessionID
		}
		return detail, sessionID, nil
	}
	if preferredID != "" && !strings.HasPrefix(strings.ToLower(preferredID), "acp-") {
		return nil, preferredID, nil
	}
	return nil, "", nil
}

func (r *REPL) switchTarget(ctx context.Context, name string) error {
	if r.targetManager == nil {
		return fmt.Errorf("remote switching is not available")
	}
	binding, err := r.targetManager.SwitchTarget(ctx, name)
	if err != nil {
		return err
	}
	if binding == nil || binding.Client == nil || binding.Service == nil {
		return fmt.Errorf("remote %q is not available", name)
	}

	oldClient := r.client
	oldService := r.service

	r.client = binding.Client
	r.service = binding.Service
	r.streamer = NewStreamer(binding.Client.Notifications())
	r.targetName = normalizeTargetName(binding.Target.Name)
	if r.targetName == "" {
		r.targetName = "local"
	}
	r.targetKind = normalizeTargetKind(binding.Target.Kind, r.targetName)
	r.prompt.SetTarget(r.targetName)
	r.sessionID = binding.SessionID
	r.serviceSessionID = ""
	r.sessionKey = binding.SessionKey
	r.sessionModel = binding.SessionModel
	r.selectedModel = ""
	r.modelCache = append([]ModelInfo(nil), binding.Models...)
	r.pendingApproval = false
	r.running = false
	r.lastUsage = nil
	r.commands.SetDynamic(binding.Commands)
	r.refreshViewState()

	if oldClient != nil {
		oldClient.Close()
	}
	if oldService != nil {
		_ = oldService.Close(context.Background())
	}
	return nil
}

func (r *REPL) currentServiceSession(ctx context.Context) (*SessionDetail, string, error) {
	if r == nil || r.service == nil {
		return nil, "", nil
	}
	detail, sessionID, err := resolveSessionDetail(ctx, r.service, firstNonEmpty(r.serviceSessionID, r.sessionID), r.sessionKey)
	if err != nil {
		return nil, "", err
	}
	if sessionID != "" {
		r.serviceSessionID = sessionID
	}
	if detail != nil && strings.TrimSpace(r.sessionModel) == "" {
		r.sessionModel = strings.TrimSpace(detail.Summary.Model)
	}
	return detail, sessionID, nil
}

func (r *REPL) currentServiceSessionID(ctx context.Context) (string, error) {
	_, sessionID, err := r.currentServiceSession(ctx)
	return sessionID, err
}

func (r *REPL) ensureCurrentRunID() {
	if r == nil || r.service == nil || strings.TrimSpace(r.currentRunID) != "" {
		return
	}
	if !r.running && strings.TrimSpace(r.foregroundRunID) == "" && len(r.backgroundRuns) > 0 {
		return
	}
	sessionID, err := r.currentServiceSessionID(context.Background())
	if err != nil || sessionID == "" {
		return
	}
	items, err := r.service.ListRuns(context.Background(), sessionID, 1)
	if err != nil || len(items) == 0 {
		return
	}
	r.currentRunID = strings.TrimSpace(items[0].ID)
	if r.lastRunID == "" {
		r.lastRunID = r.currentRunID
	}
}

func (r *REPL) refreshDeliveryStateForLatestRun() {
	if r == nil {
		return
	}
	runID := strings.TrimSpace(defaultString(r.currentRunID, r.lastRunID))
	if runID == "" {
		r.ensureCurrentRunID()
		runID = strings.TrimSpace(defaultString(r.currentRunID, r.lastRunID))
	}
	if runID == "" {
		return
	}
	r.refreshDeliveryState(runID)
}

func (r *REPL) refreshViewState() {
	if r == nil {
		return
	}
	if r.prompt != nil {
		r.prompt.SetTarget(r.targetName)
		if r.prompt.stateProvider == nil {
			r.prompt.SetStateProvider(func() REPLViewState {
				r.refreshViewState()
				return r.viewState
			})
		}
	}
	cwd, _ := os.Getwd()
	git := detectGitSnapshot(cwd)
	execution := "ready"
	switch {
	case r.pendingApproval:
		execution = "waiting approval"
	case r.phase == PhasePaused:
		execution = "paused"
	case r.phase == PhaseCancelled:
		execution = "cancelled"
	case r.phase == PhaseCompleted:
		execution = "completed"
	case r.phase == PhaseError:
		execution = "error"
	case r.running,
		r.phase == PhaseThinking,
		r.phase == PhasePlanning,
		r.phase == PhaseExecutingTools,
		r.phase == PhaseProcessingResults,
		r.phase == PhaseDelivering:
		execution = "running"
	}
	model := r.effectiveModel()
	if model == "" {
		model = "(default)"
	}
	think := "off"
	if r.thinking {
		think = "on"
	}
	health := "ok"
	if r.lastToolStatus != "" {
		health = "warn"
	}
	if r.readinessSnapshot != nil {
		snapshotHealth := readinessOverallStatus(readinessOverallSnapshotStatus(r.readinessSnapshot), r.readinessSnapshot.Categories)
		switch snapshotHealth {
		case "blocked":
			health = "blocked"
		case "degraded":
			if health != "blocked" {
				health = "degraded"
			}
		}
	}
	updateVersion := ""
	if result := updatepkg.LastCheckResult(); result != nil && !result.UpToDate && result.LatestVersion != "" {
		updateVersion = result.LatestVersion
	}
	project := ""
	if r.currentProject != nil {
		project = r.currentProject.Name
	}
	runtimeMode := "local"
	if normalizeTargetKind(r.targetKind, r.targetName) == "remote" {
		runtimeMode = "gateway"
	}
	queueDepth := 0
	if r.supervisorSnapshot != nil {
		queueDepth = max(r.supervisorSnapshot.BackgroundRunCount, len(r.backgroundRuns))
	}
	quality := ""
	if snapshot := r.readinessSnapshot; snapshot != nil {
		for _, item := range snapshot.Categories {
			if strings.TrimSpace(item.ID) != "quality_release" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(item.Status))
			if status == "" || status == "ready" {
				break
			}
			quality = status
			break
		}
	}
	contextPercent := 0
	if len(r.modelCache) > 0 && r.lastUsage != nil {
		window := 0
		for _, item := range r.modelCache {
			if item.ID == model {
				window = item.ContextWindow
				break
			}
		}
		if window > 0 {
			contextPercent = int(float64(r.lastUsage.TotalTokens) / float64(window) * 100)
		}
	}
	elapsed := ""
	duration := ""
	if !r.runStartedAt.IsZero() && (r.running || r.phase == PhaseCompleted || r.phase == PhaseCancelled || r.phase == PhaseError || r.phase == PhasePaused || r.pendingApproval) {
		value := time.Since(r.runStartedAt)
		if r.lastRunDuration > 0 && (r.phase == PhaseCompleted || r.phase == PhaseCancelled || r.phase == PhaseError || r.phase == PhasePaused) {
			value = r.lastRunDuration
		}
		elapsed = formatClockDuration(value)
		duration = elapsed
	}
	badgeSize := 4
	if r.badgeMgr != nil {
		badgeSize = resolveBadgeSize(r.badgeMgr.Config().Size)
	}
	foregroundRunCount := 0
	backgroundRunCount := 0
	pausedRunCount := 0
	attentionCount := 0
	attentionPrimary := ""
	if r.supervisorSnapshot != nil {
		if strings.TrimSpace(r.supervisorSnapshot.ForegroundRunID) != "" && len(r.backgroundRuns) == 0 {
			foregroundRunCount = 1
		}
		backgroundRunCount = max(r.supervisorSnapshot.BackgroundRunCount, len(r.backgroundRuns))
		pausedRunCount = r.supervisorSnapshot.PausedRunCount
		attentionCount = r.supervisorSnapshot.AttentionCount
		attentionPrimary = supervisorAttentionPrimary(r.supervisorSnapshot)
	}
	if r.running && strings.TrimSpace(firstNonEmpty(r.foregroundRunID, r.currentRunID)) != "" {
		foregroundRunCount = 1
	}
	if attentionPrimary == "" && strings.EqualFold(strings.TrimSpace(r.deliveryState.State), "retrying") {
		attentionPrimary = "delivery"
		attentionCount = max(attentionCount, 1)
	}
	r.viewState = REPLViewState{
		Target:             defaultString(r.targetName, "local"),
		TargetKind:         r.targetKind,
		Profile:            "",
		Model:              model,
		Think:              think,
		ExecutionState:     execution,
		ApprovalMode:       "on-request",
		ApprovalID:         strings.TrimSpace(r.approvalState.ID),
		Health:             health,
		UpdateVersion:      updateVersion,
		CWD:                cwd,
		GitBranch:          git.Branch,
		GitAdded:           git.Added,
		GitModified:        git.Modified,
		Project:            project,
		Badge:              r.currentBadgeLabel(),
		BadgeSize:          badgeSize,
		SessionKey:         defaultString(r.sessionKey, "default"),
		Channel:            "",
		Runtime:            runtimeMode,
		QueueDepth:         queueDepth,
		Sandbox:            "",
		Phase:              r.phase.String(),
		LastTool:           currentToolName(r.lastToolStatus),
		ScopeSummary:       defaultString(r.approvalState.Scope, r.deliveryState.Summary),
		ContextPercent:     contextPercent,
		PromptTokens:       usagePromptTokens(r.lastUsage),
		CompletionTokens:   usageCompletionTokens(r.lastUsage),
		Quality:            quality,
		LastFailure:        defaultString(r.lastFailure, latestFailureSummary(r.lastTimeline)),
		RunID:              defaultString(r.foregroundRunID, defaultString(r.currentRunID, r.lastRunID)),
		Elapsed:            elapsed,
		Duration:           duration,
		ApprovalCount:      boolToInt(r.pendingApproval),
		ApprovalRisk:       strings.ToLower(strings.TrimSpace(r.approvalState.Risk)),
		Resumable:          r.pausedRun != nil || r.phase == PhasePaused,
		ForegroundRunCount: foregroundRunCount,
		BackgroundRunCount: backgroundRunCount,
		PausedRunCount:     pausedRunCount,
		AttentionCount:     attentionCount,
		AttentionPrimary:   attentionPrimary,
		DeliveryState:      strings.TrimSpace(r.deliveryState.State),
		DeliverySummary:    strings.TrimSpace(r.deliveryState.Summary),
		DeliveryNext:       strings.TrimSpace(r.deliveryState.Next),
		MemoryStrip:        memoryStripSummary(r.memoryUsage),
		ActivePanel:        strings.TrimSpace(r.activePanel),
		LayoutMode:         r.layoutMode,
		TerminalWidth:      r.terminalWidth(),
	}
	r.viewState.Profile = inferProfile(r.viewState)
}

func supervisorAttentionPrimary(snapshot *SupervisorSnapshot) string {
	if snapshot == nil {
		return ""
	}
	best := ""
	bestRank := -1
	foregroundID := strings.TrimSpace(snapshot.ForegroundRunID)
	for _, item := range snapshot.Items {
		attention := strings.TrimSpace(item.Attention)
		if attention == "" && item.Resumable {
			attention = "resumable"
		}
		if attention == "" {
			continue
		}
		rank := attentionPriority(attention)
		if strings.TrimSpace(item.ID) == foregroundID {
			rank += 10
		}
		if rank > bestRank {
			best = attention
			bestRank = rank
		}
	}
	return best
}

func attentionPriority(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approval":
		return 40
	case "error", "failed", "critical":
		return 35
	case "blocked", "degraded":
		return 30
	case "delivery":
		return 25
	case "resumable":
		return 20
	default:
		return 10
	}
}

func (r *REPL) transitionPhase(next Phase, toolName string) {
	if r == nil {
		return
	}
	if next == "" {
		next = PhaseIdle
	}
	if r.phase == next {
		if next != PhaseExecutingTools || strings.TrimSpace(toolName) == strings.TrimSpace(r.viewState.LastTool) {
			r.refreshViewState()
			return
		}
	}
	line := formatPhaseLine(next, toolName)
	if line != "" && line != r.lastPhaseLine && r.renderer != nil {
		if !r.suppressPromptWorkbenchRuntimeNoise() {
			r.renderer.RenderPhase(next, toolName)
		}
		r.lastPhaseLine = line
	} else if line == "" {
		r.lastPhaseLine = ""
	}
	r.phase = next
	if strings.TrimSpace(toolName) != "" {
		r.viewState.LastTool = strings.TrimSpace(toolName)
	}
	r.refreshViewState()
	r.renderDock()
}

func (r *REPL) startOrUpdateToolTimeline(name string, summary string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if r.activeTimeline != nil && r.activeTimeline.Name == name {
		if trimmed := strings.TrimSpace(summary); trimmed != "" {
			r.activeTimeline.Summary = trimmed
		}
		return
	}
	r.finishActiveToolTimeline("ok", time.Now())
	r.activeTimeline = &activeToolTimeline{
		Name:      name,
		Summary:   strings.TrimSpace(summary),
		StartedAt: time.Now(),
	}
}

func (r *REPL) finishActiveToolTimeline(status string, finishedAt time.Time) {
	if r == nil || r.activeTimeline == nil {
		return
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	duration := time.Duration(0)
	if !r.activeTimeline.StartedAt.IsZero() && finishedAt.After(r.activeTimeline.StartedAt) {
		duration = finishedAt.Sub(r.activeTimeline.StartedAt)
	}
	r.lastTimeline = append(r.lastTimeline, ToolTimelineEntry{
		Name:     r.activeTimeline.Name,
		Status:   defaultString(strings.TrimSpace(status), "ok"),
		Summary:  strings.TrimSpace(r.activeTimeline.Summary),
		Duration: duration,
	})
	r.activeTimeline = nil
}

func (r *REPL) cacheLatestRunTimeline() {
	if r == nil || len(r.lastTimeline) == 0 {
		return
	}
	if r.runTimelines == nil {
		r.runTimelines = make(map[string][]ToolTimelineEntry)
	}
	runID := strings.TrimSpace(defaultString(r.currentRunID, r.lastRunID))
	if runID == "" {
		if r.service == nil {
			return
		}
		sessionID, err := r.currentServiceSessionID(context.Background())
		if err != nil || sessionID == "" {
			return
		}
		items, err := r.service.ListRuns(context.Background(), sessionID, 1)
		if err != nil || len(items) == 0 {
			return
		}
		runID = strings.TrimSpace(items[0].ID)
	}
	if runID == "" {
		return
	}
	r.lastRunID = runID
	r.currentRunID = runID
	r.runTimelines[runID] = cloneToolTimeline(r.lastTimeline)
}

func (r *REPL) timelineForRun(runID string) []ToolTimelineEntry {
	runID = strings.TrimSpace(runID)
	if runID != "" && r.runTimelines != nil {
		if items, ok := r.runTimelines[runID]; ok {
			return cloneToolTimeline(items)
		}
	}
	if runID != "" && runID == r.lastRunID {
		return cloneToolTimeline(r.lastTimeline)
	}
	return nil
}

func cloneToolTimeline(items []ToolTimelineEntry) []ToolTimelineEntry {
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolTimelineEntry, len(items))
	copy(out, items)
	return out
}

func (r *REPL) completionModelChoices() []string {
	if r == nil || r.service == nil {
		return nil
	}
	if len(r.modelCache) == 0 {
		if models, err := r.service.Models(context.Background()); err == nil {
			r.modelCache = append([]ModelInfo(nil), models...)
		}
	}
	out := make([]string, 0, len(r.modelCache))
	for _, item := range r.modelCache {
		if name := strings.TrimSpace(item.ID); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func (r *REPL) completionRemoteChoices() []string {
	if r == nil || r.targetManager == nil {
		return nil
	}
	targets, err := r.targetManager.ListTargets(context.Background())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(targets))
	for _, item := range targets {
		if name := strings.TrimSpace(item.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func mapSandbox(runtimeMode string) string {
	if runtimeMode == "gateway" {
		return "remote"
	}
	return "local"
}

func currentToolName(status string) string {
	if status == "" {
		return ""
	}
	line, _, _ := strings.Cut(status, "\n")
	return strings.TrimSpace(line)
}

func latestFailureSummary(items []ToolTimelineEntry) string {
	for i := len(items) - 1; i >= 0; i-- {
		status := strings.ToLower(strings.TrimSpace(items[i].Status))
		switch status {
		case "", "ok", "done", "completed":
			continue
		}
		if summary := strings.TrimSpace(items[i].Summary); summary != "" {
			return items[i].Name + " " + summary
		}
		return items[i].Name + " " + status
	}
	return ""
}

func usagePromptTokens(info *acp.UsageInfo) int {
	if info == nil {
		return 0
	}
	return info.PromptTokens
}

func usageCompletionTokens(info *acp.UsageInfo) int {
	if info == nil {
		return 0
	}
	return info.CompletionTokens
}

func attachmentBlockCount(blocks []contextengine.ContentBlock) int {
	total := 0
	for _, block := range blocks {
		if block.Type != contextengine.ContentBlockText {
			total++
		}
	}
	return total
}

func isTerminalWriter(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}
