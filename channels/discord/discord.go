package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"

	"github.com/gorilla/websocket"
)

var log = logging.WithSubsystem("discord")

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

// Discord REST API base URL.
const discordAPIBase = "https://discord.com/api/v10"

const (
	discordReconnectInitialBackoff = 2 * time.Second
	discordReconnectMaxBackoff     = 30 * time.Second
)

// Discord Gateway opcodes.
const (
	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opHeartbeatACK = 11
	opHello        = 10
)

// Discord intent flags.
const (
	intentGuildMessages  = 1 << 9
	intentDirectMessages = 1 << 12
	intentMessageContent = 1 << 15
)

// Config holds the configuration for the Discord adapter.
type Config struct {
	BotToken string `json:"bot_token" yaml:"bot_token"`
}

// gatewayMessage is the envelope for all Discord Gateway messages.
type gatewayMessage struct {
	Op       int             `json:"op"`
	Data     json.RawMessage `json:"d,omitempty"`
	Sequence *int64          `json:"s,omitempty"`
	Type     string          `json:"t,omitempty"`
}

// helloData is the payload of a Gateway Hello (op 10).
type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"` // milliseconds
}

// messageCreateEvent is a subset of the MESSAGE_CREATE dispatch payload.
type messageCreateEvent struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id,omitempty"`
	Content   string `json:"content"`
	Author    struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
}

// Adapter implements channels.Adapter for the Discord Gateway.
type Adapter struct {
	config Config

	base        channels.BaseAdapter
	httpClient  *http.Client
	mu          sync.Mutex
	conn        *websocket.Conn
	connSession *gatewaySession
	dialGateway func(context.Context, string) (*websocket.Conn, *http.Response, error)

	sequence atomic.Int64 // last received sequence number; -1 = none

	reconnectInitialBackoff time.Duration
	reconnectMaxBackoff     time.Duration
}

// New creates a new Discord adapter with the given configuration.
func New(cfg Config) *Adapter {
	a := &Adapter{
		config:     cfg,
		base:       channels.NewBaseAdapter("discord"),
		httpClient: http.DefaultClient,
		dialGateway: func(ctx context.Context, rawURL string) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, rawURL, nil)
		},
		reconnectInitialBackoff: discordReconnectInitialBackoff,
		reconnectMaxBackoff:     discordReconnectMaxBackoff,
	}
	a.sequence.Store(-1)
	return a
}

// Connect establishes a connection to the Discord Gateway.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.config.BotToken == "" {
		return fmt.Errorf("discord: bot_token is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}
	a.base.SetStatus(channels.StatusConnecting)

	session, err := a.openGatewaySession(ctx)
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return err
	}

	wsCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		session.close()
		return nil
	}
	a.conn = session.conn
	a.connSession = session
	go a.connectionLoop(wsCtx, session)

	log.Info("discord: adapter connected")
	return nil
}

// Disconnect gracefully closes the Discord Gateway connection.
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
	if a.connSession != nil {
		a.connSession.close()
		a.connSession = nil
		a.conn = nil
	}
	log.Info("discord: adapter disconnected")
	return nil
}

type gatewaySession struct {
	conn              *websocket.Conn
	heartbeatInterval time.Duration
	outbound          *channels.OutboundSerializer
	closed            atomic.Bool
}

func (s *gatewaySession) writeJSON(v any) error {
	if s == nil || s.conn == nil || s.closed.Load() {
		return nil
	}
	return s.outbound.Do(func() error {
		if s.closed.Load() {
			return nil
		}
		return s.conn.WriteJSON(v)
	})
}

func (s *gatewaySession) writeMessage(messageType int, data []byte) error {
	if s == nil || s.conn == nil || s.closed.Load() {
		return nil
	}
	return s.outbound.Do(func() error {
		if s.closed.Load() {
			return nil
		}
		return s.conn.WriteMessage(messageType, data)
	})
}

func (s *gatewaySession) close() {
	if s == nil || s.conn == nil {
		return
	}
	_ = s.outbound.Do(func() error {
		if s.closed.Swap(true) {
			return nil
		}
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		return s.conn.Close()
	})
}

// Maximum text length per Discord message.
const discordMaxMessageLen = 2000

// Maximum embed description length per Discord embed.
const discordEmbedMaxChars = 4096

// Send delivers a message to a Discord channel via the REST API.
// Long messages are automatically split into chunks of up to 2000 characters.
// When blocks are present, they are rendered as a Discord embed with fields.
// Attachments are added as embed fields with URLs.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("discord: adapter is not connected")
	}
	token := a.config.BotToken

	channelID := msg.TargetID
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("discord: target_id (channel_id) is required")
	}

	// Use embed when blocks are present and non-empty.
	if len(msg.Blocks) > 0 || len(msg.Attachments) > 0 {
		embed := buildDiscordEmbed(msg.Blocks, msg.Attachments)
		if hasEmbedContent(embed) {
			body := map[string]any{"embeds": []map[string]any{embed}}
			if strings.TrimSpace(msg.ReplyToID) != "" {
				body["message_reference"] = map[string]string{
					"message_id": msg.ReplyToID,
				}
			}
			payload, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("discord: marshal embed body: %w", err)
			}
			reqURL := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID)
			return a.doSendWithRetry(ctx, reqURL, payload, token)
		}
	}

	// Fallback to plain text chunks.
	chunks := channels.ChunkText(msg.Content, discordMaxMessageLen)
	for i, chunk := range chunks {
		body := map[string]any{
			"content": chunk,
		}
		if i == 0 && strings.TrimSpace(msg.ReplyToID) != "" {
			body["message_reference"] = map[string]string{
				"message_id": msg.ReplyToID,
			}
		}

		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("discord: marshal send body: %w", err)
		}

		reqURL := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID)
		if err := a.doSendWithRetry(ctx, reqURL, payload, token); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) BeginStreaming(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	return a.createMessage(ctx, channels.OutboundMessage{
		ChannelID: msg.ChannelID,
		TargetID:  msg.TargetID,
		ReplyToID: msg.ReplyToID,
		Content:   "⏳ Thinking...",
		Format:    "text",
		Metadata:  msg.Metadata,
	})
}

func (a *Adapter) UpdateStreaming(ctx context.Context, streamHandle string, content string) error {
	channelID, messageID, err := parseDiscordStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	return a.EditMessage(ctx, channelID, messageID, content)
}

func (a *Adapter) EndStreaming(ctx context.Context, streamHandle string, final channels.OutboundMessage) error {
	channelID, messageID, err := parseDiscordStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	return a.updateMessage(ctx, channelID, messageID, final)
}

// buildDiscordEmbed converts OutboundBlocks and OutboundAttachments into a
// Discord embed object. The first block becomes the description; subsequent
// blocks become embed fields. Attachments are listed as linked fields.
// discordEmbedMaxFields is the maximum number of fields per Discord embed.
const discordEmbedMaxFields = 25

// discordFieldMaxChars is the maximum text length per embed field value.
const discordFieldMaxChars = 1024

func buildDiscordEmbed(blocks []channels.OutboundBlock, attachments []channels.OutboundAttachment) map[string]any {
	embed := map[string]any{
		"color": 0x5865F2, // Discord blurple
	}

	var fields []map[string]any
	for i, b := range blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		title := strings.TrimSpace(b.Title)
		if i == 0 && title != "" {
			embed["title"] = title
			embed["description"] = channels.TruncateUTF8(content, discordEmbedMaxChars)
			continue
		}
		if len(fields) >= discordEmbedMaxFields {
			break
		}
		if title == "" {
			title = "Details"
		}
		fields = append(fields, map[string]any{
			"name":  title,
			"value": channels.TruncateUTF8(content, discordFieldMaxChars),
		})
	}

	for _, att := range attachments {
		if len(fields) >= discordEmbedMaxFields {
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
		fields = append(fields, map[string]any{
			"name":   "Attachment",
			"value":  fmt.Sprintf("[%s](%s)", label, uri),
			"inline": true,
		})
	}

	if len(fields) > 0 {
		embed["fields"] = fields
	}
	return embed
}

// hasEmbedContent returns true if the embed has at least one displayable field
// required by the Discord API.
func hasEmbedContent(embed map[string]any) bool {
	for _, key := range []string{"title", "description", "fields"} {
		if v, ok := embed[key]; ok && v != nil {
			return true
		}
	}
	return false
}

func buildDiscordMessagePayload(msg channels.OutboundMessage) map[string]any {
	if len(msg.Blocks) > 0 || len(msg.Attachments) > 0 {
		if embed := buildDiscordEmbed(msg.Blocks, msg.Attachments); hasEmbedContent(embed) {
			body := map[string]any{"embeds": []map[string]any{embed}}
			if strings.TrimSpace(msg.ReplyToID) != "" {
				body["message_reference"] = map[string]string{
					"message_id": msg.ReplyToID,
				}
			}
			return body
		}
	}

	body := map[string]any{
		"content": channels.TruncateUTF8(msg.Content, discordMaxMessageLen),
	}
	if strings.TrimSpace(msg.ReplyToID) != "" {
		body["message_reference"] = map[string]string{
			"message_id": msg.ReplyToID,
		}
	}
	return body
}

// doSendWithRetry sends a POST request with rate-limit retry handling.
// It recreates the HTTP request on each attempt to avoid body-reuse issues.
func (a *Adapter) doSendWithRetry(ctx context.Context, reqURL string, payload []byte, token string) error {
	for attempt := 0; attempt <= channels.RateLimitMaxRetries(); attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("discord: create request: %w", err)
		}
		req.Header.Set("Authorization", "Bot "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("discord: send message: %w", err)
		}

		if rlErr := channels.CheckRateLimit(resp); rlErr != nil {
			resp.Body.Close()
			log.Warn("discord: rate limited", "retry_after", rlErr.RetryAfter, "attempt", attempt+1)
			if attempt >= channels.RateLimitMaxRetries() {
				return rlErr
			}
			if waitErr := channels.WaitForRateLimit(ctx, rlErr); waitErr != nil {
				return waitErr
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("discord: send message: status %d body %s", resp.StatusCode, string(respBody))
		}
		resp.Body.Close()
		return nil
	}
	return fmt.Errorf("discord: send message: rate limit retries exhausted")
}

// Capabilities returns what the Discord adapter supports.
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
		RichCards:      true,
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

// fetchGatewayURL calls GET /gateway to obtain the WebSocket URL.
func (a *Adapter) fetchGatewayURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/v10/gateway", nil)
	if err != nil {
		return "", err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty gateway url in response")
	}
	return result.URL, nil
}

func (a *Adapter) openGatewaySession(ctx context.Context) (*gatewaySession, error) {
	a.sequence.Store(-1)

	gatewayURL, err := a.fetchGatewayURL(ctx)
	if err != nil {
		return nil, fmt.Errorf("discord: fetch gateway url: %w", err)
	}

	wsURL := gatewayURL + "?v=10&encoding=json"
	dial := a.dialGateway
	if dial == nil {
		dial = func(ctx context.Context, rawURL string) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, rawURL, nil)
		}
	}
	conn, _, err := dial(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("discord: websocket dial: %w", err)
	}

	hello, err := a.readGatewayMessage(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("discord: read hello: %w", err)
	}
	if hello.Op != opHello {
		_ = conn.Close()
		return nil, fmt.Errorf("discord: expected op %d (Hello), got op %d", opHello, hello.Op)
	}

	var hd helloData
	if err := json.Unmarshal(hello.Data, &hd); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("discord: parse hello data: %w", err)
	}
	session := &gatewaySession{
		conn:              conn,
		heartbeatInterval: time.Duration(hd.HeartbeatInterval) * time.Millisecond,
		outbound:          channels.NewOutboundSerializer(),
	}
	if err := a.sendIdentify(session); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("discord: send identify: %w", err)
	}

	return session, nil
}

// readGatewayMessage reads a single JSON message from the WebSocket.
func (a *Adapter) readGatewayMessage(conn *websocket.Conn) (*gatewayMessage, error) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var msg gatewayMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if msg.Sequence != nil {
		a.sequence.Store(*msg.Sequence)
	}
	return &msg, nil
}

// sendIdentify sends an Identify (op 2) payload with the bot token and intents.
func (a *Adapter) sendIdentify(session *gatewaySession) error {
	identify := map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   a.config.BotToken,
			"intents": intentGuildMessages | intentDirectMessages | intentMessageContent,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "HopClaw",
				"device":  "HopClaw",
			},
		},
	}
	return session.writeJSON(identify)
}

// heartbeatLoop sends periodic heartbeats (op 1) to keep the connection alive.
func (a *Adapter) heartbeatLoop(ctx context.Context, session *gatewaySession, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			seq := a.sequence.Load()
			var payload any
			if seq >= 0 {
				payload = seq
			}
			msg := map[string]any{
				"op": opHeartbeat,
				"d":  payload,
			}

			if session == nil || session.conn == nil {
				return nil
			}
			if err := session.writeJSON(msg); err != nil {
				return fmt.Errorf("discord: heartbeat send failed: %w", err)
			}
		}
	}
}

// readerLoop reads dispatched events from the Gateway WebSocket.
func (a *Adapter) readerLoop(ctx context.Context, session *gatewaySession) error {
	if session == nil || session.conn == nil {
		return nil
	}
	conn := session.conn
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("discord: read error: %w", err)
		}

		var msg gatewayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Warn("discord: unmarshal gateway message", "error", err)
			continue
		}
		if msg.Sequence != nil {
			a.sequence.Store(*msg.Sequence)
		}

		switch msg.Op {
		case opDispatch:
			a.handleDispatch(msg)
		case opHeartbeatACK:
			// No action needed.
		case opHeartbeat:
			// Server requested an immediate heartbeat.
			seq := a.sequence.Load()
			var payload any
			if seq >= 0 {
				payload = seq
			}
			hb := map[string]any{"op": opHeartbeat, "d": payload}
			if err := session.writeJSON(hb); err != nil {
				return fmt.Errorf("discord: immediate heartbeat send failed: %w", err)
			}
		}
	}
}

// handleDispatch processes Dispatch (op 0) events based on the event type.
func (a *Adapter) handleDispatch(msg gatewayMessage) {
	switch msg.Type {
	case "MESSAGE_CREATE":
		a.handleMessageCreate(msg.Data)
	}
}

// discordHistoryMessage is a subset of the Discord message object returned by
// the GET /channels/{id}/messages endpoint.
type discordHistoryMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Author  struct {
		ID string `json:"id"`
	} `json:"author"`
	Timestamp string `json:"timestamp"`
}

// handleMessageCreate processes a MESSAGE_CREATE event.
func (a *Adapter) handleMessageCreate(data json.RawMessage) {
	var evt messageCreateEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		log.Warn("discord: unmarshal MESSAGE_CREATE", "error", err)
		return
	}

	// Ignore messages from bots.
	if evt.Author.Bot {
		return
	}

	content := strings.TrimSpace(evt.Content)
	if content == "" {
		return
	}

	inbound := channels.InboundMessage{
		ChannelID:  "discord",
		SenderID:   evt.Author.ID,
		SenderName: evt.Author.Username,
		Content:    content,
		RawEvent: map[string]any{
			"channel_id": evt.ChannelID,
			"message_id": evt.ID,
			"guild_id":   evt.GuildID,
		},
	}

	log.Info("discord: received message",
		"sender", evt.Author.ID,
		"channel_id", evt.ChannelID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("discord: subscriber channel full, dropping message")
	})
}

func (a *Adapter) clearConnIfMatch(conn *websocket.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == conn {
		a.conn = nil
		a.connSession = nil
	}
}

func (a *Adapter) setConn(session *gatewaySession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if session == nil {
		a.conn = nil
		a.connSession = nil
		return
	}
	a.conn = session.conn
	a.connSession = session
}

func (a *Adapter) connectionLoop(ctx context.Context, session *gatewaySession) {
	backoff := time.Duration(0)
	current := session

	for {
		if ctx.Err() != nil {
			if current != nil {
				current.close()
				a.clearConnIfMatch(current.conn)
			}
			return
		}
		if current == nil {
			a.base.SetStatus(channels.StatusConnecting)
			reconnected, err := a.openGatewaySession(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				a.base.SetStatus(channels.StatusError)
				log.Error("discord: reconnect failed", "error", err)
				backoff = nextDiscordReconnectBackoff(backoff, a.reconnectInitialBackoff, a.reconnectMaxBackoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				continue
			}
			current = reconnected
			a.setConn(reconnected)
			backoff = 0
		}

		a.base.SetStatus(channels.StatusConnected)
		err := a.runGatewaySession(ctx, current)
		current.close()
		a.clearConnIfMatch(current.conn)
		current = nil
		if ctx.Err() != nil || err == nil {
			return
		}

		a.base.SetStatus(channels.StatusError)
		log.Error("discord: gateway session ended; reconnecting", "error", err)
		backoff = nextDiscordReconnectBackoff(backoff, a.reconnectInitialBackoff, a.reconnectMaxBackoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (a *Adapter) runGatewaySession(ctx context.Context, session *gatewaySession) error {
	if session == nil || session.conn == nil {
		return nil
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- a.heartbeatLoop(sessionCtx, session, session.heartbeatInterval)
	}()
	go func() {
		errCh <- a.readerLoop(sessionCtx, session)
	}()

	err := <-errCh
	cancel()
	return err
}

func nextDiscordReconnectBackoff(current, initial, max time.Duration) time.Duration {
	if initial <= 0 {
		initial = discordReconnectInitialBackoff
	}
	if max <= 0 {
		max = discordReconnectMaxBackoff
	}
	if current <= 0 {
		return initial
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// discordAPIRequest performs an authenticated HTTP request to the Discord REST API.
// The caller is responsible for closing the response body.
func (a *Adapter) discordAPIRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("discord: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+a.config.BotToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: api request: %w", err)
	}
	return resp, nil
}

func (a *Adapter) createMessage(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	channelID := strings.TrimSpace(msg.TargetID)
	if channelID == "" {
		return "", fmt.Errorf("discord: target_id (channel_id) is required")
	}

	payload, err := json.Marshal(buildDiscordMessagePayload(msg))
	if err != nil {
		return "", fmt.Errorf("discord: marshal create message body: %w", err)
	}

	reqURL := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID)
	resp, err := a.discordAPIRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("discord: read create message response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("discord: create message: status %d body %s", resp.StatusCode, string(respBody))
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		return "", fmt.Errorf("discord: decode create message response: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return "", fmt.Errorf("discord: create message missing message id")
	}
	return encodeDiscordStreamHandle(channelID, created.ID), nil
}

func (a *Adapter) updateMessage(ctx context.Context, channelID, messageID string, msg channels.OutboundMessage) error {
	payload, err := json.Marshal(buildDiscordMessagePayload(channels.OutboundMessage{
		Content:     msg.Content,
		Blocks:      msg.Blocks,
		Attachments: msg.Attachments,
	}))
	if err != nil {
		return fmt.Errorf("discord: marshal update message body: %w", err)
	}

	reqURL := fmt.Sprintf("%s/channels/%s/messages/%s", discordAPIBase, channelID, messageID)
	resp, err := a.discordAPIRequest(ctx, http.MethodPatch, reqURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: update message: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Optional capability interfaces
// ---------------------------------------------------------------------------

// EditMessage edits the content of a previously sent message.
func (a *Adapter) EditMessage(ctx context.Context, channelID, messageID, newContent string) error {
	payload, err := json.Marshal(map[string]string{"content": newContent})
	if err != nil {
		return fmt.Errorf("discord: marshal edit body: %w", err)
	}

	reqURL := fmt.Sprintf("%s/channels/%s/messages/%s", discordAPIBase, channelID, messageID)
	resp, err := a.discordAPIRequest(ctx, http.MethodPatch, reqURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: edit message: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// DeleteMessage deletes a message from a channel.
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	reqURL := fmt.Sprintf("%s/channels/%s/messages/%s", discordAPIBase, channelID, messageID)
	resp, err := a.discordAPIRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: delete message: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// AddReaction adds a reaction emoji to a message.
func (a *Adapter) AddReaction(ctx context.Context, channelID, messageID, emoji string) error {
	encoded := url.PathEscape(emoji)
	reqURL := fmt.Sprintf("%s/channels/%s/messages/%s/reactions/%s/@me", discordAPIBase, channelID, messageID, encoded)
	resp, err := a.discordAPIRequest(ctx, http.MethodPut, reqURL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: add reaction: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// RemoveReaction removes a reaction emoji from a message.
func (a *Adapter) RemoveReaction(ctx context.Context, channelID, messageID, emoji string) error {
	encoded := url.PathEscape(emoji)
	reqURL := fmt.Sprintf("%s/channels/%s/messages/%s/reactions/%s/@me", discordAPIBase, channelID, messageID, encoded)
	resp, err := a.discordAPIRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: remove reaction: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ReadHistory retrieves recent messages from a channel.
func (a *Adapter) ReadHistory(ctx context.Context, channelID string, limit int, before string) ([]channels.HistoryMessage, error) {
	reqURL := fmt.Sprintf("%s/channels/%s/messages?limit=%s", discordAPIBase, channelID, strconv.Itoa(limit))
	if strings.TrimSpace(before) != "" {
		reqURL += "&before=" + before
	}

	resp, err := a.discordAPIRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discord: read history: status %d body %s", resp.StatusCode, string(respBody))
	}

	var raw []discordHistoryMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("discord: decode history response: %w", err)
	}

	messages := make([]channels.HistoryMessage, len(raw))
	for i, m := range raw {
		messages[i] = channels.HistoryMessage{
			ID:        m.ID,
			ChannelID: channelID,
			SenderID:  m.Author.ID,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		}
	}
	return messages, nil
}

func encodeDiscordStreamHandle(channelID, messageID string) string {
	return strings.TrimSpace(channelID) + "|" + strings.TrimSpace(messageID)
}

func parseDiscordStreamHandle(handle string) (channelID string, messageID string, err error) {
	parts := strings.SplitN(strings.TrimSpace(handle), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("discord: invalid stream handle")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
