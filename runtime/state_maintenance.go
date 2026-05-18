package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

type DataRetentionPolicy struct {
	Sessions time.Duration
	Runs     time.Duration
	Events   time.Duration
	Interval time.Duration
}

func (p DataRetentionPolicy) Normalize() DataRetentionPolicy {
	if p.Sessions < 0 {
		p.Sessions = 0
	}
	if p.Runs < 0 {
		p.Runs = 0
	}
	if p.Events < 0 {
		p.Events = 0
	}
	if p.Enabled() && p.Interval <= 0 {
		p.Interval = defaultPruneInterval
	}
	return p
}

func (p DataRetentionPolicy) Enabled() bool {
	return p.Sessions > 0 || p.Runs > 0 || p.Events > 0
}

type StatePruneResult struct {
	SessionsDeleted int       `json:"sessions_deleted"`
	RunsDeleted     int       `json:"runs_deleted"`
	EventsDeleted   int       `json:"events_deleted"`
	SessionBefore   time.Time `json:"session_before,omitempty"`
	RunBefore       time.Time `json:"run_before,omitempty"`
	EventBefore     time.Time `json:"event_before,omitempty"`
}

type EventPruner interface {
	PruneEvents(ctx context.Context, before time.Time) (int, error)
}

type StatePruner struct {
	service  *Service
	interval time.Duration
	mu       sync.Mutex
	cancel   context.CancelFunc
}

func NewStatePruner(service *Service, interval time.Duration) *StatePruner {
	if interval <= 0 {
		interval = defaultPruneInterval
	}
	return &StatePruner{
		service:  service,
		interval: interval,
	}
}

func (p *StatePruner) Start(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.mu.Unlock()
	go p.loop(ctx)
}

func (p *StatePruner) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *StatePruner) loop(ctx context.Context) {
	ticker := p.service.runtimeClock().NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			result, err := p.service.PruneState(ctx)
			if err != nil {
				log.Warn("state auto-prune failed", "error", err)
				continue
			}
			if result != nil && (result.SessionsDeleted > 0 || result.RunsDeleted > 0 || result.EventsDeleted > 0) {
				log.Info("state auto-prune completed",
					"sessions_deleted", result.SessionsDeleted,
					"runs_deleted", result.RunsDeleted,
					"events_deleted", result.EventsDeleted,
				)
			}
		}
	}
}

func (s *Service) GetRunScoped(ctx context.Context, id string, scope agent.ScopeFilter) (*agent.Run, error) {
	if scoped, ok := s.runs.(agent.ScopedRunReader); ok {
		return scoped.GetScoped(ctx, id, scope)
	}
	run, err := s.runs.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !scope.Matches(run.Scope) {
		return nil, fmt.Errorf("run %s not found", id)
	}
	return run, nil
}

func (s *Service) GetSessionScoped(ctx context.Context, id string, scope agent.ScopeFilter) (*agent.Session, error) {
	return agent.LoadSession(ctx, s.sessions, id, scope)
}

func (s *Service) getSessionMetadataScoped(ctx context.Context, id string, scope agent.ScopeFilter) (*agent.Session, error) {
	return agent.LoadSessionMetadata(ctx, s.sessions, id, scope)
}

func (s *Service) GetSessionSummaryScoped(ctx context.Context, id string, scope agent.ScopeFilter) (agent.SessionSummary, error) {
	session, err := s.getSessionMetadataScoped(ctx, id, scope)
	if err != nil {
		return agent.SessionSummary{}, err
	}
	return session.ToSummary(), nil
}

func (s *Service) RecoverOrphanedRuns(ctx context.Context, reason string) (int, error) {
	runs, err := s.ListRuns(ctx, agent.RunListFilter{})
	if err != nil {
		return 0, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "process_restart"
	}
	recovered := 0
	for _, run := range runs {
		if run == nil || run.Status.Terminal() {
			continue
		}
		if err := s.cancelOrphanedApproval(ctx, run, reason); err != nil {
			return recovered, err
		}
		previousStatus := run.Status
		run.Status = agent.RunFailed
		run.ApprovalID = ""
		run.PendingTools = nil
		run.Error = reason
		run.FinishedAt = s.nowUTC()
		if err := s.runs.Update(ctx, run); err != nil {
			return recovered, fmt.Errorf("recover orphaned run %s: %w", run.ID, err)
		}
		loggingContext := eventbus.RunStatusAttrs{
			Channel: runtimeRunChannel(ctx, s.sessions, run),
			Error:   reason,
			Summary: fmt.Sprintf("run recovered from %s after process restart", previousStatus),
		}
		if err := s.publish(ctx, eventbus.NewRunStatusEvent(
			eventbus.EventRunFailed,
			run.ID,
			run.SessionID,
			loggingContext,
			nil,
		)); err != nil {
			log.Warn("publish orphaned run failure event failed", "run_id", run.ID, "error", err)
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) PruneState(ctx context.Context) (*StatePruneResult, error) {
	policy := s.dataRetention.Normalize()
	if !policy.Enabled() {
		return nil, fmt.Errorf("state prune requires session, run, or event retention")
	}
	now := s.nowUTC()
	result := &StatePruneResult{}
	if policy.Sessions > 0 {
		before := now.Add(-policy.Sessions)
		activeSessionIDs, err := s.activeSessionIDs(ctx)
		if err != nil {
			return nil, err
		}
		deleted, err := s.pruneSessions(ctx, before, activeSessionIDs)
		if err != nil {
			return nil, err
		}
		result.SessionsDeleted = deleted
		result.SessionBefore = before
	}
	if policy.Runs > 0 {
		pruner, ok := s.runs.(agent.RunPruner)
		if !ok {
			return nil, fmt.Errorf("run store does not support pruning")
		}
		before := now.Add(-policy.Runs)
		deleted, err := pruner.PruneRuns(ctx, before)
		if err != nil {
			return nil, err
		}
		result.RunsDeleted = deleted
		result.RunBefore = before
	}
	if policy.Events > 0 {
		before := now.Add(-policy.Events)
		pruner, ok := s.eventReader.(EventPruner)
		if !ok {
			pruner, ok = s.events.(EventPruner)
		}
		if !ok {
			return nil, fmt.Errorf("event store does not support pruning")
		}
		deleted, err := pruner.PruneEvents(ctx, before)
		if err != nil {
			return nil, err
		}
		result.EventsDeleted = deleted
		result.EventBefore = before
	}
	return result, nil
}

func (s *Service) activeSessionIDs(ctx context.Context) ([]string, error) {
	lister, ok := s.runs.(agent.RunLister)
	if !ok {
		return nil, nil
	}
	runs, err := lister.List(ctx, agent.RunListFilter{})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(runs))
	active := make([]string, 0, len(runs))
	for _, run := range runs {
		if run == nil || !run.Status.Active() || strings.TrimSpace(run.SessionID) == "" {
			continue
		}
		if _, exists := seen[run.SessionID]; exists {
			continue
		}
		seen[run.SessionID] = struct{}{}
		active = append(active, run.SessionID)
	}
	return active, nil
}

func (s *Service) pruneSessions(ctx context.Context, before time.Time, activeSessionIDs []string) (int, error) {
	return agent.PruneStoredSessions(ctx, s.sessions, before, activeSessionIDs)
}

func (s *Service) cancelOrphanedApproval(ctx context.Context, run *agent.Run, reason string) error {
	if s == nil || s.approvals == nil || run == nil {
		return nil
	}
	var (
		ticket *approval.Ticket
		err    error
	)
	if approvalID := strings.TrimSpace(run.ApprovalID); approvalID != "" {
		ticket, err = s.approvals.Get(ctx, approvalID)
	} else if strings.TrimSpace(run.ID) != "" {
		ticket, err = s.approvals.GetByRun(ctx, run.ID)
	}
	if err != nil || ticket == nil || ticket.Status != approval.StatusPending {
		return nil
	}
	_, err = s.approvals.Resolve(ctx, ticket.ID, approval.Resolution{
		Status:     approval.StatusCancelled,
		ResolvedBy: "system_restart",
		Note:       reason,
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already resolved") {
		return fmt.Errorf("cancel orphaned approval %s: %w", ticket.ID, err)
	}
	return nil
}

func runtimeRunChannel(ctx context.Context, sessions agent.SessionStore, run *agent.Run) string {
	if run == nil || sessions == nil || strings.TrimSpace(run.SessionID) == "" {
		return ""
	}
	session, err := agent.LoadSession(ctx, sessions, run.SessionID, agent.ScopeFilter{})
	if err != nil || session == nil {
		return ""
	}
	key := strings.TrimSpace(session.Key)
	if key == "" {
		return ""
	}
	channel, _, ok := strings.Cut(key, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(channel)
}
