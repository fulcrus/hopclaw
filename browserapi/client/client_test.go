package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
)

func TestDoUsesConfiguredHTTPTimeout(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browser/v1":
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(browsertypes.Response{
				OK:   true,
				Data: map[string]any{"ok": true},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	client := NewWithConfig(Config{BaseURL: host.URL, Timeout: 20 * time.Millisecond})
	_, err := client.Do(context.Background(), browsertypes.Request{Action: browsertypes.ActionSnapshot})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("Do() error = %v, want timeout", err)
	}
}

func TestDoWithTimeoutOverridesConfiguredHTTPTimeout(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browser/v1":
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(browsertypes.Response{
				OK:   true,
				Data: map[string]any{"ok": true},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	client := NewWithConfig(Config{BaseURL: host.URL, Timeout: 20 * time.Millisecond})
	resp, err := client.DoWithTimeout(context.Background(), browsertypes.Request{Action: browsertypes.ActionScreenshotLabeled}, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("DoWithTimeout() error = %v", err)
	}
	if resp == nil || !resp.OK {
		t.Fatalf("DoWithTimeout() response = %#v, want ok response", resp)
	}
}
