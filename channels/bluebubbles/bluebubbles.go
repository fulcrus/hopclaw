// Package bluebubbles implements channels.Adapter for BlueBubbles Server.
package bluebubbles

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

var log = logging.WithSubsystem("bluebubbles")

// Config holds the configuration for the BlueBubbles adapter.
type Config struct {
	BaseURL  string `json:"base_url" yaml:"base_url"`
	Password string `json:"password" yaml:"password"`
}

// Adapter implements channels.Adapter for BlueBubbles Server.
type Adapter struct {
	config Config
	client *http.Client

	base       channels.BaseAdapter
	stateMu    sync.Mutex
	lastPollTS int64 // epoch millis of last seen message
	polling    bool
}

// New creates a new BlueBubbles adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config:     cfg,
		client:     &http.Client{Timeout: 30 * time.Second},
		base:       channels.NewBaseAdapter("bluebubbles"),
		lastPollTS: time.Now().UnixMilli(),
	}
}

// bbMessage represents a message from the BlueBubbles Server API.
type bbMessage struct {
	GUID        string    `json:"guid"`
	Text        string    `json:"text"`
	IsFromMe    bool      `json:"isFromMe"`
	DateCreated int64     `json:"dateCreated"`
	Handle      *bbHandle `json:"handle,omitempty"`
	Chats       []bbChat  `json:"chats,omitempty"`
}

type bbHandle struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
}

type bbChat struct {
	GUID           string `json:"guid"`
	ChatIdentifier string `json:"chatIdentifier"`
	DisplayName    string `json:"displayName"`
}

// bbPollResponse is the response from the messages endpoint.
type bbPollResponse struct {
	Status  int         `json:"status"`
	Message string      `json:"message"`
	Data    []bbMessage `json:"data"`
}

// Connect starts the polling goroutine to receive messages from BlueBubbles.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.BaseURL == "" {
		return fmt.Errorf("bluebubbles: base_url is required")
	}
	if a.config.Password == "" {
		return fmt.Errorf("bluebubbles: password is required")
	}
	pollCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.pollLoop(pollCtx)

	log.Info("bluebubbles: adapter connected")
	return nil
}

// Disconnect stops the polling goroutine and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	log.Info("bluebubbles: adapter disconnected")
	return nil
}

// Send delivers an outbound message through the BlueBubbles Server API.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("bluebubbles: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("bluebubbles: target_id (chatGuid) is required")
	}

	payload := map[string]string{
		"chatGuid": msg.TargetID,
		"message":  msg.Content,
		"password": a.config.Password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("bluebubbles: marshal send payload: %w", err)
	}

	url := strings.TrimRight(a.config.BaseURL, "/") + "/api/v1/message/text"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("bluebubbles: create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("bluebubbles: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bluebubbles: send returned status %d: %s", resp.StatusCode, string(respBody))
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
		ReceiveEvent:   false,
		Interactive:    true,
		Mobile:         true,
		InlineDelivery: true,
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

// pollLoop polls the BlueBubbles messages endpoint at a 3-second interval.
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

// pollOnce fetches new messages from the BlueBubbles Server.
func (a *Adapter) pollOnce(ctx context.Context) {
	a.stateMu.Lock()
	if a.polling {
		a.stateMu.Unlock()
		return
	}
	a.polling = true
	afterTS := a.lastPollTS
	a.stateMu.Unlock()
	defer func() {
		a.stateMu.Lock()
		a.polling = false
		a.stateMu.Unlock()
	}()

	url := strings.TrimRight(a.config.BaseURL, "/") +
		"/api/v1/message?after=" + strconv.FormatInt(afterTS, 10) +
		"&password=" + a.config.Password +
		"&limit=100"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("bluebubbles: create poll request", "error", err)
		return
	}

	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("bluebubbles: poll request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("bluebubbles: read poll response", "error", err)
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("bluebubbles: poll returned non-2xx", "status", resp.StatusCode)
		return
	}

	var pollResp bbPollResponse
	if err := json.Unmarshal(body, &pollResp); err != nil {
		log.Warn("bluebubbles: decode poll response", "error", err)
		return
	}

	var maxTS int64
	for _, m := range pollResp.Data {
		if m.IsFromMe {
			continue
		}
		if m.DateCreated > maxTS {
			maxTS = m.DateCreated
		}
		a.handleMessage(m)
	}

	if maxTS > 0 {
		a.stateMu.Lock()
		if maxTS > a.lastPollTS {
			a.lastPollTS = maxTS
		}
		a.stateMu.Unlock()
	}
}

// handleMessage processes a single BlueBubbles message and publishes it.
func (a *Adapter) handleMessage(msg bbMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	senderID := ""
	senderName := ""
	if msg.Handle != nil {
		senderID = msg.Handle.Address
		senderName = msg.Handle.DisplayName
	}

	chatGUID := ""
	if len(msg.Chats) > 0 {
		chatGUID = msg.Chats[0].GUID
	}

	rawEvent := map[string]any{
		"guid":         msg.GUID,
		"date_created": msg.DateCreated,
		"chat_guid":    chatGUID,
	}

	inbound := channels.InboundMessage{
		ChannelID:  "bluebubbles",
		SenderID:   senderID,
		SenderName: senderName,
		Content:    text,
		RawEvent:   rawEvent,
	}

	log.Info("bluebubbles: received message",
		"sender", senderID,
		"content_length", len(text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("bluebubbles: subscriber channel full, dropping message")
	})
}
