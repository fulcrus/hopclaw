package cron

import (
	"fmt"
	"strings"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

// cronParser uses the standard five-field cron format (minute through day-of-week).
var cronParser = cronlib.NewParser(
	cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow,
)

// NextRunTime computes the next execution time for a schedule after the given reference.
//
// For "at" schedules, the parsed time is returned if it is still in the future;
// otherwise zero time (expired) is returned with no error.
//
// For "every" schedules, the next interval tick after `after` is returned. The
// anchor point is derived from the job's CreatedAt (passed via `anchor`); if
// zero, `after` is used.
//
// For "cron" schedules, the robfig/cron parser computes the next occurrence.
func NextRunTime(schedule Schedule, after time.Time) (time.Time, error) {
	switch strings.TrimSpace(strings.ToLower(schedule.Kind)) {
	case ScheduleKindAt:
		return nextRunAt(schedule, after)
	case ScheduleKindEvery:
		return nextRunEvery(schedule, after)
	case ScheduleKindCron:
		return nextRunCron(schedule, after)
	default:
		return time.Time{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidSchedule, schedule.Kind)
	}
}

// NextRunTimeAnchored is like NextRunTime but accepts an explicit anchor for
// "every" schedules (typically the job's CreatedAt or LastRunAt).
func NextRunTimeAnchored(schedule Schedule, after time.Time, anchor time.Time) (time.Time, error) {
	if schedule.Kind == ScheduleKindEvery {
		return nextRunEveryAnchored(schedule, after, anchor)
	}
	return NextRunTime(schedule, after)
}

// ---------------------------------------------------------------------------
// "at" — one-shot at an absolute time
// ---------------------------------------------------------------------------

func nextRunAt(schedule Schedule, after time.Time) (time.Time, error) {
	at := strings.TrimSpace(schedule.At)
	if at == "" {
		return time.Time{}, fmt.Errorf("%w: at field is required for kind %q", ErrInvalidSchedule, ScheduleKindAt)
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse at %q: %v", ErrInvalidSchedule, at, err)
	}
	if t.After(after) {
		return t, nil
	}
	// Expired — return zero time.
	return time.Time{}, nil
}

// ---------------------------------------------------------------------------
// "every" — repeating interval
// ---------------------------------------------------------------------------

func nextRunEvery(schedule Schedule, after time.Time) (time.Time, error) {
	return nextRunEveryAnchored(schedule, after, after)
}

func nextRunEveryAnchored(schedule Schedule, after time.Time, anchor time.Time) (time.Time, error) {
	raw := strings.TrimSpace(schedule.Every)
	if raw == "" {
		return time.Time{}, fmt.Errorf("%w: every field is required for kind %q", ErrInvalidSchedule, ScheduleKindEvery)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse every %q: %v", ErrInvalidSchedule, raw, err)
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("%w: every duration must be positive, got %s", ErrInvalidSchedule, d)
	}
	if anchor.IsZero() {
		anchor = after
	}
	if after.Before(anchor) {
		return anchor, nil
	}
	elapsed := after.Sub(anchor)
	steps := int64(elapsed/d) + 1
	return anchor.Add(time.Duration(steps) * d), nil
}

// ---------------------------------------------------------------------------
// "cron" — standard cron expression
// ---------------------------------------------------------------------------

func nextRunCron(schedule Schedule, after time.Time) (time.Time, error) {
	expr := strings.TrimSpace(schedule.Expression)
	if expr == "" {
		return time.Time{}, fmt.Errorf("%w: expression field is required for kind %q", ErrInvalidSchedule, ScheduleKindCron)
	}

	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse cron expression %q: %v", ErrInvalidSchedule, expr, err)
	}

	loc := time.Local
	if tz := strings.TrimSpace(schedule.Timezone); tz != "" {
		loc, err = time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: load timezone %q: %v", ErrInvalidSchedule, tz, err)
		}
	}

	ref := after.In(loc)
	next := sched.Next(ref)
	if next.IsZero() {
		return time.Time{}, nil
	}
	return next.UTC(), nil
}
