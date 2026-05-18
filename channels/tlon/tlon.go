// Package tlon implements channels.Adapter for Tlon (Urbit).
package tlon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("tlon")

// Config holds the configuration for the Tlon/Urbit adapter.
type Config struct {
	ShipURL  string `json:"ship_url" yaml:"ship_url"`   // e.g. http://localhost:8080
	ShipCode string `json:"ship_code" yaml:"ship_code"` // +code from dojo
}

// Adapter implements channels.Adapter for Tlon (Urbit).
type Adapter struct {
	config    Config
	client    *http.Client
	channelID string // unique channel identifier for Eyre

	base    channels.BaseAdapter
	stateMu sync.RWMutex
	eventID int64 // monotonically increasing event counter
}

// New creates a new Tlon/Urbit adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 0}, // no timeout for SSE
		base:   channels.NewBaseAdapter("tlon"),
	}
}

// Connect authenticates with the Urbit ship and starts an SSE event stream.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.ShipURL == "" {
		return fmt.Errorf("tlon: ship_url is required")
	}
	if a.config.ShipCode == "" {
		return fmt.Errorf("tlon: ship_code is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	// Create a client with a cookie jar for session management.
	jar, err := cookiejar.New(nil)
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("tlon: create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 0, // SSE streams should not time out
	}
	a.stateMu.Lock()
	a.client = client
	a.stateMu.Unlock()

	// Authenticate via +code login.
	if err := a.authenticate(ctx); err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("tlon: authentication failed: %w", err)
	}

	// Generate a unique channel ID.
	a.stateMu.Lock()
	a.channelID = fmt.Sprintf("hopclaw-%d-%d", time.Now().UnixMilli(), rand.Int63n(1000000))
	a.eventID = 0
	a.stateMu.Unlock()

	sseCtx, cancel := context.WithCancel(ctx)

	// Open the channel with an initial poke to establish it.
	if err := a.openChannel(ctx); err != nil {
		cancel()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("tlon: open channel: %w", err)
	}
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.sseLoop(sseCtx)

	log.Info("tlon: adapter connected", "channel", a.channelID)
	return nil
}

// authenticate logs in to the Urbit ship using the +code.
func (a *Adapter) authenticate(ctx context.Context) error {
	loginURL := strings.TrimRight(a.config.ShipURL, "/") + "/~/login"
	body := url.Values{"password": {a.config.ShipCode}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, loginURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("login returned status %d", resp.StatusCode)
	}
	return nil
}

// openChannel sends an initial poke to create the Eyre channel.
func (a *Adapter) openChannel(ctx context.Context) error {
	action := []map[string]any{
		{
			"id":     a.nextEventID(),
			"action": "poke",
			"ship":   a.shipName(),
			"app":    "hood",
			"mark":   "helm-hi",
			"json":   "hopclaw connected",
		},
	}
	return a.sendActions(ctx, action)
}

// sendActions sends a list of actions to the Eyre channel endpoint.
func (a *Adapter) sendActions(ctx context.Context, actions []map[string]any) error {
	body, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("marshal actions: %w", err)
	}

	url := a.channelURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create channel request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("channel request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("channel returned status %d", resp.StatusCode)
	}
	return nil
}

// sseLoop reads SSE events from the Eyre channel.
func (a *Adapter) sseLoop(ctx context.Context) {
	defer func() {
		if a.base.Status() == channels.StatusConnected {
			a.base.SetStatus(channels.StatusDisconnected)
		}
	}()

	for {
		if ctx.Err() != nil {
			return
		}

		err := a.readSSEStream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("tlon: SSE stream error, reconnecting", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

// readSSEStream connects to the SSE endpoint and processes events until an error occurs.
func (a *Adapter) readSSEStream(ctx context.Context) error {
	url := a.channelURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var currentID string
	var dataLines []string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = end of event.
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				a.handleSSEEvent(ctx, currentID, data)
			}
			currentID = ""
			dataLines = nil
			continue
		}

		if strings.HasPrefix(line, "id:") {
			currentID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE scanner: %w", err)
	}
	return fmt.Errorf("SSE stream ended")
}

// handleSSEEvent processes a single SSE event from the Urbit channel.
func (a *Adapter) handleSSEEvent(ctx context.Context, id string, data string) {
	data = strings.TrimSpace(data)
	if data == "" {
		return
	}

	// Acknowledge the event.
	if id != "" {
		go a.ackEvent(ctx, id)
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Debug("tlon: non-JSON SSE event", "data", data)
		return
	}

	// Look for chat message events.
	jsonData, ok := event["json"]
	if !ok {
		return
	}

	a.processUrbitMessage(event, jsonData)
}

// processUrbitMessage attempts to extract a chat message from an Urbit event.
func (a *Adapter) processUrbitMessage(event map[string]any, jsonData any) {
	// Urbit chat messages come in various structures depending on the app.
	// This handles the common graph-store add-nodes pattern.
	dataMap, ok := jsonData.(map[string]any)
	if !ok {
		return
	}

	// Try graph-update path.
	graphUpdate, ok := dataMap["graph-update"].(map[string]any)
	if !ok {
		return
	}
	addNodes, ok := graphUpdate["add-nodes"].(map[string]any)
	if !ok {
		return
	}

	resource, _ := addNodes["resource"].(map[string]any)
	nodes, ok := addNodes["nodes"].(map[string]any)
	if !ok {
		return
	}

	ship := ""
	name := ""
	if resource != nil {
		ship, _ = resource["ship"].(string)
		name, _ = resource["name"].(string)
	}

	for index, nodeRaw := range nodes {
		node, ok := nodeRaw.(map[string]any)
		if !ok {
			continue
		}
		post, ok := node["post"].(map[string]any)
		if !ok {
			continue
		}

		author, _ := post["author"].(string)
		contents, ok := post["contents"].([]any)
		if !ok || len(contents) == 0 {
			continue
		}

		// Extract text from contents.
		var textParts []string
		for _, c := range contents {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := cm["text"].(string); ok {
				textParts = append(textParts, t)
			}
		}
		text := strings.Join(textParts, " ")
		if strings.TrimSpace(text) == "" {
			continue
		}

		rawEvent := map[string]any{
			"ship":  ship,
			"name":  name,
			"index": index,
			"post":  post,
		}

		inbound := channels.InboundMessage{
			ChannelID:  "tlon:" + ship + "/" + name,
			SenderID:   author,
			SenderName: author,
			Content:    text,
			RawEvent:   rawEvent,
		}

		log.Info("tlon: received message",
			"sender", author,
			"resource", ship+"/"+name,
			"content_length", len(text),
		)

		a.base.PublishInbound(inbound, func() {
			log.Warn("tlon: subscriber channel full, dropping message")
		})
	}
}

// ackEvent acknowledges receipt of an SSE event.
func (a *Adapter) ackEvent(ctx context.Context, id string) {
	idNum, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return
	}

	eid := a.nextEventID()

	actions := []map[string]any{
		{
			"id":       eid,
			"action":   "ack",
			"event-id": idNum,
		},
	}

	if err := a.sendActions(ctx, actions); err != nil {
		log.Debug("tlon: ack failed", "error", err, "event_id", id)
	}
}

// Disconnect closes the SSE connection and cleans up.
func (a *Adapter) Disconnect(ctx context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}

	// Send delete action to clean up the channel on the ship.
	eid := a.nextEventID()
	actions := []map[string]any{
		{
			"id":     eid,
			"action": "delete",
		},
	}
	body, _ := json.Marshal(actions)
	channelURL := a.channelURL()
	client := a.httpClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, channelURL, bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	a.stateMu.Lock()
	a.channelID = ""
	a.stateMu.Unlock()
	log.Info("tlon: adapter disconnected")
	return nil
}

// Send delivers a message to an Urbit chat channel via poke.
// msg.TargetID should be "~ship/channel-name".
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("tlon: adapter is not connected")
	}
	eid := a.nextEventID()
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("tlon: target_id (resource path) is required")
	}

	// Parse target: "~ship/channel-name".
	parts := strings.SplitN(msg.TargetID, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("tlon: target_id must be in format ~ship/channel-name")
	}

	now := time.Now().UnixMilli()
	actions := []map[string]any{
		{
			"id":     eid,
			"action": "poke",
			"ship":   a.shipName(),
			"app":    "graph-push-hook",
			"mark":   "graph-update-3",
			"json": map[string]any{
				"add-nodes": map[string]any{
					"resource": map[string]string{
						"ship": parts[0],
						"name": parts[1],
					},
					"nodes": map[string]any{
						"/" + strconv.FormatInt(now, 10): map[string]any{
							"post": map[string]any{
								"author":     "~" + a.shipName(),
								"index":      "/" + strconv.FormatInt(now, 10),
								"time-sent":  now,
								"contents":   []map[string]string{{"text": msg.Content}},
								"hash":       nil,
								"signatures": []any{},
							},
							"children": nil,
						},
					},
				},
			},
		},
	}

	if err := a.sendActions(ctx, actions); err != nil {
		return fmt.Errorf("tlon: send message: %w", err)
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

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns a channel that receives inbound messages.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

// channelURL returns the full Eyre channel URL.
func (a *Adapter) channelURL() string {
	a.stateMu.RLock()
	channelID := a.channelID
	a.stateMu.RUnlock()
	return strings.TrimRight(a.config.ShipURL, "/") + "/~/channel/" + channelID
}

// shipName extracts the ship name from the ShipURL or returns a default.
func (a *Adapter) shipName() string {
	// The ship name can be extracted from cookies after login.
	// Prefer cookies, then fall back to the host name without inventing a ship.
	u, err := url.Parse(a.config.ShipURL)
	if err != nil {
		return ""
	}
	// Check cookies for urbauth ship name.
	client := a.httpClient()
	if client == nil || client.Jar == nil {
		return ""
	}
	for _, c := range client.Jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "urbauth-~") {
			return strings.TrimPrefix(c.Name, "urbauth-~")
		}
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return ""
	}
	host = strings.TrimPrefix(host, "~")
	if dot := strings.IndexByte(host, '.'); dot > 0 {
		host = host[:dot]
	}
	return strings.TrimSpace(host)
}

func (a *Adapter) nextEventID() int64 {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.eventID++
	return a.eventID
}

func (a *Adapter) httpClient() *http.Client {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.client
}
