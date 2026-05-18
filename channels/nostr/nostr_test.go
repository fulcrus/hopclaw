package nostr

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

func TestBuildOutboundEventRejectsDirectMessagesWithoutEncryption(t *testing.T) {
	t.Parallel()

	adapter := &Adapter{pubKey: strings.Repeat("a", 64)}
	_, err := adapter.buildOutboundEvent(channels.OutboundMessage{
		TargetID: "npub1target",
		Content:  "secret",
	}, time.Now().Unix())
	if err == nil {
		t.Fatal("expected direct message to be rejected")
	}
	if !strings.Contains(err.Error(), "NIP-04") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildOutboundEventCreatesPublicNote(t *testing.T) {
	t.Parallel()

	adapter := &Adapter{pubKey: strings.Repeat("b", 64)}
	event, err := adapter.buildOutboundEvent(channels.OutboundMessage{
		Content: "hello nostr",
	}, 123)
	if err != nil {
		t.Fatalf("buildOutboundEvent() error = %v", err)
	}
	if event.Kind != 1 {
		t.Fatalf("Kind = %d, want 1", event.Kind)
	}
	if event.Content != "hello nostr" {
		t.Fatalf("Content = %q", event.Content)
	}
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		PrivateKey: strings.Repeat("1", 64),
		Relays:     []string{"wss://relay.example"},
	})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		PrivateKey: strings.Repeat("1", 64),
		Relays:     []string{"wss://relay.example"},
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

func TestHandleEventDropsEncryptedDMUntilDecryptionExists(t *testing.T) {
	t.Parallel()

	adapter := &Adapter{
		base:   channels.NewBaseAdapter("nostr"),
		pubKey: strings.Repeat("a", 64),
	}
	inbox := adapter.SubscribeEvents()

	adapter.handleEvent("wss://relay.example", nostrEvent{
		ID:      "evt-1",
		PubKey:  strings.Repeat("b", 64),
		Kind:    4,
		Content: "ciphertext?iv=stub",
	})

	select {
	case msg := <-inbox:
		t.Fatalf("expected encrypted DM to be dropped, got %#v", msg)
	default:
	}
}
