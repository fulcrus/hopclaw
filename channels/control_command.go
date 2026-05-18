package channels

import (
	"context"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/runtime"
)

type ControlCommand string

const (
	ControlCommandStatus ControlCommand = "status"
	ControlCommandCancel ControlCommand = "cancel"
	ControlCommandBind   ControlCommand = "bind"
	ControlCommandUnbind ControlCommand = "unbind"
)

type controlRunGetter interface {
	GetRun(ctx context.Context, id string) (*agent.Run, error)
}

type controlRunCanceller interface {
	CancelRun(ctx context.Context, runID string) (*agent.Run, error)
}

type controlRunLister interface {
	ListRuns(ctx context.Context, filter agent.RunListFilter) ([]*agent.Run, error)
}

// ParseControlCommand matches English slash-command identifiers.
// No non-English aliases — this is an international product.
// The primary path is StructuredCommand from UI buttons.
func ParseControlCommand(text string) (ControlCommand, bool) {
	cmd, ok := runtime.ParseTextControlCommand(text)
	if !ok {
		return "", false
	}
	switch cmd {
	case runtime.TextControlCommandStatus:
		return ControlCommandStatus, true
	case runtime.TextControlCommandCancel:
		return ControlCommandCancel, true
	case runtime.TextControlCommandBind:
		return ControlCommandBind, true
	case runtime.TextControlCommandUnbind:
		return ControlCommandUnbind, true
	default:
		return "", false
	}
}

func HandleControlCommand(ctx context.Context, cmd ControlCommand, runtime controlRunGetter, sessions agent.SessionStore, notifier *RunStatusNotifier, send func(context.Context, OutboundMessage) error, sessionKey string, target RunNotificationTarget) bool {
	switch cmd {
	case ControlCommandStatus:
		return replyStatusCommand(ctx, runtime, sessions, notifier, send, sessionKey, target)
	case ControlCommandCancel:
		return cancelStatusCommand(ctx, runtime, sessions, notifier, send, sessionKey, target)
	default:
		return false
	}
}

// HandleBindCommand processes /bind and /unbind commands for thread-session binding.
// channelName is the adapter name (e.g. "discord", "telegram"), threadID is the
// thread or topic identifier from the platform, and sessionKey is the current session.
func HandleBindCommand(ctx context.Context, cmd ControlCommand, bindings *ThreadBinding, send func(context.Context, OutboundMessage) error, channelName, threadID, sessionKey string, target RunNotificationTarget) bool {
	if bindings == nil || send == nil || stringsTrim(target.TargetID) == "" {
		return false
	}
	if threadID == "" {
		replyBindMessage(ctx, send, target, "bind/unbind requires a thread or topic context")
		return true
	}

	switch cmd {
	case ControlCommandBind:
		bindings.Bind(channelName, threadID, sessionKey)
		replyBindMessage(ctx, send, target, "thread bound to current session")
		return true
	case ControlCommandUnbind:
		bindings.Unbind(channelName, threadID)
		replyBindMessage(ctx, send, target, "thread unbound from session")
		return true
	default:
		return false
	}
}

func replyBindMessage(ctx context.Context, send func(context.Context, OutboundMessage) error, target RunNotificationTarget, content string) {
	if stringsTrim(target.Format) == "" {
		target.Format = "text"
	}
	metadata := cloneAnyMap(target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 2)
	}
	metadata[meta.KeyStatusKind] = meta.StatusKindControlBind.String()
	logging.LogIfErr(ctx, send(ctx, OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadata,
	}), "send bind command response failed")
}

func replyStatusCommand(ctx context.Context, runtime controlRunGetter, sessions agent.SessionStore, notifier *RunStatusNotifier, send func(context.Context, OutboundMessage) error, sessionKey string, target RunNotificationTarget) bool {
	if runtime == nil || send == nil || stringsTrim(target.TargetID) == "" {
		return false
	}
	run, snapshot, ok := lookupSessionProgress(ctx, runtime, sessions, notifier, sessionKey)
	content := BridgeNoActiveRunMessage(target.InputContent)
	metadata := cloneAnyMap(target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	metadata[meta.KeyStatusKind] = meta.StatusKindControlStatus.String()
	metadata["control_command"] = string(ControlCommandStatus)
	if ok && run != nil {
		content = BridgeProgressStatusMessage(target.InputContent, run, snapshot.ToolRounds, snapshot.ToolNames)
		metadata[meta.KeyRunID] = run.ID
		metadata["run_status"] = run.Status
		if stringsTrim(string(run.Phase)) != "" {
			metadata["run_phase"] = run.Phase
		}
	} else {
		metadata["run_status"] = "idle"
	}
	if stringsTrim(target.Format) == "" {
		target.Format = "text"
	}
	logging.LogIfErr(ctx, send(ctx, OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadata,
	}), "send control command response failed")
	return true
}

func cancelStatusCommand(ctx context.Context, runtime controlRunGetter, sessions agent.SessionStore, notifier *RunStatusNotifier, send func(context.Context, OutboundMessage) error, sessionKey string, target RunNotificationTarget) bool {
	if runtime == nil || send == nil || stringsTrim(target.TargetID) == "" {
		return false
	}
	run, _, ok := lookupSessionProgress(ctx, runtime, sessions, notifier, sessionKey)
	content := BridgeNoActiveRunMessage(target.InputContent)
	metadata := cloneAnyMap(target.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	metadata[meta.KeyStatusKind] = meta.StatusKindControlCancel.String()
	metadata["control_command"] = string(ControlCommandCancel)
	if ok && run != nil {
		if canceller, ok := any(runtime).(controlRunCanceller); ok {
			if _, err := canceller.CancelRun(ctx, run.ID); err == nil {
				content = BridgeCancelAcceptedMessage(target.InputContent)
				metadata[meta.KeyRunID] = run.ID
				metadata["run_status"] = agent.RunCancelled
			} else {
				content = BridgeCancelFailureMessage(target.InputContent)
				metadata[meta.KeyRunID] = run.ID
				metadata["run_status"] = run.Status
				metadata["control_error"] = err.Error()
			}
		} else {
			content = BridgeCancelFailureMessage(target.InputContent)
			metadata[meta.KeyRunID] = run.ID
			metadata["run_status"] = run.Status
		}
	} else {
		metadata["run_status"] = "idle"
	}
	if stringsTrim(target.Format) == "" {
		target.Format = "text"
	}
	logging.LogIfErr(ctx, send(ctx, OutboundMessage{
		ChannelID: target.ChannelID,
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    target.Format,
		Metadata:  metadata,
	}), "send control command response failed")
	return true
}

func lookupSessionProgress(ctx context.Context, runtime controlRunGetter, sessions agent.SessionStore, notifier *RunStatusNotifier, sessionKey string) (*agent.Run, RunProgressSnapshot, bool) {
	if stringsTrim(sessionKey) == "" {
		return nil, RunProgressSnapshot{}, false
	}
	if session, err := agent.LoadSessionMetadataByKey(ctx, sessions, sessionKey, agent.ScopeFilter{}); err == nil && session != nil {
		if lister, ok := any(runtime).(controlRunLister); ok {
			if runs, err := lister.ListRuns(ctx, agent.RunListFilter{SessionID: session.ID, Limit: 16}); err == nil {
				if run := chooseProgressRun(runs); run != nil {
					var snapshot RunProgressSnapshot
					if notifier != nil {
						snapshot, _ = notifier.SnapshotRun(run.ID)
					}
					return run, snapshot, true
				}
			}
		}
	}
	if notifier != nil {
		if snapshot, ok := notifier.FindBySessionKey(sessionKey); ok && stringsTrim(snapshot.Target.RunID) != "" {
			run, err := runtime.GetRun(ctx, snapshot.Target.RunID)
			if err == nil && run != nil {
				return run, snapshot, true
			}
		}
	}
	return nil, RunProgressSnapshot{}, false
}

func chooseProgressRun(runs []*agent.Run) *agent.Run {
	var selected *agent.Run
	bestPriority := -1
	for _, run := range runs {
		if run == nil {
			continue
		}
		priority := progressRunPriority(run.Status)
		if priority < 0 {
			continue
		}
		if selected == nil || priority > bestPriority || (priority == bestPriority && run.UpdatedAt.After(selected.UpdatedAt)) {
			selected = run
			bestPriority = priority
		}
	}
	return selected
}

func progressRunPriority(status agent.RunStatus) int {
	switch status {
	case agent.RunRunning, agent.RunStreaming:
		return 4
	case agent.RunWaitingInput:
		return 3
	case agent.RunWaitingApproval:
		return 3
	case agent.RunQueued:
		return 2
	default:
		return -1
	}
}
