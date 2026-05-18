package store

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

func TestSQLiteSessionStateCRUD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)

	session, err := sessions.GetOrCreate(ctx, "state-crud", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	now := time.Now().UTC().Add(-time.Minute)
	if err := sessions.UpsertState(ctx, session.ID, []contextengine.StateEntry{
		{
			Key:           "decision:sqlite",
			Category:      "decision",
			Value:         "Use SQLite durable state.",
			Confidence:    0.9,
			CreatedAt:     now,
			UpdatedAt:     now,
			SourceEpisode: "ep-1",
		},
		{
			Key:        "todo:tests",
			Category:   "todo",
			Value:      "Add regression coverage.",
			Confidence: 0.7,
		},
		{
			Key:        "constraint:expired",
			Category:   "constraint",
			Value:      "This state should not be active.",
			ExpiresAt:  time.Now().UTC().Add(-time.Minute),
			Confidence: 0.4,
		},
	}); err != nil {
		t.Fatalf("UpsertState() insert error = %v", err)
	}

	if err := sessions.UpsertState(ctx, session.ID, []contextengine.StateEntry{
		{
			Key:           "decision:sqlite",
			Category:      "decision",
			Value:         "Use SQLite-backed durable state.",
			Status:        "active",
			Confidence:    0.95,
			UpdatedAt:     time.Now().UTC(),
			SourceEpisode: "ep-2",
			SourceSegment: "seg-2",
		},
		{
			Key:        "constraint:inactive",
			Category:   "constraint",
			Value:      "Ignore inactive entries.",
			Status:     "superseded",
			Confidence: 0.5,
		},
	}); err != nil {
		t.Fatalf("UpsertState() update error = %v", err)
	}

	active, err := sessions.ActiveStates(ctx, session.ID)
	if err != nil {
		t.Fatalf("ActiveStates() error = %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active state count = %d, want 2", len(active))
	}

	if active[0].Category != "decision" || active[0].Value != "Use SQLite-backed durable state." {
		t.Fatalf("decision state = %#v", active[0])
	}
	if active[0].CreatedAt.IsZero() || active[0].UpdatedAt.IsZero() {
		t.Fatalf("decision timestamps not parsed: %#v", active[0])
	}
	if active[0].SourceEpisode != "ep-2" || active[0].SourceSegment != "seg-2" {
		t.Fatalf("decision provenance = %#v", active[0])
	}

	if active[1].Category != "todo" || active[1].Value != "Add regression coverage." {
		t.Fatalf("todo state = %#v", active[1])
	}
}
