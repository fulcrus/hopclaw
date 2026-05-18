package cron

import (
	"context"
	"errors"
	"time"

	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrNotFound        = errors.New("not found")
	ErrDuplicateID     = errors.New("duplicate job id")
	ErrInvalidSchedule = errors.New("invalid schedule")
)

// ---------------------------------------------------------------------------
// Schedule kinds
// ---------------------------------------------------------------------------

const (
	ScheduleKindAt    = "at"
	ScheduleKindEvery = "every"
	ScheduleKindCron  = "cron"
)

// ---------------------------------------------------------------------------
// Run status values
// ---------------------------------------------------------------------------

const (
	RunStatusOK      = "ok"
	RunStatusError   = "error"
	RunStatusSkipped = "skipped"
)

// ---------------------------------------------------------------------------
// Resilience constants
// ---------------------------------------------------------------------------

const (
	// maxConsecutiveErrors is the threshold after which a job is auto-disabled.
	maxConsecutiveErrors = 10

	// failureAlertThreshold triggers an alert delivery after this many consecutive errors.
	failureAlertThreshold = 3

	// backoffBase is the initial backoff duration after a failure.
	backoffBase = 30 * time.Second

	// backoffMax caps the exponential backoff.
	backoffMax = 1 * time.Hour
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// Job represents a scheduled task that submits messages to the agent runtime.
type Job struct {
	ID                      string                        `json:"id"`
	Name                    string                        `json:"name"`
	Enabled                 bool                          `json:"enabled"`
	Schedule                Schedule                      `json:"schedule"`
	Payload                 Payload                       `json:"payload"`
	Delivery                *Delivery                     `json:"delivery,omitempty"`
	Notifications           NotificationStats             `json:"notifications,omitempty"`
	SessionKey              string                        `json:"session_key,omitempty"`
	Model                   string                        `json:"model,omitempty"`
	AutomationID            string                        `json:"automation_id,omitempty"`
	LastRunAt               time.Time                     `json:"last_run_at,omitempty"`
	LastRunID               string                        `json:"last_run_id,omitempty"`
	NextRunAt               time.Time                     `json:"next_run_at,omitempty"`
	LastStatus              string                        `json:"last_status,omitempty"`
	LastError               string                        `json:"last_error,omitempty"`
	LastSummary             string                        `json:"last_summary,omitempty"`
	LastVerificationStatus  string                        `json:"last_verification_status,omitempty"`
	LastVerificationSummary string                        `json:"last_verification_summary,omitempty"`
	LastResult              *resultmodel.AutomationResult `json:"last_result,omitempty"`
	ConsecutiveErrors       int                           `json:"consecutive_errors,omitempty"`
	BackoffUntil            time.Time                     `json:"backoff_until,omitempty"`
	CreatedAt               time.Time                     `json:"created_at"`
	UpdatedAt               time.Time                     `json:"updated_at"`
}

// Schedule defines when a job should fire.
type Schedule struct {
	Kind       string `json:"kind"`                 // "at", "every", "cron"
	At         string `json:"at,omitempty"`         // RFC3339 for one-shot
	Every      string `json:"every,omitempty"`      // Go duration string "30m", "1h"
	Expression string `json:"expression,omitempty"` // cron expression "0 9 * * *"
	Timezone   string `json:"timezone,omitempty"`
}

// Payload is the content submitted as an agent run.
type Payload struct {
	Content string `json:"content"`
}

// Delivery configures where to send the agent's response.
type Delivery = automation.DeliveryTarget

// ---------------------------------------------------------------------------
// Store file envelope
// ---------------------------------------------------------------------------

// StoreFile is the on-disk JSON envelope for persisted jobs.
type StoreFile struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// ---------------------------------------------------------------------------
// Runtime integration interfaces
// ---------------------------------------------------------------------------

// RuntimeSubmitter abstracts the runtime Submit call so the cron package
// does not import the heavy runtime or agent packages.
type RuntimeSubmitter = automation.Runtime
type NotificationStats = automation.NotificationStats

type RuntimeVerifier interface {
	GetRunVerification(ctx context.Context, runID string) (*verifyrt.RunVerification, error)
}

type Option func(*Service)

func WithExecutionTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if s != nil && timeout > 0 {
			s.executionTimeout = timeout
		}
	}
}

func WithPollInterval(interval time.Duration) Option {
	return func(s *Service) {
		if s != nil && interval > 0 {
			s.pollInterval = interval
		}
	}
}

// CronRunResult is the canonical automation outcome returned by the runtime.
type CronRunResult = resultmodel.AutomationResult

// ChannelDeliverer abstracts outbound message delivery so the cron package
// does not import the channels packages directly.
type ChannelDeliverer interface {
	DeliverMessage(ctx context.Context, target automation.DeliveryTarget, content string) error
}
