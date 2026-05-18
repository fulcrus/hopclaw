package irc

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingServerReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Nick: "hopclaw"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing server")
	}
}

func TestConnectMissingNickReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing nick")
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	caps := New(Config{}).Capabilities()
	if !caps.SendText {
		t.Fatal("expected SendText=true")
	}
	if !caps.ReceiveMessage || !caps.ReceiveEvent {
		t.Fatal("expected receive capabilities to be true")
	}
	if caps.SendRichText {
		t.Fatal("expected SendRichText=false")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "#general",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty target_id")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	sub := adapter.SubscribeEvents()
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("expected subscriber channel to be closed")
		}
	default:
		t.Fatal("subscriber channel should be closed after disconnect")
	}
}

func TestDisconnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandlePrivmsgPublishesToSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	sub := adapter.SubscribeEvents()

	adapter.handlePrivmsg("testuser!user@host.com", "#general", "hello from irc")

	select {
	case msg := <-sub:
		if msg.Content != "hello from irc" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "testuser" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "testuser" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "irc" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["target"] != "#general" {
			t.Fatalf("RawEvent[target] = %v", msg.RawEvent["target"])
		}
		if msg.RawEvent["prefix"] != "testuser!user@host.com" {
			t.Fatalf("RawEvent[prefix] = %v", msg.RawEvent["prefix"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandlePrivmsgSkipsSelfMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	sub := adapter.SubscribeEvents()

	adapter.handlePrivmsg("hopclaw!bot@host.com", "#general", "self message")

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for self, got %q", msg.Content)
	default:
	}
}

func TestHandlePrivmsgSkipsEmptyText(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	sub := adapter.SubscribeEvents()

	adapter.handlePrivmsg("testuser!user@host.com", "#general", "   ")

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty text, got %q", msg.Content)
	default:
	}
}

func TestParseLineBasic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantPrefix string
		wantCmd    string
		wantParams []string
	}{
		{
			name:       "PRIVMSG with prefix",
			input:      ":nick!user@host PRIVMSG #channel :hello world",
			wantPrefix: "nick!user@host",
			wantCmd:    "PRIVMSG",
			wantParams: []string{"#channel", "hello world"},
		},
		{
			name:       "PING",
			input:      "PING :server.example.com",
			wantPrefix: "",
			wantCmd:    "PING",
			wantParams: []string{"server.example.com"},
		},
		{
			name:       "numeric 001",
			input:      ":server 001 nick :Welcome to the server",
			wantPrefix: "server",
			wantCmd:    "001",
			wantParams: []string{"nick", "Welcome to the server"},
		},
		{
			name:       "empty",
			input:      "",
			wantPrefix: "",
			wantCmd:    "",
			wantParams: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prefix, cmd, params := parseLine(tt.input)
			if prefix != tt.wantPrefix {
				t.Fatalf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(params) != len(tt.wantParams) {
				t.Fatalf("len(params) = %d, want %d", len(params), len(tt.wantParams))
			}
			for i, p := range params {
				if p != tt.wantParams[i] {
					t.Fatalf("params[%d] = %q, want %q", i, p, tt.wantParams[i])
				}
			}
		})
	}
}

func TestExtractNick(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefix string
		want   string
	}{
		{"nick!user@host", "nick"},
		{"nick", "nick"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			t.Parallel()

			got := extractNick(tt.prefix)
			if got != tt.want {
				t.Fatalf("extractNick(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestConfigUseTLSDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{Server: "irc.example.com:6697", Nick: "hopclaw"}
	if !cfg.useTLS() {
		t.Fatal("expected useTLS() to default to true when UseTLS is nil")
	}
}

func TestConfigUseTLSExplicitFalse(t *testing.T) {
	t.Parallel()

	f := false
	cfg := Config{Server: "irc.example.com:6667", Nick: "hopclaw", UseTLS: &f}
	if cfg.useTLS() {
		t.Fatal("expected useTLS() to be false when UseTLS is explicitly false")
	}
}

func TestConfigChannelList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		channels string
		want     []string
	}{
		{"#general,#dev", []string{"#general", "#dev"}},
		{"#general", []string{"#general"}},
		{"  #general , #dev , ", []string{"#general", "#dev"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.channels, func(t *testing.T) {
			t.Parallel()

			cfg := Config{Channels: tt.channels}
			got := cfg.channelList()
			if len(got) != len(tt.want) {
				t.Fatalf("channelList() = %v, want %v", got, tt.want)
			}
			for i, ch := range got {
				if ch != tt.want[i] {
					t.Fatalf("channelList()[%d] = %q, want %q", i, ch, tt.want[i])
				}
			}
		})
	}
}
