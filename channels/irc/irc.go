package irc

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

var log = logging.WithSubsystem("irc")

// Config holds the configuration for the IRC adapter.
type Config struct {
	Server   string `json:"server" yaml:"server"`     // host:port
	Nick     string `json:"nick" yaml:"nick"`         // bot nickname
	Password string `json:"password" yaml:"password"` // server password (optional)
	UseTLS   *bool  `json:"use_tls" yaml:"use_tls"`   // nil defaults to true
	Channels string `json:"channels" yaml:"channels"` // comma-separated channel list, e.g. "#general,#dev"
}

// useTLS returns true unless explicitly set to false.
func (c Config) useTLS() bool {
	return c.UseTLS == nil || *c.UseTLS
}

// channelList returns the parsed list of IRC channels to join.
func (c Config) channelList() []string {
	var out []string
	for _, ch := range strings.Split(c.Channels, ",") {
		ch = strings.TrimSpace(ch)
		if ch != "" {
			out = append(out, ch)
		}
	}
	return out
}

// Adapter implements channels.Adapter for IRC.
type Adapter struct {
	config Config

	base   channels.BaseAdapter
	mu     sync.Mutex
	conn   net.Conn
	writer *bufio.Writer
}

// New creates a new IRC adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		base:   channels.NewBaseAdapter("irc"),
	}
}

// Connect establishes a TCP/TLS connection to the IRC server, registers the
// nick, and joins the configured channels.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.config.Server == "" || a.config.Nick == "" {
		return fmt.Errorf("irc: server and nick are required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	// Dial TCP or TLS.
	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 15 * time.Second}

	if a.config.useTLS() {
		conn, err = tls.DialWithDialer(dialer, "tcp", a.config.Server, &tls.Config{
			MinVersion: tls.VersionTLS12,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", a.config.Server)
	}
	if err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("irc: dial %s: %w", a.config.Server, err)
	}

	a.conn = conn
	a.writer = bufio.NewWriter(conn)

	// Send registration commands.
	if a.config.Password != "" {
		if err := a.writeLine("PASS " + a.config.Password); err != nil {
			a.closeConnLocked()
			a.base.SetStatus(channels.StatusError)
			return fmt.Errorf("irc: send PASS: %w", err)
		}
	}
	if err := a.writeLine("NICK " + a.config.Nick); err != nil {
		a.closeConnLocked()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("irc: send NICK: %w", err)
	}
	if err := a.writeLine("USER " + a.config.Nick + " 0 * :" + a.config.Nick); err != nil {
		a.closeConnLocked()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("irc: send USER: %w", err)
	}

	readCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		a.closeConnLocked()
		return nil
	}

	go a.readLoop(readCtx, conn)

	log.Info("irc: adapter connected", "server", a.config.Server, "nick", a.config.Nick)
	return nil
}

// Disconnect gracefully closes the IRC connection.
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

	// Send QUIT before closing.
	if a.conn != nil {
		_ = a.writeLine("QUIT :HopClaw signing off")
	}
	a.closeConnLocked()
	log.Info("irc: adapter disconnected")
	return nil
}

// Send delivers a PRIVMSG to the specified channel or user.
// msg.TargetID is the IRC channel (e.g. "#general") or nick.
func (a *Adapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("irc: adapter is not connected")
	}
	target := strings.TrimSpace(msg.TargetID)
	if target == "" {
		return fmt.Errorf("irc: target_id (channel or nick) is required")
	}

	// Render blocks as plain text for IRC (no rich formatting).
	content := msg.Content
	if len(msg.Blocks) > 0 {
		content = channels.ContentWithBlocks(msg, channels.RenderBlocksAsPlain)
	} else if len(msg.Attachments) > 0 {
		if att := channels.RenderAttachmentsAsText(msg.Attachments); att != "" {
			content = content + "\n\n" + att
		}
	}

	// IRC protocol: max 512 bytes per line including CRLF.
	// Reserve space for "PRIVMSG <target> :" prefix + CRLF.
	prefixLen := len("PRIVMSG ") + len(target) + len(" :\r\n")
	maxLineLen := 512 - prefixLen
	if maxLineLen < 50 {
		maxLineLen = 50
	}

	// IRC messages cannot contain newlines; send each line separately.
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Chunk lines that exceed the IRC protocol limit.
		for len(line) > maxLineLen {
			if err := a.writeLine("PRIVMSG " + target + " :" + line[:maxLineLen]); err != nil {
				return fmt.Errorf("irc: send PRIVMSG: %w", err)
			}
			line = line[maxLineLen:]
		}
		if line != "" {
			if err := a.writeLine("PRIVMSG " + target + " :" + line); err != nil {
				return fmt.Errorf("irc: send PRIVMSG: %w", err)
			}
		}
	}
	return nil
}

// Capabilities returns what the IRC adapter supports.
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

// readLoop reads lines from the IRC connection and dispatches events.
func (a *Adapter) readLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		a.clearConnIfMatch(conn)
		if a.base.Status() == channels.StatusConnected {
			a.base.SetStatus(channels.StatusDisconnected)
		}
	}()

	scanner := bufio.NewScanner(conn)
	joinSent := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			if err := scanner.Err(); err != nil {
				log.Error("irc: read error", "error", err)
			}
			a.base.SetStatus(channels.StatusError)
			return
		}

		raw := scanner.Text()
		log.Debug("irc: recv", "line", raw)

		// Handle PING to keep the connection alive.
		if strings.HasPrefix(raw, "PING") {
			token := strings.TrimPrefix(raw, "PING ")
			a.mu.Lock()
			_ = a.writeLine("PONG " + token)
			a.mu.Unlock()
			continue
		}

		prefix, command, params := parseLine(raw)

		// After receiving the welcome numeric (001), join channels.
		if command == "001" && !joinSent {
			joinSent = true
			a.mu.Lock()
			for _, ch := range a.config.channelList() {
				_ = a.writeLine("JOIN " + ch)
			}
			a.mu.Unlock()
			continue
		}

		// Dispatch PRIVMSG events to subscribers.
		if command == "PRIVMSG" && len(params) >= 2 {
			a.handlePrivmsg(prefix, params[0], params[1])
		}
	}
}

// handlePrivmsg processes a PRIVMSG and publishes it to subscribers.
func (a *Adapter) handlePrivmsg(prefix, target, text string) {
	nick := extractNick(prefix)

	// Skip messages from ourselves.
	if strings.EqualFold(nick, a.config.Nick) {
		return
	}

	content := strings.TrimSpace(text)
	if content == "" {
		return
	}

	inbound := channels.InboundMessage{
		ChannelID:  "irc",
		SenderID:   nick,
		SenderName: nick,
		Content:    content,
		RawEvent: map[string]any{
			"target": target,
			"prefix": prefix,
		},
	}

	log.Info("irc: received message",
		"sender", nick,
		"target", target,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("irc: subscriber channel full, dropping message")
	})
}

// writeLine writes a single IRC line (appending \r\n) and flushes.
// Caller must hold a.mu or ensure exclusive access.
func (a *Adapter) writeLine(line string) error {
	if a.writer == nil {
		return fmt.Errorf("writer is nil")
	}
	if _, err := a.writer.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return a.writer.Flush()
}

// closeConn closes the underlying TCP connection and clears references.
func (a *Adapter) closeConnLocked() {
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn = nil
	}
	a.writer = nil
}

func (a *Adapter) clearConnIfMatch(conn net.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == conn {
		a.conn = nil
		a.writer = nil
	}
}

// parseLine parses a raw IRC line into prefix, command, and params.
// Format: [:prefix] command param1 param2 ... [:trailing]
func parseLine(raw string) (prefix, command string, params []string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}

	// Extract prefix.
	if raw[0] == ':' {
		idx := strings.Index(raw, " ")
		if idx < 0 {
			return raw[1:], "", nil
		}
		prefix = raw[1:idx]
		raw = raw[idx+1:]
	}

	// Split the remainder into command and parameters.
	trailingIdx := strings.Index(raw, " :")
	var trailing string
	if trailingIdx >= 0 {
		trailing = raw[trailingIdx+2:]
		raw = raw[:trailingIdx]
	}

	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return prefix, "", nil
	}
	command = parts[0]
	params = parts[1:]
	if trailing != "" {
		params = append(params, trailing)
	}
	return prefix, command, params
}

// extractNick extracts the nickname from an IRC prefix (nick!user@host).
func extractNick(prefix string) string {
	if idx := strings.Index(prefix, "!"); idx >= 0 {
		return prefix[:idx]
	}
	return prefix
}
