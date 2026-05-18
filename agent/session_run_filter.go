package agent

import (
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
)

func filterSessionMessagesForRun(session *Session, runID string) {
	if session == nil || strings.TrimSpace(runID) == "" || len(session.Messages) == 0 {
		return
	}

	filtered := make([]contextengine.Message, 0, len(session.Messages))
	for _, msg := range session.Messages {
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		filtered = append(filtered, msg)
	}
	session.Messages = filtered
	session.MessageCount = len(filtered)
}
