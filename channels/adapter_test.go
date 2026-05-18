package channels

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type optionalAdapterSupport struct{}

func (optionalAdapterSupport) Setup(context.Context, map[string]any) error { return nil }

func (optionalAdapterSupport) Health(context.Context) (HealthStatus, error) {
	return HealthStatus{Status: StatusConnected, Message: "ok"}, nil
}

func (optionalAdapterSupport) Metrics() ChannelMetrics {
	return ChannelMetrics{SentMessages: 1, ReceivedMessages: 2}
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   string
	}{
		{StatusConnected, "connected"},
		{StatusDisconnected, "disconnected"},
		{StatusConnecting, "connecting"},
		{StatusError, "error"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Fatalf("Status %q != %q", tt.status, tt.want)
		}
	}
}

func TestHTTPInboundErrorImplementsError(t *testing.T) {
	t.Parallel()

	err := &HTTPInboundError{
		StatusCode: http.StatusBadRequest,
		Message:    "bad request body",
	}

	var e error = err
	if e.Error() != "bad request body" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "bad request body")
	}
}

func TestNewHTTPInboundErrorFormat(t *testing.T) {
	t.Parallel()

	err := NewHTTPInboundError(http.StatusForbidden, "user %s not allowed", "alice")

	var httpErr *HTTPInboundError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected error to be *HTTPInboundError")
	}
	if httpErr.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusForbidden)
	}
	if httpErr.Message != "user alice not allowed" {
		t.Fatalf("Message = %q, want %q", httpErr.Message, "user alice not allowed")
	}
}

func TestNewHTTPInboundErrorNoArgs(t *testing.T) {
	t.Parallel()

	err := NewHTTPInboundError(http.StatusUnauthorized, "missing token")
	var httpErr *HTTPInboundError
	if !errors.As(err, &httpErr) {
		t.Fatal("expected error to be *HTTPInboundError")
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusUnauthorized)
	}
}

func TestCapabilitiesZeroValue(t *testing.T) {
	t.Parallel()

	var caps Capabilities
	if caps.SendText || caps.SendRichText || caps.SendFile || caps.ReceiveMessage || caps.ReceiveEvent {
		t.Fatal("zero-value Capabilities should all be false")
	}
}

func TestOptionalAdapterInterfacesRemainOptIn(t *testing.T) {
	t.Parallel()

	var (
		_ SetupAdapter   = optionalAdapterSupport{}
		_ HealthAdapter  = optionalAdapterSupport{}
		_ MetricsAdapter = optionalAdapterSupport{}
	)
}

func TestHealthStatusAndMetricsTypes(t *testing.T) {
	t.Parallel()

	adapter := optionalAdapterSupport{}
	health, err := adapter.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != StatusConnected {
		t.Fatalf("Health().Status = %q, want %q", health.Status, StatusConnected)
	}

	metrics := adapter.Metrics()
	if metrics.SentMessages != 1 || metrics.ReceivedMessages != 2 {
		t.Fatalf("Metrics() = %#v", metrics)
	}
}

func TestBaseAdapterZeroValueIsSafe(t *testing.T) {
	t.Parallel()

	var adapter BaseAdapter
	if got := adapter.Status(); got != StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, StatusDisconnected)
	}
	if sub := adapter.SubscribeEvents(); sub == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestBaseAdapterLifecycleAndCancel(t *testing.T) {
	t.Parallel()

	adapter := NewBaseAdapter("test")
	cancelled := false
	cancel, ok := adapter.MarkDisconnected()
	if cancel != nil || ok {
		t.Fatalf("MarkDisconnected() on disconnected adapter = (%v, %v), want (nil, false)", cancel, ok)
	}
	if !adapter.MarkConnected(func() { cancelled = true }) {
		t.Fatal("MarkConnected() = false, want true")
	}
	if adapter.MarkConnected(nil) {
		t.Fatal("second MarkConnected() = true, want false")
	}
	cancel, ok = adapter.MarkDisconnected()
	if !ok {
		t.Fatal("MarkDisconnected() = false, want true")
	}
	if cancel == nil {
		t.Fatal("MarkDisconnected() cancel = nil, want non-nil")
	}
	cancel()
	if !cancelled {
		t.Fatal("expected cancel function to be returned and executed")
	}
	if got := adapter.Status(); got != StatusDisconnected {
		t.Fatalf("Status() after disconnect = %q, want %q", got, StatusDisconnected)
	}
}

func TestBaseAdapterSetStatusSupportsTransitionalStates(t *testing.T) {
	t.Parallel()

	adapter := NewBaseAdapter("test")
	adapter.SetStatus(StatusConnecting)
	if got := adapter.Status(); got != StatusConnecting {
		t.Fatalf("Status() after SetStatus(connecting) = %q, want %q", got, StatusConnecting)
	}
	if !adapter.MarkConnected(nil) {
		t.Fatal("MarkConnected() from connecting = false, want true")
	}
	adapter.SetStatus(StatusError)
	if got := adapter.Status(); got != StatusError {
		t.Fatalf("Status() after SetStatus(error) = %q, want %q", got, StatusError)
	}
	if cancel, ok := adapter.MarkDisconnected(); cancel != nil || !ok {
		t.Fatalf("MarkDisconnected() from error = (%v, %v), want (nil, true)", cancel, ok)
	}
}

func TestBaseAdapterDisconnectClosesSubscribersAndPublishesInbound(t *testing.T) {
	t.Parallel()

	adapter := NewBaseAdapter("test")
	sub := adapter.SubscribeEvents()
	msg := InboundMessage{ChannelID: "test", Content: "hello"}
	adapter.PublishInbound(msg, nil)

	select {
	case got := <-sub:
		if got.Content != "hello" {
			t.Fatalf("inbound content = %q, want %q", got.Content, "hello")
		}
	default:
		t.Fatal("expected inbound message to be published")
	}

	if !adapter.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	if cancel, ok := adapter.MarkDisconnected(); cancel != nil || !ok {
		t.Fatalf("MarkDisconnected() = (%v, %v), want (nil, true)", cancel, ok)
	}

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("subscriber channel should be closed after disconnect")
		}
	default:
		t.Fatal("expected subscriber channel to be closed")
	}

	if !adapter.MarkConnected(nil) {
		t.Fatal("MarkConnected() after disconnect = false, want true")
	}
	sub = adapter.SubscribeEvents()
	adapter.PublishInbound(InboundMessage{ChannelID: "test", Content: "again"}, nil)
	select {
	case got := <-sub:
		if got.Content != "again" {
			t.Fatalf("inbound content = %q, want %q", got.Content, "again")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected inbound message after reconnect")
	}
}
