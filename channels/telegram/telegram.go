package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("telegram")

const apiBase = "https://api.telegram.org/bot"

const (
	telegramReconnectInitialBackoff = 2 * time.Second
	telegramReconnectMaxBackoff     = 30 * time.Second
)

// Compile-time interface assertions.
var (
	_ channels.MessageEditor      = (*Adapter)(nil)
	_ channels.MessageDeleter     = (*Adapter)(nil)
	_ channels.StreamingRenderer  = (*Adapter)(nil)
	_ channels.CapabilityReporter = (*Adapter)(nil)
)

// Config holds the configuration for the Telegram adapter.
type Config struct {
	BotToken string `json:"bot_token" yaml:"bot_token"`
}

// Adapter implements channels.Adapter for the Telegram Bot API.
type Adapter struct {
	config Config
	client *http.Client

	base channels.BaseAdapter
}

// New creates a new Telegram adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 60 * time.Second},
		base:   channels.NewBaseAdapter("telegram"),
	}
}

// Connect starts the long-polling loop to receive updates from Telegram.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.BotToken == "" {
		return fmt.Errorf("telegram: bot_token is required")
	}

	pollCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.pollLoop(pollCtx)

	log.Info("telegram: adapter connected")
	return nil
}

// Disconnect stops the long-polling loop and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	log.Info("telegram: adapter disconnected")
	return nil
}

// Maximum text length per Telegram message.
const telegramMaxMessageLen = 4096

// Send delivers an outbound message through the Telegram Bot API.
// msg.TargetID is used as chat_id. If msg.ReplyToID is set, it is
// parsed as an integer and included as reply_to_message_id.
// Long messages are automatically split into chunks of up to 4096 characters.
// When format is "markdown" or "rich", content is converted to Telegram HTML.
// Blocks are rendered as titled HTML sections; attachments with image content
// types are sent via sendPhoto, other files via sendDocument.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("telegram: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("telegram: target_id (chat_id) is required")
	}

	content, parseMode := renderTelegramText(msg)

	// Send text in chunks.
	chunks := channels.ChunkText(content, telegramMaxMessageLen)
	for _, chunk := range chunks {
		if err := a.sendChunk(ctx, msg.TargetID, chunk, msg.ReplyToID, parseMode, msg.Metadata); err != nil {
			return err
		}
	}

	// Send attachments as native Telegram media messages.
	for _, att := range msg.Attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		if err := a.sendAttachment(ctx, msg.TargetID, msg.ReplyToID, att); err != nil {
			log.Warn("telegram: send attachment failed, falling back to link", "uri", uri, "error", err)
		}
	}
	return nil
}

func (a *Adapter) BeginStreaming(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	resp, err := a.telegramAPIPostResult(ctx, "sendMessage", map[string]any{
		"chat_id": msg.TargetID,
		"text":    "⏳ Thinking...",
	})
	if err != nil {
		return "", err
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("telegram: decode sendMessage result: %w", err)
	}
	if result.MessageID <= 0 {
		return "", fmt.Errorf("telegram: sendMessage missing message id")
	}
	return encodeTelegramStreamHandle(msg.TargetID, strconv.FormatInt(result.MessageID, 10)), nil
}

func (a *Adapter) UpdateStreaming(ctx context.Context, streamHandle string, content string) error {
	chatID, messageID, err := parseTelegramStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	return a.EditMessage(ctx, chatID, messageID, channels.TruncateUTF8(content, telegramMaxMessageLen))
}

func (a *Adapter) EndStreaming(ctx context.Context, streamHandle string, final channels.OutboundMessage) error {
	chatID, messageID, err := parseTelegramStreamHandle(streamHandle)
	if err != nil {
		return err
	}
	msgID, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid message_id %q: %w", messageID, err)
	}
	content, parseMode := renderTelegramText(final)
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       content,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	return a.telegramAPIPost(ctx, "editMessageText", payload)
}

// renderBlocksTelegramHTML converts OutboundBlocks to Telegram-compatible HTML.
func renderBlocksTelegramHTML(blocks []channels.OutboundBlock) string {
	var parts []string
	for _, b := range blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		content = channels.MarkdownToTelegramHTML(content)
		title := strings.TrimSpace(b.Title)
		if title != "" {
			parts = append(parts, fmt.Sprintf("<b>%s</b>\n%s", title, content))
		} else {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderTelegramText(msg channels.OutboundMessage) (content string, parseMode string) {
	content = msg.Content
	format := strings.TrimSpace(strings.ToLower(msg.Format))
	switch {
	case len(msg.Blocks) > 0:
		return renderBlocksTelegramHTML(msg.Blocks), "HTML"
	case format == "markdown" || format == "rich":
		return channels.MarkdownToTelegramHTML(msg.Content), "HTML"
	default:
		return content, ""
	}
}

// sendAttachment sends a file or image via the Telegram sendDocument/sendPhoto API.
func (a *Adapter) sendAttachment(ctx context.Context, chatID, replyToID string, att channels.OutboundAttachment) error {
	uri := strings.TrimSpace(att.URI)
	_, images := channels.AttachmentsByKind([]channels.OutboundAttachment{att})
	isImage := len(images) > 0

	method := "sendDocument"
	fileField := "document"
	if isImage {
		method = "sendPhoto"
		fileField = "photo"
	}

	payload := map[string]any{
		"chat_id": chatID,
		fileField: uri,
	}
	label := strings.TrimSpace(att.Label)
	if label != "" {
		payload["caption"] = label
	}
	if replyTo := strings.TrimSpace(replyToID); replyTo != "" {
		if msgID, err := strconv.ParseInt(replyTo, 10, 64); err == nil {
			payload["reply_to_message_id"] = msgID
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal %s payload: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL(method), strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("telegram: create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("telegram: decode %s response: %w", method, err)
	}
	if !result.OK {
		return fmt.Errorf("telegram: %s failed: %s", method, result.Description)
	}
	return nil
}

// sendChunk sends a single text chunk via the Telegram sendMessage API.
// It retries automatically on HTTP 429 rate limit responses.
func (a *Adapter) sendChunk(ctx context.Context, chatID, text, replyToID, parseMode string, metadata map[string]any) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if topicID, ok := metadata["topic_id"].(string); ok && strings.TrimSpace(topicID) != "" {
		if threadID, err := strconv.ParseInt(topicID, 10, 64); err == nil {
			payload["message_thread_id"] = threadID
		}
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if replyTo := strings.TrimSpace(replyToID); replyTo != "" {
		if msgID, err := strconv.ParseInt(replyTo, 10, 64); err == nil {
			payload["reply_to_message_id"] = msgID
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal sendMessage payload: %w", err)
	}

	url := a.apiURL("sendMessage")

	for attempt := 0; attempt <= channels.RateLimitMaxRetries(); attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("telegram: create sendMessage request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("telegram: sendMessage: %w", err)
		}

		if rlErr := channels.CheckRateLimit(resp); rlErr != nil {
			resp.Body.Close()
			log.Warn("telegram: rate limited", "retry_after", rlErr.RetryAfter, "attempt", attempt+1)
			if attempt >= channels.RateLimitMaxRetries() {
				return rlErr
			}
			if waitErr := channels.WaitForRateLimit(ctx, rlErr); waitErr != nil {
				return waitErr
			}
			continue
		}

		var result struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return fmt.Errorf("telegram: decode sendMessage response: %w", err)
		}
		resp.Body.Close()
		if !result.OK {
			return fmt.Errorf("telegram: sendMessage failed: %s", result.Description)
		}
		return nil
	}
	return fmt.Errorf("telegram: sendMessage: max retries exceeded")
}

// Capabilities returns what this adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   true,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
		Interactive:    true,
		Mobile:         true,
		InlineDelivery: true,
	}
}

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		Threading:      true,
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

// pollLoop runs getUpdates in a loop until the context is cancelled.
func (a *Adapter) pollLoop(ctx context.Context) {
	var offset int64
	var backoff time.Duration
	for {
		if ctx.Err() != nil {
			return
		}
		updates, nextOffset, err := a.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("telegram: getUpdates failed", "error", err)
			backoff = nextTelegramReconnectBackoff(backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		backoff = 0
		offset = nextOffset
		for _, update := range updates {
			a.handleUpdate(update)
		}
	}
}

func nextTelegramReconnectBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return telegramReconnectInitialBackoff
	}
	next := current * 2
	if next > telegramReconnectMaxBackoff {
		return telegramReconnectMaxBackoff
	}
	return next
}

// telegramUpdate represents a subset of the Telegram Update object.
type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
}

// telegramMessage represents a subset of the Telegram Message object.
type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	From      *telegramUser `json:"from,omitempty"`
	Chat      telegramChat  `json:"chat"`
	Text      string        `json:"text"`
}

// telegramUser represents a subset of the Telegram User object.
type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// telegramChat represents a subset of the Telegram Chat object.
type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// getUpdates calls the Telegram getUpdates API with long polling.
// Returns the parsed updates and the next offset to use.
func (a *Adapter) getUpdates(ctx context.Context, offset int64) ([]telegramUpdate, int64, error) {
	params := "?timeout=30&allowed_updates=[\"message\"]"
	if offset > 0 {
		params += "&offset=" + strconv.FormatInt(offset, 10)
	}

	url := a.apiURL("getUpdates") + params
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, offset, fmt.Errorf("create request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, offset, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, offset, fmt.Errorf("read response body: %w", err)
	}

	var result struct {
		OK          bool             `json:"ok"`
		Result      []telegramUpdate `json:"result"`
		Description string           `json:"description"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, offset, fmt.Errorf("decode response: %w", err)
	}
	if !result.OK {
		return nil, offset, fmt.Errorf("API error: %s", result.Description)
	}

	nextOffset := offset
	for _, u := range result.Result {
		if u.UpdateID >= nextOffset {
			nextOffset = u.UpdateID + 1
		}
	}
	return result.Result, nextOffset, nil
}

// handleUpdate processes a single Telegram update and publishes text
// messages to subscribers. Non-text updates are ignored.
func (a *Adapter) handleUpdate(update telegramUpdate) {
	if update.Message == nil {
		return
	}
	msg := update.Message

	// Only handle text messages.
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	senderID := ""
	senderName := ""
	if msg.From != nil {
		senderID = strconv.FormatInt(msg.From.ID, 10)
		senderName = strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		if senderName == "" {
			senderName = msg.From.Username
		}
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	messageID := strconv.FormatInt(msg.MessageID, 10)

	inbound := channels.InboundMessage{
		ChannelID:  "telegram",
		SenderID:   senderID,
		SenderName: senderName,
		Content:    text,
		RawEvent: map[string]any{
			"chat_id":    chatID,
			"message_id": messageID,
			"chat_type":  msg.Chat.Type,
		},
	}

	log.Info("telegram: received message",
		"sender", senderID,
		"chat_id", chatID,
		"content_length", len(text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("telegram: subscriber channel full, dropping message")
	})
}

// ---------------------------------------------------------------------------
// Optional capability interfaces
// ---------------------------------------------------------------------------

// EditMessage edits an existing message in the given chat.
// channelID is the Telegram chat_id; messageID is the numeric message_id as a string.
func (a *Adapter) EditMessage(ctx context.Context, channelID, messageID, newContent string) error {
	msgID, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid message_id %q: %w", messageID, err)
	}
	return a.telegramAPIPost(ctx, "editMessageText", map[string]any{
		"chat_id":    channelID,
		"message_id": msgID,
		"text":       newContent,
	})
}

// DeleteMessage deletes a message from the given chat.
// channelID is the Telegram chat_id; messageID is the numeric message_id as a string.
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	msgID, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid message_id %q: %w", messageID, err)
	}
	return a.telegramAPIPost(ctx, "deleteMessage", map[string]any{
		"chat_id":    channelID,
		"message_id": msgID,
	})
}

// telegramAPIPost sends a JSON POST request to the given Telegram Bot API method
// and checks the response for success.
func (a *Adapter) telegramAPIPost(ctx context.Context, method string, payload map[string]any) error {
	_, err := a.telegramAPIPostResult(ctx, method, payload)
	return err
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (a *Adapter) telegramAPIPostResult(ctx context.Context, method string, payload map[string]any) (*telegramAPIResponse, error) {
	if a.base.Status() != channels.StatusConnected {
		return nil, fmt.Errorf("telegram: adapter is not connected")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("telegram: marshal %s payload: %w", method, err)
	}

	url := a.apiURL(method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("telegram: create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	var result telegramAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode %s response: %w", method, err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: %s failed: %s", method, result.Description)
	}
	return &result, nil
}

// apiURL builds a Telegram Bot API endpoint URL.
func (a *Adapter) apiURL(method string) string {
	return apiBase + a.config.BotToken + "/" + method
}

func encodeTelegramStreamHandle(chatID, messageID string) string {
	return strings.TrimSpace(chatID) + "|" + strings.TrimSpace(messageID)
}

func parseTelegramStreamHandle(handle string) (chatID string, messageID string, err error) {
	parts := strings.SplitN(strings.TrimSpace(handle), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("telegram: invalid stream handle")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
