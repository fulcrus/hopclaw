package cron

import (
	"errors"
	"testing"
	"time"
)

func TestNextRunTimeAtFuture(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(24 * time.Hour).UTC()
	schedule := Schedule{
		Kind: ScheduleKindAt,
		At:   future.Format(time.RFC3339),
	}

	next, err := NextRunTime(schedule, time.Now())
	if err != nil {
		t.Fatalf("NextRunTime error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero time for future at schedule")
	}
}

func TestNextRunTimeAtExpired(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-24 * time.Hour).UTC()
	schedule := Schedule{
		Kind: ScheduleKindAt,
		At:   past.Format(time.RFC3339),
	}

	next, err := NextRunTime(schedule, time.Now())
	if err != nil {
		t.Fatalf("NextRunTime error: %v", err)
	}
	if !next.IsZero() {
		t.Fatal("expected zero time for expired at schedule")
	}
}

func TestNextRunTimeAtEmptyField(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindAt, At: ""}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for empty at field")
	}
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("error = %v, want ErrInvalidSchedule", err)
	}
}

func TestNextRunTimeEvery(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindEvery, Every: "1h"}
	now := time.Now()

	next, err := NextRunTime(schedule, now)
	if err != nil {
		t.Fatalf("NextRunTime error: %v", err)
	}
	if next.Before(now) {
		t.Fatal("next run time should be after 'after'")
	}
}

func TestNextRunTimeEveryNegativeDuration(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindEvery, Every: "-1h"}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
}

func TestNextRunTimeEveryEmptyField(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindEvery, Every: ""}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for empty every field")
	}
}

func TestNextRunTimeCron(t *testing.T) {
	t.Parallel()

	schedule := Schedule{
		Kind:       ScheduleKindCron,
		Expression: "0 9 * * *",
	}

	now := time.Now()
	next, err := NextRunTime(schedule, now)
	if err != nil {
		t.Fatalf("NextRunTime error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero time for cron schedule")
	}
	if !next.After(now) {
		t.Fatal("next run time should be after 'after'")
	}
}

func TestNextRunTimeCronEmptyExpression(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindCron, Expression: ""}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for empty cron expression")
	}
}

func TestNextRunTimeCronInvalidExpression(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: ScheduleKindCron, Expression: "invalid cron"}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNextRunTimeUnknownKind(t *testing.T) {
	t.Parallel()

	schedule := Schedule{Kind: "unknown"}
	_, err := NextRunTime(schedule, time.Now())
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("error = %v, want ErrInvalidSchedule", err)
	}
}

func TestNextRunTimeAnchoredEvery(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2025, 1, 1, 2, 30, 0, 0, time.UTC)

	schedule := Schedule{Kind: ScheduleKindEvery, Every: "1h"}
	next, err := NextRunTimeAnchored(schedule, after, anchor)
	if err != nil {
		t.Fatalf("NextRunTimeAnchored error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero next run time")
	}
	if !next.After(after) {
		t.Fatal("next run time should be after 'after'")
	}
}

func TestNextRunTimeAnchoredNonEvery(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(24 * time.Hour).UTC()
	schedule := Schedule{Kind: ScheduleKindAt, At: future.Format(time.RFC3339)}

	next, err := NextRunTimeAnchored(schedule, time.Now(), time.Time{})
	if err != nil {
		t.Fatalf("NextRunTimeAnchored error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero time for at schedule via anchored")
	}
}
