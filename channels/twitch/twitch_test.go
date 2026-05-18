package twitch

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		OAuthToken: "oauth:test-token",
		Nick:       "botnick",
		Channels:   "channel-one",
	})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		OAuthToken: "oauth:test-token",
		Nick:       "botnick",
		Channels:   "channel-one",
	})
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

func TestHandleLinePublishesPrivmsg(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		OAuthToken: "oauth:test-token",
		Nick:       "botnick",
		Channels:   "channel-one",
	})
	sub := adapter.SubscribeEvents()

	adapter.handleLine("@display-name=Streamer;user-id=42;id=msg-1 :viewer!viewer@viewer.tmi.twitch.tv PRIVMSG #channel-one :hello from twitch")

	select {
	case msg := <-sub:
		if msg.ChannelID != "twitch:#channel-one" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "42" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Streamer" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.Content != "hello from twitch" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if got := msg.RawEvent["msg-id"]; got != "msg-1" {
			t.Fatalf("RawEvent[msg-id] = %v", got)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestConnectWritesHandshakeAndJoinsChannels(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	originalDial := dialTwitchTLS
	dialTwitchTLS = func(context.Context, string) (net.Conn, error) {
		return clientConn, nil
	}
	defer func() {
		dialTwitchTLS = originalDial
	}()

	adapter := New(Config{
		OAuthToken: "test-token",
		Nick:       "BotNick",
		Channels:   "channel-one,#channel-two",
	})

	lines := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(serverConn)
		count := 0
		for scanner.Scan() {
			lines <- scanner.Text()
			count++
			if count >= 5 {
				return
			}
		}
	}()

	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	var got []string
	deadline := time.After(time.Second)
	for len(got) < 5 {
		select {
		case line := <-lines:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("handshake lines = %#v, want 5 lines", got)
		}
	}
	<-done

	want := []string{
		"CAP REQ :twitch.tv/membership twitch.tv/tags twitch.tv/commands",
		"PASS oauth:test-token",
		"NICK botnick",
		"JOIN #channel-one",
		"JOIN #channel-two",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if adapter.Status() != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", adapter.Status(), channels.StatusConnected)
	}
	if cancel, ok := adapter.base.MarkDisconnected(); ok && cancel != nil {
		cancel()
	}
}

func TestSendWritesPrivmsg(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	adapter := New(Config{
		OAuthToken: "oauth:test-token",
		Nick:       "botnick",
		Channels:   "channel-one",
	})
	adapter.conn = clientConn
	adapter.writer = bufio.NewWriter(clientConn)
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	defer func() {
		if cancel, ok := adapter.base.MarkDisconnected(); ok && cancel != nil {
			cancel()
		}
	}()

	gotLine := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(serverConn)
		if scanner.Scan() {
			gotLine <- scanner.Text()
		}
	}()

	if err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "channel-one",
		Content:  "hello\nfrom twitch",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case line := <-gotLine:
		if line != "PRIVMSG #channel-one :hello from twitch" {
			t.Fatalf("line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("expected PRIVMSG line")
	}
}
