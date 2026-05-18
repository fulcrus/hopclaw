package signal

import (
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestHandleMessagePublishesToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub1 := adapter.SubscribeEvents()
	sub2 := adapter.SubscribeEvents()

	msg := signalMessage{
		Envelope: signalEnvelope{
			Source:     "+15559876543",
			SourceName: "Alice",
			DataMessage: &signalDataMessage{
				Message:   "multi sub test",
				Timestamp: 1709000000000,
			},
		},
	}

	adapter.handleMessage(msg)

	for i, sub := range []<-chan channels.InboundMessage{sub1, sub2} {
		select {
		case inbound := <-sub:
			if inbound.Content != "multi sub test" {
				t.Fatalf("sub%d: Content = %q", i+1, inbound.Content)
			}
			if inbound.SenderID != "+15559876543" {
				t.Fatalf("sub%d: SenderID = %q", i+1, inbound.SenderID)
			}
		default:
			t.Fatalf("sub%d: expected message on subscriber channel", i+1)
		}
	}
}

func TestHandleMessageTrimsWhitespace(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source: "+15559876543",
			DataMessage: &signalDataMessage{
				Message: "  padded content  ",
			},
		},
	})

	select {
	case msg := <-sub:
		if msg.Content != "padded content" {
			t.Fatalf("Content = %q, want %q", msg.Content, "padded content")
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessageGroupChannelIDIncludesGroupID(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source:     "+15559876543",
			SourceName: "Bob",
			DataMessage: &signalDataMessage{
				Message: "group hello",
				GroupInfo: &signalGroupInfo{
					GroupID: "group-xyz-789",
				},
			},
		},
	})

	select {
	case msg := <-sub:
		if msg.ChannelID != "signal:group-xyz-789" {
			t.Fatalf("ChannelID = %q, want %q", msg.ChannelID, "signal:group-xyz-789")
		}
		if msg.RawEvent["group_id"] != "group-xyz-789" {
			t.Fatalf("RawEvent[group_id] = %v", msg.RawEvent["group_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessageDirectChannelIDIsSignal(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source: "+15559876543",
			DataMessage: &signalDataMessage{
				Message:   "direct message",
				Timestamp: 1709000000000,
			},
		},
	})

	select {
	case msg := <-sub:
		if msg.ChannelID != "signal" {
			t.Fatalf("ChannelID = %q, want %q", msg.ChannelID, "signal")
		}
		if msg.RawEvent["source"] != "+15559876543" {
			t.Fatalf("RawEvent[source] = %v", msg.RawEvent["source"])
		}
		ts, ok := msg.RawEvent["timestamp"].(int64)
		if !ok || ts != 1709000000000 {
			t.Fatalf("RawEvent[timestamp] = %v", msg.RawEvent["timestamp"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessagePreservesSourceName(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source:     "+15559876543",
			SourceName: "Charlie Smith",
			DataMessage: &signalDataMessage{
				Message: "name test",
			},
		},
	})

	select {
	case msg := <-sub:
		if msg.SenderName != "Charlie Smith" {
			t.Fatalf("SenderName = %q, want %q", msg.SenderName, "Charlie Smith")
		}
		if msg.SenderID != "+15559876543" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}
