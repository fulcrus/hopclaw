// Package twitch implements channels.Adapter for Twitch IRC Chat.
package twitch

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("twitch")

const (
	twitchIRCHost = "irc.chat.twitch.tv"
	twitchIRCPort = "6697"
)

var dialTwitchTLS = func(ctx context.Context, address string) (net.Conn, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
	}
	return dialer.DialContext(ctx, "tcp", address)
}

// Config holds the configuration for the Twitch IRC adapter.
type Config struct {
	OAuthToken string `json:"oauth_token" yaml:"oauth_token"` // oauth:xxx token
	Nick       string `json:"nick" yaml:"nick"`               // bot username (lowercase)
	Channels   string `json:"channels" yaml:"channels"`       // comma-separated channel names (e.g. "#chan1,#chan2")
}

// Adapter implements channels.Adapter for Twitch IRC Chat.
type Adapter struct {
	config   Config
	channels []string // parsed channel list

	base   channels.BaseAdapter
	mu     sync.Mutex
	conn   net.Conn
	writer *bufio.Writer
}

// New creates a new Twitch IRC adapter with the given configuration.
func New(cfg Config) *Adapter {
	// Parse comma-separated channel list.
	var chans []string
	for _, ch := range strings.Split(cfg.Channels, ",") {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		// Ensure channels start with #.
		if !strings.HasPrefix(ch, "#") {
			ch = "#" + ch
		}
		chans = append(chans, strings.ToLower(ch))
	}

	return &Adapter{
		config:   cfg,
		channels: chans,
		base:     channels.NewBaseAdapter("twitch"),
	}
}

// Connect establishes a TLS connection to Twitch IRC and joins configured channels.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.config.OAuthToken == "" {
		return fmt.Errorf("twitch: oauth_token is required")
	}
	if a.config.Nick == "" {
		return fmt.Errorf("twitch: nick is required")
	}
	if len(a.channels) == 0 {
		return fmt.Errorf("twitch: at least one channel is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	// Dial TLS connection.
	conn, err := dialTwitchTLS(ctx, net.JoinHostPort(twitchIRCHost, twitchIRCPort))
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("twitch: dial TLS: %w", err)
	}
	a.conn = conn
	a.writer = bufio.NewWriter(conn)

	// Request capabilities.
	if err := a.writeLine("CAP REQ :twitch.tv/membership twitch.tv/tags twitch.tv/commands"); err != nil {
		a.closeConnLocked()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("twitch: request capabilities: %w", err)
	}

	// Authenticate.
	token := a.config.OAuthToken
	if !strings.HasPrefix(strings.ToLower(token), "oauth:") {
		token = "oauth:" + token
	}
	if err := a.writeLine("PASS " + token); err != nil {
		a.closeConnLocked()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("twitch: send PASS: %w", err)
	}
	if err := a.writeLine("NICK " + strings.ToLower(a.config.Nick)); err != nil {
		a.closeConnLocked()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("twitch: send NICK: %w", err)
	}

	// Join channels.
	for _, ch := range a.channels {
		if err := a.writeLine("JOIN " + ch); err != nil {
			a.closeConnLocked()
			a.base.SetStatus(channels.StatusError)
			return fmt.Errorf("twitch: join %s: %w", ch, err)
		}
	}

	ircCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		a.closeConnLocked()
		return nil
	}

	go a.readLoop(ircCtx, conn)

	log.Info("twitch: adapter connected",
		"nick", a.config.Nick,
		"channels", a.channels,
	)
	return nil
}

// Disconnect gracefully leaves all channels and closes the IRC connection.
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

	// Send PART for all channels before closing.
	if a.conn != nil {
		for _, ch := range a.channels {
			_ = a.writeLine("PART " + ch)
		}
		_ = a.writeLine("QUIT :goodbye")
	}

	a.closeConnLocked()
	log.Info("twitch: adapter disconnected")
	return nil
}

// Send writes a PRIVMSG to the specified Twitch channel.
// msg.TargetID is the channel name (e.g. "#channelname").
func (a *Adapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("twitch: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("twitch: target_id (channel name) is required")
	}

	target := msg.TargetID
	if !strings.HasPrefix(target, "#") {
		target = "#" + target
	}

	// IRC PRIVMSG does not support newlines; replace them.
	content := strings.ReplaceAll(msg.Content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", "")

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.writeLine("PRIVMSG " + target + " :" + content); err != nil {
		return fmt.Errorf("twitch: send PRIVMSG: %w", err)
	}

	if msg.ReplyToID != "" {
		log.Debug("twitch: reply_to_id is not supported in IRC, sending as regular message")
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

// readLoop reads lines from the IRC connection and dispatches them.
func (a *Adapter) readLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		if a.base.Status() == channels.StatusConnected {
			a.base.SetStatus(channels.StatusDisconnected)
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		a.handleLine(line)
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("twitch: IRC read error", "error", err)
		a.base.SetStatus(channels.StatusError)
	}
}

// handleLine processes a single raw IRC line.
func (a *Adapter) handleLine(line string) {
	// Handle PING/PONG to stay connected.
	if strings.HasPrefix(line, "PING") {
		pongPayload := strings.TrimPrefix(line, "PING")
		a.mu.Lock()
		_ = a.writeLine("PONG" + pongPayload)
		a.mu.Unlock()
		return
	}

	// Parse tags, prefix, command, and params.
	tags, prefix, command, params := parseIRCLine(line)
	_ = tags

	if command != "PRIVMSG" {
		return
	}
	if len(params) < 2 {
		return
	}

	channel := params[0]
	content := params[1]

	// Extract sender nick from prefix (nick!user@host).
	senderNick := ""
	senderID := ""
	if idx := strings.Index(prefix, "!"); idx > 0 {
		senderNick = prefix[:idx]
		senderID = prefix[:idx]
	} else {
		senderNick = prefix
		senderID = prefix
	}

	// Skip our own messages.
	if strings.EqualFold(senderNick, a.config.Nick) {
		return
	}

	rawEvent := map[string]any{
		"channel": channel,
		"prefix":  prefix,
	}
	// Include useful tags.
	if displayName, ok := tags["display-name"]; ok && displayName != "" {
		senderNick = displayName
		rawEvent["display-name"] = displayName
	}
	if msgID, ok := tags["id"]; ok {
		rawEvent["msg-id"] = msgID
	}
	if userID, ok := tags["user-id"]; ok {
		senderID = userID
		rawEvent["user-id"] = userID
	}

	inbound := channels.InboundMessage{
		ChannelID:  "twitch:" + channel,
		SenderID:   senderID,
		SenderName: senderNick,
		Content:    content,
		RawEvent:   rawEvent,
	}

	log.Info("twitch: received message",
		"sender", senderNick,
		"channel", channel,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("twitch: subscriber channel full, dropping message")
	})
}

// parseIRCLine parses a raw IRC line into tags, prefix, command, and params.
// Format: [@tags] [:prefix] <command> [params...] [:trailing]
func parseIRCLine(line string) (tags map[string]string, prefix, command string, params []string) {
	tags = make(map[string]string)

	// Parse optional tags.
	if strings.HasPrefix(line, "@") {
		idx := strings.Index(line, " ")
		if idx < 0 {
			return
		}
		tagStr := line[1:idx]
		line = strings.TrimLeft(line[idx+1:], " ")

		for _, tag := range strings.Split(tagStr, ";") {
			if eqIdx := strings.Index(tag, "="); eqIdx >= 0 {
				tags[tag[:eqIdx]] = unescapeTagValue(tag[eqIdx+1:])
			} else {
				tags[tag] = ""
			}
		}
	}

	// Parse optional prefix.
	if strings.HasPrefix(line, ":") {
		idx := strings.Index(line, " ")
		if idx < 0 {
			return
		}
		prefix = line[1:idx]
		line = strings.TrimLeft(line[idx+1:], " ")
	}

	// Parse command and params.
	if trailIdx := strings.Index(line, " :"); trailIdx >= 0 {
		head := line[:trailIdx]
		trailing := line[trailIdx+2:]

		parts := strings.Fields(head)
		if len(parts) > 0 {
			command = parts[0]
			params = append(parts[1:], trailing)
		}
	} else {
		parts := strings.Fields(line)
		if len(parts) > 0 {
			command = parts[0]
			params = parts[1:]
		}
	}
	return
}

// unescapeTagValue unescapes IRCv3 tag values.
func unescapeTagValue(s string) string {
	r := strings.NewReplacer(
		"\\:", ";",
		"\\s", " ",
		"\\\\", "\\",
		"\\r", "\r",
		"\\n", "\n",
	)
	return r.Replace(s)
}

// writeLine writes a line to the IRC connection followed by CRLF and flushes.
// Caller must hold a.mu if called concurrently.
func (a *Adapter) writeLine(line string) error {
	if a.writer == nil {
		return fmt.Errorf("not connected")
	}
	_, err := a.writer.WriteString(line + "\r\n")
	if err != nil {
		return err
	}
	return a.writer.Flush()
}

// closeConnLocked closes the underlying TCP connection.
// Caller must hold a.mu when invoking it.
func (a *Adapter) closeConnLocked() {
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn = nil
		a.writer = nil
	}
}
