package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type channelRunRestorer interface {
	RestoreRun(ctx context.Context, target channels.RunNotificationTarget, run *agent.Run) bool
}

func (a *App) syncActiveChannelSessionsForReload(ctx context.Context, changedNames []string, activeBridges []namedChannelBridge) {
	if a == nil || len(changedNames) == 0 || a.Sessions == nil || a.Runs == nil {
		return
	}
	runLister, ok := a.Runs.(agent.RunLister)
	if !ok {
		return
	}
	sessions, err := agent.ListSessions(ctx, a.Sessions, agent.SessionListFilter{})
	if err != nil {
		log.Warn("channel reload session sync skipped: list sessions failed", "error", err)
		return
	}
	if len(sessions) == 0 {
		return
	}

	changed := make(map[string]struct{}, len(changedNames))
	for _, name := range changedNames {
		name = strings.TrimSpace(name)
		if name != "" {
			changed[name] = struct{}{}
		}
	}
	if len(changed) == 0 {
		return
	}

	restorers := make(map[string]channelRunRestorer, len(activeBridges))
	for _, entry := range activeBridges {
		restorer, ok := entry.bridge.(channelRunRestorer)
		if !ok || strings.TrimSpace(entry.name) == "" {
			continue
		}
		restorers[entry.name] = restorer
	}
	if len(restorers) == 0 {
		return
	}

	for _, listed := range sessions {
		if listed == nil || strings.TrimSpace(listed.ID) == "" {
			continue
		}
		session, err := agent.LoadSession(ctx, a.Sessions, listed.ID, agent.ScopeFilter{})
		if err != nil || session == nil {
			continue
		}
		channelName := strings.TrimSpace(agent.SessionChannelName(session))
		if _, ok := changed[channelName]; !ok {
			continue
		}
		restorer, ok := restorers[channelName]
		if !ok {
			continue
		}
		_, ok = runNotificationTargetFromSession(session, nil)
		if !ok {
			log.Warn("channel reload session sync skipped: missing delivery target", "channel", channelName, "session_id", session.ID)
			continue
		}
		runs, err := runLister.List(ctx, agent.RunListFilter{SessionID: session.ID, Limit: 16})
		if err != nil {
			log.Warn("channel reload session sync skipped: list runs failed", "channel", channelName, "session_id", session.ID, "error", err)
			continue
		}
		restored := 0
		for _, run := range runs {
			if run == nil || !runStatusNeedsReloadTracking(run.Status) {
				continue
			}
			runTarget, ok := runNotificationTargetFromSession(session, run)
			if !ok {
				log.Warn("channel reload session sync skipped: missing run target", "channel", channelName, "session_id", session.ID, "run_id", run.ID)
				continue
			}
			if restorer.RestoreRun(ctx, runTarget, run) {
				restored++
			}
		}
		if restored > 0 {
			log.Info("channel reload restored active session tracking", "channel", channelName, "session_id", session.ID, "runs", restored)
		}
	}
}

func runStatusNeedsReloadTracking(status agent.RunStatus) bool {
	switch status {
	case agent.RunQueued, agent.RunRunning, agent.RunStreaming, agent.RunWaitingApproval, agent.RunWaitingInput:
		return true
	default:
		return false
	}
}

func runNotificationTargetFromSession(session *agent.Session, run *agent.Run) (channels.RunNotificationTarget, bool) {
	if session == nil {
		return channels.RunNotificationTarget{}, false
	}
	inputEventID := ""
	if run != nil {
		inputEventID = strings.TrimSpace(run.InputEventID)
	}
	target := channels.RunNotificationTarget{
		SessionKey:   session.Key,
		ChannelID:    strings.TrimSpace(agent.SessionChannelName(session)),
		Format:       "text",
		InputContent: strings.TrimSpace(channels.BridgeInputContent(session, inputEventID)),
	}
	sourceMetadata := sessionNotificationMetadata(session, inputEventID, target.ChannelID)
	if sourceMetadata == nil && len(session.Metadata) > 0 && metadataDeliveryTarget(session.Metadata) != "" {
		sourceMetadata = cloneSessionMetadataMap(session.Metadata)
	}
	if sourceMetadata == nil {
		return channels.RunNotificationTarget{}, false
	}
	if target.ChannelID == "" {
		target.ChannelID = sessionMetadataChannelName(sourceMetadata)
	}
	target.TargetID = metadataDeliveryTarget(sourceMetadata)
	target.ReplyToID = metadataReplyTarget(sourceMetadata)
	target.Metadata = sourceMetadata
	if run != nil {
		target.RunID = strings.TrimSpace(run.ID)
	}
	return target, strings.TrimSpace(target.ChannelID) != "" && strings.TrimSpace(target.TargetID) != ""
}

func sessionNotificationMetadata(session *agent.Session, inputEventID, channelName string) map[string]any {
	if session == nil {
		return nil
	}
	var (
		fallbackMetadata map[string]any
		inputMetadata    map[string]any
	)
	for idx := len(session.Messages) - 1; idx >= 0; idx-- {
		msg := session.Messages[idx]
		if len(msg.Metadata) == 0 {
			continue
		}
		if channelName != "" {
			if channel := sessionMetadataChannelName(msg.Metadata); channel != "" && channel != channelName {
				continue
			}
		}
		if inputEventID != "" {
			if messageID := strings.TrimSpace(metadataString(msg.Metadata, meta.KeyMessageID, "message_id")); messageID == inputEventID {
				inputMetadata = cloneSessionMetadataMap(msg.Metadata)
				if metadataDeliveryTarget(msg.Metadata) != "" {
					return inputMetadata
				}
			}
		}
		if fallbackMetadata == nil && metadataDeliveryTarget(msg.Metadata) != "" {
			fallbackMetadata = cloneSessionMetadataMap(msg.Metadata)
		}
	}
	if inputMetadata == nil {
		return fallbackMetadata
	}
	if fallbackMetadata == nil {
		return inputMetadata
	}
	for _, key := range []string{
		meta.KeyMessageID,
		"message_id",
		meta.KeyReplyToID,
		"reply_to_message_id",
		"reply_to_id",
		meta.KeyThreadID,
		"thread_id",
		"thread_ts",
		"root_id",
		meta.KeyReceiveIDType,
	} {
		if value, ok := inputMetadata[key]; ok {
			fallbackMetadata[key] = value
		}
	}
	if channel := sessionMetadataChannelName(inputMetadata); channel != "" {
		fallbackMetadata[meta.KeyChannelName] = channel
		fallbackMetadata[meta.KeyChannel] = channel
	}
	return fallbackMetadata
}

func sessionMetadataChannelName(metadata map[string]any) string {
	return strings.TrimSpace(metadataString(metadata, meta.KeyChannelName, meta.KeyChannel, "channel"))
}

func metadataDeliveryTarget(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	channelName := sessionMetadataChannelName(metadata)
	for _, key := range []string{"target_id", meta.KeyChatID, meta.KeyChannelID, "chat_id", "channel_id", "sender_id", "from", "channel"} {
		value := strings.TrimSpace(metadataString(metadata, key))
		if value == "" {
			continue
		}
		if key == meta.KeyChannel && channelName != "" && value == channelName {
			continue
		}
		return value
	}
	return ""
}

func metadataReplyTarget(metadata map[string]any) string {
	return strings.TrimSpace(metadataString(
		metadata,
		meta.KeyReplyToID,
		meta.KeyThreadID,
		meta.KeyMessageID,
		"reply_to_message_id",
		"reply_to_id",
		"thread_id",
		"thread_ts",
		"message_id",
		"ts",
		"root_id",
	))
}

func metadataString(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			if text := strings.TrimSpace(toString(value)); text != "" {
				return text
			}
		}
	}
	return ""
}

func cloneSessionMetadataMap(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}
