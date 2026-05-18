package channels

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/meta"
)

const DefaultStatusReminderDelay = 3 * time.Second

type RunNotificationTarget struct {
	RunID        string
	SessionKey   string
	ChannelID    string
	TargetID     string
	ReplyToID    string
	InputContent string
	Format       string
	Metadata     map[string]any
}

type RunStatusNotifier struct {
	delay time.Duration
	send  func(context.Context, OutboundMessage) error

	mu    sync.Mutex
	items map[string]runStatusItem
}

type runStatusItem struct {
	target      RunNotificationTarget
	version     int
	toolRounds  int
	toolNames   []string
	phase       string
	activeTask  string
	completed   int
	total       int
	dirty       bool
	progressSeq int
}

func NewRunStatusNotifier(delay time.Duration, send func(context.Context, OutboundMessage) error) *RunStatusNotifier {
	if delay <= 0 {
		delay = DefaultStatusReminderDelay
	}
	return &RunStatusNotifier{
		delay: delay,
		send:  send,
		items: make(map[string]runStatusItem),
	}
}

func (n *RunStatusNotifier) Track(ctx context.Context, target RunNotificationTarget) {
	if n == nil || n.send == nil || target.RunID == "" || target.TargetID == "" {
		return
	}
	n.mu.Lock()
	item := runStatusItem{target: cloneNotificationTarget(target), version: 1}
	n.items[target.RunID] = item
	n.mu.Unlock()
	n.scheduleHeartbeat(ctx, target.RunID, item.version)
}

func (n *RunStatusNotifier) Restore(ctx context.Context, target RunNotificationTarget, run *agent.Run) bool {
	if n == nil || run == nil || stringsTrim(run.ID) == "" {
		return false
	}
	target.RunID = run.ID
	n.Track(ctx, target)
	switch run.Status {
	case agent.RunWaitingApproval:
		return n.NotifyApproval(ctx, run.ID)
	case agent.RunQueued, agent.RunRunning, agent.RunStreaming:
		if phase := stringsTrim(string(run.Phase)); phase != "" {
			n.NotifyPhaseChanged(run.ID, phase, nil)
		}
		return n.NotifyTrackingResumed(ctx, run.ID)
	case agent.RunWaitingInput:
		return true
	default:
		return false
	}
}

func (n *RunStatusNotifier) Clear(runID string) {
	if n == nil || runID == "" {
		return
	}
	n.mu.Lock()
	delete(n.items, runID)
	n.mu.Unlock()
}

func (n *RunStatusNotifier) NotifyApproval(ctx context.Context, runID string) bool {
	if n == nil || runID == "" {
		return false
	}
	item, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		item.version++
		item.phase = "waiting_approval"
		item.progressSeq++
		item.dirty = true
		return item
	})
	if !ok {
		return false
	}
	log.Info("run waiting approval", "run_id", runID)
	if !n.sendApprovalStatus(ctx, runID, item) {
		return false
	}
	n.clearProgressDirty(runID, item.progressSeq)
	n.scheduleHeartbeat(ctx, runID, item.version)
	return true
}

func (n *RunStatusNotifier) NotifyAutoApproved(ctx context.Context, runID string) bool {
	if n == nil || runID == "" {
		return false
	}
	_, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		item.version++
		item.dirty = false
		return item
	})
	if !ok {
		return false
	}
	log.Info("approval auto-approved", "run_id", runID)
	return true
}

func (n *RunStatusNotifier) NotifyResumed(ctx context.Context, runID string) bool {
	if n == nil || runID == "" {
		return false
	}
	item, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		item.version++
		item.phase = ""
		item.progressSeq++
		item.dirty = true
		return item
	})
	if !ok {
		return false
	}
	n.scheduleHeartbeat(ctx, runID, item.version)
	return true
}

func (n *RunStatusNotifier) NotifyToolProgress(runID string, toolRounds int, toolNames []string) bool {
	if n == nil || runID == "" {
		return false
	}
	_, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		if toolRounds > item.toolRounds {
			item.toolRounds = toolRounds
		}
		item.toolNames = append([]string(nil), toolNames...)
		item.progressSeq++
		item.dirty = true
		return item
	})
	return ok
}

func (n *RunStatusNotifier) NotifyPhaseChanged(runID, phase string, toolNames []string) bool {
	if n == nil || runID == "" {
		return false
	}
	phase = stringsTrim(phase)
	_, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		if phase != "" {
			item.phase = phase
		}
		if len(toolNames) > 0 {
			item.toolNames = append([]string(nil), toolNames...)
		}
		item.progressSeq++
		item.dirty = true
		return item
	})
	return ok
}

func (n *RunStatusNotifier) NotifyCancelled(ctx context.Context, runID string) bool {
	if n == nil || runID == "" {
		return false
	}
	item, ok := n.deleteAndReturn(runID)
	if !ok {
		return false
	}
	return n.sendStatus(ctx, item.target, BridgeCancelledMessage(item.target.InputContent), meta.StatusKindCancelled)
}

func (n *RunStatusNotifier) NotifySubmitFailure(ctx context.Context, target RunNotificationTarget, raw string) bool {
	if n == nil || n.send == nil || target.TargetID == "" {
		return false
	}
	return n.sendStatus(ctx, target, BridgeSubmitFailureMessage(target.InputContent, raw), meta.StatusKindSubmitFailed)
}

func (n *RunStatusNotifier) NotifyBackendAuthUnavailable(ctx context.Context, target RunNotificationTarget) bool {
	if n == nil || n.send == nil || target.TargetID == "" {
		return false
	}
	return n.sendStatus(ctx, target, BridgeBackendAuthUnavailableMessage(target.InputContent), meta.StatusKindBackendAuthUnavailable)
}

func (n *RunStatusNotifier) NotifyPreflight(ctx context.Context, target RunNotificationTarget, report *agent.RunPreflightReport) bool {
	if n == nil || n.send == nil || target.TargetID == "" || report == nil {
		return false
	}
	if report.State == agent.RunPreflightReady {
		return false
	}
	metadata := cloneAnyMap(target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	metadata["preflight_state"] = report.State
	metadata["preflight_blocking"] = report.Blocking
	metadata["preflight_summary"] = report.Summary
	metadata["preflight_question"] = report.Question
	metadata["preflight_continue_hint"] = report.ContinueHint
	if replyTemplate := bridgePreflightReplyTemplate(target.InputContent, report); replyTemplate != "" {
		metadata["preflight_reply_template"] = replyTemplate
	}
	if len(report.ReplyHints) > 0 {
		metadata["preflight_reply_hints"] = append([]string(nil), report.ReplyHints...)
	}
	metadata[meta.KeyStatusKind] = meta.PreflightStatusKind(string(report.State)).String()
	if report.State == agent.RunPreflightNeedsConfirmation {
		metadata["outcome"] = "needs_confirmation"
	}
	if target.RunID != "" {
		metadata[meta.KeyRunID] = target.RunID
	}
	return n.send(ctx, OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   BridgePreflightMessage(target.InputContent, report),
		Format:    defaultOutboundFormat(target.Format),
		Metadata:  metadata,
	}) == nil
}

type RunProgressSnapshot struct {
	Target     RunNotificationTarget
	ToolRounds int
	ToolNames  []string
	Phase      string
	ActiveTask string
	Completed  int
	Total      int
}

func (n *RunStatusNotifier) SnapshotRun(runID string) (RunProgressSnapshot, bool) {
	if n == nil || runID == "" {
		return RunProgressSnapshot{}, false
	}
	item, ok := n.snapshot(runID, nil)
	if !ok {
		return RunProgressSnapshot{}, false
	}
	return RunProgressSnapshot{
		Target:     cloneNotificationTarget(item.target),
		ToolRounds: item.toolRounds,
		ToolNames:  append([]string(nil), item.toolNames...),
		Phase:      item.phase,
		ActiveTask: item.activeTask,
		Completed:  item.completed,
		Total:      item.total,
	}, true
}

func (n *RunStatusNotifier) FindBySessionKey(sessionKey string) (RunProgressSnapshot, bool) {
	if n == nil || stringsTrim(sessionKey) == "" {
		return RunProgressSnapshot{}, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var selected runStatusItem
	var ok bool
	for _, item := range n.items {
		if item.target.SessionKey != sessionKey {
			continue
		}
		if !ok || item.version >= selected.version {
			selected = item
			ok = true
		}
	}
	if !ok {
		return RunProgressSnapshot{}, false
	}
	return RunProgressSnapshot{
		Target:     cloneNotificationTarget(selected.target),
		ToolRounds: selected.toolRounds,
		ToolNames:  append([]string(nil), selected.toolNames...),
		Phase:      selected.phase,
		ActiveTask: selected.activeTask,
		Completed:  selected.completed,
		Total:      selected.total,
	}, true
}

func (n *RunStatusNotifier) scheduleHeartbeat(ctx context.Context, runID string, version int) {
	go func() {
		ticker := time.NewTicker(n.delay)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			item, ok := n.snapshot(runID, nil)
			if !ok || item.version != version {
				return
			}
			log.Info("run heartbeat",
				"run_id", runID,
				"phase", item.phase,
				"tool_rounds", item.toolRounds,
				"active_task", item.activeTask,
				"progress", fmt.Sprintf("%d/%d", item.completed, item.total),
			)
			if !item.dirty {
				continue
			}
			if !n.sendProgressStatus(ctx, runID, item) {
				continue
			}
			n.clearProgressDirty(runID, item.progressSeq)
		}
	}()
}

func (n *RunStatusNotifier) NotifyPlanProgress(runID, activeTask string, completed, total int) bool {
	if n == nil || runID == "" {
		return false
	}
	_, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		item.activeTask = stringsTrim(activeTask)
		if completed >= 0 {
			item.completed = completed
		}
		if total >= 0 {
			item.total = total
		}
		item.progressSeq++
		item.dirty = true
		return item
	})
	return ok
}

func (n *RunStatusNotifier) NotifyTrackingResumed(ctx context.Context, runID string) bool {
	if n == nil || runID == "" {
		return false
	}
	item, ok := n.snapshot(runID, func(item runStatusItem) runStatusItem {
		item.progressSeq++
		item.dirty = true
		return item
	})
	if !ok {
		return false
	}
	if !n.sendProgressStatus(ctx, runID, item) {
		return false
	}
	n.clearProgressDirty(runID, item.progressSeq)
	return true
}

func (n *RunStatusNotifier) sendProgressStatus(ctx context.Context, runID string, item runStatusItem) bool {
	if n == nil || n.send == nil {
		return false
	}
	if item.phase == "waiting_approval" {
		return n.sendApprovalStatus(ctx, runID, item)
	}
	content := BridgeProgressUpdateMessage(
		item.target.InputContent,
		item.phase,
		item.toolRounds,
		item.toolNames,
		item.activeTask,
		item.completed,
		item.total,
	)
	if stringsTrim(content) == "" || stringsTrim(item.target.TargetID) == "" {
		return false
	}
	metadata := cloneAnyMap(item.target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 8)
	}
	metadata[meta.KeyRunID] = runID
	metadata[meta.KeyStatusKind] = meta.StatusKindProcessing.String()
	if item.phase != "" {
		metadata["phase"] = item.phase
	}
	if item.toolRounds > 0 {
		metadata["tool_rounds"] = item.toolRounds
	}
	if len(item.toolNames) > 0 {
		metadata["tool_names"] = append([]string(nil), item.toolNames...)
	}
	if item.activeTask != "" {
		metadata["active_task"] = item.activeTask
	}
	if item.completed >= 0 {
		metadata["completed"] = item.completed
	}
	if item.total > 0 {
		metadata["total"] = item.total
	}
	return n.send(ctx, OutboundMessage{
		ChannelID: item.target.ChannelID,
		TargetID:  item.target.TargetID,
		ReplyToID: item.target.ReplyToID,
		Content:   content,
		Format:    defaultOutboundFormat(item.target.Format),
		Metadata:  metadata,
	}) == nil
}

func (n *RunStatusNotifier) sendApprovalStatus(ctx context.Context, runID string, item runStatusItem) bool {
	if n == nil || n.send == nil {
		return false
	}
	content := BridgeApprovalMessage(item.target.InputContent)
	if stringsTrim(content) == "" || stringsTrim(item.target.TargetID) == "" {
		return false
	}
	metadata := cloneAnyMap(item.target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	metadata[meta.KeyRunID] = runID
	metadata[meta.KeyStatusKind] = meta.StatusKindApprovalWaiting.String()
	metadata["phase"] = "waiting_approval"
	return n.send(ctx, OutboundMessage{
		ChannelID: item.target.ChannelID,
		TargetID:  item.target.TargetID,
		ReplyToID: item.target.ReplyToID,
		Content:   content,
		Format:    defaultOutboundFormat(item.target.Format),
		Metadata:  metadata,
	}) == nil
}

func (n *RunStatusNotifier) clearProgressDirty(runID string, seq int) {
	if n == nil || runID == "" {
		return
	}
	_, _ = n.snapshot(runID, func(item runStatusItem) runStatusItem {
		if item.progressSeq == seq {
			item.dirty = false
		}
		return item
	})
}

func (n *RunStatusNotifier) sendStatus(ctx context.Context, target RunNotificationTarget, content string, statusKind meta.StatusKind) bool {
	if stringsTrim(content) == "" || stringsTrim(target.TargetID) == "" {
		return false
	}
	metadata := cloneAnyMap(target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 2)
	}
	if target.RunID != "" {
		metadata[meta.KeyRunID] = target.RunID
	}
	metadata[meta.KeyStatusKind] = statusKind.String()
	if stringsTrim(target.Format) == "" {
		target.Format = "text"
	}
	if err := n.send(ctx, OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadata,
	}); err != nil {
		return false
	}
	return true
}

func (n *RunStatusNotifier) snapshot(runID string, mutate func(runStatusItem) runStatusItem) (runStatusItem, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	item, ok := n.items[runID]
	if !ok {
		return runStatusItem{}, false
	}
	if mutate != nil {
		item = mutate(item)
		n.items[runID] = item
	}
	return item, true
}

func (n *RunStatusNotifier) deleteAndReturn(runID string) (runStatusItem, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	item, ok := n.items[runID]
	if ok {
		delete(n.items, runID)
	}
	return item, ok
}
