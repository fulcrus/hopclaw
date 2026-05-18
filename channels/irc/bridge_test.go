package irc

import (
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestHandlePrivmsgPublishesToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	sub1 := adapter.SubscribeEvents()
	sub2 := adapter.SubscribeEvents()

	adapter.handlePrivmsg("alice!user@host.com", "#dev", "hello everyone")

	for i, sub := range []<-chan channels.InboundMessage{sub1, sub2} {
		select {
		case msg := <-sub:
			if msg.Content != "hello everyone" {
				t.Fatalf("sub%d: Content = %q", i+1, msg.Content)
			}
			if msg.SenderID != "alice" {
				t.Fatalf("sub%d: SenderID = %q", i+1, msg.SenderID)
			}
		default:
			t.Fatalf("sub%d: expected message on subscriber channel", i+1)
		}
	}
}

func TestHandlePrivmsgSetsRawEventFields(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "hopclaw"})
	sub := adapter.SubscribeEvents()

	adapter.handlePrivmsg("bob!user@host.example", "#general", "test raw event")

	select {
	case msg := <-sub:
		if msg.RawEvent["target"] != "#general" {
			t.Fatalf("RawEvent[target] = %v", msg.RawEvent["target"])
		}
		if msg.RawEvent["prefix"] != "bob!user@host.example" {
			t.Fatalf("RawEvent[prefix] = %v", msg.RawEvent["prefix"])
		}
		if msg.ChannelID != "irc" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandlePrivmsgSkipsSelfCaseInsensitive(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Server: "irc.example.com:6697", Nick: "HopClaw"})
	sub := adapter.SubscribeEvents()

	// The adapter uses strings.EqualFold for self-message detection.
	adapter.handlePrivmsg("hopclaw!bot@host.com", "#general", "case insensitive self")

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for case-insensitive self, got %q", msg.Content)
	default:
	}
}

func TestParseLineTrailingOnly(t *testing.T) {
	t.Parallel()

	prefix, cmd, params := parseLine(":server NOTICE * :Server shutting down")
	if prefix != "server" {
		t.Fatalf("prefix = %q", prefix)
	}
	if cmd != "NOTICE" {
		t.Fatalf("cmd = %q", cmd)
	}
	if len(params) != 2 {
		t.Fatalf("len(params) = %d", len(params))
	}
	if params[0] != "*" {
		t.Fatalf("params[0] = %q", params[0])
	}
	if params[1] != "Server shutting down" {
		t.Fatalf("params[1] = %q", params[1])
	}
}

func TestParseLineNoTrailing(t *testing.T) {
	t.Parallel()

	prefix, cmd, params := parseLine(":server 433 * hopclaw")
	if prefix != "server" {
		t.Fatalf("prefix = %q", prefix)
	}
	if cmd != "433" {
		t.Fatalf("cmd = %q", cmd)
	}
	if len(params) != 2 {
		t.Fatalf("len(params) = %d, want 2", len(params))
	}
	if params[0] != "*" {
		t.Fatalf("params[0] = %q", params[0])
	}
	if params[1] != "hopclaw" {
		t.Fatalf("params[1] = %q", params[1])
	}
}
