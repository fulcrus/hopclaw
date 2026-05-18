package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/discovery"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
)

type stubOperatorSessionCapability struct {
	sessions []*captypes.SessionHandle
}

func (s *stubOperatorSessionCapability) Manifest() captypes.Manifest {
	return captypes.Manifest{Name: "desktop", Kind: captypes.KindSession, SessionScoped: true}
}

func (s *stubOperatorSessionCapability) Health(context.Context) captypes.Health {
	return captypes.Health{Status: captypes.StatusReady}
}

func (s *stubOperatorSessionCapability) Invoke(context.Context, captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}

func (s *stubOperatorSessionCapability) OpenSession(context.Context, map[string]any) (*captypes.SessionHandle, error) {
	return nil, nil
}

func (s *stubOperatorSessionCapability) CloseSession(context.Context, string) error {
	return nil
}

func (s *stubOperatorSessionCapability) ListSessions() []*captypes.SessionHandle {
	return s.sessions
}

type stubDiscoveryResolver struct {
	peers []discovery.Peer
}

func (s stubDiscoveryResolver) Discover(context.Context) ([]discovery.Peer, error) {
	return s.peers, nil
}

func (s stubDiscoveryResolver) Announce(context.Context, discovery.Peer) error { return nil }
func (s stubDiscoveryResolver) Stop() error                                    { return nil }

func TestHandleNodesList(t *testing.T) {
	gw := newTestGatewayFull(t)
	store := deviceauth.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	if err := store.RegisterDevice(&deviceauth.DeviceIdentity{DeviceID: "ios-1", Name: "Alice Phone", Trusted: true}); err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	gw.SetDeviceAuth(store, deviceauth.NewPairingManager(store))
	nodeRegistry := gatewaynodes.NewRegistry()
	gw.SetWSHandler(NewWSHandler(gw, nodeRegistry))
	defer nodeRegistry.Stop()

	nodeRegistry.Register(gatewaynodes.NodeSession{
		NodeID:          "ios-1",
		Platform:        "iOS",
		Version:         "1.2.3",
		DeviceFamily:    "iPhone",
		ModelIdentifier: "iPhone15,2",
		RemoteIP:        "10.0.0.2",
		Capabilities:    []string{"camera", "canvas"},
		Commands:        []string{"camera.snap", "canvas.present"},
	}, func([]byte) error { return nil })

	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/nodes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/nodes status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload operatorNodeListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if got := payload.Items[0].NodeID; got != "ios-1" {
		t.Fatalf("node_id = %q, want ios-1", got)
	}
	if got := payload.Items[0].Name; got != "Alice Phone" {
		t.Fatalf("name = %q, want Alice Phone", got)
	}
	if payload.Items[0].Status != "connected" {
		t.Fatalf("status = %q, want connected", payload.Items[0].Status)
	}
}

func TestHandleInstancesList(t *testing.T) {
	gw := newTestGatewayFull(t)
	nodeRegistry := gatewaynodes.NewRegistry()
	gw.SetWSHandler(NewWSHandler(gw, nodeRegistry))
	defer nodeRegistry.Stop()

	now := time.Now().UTC()
	gw.wsHandler.registry.add(&wsClient{
		id:          "operator-web",
		platform:    "web",
		remoteAddr:  "127.0.0.1:9999",
		connectedAt: now,
	})
	nodeRegistry.Register(gatewaynodes.NodeSession{
		NodeID:       "mac-mini",
		Platform:     "macOS",
		DeviceFamily: "Mac",
		RemoteIP:     "192.168.1.8",
	}, func([]byte) error { return nil })

	reg := capregistry.New()
	if err := reg.Register(&stubOperatorSessionCapability{sessions: []*captypes.SessionHandle{{
		ID:         "desktop-1",
		Capability: "desktop",
		CreatedAt:  now.Add(-time.Minute),
		Metadata:   map[string]any{"window": "Terminal"},
	}}}); err != nil {
		t.Fatalf("Register(desktop) error = %v", err)
	}
	gw.capabilities = reg
	gw.discovery = stubDiscoveryResolver{peers: []discovery.Peer{{
		ID:      "peer-1",
		Name:    "edge-box",
		Address: "10.0.0.9:9443",
		Status:  discovery.StatusOnline,
		SeenAt:  now.Add(-2 * time.Minute),
	}}}

	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/instances status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload instancesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 4 {
		t.Fatalf("count = %d, want 4", payload.Count)
	}

	seenKinds := map[string]bool{}
	for _, item := range payload.Items {
		seenKinds[item.Kind] = true
	}
	for _, kind := range []string{"websocket", "node", "capability-session", "peer"} {
		if !seenKinds[kind] {
			t.Fatalf("missing instance kind %q in %#v", kind, payload.Items)
		}
	}
}

func TestHandleDiscoveryPeersUsesCanonicalItemsShape(t *testing.T) {
	gw := newTestGatewayFull(t)
	gw.discovery = stubDiscoveryResolver{peers: []discovery.Peer{{
		ID:      "peer-1",
		Name:    "edge-box",
		Address: "10.0.0.9:9443",
		Status:  discovery.StatusOnline,
	}}}

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/discovery/peers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/discovery/peers status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []discovery.Peer `json:"items"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode discovery peers response: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("unexpected discovery peers payload: %#v", payload)
	}
	if payload.Items[0].ID != "peer-1" {
		t.Fatalf("unexpected discovery peer ids: %#v", payload)
	}
}

func TestHandleDiscoveryStatusReflectsResolverAvailability(t *testing.T) {
	gw := newTestGatewayFull(t)

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/discovery/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/discovery/status status = %d body=%s", rec.Code, rec.Body.String())
	}
	var initial discoveryStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode discovery status response: %v", err)
	}
	if initial.MDNS {
		t.Fatalf("expected mdns=false without resolver, got %#v", initial)
	}

	gw.discovery = stubDiscoveryResolver{}
	rec = doRequest(t, gw.Handler(), http.MethodGet, "/operator/discovery/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/discovery/status with resolver status = %d body=%s", rec.Code, rec.Body.String())
	}
	var configured discoveryStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &configured); err != nil {
		t.Fatalf("decode configured discovery status response: %v", err)
	}
	if !configured.MDNS {
		t.Fatalf("expected mdns=true with resolver, got %#v", configured)
	}
}
