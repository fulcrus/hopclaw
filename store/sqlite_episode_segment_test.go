package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestSQLiteSessionStoreEpisodeLifecycleAndSegmentQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-crud", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	episodeID, err := sessions.CreateEpisode(ctx, session.ID, "default")
	if err != nil {
		t.Fatalf("CreateEpisode() error = %v", err)
	}
	activeEpisode, err := sessions.ActiveEpisode(ctx, session.ID)
	if err != nil {
		t.Fatalf("ActiveEpisode() error = %v", err)
	}
	if activeEpisode != episodeID {
		t.Fatalf("active episode = %q, want %q", activeEpisode, episodeID)
	}

	segment := contextengine.SummarySegment{
		SessionID:    session.ID,
		EpisodeID:    episodeID,
		Level:        1,
		SeqStart:     3,
		SeqEnd:       7,
		TSStart:      time.Now().UTC().Add(-5 * time.Minute),
		TSEnd:        time.Now().UTC().Add(-2 * time.Minute),
		SummaryText:  "Compacted immutable summary.",
		Decisions:    []string{"DECISION: keep immutable segments."},
		TODOs:        []string{"TODO: add coverage."},
		Constraints:  []string{"MUST preserve ordering."},
		Entities:     []string{"https://example.com/spec", "/tmp/summary.log"},
		ArtifactRefs: []string{"https://example.com/spec", "/tmp/summary.log"},
		Keywords:     "compact immutable summary",
		QualityScore: 0.9,
		CreatedAt:    time.Now().UTC(),
	}
	if err := sessions.InsertSegment(ctx, segment); err != nil {
		t.Fatalf("InsertSegment() error = %v", err)
	}

	recent, err := sessions.RecentSegments(ctx, session.ID, 1, 1)
	if err != nil {
		t.Fatalf("RecentSegments() error = %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent segment count = %d, want 1", len(recent))
	}
	if recent[0].SummaryText != segment.SummaryText {
		t.Fatalf("recent summary = %q, want %q", recent[0].SummaryText, segment.SummaryText)
	}

	byEpisode, err := sessions.SegmentsByEpisode(ctx, episodeID)
	if err != nil {
		t.Fatalf("SegmentsByEpisode() error = %v", err)
	}
	if len(byEpisode) != 1 {
		t.Fatalf("episode segment count = %d, want 1", len(byEpisode))
	}
	if len(byEpisode[0].Decisions) != 1 || len(byEpisode[0].ArtifactRefs) != 2 {
		t.Fatalf("loaded segment = %#v", byEpisode[0])
	}

	episodes, err := sessions.ListEpisodes(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("episode count = %d, want 1", len(episodes))
	}
	if episodes[0].MessageCount != 5 {
		t.Fatalf("episode message_count = %d, want 5", episodes[0].MessageCount)
	}

	var msgSeqStart, msgSeqEnd int64
	if err := db.QueryRow(`SELECT msg_seq_start, msg_seq_end FROM session_episodes WHERE id = ?`, episodeID).Scan(&msgSeqStart, &msgSeqEnd); err != nil {
		t.Fatalf("query episode coverage: %v", err)
	}
	if msgSeqStart != 3 || msgSeqEnd != 7 {
		t.Fatalf("episode coverage = [%d,%d], want [3,7]", msgSeqStart, msgSeqEnd)
	}

	if err := sessions.SealEpisode(ctx, episodeID, 7); err != nil {
		t.Fatalf("SealEpisode() error = %v", err)
	}
	activeEpisode, err = sessions.ActiveEpisode(ctx, session.ID)
	if err != nil {
		t.Fatalf("ActiveEpisode() after seal error = %v", err)
	}
	if activeEpisode != "" {
		t.Fatalf("active episode after seal = %q, want empty", activeEpisode)
	}
	episodes, err = sessions.ListEpisodes(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListEpisodes() after seal error = %v", err)
	}
	if episodes[0].Status != "sealed" || episodes[0].SealedAt.IsZero() {
		t.Fatalf("sealed episode = %#v", episodes[0])
	}
}

func TestSQLiteSegmentChainTraceableAcrossCompactions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-chain", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	seedSessionMessages(t, db, session.ID, 1, 8)

	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		CompactKeepLastN:    2,
		CompactSummaryChars: 400,
		SegmentWriter:       sessions,
		SegmentReader:       sessions,
	}, nil)

	snapshot, release, err := sessions.LoadExecutionSnapshot(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadExecutionSnapshot() error = %v", err)
	}
	if err := engine.Compact(ctx, &snapshot.Session.Session, contextengine.CompactManual); err != nil {
		release()
		t.Fatalf("first Compact() error = %v", err)
	}
	if err := sessions.Save(ctx, snapshot.Session); err != nil {
		release()
		t.Fatalf("Save() after first compact error = %v", err)
	}
	release()

	seedSessionMessages(t, db, session.ID, 9, 4)

	snapshot, release, err = sessions.LoadExecutionSnapshot(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadExecutionSnapshot() second error = %v", err)
	}
	if err := engine.Compact(ctx, &snapshot.Session.Session, contextengine.CompactManual); err != nil {
		release()
		t.Fatalf("second Compact() error = %v", err)
	}
	if err := sessions.Save(ctx, snapshot.Session); err != nil {
		release()
		t.Fatalf("Save() after second compact error = %v", err)
	}
	release()

	episodes, err := sessions.ListEpisodes(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("episode count = %d, want 1 default episode", len(episodes))
	}
	if episodes[0].MessageCount != 10 {
		t.Fatalf("episode message_count = %d, want 10", episodes[0].MessageCount)
	}

	episodeID := episodes[0].ID
	segments, err := sessions.SegmentsByEpisode(ctx, episodeID)
	if err != nil {
		t.Fatalf("SegmentsByEpisode() error = %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("segment count = %d, want 2", len(segments))
	}
	if segments[0].SeqStart != 1 || segments[0].SeqEnd != 6 {
		t.Fatalf("first segment range = [%d,%d], want [1,6]", segments[0].SeqStart, segments[0].SeqEnd)
	}
	if segments[1].SeqStart != 7 || segments[1].SeqEnd != 10 {
		t.Fatalf("second segment range = [%d,%d], want [7,10]", segments[1].SeqStart, segments[1].SeqEnd)
	}

	recent, err := sessions.RecentSegments(ctx, session.ID, 1, 1)
	if err != nil {
		t.Fatalf("RecentSegments() error = %v", err)
	}
	if len(recent) != 1 || recent[0].ID != segments[1].ID {
		t.Fatalf("recent segment = %#v, want latest %#v", recent, segments[1])
	}

	var msgSeqStart, msgSeqEnd int64
	if err := db.QueryRow(`SELECT msg_seq_start, msg_seq_end FROM session_episodes WHERE id = ?`, episodeID).Scan(&msgSeqStart, &msgSeqEnd); err != nil {
		t.Fatalf("query episode coverage: %v", err)
	}
	if msgSeqStart != 1 || msgSeqEnd != 10 {
		t.Fatalf("episode coverage = [%d,%d], want [1,10]", msgSeqStart, msgSeqEnd)
	}
}

func TestSQLiteSessionStoreStartNewEpisodeRotatesActiveEpisode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-rotate", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if _, err := sessions.EnsureActiveEpisode(ctx, session.ID, "default"); err != nil {
		t.Fatalf("EnsureActiveEpisode() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, session.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	newEpisodeID, err := sessions.StartNewEpisode(ctx, session.ID, "manual")
	if err != nil {
		t.Fatalf("StartNewEpisode() error = %v", err)
	}

	activeEpisode, err := sessions.ActiveEpisode(ctx, session.ID)
	if err != nil {
		t.Fatalf("ActiveEpisode() error = %v", err)
	}
	if activeEpisode != newEpisodeID {
		t.Fatalf("active episode = %q, want %q", activeEpisode, newEpisodeID)
	}

	episodes, err := sessions.ListEpisodes(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episode count = %d, want 2", len(episodes))
	}
	if episodes[0].Status != "sealed" || episodes[0].SealedAt.IsZero() {
		t.Fatalf("first episode = %#v, want sealed", episodes[0])
	}
	if episodes[1].ID != newEpisodeID || episodes[1].Status != "active" {
		t.Fatalf("second episode = %#v, want active %q", episodes[1], newEpisodeID)
	}
}

func TestSQLiteSessionStoreSearchSegmentsByCosine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-search-cosine", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	episodeID, err := sessions.CreateEpisode(ctx, session.ID, "default")
	if err != nil {
		t.Fatalf("CreateEpisode() error = %v", err)
	}

	now := time.Now().UTC()
	segments := []contextengine.SummarySegment{
		{ID: "seg-east", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 1, SeqEnd: 1, TSStart: now, TSEnd: now, SummaryText: "East", Embedding: []float32{1, 0}, CreatedAt: now, Keywords: "east"},
		{ID: "seg-northeast", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 2, SeqEnd: 2, TSStart: now, TSEnd: now, SummaryText: "NorthEast", Embedding: []float32{0.9, 0.1}, CreatedAt: now.Add(time.Second), Keywords: "northeast"},
		{ID: "seg-north", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 3, SeqEnd: 3, TSStart: now, TSEnd: now, SummaryText: "North", Embedding: []float32{0, 1}, CreatedAt: now.Add(2 * time.Second), Keywords: "north"},
	}
	for _, segment := range segments {
		if err := sessions.InsertSegment(ctx, segment); err != nil {
			t.Fatalf("InsertSegment(%s) error = %v", segment.ID, err)
		}
	}

	results, err := sessions.SearchSegments(ctx, session.ID, "", []float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("SearchSegments() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].ID != "seg-east" || results[1].ID != "seg-northeast" {
		t.Fatalf("results = %#v, want east then northeast", results)
	}
}

func TestSQLiteSessionStoreSearchSegmentsKeywordFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-search-keywords", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	episodeID, err := sessions.CreateEpisode(ctx, session.ID, "default")
	if err != nil {
		t.Fatalf("CreateEpisode() error = %v", err)
	}

	now := time.Now().UTC()
	segments := []contextengine.SummarySegment{
		{ID: "seg-best", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 1, SeqEnd: 1, TSStart: now, TSEnd: now, SummaryText: "SQLite audit notes", Keywords: "sqlite audit durable", CreatedAt: now},
		{ID: "seg-second", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 2, SeqEnd: 2, TSStart: now, TSEnd: now, SummaryText: "Audit note", Keywords: "audit logs", CreatedAt: now.Add(time.Second)},
		{ID: "seg-miss", SessionID: session.ID, EpisodeID: episodeID, Level: 1, SeqStart: 3, SeqEnd: 3, TSStart: now, TSEnd: now, SummaryText: "Other", Keywords: "network browser", CreatedAt: now.Add(2 * time.Second)},
	}
	for _, segment := range segments {
		if err := sessions.InsertSegment(ctx, segment); err != nil {
			t.Fatalf("InsertSegment(%s) error = %v", segment.ID, err)
		}
	}

	results, err := sessions.SearchSegments(ctx, session.ID, "sqlite audit", nil, 2)
	if err != nil {
		t.Fatalf("SearchSegments() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].ID != "seg-best" || results[1].ID != "seg-second" {
		t.Fatalf("results = %#v, want seg-best then seg-second", results)
	}
}

func TestSQLiteSessionStoreUnparentedL1SegmentsAndParenting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-unparented", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	episodeID, err := sessions.CreateEpisode(ctx, session.ID, "default")
	if err != nil {
		t.Fatalf("CreateEpisode() error = %v", err)
	}

	now := time.Now().UTC()
	for idx := 0; idx < 3; idx++ {
		if err := sessions.InsertSegment(ctx, contextengine.SummarySegment{
			ID:          "seg-unparented-" + string(rune('a'+idx)),
			SessionID:   session.ID,
			EpisodeID:   episodeID,
			Level:       1,
			SeqStart:    int64(idx + 1),
			SeqEnd:      int64(idx + 1),
			TSStart:     now,
			TSEnd:       now,
			SummaryText: "L1 summary",
			CreatedAt:   now.Add(time.Duration(idx) * time.Second),
			Keywords:    "summary",
		}); err != nil {
			t.Fatalf("InsertSegment() error = %v", err)
		}
	}

	if err := sessions.UpdateParentSegmentID(ctx, "seg-unparented-a", "seg-l2-parent"); err != nil {
		t.Fatalf("UpdateParentSegmentID() error = %v", err)
	}

	unparented, err := sessions.UnparentedL1Segments(ctx, episodeID, 10)
	if err != nil {
		t.Fatalf("UnparentedL1Segments() error = %v", err)
	}
	if len(unparented) != 2 {
		t.Fatalf("len(unparented) = %d, want 2", len(unparented))
	}
	if unparented[0].ID != "seg-unparented-b" || unparented[1].ID != "seg-unparented-c" {
		t.Fatalf("unparented = %#v", unparented)
	}
}

func TestSQLiteSessionStoreSealEpisodeGeneratesL3Overview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "episodes-l3", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	episodeID, err := sessions.CreateEpisode(ctx, session.ID, "default")
	if err != nil {
		t.Fatalf("CreateEpisode() error = %v", err)
	}

	now := time.Now().UTC()
	for idx := 0; idx < 8; idx++ {
		if err := sessions.InsertSegment(ctx, contextengine.SummarySegment{
			ID:          fmt.Sprintf("seg-l1-%d", idx+1),
			SessionID:   session.ID,
			EpisodeID:   episodeID,
			Level:       1,
			SeqStart:    int64(idx*2 + 1),
			SeqEnd:      int64(idx*2 + 2),
			TSStart:     now.Add(time.Duration(idx) * time.Minute),
			TSEnd:       now.Add(time.Duration(idx+1) * time.Minute),
			SummaryText: fmt.Sprintf("L1 summary %d", idx+1),
			Decisions:   []string{fmt.Sprintf("Decision %d", idx+1)},
			CreatedAt:   now.Add(time.Duration(idx) * time.Second),
			Keywords:    "sqlite overview",
		}); err != nil {
			t.Fatalf("InsertSegment() error = %v", err)
		}
	}

	if err := contextengine.MaybeGenerateL2(ctx, sessions, sessions, episodeID); err != nil {
		t.Fatalf("MaybeGenerateL2() error = %v", err)
	}
	if err := sessions.SealEpisode(ctx, episodeID, 16); err != nil {
		t.Fatalf("SealEpisode() error = %v", err)
	}

	segments, err := sessions.SegmentsByEpisode(ctx, episodeID)
	if err != nil {
		t.Fatalf("SegmentsByEpisode() error = %v", err)
	}

	var level2Count, level3Count int
	for _, segment := range segments {
		switch segment.Level {
		case 2:
			level2Count++
		case 3:
			level3Count++
			if segment.SummaryText == "" {
				t.Fatal("expected l3 summary text")
			}
		}
	}
	if level2Count != 1 {
		t.Fatalf("level2Count = %d, want 1", level2Count)
	}
	if level3Count != 1 {
		t.Fatalf("level3Count = %d, want 1", level3Count)
	}
}
