package agent

import (
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
)

func TestSessionChannelNameUsesSessionMessageMetadata(t *testing.T) {
	t.Parallel()

	session := &Session{
		Key: "opaque-session",
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{
					Role:    contextengine.RoleUser,
					Content: "hello",
					Metadata: map[string]any{
						meta.KeyChannel: "telegram",
					},
				},
			},
		},
	}

	if got := SessionChannelName(session); got != "telegram" {
		t.Fatalf("SessionChannelName() = %q, want %q", got, "telegram")
	}
}

func TestIdleEpisodeBoundaryReasonDoesNotGuessFromSessionKeyPrefix(t *testing.T) {
	t.Parallel()

	session := &Session{
		Key: "cli-idle",
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:      contextengine.RoleUser,
				Content:   "first",
				CreatedAt: time.Now().UTC().Add(-3 * time.Hour),
			}},
		},
	}

	if got := idleEpisodeBoundaryReason(session, IncomingMessage{Content: "continue"}); got != "" {
		t.Fatalf("idleEpisodeBoundaryReason() = %q, want empty result without channel metadata", got)
	}
}
