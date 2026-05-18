package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/gorilla/websocket"
)

var log = logging.WithSubsystem("slack")

// Compile-time interface assertions.
var (
	_ channels.Adapter            = (*Adapter)(nil)
	_ channels.MessageEditor      = (*Adapter)(nil)
	_ channels.MessageDeleter     = (*Adapter)(nil)
	_ channels.MessageReactor     = (*Adapter)(nil)
	_ channels.HistoryReader      = (*Adapter)(nil)
	_ channels.StreamingRenderer  = (*Adapter)(nil)
	_ channels.CapabilityReporter = (*Adapter)(nil)
)

// Slack API endpoints.
const (
	slackAPIChatUpdate           = "https://slack.com/api/chat.update"
	slackAPIChatDelete           = "https://slack.com/api/chat.delete"
	slackAPIReactionsAdd         = "https://slack.com/api/reactions.add"
	slackAPIReactionsRemove      = "https://slack.com/api/reactions.remove"
	slackAPIConversationsHistory = "https://slack.com/api/conversations.history"
)

// Config holds the tokens required for Slack Socket Mode.
type Config struct {
	BotToken string `json:"bot_token" yaml:"bot_token"` // xoxb-...
	AppToken string `json:"app_token" yaml:"app_token"` // xapp-...
}

// Adapter implements channels.Adapter for Slack using Socket Mode.
type Adapter struct {
	config Config

	base       channels.BaseAdapter
	httpClient *http.Client
	mu         sync.Mutex
	conn       *websocket.Conn
}

// New creates a new Slack adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config:     cfg,
		base:       channels.NewBaseAdapter("slack"),
		httpClient: http.DefaultClient,
	}
}

// connectionsOpenResponse is the response from apps.connections.open.
type connectionsOpenResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// socketEnvelope is the top-level JSON frame received over the WebSocket.
type socketEnvelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
}

// envelopePayload contains the event wrapper inside the envelope payload.
type envelopePayload struct {
	Event messageEvent `json:"event"`
}

// messageEvent represents a Slack message event.
type messageEvent struct {
	Type     string `json:"type"`
	SubType  string `json:"subtype"`
	Text     string `json:"text"`
	Channel  string `json:"channel"`
	User     string `json:"user"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

// envelopeAck is sent back to acknowledge receipt of an envelope.
type envelopeAck struct {
	EnvelopeID string `json:"envelope_id"`
}

// Connect establishes a Socket Mode connection to Slack.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.config.BotToken == "" || a.config.AppToken == "" {
		return fmt.Errorf("slack: bot_token and app_token are required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	wsURL, err := a.openConnection(ctx)
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("slack: connections.open: %w", err)
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("slack: websocket dial: %w", err)
	}
	outbound := channels.NewOutboundSerializer()

	wsCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		conn.Close()
		return nil
	}
	a.conn = conn

	go a.readLoop(wsCtx, conn, outbound)

	log.Info("slack: adapter connected")
	return nil
}

// openConnection calls apps.connections.open to obtain the WebSocket URL.
func (a *Adapter) openConnection(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/apps.connections.open", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.config.AppToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result connectionsOpenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("api error: %s", result.Error)
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty websocket url in response")
	}
	return result.URL, nil
}

// readLoop reads messages from the WebSocket connection and dispatches them.
func (a *Adapter) readLoop(ctx context.Context, conn *websocket.Conn, outbound *channels.OutboundSerializer) {
	defer func() {
		a.clearConnIfMatch(conn)
		if a.base.Status() == channels.StatusConnected {
			a.base.SetStatus(channels.StatusDisconnected)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("slack: websocket read error", "error", err)
			a.base.SetStatus(channels.StatusError)
			return
		}

		var envelope socketEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			log.Warn("slack: failed to parse envelope", "error", err)
			continue
		}

		// Acknowledge every envelope that has an ID.
		if envelope.EnvelopeID != "" {
			a.ackEnvelope(conn, outbound, envelope.EnvelopeID)
		}

		if envelope.Type == "events_api" {
			a.handleEventsAPI(envelope.Payload)
		}
	}
}

// ackEnvelope sends an acknowledgement for the given envelope ID.
func (a *Adapter) ackEnvelope(conn *websocket.Conn, outbound *channels.OutboundSerializer, envelopeID string) {
	ack := envelopeAck{EnvelopeID: envelopeID}
	data, err := json.Marshal(ack)
	if err != nil {
		log.Error("slack: marshal ack", "error", err)
		return
	}
	if conn == nil {
		return
	}
	if err := outbound.Do(func() error {
		return conn.WriteMessage(websocket.TextMessage, data)
	}); err != nil {
		log.Error("slack: write ack", "error", err, "envelope_id", envelopeID)
	}
}

// handleEventsAPI processes an events_api envelope payload.
func (a *Adapter) handleEventsAPI(raw json.RawMessage) {
	var payload envelopePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Warn("slack: failed to parse event payload", "error", err)
		return
	}

	event := payload.Event
	// Only handle plain messages (no subtype means a user-posted message).
	if event.Type != "message" || event.SubType != "" {
		return
	}
	if strings.TrimSpace(event.Text) == "" {
		return
	}

	rawEvent := map[string]any{
		"channel": event.Channel,
		"ts":      event.TS,
		"user":    event.User,
	}
	if event.ThreadTS != "" {
		rawEvent["thread_ts"] = event.ThreadTS
	}

	inbound := channels.InboundMessage{
		ChannelID:  "slack",
		SenderID:   event.User,
		SenderName: "",
		Content:    event.Text,
		RawEvent:   rawEvent,
	}

	log.Info("slack: received message",
		"user", event.User,
		"channel", event.Channel,
		"content_length", len(event.Text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("slack: subscriber channel full, dropping message")
	})
}

// Disconnect gracefully closes the Slack WebSocket connection.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn = nil
	}
	log.Info("slack: adapter disconnected")
	return nil
}

// Send posts a message to Slack using chat.postMessage.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("slack: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("slack: target_id (channel) is required")
	}
	_, err := a.sendMessage(ctx, msg)
	return err
}

// buildSlackBlocks converts OutboundBlocks and OutboundAttachments into Slack
// Block Kit structures. Each block becomes a section with optional header;
// attachments become context blocks with link references.
func buildSlackBlocks(blocks []channels.OutboundBlock, attachments []channels.OutboundAttachment) []map[string]any {
	var out []map[string]any

	for _, b := range blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		title := strings.TrimSpace(b.Title)
		if title != "" {
			out = append(out, map[string]any{
				"type": "header",
				"text": map[string]any{
					"type": "plain_text",
					"text": title,
				},
			})
		}
		out = append(out, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": truncateSlackText(content, slackSectionMaxChars),
			},
		})
	}

	if len(attachments) > 0 && len(out) < slackMaxBlockCount {
		if len(out) > 0 {
			out = append(out, map[string]any{"type": "divider"})
		}
		for _, att := range attachments {
			if len(out) >= slackMaxBlockCount {
				break
			}
			uri := strings.TrimSpace(att.URI)
			if uri == "" {
				continue
			}
			label := strings.TrimSpace(att.Label)
			if label == "" {
				label = uri
			}
			out = append(out, map[string]any{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("<%s|%s>", uri, label),
				},
			})
		}
	}

	if len(out) > slackMaxBlockCount {
		out = out[:slackMaxBlockCount]
	}
	return out
}

// slackSectionMaxChars is the maximum text length in a single Slack section block.
const slackSectionMaxChars = 3000

// slackMaxBlockCount is the maximum number of blocks per Slack message.
const slackMaxBlockCount = 50

func truncateSlackText(text string, limit int) string {
	return channels.TruncateUTF8(text, limit)
}

// Capabilities returns the capabilities supported by the Slack adapter.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   false,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
		Interactive:    true,
		InlineDelivery: true,
	}
}

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		Threading:      true,
		Reactions:      true,
		History:        true,
		EditMessage:    true,
		DeleteMessage:  true,
		PolicyControls: true,
		Dedupe:         true,
		ThreadBinding:  true,
	}
}

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns a channel that receives inbound messages.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

func (a *Adapter) clearConnIfMatch(conn *websocket.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == conn {
		a.conn = nil
	}
}

// ---------------------------------------------------------------------------
// Slack API helpers
// ---------------------------------------------------------------------------

// slackAPIResponse is the common envelope for Slack API responses.
type slackAPIResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// slackAPIPost posts a JSON payload to a Slack API endpoint using BotToken
// auth and returns an error if the request fails or the API returns ok=false.
func (a *Adapter) slackAPIPost(ctx context.Context, endpoint string, payload any) error {
	_, err := a.slackAPIPostResult(ctx, endpoint, payload)
	return err
}

// slackAPIPostResult posts a JSON payload to a Slack API endpoint and returns
// the raw response body on success. Automatically retries on HTTP 429.
func (a *Adapter) slackAPIPostResult(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("slack: marshal request: %w", err)
	}

	for attempt := 0; attempt <= channels.RateLimitMaxRetries(); attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("slack: create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+a.config.BotToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("slack: api call: %w", err)
		}

		if rlErr := channels.CheckRateLimit(resp); rlErr != nil {
			resp.Body.Close()
			log.Warn("slack: rate limited", "retry_after", rlErr.RetryAfter, "attempt", attempt)
			if waitErr := channels.WaitForRateLimit(ctx, rlErr); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("slack: read response: %w", err)
		}

		var result slackAPIResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("slack: decode response: %w", err)
		}
		if !result.OK {
			return nil, fmt.Errorf("slack: %s: %s", endpoint, result.Error)
		}
		return respBody, nil
	}
	return nil, fmt.Errorf("slack: %s: rate limit retries exhausted", endpoint)
}

// ---------------------------------------------------------------------------
// Optional capability interfaces
// ---------------------------------------------------------------------------

// EditMessage updates the text of an existing Slack message.
func (a *Adapter) EditMessage(ctx context.Context, channelID, messageID, newContent string) error {
	payload := struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Text    string `json:"text"`
	}{
		Channel: channelID,
		TS:      messageID,
		Text:    newContent,
	}
	return a.slackAPIPost(ctx, slackAPIChatUpdate, payload)
}

func (a *Adapter) BeginStreaming(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	return a.sendMessage(ctx, channels.OutboundMessage{
		ChannelID: msg.ChannelID,
		TargetID:  msg.TargetID,
		ReplyToID: msg.ReplyToID,
		Content:   "⏳ Thinking...",
		Format:    "text",
		Metadata:  msg.Metadata,
	})
}

func (a *Adapter) UpdateStreaming(ctx context.Context, streamHandle string, content string) error {
	channelID, messageID, err := parseSlackStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	return a.EditMessage(ctx, channelID, messageID, content)
}

func (a *Adapter) EndStreaming(ctx context.Context, streamHandle string, final channels.OutboundMessage) error {
	channelID, messageID, err := parseSlackStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	return a.updateMessage(ctx, channelID, messageID, final)
}

// DeleteMessage deletes an existing Slack message.
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	payload := struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}{
		Channel: channelID,
		TS:      messageID,
	}
	return a.slackAPIPost(ctx, slackAPIChatDelete, payload)
}

// AddReaction adds an emoji reaction to a Slack message.
func (a *Adapter) AddReaction(ctx context.Context, channelID, messageID, emoji string) error {
	payload := struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		Name      string `json:"name"`
	}{
		Channel:   channelID,
		Timestamp: messageID,
		Name:      emoji,
	}
	return a.slackAPIPost(ctx, slackAPIReactionsAdd, payload)
}

// RemoveReaction removes an emoji reaction from a Slack message.
func (a *Adapter) RemoveReaction(ctx context.Context, channelID, messageID, emoji string) error {
	payload := struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		Name      string `json:"name"`
	}{
		Channel:   channelID,
		Timestamp: messageID,
		Name:      emoji,
	}
	return a.slackAPIPost(ctx, slackAPIReactionsRemove, payload)
}

// historyRequest is the request body for conversations.history.
type historyRequest struct {
	Channel string `json:"channel"`
	Limit   int    `json:"limit"`
	Latest  string `json:"latest,omitempty"`
}

// historyResponse is the response from conversations.history.
type historyResponse struct {
	OK       bool             `json:"ok"`
	Error    string           `json:"error,omitempty"`
	Messages []historyMessage `json:"messages"`
}

// historyMessage represents a single message in the conversations.history response.
type historyMessage struct {
	TS   string `json:"ts"`
	User string `json:"user"`
	Text string `json:"text"`
}

// ReadHistory retrieves recent messages from a Slack channel.
func (a *Adapter) ReadHistory(ctx context.Context, channelID string, limit int, before string) ([]channels.HistoryMessage, error) {
	reqBody := historyRequest{
		Channel: channelID,
		Limit:   limit,
		Latest:  before,
	}

	respBody, err := a.slackAPIPostResult(ctx, slackAPIConversationsHistory, reqBody)
	if err != nil {
		return nil, err
	}

	var histResp historyResponse
	if err := json.Unmarshal(respBody, &histResp); err != nil {
		return nil, fmt.Errorf("slack: decode history response: %w", err)
	}

	messages := make([]channels.HistoryMessage, 0, len(histResp.Messages))
	for i, m := range histResp.Messages {
		messages = append(messages, channels.HistoryMessage{
			ID:        m.TS,
			ChannelID: channelID,
			SenderID:  m.User,
			Content:   m.Text,
			Timestamp: m.TS,
			Metadata: map[string]any{
				"index": strconv.Itoa(i),
			},
		})
	}
	return messages, nil
}

func (a *Adapter) sendMessage(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	respBody, err := a.slackAPIPostResult(ctx, "https://slack.com/api/chat.postMessage", slackMessagePayload(msg))
	if err != nil {
		return "", err
	}

	var result struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("slack: decode chat.postMessage response: %w", err)
	}
	if strings.TrimSpace(result.Channel) == "" || strings.TrimSpace(result.TS) == "" {
		return "", fmt.Errorf("slack: chat.postMessage missing message handle")
	}
	return encodeSlackStreamHandle(result.Channel, result.TS), nil
}

func (a *Adapter) updateMessage(ctx context.Context, channelID, messageID string, msg channels.OutboundMessage) error {
	payload := slackMessagePayload(msg)
	payload["channel"] = channelID
	payload["ts"] = messageID
	return a.slackAPIPost(ctx, slackAPIChatUpdate, payload)
}

func slackMessagePayload(msg channels.OutboundMessage) map[string]any {
	payload := map[string]any{
		"channel": msg.TargetID,
		"text":    msg.Content,
	}
	if strings.TrimSpace(msg.ReplyToID) != "" {
		payload["thread_ts"] = msg.ReplyToID
	}
	if len(msg.Blocks) > 0 || len(msg.Attachments) > 0 {
		if slackBlocks := buildSlackBlocks(msg.Blocks, msg.Attachments); len(slackBlocks) > 0 {
			payload["blocks"] = slackBlocks
		}
	}
	return payload
}

func encodeSlackStreamHandle(channelID, messageID string) string {
	return strings.TrimSpace(channelID) + "|" + strings.TrimSpace(messageID)
}

func parseSlackStreamHandle(handle string) (channelID string, messageID string, err error) {
	parts := strings.SplitN(strings.TrimSpace(handle), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("slack: invalid stream handle")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
