package agent

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/internal/meta"
)

func (a *AgentComponent) resolveRunChannel(ctx context.Context, run *Run, session *Session) string {
	if session != nil {
		return SessionChannelName(session)
	}
	if a == nil || run == nil || strings.TrimSpace(run.SessionID) == "" {
		return ""
	}
	loaded, err := LoadSession(ctx, a.sessions, run.SessionID, ScopeFilter{})
	if err != nil || loaded == nil {
		return ""
	}
	return SessionChannelName(loaded)
}

func SessionChannelName(session *Session) string {
	if session == nil {
		return ""
	}
	if channel := sessionChannelNameFromMetadata(session.Metadata); channel != "" {
		return channel
	}
	for idx := len(session.Messages) - 1; idx >= 0; idx-- {
		if channel := sessionChannelNameFromMetadata(session.Messages[idx].Metadata); channel != "" {
			return channel
		}
	}
	return ""
}

func sessionChannelNameFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(submitMetadataString(metadata, meta.KeyChannelName, meta.KeyChannel, "channel"))
}
