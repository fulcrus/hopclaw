package agent

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestRecencyDecay_Continuous(t *testing.T) {
	score29 := recencyDecay(29 * 24 * time.Hour)
	score30 := recencyDecay(30 * 24 * time.Hour)
	score31 := recencyDecay(31 * 24 * time.Hour)

	if !(score29 > score30 && score30 > score31) {
		t.Fatalf("expected monotonic decay around half-life, got 29d=%f 30d=%f 31d=%f", score29, score30, score31)
	}
	if gap := score29 - score31; gap >= 0.03 {
		t.Fatalf("expected smooth decay across 29d->31d, got gap=%f", gap)
	}
}

func TestRecencyDecay_HalfLifeAt30Days(t *testing.T) {
	score := recencyDecay(30 * 24 * time.Hour)
	if math.Abs(score-0.5) > 1e-9 {
		t.Fatalf("expected 30-day half-life to decay to 0.5, got %f", score)
	}
}

func TestRecencyDecay_ZeroDays(t *testing.T) {
	if score := recencyDecay(0); score != 1.0 {
		t.Fatalf("expected zero age to have full weight, got %f", score)
	}
}

func TestRecencyDecay_VeryOld(t *testing.T) {
	score := recencyDecay(365 * 24 * time.Hour)
	if score <= 0 {
		t.Fatalf("expected positive decay for old memories, got %f", score)
	}
	if score >= 0.001 {
		t.Fatalf("expected 365-day-old memory to decay near zero, got %f", score)
	}
}

func TestRecencyDecay_RecallWithQueryUsesSameFunction(t *testing.T) {
	age := 45 * 24 * time.Hour
	entry := MemoryEntry{LastUsedAt: time.Now().Add(-age)}

	got := recallRecencyScore(entry)
	want := recencyDecay(time.Since(entry.LastUsedAt))
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("expected RecallWithQuery recency to reuse recencyDecay, got %f want %f", got, want)
	}
}

func TestComputeScoreUserSource(t *testing.T) {
	entry := MemoryEntry{Source: MemorySourceUser}
	if s := ComputeScore(entry); s != 0 {
		t.Fatalf("expected 0 for user source, got %f", s)
	}
}

func TestComputeScoreAgentFresh(t *testing.T) {
	entry := MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 1,
		LastUsedAt:    time.Now(),
	}
	s := ComputeScore(entry)
	if s < 0.2 || s > 0.4 {
		t.Fatalf("expected score ~0.3 for fresh agent memory, got %f", s)
	}
}

func TestComputeScoreHighEvidence(t *testing.T) {
	entry := MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 5,
		LastUsedAt:    time.Now(),
		UsedCount:     10,
	}
	s := ComputeScore(entry)
	if s < 0.5 {
		t.Fatalf("expected score >= 0.5 for high evidence, got %f", s)
	}
}

func TestComputeScoreDecayed(t *testing.T) {
	entry := MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 3,
		LastUsedAt:    time.Now().Add(-100 * 24 * time.Hour), // 100 days ago
	}
	s := ComputeScore(entry)
	if s > 0.5 {
		t.Fatalf("expected decayed score < 0.5, got %f", s)
	}
}

func TestComputeScoreCorrected(t *testing.T) {
	entry := MemoryEntry{
		Source:          MemorySourceAgent,
		EvidenceCount:   3,
		LastUsedAt:      time.Now(),
		CorrectionCount: 2,
	}
	s := ComputeScore(entry)
	if s > 0.3 {
		t.Fatalf("expected low score for corrected memory, got %f", s)
	}
}

func TestComputeScoreNeverExceeds095(t *testing.T) {
	entry := MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 100,
		LastUsedAt:    time.Now(),
		UsedCount:     1000,
	}
	s := ComputeScore(entry)
	if s > 0.95 {
		t.Fatalf("score should never exceed 0.95, got %f", s)
	}
}

func TestComputeScore_VerificationBoost(t *testing.T) {
	baseEntry := MemoryEntry{
		Source:          MemorySourceAgent,
		EvidenceCount:   3,
		LastUsedAt:      time.Now(),
		CorrectionCount: 1,
	}

	baseScore := ComputeScore(baseEntry)
	passedOnce := baseEntry
	passedOnce.VerificationPassCount = 1
	passedThreeTimes := baseEntry
	passedThreeTimes.VerificationPassCount = 3

	onceScore := ComputeScore(passedOnce)
	threeScore := ComputeScore(passedThreeTimes)

	if onceScore <= baseScore {
		t.Fatalf("expected verification pass to boost score: base=%f once=%f", baseScore, onceScore)
	}
	if threeScore <= onceScore {
		t.Fatalf("expected 3 verification passes to outscore 1 pass: once=%f three=%f", onceScore, threeScore)
	}
}

func TestComputeScore_VerificationPenalty(t *testing.T) {
	baseEntry := MemoryEntry{
		Source:        MemorySourceAgent,
		EvidenceCount: 3,
		LastUsedAt:    time.Now(),
	}

	baseScore := ComputeScore(baseEntry)
	failedEntry := baseEntry
	failedEntry.VerificationPassCount = 3
	failedEntry.VerificationFailCount = 1

	failedScore := ComputeScore(failedEntry)
	if failedScore >= baseScore {
		t.Fatalf("expected verification failure to penalize score: base=%f failed=%f", baseScore, failedScore)
	}
}

func TestRecallForContext(t *testing.T) {
	entries := []MemoryEntry{
		{Key: "global", Value: "g", Source: MemorySourceUser, State: MemoryActive},
		{Key: "project-global", Value: "pg", Source: MemorySourceAgent, State: MemoryActive, ProjectID: "proj_a"},
		{Key: "projA", Value: "a", Source: MemorySourceAgent, State: MemoryActive, SessionKey: "sess_alice", ProjectID: "proj_a"},
		{Key: "projB", Value: "b", Source: MemorySourceAgent, State: MemoryActive, SessionKey: "sess_alice", ProjectID: "proj_b"},
		{Key: "other-session", Value: "c", Source: MemorySourceAgent, State: MemoryActive, SessionKey: "sess_bob", ProjectID: "proj_a"},
		{Key: "superseded", Value: "old", Source: MemorySourceAgent, State: MemorySuperseded, SessionKey: "sess_alice", ProjectID: "proj_a"},
	}
	result := RecallForContext(entries, "sess_alice", "proj_a")
	if len(result.Memories) != 3 {
		t.Fatalf("expected 3 memories (global + project-global + projA), got %d", len(result.Memories))
	}
	if result.Memories[0].Key != "global" {
		t.Fatalf("expected global (user source) first, got %s", result.Memories[0].Key)
	}
}

func TestDetectMemoryConflicts(t *testing.T) {
	memories := []MemoryEntry{
		{Key: "a", Namespace: "project", Field: "db_type", Value: "PostgreSQL"},
		{Key: "b", Namespace: "project", Field: "db_type", Value: "MySQL"},
	}
	conflicts := detectMemoryConflicts(memories)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Kind != ConflictMemoryVsMemory {
		t.Fatalf("expected memory_vs_memory conflict")
	}
}

func TestTouchMemory(t *testing.T) {
	entry := MemoryEntry{
		Source:        MemorySourceAgent,
		State:         MemoryActive,
		UsedCount:     5,
		EvidenceCount: 3,
	}
	TouchMemory(&entry)
	if entry.UsedCount != 6 {
		t.Fatalf("expected UsedCount 6, got %d", entry.UsedCount)
	}
	if entry.LastUsedAt.IsZero() {
		t.Fatal("expected LastUsedAt to be set")
	}
	if entry.Score <= 0 {
		t.Fatalf("expected recomputed score, got %f", entry.Score)
	}
}

func TestTouchMemoryVerification(t *testing.T) {
	passedEntry := MemoryEntry{
		Source:        MemorySourceAgent,
		State:         MemoryActive,
		UsedCount:     2,
		EvidenceCount: 3,
	}
	TouchMemoryVerification(&passedEntry, true)
	if passedEntry.UsedCount != 3 {
		t.Fatalf("expected UsedCount 3 after verification pass, got %d", passedEntry.UsedCount)
	}
	if passedEntry.LastUsedAt.IsZero() {
		t.Fatal("expected LastUsedAt to be set on verification pass")
	}
	if passedEntry.VerificationPassCount != 1 || passedEntry.VerificationFailCount != 0 {
		t.Fatalf("unexpected verification counters after pass: pass=%d fail=%d", passedEntry.VerificationPassCount, passedEntry.VerificationFailCount)
	}
	if want := ComputeScore(passedEntry); math.Abs(passedEntry.Score-want) > 1e-6 {
		t.Fatalf("expected score to be recomputed after pass: got %f want %f", passedEntry.Score, want)
	}

	failedEntry := MemoryEntry{
		Source:                MemorySourceAgent,
		State:                 MemoryActive,
		UsedCount:             2,
		EvidenceCount:         3,
		VerificationPassCount: 2,
	}
	TouchMemoryVerification(&failedEntry, false)
	if failedEntry.UsedCount != 3 {
		t.Fatalf("expected UsedCount 3 after verification failure, got %d", failedEntry.UsedCount)
	}
	if failedEntry.VerificationPassCount != 2 || failedEntry.VerificationFailCount != 1 {
		t.Fatalf("unexpected verification counters after failure: pass=%d fail=%d", failedEntry.VerificationPassCount, failedEntry.VerificationFailCount)
	}
	if want := ComputeScore(failedEntry); math.Abs(failedEntry.Score-want) > 1e-6 {
		t.Fatalf("expected score to be recomputed after failure: got %f want %f", failedEntry.Score, want)
	}
}

func TestFieldsSimilar(t *testing.T) {
	if !fieldsSimilar("db_type", "db_type") {
		t.Fatal("same field should be similar")
	}
	if !fieldsSimilar("db_type", "DB_TYPE") {
		t.Fatal("case-insensitive should match")
	}
	if !fieldsSimilar("db", "db_type") {
		t.Fatal("substring should match")
	}
	if fieldsSimilar("db_type", "server_port") {
		t.Fatal("unrelated fields should not match")
	}
}

func TestRecallWithQuery_LexicalMatch(t *testing.T) {
	now := time.Now().UTC()
	entries := []MemoryEntry{
		{
			Key:           "calendar",
			Value:         "calendar reminder cadence",
			Source:        MemorySourceAgent,
			State:         MemoryActive,
			EvidenceCount: 1,
			LastUsedAt:    now,
		},
		{
			Key:           "database",
			Value:         "postgres connection string for reporting",
			Source:        MemorySourceAgent,
			State:         MemoryActive,
			EvidenceCount: 1,
			LastUsedAt:    now,
		},
	}

	hits := RecallWithQuery(entries, MemoryQuery{Text: "postgres connection", MaxResults: 10})
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Entry.Key != "database" {
		t.Fatalf("expected lexical hit first, got %s", hits[0].Entry.Key)
	}
	if !strings.Contains(hits[0].Reason, "lexical:value") {
		t.Fatalf("expected lexical reason, got %q", hits[0].Reason)
	}
}

func TestRecallWithQuery_AuthorityOrder(t *testing.T) {
	now := time.Now().UTC()
	entries := []MemoryEntry{
		{
			Key:           "agent",
			Value:         "shared topic",
			Source:        MemorySourceAgent,
			State:         MemoryActive,
			LastUsedAt:    now,
			EvidenceCount: 0,
		},
		{
			Key:           "managed",
			Value:         "shared topic",
			Source:        MemorySourceAgent,
			Managed:       true,
			State:         MemoryActive,
			LastUsedAt:    now,
			EvidenceCount: 0,
		},
		{
			Key:           "user",
			Value:         "shared topic",
			Source:        MemorySourceUser,
			State:         MemoryActive,
			LastUsedAt:    now,
			EvidenceCount: 0,
		},
	}

	hits := RecallWithQuery(entries, MemoryQuery{Text: "shared topic", MaxResults: 10})
	assertRecallHitKeys(t, hits, []string{"user", "managed", "agent"})
}

func TestRecallWithQuery_CorrectionPenalty(t *testing.T) {
	now := time.Now().UTC()
	entries := []MemoryEntry{
		{
			Key:             "stable",
			Value:           "deploy target service",
			Source:          MemorySourceAgent,
			State:           MemoryActive,
			LastUsedAt:      now,
			EvidenceCount:   2,
			CorrectionCount: 0,
		},
		{
			Key:             "corrected",
			Value:           "deploy target service",
			Source:          MemorySourceAgent,
			State:           MemoryActive,
			LastUsedAt:      now,
			EvidenceCount:   2,
			CorrectionCount: 3,
		},
	}

	hits := RecallWithQuery(entries, MemoryQuery{Text: "deploy target", MaxResults: 10})
	assertRecallHitKeys(t, hits, []string{"stable", "corrected"})
	if hits[0].RelevanceScore <= hits[1].RelevanceScore {
		t.Fatalf("expected correction penalty to lower score: stable=%f corrected=%f", hits[0].RelevanceScore, hits[1].RelevanceScore)
	}
}

func TestRecallWithQuery_DomainBoost(t *testing.T) {
	now := time.Now().UTC()
	entries := []MemoryEntry{
		{
			Key:           "browser",
			Value:         "tab capture workflow",
			Source:        MemorySourceAgent,
			State:         MemoryActive,
			LastUsedAt:    now,
			EvidenceCount: 1,
			Tags:          []string{"browser", "capture"},
		},
		{
			Key:           "plain",
			Value:         "tab capture workflow",
			Source:        MemorySourceAgent,
			State:         MemoryActive,
			LastUsedAt:    now,
			EvidenceCount: 1,
			Tags:          []string{"capture"},
		},
	}

	hits := RecallWithQuery(entries, MemoryQuery{
		Text:       "tab capture",
		Domains:    []string{"browser"},
		MaxResults: 10,
	})
	assertRecallHitKeys(t, hits, []string{"browser", "plain"})
	if !strings.Contains(hits[0].Reason, "domain:browser") {
		t.Fatalf("expected domain boost reason, got %q", hits[0].Reason)
	}
}

func TestRecallWithQuery_FallbackToStatic(t *testing.T) {
	now := time.Now().UTC()
	entries := []MemoryEntry{
		{
			Key:        "user-global",
			Value:      "global preference",
			Source:     MemorySourceUser,
			State:      MemoryActive,
			LastUsedAt: now.Add(-48 * time.Hour),
		},
		{
			Key:        "high-score",
			Value:      "project memory",
			Source:     MemorySourceAgent,
			State:      MemoryActive,
			SessionKey: "sess-a",
			ProjectID:  "proj-a",
			Score:      0.9,
			LastUsedAt: now,
		},
		{
			Key:        "low-score",
			Value:      "project memory",
			Source:     MemorySourceAgent,
			State:      MemoryActive,
			SessionKey: "sess-a",
			ProjectID:  "proj-a",
			Score:      0.2,
			LastUsedAt: now.Add(-24 * time.Hour),
		},
		{
			Key:        "other-project",
			Value:      "ignore me",
			Source:     MemorySourceAgent,
			State:      MemoryActive,
			SessionKey: "sess-a",
			ProjectID:  "proj-b",
			Score:      1.0,
		},
	}

	staticResult := RecallForContext(entries, "sess-a", "proj-a")
	hits := RecallWithQuery(entries, MemoryQuery{SessionKey: "sess-a", ProjectID: "proj-a"})
	if len(hits) != len(staticResult.Memories) {
		t.Fatalf("expected %d fallback hits, got %d", len(staticResult.Memories), len(hits))
	}
	for i, memory := range staticResult.Memories {
		if hits[i].Entry.Key != memory.Key {
			t.Fatalf("fallback order mismatch at %d: got %s want %s", i, hits[i].Entry.Key, memory.Key)
		}
		if hits[i].Reason != "fallback:static" {
			t.Fatalf("expected fallback reason, got %q", hits[i].Reason)
		}
	}
}

func assertRecallHitKeys(t *testing.T, hits []MemoryHit, want []string) {
	t.Helper()
	if len(hits) < len(want) {
		t.Fatalf("expected at least %d hits, got %d", len(want), len(hits))
	}
	for i, key := range want {
		if hits[i].Entry.Key != key {
			t.Fatalf("hit[%d] = %s, want %s", i, hits[i].Entry.Key, key)
		}
	}
}
