package line

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

func TestNewBridgeStartStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	bridge := NewBridge(New(Config{}), nil, agent.NewInMemorySessionStore(), nil, time.Millisecond)
	bridge.Start(ctx)
	cancel()
	bridge.Stop()
}
