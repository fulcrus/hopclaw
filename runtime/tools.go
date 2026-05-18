package runtime

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
)

func (s *Service) ListTools(ctx context.Context, sessionKey string) ([]agent.ToolDefinition, error) {
	if s.agent == nil {
		return nil, fmt.Errorf("agent component is required")
	}
	return s.agent.ListTools(ctx, sessionKey)
}
