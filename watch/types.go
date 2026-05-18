package watch

import (
	"context"
	"errors"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrDuplicateID     = errors.New("duplicate watch id")
	ErrInvalidInterval = errors.New("invalid interval")
	ErrInvalidSource   = errors.New("invalid source")
)

const (
	SourceKindHTTP            = "http"
	SourceKindFile            = "file"
	SourceKindFeed            = "feed"
	SourceKindMailbox         = "mailbox"
	SourceKindBrowserSnapshot = "browser_snapshot"
	SourceKindCalendar        = "calendar"
	SourceKindWebhook         = "webhook"
	SourceKindStructuredInbox = "structured_app_inbox"
)

const (
	RunStatusOK        = "ok"
	RunStatusError     = "error"
	RunStatusUnchanged = "unchanged"
	RunStatusPrimed    = "primed"
	RunStatusTriggered = "triggered"
)

type Watch struct {
	ID                      string                        `json:"id"`
	Name                    string                        `json:"name"`
	Enabled                 bool                          `json:"enabled"`
	Interval                string                        `json:"interval"`
	Source                  Source                        `json:"source"`
	Delivery                *automation.DeliveryTarget    `json:"delivery,omitempty"`
	Notifications           NotificationStats             `json:"notifications,omitempty"`
	Prompt                  string                        `json:"prompt,omitempty"`
	SessionKey              string                        `json:"session_key,omitempty"`
	Model                   string                        `json:"model,omitempty"`
	AutomationID            string                        `json:"automation_id,omitempty"`
	FireOnStart             bool                          `json:"fire_on_start,omitempty"`
	LastCheckedAt           time.Time                     `json:"last_checked_at,omitempty"`
	LastTriggeredAt         time.Time                     `json:"last_triggered_at,omitempty"`
	LastRunID               string                        `json:"last_run_id,omitempty"`
	LastStatus              string                        `json:"last_status,omitempty"`
	LastError               string                        `json:"last_error,omitempty"`
	LastSummary             string                        `json:"last_summary,omitempty"`
	LastVerificationStatus  string                        `json:"last_verification_status,omitempty"`
	LastVerificationSummary string                        `json:"last_verification_summary,omitempty"`
	LastResult              *resultmodel.AutomationResult `json:"last_result,omitempty"`
	ConsecutiveErrors       int                           `json:"consecutive_errors,omitempty"`
	BackoffUntil            time.Time                     `json:"backoff_until,omitempty"`
	LastFingerprint         string                        `json:"last_fingerprint,omitempty"`
	NextCheckAt             time.Time                     `json:"next_check_at,omitempty"`
	CreatedAt               time.Time                     `json:"created_at"`
	UpdatedAt               time.Time                     `json:"updated_at"`
}

type Source struct {
	Kind            string                 `json:"kind"`
	HTTP            *HTTPSource            `json:"http,omitempty"`
	File            *FileSource            `json:"file,omitempty"`
	Feed            *FeedSource            `json:"feed,omitempty"`
	Mailbox         *MailboxSource         `json:"mailbox,omitempty"`
	BrowserSnapshot *BrowserSnapshotSource `json:"browser_snapshot,omitempty"`
	Calendar        *CalendarSource        `json:"calendar,omitempty"`
	Webhook         *WebhookSource         `json:"webhook,omitempty"`
	StructuredInbox *StructuredInboxSource `json:"structured_app_inbox,omitempty"`
}

type HTTPSource struct {
	URL string `json:"url"`
}

type FileSource struct {
	Path string `json:"path"`
}

type FeedSource struct {
	URL string `json:"url"`
}

type MailboxSource struct {
	Folder string `json:"folder,omitempty"`
	Query  string `json:"query,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type BrowserSnapshotSource struct {
	URL string `json:"url"`
}

type CalendarSource struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type WebhookSource struct {
	WebhookID  string `json:"webhook_id,omitempty"`
	SenderID   string `json:"sender_id,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type StructuredInboxSource struct {
	SessionKey string `json:"session_key,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type StoreFile struct {
	Version int     `json:"version"`
	Watches []Watch `json:"watches"`
}

type RuntimeSubmitter = automation.Runtime
type DeliveryTarget = automation.DeliveryTarget
type NotificationStats = automation.NotificationStats

type RuntimeVerifier interface {
	GetRunVerification(ctx context.Context, runID string) (*verifyrt.RunVerification, error)
}

type ChannelDeliverer interface {
	DeliverMessage(ctx context.Context, target automation.DeliveryTarget, content string) error
}

type EmailConfig struct {
	IMAPHost string
	IMAPPort int
	Username string
	Password string
}

type CalendarConfig struct {
	CalDAVURL string
	Username  string
	Password  string
}

type SessionInboxReader interface {
	GetByKey(ctx context.Context, sessionKey string) (*agent.Session, error)
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

func WithEmailConfig(cfg EmailConfig) Option {
	return func(s *Service) {
		if s != nil {
			s.email = cfg
		}
	}
}

func WithBrowserClient(client *browserclient.Client) Option {
	return func(s *Service) {
		if s != nil {
			s.browserClient = client
		}
	}
}

func WithCalendarConfig(cfg CalendarConfig) Option {
	return func(s *Service) {
		if s != nil {
			s.calendar = cfg
		}
	}
}

func WithSessionInboxReader(reader SessionInboxReader) Option {
	return func(s *Service) {
		if s != nil {
			s.sessionReader = reader
		}
	}
}

func WithChannelDeliverer(deliverer ChannelDeliverer) Option {
	return func(s *Service) {
		if s != nil {
			s.channels = deliverer
		}
	}
}
