package automation

import (
	"strings"
	"time"
)

type Kind string

const (
	KindCron   Kind = "cron"
	KindWakeup Kind = "wakeup"
	KindWatch  Kind = "watch"
	KindHook   Kind = "hook"
)

type DeliveryTarget struct {
	Kind       string `json:"kind,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Channel    string `json:"channel,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	Target     string `json:"target,omitempty"`
	Label      string `json:"label,omitempty"`
}

type NotificationStats struct {
	TotalCount      int       `json:"total_count,omitempty"`
	FailureCount    int       `json:"failure_count,omitempty"`
	TodayCount      int       `json:"today_count,omitempty"`
	TodayDate       string    `json:"today_date,omitempty"`
	LastAttemptAt   time.Time `json:"last_attempt_at,omitempty"`
	LastDeliveredAt time.Time `json:"last_delivered_at,omitempty"`
	LastStatus      string    `json:"last_status,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

type NotificationSummary struct {
	TotalCount   int    `json:"total_count"`
	FailureCount int    `json:"failure_count"`
	TodayCount   int    `json:"today_count"`
	TodayDate    string `json:"today_date,omitempty"`
}

type ExecutionRecord struct {
	OccurredAt          time.Time `json:"occurred_at,omitempty"`
	Status              string    `json:"status,omitempty"`
	RunID               string    `json:"run_id,omitempty"`
	SessionID           string    `json:"session_id,omitempty"`
	ToolName            string    `json:"tool_name,omitempty"`
	TargetLabel         string    `json:"target_label,omitempty"`
	Summary             string    `json:"summary,omitempty"`
	Error               string    `json:"error,omitempty"`
	VerificationStatus  string    `json:"verification_status,omitempty"`
	VerificationSummary string    `json:"verification_summary,omitempty"`
}

type Item struct {
	ID            string             `json:"id"`
	Kind          Kind               `json:"kind"`
	Name          string             `json:"name"`
	Enabled       bool               `json:"enabled"`
	Schedule      string             `json:"schedule,omitempty"`
	Channel       string             `json:"channel,omitempty"`
	Message       string             `json:"message,omitempty"`
	SessionKey    string             `json:"session_key,omitempty"`
	Model         string             `json:"model,omitempty"`
	SourceKind    string             `json:"source_kind,omitempty"`
	SourceLabel   string             `json:"source_label,omitempty"`
	PromptPreview string             `json:"prompt_preview,omitempty"`
	NextRunAt     time.Time          `json:"next_run_at,omitempty"`
	LastRunAt     time.Time          `json:"last_run_at,omitempty"`
	LastExecution *ExecutionRecord   `json:"last_execution,omitempty"`
	Delivery      *DeliveryTarget    `json:"delivery,omitempty"`
	Notifications *NotificationStats `json:"notifications,omitempty"`
}

type ServiceStatus struct {
	Available bool `json:"available"`
	Running   bool `json:"running"`
	Count     int  `json:"count"`
}

func RecordNotification(stats NotificationStats, now time.Time, delivered bool, errText string) NotificationStats {
	now = now.UTC()
	day := now.Format("2006-01-02")
	if strings.TrimSpace(stats.TodayDate) != day {
		stats.TodayDate = day
		stats.TodayCount = 0
	}
	stats.LastAttemptAt = now
	if delivered {
		stats.TotalCount++
		stats.TodayCount++
		stats.LastDeliveredAt = now
		stats.LastStatus = "delivered"
		stats.LastError = ""
		return stats
	}
	stats.FailureCount++
	stats.LastStatus = "failed"
	stats.LastError = strings.TrimSpace(errText)
	return stats
}

func (s NotificationStats) Populated() bool {
	return s.TotalCount > 0 ||
		s.FailureCount > 0 ||
		s.TodayCount > 0 ||
		!s.LastAttemptAt.IsZero() ||
		!s.LastDeliveredAt.IsZero() ||
		strings.TrimSpace(s.LastStatus) != "" ||
		strings.TrimSpace(s.LastError) != ""
}

func AggregateNotifications(items []Item, now time.Time) NotificationSummary {
	now = now.UTC()
	day := now.Format("2006-01-02")
	out := NotificationSummary{TodayDate: day}
	for _, item := range items {
		if item.Notifications == nil {
			continue
		}
		out.TotalCount += item.Notifications.TotalCount
		out.FailureCount += item.Notifications.FailureCount
		if strings.TrimSpace(item.Notifications.TodayDate) == day {
			out.TodayCount += item.Notifications.TodayCount
		}
	}
	return out
}
