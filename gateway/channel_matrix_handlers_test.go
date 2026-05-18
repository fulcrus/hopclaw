package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
)

type matrixTestAdapter struct{}

func (a *matrixTestAdapter) Connect(context.Context) error                        { return nil }
func (a *matrixTestAdapter) Disconnect(context.Context) error                     { return nil }
func (a *matrixTestAdapter) Send(context.Context, channels.OutboundMessage) error { return nil }
func (a *matrixTestAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}
func (a *matrixTestAdapter) Status() channels.Status                         { return channels.StatusConnected }
func (a *matrixTestAdapter) SubscribeEvents() <-chan channels.InboundMessage { return nil }
func (a *matrixTestAdapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{PolicyControls: true, Dedupe: true, ThreadBinding: true}
}

func TestChannelMatrixHandler(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	mgr := channelmgr.New()
	if err := mgr.Register("slack", &matrixTestAdapter{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	gw.channels = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/channels/matrix", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload channelMatrixResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d", payload.Count)
	}
	if !payload.Items[0].Capabilities.PolicyControls {
		t.Fatal("expected policy_controls to be true")
	}
}

func TestChannelThreadBindingsHandlers(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	bindings := channels.NewThreadBinding()
	bindings.Bind("slack", "thread-1", "slack:thread:thread-1")
	gw.SetThreadBindings(bindings)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/channels/thread-bindings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload threadBindingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d", payload.Count)
	}

	rec = doRequest(t, handler, http.MethodDelete, "/operator/channels/thread-bindings/slack/thread-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := bindings.Resolve("slack", "thread-1"); ok {
		t.Fatal("expected binding to be deleted")
	}
}
