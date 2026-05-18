package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/logging"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

var log = logging.WithSubsystem("runtime")

const eventReplayCacheTTL = 250 * time.Millisecond

// Service exposes the runtime-facing API around agents, approvals, artifacts,
// events, and memory.
type Service struct {
	agent                *agent.AgentComponent
	sessions             agent.SessionStore
	runs                 agent.RunStore
	approvals            approval.Store
	artifacts            artifact.Store
	memory               agent.MemoryStore
	projects             agent.ProjectStore
	retention            time.Duration
	dataRetention        DataRetentionPolicy
	events               eventbus.Snapshotter
	eventReader          EventReplayReader
	agentRouter          atomic.Pointer[AgentRouter]
	rateLimiter          *SessionRateLimiter
	clock                Clock
	scheduler            Scheduler
	ingressClassifier    InteractionIngressClassifier
	classifier           InteractionClassifier
	automationClassifier AutomationIntentClassifier
	evalRunner           EvalRunner
	directives           agent.SessionDirectiveStore
	grantStore           *approval.GrantStore
	dispatching          sync.Map
	bgCtx                context.Context
	bgCancel             context.CancelFunc
	bgWG                 sync.WaitGroup
	configSnap           atomic.Pointer[controlplane.EffectiveConfigSnapshot]
	approvalSync         ApprovalSyncer
	governance           GovernanceDeliveryController
	eventCacheMu         sync.RWMutex
	eventCacheAt         time.Time
	eventCache           []eventbus.Event
	minActionConfidence  float64
	verificationPolicy   verifyrt.Policy
	releaseGatePolicy    ReleaseExecutionGatePolicy
}

type sessionMediaStoreSetter interface {
	SetMediaStore(agent.MediaStore)
}

type artifactMediaStore struct {
	store artifact.Store
}

func (s artifactMediaStore) Put(ctx context.Context, kind, contentType string, body []byte) (string, error) {
	if s.store == nil {
		return "", agent.ErrArtifactStoreNil
	}
	blob, err := s.store.Put(ctx, artifact.PutRequest{
		Kind:        kind,
		ContentType: contentType,
		Body:        append([]byte(nil), body...),
	})
	if err != nil {
		return "", err
	}
	if blob == nil {
		return "", fmt.Errorf("artifact store returned nil blob")
	}
	return blob.URI, nil
}

// NewService wires the core runtime dependencies and creates background
// dispatch context used for asynchronous run execution.
func NewService(component *agent.AgentComponent, sessions agent.SessionStore, runs agent.RunStore, approvals approval.Store, events eventbus.Snapshotter, artifacts artifact.Store) *Service {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	configureSessionMediaStore(sessions, artifacts)
	return &Service{
		agent:               component,
		sessions:            sessions,
		runs:                runs,
		approvals:           approvals,
		artifacts:           artifacts,
		events:              events,
		bgCtx:               bgCtx,
		bgCancel:            bgCancel,
		minActionConfidence: defaultMinActionConfidence,
		verificationPolicy:  verifyrt.DefaultPolicy(),
	}
}

// WithApprovals updates the approval store used by runtime and agent-layer
// approval flows after service construction.
func (s *Service) WithApprovals(store approval.Store) *Service {
	if s == nil {
		return nil
	}
	s.approvals = store
	if s.agent != nil {
		s.agent.WithApprovals(store)
	}
	return s
}

func configureSessionMediaStore(sessions agent.SessionStore, store artifact.Store) {
	setter, ok := sessions.(sessionMediaStoreSetter)
	if !ok {
		return
	}
	if store == nil {
		setter.SetMediaStore(nil)
		return
	}
	setter.SetMediaStore(artifactMediaStore{store: store})
}

// Agent returns the agent component backing this runtime service.
func (s *Service) Agent() *agent.AgentComponent {
	return s.agent
}

// Close stops background dispatchers and waits until in-flight work finishes or
// ctx is cancelled.
func (s *Service) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
	if s.bgCancel != nil {
		s.bgCancel()
	}
	done := make(chan struct{})
	go func() {
		s.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WithMemoryStore sets the optional memory KV store for memory API endpoints.
func (s *Service) WithMemoryStore(store agent.MemoryStore) *Service {
	s.memory = store
	if s.agent != nil {
		s.agent.WithMemoryStore(store)
	}
	return s
}

// WithProjectStore sets the optional project store for project API endpoints.
func (s *Service) WithProjectStore(store agent.ProjectStore) *Service {
	s.projects = store
	return s
}

// WithEventReader sets the durable event replay source used by runtime read
// models. When present, read paths prefer this persisted source over the
// in-memory snapshot bus.
func (s *Service) WithEventReader(reader EventReplayReader) *Service {
	s.eventReader = reader
	return s
}

// WithRateLimiter sets the per-session submit rate limiter.
func (s *Service) WithRateLimiter(rl *SessionRateLimiter) *Service {
	s.rateLimiter = rl
	return s
}

// AgentRouter returns the agent router, or nil if not configured.
func (s *Service) AgentRouter() *AgentRouter {
	return s.agentRouter.Load()
}

// SetAgentRouter sets the agent router for multi-agent session key routing.
func (s *Service) SetAgentRouter(r *AgentRouter) {
	s.agentRouter.Store(r)
}

// WithClassifier sets the legacy semantic interaction classifier used only when
// the unified ingress classifier is not configured.
func (s *Service) WithClassifier(c InteractionClassifier) *Service {
	s.classifier = c
	return s
}

// WithIngressClassifier sets the authoritative natural-language ingress
// classifier used by Interact for the main product path.
func (s *Service) WithIngressClassifier(c InteractionIngressClassifier) *Service {
	s.ingressClassifier = c
	return s
}

// WithMinActionConfidence sets the minimum semantic confidence required before
// Interact can execute a side-effectful decision directly.
func (s *Service) WithMinActionConfidence(threshold float64) *Service {
	if s == nil {
		return nil
	}
	s.minActionConfidence = normalizeMinActionConfidence(threshold)
	return s
}

// WithVerificationPolicy sets verifier severity overrides used when evaluating
// run verification. Nil or empty policies preserve the default warning-based behavior.
func (s *Service) WithVerificationPolicy(policy verifyrt.Policy) *Service {
	if s == nil {
		return nil
	}
	s.verificationPolicy = policy.Normalized()
	return s
}

// WithReleaseExecutionGate sets the quality gate that applies before the first
// high-risk dispatch for a run. The zero value disables execution gating.
func (s *Service) WithReleaseExecutionGate(policy ReleaseExecutionGatePolicy) *Service {
	if s == nil {
		return nil
	}
	s.releaseGatePolicy = normalizeReleaseExecutionGatePolicy(policy)
	return s
}

// WithAutomationClassifier sets the legacy automation-intent classifier used
// only when the unified ingress classifier is not configured.
func (s *Service) WithAutomationClassifier(c AutomationIntentClassifier) *Service {
	s.automationClassifier = c
	return s
}

// WithEvalRunner sets the optional evaluation runner used by eval endpoints.
func (s *Service) WithEvalRunner(r EvalRunner) *Service {
	s.evalRunner = r
	return s
}

// WithDirectives sets the session directive store used by Interact for steering.
func (s *Service) WithDirectives(d agent.SessionDirectiveStore) *Service {
	s.directives = d
	return s
}

// WithGrantStore sets the approval grant store used by approval and governance
// workflows.
func (s *Service) WithGrantStore(store *approval.GrantStore) *Service {
	s.grantStore = store
	if s.agent != nil {
		s.agent.WithGrantStore(store)
	}
	return s
}

// WithArtifactRetention sets how long artifact pruning keeps runtime artifacts.
func (s *Service) WithArtifactRetention(retention time.Duration) *Service {
	s.retention = retention
	return s
}

// WithDataRetention sets the normalized runtime data-retention policy.
func (s *Service) WithDataRetention(policy DataRetentionPolicy) *Service {
	s.dataRetention = policy.Normalize()
	return s
}

// WithEffectiveConfigSnapshot stores a clone of the effective control-plane
// config snapshot for later run binding.
func (s *Service) WithEffectiveConfigSnapshot(snapshot *controlplane.EffectiveConfigSnapshot) *Service {
	if snapshot == nil {
		s.configSnap.Store(nil)
		return s
	}
	s.configSnap.Store(snapshot.Clone())
	return s
}

// EffectiveConfigSnapshot returns a cloned effective-config snapshot, or nil
// when none has been installed.
func (s *Service) EffectiveConfigSnapshot() *controlplane.EffectiveConfigSnapshot {
	snapshot := s.configSnap.Load()
	if snapshot == nil {
		return nil
	}
	return snapshot.Clone()
}

// ListProjects returns the known project list for project API handlers.
func (s *Service) ListProjects(ctx context.Context) ([]agent.Project, error) {
	if s == nil || s.projects == nil {
		return []agent.Project{}, nil
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name != projects[j].Name {
			return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
		}
		return projects[i].Directory < projects[j].Directory
	})
	return projects, nil
}

// FindProjectByName returns one project by its human-readable name.
func (s *Service) FindProjectByName(ctx context.Context, name string) (*agent.Project, error) {
	if s == nil || s.projects == nil {
		return nil, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	return s.projects.FindByName(ctx, name)
}

// RenameProject updates one project's human-readable name when a project store is configured.
func (s *Service) RenameProject(ctx context.Context, oldName, newName string) error {
	if s == nil || s.projects == nil {
		return nil
	}
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("project name is required")
	}
	project, err := s.FindProjectByName(ctx, oldName)
	if err != nil {
		return err
	}
	if project == nil {
		return fmt.Errorf("project %q not found", oldName)
	}
	if strings.EqualFold(oldName, newName) {
		return nil
	}
	existing, err := s.FindProjectByName(ctx, newName)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != project.ID {
		return fmt.Errorf("project %q already exists", newName)
	}
	project.Name = newName
	return s.projects.Upsert(ctx, *project)
}

// DeleteProject removes one project by name when a project store is configured.
func (s *Service) DeleteProject(ctx context.Context, name string) error {
	project, err := s.FindProjectByName(ctx, name)
	if err != nil || project == nil {
		return err
	}
	return s.projects.Delete(ctx, project.ID)
}

func (s *Service) bindEffectiveConfigSnapshot(ctx context.Context, run *agent.Run) error {
	if run == nil {
		return nil
	}
	snapshot := s.EffectiveConfigSnapshot()
	if snapshot == nil || strings.TrimSpace(snapshot.ID) == "" {
		return nil
	}
	if run.Governance != nil && strings.TrimSpace(run.Governance.EffectiveConfigSnapshotID) == snapshot.ID {
		return nil
	}
	evaluation := domaingov.Evaluation{}
	if run.Governance != nil {
		evaluation = run.Governance.Normalized()
	}
	evaluation.EffectiveConfigSnapshotID = snapshot.ID
	normalized := evaluation.Normalized()
	run.Governance = &normalized
	return s.runs.Update(ctx, run)
}

// EventSnapshot returns the latest event snapshot using a background context.
func (s *Service) EventSnapshot() []eventbus.Event {
	return s.EventSnapshotContext(context.Background())
}

// EventSnapshotContext returns events from the durable replay source when
// available, otherwise from the in-memory snapshot bus.
func (s *Service) EventSnapshotContext(ctx context.Context) []eventbus.Event {
	if events, ok := s.replayPersistedEvents(ctx); ok {
		return events
	}
	if isNilSnapshotter(s.events) {
		return nil
	}
	return s.events.Snapshot()
}

// ListApprovals lists approvals filtered only by status.
func (s *Service) ListApprovals(ctx context.Context, status approval.Status) ([]*approval.Ticket, error) {
	return s.ListApprovalsFiltered(ctx, approval.ListFilter{Status: status}, agent.ScopeFilter{})
}

// ListApprovalsFiltered lists approvals and optionally filters them by scope.
// When a scope is supplied, paging is applied after scope checks complete.
func (s *Service) ListApprovalsFiltered(ctx context.Context, filter approval.ListFilter, scope agent.ScopeFilter) ([]*approval.Ticket, error) {
	filter = filter.Normalize()
	if s.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	storeFilter := filter
	if !scope.IsZero() {
		storeFilter.Limit = 0
		storeFilter.Offset = 0
	}
	items, err := s.approvals.List(ctx, storeFilter)
	if err != nil {
		return nil, err
	}
	if scope.IsZero() {
		return items, nil
	}
	filtered := make([]*approval.Ticket, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		match, err := s.approvalMatchesScope(ctx, item, scope)
		if err != nil {
			return nil, err
		}
		if match {
			filtered = append(filtered, item)
		}
	}
	if filter.Offset > 0 {
		if filter.Offset >= len(filtered) {
			return nil, nil
		}
		filtered = filtered[filter.Offset:]
	}
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[:filter.Limit]
	}
	return filtered, nil
}

// GetApproval loads an approval ticket without scope filtering.
func (s *Service) GetApproval(ctx context.Context, id string) (*approval.Ticket, error) {
	return s.GetApprovalScoped(ctx, id, agent.ScopeFilter{})
}

// GetApprovalScoped loads an approval ticket and returns not found when it
// falls outside the supplied scope filter.
func (s *Service) GetApprovalScoped(ctx context.Context, id string, scope agent.ScopeFilter) (*approval.Ticket, error) {
	if s.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	ticket, err := s.approvals.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.IsZero() {
		return ticket, nil
	}
	match, err := s.approvalMatchesScope(ctx, ticket, scope)
	if err != nil {
		return nil, err
	}
	if !match {
		return nil, fmt.Errorf("approval %s not found", id)
	}
	return ticket, nil
}

// GetApprovalByExternal loads an approval by provider-specific external ID.
func (s *Service) GetApprovalByExternal(ctx context.Context, provider, externalID string) (*approval.Ticket, error) {
	if s.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	return s.approvals.GetByExternal(ctx, provider, externalID)
}

// UpsertApprovalExternalRef attaches or updates one external approval reference
// on the target ticket.
func (s *Service) UpsertApprovalExternalRef(ctx context.Context, id string, ref approval.ExternalReference) (*approval.Ticket, error) {
	if s.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	return s.approvals.UpsertExternalRef(ctx, id, ref)
}

// FindPendingApproval returns the newest pending approval ticket for a session.
// sessionID must be non-empty or an error is returned.
func (s *Service) FindPendingApproval(ctx context.Context, sessionID string) (*approval.Ticket, error) {
	if s.approvals == nil {
		return nil, agent.ErrApprovalStoreNil
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	items, err := s.approvals.List(ctx, approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		return nil, err
	}
	var latest *approval.Ticket
	for _, item := range items {
		if item == nil || item.SessionID != sessionID {
			continue
		}
		if latest == nil || item.CreatedAt.After(latest.CreatedAt) {
			latest = item
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("pending approval for session %s not found", sessionID)
	}
	return latest, nil
}

// GetRun loads a run without scope filtering.
func (s *Service) GetRun(ctx context.Context, id string) (*agent.Run, error) {
	return s.GetRunScoped(ctx, id, agent.ScopeFilter{})
}

// GetArtifact returns artifact metadata by ID. It returns
// `agent.ErrArtifactStoreNil` when artifact storage is not configured.
func (s *Service) GetArtifact(ctx context.Context, id string) (*artifact.Blob, error) {
	if s.artifacts == nil {
		return nil, agent.ErrArtifactStoreNil
	}
	return s.artifacts.Get(ctx, id)
}

// ReadArtifact returns artifact bytes and content type by ID. It returns
// `agent.ErrArtifactStoreNil` when artifact storage is not configured.
func (s *Service) ReadArtifact(ctx context.Context, id string) ([]byte, string, error) {
	if s.artifacts == nil {
		return nil, "", agent.ErrArtifactStoreNil
	}
	return s.artifacts.Read(ctx, id)
}

// ListRuns lists runs from the store and applies any scope filter in memory
// when needed.
func (s *Service) ListRuns(ctx context.Context, filter agent.RunListFilter) ([]*agent.Run, error) {
	lister, ok := s.runs.(agent.RunLister)
	if !ok {
		return nil, fmt.Errorf("run store does not support listing")
	}
	items, err := lister.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if filter.Scope.IsZero() {
		return items, nil
	}
	filtered := make([]*agent.Run, 0, len(items))
	for _, run := range items {
		if run != nil && filter.Scope.Matches(run.Scope) {
			filtered = append(filtered, run)
		}
	}
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[:filter.Limit]
	}
	return filtered, nil
}

// ListSessions returns summarized sessions with no additional filters.
func (s *Service) ListSessions(ctx context.Context) ([]agent.SessionSummary, error) {
	return s.ListSessionsFiltered(ctx, agent.SessionListFilter{})
}

// ListSessionsFiltered returns summarized sessions that match the supplied
// filter.
func (s *Service) ListSessionsFiltered(ctx context.Context, filter agent.SessionListFilter) ([]agent.SessionSummary, error) {
	sessions, err := agent.ListSessions(ctx, s.sessions, filter)
	if err != nil {
		return nil, err
	}
	summaries := make([]agent.SessionSummary, len(sessions))
	for i, sess := range sessions {
		summaries[i] = sess.ToSummary()
	}
	return summaries, nil
}

// GetSession loads a session without scope filtering.
func (s *Service) GetSession(ctx context.Context, id string) (*agent.Session, error) {
	return s.GetSessionScoped(ctx, id, agent.ScopeFilter{})
}

// DeleteSession removes a session and all its data.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	return agent.DeleteStoredSession(ctx, s.sessions, id)
}

// StartNewEpisode seals any active episode for the session and opens a new one.
func (s *Service) StartNewEpisode(ctx context.Context, id string) (string, error) {
	manager, ok := s.sessions.(agent.SessionEpisodeManager)
	if !ok {
		return "", fmt.Errorf("session episode management is not supported")
	}
	return manager.StartNewEpisode(ctx, id, "manual")
}

// CompactSession forces emergency transcript compaction for the given session.
func (s *Service) CompactSession(ctx context.Context, id string) (*agent.Session, error) {
	if s == nil || s.agent == nil {
		return nil, fmt.Errorf("agent component is required")
	}
	return s.agent.CompactSession(ctx, id, contextengine.CompactEmergency)
}

// SubscribeEvents returns a live event subscription. Returns nil if the
// event bus does not support channel subscriptions.
func (s *Service) SubscribeEvents(bufSize int) *eventbus.Subscription {
	bus, ok := s.events.(*eventbus.InMemoryBus)
	if !ok {
		return nil
	}
	return bus.SubscribeChannel(bufSize)
}

// CancelRun requests cancellation for a run and returns the updated run state.
func (s *Service) CancelRun(ctx context.Context, runID string) (*agent.Run, error) {
	if s.agent == nil {
		return nil, fmt.Errorf("agent component is required")
	}
	return s.agent.CancelRun(ctx, runID)
}

// EventsSince returns events after the supplied cursor and reports whether the
// cursor was found, expired, or omitted.
func (s *Service) EventsSince(sinceID string, limit int) eventbus.CursorResult {
	if events, ok := s.replayPersistedEvents(context.Background()); ok {
		return cursorResultFromEvents(events, sinceID, limit)
	}
	bus, ok := s.events.(*eventbus.InMemoryBus)
	if ok {
		return bus.SnapshotSinceWithStatus(sinceID, limit)
	}
	// Fallback: full snapshot, no cursor metadata.
	all := s.EventSnapshot()
	if sinceID == "" {
		if limit > 0 && len(all) > limit {
			all = all[len(all)-limit:]
		}
		nextCursor := ""
		if len(all) > 0 {
			nextCursor = all[len(all)-1].ID
		}
		return eventbus.CursorResult{
			Events:     all,
			Status:     eventbus.CursorEmpty,
			NextCursor: nextCursor,
		}
	}
	found := false
	start := 0
	for i, e := range all {
		if e.ID == sinceID {
			start = i + 1
			found = true
			break
		}
	}
	status := eventbus.CursorOK
	if !found {
		start = 0
		status = eventbus.CursorExpired
	}
	out := all[start:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	nextCursor := ""
	if len(out) > 0 {
		nextCursor = out[len(out)-1].ID
	}
	return eventbus.CursorResult{
		Events:     out,
		Status:     status,
		NextCursor: nextCursor,
	}
}

func (s *Service) replayPersistedEvents(ctx context.Context) ([]eventbus.Event, bool) {
	if s == nil || s.eventReader == nil {
		return nil, false
	}
	if cached, ok := s.cachedReplayEvents(); ok {
		return cached, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		events []eventbus.Event
		err    error
	)
	events, err = s.eventReader.ReplayContext(ctx)
	if err != nil {
		log.Warn("runtime event replay failed; falling back to live snapshot", "error", err)
		return nil, false
	}
	s.storeReplayEvents(events)
	if len(events) == 0 {
		return nil, true
	}
	return cloneEvents(events), true
}

func (s *Service) cachedReplayEvents() ([]eventbus.Event, bool) {
	if s == nil {
		return nil, false
	}
	s.eventCacheMu.RLock()
	defer s.eventCacheMu.RUnlock()
	if s.eventCacheAt.IsZero() || s.runtimeClock().Since(s.eventCacheAt) > eventReplayCacheTTL {
		return nil, false
	}
	return cloneEvents(s.eventCache), true
}

func (s *Service) storeReplayEvents(events []eventbus.Event) {
	if s == nil {
		return
	}
	s.eventCacheMu.Lock()
	defer s.eventCacheMu.Unlock()
	s.eventCache = cloneEvents(events)
	s.eventCacheAt = s.runtimeClock().Now()
}

func cloneEvents(events []eventbus.Event) []eventbus.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]eventbus.Event, len(events))
	copy(out, events)
	return out
}

func cursorResultFromEvents(all []eventbus.Event, sinceID string, limit int) eventbus.CursorResult {
	if sinceID == "" {
		if limit > 0 && len(all) > limit {
			all = all[len(all)-limit:]
		}
		nextCursor := ""
		if len(all) > 0 {
			nextCursor = all[len(all)-1].ID
		}
		return eventbus.CursorResult{
			Events:     all,
			Status:     eventbus.CursorEmpty,
			NextCursor: nextCursor,
		}
	}
	found := false
	start := 0
	for i, e := range all {
		if e.ID == sinceID {
			start = i + 1
			found = true
			break
		}
	}
	status := eventbus.CursorOK
	if !found {
		start = 0
		status = eventbus.CursorExpired
	}
	out := all[start:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	nextCursor := ""
	if len(out) > 0 {
		nextCursor = out[len(out)-1].ID
	}
	return eventbus.CursorResult{
		Events:     out,
		Status:     status,
		NextCursor: nextCursor,
	}
}

func isNilSnapshotter(snapshotter eventbus.Snapshotter) bool {
	if snapshotter == nil {
		return true
	}
	value := reflect.ValueOf(snapshotter)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}

func (s *Service) publish(ctx context.Context, event eventbus.Event) error {
	publisher, ok := s.events.(eventbus.Bus)
	if !ok {
		return nil
	}
	return publisher.Publish(ctx, event)
}

func (s *Service) dispatchRun(ctx context.Context, runID string, resume bool) error {
	if s.agent == nil {
		return fmt.Errorf("agent component is required")
	}
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	if _, loaded := s.dispatching.LoadOrStore(runID, struct{}{}); loaded {
		return nil
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		s.dispatching.Delete(runID)
		return err
	}
	held, err := s.applyReleaseExecutionGate(ctx, run)
	if err != nil {
		s.dispatching.Delete(runID)
		return err
	}
	if held {
		s.dispatching.Delete(runID)
		return nil
	}

	bg, release := s.backgroundDispatchContext(ctx)
	s.bgWG.Add(1)
	s.runtimeScheduler().Go(func() {
		defer s.bgWG.Done()
		defer release()
		defer s.dispatching.Delete(runID)
		sessionID := ""
		var err error
		if resume {
			if run, getErr := s.runs.Get(bg, runID); getErr == nil && run != nil {
				sessionID = strings.TrimSpace(run.SessionID)
			}
			err = s.agent.ResumeRun(bg, runID)
		} else {
			run, getErr := s.runs.Get(bg, runID)
			if getErr != nil {
				err = getErr
			} else {
				sessionID = strings.TrimSpace(run.SessionID)
				err = s.agent.ExecuteRun(bg, run)
			}
		}
		if err != nil {
			logging.LogIfErr(bg, s.markBackgroundFailure(bg, runID, err), "mark background failure failed")
		}
		if verifyErr := s.recordRunMemoryVerification(bg, runID); verifyErr != nil {
			log.Warn("record run memory verification failed", "run_id", runID, "error", verifyErr)
		}
		if s.checkAndDispatchWorkflowContinuation(bg, runID) {
			return
		}
		if strings.TrimSpace(sessionID) != "" {
			logging.LogIfErr(bg, s.dispatchNextQueuedRun(bg, sessionID, runID), "dispatch next queued run failed", slog.String("run_id", runID), slog.String("session_id", sessionID))
		}
	})

	return nil
}

func (s *Service) backgroundDispatchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	bg := context.WithoutCancel(ctx)
	if s == nil || s.bgCtx == nil {
		return bg, func() {}
	}
	runCtx, cancel := context.WithCancel(bg)
	stop := context.AfterFunc(s.bgCtx, cancel)
	return runCtx, func() {
		stop()
		cancel()
	}
}

func (s *Service) markBackgroundFailure(ctx context.Context, runID string, err error) error {
	if s.agent == nil {
		return fmt.Errorf("agent component is required")
	}
	return s.agent.MarkBackgroundFailure(ctx, runID, err)
}

func (s *Service) dispatchNextQueuedRun(ctx context.Context, sessionID, finishedRunID string) error {
	if s == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	current, err := s.runs.Get(ctx, finishedRunID)
	if err != nil {
		return err
	}
	if current == nil || current.Status.Active() {
		return nil
	}

	coord := s.agent.Coordinator()
	if coord == nil {
		return nil
	}
	nextRunID, ok, err := coord.NextQueuedRun(ctx, sessionID)
	if err != nil {
		return err
	}
	nextRunID = strings.TrimSpace(nextRunID)
	if !ok || nextRunID == "" || nextRunID == strings.TrimSpace(finishedRunID) {
		return nil
	}
	return s.dispatchRun(ctx, nextRunID, false)
}

func (s *Service) recordRunMemoryVerification(ctx context.Context, runID string) error {
	if s == nil || strings.TrimSpace(runID) == "" || s.memory == nil {
		return nil
	}
	verifier, ok := s.memory.(agent.MemoryVerificationStore)
	if !ok {
		return nil
	}

	base, err := s.getRunResultBase(ctx, runID)
	if err != nil {
		return err
	}
	if base == nil || base.run == nil || base.session == nil || !base.run.Status.Terminal() {
		return nil
	}
	verification, err := s.getRunVerification(ctx, base.run, base.result, base.session)
	if err != nil {
		return err
	}

	var passed bool
	switch verification.Status {
	case verifyrt.StatusPassed:
		passed = true
	case verifyrt.StatusFailed:
		passed = false
	default:
		return nil
	}

	entries, err := s.memory.List(ctx)
	if err != nil {
		return err
	}
	for _, key := range collectRunVerificationMemoryKeys(entries, base.session.Key) {
		if _, err := verifier.TouchMemoryVerification(ctx, key, passed); err != nil {
			return err
		}
	}
	return nil
}

func collectRunVerificationMemoryKeys(entries []agent.MemoryEntry, sessionKey string) []string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || len(entries) == 0 {
		return nil
	}

	projectSet := map[string]struct{}{"": {}}
	for _, entry := range entries {
		if entry.State == agent.MemorySuperseded {
			continue
		}
		if strings.TrimSpace(entry.SessionKey) != sessionKey {
			continue
		}
		if projectID := strings.TrimSpace(entry.ProjectID); projectID != "" {
			projectSet[projectID] = struct{}{}
		}
	}

	projectIDs := make([]string, 0, len(projectSet))
	for projectID := range projectSet {
		projectIDs = append(projectIDs, projectID)
	}
	sort.Strings(projectIDs)

	seen := make(map[string]struct{})
	keys := make([]string, 0, len(entries))
	for _, projectID := range projectIDs {
		recalled := agent.RecallForContext(entries, sessionKey, projectID)
		for _, entry := range recalled.Memories {
			key := strings.TrimSpace(entry.Key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Memory KV store
// ---------------------------------------------------------------------------

var errMemoryStoreNil = fmt.Errorf("memory store is not configured")

type MemoryStatus struct {
	StoreType  string
	EntryCount int
	IndexReady bool
}

type MemoryReindexResult struct {
	Status  string
	Indexed int
}

// GetMemory returns one memory entry by key. It returns an error when no memory
// store is configured.
func (s *Service) GetMemory(ctx context.Context, key string) (*agent.MemoryEntry, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	return s.memory.Get(ctx, key)
}

// SetMemory writes one key/value memory entry. It returns an error when no
// memory store is configured.
func (s *Service) SetMemory(ctx context.Context, key, value string) error {
	if s.memory == nil {
		return errMemoryStoreNil
	}
	return s.memory.Set(ctx, key, value)
}

// DeleteMemory removes one memory entry by key. It returns an error when no
// memory store is configured.
func (s *Service) DeleteMemory(ctx context.Context, key string) error {
	if s.memory == nil {
		return errMemoryStoreNil
	}
	return s.memory.Delete(ctx, key)
}

// SearchMemory returns memory entries matching a query. It returns an error
// when no memory store is configured.
func (s *Service) SearchMemory(ctx context.Context, query string) ([]agent.MemoryEntry, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	return s.memory.Search(ctx, query)
}

// ListMemoryFiltered returns memory entries matching the supplied filter. When
// the store lacks native filtering, query matching is emulated in memory.
func (s *Service) ListMemoryFiltered(ctx context.Context, filter agent.MemoryFilter) ([]agent.MemoryEntry, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	if managed, ok := s.memory.(agent.ManagedMemoryStore); ok {
		return managed.ListFiltered(ctx, filter)
	}
	if strings.TrimSpace(filter.Query) != "" {
		results, err := s.memory.Search(ctx, filter.Query)
		if err != nil {
			return nil, err
		}
		return filterMemoryEntries(results, filter), nil
	}
	results, err := s.memory.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterMemoryEntries(results, filter), nil
}

// ListMemory returns all memory entries from the configured store.
func (s *Service) ListMemory(ctx context.Context) ([]agent.MemoryEntry, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	return s.memory.List(ctx)
}

func (s *Service) GetMemoryStatus(ctx context.Context) (MemoryStatus, error) {
	if s.memory == nil {
		return MemoryStatus{}, errMemoryStoreNil
	}
	entries, err := s.memory.List(ctx)
	if err != nil {
		return MemoryStatus{}, err
	}

	status := MemoryStatus{
		StoreType:  describeMemoryStoreType(s.memory),
		EntryCount: len(entries),
		IndexReady: true,
	}
	hasEmbeddingProvider, hasEmbedding := s.memory.(interface{ HasEmbedding() bool })
	if !hasEmbedding || !hasEmbeddingProvider.HasEmbedding() {
		return status, nil
	}
	statsProvider, ok := s.memory.(interface{ VectorStats() (int, int) })
	if !ok {
		status.IndexReady = false
		return status, nil
	}
	vectorCount, _ := statsProvider.VectorStats()
	status.IndexReady = vectorCount >= len(entries)
	return status, nil
}

func (s *Service) ReindexMemory(ctx context.Context, force bool) (MemoryReindexResult, error) {
	status, err := s.GetMemoryStatus(ctx)
	if err != nil {
		return MemoryReindexResult{}, err
	}

	result := MemoryReindexResult{
		Status:  "noop",
		Indexed: status.EntryCount,
	}
	indexer, ok := s.memory.(agent.MemoryIndexer)
	if !ok {
		return result, nil
	}

	indexed, err := indexer.Reindex(ctx, force)
	if err != nil {
		return MemoryReindexResult{}, err
	}
	result.Indexed = indexed
	result.Status = "rebuilt"
	if force {
		result.Status = "rebuilt_forced"
	}
	return result, nil
}

// UpsertMemoryRecord writes a structured memory record and returns the stored
// entry. record.Key must be non-empty when native upsert support is absent.
func (s *Service) UpsertMemoryRecord(ctx context.Context, record agent.MemoryRecord) (*agent.MemoryEntry, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	if managed, ok := s.memory.(agent.ManagedMemoryStore); ok {
		return managed.UpsertRecord(ctx, record)
	}
	key := strings.TrimSpace(record.Key)
	if key == "" {
		return nil, fmt.Errorf("memory record key is required")
	}
	if err := s.memory.Set(ctx, key, record.Value); err != nil {
		return nil, err
	}
	return s.memory.Get(ctx, key)
}

func describeMemoryStoreType(store agent.MemoryStore) string {
	if provider, ok := store.(agent.MemoryStoreMetadataProvider); ok {
		return provider.StoreType()
	}
	return reflect.TypeOf(store).String()
}

// GetMemoryNotebook returns the notebook snapshot exposed by the configured
// memory store, or an error when the feature is unavailable.
func (s *Service) GetMemoryNotebook(ctx context.Context) (*agent.MemoryNotebookSnapshot, error) {
	if s.memory == nil {
		return nil, errMemoryStoreNil
	}
	provider, ok := s.memory.(agent.MemoryNotebookProvider)
	if !ok {
		return nil, fmt.Errorf("memory notebook not available")
	}
	return provider.NotebookSnapshot(ctx)
}

// ---------------------------------------------------------------------------
// One-shot execution
// ---------------------------------------------------------------------------

const (
	oneShotPollInterval = 250 * time.Millisecond
	oneShotTimeout      = 5 * time.Minute
	oneShotSessionKey   = "_oneshot"
)

// ExecuteOneShot runs a single prompt and returns the response text.
// It creates a temporary session, submits the message, polls until
// the run completes, and returns the concatenated assistant response.
func (s *Service) ExecuteOneShot(ctx context.Context, message, model string) (string, error) {
	if s.agent == nil {
		return "", fmt.Errorf("agent component is required")
	}

	sessionKey := fmt.Sprintf("%s:%d", oneShotSessionKey, s.runtimeClock().Now().UnixNano())

	run, err := s.Submit(ctx, SubmitRequest{
		SessionKey: sessionKey,
		Content:    message,
		Model:      model,
	})
	if err != nil {
		return "", fmt.Errorf("submit one-shot: %w", err)
	}

	deadline := s.runtimeClock().NewTimer(oneShotTimeout)
	ticker := s.runtimeClock().NewTicker(oneShotPollInterval)
	defer deadline.Stop()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C():
			return "", fmt.Errorf("one-shot execution timed out after %s", oneShotTimeout)
		case <-ticker.C():
			run, err = s.runs.Get(ctx, run.ID)
			if err != nil {
				return "", fmt.Errorf("poll run status: %w", err)
			}
			switch run.Status {
			case agent.RunCompleted:
				return s.extractOneShotResponse(ctx, run.SessionID)
			case agent.RunFailed:
				return "", fmt.Errorf("run failed: %s", run.Error)
			case agent.RunCancelled:
				return "", fmt.Errorf("run was cancelled")
			}
		}
	}
}

// extractOneShotResponse loads the session and returns the last assistant
// message content.
func (s *Service) extractOneShotResponse(ctx context.Context, sessionID string) (string, error) {
	session, err := agent.LoadSession(ctx, s.sessions, sessionID, agent.ScopeFilter{})
	if err != nil {
		if errors.Is(err, agent.ErrSessionReadUnsupported) {
			return "", fmt.Errorf("session store does not support reading")
		}
		return "", fmt.Errorf("load session: %w", err)
	}
	if session == nil {
		return "", fmt.Errorf("load session: session %s not found", sessionID)
	}

	// Walk messages backwards to find the last assistant response.
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role == "assistant" && msg.Content != "" {
			return msg.Content, nil
		}
	}
	return "", fmt.Errorf("no assistant response found")
}
