package bluebubbles

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

type bluebubblesRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn bluebubblesRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestPollOnceSkipsConcurrentPolls(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://bluebubbles.example.test",
		Password: "secret",
	})
	var requests atomic.Int32
	block := make(chan struct{})
	adapter.client = &http.Client{Transport: bluebubblesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		requests.Add(1)
		<-block
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rec).Encode(bbPollResponse{})
		return rec.Result(), nil
	})}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		adapter.pollOnce(context.Background())
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		defer wg.Done()
		adapter.pollOnce(context.Background())
	}()

	time.Sleep(20 * time.Millisecond)
	close(block)
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestConnectMarksAdapterConnected(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://bluebubbles.example.test",
		Password: "secret",
	})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestSendPostsMessagePayload(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotContentType string
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
			t.Fatalf("Decode(payload) error = %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := New(Config{
		BaseURL:  server.URL,
		Password: "secret",
	})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer adapter.Disconnect(context.Background())

	if err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "chat-guid-1",
		Content:  "hello bluebubbles",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/message/text" {
		t.Fatalf("path = %q, want /api/v1/message/text", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", gotContentType)
	}
	if payload["chatGuid"] != "chat-guid-1" {
		t.Fatalf("chatGuid = %q", payload["chatGuid"])
	}
	if payload["message"] != "hello bluebubbles" {
		t.Fatalf("message = %q", payload["message"])
	}
	if payload["password"] != "secret" {
		t.Fatalf("password = %q", payload["password"])
	}
}

func TestPollOnceAdvancesLastPollTimestamp(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://bluebubbles.example.test",
		Password: "secret",
	})
	adapter.lastPollTS = 100
	adapter.client = &http.Client{Transport: bluebubblesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rec).Encode(bbPollResponse{
			Data: []bbMessage{{
				GUID:        "msg-1",
				Text:        "hello",
				DateCreated: 200,
				Handle:      &bbHandle{Address: "user@example.com", DisplayName: "User"},
				Chats:       []bbChat{{GUID: "chat-1"}},
			}},
		})
		return rec.Result(), nil
	})}

	sub := adapter.SubscribeEvents()
	adapter.pollOnce(context.Background())

	select {
	case msg := <-sub:
		if msg.Content != "hello" {
			t.Fatalf("msg.Content = %q", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("expected inbound message")
	}

	adapter.stateMu.Lock()
	got := adapter.lastPollTS
	adapter.stateMu.Unlock()
	if got != 200 {
		t.Fatalf("lastPollTS = %d, want 200", got)
	}
}
