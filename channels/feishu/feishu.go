package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/core/httpserverext"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var log = logging.WithSubsystem("feishu")

var newManagedWebsocketClientFunc = newManagedWebsocketClient

var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

type clientWrapper struct {
	api   *lark.Client
	apiWS websocketClient
}

type Adapter struct {
	config           Config
	defaultAccountID string

	base     channels.BaseAdapter
	stateMu  sync.RWMutex
	accounts map[string]*accountState
	wsWG     sync.WaitGroup
}

func New(cfg Config) *Adapter {
	return &Adapter{
		config:   cfg,
		base:     channels.NewBaseAdapter("feishu"),
		accounts: make(map[string]*accountState),
	}
}

func (a *Adapter) Connect(ctx context.Context) error {
	if a.base.Status() == channels.StatusConnected {
		return nil
	}
	a.base.SetStatus(channels.StatusConnecting)

	defaultAccountID, resolved := resolveAccounts(a.config)
	if len(resolved) == 0 {
		return fmt.Errorf("feishu: no accounts configured")
	}

	accounts := make(map[string]*accountState)
	connected := 0
	for _, account := range resolved {
		if !account.Enabled || strings.TrimSpace(account.AppID) == "" || strings.TrimSpace(account.AppSecret) == "" {
			continue
		}
		state, err := a.connectAccount(ctx, account)
		if err != nil {
			return err
		}
		accounts[account.ID] = state
		connected++
	}
	if connected == 0 {
		return fmt.Errorf("feishu: app_id and app_secret are required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}
	a.stateMu.Lock()
	a.accounts = accounts
	a.defaultAccountID = defaultAccountID
	a.stateMu.Unlock()

	for _, account := range accounts {
		if account.config.ConnectionMode != "websocket" || account.client == nil {
			continue
		}
		acct := account
		accountID := acct.id
		appID := acct.config.AppID
		a.wsWG.Add(1)
		go func() {
			defer a.wsWG.Done()
			log.Info("feishu: starting WebSocket connection", "account_id", accountID, "app_id", appID)
			if err := acct.client.apiWS.Start(runCtx); err != nil {
				if runCtx.Err() == nil {
					log.Error("feishu: WebSocket error", "error", err, "account_id", accountID)
				}
				a.base.SetStatus(channels.StatusError)
			}
		}()
	}
	log.Info("feishu: adapter connected", "accounts", connected)
	return nil
}

func (a *Adapter) connectAccount(_ context.Context, account ResolvedAccount) (*accountState, error) {
	domain := resolveOpenBaseURL(account.Domain)
	client := lark.NewClient(account.AppID, account.AppSecret,
		lark.WithEnableTokenCache(true),
		lark.WithLogLevel(larkcore.LogLevelInfo),
		lark.WithOpenBaseUrl(domain),
	)

	eventDispatcher := dispatcher.NewEventDispatcher(
		account.VerificationToken,
		account.EncryptKey,
	)
	eventDispatcher.OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
		a.handleIncomingMessage(account, event)
		return nil
	})

	state := &accountState{
		id:     account.ID,
		name:   account.Name,
		config: AccountConfig{AppID: account.AppID, AppSecret: account.AppSecret, EncryptKey: account.EncryptKey, VerificationToken: account.VerificationToken, Domain: account.Domain, ConnectionMode: account.ConnectionMode, DMPolicy: account.DMPolicy, AllowFrom: cloneStrings(account.AllowFrom), GroupPolicy: account.GroupPolicy, GroupAllowFrom: cloneStrings(account.GroupAllowFrom), GroupSessionScope: account.GroupSessionScope, ReplyInThread: boolToEnabledDisabled(account.ReplyInThread)},
		domain: account.Domain,
		client: &clientWrapper{api: client},
	}
	if account.RequireMention {
		b := true
		state.config.RequireMention = &b
	}
	if account.ConnectionMode == "websocket" {
		state.client.apiWS = newManagedWebsocketClientFunc(account.AppID, account.AppSecret, domain, eventDispatcher)
	}
	state.httpHandle = buildHTTPHandler(eventDispatcher)
	return state, nil
}

func (a *Adapter) Disconnect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	a.stateMu.RLock()
	accounts := make(map[string]*accountState, len(a.accounts))
	for id, account := range a.accounts {
		accounts[id] = account
	}
	a.stateMu.RUnlock()
	if cancel != nil {
		cancel()
	}
	a.stateMu.Lock()
	a.accounts = make(map[string]*accountState)
	a.defaultAccountID = ""
	a.stateMu.Unlock()

	var closeErr error
	for _, account := range accounts {
		if account == nil || account.client == nil || account.client.apiWS == nil {
			continue
		}
		if err := account.client.apiWS.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	done := make(chan struct{})
	go func() {
		a.wsWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		if closeErr != nil {
			return errors.Join(ctx.Err(), closeErr)
		}
		return ctx.Err()
	}
	if closeErr != nil {
		return closeErr
	}
	log.Info("feishu: adapter disconnected")
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("feishu: adapter is not connected")
	}
	accountID, account := a.resolveAccount(msg.Metadata)
	if account == nil || account.client == nil || account.client.api == nil {
		return fmt.Errorf("feishu: adapter is not connected")
	}
	if strings.TrimSpace(msg.ReplyToID) != "" {
		msgType, content := buildFeishuContent(msg)
		return replyWithClient(ctx, account.client.api, msg.ReplyToID, msgType, content, replyInThreadFromMetadata(msg.Metadata, account.config))
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("feishu: target id is required")
	}

	msgType, content := buildFeishuContent(msg)
	resp, err := account.client.api.Im.V1.Message.Create(ctx,
		larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(feishuReceiveIDType(msg)).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(msg.TargetID).
				MsgType(msgType).
				Content(content).
				Build(),
			).
			Build(),
	)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: send message: code=%d msg=%s", resp.Code, resp.Msg)
	}
	log.Info("feishu: sent message", "account_id", accountID, "target_id", msg.TargetID)
	return nil
}

func (a *Adapter) Reply(ctx context.Context, messageID string, text string) error {
	_, account := a.resolveAccount(nil)

	if account == nil || account.client == nil || account.client.api == nil {
		return fmt.Errorf("feishu: adapter is not connected")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	return replyWithClient(ctx, account.client.api, messageID, "text", string(content), replyInThreadFromMetadata(nil, account.config))
}

func replyWithClient(ctx context.Context, client *lark.Client, messageID string, msgType, content string, replyInThread bool) error {
	builder := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(msgType).
		Content(content)
	if replyInThread {
		builder.ReplyInThread(true)
	}
	resp, err := client.Im.V1.Message.Reply(ctx,
		larkim.NewReplyMessageReqBuilder().
			MessageId(messageID).
			Body(builder.Build()).
			Build(),
	)
	if err != nil {
		return fmt.Errorf("feishu: reply message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: reply message: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

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
		Threading:        true,
		Reactions:        true,
		EditMessage:      true,
		RichCards:        true,
		StreamingUpdates: true,
		TypingIndicator:  true,
		MultiAccount:     true,
		WebhookInbound:   true,
		PolicyControls:   true,
		Dedupe:           true,
		Pairing:          true,
	}
}

func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	webhookAccounts := a.webhookAccounts()
	var lastErr error
	for _, account := range webhookAccounts {
		resp, err := account.httpHandle(inboundHTTPRequest{
			method: req.Method,
			header: req.Header.Clone(),
			body:   append([]byte(nil), req.Body...),
		})
		if err != nil {
			lastErr = err
			continue
		}
		if resp == nil {
			continue
		}
		return &channels.HTTPInboundResponse{
			StatusCode: resp.statusCode,
			Headers:    resp.headers,
			Body:       resp.body,
		}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "feishu: no webhook account accepted request")
}

func (a *Adapter) StartStreamingCard(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	_, account := a.resolveAccount(msg.Metadata)
	if account == nil || account.client == nil || account.client.api == nil {
		return "", fmt.Errorf("feishu: adapter is not connected")
	}

	content := buildStreamingCardContent("", false)
	if strings.TrimSpace(msg.ReplyToID) != "" {
		builder := larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(content)
		if replyInThreadFromMetadata(msg.Metadata, account.config) {
			builder.ReplyInThread(true)
		}
		resp, err := account.client.api.Im.V1.Message.Reply(ctx,
			larkim.NewReplyMessageReqBuilder().
				MessageId(msg.ReplyToID).
				Body(builder.Build()).
				Build(),
		)
		if err != nil {
			return "", err
		}
		if !resp.Success() || resp.Data == nil || resp.Data.MessageId == nil {
			return "", fmt.Errorf("feishu: start streaming reply failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return derefStr(resp.Data.MessageId), nil
	}

	resp, err := account.client.api.Im.V1.Message.Create(ctx,
		larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(feishuReceiveIDType(msg)).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(msg.TargetID).
				MsgType("interactive").
				Content(content).
				Build(),
			).
			Build(),
	)
	if err != nil {
		return "", err
	}
	if !resp.Success() || resp.Data == nil || resp.Data.MessageId == nil {
		return "", fmt.Errorf("feishu: start streaming create failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return derefStr(resp.Data.MessageId), nil
}

func (a *Adapter) UpdateStreamingCard(ctx context.Context, messageID string, content string, final bool, metadata map[string]any) error {
	_, account := a.resolveAccount(metadata)
	if account == nil || account.client == nil || account.client.api == nil {
		return fmt.Errorf("feishu: adapter is not connected")
	}
	resp, err := account.client.api.Im.V1.Message.Patch(ctx,
		larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().
				Content(buildStreamingCardContent(content, final)).
				Build(),
			).
			Build(),
	)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: patch streaming card failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (a *Adapter) AddTypingIndicator(ctx context.Context, messageID string, metadata map[string]any) (string, error) {
	_, account := a.resolveAccount(metadata)
	if account == nil || account.client == nil || account.client.api == nil {
		return "", fmt.Errorf("feishu: adapter is not connected")
	}
	resp, err := account.client.api.Im.V1.MessageReaction.Create(ctx,
		larkim.NewCreateMessageReactionReqBuilder().
			MessageId(messageID).
			Body(larkim.NewCreateMessageReactionReqBodyBuilder().
				ReactionType(larkim.NewEmojiBuilder().EmojiType("Typing").Build()).
				Build(),
			).
			Build(),
	)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu: add typing indicator failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.ReactionId == nil {
		return "", nil
	}
	return derefStr(resp.Data.ReactionId), nil
}

func (a *Adapter) RemoveTypingIndicator(ctx context.Context, messageID, reactionID string, metadata map[string]any) error {
	if strings.TrimSpace(reactionID) == "" {
		return nil
	}
	_, account := a.resolveAccount(metadata)
	if account == nil || account.client == nil || account.client.api == nil {
		return fmt.Errorf("feishu: adapter is not connected")
	}
	resp, err := account.client.api.Im.V1.MessageReaction.Delete(ctx,
		larkim.NewDeleteMessageReactionReqBuilder().
			MessageId(messageID).
			ReactionId(reactionID).
			Build(),
	)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: remove typing indicator failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func buildHTTPHandler(eventDispatcher *dispatcher.EventDispatcher) httpHandler {
	handler := httpserverext.NewEventHandlerFunc(eventDispatcher)
	return func(req inboundHTTPRequest) (*inboundHTTPResponse, error) {
		httpReq := httptest.NewRequest(req.method, "http://hopclaw.local/channels/feishu/inbound", bytes.NewReader(req.body))
		for key, values := range req.header {
			for _, value := range values {
				httpReq.Header.Add(key, value)
			}
		}
		recorder := httptest.NewRecorder()
		handler(recorder, httpReq)
		result := recorder.Result()
		defer result.Body.Close()
		body := recorder.Body.Bytes()
		headers := make(map[string]string, len(result.Header))
		for key, values := range result.Header {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
		if result.StatusCode >= http.StatusBadRequest {
			return nil, channels.NewHTTPInboundError(result.StatusCode, "%s", string(body))
		}
		return &inboundHTTPResponse{
			statusCode: result.StatusCode,
			headers:    headers,
			body:       body,
		}, nil
	}
}

func (a *Adapter) handleIncomingMessage(account ResolvedAccount, event *larkim.P2MessageReceiveV1) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	msg := event.Event.Message
	content := parseTextContent(derefStr(msg.MessageType), derefStr(msg.Content))
	if strings.TrimSpace(content) == "" {
		return
	}

	senderID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		senderID = derefStr(event.Event.Sender.SenderId.OpenId)
	}
	mentioned := len(msg.Mentions) > 0 || strings.Contains(derefStr(msg.Content), "<at ")
	chatType := derefStr(msg.ChatType)
	inbound := channels.InboundMessage{
		ChannelID:  "feishu",
		SenderID:   senderID,
		SenderName: "",
		Content:    content,
		RawEvent: map[string]any{
			"account_id":   account.ID,
			"message_id":   derefStr(msg.MessageId),
			"chat_id":      derefStr(msg.ChatId),
			"chat_type":    chatType,
			"message_type": derefStr(msg.MessageType),
			"thread_id":    derefStr(msg.ThreadId),
			"root_id":      derefStr(msg.RootId),
			"parent_id":    derefStr(msg.ParentId),
			"mentioned":    mentioned,
		},
	}

	log.Info("feishu: received message",
		"account_id", account.ID,
		"sender", senderID,
		"chat_id", derefStr(msg.ChatId),
		"chat_type", chatType,
		"thread_id", derefStr(msg.ThreadId),
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("feishu: subscriber channel full, dropping message")
	})
}

func buildFeishuContent(msg channels.OutboundMessage) (string, string) {
	if len(msg.Blocks) == 0 && len(msg.Attachments) == 0 {
		content, _ := json.Marshal(map[string]string{"text": msg.Content})
		return "text", string(content)
	}

	var elements []map[string]any
	for _, b := range msg.Blocks {
		c := strings.TrimSpace(b.Content)
		if c == "" {
			continue
		}
		title := strings.TrimSpace(b.Title)
		if title != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + title + "**",
			})
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": c,
		})
		elements = append(elements, map[string]any{"tag": "hr"})
	}
	for _, att := range msg.Attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		label := strings.TrimSpace(att.Label)
		if label == "" {
			label = uri
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "[" + label + "](" + uri + ")",
		})
	}
	if n := len(elements); n > 0 {
		if tag, _ := elements[n-1]["tag"].(string); tag == "hr" {
			elements = elements[:n-1]
		}
	}
	const feishuMaxElements = 30
	if len(elements) > feishuMaxElements {
		elements = elements[:feishuMaxElements]
	}

	card := map[string]any{
		"config": map[string]any{
			"update_multi": true,
		},
		"elements": elements,
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "HopClaw",
			},
			"template": "blue",
		},
	}
	content, _ := json.Marshal(card)
	return "interactive", string(content)
}

func buildStreamingCardContent(content string, final bool) string {
	headerTitle := "HopClaw"
	headerTemplate := "blue"
	statusText := "Generating..."
	if final {
		headerTemplate = "green"
		statusText = "Completed"
	}
	if strings.TrimSpace(content) == "" {
		content = "_No content yet._"
	}
	card := map[string]any{
		"config": map[string]any{
			"update_multi": true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": headerTitle,
			},
			"template": headerTemplate,
		},
		"elements": []map[string]any{
			{
				"tag":     "markdown",
				"content": "**" + statusText + "**",
			},
			{
				"tag":     "markdown",
				"content": content,
			},
		},
	}
	data, _ := json.Marshal(card)
	return string(data)
}

func feishuReceiveIDType(msg channels.OutboundMessage) string {
	if msg.Metadata != nil {
		if value, ok := msg.Metadata["receive_id_type"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "chat_id"
}

func accountIDFromMetadata(metadata map[string]any, fallback string) string {
	if metadata != nil {
		if value, ok := metadata["account_id"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func replyInThreadFromMetadata(metadata map[string]any, cfg AccountConfig) bool {
	if metadata != nil {
		if value, ok := metadata["reply_in_thread"].(bool); ok {
			return value
		}
		if value, ok := metadata["receive_id_type"].(string); ok && strings.TrimSpace(value) == "thread_id" {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(cfg.ReplyInThread), "enabled")
}

func parseTextContent(msgType, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch msgType {
	case "text":
		var payload struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil {
			return strings.TrimSpace(payload.Text)
		}
		return raw
	case "post":
		var payload map[string]any
		if json.Unmarshal([]byte(raw), &payload) == nil {
			return extractPostText(payload)
		}
		return raw
	default:
		return fmt.Sprintf("[%s message]", msgType)
	}
}

func extractPostText(post map[string]any) string {
	for _, langBlock := range post {
		block, ok := langBlock.(map[string]any)
		if !ok {
			continue
		}
		paragraphs, ok := block["content"].([]any)
		if !ok {
			continue
		}
		var parts []string
		for _, para := range paragraphs {
			elements, ok := para.([]any)
			if !ok {
				continue
			}
			for _, elem := range elements {
				obj, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := obj["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func resolveOpenBaseURL(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "", "feishu":
		return lark.FeishuBaseUrl
	case "lark":
		return lark.LarkBaseUrl
	default:
		return normalizeDomain(domain)
	}
}

type accountConfigProvider interface {
	DefaultAccountID() string
	Account(accountID string) (ResolvedAccount, bool)
}

func (a *Adapter) DefaultAccountID() string {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.defaultAccountID
}

func (a *Adapter) Account(accountID string) (ResolvedAccount, bool) {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	state := a.accounts[strings.TrimSpace(accountID)]
	if state == nil {
		return ResolvedAccount{}, false
	}
	return ResolvedAccount{
		ID:                state.id,
		Name:              state.name,
		Enabled:           state.config.Enabled == nil || *state.config.Enabled,
		AppID:             state.config.AppID,
		AppSecret:         state.config.AppSecret,
		EncryptKey:        state.config.EncryptKey,
		VerificationToken: state.config.VerificationToken,
		Domain:            state.config.Domain,
		ConnectionMode:    state.config.ConnectionMode,
		DMPolicy:          normalizeDMPolicy(state.config.DMPolicy),
		AllowFrom:         cloneStrings(state.config.AllowFrom),
		GroupPolicy:       normalizeGroupPolicy(state.config.GroupPolicy),
		GroupAllowFrom:    cloneStrings(state.config.GroupAllowFrom),
		RequireMention:    state.config.RequireMention != nil && *state.config.RequireMention,
		GroupSessionScope: normalizeGroupSessionScope(state.config.GroupSessionScope),
		ReplyInThread:     strings.EqualFold(strings.TrimSpace(state.config.ReplyInThread), "enabled"),
	}, true
}

func (a *Adapter) resolveAccount(metadata map[string]any) (string, *accountState) {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	defaultID := a.defaultAccountID
	accountID := accountIDFromMetadata(metadata, defaultID)
	account := a.accounts[accountID]
	if account == nil && defaultID != "" {
		accountID = defaultID
		account = a.accounts[defaultID]
	}
	return accountID, account
}

func (a *Adapter) webhookAccounts() []*accountState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	accounts := make([]*accountState, 0, len(a.accounts))
	for _, account := range a.accounts {
		if account.config.ConnectionMode != "webhook" || account.httpHandle == nil {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts
}
