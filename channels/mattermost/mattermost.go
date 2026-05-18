package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/gorilla/websocket"
)

var log = logging.WithSubsystem("mattermost")

var _ channels.CapabilityReporter = (*Adapter)(nil)

// Config holds the configuration for the Mattermost adapter.
type Config struct {
	BaseURL      string `json:"base_url" yaml:"base_url"`           // e.g. "https://mattermost.example.com"
	BotToken     string `json:"bot_token" yaml:"bot_token"`         // authentication token
	WebSocketURL string `json:"websocket_url" yaml:"websocket_url"` // e.g. "wss://mattermost.example.com"
}

// wsEvent represents a Mattermost WebSocket event.
type wsEvent struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Broadcast wsBroadcast     `json:"broadcast"`
	Seq       int64           `json:"seq"`
}

// wsBroadcast contains targeting info for a WebSocket event.
type wsBroadcast struct {
	ChannelID string `json:"channel_id"`
	TeamID    string `json:"team_id"`
	UserID    string `json:"user_id"`
}

// postedData is the data payload for a "posted" WebSocket event.
type postedData struct {
	Post        string `json:"post"` // JSON-encoded Post object
	ChannelName string `json:"channel_name"`
	ChannelType string `json:"channel_type"`
	SenderName  string `json:"sender_name"`
	TeamID      string `json:"team_id"`
}

// mmPost represents a Mattermost Post object.
type mmPost struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Message   string `json:"message"`
	RootID    string `json:"root_id,omitempty"`
	Type      string `json:"type"`
}

// Adapter implements channels.Adapter for Mattermost.
type Adapter struct {
	config Config

	base      channels.BaseAdapter
	stateMu   sync.Mutex
	conn      *websocket.Conn
	outbound  *channels.OutboundSerializer
	botUserID string // populated during connect
}

// New creates a new Mattermost adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		base:   channels.NewBaseAdapter("mattermost"),
	}
}

// Connect establishes a WebSocket connection to Mattermost.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.BaseURL == "" || a.config.BotToken == "" {
		return fmt.Errorf("mattermost: base_url and bot_token are required")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	// Resolve the bot's own user ID to skip self-messages.
	userID, err := a.fetchBotUserID(ctx)
	if err != nil {
		log.Warn("mattermost: could not resolve bot user id, self-messages will not be filtered", "error", err)
	} else {
		a.botUserID = userID
	}

	// Determine WebSocket URL.
	wsBase := a.config.WebSocketURL
	if wsBase == "" {
		// Derive from BaseURL by replacing the scheme.
		wsBase = strings.Replace(a.config.BaseURL, "https://", "wss://", 1)
		wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	}
	wsURL := strings.TrimRight(wsBase, "/") + "/api/v4/websocket"

	// Connect the WebSocket.
	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.config.BotToken)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("mattermost: websocket dial: %w", err)
	}
	outbound := channels.NewOutboundSerializer()

	// Send authentication challenge over the WebSocket.
	authMsg := map[string]any{
		"seq":    1,
		"action": "authentication_challenge",
		"data": map[string]string{
			"token": a.config.BotToken,
		},
	}
	if err := outbound.Do(func() error {
		return conn.WriteJSON(authMsg)
	}); err != nil {
		conn.Close()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("mattermost: websocket auth: %w", err)
	}

	wsCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		conn.Close()
		return nil
	}
	a.conn = conn
	a.outbound = outbound

	go a.readLoop(wsCtx, conn)

	log.Info("mattermost: adapter connected", "base_url", a.config.BaseURL)
	return nil
}

// Disconnect gracefully closes the Mattermost WebSocket connection.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	if a.conn != nil {
		_ = a.outbound.Do(func() error {
			return a.conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
		})
		_ = a.conn.Close()
		a.conn = nil
		a.outbound = nil
	}
	log.Info("mattermost: adapter disconnected")
	return nil
}

// Send posts a message to a Mattermost channel via the REST API.
// msg.TargetID is the channel_id.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("mattermost: adapter is not connected")
	}

	channelID := strings.TrimSpace(msg.TargetID)
	if channelID == "" {
		return fmt.Errorf("mattermost: target_id (channel_id) is required")
	}

	// Use markdown block rendering when blocks are present; Mattermost
	// natively supports markdown in the message field.
	content := msg.Content
	if len(msg.Blocks) > 0 {
		content = channels.ContentWithBlocks(msg, channels.RenderBlocksAsMarkdown)
	} else if len(msg.Attachments) > 0 {
		if att := channels.RenderAttachmentsAsText(msg.Attachments); att != "" {
			content = content + "\n\n" + att
		}
	}

	post := map[string]any{
		"channel_id": channelID,
		"message":    content,
	}
	if rootID := strings.TrimSpace(msg.ReplyToID); rootID != "" {
		post["root_id"] = rootID
	}

	// Add Mattermost-native attachments for structured blocks.
	if len(msg.Blocks) > 0 {
		var mmAttachments []map[string]any
		for _, b := range msg.Blocks {
			c := strings.TrimSpace(b.Content)
			if c == "" {
				continue
			}
			att := map[string]any{"text": c, "color": "#3498db"}
			if title := strings.TrimSpace(b.Title); title != "" {
				att["title"] = title
			}
			mmAttachments = append(mmAttachments, att)
		}
		if len(mmAttachments) > 0 {
			post["props"] = map[string]any{"attachments": mmAttachments}
		}
	}

	payload, err := json.Marshal(post)
	if err != nil {
		return fmt.Errorf("mattermost: marshal post: %w", err)
	}

	url := strings.TrimRight(a.config.BaseURL, "/") + "/api/v4/posts"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mattermost: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("mattermost: send post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mattermost: send post: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Capabilities returns what the Mattermost adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   true,
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

// readLoop reads WebSocket frames from Mattermost and dispatches events.
func (a *Adapter) readLoop(ctx context.Context, conn *websocket.Conn) {
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
			log.Error("mattermost: websocket read error", "error", err)
			a.base.SetStatus(channels.StatusError)
			return
		}

		var evt wsEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			log.Debug("mattermost: ignoring non-event frame", "error", err)
			continue
		}

		if evt.Event == "posted" {
			a.handlePosted(evt)
		}
	}
}

// handlePosted processes a "posted" WebSocket event.
func (a *Adapter) handlePosted(evt wsEvent) {
	var data postedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		log.Warn("mattermost: failed to parse posted data", "error", err)
		return
	}

	// The post field is a JSON-encoded string.
	var post mmPost
	if err := json.Unmarshal([]byte(data.Post), &post); err != nil {
		log.Warn("mattermost: failed to parse post object", "error", err)
		return
	}

	// Skip messages from the bot itself.
	if post.UserID == a.currentBotUserID() {
		return
	}

	// Skip system messages.
	if post.Type != "" {
		return
	}

	content := strings.TrimSpace(post.Message)
	if content == "" {
		return
	}

	inbound := channels.InboundMessage{
		ChannelID:  "mattermost",
		SenderID:   post.UserID,
		SenderName: data.SenderName,
		Content:    content,
		RawEvent: map[string]any{
			"channel_id":   post.ChannelID,
			"post_id":      post.ID,
			"root_id":      post.RootID,
			"channel_name": data.ChannelName,
			"channel_type": data.ChannelType,
			"team_id":      data.TeamID,
		},
	}

	log.Info("mattermost: received message",
		"sender", post.UserID,
		"channel_id", post.ChannelID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("mattermost: subscriber channel full, dropping message")
	})
}

func (a *Adapter) currentBotUserID() string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.botUserID
}

func (a *Adapter) clearConnIfMatch(conn *websocket.Conn) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.conn == conn {
		a.conn = nil
		a.outbound = nil
	}
}

// fetchBotUserID calls GET /api/v4/users/me to resolve the bot's user ID.
func (a *Adapter) fetchBotUserID(ctx context.Context) (string, error) {
	url := strings.TrimRight(a.config.BaseURL, "/") + "/api/v4/users/me"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}
