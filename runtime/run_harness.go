package runtime

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func (s *Service) BuildRunHarnessSummary(ctx context.Context, run *agent.Run) *agent.RunHarnessSummary {
	if run == nil {
		return nil
	}
	return agent.ProjectRunHarnessSummary(run, s.runHarnessPrompt(ctx, run))
}

func (s *Service) runHarnessPrompt(ctx context.Context, run *agent.Run) string {
	if run == nil {
		return ""
	}
	session, err := s.lookupRunSession(ctx, run.SessionID)
	if err != nil || session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role != contextengine.RoleUser {
			continue
		}
		return strings.TrimSpace(session.Messages[i].TextContent())
	}
	return ""
}
