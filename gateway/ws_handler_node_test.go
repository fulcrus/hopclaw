package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/deviceauth"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
	"github.com/fulcrus/hopclaw/nodeclient"
)

func TestDevicePairClaimRegistersNodeAndHandlesInvoke(t *testing.T) {
	gw := newTestGatewayFull(t)
	store := deviceauth.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	pairing := deviceauth.NewPairingManager(store)
	gw.SetDeviceAuth(store, pairing)
	nodeRegistry := gatewaynodes.NewRegistry()
	defer nodeRegistry.Stop()
	gw.SetWSHandler(NewWSHandler(gw, nodeRegistry))

	srv := httptest.NewServer(gw.Handler())
	defer srv.Close()

	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/operator/devices/pair", bytes.NewBufferString(`{"device_id":"node-1","channel":"desktop","name":"Node"}`))
	createReq.Header.Set("Authorization", "Bearer test-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := srv.Client().Do(createReq)
	if err != nil {
		t.Fatalf("create pairing request failed: %v", err)
	}
	defer createResp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	code, _ := created["code"].(string)
	if code == "" {
		t.Fatalf("pairing code missing in %#v", created)
	}

	claim, err := nodeclient.ClaimPairing(context.Background(), srv.URL, nodeclient.PairClaimRequest{
		Code:         code,
		DeviceID:     "node-1",
		Name:         "Node",
		Platform:     "linux",
		DeviceFamily: "desktop",
		Role:         string(deviceauth.RoleNode),
	})
	if err != nil {
		t.Fatalf("ClaimPairing() error = %v", err)
	}
	if claim.Token == "" {
		t.Fatalf("claim token missing in %#v", claim)
	}
	if claim.WSURL == "" {
		t.Fatalf("claim websocket url missing in %#v", claim)
	}

	node := nodeclient.New(nodeclient.Config{
		WebSocketURL: claim.WSURL,
		DeviceID:     claim.DeviceID,
		Token:        claim.Token,
		Role:         deviceauth.RoleNode,
		ClientID:     "test-node",
		ClientMode:   "node",
		NodeID:       claim.DeviceID,
		Platform:     "Linux",
		DeviceFamily: "desktop",
		Version:      "1.0.0",
		Commands:     []string{"device.info", "desktop.list_apps"},
	})
	node.Register("device.info", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"device_id": claim.DeviceID, "kind": "test"}, nil
	})
	node.Register("desktop.list_apps", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"apps": []any{map[string]any{"name": "Terminal"}}}, nil
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = node.Run(runCtx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sessions := nodeRegistry.List(); len(sessions) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := len(nodeRegistry.List()); got != 1 {
		t.Fatalf("connected nodes = %d, want 1", got)
	}

	resp, err := nodeRegistry.Invoke(gatewaynodes.NodeInvokeRequest{NodeID: claim.DeviceID, Command: "device.info"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("Invoke() ok = false error=%q", resp.Error)
	}
	if got := resp.Data["device_id"]; got != claim.DeviceID {
		t.Fatalf("device_id = %#v, want %q", got, claim.DeviceID)
	}

	listResp, err := nodeRegistry.Invoke(gatewaynodes.NodeInvokeRequest{NodeID: claim.DeviceID, Command: "desktop.list_apps"})
	if err != nil {
		t.Fatalf("Invoke(list_apps) error = %v", err)
	}
	if !listResp.OK {
		t.Fatalf("Invoke(list_apps) ok = false error=%q", listResp.Error)
	}
}
