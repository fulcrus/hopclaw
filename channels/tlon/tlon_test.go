package tlon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

func TestShipNameFallsBackToHostInsteadOfHardcodedShip(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	adapter := New(Config{ShipURL: "https://mars.example.net"})
	adapter.client.Jar = jar

	if got := adapter.shipName(); got != "mars" {
		t.Fatalf("shipName() = %q, want %q", got, "mars")
	}
}

func TestShipNameReturnsEmptyForInvalidURL(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ShipURL: "://bad-url"})
	if got := adapter.shipName(); got != "" {
		t.Fatalf("shipName() = %q, want empty string", got)
	}
}

func TestConnectRequiresShipURL(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ShipCode: "lidlut-tabwed"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing ship_url")
	}
}

func TestConnectRequiresShipCode(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ShipURL: "https://mars.example.net"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing ship_code")
	}
}

func TestSendFormatsGraphUpdatePayload(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotActions []map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotActions); err != nil {
			t.Errorf("decode actions: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ShipURL: server.URL, ShipCode: "lidlut-tabwed"})
	adapter.client = server.Client()
	adapter.channelID = "channel-1"
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "~zod/general",
		Content:  "hello tlon",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotPath != "/~/channel/channel-1" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(gotActions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(gotActions))
	}
	action := gotActions[0]
	if action["action"] != "poke" {
		t.Fatalf("action = %v, want poke", action["action"])
	}
	jsonPayload, ok := action["json"].(map[string]any)
	if !ok {
		t.Fatalf("json payload = %#v", action["json"])
	}
	addNodes, ok := jsonPayload["add-nodes"].(map[string]any)
	if !ok {
		t.Fatalf("add-nodes payload = %#v", jsonPayload["add-nodes"])
	}
	resource, ok := addNodes["resource"].(map[string]any)
	if !ok {
		t.Fatalf("resource = %#v", addNodes["resource"])
	}
	if resource["ship"] != "~zod" || resource["name"] != "general" {
		t.Fatalf("resource = %#v", resource)
	}
	nodes, ok := addNodes["nodes"].(map[string]any)
	if !ok || len(nodes) != 1 {
		t.Fatalf("nodes = %#v", addNodes["nodes"])
	}
	for _, nodeRaw := range nodes {
		node, ok := nodeRaw.(map[string]any)
		if !ok {
			t.Fatalf("node = %#v", nodeRaw)
		}
		post, ok := node["post"].(map[string]any)
		if !ok {
			t.Fatalf("post = %#v", node["post"])
		}
		if post["author"] != "~"+adapter.shipName() {
			t.Fatalf("author = %v, want %q", post["author"], "~"+adapter.shipName())
		}
		contents, ok := post["contents"].([]any)
		if !ok || len(contents) != 1 {
			t.Fatalf("contents = %#v", post["contents"])
		}
		contentItem, ok := contents[0].(map[string]any)
		if !ok || contentItem["text"] != "hello tlon" {
			t.Fatalf("content item = %#v", contents[0])
		}
	}
}

func TestHandleSSEEventPublishesInboundAndAcknowledges(t *testing.T) {
	t.Parallel()

	ackCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var actions []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&actions); err != nil {
			t.Errorf("decode actions: %v", err)
		}
		if len(actions) == 1 && actions[0]["action"] == "ack" {
			ackCh <- actions[0]
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ShipURL: server.URL, ShipCode: "lidlut-tabwed"})
	adapter.client = server.Client()
	adapter.channelID = "channel-1"
	sub := adapter.SubscribeEvents()

	adapter.handleSSEEvent(context.Background(), "42", `{
		"json": {
			"graph-update": {
				"add-nodes": {
					"resource": {"ship": "~zod", "name": "general"},
					"nodes": {
						"/1700000000": {
							"post": {
								"author": "~nec",
								"contents": [{"text": "hello"}, {"text": "tlon"}]
							}
						}
					}
				}
			}
		}
	}`)

	select {
	case msg := <-sub:
		if msg.ChannelID != "tlon:~zod/general" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "~nec" || msg.SenderName != "~nec" {
			t.Fatalf("sender = %q / %q", msg.SenderID, msg.SenderName)
		}
		if msg.Content != "hello tlon" {
			t.Fatalf("Content = %q", msg.Content)
		}
	default:
		t.Fatal("expected inbound message to be published")
	}

	select {
	case ack := <-ackCh:
		if ack["event-id"] != float64(42) {
			t.Fatalf("event-id = %v, want 42", ack["event-id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected ack action to be sent")
	}
}

func TestDisconnectSendsDeleteAndClosesSubscribers(t *testing.T) {
	t.Parallel()

	deleteCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var actions []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&actions); err != nil {
			t.Errorf("decode actions: %v", err)
		}
		if len(actions) == 1 && actions[0]["action"] == "delete" {
			deleteCh <- actions[0]
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ShipURL: server.URL, ShipCode: "lidlut-tabwed"})
	adapter.client = server.Client()
	adapter.channelID = "channel-1"
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	sub := adapter.SubscribeEvents()

	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	select {
	case <-deleteCh:
	default:
		t.Fatal("expected delete action to be sent")
	}
	if adapter.channelID != "" {
		t.Fatalf("channelID = %q, want empty", adapter.channelID)
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
