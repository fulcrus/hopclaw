package zalo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestSendRetriesTransientHTTPFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":1,"message":"temporary"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":0,"message":"ok"}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{AppID: "app-1", SecretKey: "secret", AccessToken: "token"})
	adapter.client = server.Client()
	adapter.sendURL = server.URL
	adapter.base.MarkConnected(nil)

	if err := adapter.Send(context.Background(), channels.OutboundMessage{TargetID: "user-1", Content: "hello"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestSendDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":1,"message":"invalid request"}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{AppID: "app-1", SecretKey: "secret", AccessToken: "token"})
	adapter.client = server.Client()
	adapter.sendURL = server.URL
	adapter.base.MarkConnected(nil)

	err := adapter.Send(context.Background(), channels.OutboundMessage{TargetID: "user-1", Content: "hello"})
	if err == nil {
		t.Fatal("Send() error = nil, want permanent failure")
	}
	var sendErr *channels.SendError
	if !errors.As(err, &sendErr) || sendErr.Retryable {
		t.Fatalf("error = %#v, want permanent SendError", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}
