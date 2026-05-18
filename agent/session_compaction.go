package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

func (a *AgentComponent) CompactSession(ctx context.Context, sessionID string, reason contextengine.CompactReason) (*Session, error) {
	if a == nil {
		return nil, fmt.Errorf("agent component is required")
	}
	if a.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if a.context == nil {
		return nil, fmt.Errorf("context engine is required")
	}

	session, release, err := a.sessions.LoadForExecution(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer release()

	if err := a.context.Compact(ctx, &session.Session, reason); err != nil {
		return nil, err
	}
	session.UpdatedAt = time.Now().UTC()
	if err := a.sessions.Save(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}
