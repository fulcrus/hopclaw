package automation

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RecordNotification
// ---------------------------------------------------------------------------

func TestRecordNotificationDelivered(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	stats := RecordNotification(NotificationStats{}, now, true, "")

	if stats.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1", stats.TotalCount)
	}
	if stats.TodayCount != 1 {
		t.Fatalf("TodayCount = %d, want 1", stats.TodayCount)
	}
	if stats.FailureCount != 0 {
		t.Fatalf("FailureCount = %d, want 0", stats.FailureCount)
	}
	if stats.LastStatus != "delivered" {
		t.Fatalf("LastStatus = %q, want %q", stats.LastStatus, "delivered")
	}
	if stats.LastError != "" {
		t.Fatalf("LastError = %q, want empty", stats.LastError)
	}
	if stats.LastDeliveredAt != now {
		t.Fatalf("LastDeliveredAt = %v, want %v", stats.LastDeliveredAt, now)
	}
	if stats.TodayDate != "2025-06-15" {
		t.Fatalf("TodayDate = %q, want %q", stats.TodayDate, "2025-06-15")
	}
}

func TestRecordNotificationFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	stats := RecordNotification(NotificationStats{}, now, false, "connection refused")

	if stats.TotalCount != 0 {
		t.Fatalf("TotalCount = %d, want 0", stats.TotalCount)
	}
	if stats.FailureCount != 1 {
		t.Fatalf("FailureCount = %d, want 1", stats.FailureCount)
	}
	if stats.LastStatus != "failed" {
		t.Fatalf("LastStatus = %q, want %q", stats.LastStatus, "failed")
	}
	if stats.LastError != "connection refused" {
		t.Fatalf("LastError = %q, want %q", stats.LastError, "connection refused")
	}
}

func TestRecordNotificationResetsCountOnNewDay(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	stats := RecordNotification(NotificationStats{}, day1, true, "")
	if stats.TodayCount != 1 {
		t.Fatalf("TodayCount after day1 = %d, want 1", stats.TodayCount)
	}

	// Same day: increments.
	stats = RecordNotification(stats, day1.Add(time.Hour), true, "")
	if stats.TodayCount != 2 {
		t.Fatalf("TodayCount after second delivery = %d, want 2", stats.TodayCount)
	}

	// New day: resets to 1.
	day2 := time.Date(2025, 6, 16, 8, 0, 0, 0, time.UTC)
	stats = RecordNotification(stats, day2, true, "")
	if stats.TodayCount != 1 {
		t.Fatalf("TodayCount after day2 = %d, want 1", stats.TodayCount)
	}
	if stats.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3", stats.TotalCount)
	}
}

// ---------------------------------------------------------------------------
// NotificationStats.Populated
// ---------------------------------------------------------------------------

func TestNotificationStatsPopulated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stats NotificationStats
		want  bool
	}{
		{"zero value", NotificationStats{}, false},
		{"total count", NotificationStats{TotalCount: 1}, true},
		{"failure count", NotificationStats{FailureCount: 1}, true},
		{"today count", NotificationStats{TodayCount: 1}, true},
		{"last attempt at", NotificationStats{LastAttemptAt: time.Now()}, true},
		{"last delivered at", NotificationStats{LastDeliveredAt: time.Now()}, true},
		{"last status", NotificationStats{LastStatus: "delivered"}, true},
		{"last error", NotificationStats{LastError: "oops"}, true},
		{"whitespace status", NotificationStats{LastStatus: "  "}, false},
		{"whitespace error", NotificationStats{LastError: "  "}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.stats.Populated()
			if got != tt.want {
				t.Fatalf("Populated() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AggregateNotifications
// ---------------------------------------------------------------------------

func TestAggregateNotificationsEmpty(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	summary := AggregateNotifications(nil, now)

	if summary.TotalCount != 0 {
		t.Fatalf("TotalCount = %d, want 0", summary.TotalCount)
	}
	if summary.TodayDate != "2025-06-15" {
		t.Fatalf("TodayDate = %q", summary.TodayDate)
	}
}

func TestAggregateNotificationsMultipleItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	items := []Item{
		{
			Notifications: &NotificationStats{
				TotalCount:   5,
				FailureCount: 1,
				TodayDate:    "2025-06-15",
				TodayCount:   3,
			},
		},
		{Notifications: nil}, // nil notifications should be skipped
		{
			Notifications: &NotificationStats{
				TotalCount:   10,
				FailureCount: 2,
				TodayDate:    "2025-06-15",
				TodayCount:   4,
			},
		},
		{
			Notifications: &NotificationStats{
				TotalCount:   3,
				FailureCount: 0,
				TodayDate:    "2025-06-14", // different day
				TodayCount:   3,
			},
		},
	}

	summary := AggregateNotifications(items, now)

	if summary.TotalCount != 18 {
		t.Fatalf("TotalCount = %d, want 18", summary.TotalCount)
	}
	if summary.FailureCount != 3 {
		t.Fatalf("FailureCount = %d, want 3", summary.FailureCount)
	}
	// Only the two items whose TodayDate matches today should contribute.
	if summary.TodayCount != 7 {
		t.Fatalf("TodayCount = %d, want 7", summary.TodayCount)
	}
}

// ---------------------------------------------------------------------------
// Item / Kind type checks
// ---------------------------------------------------------------------------

func TestKindConstants(t *testing.T) {
	t.Parallel()

	kinds := []Kind{KindCron, KindWakeup, KindWatch, KindHook}
	for _, k := range kinds {
		if string(k) == "" {
			t.Fatalf("Kind constant is empty")
		}
	}
}

func TestServiceStatusDefaults(t *testing.T) {
	t.Parallel()

	var s ServiceStatus
	if s.Available || s.Running || s.Count != 0 {
		t.Fatalf("zero ServiceStatus has non-zero fields")
	}
}
