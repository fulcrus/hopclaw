package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	channelapi "github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
)

type stubChannelAdapter struct {
	status       channelapi.Status
	connectErr   error
	sendErr      error
	connectCalls int
	sendCalls    []channelapi.OutboundMessage
}

func (s *stubChannelAdapter) Connect(_ context.Context) error {
	s.connectCalls++
	if s.connectErr != nil {
		s.status = channelapi.StatusError
		return s.connectErr
	}
	if s.status == "" || s.status == channelapi.StatusDisconnected || s.status == channelapi.StatusError {
		s.status = channelapi.StatusConnected
	}
	return nil
}

func (s *stubChannelAdapter) Disconnect(context.Context) error { return nil }

func (s *stubChannelAdapter) Send(_ context.Context, msg channelapi.OutboundMessage) error {
	s.sendCalls = append(s.sendCalls, msg)
	return s.sendErr
}

func (s *stubChannelAdapter) Capabilities() channelapi.ChannelCapabilityDescriptor {
	return channelapi.Capabilities{SendText: true}
}

func (s *stubChannelAdapter) Status() channelapi.Status { return s.status }

func (s *stubChannelAdapter) SubscribeEvents() <-chan channelapi.InboundMessage { return nil }

func newTestGatewayWithChannels(t *testing.T, adapters map[string]*stubChannelAdapter) *Gateway {
	t.Helper()

	manager := channelmgr.New()
	for name, adapter := range adapters {
		if err := manager.Register(name, adapter); err != nil {
			t.Fatalf("Register(%q) error = %v", name, err)
		}
	}

	gw := newTestGatewayFull(t)
	gw.channels = manager
	gw.config.Channels = manager
	return gw
}

func TestChannelsValidateConnectsAdapter(t *testing.T) {
	t.Parallel()

	adapter := &stubChannelAdapter{status: channelapi.StatusDisconnected}
	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": adapter})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/validate", `{"channel":"slack","config":{"token":"x"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload channelValidateResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.Valid {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Status != string(channelapi.StatusConnected) {
		t.Fatalf("Status = %q", payload.Status)
	}
	if adapter.connectCalls != 1 {
		t.Fatalf("connectCalls = %d", adapter.connectCalls)
	}
}

func TestChannelsValidateReturnsConnectError(t *testing.T) {
	t.Parallel()

	adapter := &stubChannelAdapter{status: channelapi.StatusDisconnected, connectErr: errors.New("bad token")}
	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": adapter})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/validate", `{"channel":"slack"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload channelValidateResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Valid {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Message != "bad token" {
		t.Fatalf("Message = %q", payload.Message)
	}
	if payload.Status != string(channelapi.StatusError) {
		t.Fatalf("Status = %q", payload.Status)
	}
}

func TestChannelsValidateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": {status: channelapi.StatusDisconnected}})
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/validate", `{"channel":"slack"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChannelsDetectReflectsLiveStatus(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{
		"connected":  {status: channelapi.StatusConnected},
		"connecting": {status: channelapi.StatusConnecting},
		"idle":       {status: channelapi.StatusDisconnected},
	})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload channelDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 3 {
		t.Fatalf("Count = %d", payload.Count)
	}

	items := make(map[string]detectedChannel, len(payload.Channels))
	for _, item := range payload.Channels {
		items[item.Name] = item
	}
	if !items["connected"].Configured || items["connected"].Status != string(channelapi.StatusConnected) {
		t.Fatalf("connected = %+v", items["connected"])
	}
	if !items["connecting"].Configured || items["connecting"].Status != string(channelapi.StatusConnecting) {
		t.Fatalf("connecting = %+v", items["connecting"])
	}
	if items["idle"].Configured || items["idle"].Status != string(channelapi.StatusDisconnected) {
		t.Fatalf("idle = %+v", items["idle"])
	}
}

func TestChannelsDetectUsesCatalogOrderWithUnknownFallback(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{
		"discord": {status: channelapi.StatusConnected},
		"slack":   {status: channelapi.StatusConnected},
		"alpha":   {status: channelapi.StatusConnected},
		"zeta":    {status: channelapi.StatusConnected},
	})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload channelDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 4 {
		t.Fatalf("Count = %d", payload.Count)
	}
	if payload.Channels[0].Name != "slack" ||
		payload.Channels[1].Name != "discord" ||
		payload.Channels[2].Name != "alpha" ||
		payload.Channels[3].Name != "zeta" {
		t.Fatalf("channels order = %#v", payload.Channels)
	}
}

func TestChannelsTestMessageSendsThroughAdapter(t *testing.T) {
	t.Parallel()

	adapter := &stubChannelAdapter{status: channelapi.StatusConnected}
	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": adapter})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/test-message", `{"channel":"slack","target_id":"C123","message":"ping"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(adapter.sendCalls) != 1 {
		t.Fatalf("sendCalls = %d", len(adapter.sendCalls))
	}
	if adapter.sendCalls[0].TargetID != "C123" || adapter.sendCalls[0].Content != "ping" {
		t.Fatalf("sendCalls = %+v", adapter.sendCalls)
	}
}

func TestChannelsTestMessageRequiresTargetID(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": {status: channelapi.StatusConnected}})
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/test-message", `{"channel":"slack","message":"ping"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChannelsTestMessageTrimsPayloadAndRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	adapter := &stubChannelAdapter{status: channelapi.StatusConnected}
	gw := newTestGatewayWithChannels(t, map[string]*stubChannelAdapter{"slack": adapter})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/test-message", `{"channel":" slack ","target_id":" C123 ","message":" ping "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(adapter.sendCalls) != 1 {
		t.Fatalf("sendCalls = %d", len(adapter.sendCalls))
	}
	if adapter.sendCalls[0].TargetID != "C123" || adapter.sendCalls[0].Content != "ping" {
		t.Fatalf("sendCalls = %+v", adapter.sendCalls)
	}

	rec = doRequest(t, gw.Handler(), http.MethodPost, "/operator/channels/test-message", `{"channel":"slack","target_id":"C123","message":"ping"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}
