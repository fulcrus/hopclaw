// Package channels defines the channel.v1 adapter interface and
// common types for messaging channel integrations (Feishu, Slack, etc.).
package channels

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

// Status represents a channel connection state.
type Status string

const (
	StatusConnected    Status = "connected"
	StatusDisconnected Status = "disconnected"
	StatusConnecting   Status = "connecting"
	StatusError        Status = "error"
)

// InboundMessage represents a message received from a channel.
type InboundMessage struct {
	ChannelID     string                       `json:"channel_id"`
	SenderID      string                       `json:"sender_id"`
	SenderName    string                       `json:"sender_name"`
	Content       string                       `json:"content"`
	ContentBlocks []contextengine.ContentBlock `json:"content_blocks,omitempty"`
	Images        []string                     `json:"images,omitempty"`
	RawEvent      map[string]any               `json:"raw_event,omitempty"`
}

// OutboundMessage represents a message to send through a channel.
type OutboundMessage struct {
	ChannelID   string               `json:"channel_id"`
	TargetID    string               `json:"target_id"`
	ReplyToID   string               `json:"reply_to_id,omitempty"`
	Content     string               `json:"content"`
	Format      string               `json:"format,omitempty"` // "text", "markdown", "rich"
	Blocks      []OutboundBlock      `json:"blocks,omitempty"`
	Attachments []OutboundAttachment `json:"attachments,omitempty"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
}

// OutboundBlock represents one structured content block in an outbound
// channel message.
type OutboundBlock struct {
	Kind    string `json:"kind"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

// OutboundAttachment describes one file or rich attachment sent with an
// outbound channel message.
type OutboundAttachment struct {
	Kind        string `json:"kind"`
	Label       string `json:"label,omitempty"`
	URI         string `json:"uri,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

// HTTPInboundRequest is the normalized HTTP request shape for webhook-style channels.
type HTTPInboundRequest struct {
	Method string
	Header http.Header
	Query  map[string][]string
	Body   []byte
}

// HTTPInboundResponse lets adapters control webhook responses when necessary.
type HTTPInboundResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// HTTPInboundAdapter is implemented by adapters that accept inbound HTTP webhooks.
type HTTPInboundAdapter interface {
	HandleHTTPInbound(ctx context.Context, req HTTPInboundRequest) (*HTTPInboundResponse, error)
}

// HTTPInboundError carries an HTTP status for webhook failures.
type HTTPInboundError struct {
	StatusCode int
	Message    string
}

// Error returns the webhook failure message carried by the adapter error.
func (e *HTTPInboundError) Error() string {
	return e.Message
}

// NewHTTPInboundError creates an adapter error with an explicit HTTP status.
func NewHTTPInboundError(statusCode int, format string, args ...any) error {
	return &HTTPInboundError{
		StatusCode: statusCode,
		Message:    fmt.Sprintf(format, args...),
	}
}

// Adapter is the channel.v1 interface that all channel integrations must implement.
type Adapter interface {
	// Connect establishes a connection to the channel service.
	Connect(ctx context.Context) error

	// Disconnect gracefully closes the channel connection.
	Disconnect(ctx context.Context) error

	// Send delivers an outbound message through the channel.
	Send(ctx context.Context, msg OutboundMessage) error

	// Capabilities returns what this adapter supports.
	Capabilities() ChannelCapabilityDescriptor

	// Status returns the current connection state.
	Status() Status

	// SubscribeEvents returns a channel that receives inbound messages.
	// The returned channel is closed when the adapter disconnects.
	SubscribeEvents() <-chan InboundMessage
}

// ---------------------------------------------------------------------------
// Optional capability interfaces
// ---------------------------------------------------------------------------

// MessageEditor is implemented by adapters that support editing sent messages.
type MessageEditor interface {
	EditMessage(ctx context.Context, channelID, messageID, newContent string) error
}

// SetupAdapter is implemented by adapters that need one-time setup with a
// loosely structured runtime configuration payload.
type SetupAdapter interface {
	Setup(ctx context.Context, config map[string]any) error
}

// HealthStatus summarizes the current operational health of a channel adapter.
type HealthStatus struct {
	Status    Status         `json:"status"`
	CheckedAt time.Time      `json:"checked_at,omitempty"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// HealthAdapter is implemented by adapters that can expose a richer health probe.
type HealthAdapter interface {
	Health(ctx context.Context) (HealthStatus, error)
}

// ChannelMetrics reports adapter-local counters that can be surfaced by
// operator views or health tooling.
type ChannelMetrics struct {
	SentMessages     uint64 `json:"sent_messages,omitempty"`
	ReceivedMessages uint64 `json:"received_messages,omitempty"`
	FailedMessages   uint64 `json:"failed_messages,omitempty"`
	DroppedEvents    uint64 `json:"dropped_events,omitempty"`
	LastError        string `json:"last_error,omitempty"`
}

// MetricsAdapter is implemented by adapters that expose lightweight counters.
type MetricsAdapter interface {
	Metrics() ChannelMetrics
}

// MessageDeleter is implemented by adapters that support deleting messages.
type MessageDeleter interface {
	DeleteMessage(ctx context.Context, channelID, messageID string) error
}

// MessageReactor is implemented by adapters that support message reactions.
type MessageReactor interface {
	AddReaction(ctx context.Context, channelID, messageID, emoji string) error
	RemoveReaction(ctx context.Context, channelID, messageID, emoji string) error
}

// StreamingRenderer is implemented by adapters that support progressive
// message updates while a run is still producing output. Adapters that do not
// implement this interface continue to receive only the final Send() call.
type StreamingRenderer interface {
	// BeginStreaming sends an initial placeholder message and returns an opaque
	// handle that the bridge passes back to UpdateStreaming and EndStreaming.
	BeginStreaming(ctx context.Context, msg OutboundMessage) (streamHandle string, err error)

	// UpdateStreaming replaces the content of the streaming message using the
	// accumulated content rendered so far.
	UpdateStreaming(ctx context.Context, streamHandle string, content string) error

	// EndStreaming finalizes the streaming message with the completed outbound
	// content. After this call the message becomes the delivered final result.
	EndStreaming(ctx context.Context, streamHandle string, final OutboundMessage) error
}

// HistoryReader is implemented by adapters that can read message history.
type HistoryReader interface {
	ReadHistory(ctx context.Context, channelID string, limit int, before string) ([]HistoryMessage, error)
}

// ActionExecutor is implemented by adapters that support custom channel actions.
type ActionExecutor interface {
	ExecuteAction(ctx context.Context, channelID string, action ChannelAction) (*ChannelActionResult, error)
}

// HistoryMessage represents a single message from channel history.
type HistoryMessage struct {
	ID        string         `json:"id"`
	ChannelID string         `json:"channel_id"`
	SenderID  string         `json:"sender_id"`
	Content   string         `json:"content"`
	Timestamp string         `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ChannelAction represents a custom action to execute on a channel.
type ChannelAction struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// ChannelActionResult holds the result of a custom channel action.
type ChannelActionResult struct {
	Success bool           `json:"success"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}
