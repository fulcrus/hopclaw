// Package nextcloudtalk implements channels.Adapter for Nextcloud Talk.
package nextcloudtalk

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
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("nextcloudtalk")

// Config holds the configuration for the Nextcloud Talk adapter.
type Config struct {
	BaseURL  string `json:"base_url" yaml:"base_url"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// Adapter implements channels.Adapter for Nextcloud Talk.
type Adapter struct {
	config Config
	client *http.Client

	mu            sync.Mutex
	base          channels.BaseAdapter
	stateMu       sync.RWMutex
	lastMessageID int64 // track last known message ID for incremental polling
}

// New creates a new Nextcloud Talk adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("nextcloudtalk"),
	}
}

// talkMessage represents a message from the Nextcloud Talk API. The JSON tags
// intentionally mirror Nextcloud's camelCase OCS payload fields.
type talkMessage struct {
	ID               int64  `json:"id"`
	Token            string `json:"token"`
	ActorType        string `json:"actorType"`
	ActorID          string `json:"actorId"`
	ActorDisplayName string `json:"actorDisplayName"`
	Message          string `json:"message"`
	Timestamp        int64  `json:"timestamp"`
	MessageType      string `json:"messageType"`
}

// talkOCSResponse wraps the OCS envelope returned by Nextcloud. The nested
// wire structs keep the provider's original field casing in JSON tags.
type talkOCSResponse struct {
	OCS struct {
		Meta struct {
			StatusCode int    `json:"statuscode"`
			Message    string `json:"message"`
		} `json:"meta"`
		Data []talkMessage `json:"data"`
	} `json:"ocs"`
}

// Connect starts the polling goroutine to receive messages from Nextcloud Talk.
// It expects the first poll to set lastMessageID for future incremental fetches.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.config.BaseURL == "" {
		return fmt.Errorf("nextcloudtalk: base_url is required")
	}
	if a.config.Username == "" || a.config.Password == "" {
		return fmt.Errorf("nextcloudtalk: username and password are required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	pollCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.pollLoop(pollCtx)

	log.Info("nextcloudtalk: adapter connected")
	return nil
}

// Disconnect stops the polling goroutine and closes all subscriber channels.
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
	log.Info("nextcloudtalk: adapter disconnected")
	return nil
}

// Send delivers an outbound message to a Nextcloud Talk conversation.
// msg.TargetID is used as the conversation token.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("nextcloudtalk: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("nextcloudtalk: target_id (conversation token) is required")
	}

	payload := map[string]string{
		"message": msg.Content,
	}
	if strings.TrimSpace(msg.ReplyToID) != "" {
		payload["replyTo"] = msg.ReplyToID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nextcloudtalk: marshal send payload: %w", err)
	}

	url := a.chatURL(msg.TargetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nextcloudtalk: create send request: %w", err)
	}
	a.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("nextcloudtalk: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nextcloudtalk: send returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Capabilities returns what this adapter supports.
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
		Threading: true,
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

// pollLoop polls Nextcloud Talk conversations for new messages at a 3-second interval.
func (a *Adapter) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOnce(ctx)
		}
	}
}

// pollOnce fetches new messages from all joined Nextcloud Talk conversations.
// In practice, the caller should configure which token to poll. For simplicity,
// this adapter uses a wildcard approach: poll the rooms endpoint, then poll
// each room that has unread messages. If lastMessageID is 0 it initialises
// without broadcasting old messages.
func (a *Adapter) pollOnce(ctx context.Context) {
	rooms, err := a.getRooms(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("nextcloudtalk: get rooms failed", "error", err)
		return
	}

	for _, room := range rooms {
		if room.UnreadMessages == 0 {
			continue
		}
		a.pollRoom(ctx, room.Token)
	}
}

// talkRoom represents a Nextcloud Talk conversation returned by the rooms
// endpoint. The JSON tags intentionally mirror Nextcloud's camelCase payload.
type talkRoom struct {
	Token          string      `json:"token"`
	DisplayName    string      `json:"displayName"`
	UnreadMessages int         `json:"unreadMessages"`
	LastMessage    talkMessage `json:"lastMessage"`
}

// talkRoomsResponse wraps the OCS envelope for rooms using the provider's
// original JSON field casing.
type talkRoomsResponse struct {
	OCS struct {
		Data []talkRoom `json:"data"`
	} `json:"ocs"`
}

// getRooms fetches the list of conversations the user is part of.
func (a *Adapter) getRooms(ctx context.Context) ([]talkRoom, error) {
	url := strings.TrimRight(a.config.BaseURL, "/") +
		"/ocs/v2.php/apps/spreed/api/v4/room"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result talkRoomsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode rooms: %w", err)
	}
	return result.OCS.Data, nil
}

// pollRoom fetches recent messages from a specific conversation token.
func (a *Adapter) pollRoom(ctx context.Context, token string) {
	a.stateMu.RLock()
	lastID := a.lastMessageID
	a.stateMu.RUnlock()

	url := a.chatURL(token) + "?lookIntoFuture=0&limit=100"
	if lastID > 0 {
		url += "&lastKnownMessageId=" + strconv.FormatInt(lastID, 10) + "&lookIntoFuture=1"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("nextcloudtalk: create room poll request", "error", err, "token", token)
		return
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("nextcloudtalk: room poll failed", "error", err, "token", token)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("nextcloudtalk: read room poll response", "error", err)
		return
	}

	// 304 means no new messages.
	if resp.StatusCode == http.StatusNotModified {
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("nextcloudtalk: room poll non-2xx", "status", resp.StatusCode, "token", token)
		return
	}

	var ocsResp talkOCSResponse
	if err := json.Unmarshal(body, &ocsResp); err != nil {
		log.Warn("nextcloudtalk: decode room poll response", "error", err)
		return
	}

	initialising := lastID == 0
	var maxID int64
	for _, m := range ocsResp.OCS.Data {
		if m.ID > maxID {
			maxID = m.ID
		}
		// Skip system messages and our own messages.
		if m.ActorType == "bots" || m.ActorID == a.config.Username {
			continue
		}
		if m.MessageType == "system" {
			continue
		}
		// On first poll, don't broadcast old messages.
		if initialising {
			continue
		}
		if m.ID <= lastID {
			continue
		}
		a.handleMessage(m)
	}

	if maxID > 0 {
		a.stateMu.Lock()
		if maxID > a.lastMessageID {
			a.lastMessageID = maxID
		}
		a.stateMu.Unlock()
	}
}

// handleMessage processes a single Nextcloud Talk message and publishes it.
func (a *Adapter) handleMessage(msg talkMessage) {
	text := strings.TrimSpace(msg.Message)
	if text == "" {
		return
	}

	rawEvent := map[string]any{
		"id":        msg.ID,
		"token":     msg.Token,
		"timestamp": msg.Timestamp,
		"actorType": msg.ActorType,
	}

	inbound := channels.InboundMessage{
		ChannelID:  "nextcloudtalk:" + msg.Token,
		SenderID:   msg.ActorID,
		SenderName: msg.ActorDisplayName,
		Content:    text,
		RawEvent:   rawEvent,
	}

	log.Info("nextcloudtalk: received message",
		"sender", msg.ActorID,
		"token", msg.Token,
		"content_length", len(text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("nextcloudtalk: subscriber channel full, dropping message")
	})
}

// chatURL returns the chat API URL for a given conversation token.
func (a *Adapter) chatURL(token string) string {
	return strings.TrimRight(a.config.BaseURL, "/") +
		"/ocs/v2.php/apps/spreed/api/v1/chat/" + token
}

// setHeaders sets the common headers required by the Nextcloud OCS API.
func (a *Adapter) setHeaders(req *http.Request) {
	req.SetBasicAuth(a.config.Username, a.config.Password)
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("Accept", "application/json")
}
