package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	domainrun "github.com/fulcrus/hopclaw/internal/domain/runstate"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type SubmitRequest struct {
	SessionKey   string
	Content      string
	Model        string
	AutomationID string
	Metadata     map[string]any
	Execute      *bool
}

type Runtime interface {
	Submit(ctx context.Context, req SubmitRequest) (*runtimesvc.RunResult, error)
	GetRunResult(ctx context.Context, runID string) (*runtimesvc.RunResult, error)
}

type Runner struct {
	runtime      Runtime
	timeout      time.Duration
	pollInterval time.Duration
}

func NewRunner(runtime Runtime, timeout, pollInterval time.Duration) *Runner {
	if runtime == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}
	return &Runner{
		runtime:      runtime,
		timeout:      timeout,
		pollInterval: pollInterval,
	}
}

func (r *Runner) Run(ctx context.Context, req SubmitRequest) (*runtimesvc.RunResult, error) {
	if r == nil || r.runtime == nil {
		return nil, fmt.Errorf("automation runner is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	result, err := r.runtime.Submit(ctx, req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("submit returned nil result")
	}
	if isTerminal(result.Status) {
		return result, nil
	}
	if strings.TrimSpace(result.RunID) == "" {
		return nil, fmt.Errorf("submit returned non-terminal status without run id")
	}

	pollRunID := result.RunID
	for {
		result, err = r.runtime.GetRunResult(ctx, pollRunID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("poll returned nil result")
		}
		if result.RunID == "" {
			result.RunID = pollRunID
		}
		pollRunID = result.RunID
		if isTerminal(result.Status) {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("execution timed out after %s", r.timeout)
		case <-time.After(r.pollInterval):
		}
	}
}

func isTerminal(status agent.RunStatus) bool {
	return domainrun.Status(status).Terminal()
}
