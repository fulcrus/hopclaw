package nodedaemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/deviceauth"
)

func TestPrepareCarriesClaimedWebSocketURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device/pair/claim" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"device_id":  "node-bootstrap",
			"role":       string(deviceauth.RoleNode),
			"scopes":     []string{"browser.proxy"},
			"token":      "claim-token",
			"ws_url":     "wss://gateway.example.com/operator/ws",
			"expires_at": "2030-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	result, err := Prepare(context.Background(), BootstrapConfig{
		StoreDir:     filepath.Join(t.TempDir(), "store"),
		GatewayURL:   server.URL,
		PairingCode:  "PAIR-123",
		DeviceID:     "node-bootstrap",
		DeviceName:   "Bootstrap Node",
		Platform:     "Linux",
		DeviceFamily: "desktop",
		Role:         deviceauth.RoleNode,
		Scopes:       []string{"browser.proxy"},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result == nil {
		t.Fatal("Prepare() = nil, want bootstrap result")
	}
	if result.WebSocketURL != "wss://gateway.example.com/operator/ws" {
		t.Fatalf("WebSocketURL = %q, want claimed operator websocket URL", result.WebSocketURL)
	}
	if result.Token != "claim-token" {
		t.Fatalf("Token = %q, want claim-token", result.Token)
	}
	if _, ok := result.Store.GetToken("node-bootstrap", deviceauth.RoleNode); !ok {
		t.Fatal("expected claimed token to be persisted in store")
	}
}
