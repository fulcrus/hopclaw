package agent

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

func TestE2EFullInteractionChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newE2ESessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "The README says HopClaw is an agent runtime.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		QueueMode:     QueueEnqueue,
		MaxToolRounds: 3,
	}, sessions, runs, NewInMemoryCoordinator(), newE2EEngine(sessions), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "HopClaw is an agent runtime.",
		}},
	}, nil).WithEventBus(bus)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "e2e-full-chain",
		ExternalEventID: "evt-e2e-full-chain",
		Content:         "read the readme and summarize it",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	session, err := sessions.GetByKey(ctx, "e2e-full-chain")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) < 4 {
		t.Fatalf("len(session.Messages) = %d, want at least 4", len(session.Messages))
	}
	if session.Messages[len(session.Messages)-1].Content != "The README says HopClaw is an agent runtime." {
		t.Fatalf("final assistant message = %q", session.Messages[len(session.Messages)-1].Content)
	}
	assertEventSeen(t, bus.Snapshot(), eventbus.EventToolExecuted, run.ID)
	assertEventSeen(t, bus.Snapshot(), eventbus.EventRunCompleted, run.ID)
}

func TestE2ECompactSegmentRetrieve(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newE2ESessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "We chose SQLite for audit logs.",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), newE2EEngine(sessions), model, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "e2e-retrieve", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	seedSessionTranscript(t, sessions, session.ID, []contextengine.Message{
		{Role: contextengine.RoleUser, Content: "We need a durable store for audit logs.", CreatedAt: time.Now().UTC().Add(-5 * time.Minute)},
		{Role: contextengine.RoleAssistant, Content: "DECISION: use SQLite for audit logs.", CreatedAt: time.Now().UTC().Add(-4 * time.Minute)},
		{Role: contextengine.RoleUser, Content: "TODO: add retrieval tests.", CreatedAt: time.Now().UTC().Add(-3 * time.Minute)},
		{Role: contextengine.RoleAssistant, Content: "I will compact this later.", CreatedAt: time.Now().UTC().Add(-2 * time.Minute)},
	})

	if _, err := component.CompactSession(ctx, session.ID, contextengine.CompactManual); err != nil {
		t.Fatalf("CompactSession() error = %v", err)
	}
	segments, err := sessions.RecentSegments(ctx, session.ID, 1, 1)
	if err != nil {
		t.Fatalf("RecentSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "e2e-retrieve",
		ExternalEventID: "evt-e2e-retrieve",
		Content:         "What database did we choose for the audit logs?",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "<recalled_context") {
		t.Fatalf("system prompt missing recalled context: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(strings.ToLower(model.lastRequest.SystemPrompt), "sqlite") {
		t.Fatalf("system prompt missing sqlite recall: %s", model.lastRequest.SystemPrompt)
	}
}

func TestE2EToolFailureRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newE2ESessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "I could not read the file, so here is a partial answer.",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		QueueMode:     QueueEnqueue,
		MaxToolRounds: 3,
	}, sessions, runs, NewInMemoryCoordinator(), newE2EEngine(sessions), model, stubToolExecutor{
		err: errors.New("permission denied"),
	}, nil).WithEventBus(bus)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "e2e-tool-recovery",
		ExternalEventID: "evt-e2e-tool-recovery",
		Content:         "read the file and report back",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	session, err := sessions.GetByKey(ctx, "e2e-tool-recovery")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if len(session.Messages) < 4 {
		t.Fatalf("len(session.Messages) = %d, want at least 4", len(session.Messages))
	}
	failureResult, ok := resultmodel.DecodeToolResultMetadata(session.Messages[2].Metadata)
	if !ok {
		t.Fatalf("tool metadata missing: %#v", session.Messages[2].Metadata)
	}
	if failureResult.Error == nil || failureResult.Error.Message != "permission denied" {
		t.Fatalf("failureResult = %#v", failureResult)
	}
	if recovered, _ := failureResult.Structured["tool_execution_error"].(bool); !recovered {
		t.Fatalf("tool_execution_error = %#v", failureResult.Structured["tool_execution_error"])
	}
	foundRecovered := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventToolExecuted || event.RunID != run.ID {
			continue
		}
		if got, _ := event.Attrs["execution_error"].(string); got != "permission denied" {
			continue
		}
		if recovered, _ := event.Attrs["recovered"].(bool); recovered {
			foundRecovered = true
		}
	}
	if !foundRecovered {
		t.Fatal("expected recovered tool execution event")
	}
}

func TestE2EApprovalFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newE2ESessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "after approval",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		QueueMode:     QueueEnqueue,
		MaxToolRounds: 3,
	}, sessions, runs, NewInMemoryCoordinator(), newE2EEngine(sessions), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-1",
			Content:    "written",
		}},
	}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
	})).WithApprovals(approvals).WithEventBus(bus)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "e2e-approval",
		ExternalEventID: "evt-e2e-approval",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	seedLocalWriteSkill(t, sessions, run.SessionID, "fs.write")

	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want waiting_approval", run.Status)
	}
	assertEventSeen(t, bus.Snapshot(), eventbus.EventApprovalRequested, run.ID)
	assertEventSeen(t, bus.Snapshot(), eventbus.EventRunWaitingApproval, run.ID)

	ticket, err := approvals.GetByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.ResolveApproval(ctx, ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if err := component.ResumeRun(ctx, run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", run.Status)
	}
	assertEventSeen(t, bus.Snapshot(), eventbus.EventApprovalResolved, run.ID)
	assertEventSeen(t, bus.Snapshot(), eventbus.EventRunCompleted, run.ID)
}

func TestE2EEpisodeCrossDomainRetrieval(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := newE2ESessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "We compared the browser spec with the repo markdown file.",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), newE2EEngine(sessions), model, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "e2e-episode-cross-domain", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	seedSessionTranscript(t, sessions, session.ID, []contextengine.Message{
		{Role: contextengine.RoleUser, Content: "Open https://example.com/spec and inspect docs/plan.md", CreatedAt: time.Now().UTC().Add(-6 * time.Minute)},
		{Role: contextengine.RoleAssistant, Content: "DECISION: compare https://example.com/spec with docs/plan.md and keep the SQLite migration path.", CreatedAt: time.Now().UTC().Add(-5 * time.Minute)},
		{Role: contextengine.RoleAssistant, Content: "I'll revisit this in the next episode.", CreatedAt: time.Now().UTC().Add(-4 * time.Minute)},
	})

	if _, err := component.CompactSession(ctx, session.ID, contextengine.CompactManual); err != nil {
		t.Fatalf("CompactSession() error = %v", err)
	}
	firstEpisodeID, err := sessions.ActiveEpisode(ctx, session.ID)
	if err != nil {
		t.Fatalf("ActiveEpisode() error = %v", err)
	}
	if firstEpisodeID == "" {
		t.Fatal("expected active episode after compaction")
	}
	if _, err := sessions.StartNewEpisode(ctx, session.ID, "manual"); err != nil {
		t.Fatalf("StartNewEpisode() error = %v", err)
	}
	firstEpisodeSegments, err := sessions.SegmentsByEpisode(ctx, firstEpisodeID)
	if err != nil {
		t.Fatalf("SegmentsByEpisode() error = %v", err)
	}
	foundOverview := false
	for _, segment := range firstEpisodeSegments {
		if segment.Level == 3 {
			foundOverview = true
			break
		}
	}
	if !foundOverview {
		t.Fatal("expected l3 overview for sealed episode")
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "e2e-episode-cross-domain",
		ExternalEventID: "evt-e2e-episode-cross-domain",
		Content:         "What spec URL and markdown file did we compare in the previous episode?",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "<recalled_context") {
		t.Fatalf("system prompt missing recalled context: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "https://example.com/spec") {
		t.Fatalf("system prompt missing spec url: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "docs/plan.md") {
		t.Fatalf("system prompt missing file path: %s", model.lastRequest.SystemPrompt)
	}
}

type e2ESessionStore struct {
	*InMemorySessionStore
	segMu     sync.RWMutex
	nextSegID atomic.Uint64
	segments  []contextengine.SummarySegment
}

func newE2ESessionStore() *e2ESessionStore {
	return &e2ESessionStore{InMemorySessionStore: NewInMemorySessionStore()}
}

func (s *e2ESessionStore) EnsureActiveEpisode(ctx context.Context, sessionID string, reason string) (string, error) {
	current, err := s.InMemorySessionStore.ActiveEpisode(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != "" {
		return current, nil
	}
	return s.CreateEpisode(ctx, sessionID, reason)
}

func (s *e2ESessionStore) CreateEpisode(_ context.Context, sessionID string, reason string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[sessionID]; !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	if current := strings.TrimSpace(s.activeEpisode[sessionID]); current != "" {
		return "", fmt.Errorf("session %s already has active episode %s", sessionID, current)
	}
	now := time.Now().UTC()
	episodeID := fmt.Sprintf("ep-e2e-%06d", s.nextEpisodeID.Add(1))
	s.episodes[sessionID] = append(s.episodes[sessionID], contextengine.EpisodeSummary{
		ID:        episodeID,
		SessionID: sessionID,
		SeqNum:    len(s.episodes[sessionID]) + 1,
		Status:    "active",
		StartedAt: now,
	})
	s.activeEpisode[sessionID] = episodeID
	_ = reason
	return episodeID, nil
}

func (s *e2ESessionStore) StartNewEpisode(ctx context.Context, sessionID string, reason string) (string, error) {
	current, err := s.InMemorySessionStore.ActiveEpisode(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != "" {
		session, err := s.InMemorySessionStore.Get(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if err := s.SealEpisode(ctx, current, int64(len(session.Messages))); err != nil {
			return "", err
		}
	}
	return s.CreateEpisode(ctx, sessionID, reason)
}

func (s *e2ESessionStore) SealEpisode(ctx context.Context, episodeID string, seqEnd int64) error {
	if err := s.InMemorySessionStore.SealEpisode(ctx, episodeID, seqEnd); err != nil {
		return err
	}
	return contextengine.GenerateL3EpisodeOverview(ctx, s, s, episodeID)
}

func (s *e2ESessionStore) InsertSegment(_ context.Context, seg contextengine.SummarySegment) error {
	if strings.TrimSpace(seg.ID) == "" {
		seg.ID = fmt.Sprintf("seg-%06d", s.nextSegID.Add(1))
	}
	if seg.CreatedAt.IsZero() {
		seg.CreatedAt = time.Now().UTC()
	}
	if seg.TSStart.IsZero() {
		seg.TSStart = seg.CreatedAt
	}
	if seg.TSEnd.IsZero() {
		seg.TSEnd = seg.TSStart
	}

	s.segMu.Lock()
	defer s.segMu.Unlock()
	for idx := range s.segments {
		if s.segments[idx].ID != seg.ID {
			continue
		}
		s.segments[idx] = cloneSummarySegment(seg)
		return nil
	}
	s.segments = append(s.segments, cloneSummarySegment(seg))
	if seg.Level == 1 {
		s.mu.Lock()
		for sessionID, episodes := range s.episodes {
			for idx := range episodes {
				if episodes[idx].ID != seg.EpisodeID {
					continue
				}
				episodes[idx].MessageCount += int(seg.SeqEnd - seg.SeqStart + 1)
				s.episodes[sessionID] = episodes
				break
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *e2ESessionStore) UpdateParentSegmentID(_ context.Context, segmentID string, parentSegmentID string) error {
	s.segMu.Lock()
	defer s.segMu.Unlock()
	for idx := range s.segments {
		if s.segments[idx].ID != segmentID {
			continue
		}
		s.segments[idx].ParentSegmentID = strings.TrimSpace(parentSegmentID)
		return nil
	}
	return fmt.Errorf("segment %s not found", segmentID)
}

func (s *e2ESessionStore) RecentSegments(_ context.Context, sessionID string, level int, limit int) ([]contextengine.SummarySegment, error) {
	s.segMu.RLock()
	defer s.segMu.RUnlock()

	filtered := make([]contextengine.SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.SessionID == sessionID && seg.Level == level {
			filtered = append(filtered, cloneSummarySegment(seg))
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SeqStart != filtered[j].SeqStart {
			return filtered[i].SeqStart > filtered[j].SeqStart
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *e2ESessionStore) SegmentsByEpisode(_ context.Context, episodeID string) ([]contextengine.SummarySegment, error) {
	s.segMu.RLock()
	defer s.segMu.RUnlock()

	filtered := make([]contextengine.SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.EpisodeID == episodeID {
			filtered = append(filtered, cloneSummarySegment(seg))
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Level != filtered[j].Level {
			return filtered[i].Level < filtered[j].Level
		}
		if filtered[i].SeqStart != filtered[j].SeqStart {
			return filtered[i].SeqStart < filtered[j].SeqStart
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func (s *e2ESessionStore) UnparentedL1Segments(_ context.Context, episodeID string, limit int) ([]contextengine.SummarySegment, error) {
	s.segMu.RLock()
	defer s.segMu.RUnlock()

	filtered := make([]contextengine.SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.EpisodeID == episodeID && seg.Level == 1 && strings.TrimSpace(seg.ParentSegmentID) == "" {
			filtered = append(filtered, cloneSummarySegment(seg))
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SeqStart != filtered[j].SeqStart {
			return filtered[i].SeqStart < filtered[j].SeqStart
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *e2ESessionStore) SearchSegments(_ context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]contextengine.SummarySegment, error) {
	s.segMu.RLock()
	defer s.segMu.RUnlock()

	if limit <= 0 {
		limit = 1
	}

	candidates := make([]contextengine.SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.SessionID == sessionID {
			candidates = append(candidates, cloneSummarySegment(seg))
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	type scoredSegment struct {
		segment contextengine.SummarySegment
		score   float64
	}

	scored := make([]scoredSegment, 0, len(candidates))
	if len(queryEmbedding) > 0 {
		for _, candidate := range candidates {
			if len(candidate.Embedding) == 0 {
				continue
			}
			score := cosineEmbedding(candidate.Embedding, queryEmbedding)
			if score <= 0 {
				continue
			}
			scored = append(scored, scoredSegment{segment: candidate, score: score})
		}
	}
	if len(scored) == 0 {
		for _, candidate := range candidates {
			score := keywordScore(candidate, queryText)
			if score <= 0 {
				continue
			}
			scored = append(scored, scoredSegment{segment: candidate, score: float64(score)})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].segment.Level != scored[j].segment.Level {
			return scored[i].segment.Level > scored[j].segment.Level
		}
		return scored[i].segment.SeqEnd > scored[j].segment.SeqEnd
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]contextengine.SummarySegment, 0, len(scored))
	for _, item := range scored {
		results = append(results, item.segment)
	}
	return results, nil
}

type e2EEmbeddingClient struct{}

func (e2EEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vectors = append(vectors, embedText(text))
	}
	return vectors, nil
}

func newE2EEngine(store *e2ESessionStore) contextengine.ContextEngine {
	return contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a test agent.",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 1024,
		DefaultOutputTokens:  128,
		CompactKeepLastN:     1,
		CompactSummaryChars:  400,
		SegmentWriter:        store,
		SegmentReader:        store,
		SegmentSearcher:      store,
		EmbeddingClient:      e2EEmbeddingClient{},
	}, nil)
}

func seedSessionTranscript(t *testing.T, sessions *e2ESessionStore, sessionID string, messages []contextengine.Message) {
	t.Helper()

	session, release, err := sessions.LoadForExecution(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	defer release()

	session.Messages = append([]contextengine.Message(nil), messages...)
	session.LoadedMessageSeqs = make([]int64, len(messages))
	for idx := range messages {
		session.LoadedMessageSeqs[idx] = int64(idx + 1)
	}
	session.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

func seedLocalWriteSkill(t *testing.T, sessions *e2ESessionStore, sessionID string, toolName string) {
	t.Helper()

	session, release, err := sessions.LoadForExecution(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	defer release()

	bound := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "writer"},
			Trust:  skill.TrustCommunity,
			ToolManifests: []skill.ToolManifest{{
				Name:            toolName,
				SideEffectClass: "local_write",
			}},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	session.SkillSnapshot = skill.SessionSkillSnapshot{
		Fingerprint: "skills-e2e",
		Skills:      map[string]skill.BoundSkill{"writer": bound},
		Ordered:     []skill.BoundSkill{bound},
	}
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

func assertEventSeen(t *testing.T, events []eventbus.Event, eventType eventbus.EventType, runID string) {
	t.Helper()

	for _, event := range events {
		if event.Type == eventType && event.RunID == runID {
			return
		}
	}
	t.Fatalf("missing event %q for run %q in %#v", eventType, runID, events)
}

func cloneSummarySegment(in contextengine.SummarySegment) contextengine.SummarySegment {
	out := in
	out.Decisions = append([]string(nil), in.Decisions...)
	out.TODOs = append([]string(nil), in.TODOs...)
	out.Constraints = append([]string(nil), in.Constraints...)
	out.Entities = append([]string(nil), in.Entities...)
	out.ArtifactRefs = append([]string(nil), in.ArtifactRefs...)
	out.Embedding = append([]float32(nil), in.Embedding...)
	return out
}

func embedText(text string) []float32 {
	const dims = 8
	vector := make([]float32, dims)
	for _, token := range tokenizeText(text) {
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte(token))
		vector[hasher.Sum32()%dims]++
	}
	return vector
}

func cosineEmbedding(left, right []float32) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for idx := range left {
		dot += float64(left[idx] * right[idx])
		leftNorm += float64(left[idx] * left[idx])
		rightNorm += float64(right[idx] * right[idx])
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func keywordScore(segment contextengine.SummarySegment, query string) int {
	score := 0
	haystack := strings.ToLower(strings.TrimSpace(segment.Keywords + " " + segment.SummaryText))
	for _, token := range tokenizeText(query) {
		if len(token) < 3 {
			continue
		}
		if strings.Contains(haystack, token) {
			score++
		}
	}
	return score
}

func tokenizeText(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		switch r {
		case '/', '.', ':', '-', '_':
			return false
		default:
			return true
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.Trim(field, "./:-_")
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
