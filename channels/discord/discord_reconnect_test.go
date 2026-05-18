package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/gorilla/websocket"
)

func TestAdapterReconnectsAfterGatewayReadError(t *testing.T) {
	var connectionCount atomic.Int32
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway" {
			http.NotFound(w, r)
			return
		}
		writeDiscordTestJSON(t, w, map[string]any{
			"url": "ws" + strings.TrimPrefix(serverURL(t, w), "http") + "/ws",
		})
	}))
	defer server.Close()

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		connectionID := connectionCount.Add(1)
		go func() {
			defer conn.Close()
			if err := conn.WriteJSON(gatewayMessage{
				Op:   opHello,
				Data: mustDiscordJSON(t, helloData{HeartbeatInterval: 10}),
			}); err != nil {
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if connectionID == 1 {
				return
			}
			_ = conn.WriteJSON(gatewayMessage{
				Op:   opDispatch,
				Type: "MESSAGE_CREATE",
				Data: mustDiscordJSON(t, messageCreateEvent{
					ID:        "msg-reconnect",
					ChannelID: "ch-1",
					Content:   "after reconnect",
					Author: struct {
						ID       string `json:"id"`
						Username string `json:"username"`
						Bot      bool   `json:"bot"`
					}{
						ID:       "user-1",
						Username: "tester",
						Bot:      false,
					},
				}),
			})
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}))
	defer wsServer.Close()

	adapter := New(Config{BotToken: "test-token"})
	adapter.httpClient = &http.Client{Transport: discordRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://discord.com/api/v10/gateway" {
			t.Fatalf("unexpected request URL %s", req.URL.String())
		}
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rec).Encode(map[string]any{
			"url": "ws" + strings.TrimPrefix(wsServer.URL, "http"),
		})
		return rec.Result(), nil
	})}
	adapter.reconnectInitialBackoff = 10 * time.Millisecond
	adapter.reconnectMaxBackoff = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer adapter.Disconnect(context.Background())

	sub := adapter.SubscribeEvents()
	select {
	case msg := <-sub:
		if msg.Content != "after reconnect" {
			t.Fatalf("msg.Content = %q, want after reconnect", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect message")
	}

	if got := connectionCount.Load(); got < 2 {
		t.Fatalf("connectionCount = %d, want at least 2", got)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
}

func TestNextDiscordReconnectBackoff(t *testing.T) {
	t.Parallel()

	if got := nextDiscordReconnectBackoff(0, 2*time.Second, 30*time.Second); got != 2*time.Second {
		t.Fatalf("nextDiscordReconnectBackoff(0) = %s", got)
	}
	if got := nextDiscordReconnectBackoff(2*time.Second, 2*time.Second, 30*time.Second); got != 4*time.Second {
		t.Fatalf("nextDiscordReconnectBackoff(initial) = %s", got)
	}
	if got := nextDiscordReconnectBackoff(30*time.Second, 2*time.Second, 30*time.Second); got != 30*time.Second {
		t.Fatalf("nextDiscordReconnectBackoff(max) = %s", got)
	}
}

func mustDiscordJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}

func writeDiscordTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

func serverURL(t *testing.T, w http.ResponseWriter) string {
	t.Helper()
	req, ok := w.(interface{ Result() *http.Response })
	if ok && req.Result().Request != nil && req.Result().Request.URL != nil {
		return req.Result().Request.URL.String()
	}
	return ""
}
