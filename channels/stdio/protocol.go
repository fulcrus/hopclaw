// Package stdio implements a channel adapter that communicates with an
// external plugin process over JSON-RPC 2.0 / NDJSON on stdio. This lets
// third-party developers write channel plugins in any language — they only
// need to read/write JSON lines on stdin/stdout.
package stdio

import (
	"encoding/json"

	"github.com/fulcrus/hopclaw/channels"
)

// ---------------------------------------------------------------------------
// Protocol version
// ---------------------------------------------------------------------------

const (
	// ProtocolVersion is the channel plugin protocol version.
	ProtocolVersion = "2025-03-15"

	// JSONRPC is the JSON-RPC version string included in every message.
	JSONRPC = "2.0"
)

// ---------------------------------------------------------------------------
// Method names — HopClaw → Plugin (requests)
// ---------------------------------------------------------------------------

const (
	// MethodInitialize performs the handshake and exchanges capabilities.
	MethodInitialize = "initialize"

	// MethodConnect tells the plugin to establish its platform connection.
	MethodConnect = "connect"

	// MethodDisconnect tells the plugin to gracefully close its connection.
	MethodDisconnect = "disconnect"

	// MethodSend delivers an outbound message through the plugin channel.
	MethodSend = "send"
)

// ---------------------------------------------------------------------------
// Method names — Plugin → HopClaw (notifications, no ID)
// ---------------------------------------------------------------------------

const (
	// MethodChannelInbound is sent by the plugin when it receives a message
	// from the external platform.
	MethodChannelInbound = "channel/inbound"

	// MethodChannelStatus is sent by the plugin to report connection state
	// changes (connected, disconnected, error).
	MethodChannelStatus = "channel/status"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 message envelope
// ---------------------------------------------------------------------------

// Message represents a JSON-RPC 2.0 request, response, or notification.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// IsResponse returns true when the message is a response (has ID, no Method).
func (m *Message) IsResponse() bool {
	return m.ID != nil && m.Method == ""
}

// IsNotification returns true when the message has a Method but no ID.
func (m *Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// ---------------------------------------------------------------------------
// Initialize
// ---------------------------------------------------------------------------

// InitializeParams is sent by HopClaw to start the handshake.
type InitializeParams struct {
	ProtocolVersion string `json:"protocol_version"`
	HostName        string `json:"host_name"`
	HostVersion     string `json:"host_version"`
}

// InitializeResult is returned by the plugin to complete the handshake.
type InitializeResult struct {
	ProtocolVersion string           `json:"protocol_version"`
	PluginName      string           `json:"plugin_name"`
	PluginVersion   string           `json:"plugin_version"`
	Capabilities    PluginCapability `json:"capabilities"`
}

// PluginCapability describes what the plugin channel supports.
type PluginCapability struct {
	SendText         bool `json:"send_text"`
	SendRichText     bool `json:"send_rich_text"`
	SendFile         bool `json:"send_file"`
	Edit             bool `json:"edit"`
	Delete           bool `json:"delete"`
	React            bool `json:"react"`
	History          bool `json:"history"`
	Threading        bool `json:"threading"`
	TypingIndicator  bool `json:"typing_indicator"`
	RichCards        bool `json:"rich_cards"`
	StreamingUpdates bool `json:"streaming_updates"`
	MultiAccount     bool `json:"multi_account"`
	WebhookInbound   bool `json:"webhook_inbound"`
	PolicyControls   bool `json:"policy_controls"`
	Dedupe           bool `json:"dedupe"`
	Pairing          bool `json:"pairing"`
	ThreadBinding    bool `json:"thread_binding"`
	Interactive      bool `json:"interactive"`
	Mobile           bool `json:"mobile"`
	InlineDelivery   bool `json:"inline_delivery"`
}

// ToChannelCapabilities converts to the channels.Capabilities type.
func (c PluginCapability) ToChannelCapabilities() channels.Capabilities {
	return channels.Capabilities{
		SendText:         c.SendText,
		SendRichText:     c.SendRichText,
		SendFile:         c.SendFile,
		ReceiveMessage:   true, // stdio plugins always receive
		ReceiveEvent:     true,
		Threading:        c.Threading,
		TypingIndicator:  c.TypingIndicator,
		RichCards:        c.RichCards,
		StreamingUpdates: c.StreamingUpdates,
		MultiAccount:     c.MultiAccount,
		WebhookInbound:   c.WebhookInbound,
		PolicyControls:   c.PolicyControls,
		Dedupe:           c.Dedupe,
		Pairing:          c.Pairing,
		ThreadBinding:    c.ThreadBinding,
		Interactive:      c.Interactive,
		Mobile:           c.Mobile,
		InlineDelivery:   c.InlineDelivery,
	}
}

// ---------------------------------------------------------------------------
// Connect / Disconnect
// ---------------------------------------------------------------------------

// ConnectParams is sent by HopClaw to tell the plugin to connect.
type ConnectParams struct {
	Config map[string]any `json:"config,omitempty"`
}

// ConnectResult is returned when the plugin has connected.
type ConnectResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// DisconnectResult is returned when the plugin has disconnected.
type DisconnectResult struct {
	OK bool `json:"ok"`
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

// SendParams wraps an outbound message for delivery.
type SendParams struct {
	ChannelID string         `json:"channel_id"`
	TargetID  string         `json:"target_id"`
	ReplyToID string         `json:"reply_to_id,omitempty"`
	Content   string         `json:"content"`
	Format    string         `json:"format,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SendResult is the plugin's acknowledgement of a sent message.
type SendResult struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Inbound notification (Plugin → HopClaw)
// ---------------------------------------------------------------------------

// InboundNotification is sent by the plugin when a message arrives.
type InboundNotification struct {
	ChannelID  string         `json:"channel_id"`
	SenderID   string         `json:"sender_id"`
	SenderName string         `json:"sender_name,omitempty"`
	Content    string         `json:"content"`
	RawEvent   map[string]any `json:"raw_event,omitempty"`
}

// ToInboundMessage converts to the channels.InboundMessage type.
func (n InboundNotification) ToInboundMessage() channels.InboundMessage {
	return channels.InboundMessage{
		ChannelID:  n.ChannelID,
		SenderID:   n.SenderID,
		SenderName: n.SenderName,
		Content:    n.Content,
		RawEvent:   n.RawEvent,
	}
}

// ---------------------------------------------------------------------------
// Status notification (Plugin → HopClaw)
// ---------------------------------------------------------------------------

// StatusNotification is sent by the plugin to report state changes.
type StatusNotification struct {
	Status  string `json:"status"` // "connected", "disconnected", "error"
	Message string `json:"message,omitempty"`
}

// ---------------------------------------------------------------------------
// JSON-RPC error codes
// ---------------------------------------------------------------------------

const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)
