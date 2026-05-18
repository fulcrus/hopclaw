package contextengine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

type stubSegmentStore struct {
	activeEpisode map[string]string
	episodes      map[string][]EpisodeSummary
	segments      []SummarySegment
	searchFn      func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error)
	searchCalls   atomic.Int64
	unparentedErr error
	updateErr     error
}

func newStubSegmentStore() *stubSegmentStore {
	return &stubSegmentStore{
		activeEpisode: make(map[string]string),
		episodes:      make(map[string][]EpisodeSummary),
	}
}

func (s *stubSegmentStore) CreateEpisode(_ context.Context, sessionID string, _ string) (string, error) {
	episodes := s.episodes[sessionID]
	id := fmt.Sprintf("ep-%d", len(episodes)+1)
	s.episodes[sessionID] = append(s.episodes[sessionID], EpisodeSummary{
		ID:        id,
		SessionID: sessionID,
		SeqNum:    len(episodes) + 1,
		Status:    "active",
		StartedAt: time.Now().UTC(),
	})
	s.activeEpisode[sessionID] = id
	return id, nil
}

func (s *stubSegmentStore) SealEpisode(_ context.Context, episodeID string, _ int64) error {
	for sessionID, episodes := range s.episodes {
		for idx := range episodes {
			if episodes[idx].ID != episodeID {
				continue
			}
			episodes[idx].Status = "sealed"
			episodes[idx].SealedAt = time.Now().UTC()
			s.episodes[sessionID] = episodes
			if s.activeEpisode[sessionID] == episodeID {
				delete(s.activeEpisode, sessionID)
			}
			return nil
		}
	}
	return nil
}

func (s *stubSegmentStore) ActiveEpisode(_ context.Context, sessionID string) (string, error) {
	return s.activeEpisode[sessionID], nil
}

func (s *stubSegmentStore) ListEpisodes(_ context.Context, sessionID string) ([]EpisodeSummary, error) {
	return append([]EpisodeSummary(nil), s.episodes[sessionID]...), nil
}

func (s *stubSegmentStore) InsertSegment(_ context.Context, seg SummarySegment) error {
	if seg.ID == "" {
		seg.ID = fmt.Sprintf("seg-%d", len(s.segments)+1)
	}
	s.segments = append(s.segments, seg)
	episodes := s.episodes[seg.SessionID]
	for idx := range episodes {
		if episodes[idx].ID != seg.EpisodeID {
			continue
		}
		episodes[idx].MessageCount += int(seg.SeqEnd - seg.SeqStart + 1)
		break
	}
	s.episodes[seg.SessionID] = episodes
	return nil
}

func (s *stubSegmentStore) RecentSegments(_ context.Context, sessionID string, level int, limit int) ([]SummarySegment, error) {
	filtered := make([]SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.SessionID == sessionID && seg.Level == level {
			filtered = append(filtered, seg)
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

func (s *stubSegmentStore) SegmentsByEpisode(_ context.Context, episodeID string) ([]SummarySegment, error) {
	filtered := make([]SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.EpisodeID == episodeID {
			filtered = append(filtered, seg)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SeqStart != filtered[j].SeqStart {
			return filtered[i].SeqStart < filtered[j].SeqStart
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func (s *stubSegmentStore) UnparentedL1Segments(_ context.Context, episodeID string, limit int) ([]SummarySegment, error) {
	if s.unparentedErr != nil {
		return nil, s.unparentedErr
	}
	filtered := make([]SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.EpisodeID == episodeID && seg.Level == 1 && strings.TrimSpace(seg.ParentSegmentID) == "" {
			filtered = append(filtered, seg)
		}
	}
	sortSegmentsForRollup(filtered)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *stubSegmentStore) UpdateParentSegmentID(_ context.Context, segmentID string, parentSegmentID string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	for idx := range s.segments {
		if s.segments[idx].ID != segmentID {
			continue
		}
		s.segments[idx].ParentSegmentID = parentSegmentID
		return nil
	}
	return fmt.Errorf("segment %s not found", segmentID)
}

func (s *stubSegmentStore) SearchSegments(_ context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
	s.searchCalls.Add(1)
	if s.searchFn != nil {
		return s.searchFn(sessionID, queryText, queryEmbedding, limit)
	}
	filtered := make([]SummarySegment, 0, len(s.segments))
	for _, seg := range s.segments {
		if seg.SessionID == sessionID {
			filtered = append(filtered, seg)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

type stubEmbeddingClient struct {
	embedFn func(texts []string) ([][]float32, error)
}

func (c *stubEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if c.embedFn != nil {
		return c.embedFn(texts)
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0, 0}
	}
	return vectors, nil
}

func TestCompactWritesL1Segment(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 300,
		SegmentWriter:       store,
	}, nil)

	session := &Session{
		ID:                "sess-compact",
		LoadedMessageSeqs: []int64{1, 2, 3},
		Messages: []Message{
			{
				Role:      RoleUser,
				Content:   "Structured segment annotations captured.",
				CreatedAt: time.Now().UTC().Add(-3 * time.Minute),
				Metadata: map[string]any{
					MetadataKeySignalDecisions:   []string{"Use SQLite."},
					MetadataKeySignalTODOs:       []string{"Add regression tests."},
					MetadataKeySignalConstraints: []string{"Preserve audit logs."},
				},
			},
			{
				Role:      RoleTool,
				Content:   "Saved notes at /tmp/compact.log and https://example.com/spec",
				CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
			},
			{
				Role:      RoleAssistant,
				Content:   "Newest reply stays hot.",
				CreatedAt: time.Now().UTC().Add(-time.Minute),
			},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(store.segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(store.segments))
	}
	segment := store.segments[0]
	if segment.Level != 1 {
		t.Fatalf("segment level = %d, want 1", segment.Level)
	}
	if segment.SeqStart != 1 || segment.SeqEnd != 2 {
		t.Fatalf("segment seq range = [%d,%d], want [1,2]", segment.SeqStart, segment.SeqEnd)
	}
	if segment.EpisodeID == "" {
		t.Fatal("expected compact to auto-create an active episode")
	}
	if len(segment.Decisions) == 0 || len(segment.TODOs) == 0 || len(segment.Constraints) == 0 {
		t.Fatalf("structured extraction missing: %#v", segment)
	}
	if len(segment.Entities) == 0 || len(segment.ArtifactRefs) == 0 {
		t.Fatalf("expected identifiers and artifact refs, got %#v", segment)
	}
	if session.Summary == "" {
		t.Fatal("expected fallback session summary to remain populated")
	}
}

func TestPreparePrefersRecentSegmentSummary(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.segments = append(store.segments, SummarySegment{
		ID:          "seg-1",
		SessionID:   "sess-prepare",
		EpisodeID:   "ep-1",
		Level:       1,
		SeqStart:    5,
		SeqEnd:      8,
		SummaryText: "Recent immutable segment summary.",
		CreatedAt:   time.Now().UTC(),
	})

	engine := NewSlidingWindowEngine(Config{
		SegmentReader: store,
	}, nil)

	session := &Session{
		ID:      "sess-prepare",
		Summary: "Legacy recursive summary.",
		Messages: []Message{
			{Role: RoleUser, Content: "What's the latest state?"},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(prepared.Messages) == 0 {
		t.Fatal("expected summary message to be injected")
	}
	if prepared.Messages[0].Name != "session-summary" {
		t.Fatalf("summary message = %#v", prepared.Messages[0])
	}
	if prepared.Messages[0].Content != "Recent immutable segment summary." {
		t.Fatalf("summary content = %q, want recent segment summary", prepared.Messages[0].Content)
	}
}

func TestMaybeGenerateL2GroupsEightUnparentedL1Segments(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.episodes["sess-rollup"] = []EpisodeSummary{{
		ID:        "ep-rollup",
		SessionID: "sess-rollup",
		SeqNum:    1,
		Status:    "active",
		StartedAt: time.Now().UTC(),
	}}

	now := time.Now().UTC()
	for idx := 0; idx < 8; idx++ {
		store.segments = append(store.segments, SummarySegment{
			ID:          fmt.Sprintf("seg-l1-%d", idx+1),
			SessionID:   "sess-rollup",
			EpisodeID:   "ep-rollup",
			Level:       1,
			SeqStart:    int64(idx*2 + 1),
			SeqEnd:      int64(idx*2 + 2),
			TSStart:     now.Add(time.Duration(idx) * time.Minute),
			TSEnd:       now.Add(time.Duration(idx+1) * time.Minute),
			SummaryText: fmt.Sprintf("Summary %d", idx+1),
			Decisions:   []string{fmt.Sprintf("Decision %d", idx+1)},
			CreatedAt:   now.Add(time.Duration(idx) * time.Second),
		})
	}

	if err := MaybeGenerateL2(context.Background(), store, store, "ep-rollup"); err != nil {
		t.Fatalf("MaybeGenerateL2() error = %v", err)
	}

	if len(store.segments) != 9 {
		t.Fatalf("segment count = %d, want 9", len(store.segments))
	}
	rollup := store.segments[len(store.segments)-1]
	if rollup.Level != 2 {
		t.Fatalf("roll-up level = %d, want 2", rollup.Level)
	}
	if rollup.SeqStart != 1 || rollup.SeqEnd != 16 {
		t.Fatalf("roll-up range = [%d,%d], want [1,16]", rollup.SeqStart, rollup.SeqEnd)
	}
	if rollup.SummaryText == "" {
		t.Fatal("expected l2 summary text")
	}

	for idx := 0; idx < 8; idx++ {
		if got := store.segments[idx].ParentSegmentID; got != rollup.ID {
			t.Fatalf("segment %d parent = %q, want %q", idx, got, rollup.ID)
		}
	}
}

func TestCompactRollupFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.unparentedErr = errors.New("boom")
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 200,
		SegmentWriter:       store,
		SegmentReader:       store,
	}, nil)

	session := &Session{
		ID:                "sess-rollup-nonfatal",
		LoadedMessageSeqs: []int64{1, 2, 3},
		Messages: []Message{
			{Role: RoleUser, Content: "Decision one", CreatedAt: time.Now().UTC().Add(-3 * time.Minute)},
			{Role: RoleAssistant, Content: "Decision two", CreatedAt: time.Now().UTC().Add(-2 * time.Minute)},
			{Role: RoleUser, Content: "Newest", CreatedAt: time.Now().UTC().Add(-time.Minute)},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(store.segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(store.segments))
	}
	if store.segments[0].Level != 1 {
		t.Fatalf("first segment level = %d, want 1", store.segments[0].Level)
	}
}

func TestCompactWritesSegmentEmbeddingWhenConfigured(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 300,
		SegmentWriter:       store,
		EmbeddingClient: &stubEmbeddingClient{
			embedFn: func(texts []string) ([][]float32, error) {
				if len(texts) != 1 {
					t.Fatalf("Embed() texts = %#v, want single summary", texts)
				}
				return [][]float32{{0.1, 0.2, 0.3}}, nil
			},
		},
	}, nil)

	session := &Session{
		ID:                "sess-embedding",
		LoadedMessageSeqs: []int64{1, 2, 3},
		Messages: []Message{
			{Role: RoleUser, Content: "Keep SQLite summaries durable.", CreatedAt: time.Now().UTC().Add(-2 * time.Minute)},
			{Role: RoleAssistant, Content: "Sure.", CreatedAt: time.Now().UTC().Add(-time.Minute)},
			{Role: RoleUser, Content: "Newest reply stays hot.", CreatedAt: time.Now().UTC()},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(store.segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(store.segments))
	}
	if got := store.segments[0].Embedding; !reflect.DeepEqual(got, []float32{0.1, 0.2, 0.3}) {
		t.Fatalf("Embedding = %#v, want %#v", got, []float32{0.1, 0.2, 0.3})
	}
}

func TestCompactContinuesWhenSegmentEmbeddingFails(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 300,
		SegmentWriter:       store,
		EmbeddingClient: &stubEmbeddingClient{
			embedFn: func(_ []string) ([][]float32, error) {
				return nil, errors.New("boom")
			},
		},
	}, nil)

	session := &Session{
		ID:                "sess-embedding-error",
		LoadedMessageSeqs: []int64{1, 2, 3},
		Messages: []Message{
			{Role: RoleUser, Content: "Keep SQLite summaries durable.", CreatedAt: time.Now().UTC().Add(-2 * time.Minute)},
			{Role: RoleAssistant, Content: "Sure.", CreatedAt: time.Now().UTC().Add(-time.Minute)},
			{Role: RoleUser, Content: "Newest reply stays hot.", CreatedAt: time.Now().UTC()},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(store.segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(store.segments))
	}
	if len(store.segments[0].Embedding) != 0 {
		t.Fatalf("Embedding = %#v, want empty on failure", store.segments[0].Embedding)
	}
}

func TestPrepareInjectsRecalledContext(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		if sessionID != "sess-recall" {
			t.Fatalf("sessionID = %q, want sess-recall", sessionID)
		}
		if queryText != "Need the historical SQLite decision" {
			t.Fatalf("queryText = %q", queryText)
		}
		if len(queryEmbedding) != 3 {
			t.Fatalf("queryEmbedding = %#v, want len 3", queryEmbedding)
		}
		if limit != 3 {
			t.Fatalf("limit = %d, want 3", limit)
		}
		return []SummarySegment{
			{
				ID:          "seg-1",
				SessionID:   sessionID,
				SummaryText: "We chose SQLite for the runtime store.",
				Decisions:   []string{"Use SQLite first."},
				TSStart:     time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:          "seg-2",
				SessionID:   sessionID,
				SummaryText: "Audit logs must remain append-only.",
				Constraints: []string{"Keep append-only semantics."},
				TSStart:     time.Date(2025, 11, 6, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 7, 0, 0, 0, 0, time.UTC),
			},
		}, nil
	}
	engine := NewSlidingWindowEngine(Config{
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
		EmbeddingClient: &stubEmbeddingClient{
			embedFn: func(texts []string) ([][]float32, error) {
				return [][]float32{{1, 0, 0}}, nil
			},
		},
		SegmentSearcher: store,
	}, nil)

	session := &Session{
		ID: "sess-recall",
		Messages: []Message{
			{Role: RoleUser, Content: "Need the historical SQLite decision"},
		},
	}
	prepared, _, err := engine.Prepare(context.Background(), session, &Run{
		MaxContextTokens: 4000,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, `<recalled_context source="segment seg-1 from 2025-11-02 to 2025-11-05">`) {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "We chose SQLite for the runtime store.") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "Audit logs must remain append-only.") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
}

func TestRecalledPromptGoalQueryPrefersRunGoalAndTarget(t *testing.T) {
	t.Parallel()

	query := recalledPromptGoalQuery(&Run{
		SystemPrompt:  "Do not use this for retrieval",
		Goal:          "Investigate the deployment history",
		TargetSummary: "staging web-api",
	})
	if strings.Contains(query, "Do not use this for retrieval") {
		t.Fatalf("query = %q, should not include system prompt", query)
	}
	if !strings.Contains(query, "Investigate the deployment history") {
		t.Fatalf("query = %q, want run goal", query)
	}
	if !strings.Contains(query, "target: staging web-api") {
		t.Fatalf("query = %q, want target summary", query)
	}
}

func TestPrepareSkipsRecallWithoutEmbeddingClient(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	engine := NewSlidingWindowEngine(Config{
		SegmentSearcher: store,
	}, nil)

	session := &Session{
		ID: "sess-no-embed",
		Messages: []Message{
			{Role: RoleUser, Content: "Need the historical SQLite decision"},
		},
	}
	prepared, _, err := engine.Prepare(context.Background(), session, &Run{
		MaxContextTokens: 1000,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if strings.Contains(prepared.SystemPrompt, "<recalled_context") {
		t.Fatalf("SystemPrompt = %q, want no recalled_context", prepared.SystemPrompt)
	}
	if store.searchCalls.Load() != 0 {
		t.Fatalf("searchCalls = %d, want 0", store.searchCalls.Load())
	}
}

func TestPrepareTrimsRecalledSegmentsToBudget(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		return []SummarySegment{
			{ID: "seg-1", SessionID: sessionID, SummaryText: "Primary segment stays.", TSStart: time.Now().UTC(), TSEnd: time.Now().UTC()},
			{ID: "seg-2", SessionID: sessionID, SummaryText: "Secondary segment stays.", TSStart: time.Now().UTC(), TSEnd: time.Now().UTC()},
			{ID: "seg-3", SessionID: sessionID, SummaryText: "Tertiary segment should be dropped.", TSStart: time.Now().UTC(), TSEnd: time.Now().UTC()},
		}, nil
	}
	engine := NewSlidingWindowEngine(Config{
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
		EmbeddingClient: &stubEmbeddingClient{
			embedFn: func(texts []string) ([][]float32, error) {
				return [][]float32{{1, 0, 0}}, nil
			},
		},
		SegmentSearcher: store,
	}, nil)

	session := &Session{
		ID: "sess-budget",
		Messages: []Message{
			{Role: RoleUser, Content: "Recall the plan"},
		},
	}
	prepared, _, err := engine.Prepare(context.Background(), session, &Run{
		MaxContextTokens: 2000,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "Primary segment stays.") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "Secondary segment stays.") {
		t.Fatalf("SystemPrompt = %q", prepared.SystemPrompt)
	}
	if strings.Contains(prepared.SystemPrompt, "Tertiary segment should be dropped.") {
		t.Fatalf("SystemPrompt = %q, want lowest-ranked recalled segment trimmed", prepared.SystemPrompt)
	}
}
