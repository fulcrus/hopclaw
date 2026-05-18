package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("matrix")

var _ channels.CapabilityReporter = (*Adapter)(nil)

// Config holds the configuration for the Matrix adapter.
type Config struct {
	HomeServer  string `json:"home_server" yaml:"home_server"`   // e.g. "https://matrix.org"
	UserID      string `json:"user_id" yaml:"user_id"`           // e.g. "@bot:matrix.org"
	AccessToken string `json:"access_token" yaml:"access_token"` // access token for authentication
}

// Adapter implements channels.Adapter for the Matrix protocol.
type Adapter struct {
	config Config
	client *http.Client

	stateMu sync.Mutex
	base    channels.BaseAdapter

	txnCounter atomic.Int64 // transaction counter for send operations
}

// syncResponse is a subset of the Matrix /sync response.
type syncResponse struct {
	NextBatch string    `json:"next_batch"`
	Rooms     syncRooms `json:"rooms"`
}

type syncRooms struct {
	Join map[string]syncJoinedRoom `json:"join"`
}

type syncJoinedRoom struct {
	Timeline syncTimeline `json:"timeline"`
}

type syncTimeline struct {
	Events []syncEvent `json:"events"`
}

type syncEvent struct {
	Type     string         `json:"type"`
	EventID  string         `json:"event_id"`
	Sender   string         `json:"sender"`
	Content  map[string]any `json:"content"`
	OriginTS int64          `json:"origin_server_ts"`
}

// New creates a new Matrix adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 90 * time.Second},
		base:   channels.NewBaseAdapter("matrix"),
	}
}

// Connect starts the Matrix /sync long-polling loop.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.HomeServer == "" || a.config.AccessToken == "" {
		return fmt.Errorf("matrix: home_server and access_token are required")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	// Perform an initial sync to obtain the since token, then start the loop.
	syncCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.syncLoop(syncCtx)

	log.Info("matrix: adapter connected", "home_server", a.config.HomeServer, "user_id", a.config.UserID)
	return nil
}

// Disconnect stops the sync loop and closes all subscriber channels.
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
	log.Info("matrix: adapter disconnected")
	return nil
}

// Send delivers a message to a Matrix room.
// msg.TargetID is the room ID (e.g. "!abc:matrix.org").
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("matrix: adapter is not connected")
	}

	roomID := strings.TrimSpace(msg.TargetID)
	if roomID == "" {
		return fmt.Errorf("matrix: target_id (room_id) is required")
	}

	txnID := fmt.Sprintf("hopclaw_%d", a.txnCounter.Add(1))

	msgType := "m.text"
	plainBody := msg.Content
	format := ""
	formattedBody := ""

	if len(msg.Blocks) > 0 {
		// Render blocks as HTML for Matrix.
		plainBody = channels.ContentWithBlocks(msg, channels.RenderBlocksAsPlain)
		formattedBody = renderBlocksMatrixHTML(msg.Blocks, msg.Attachments)
		format = "org.matrix.custom.html"
	} else if len(msg.Attachments) > 0 {
		if att := channels.RenderAttachmentsAsText(msg.Attachments); att != "" {
			plainBody = plainBody + "\n\n" + att
		}
	} else if msg.Format == "markdown" || msg.Format == "rich" {
		format = "org.matrix.custom.html"
		formattedBody = msg.Content
	}

	body := map[string]any{
		"msgtype": msgType,
		"body":    plainBody,
	}
	if format != "" {
		body["format"] = format
		body["formatted_body"] = formattedBody
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("matrix: marshal message: %w", err)
	}

	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		strings.TrimRight(a.config.HomeServer, "/"), roomID, txnID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("matrix: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("matrix: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("matrix: send message: status %d body %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Capabilities returns what the Matrix adapter supports.
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

// syncLoop runs the Matrix /sync endpoint in a long-polling loop.
func (a *Adapter) syncLoop(ctx context.Context) {
	defer func() {
		if a.base.Status() == channels.StatusConnected {
			a.base.SetStatus(channels.StatusDisconnected)
		}
	}()

	var since string

	// Initial sync with a short timeout to get the since token without
	// processing stale messages.
	initialResp, err := a.doSync(ctx, "", "0")
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("matrix: initial sync failed", "error", err)
	} else {
		since = initialResp.NextBatch
	}

	for {
		if ctx.Err() != nil {
			return
		}

		resp, err := a.doSync(ctx, since, "30000")
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("matrix: sync failed", "error", err)
			// Brief backoff before retrying.
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}

		// Process room events.
		for roomID, room := range resp.Rooms.Join {
			for _, evt := range room.Timeline.Events {
				a.handleTimelineEvent(roomID, evt)
			}
		}

		since = resp.NextBatch
	}
}

// doSync calls the Matrix /sync endpoint.
func (a *Adapter) doSync(ctx context.Context, since, timeout string) (*syncResponse, error) {
	base := strings.TrimRight(a.config.HomeServer, "/")
	url := base + "/_matrix/client/v3/sync?timeout=" + timeout
	if since != "" {
		url += "&since=" + since
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.AccessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result syncResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode sync response: %w", err)
	}
	return &result, nil
}

// handleTimelineEvent processes a single timeline event from a room.
func (a *Adapter) handleTimelineEvent(roomID string, evt syncEvent) {
	// Only handle m.room.message events.
	if evt.Type != "m.room.message" {
		return
	}

	// Skip messages from ourselves.
	if evt.Sender == a.config.UserID {
		return
	}

	// Extract the message body.
	body, _ := evt.Content["body"].(string)
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}

	msgtype, _ := evt.Content["msgtype"].(string)

	inbound := channels.InboundMessage{
		ChannelID:  "matrix",
		SenderID:   evt.Sender,
		SenderName: evt.Sender,
		Content:    body,
		RawEvent: map[string]any{
			"room_id":  roomID,
			"event_id": evt.EventID,
			"msgtype":  msgtype,
		},
	}

	log.Info("matrix: received message",
		"sender", evt.Sender,
		"room_id", roomID,
		"content_length", len(body),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("matrix: subscriber channel full, dropping message")
	})
}

// renderBlocksMatrixHTML converts OutboundBlocks and OutboundAttachments to
// an org.matrix.custom.html formatted_body string.
func renderBlocksMatrixHTML(blocks []channels.OutboundBlock, attachments []channels.OutboundAttachment) string {
	var parts []string
	for _, b := range blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		title := channels.EscapeHTML(strings.TrimSpace(b.Title))
		content = channels.EscapeHTML(content)
		if title != "" {
			parts = append(parts, fmt.Sprintf("<h4>%s</h4>\n<p>%s</p>", title, content))
		} else {
			parts = append(parts, "<p>"+content+"</p>")
		}
	}
	for _, att := range attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		label := channels.EscapeHTML(strings.TrimSpace(att.Label))
		if label == "" {
			label = channels.EscapeHTML(uri)
		}
		parts = append(parts, fmt.Sprintf("<p><a href=\"%s\">%s</a></p>", channels.EscapeHTML(uri), label))
	}
	return strings.Join(parts, "\n")
}
