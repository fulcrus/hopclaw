package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/deviceauth"
)

func newTestGatewayWithDeviceAuth(t *testing.T) (*Gateway, *deviceauth.Store, *deviceauth.PairingManager) {
	t.Helper()

	gw := newTestGatewayFull(t)
	store := deviceauth.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}
	pairing := deviceauth.NewPairingManager(store)
	gw.SetDeviceAuth(store, pairing)
	return gw, store, pairing
}

func createDevicePairForTest(t *testing.T, handler http.Handler, deviceID, channel string) devicePairCreateResponse {
	t.Helper()

	rec := doRequest(t, handler, http.MethodPost, "/operator/devices/pair", fmt.Sprintf(`{
		"device_id":%q,
		"channel":%q
	}`, deviceID, channel))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair status = %d body=%s", rec.Code, rec.Body.String())
	}

	var created devicePairCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	if created.Code == "" {
		t.Fatal("expected pairing code")
	}
	return created
}

func TestHandleDevicePairClaimForcesNodeRoleAndBuildsWSURL(t *testing.T) {
	t.Parallel()

	gw, store, _ := newTestGatewayWithDeviceAuth(t)

	create := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair", `{
		"device_id":"node-2",
		"name":"Worker Node",
		"platform":"linux",
		"device_family":"desktop",
		"channel":"desktop"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair status = %d body=%s", create.Code, create.Body.String())
	}

	var created devicePairCreateResponse
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	if created.Code == "" {
		t.Fatal("expected pairing code")
	}

	req := makeUnauthRequest(t, http.MethodPost, "/device/pair/claim", fmt.Sprintf(`{
		"code":%q,
		"device_id":"node-2",
		"name":"Worker Node",
		"platform":"linux",
		"device_family":"desktop",
		"role":"viewer",
		"scopes":["nodes.read"]
	}`, created.Code))
	req.Host = "gateway.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")

	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /device/pair/claim status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp devicePairClaimResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal(claim) error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("OK = false, payload = %#v", resp)
	}
	if resp.Role != string(deviceauth.RoleNode) {
		t.Fatalf("Role = %q, want %q", resp.Role, deviceauth.RoleNode)
	}
	if resp.WSURL != "wss://gateway.example.com/operator/ws" {
		t.Fatalf("WSURL = %q, want wss://gateway.example.com/operator/ws", resp.WSURL)
	}
	if _, ok := store.GetToken("node-2", deviceauth.RoleNode); !ok {
		t.Fatal("expected node token to be stored")
	}
	if _, ok := store.GetToken("node-2", deviceauth.RoleViewer); ok {
		t.Fatal("did not expect viewer token to be stored")
	}
}

func TestDevicePairWSURLHonorsForwardedPrefix(t *testing.T) {
	t.Parallel()

	req := makeUnauthRequest(t, http.MethodPost, "/device/pair/claim", "")
	req.Host = "gateway.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/edge")

	if got := devicePairWSURL(req); got != "wss://gateway.example.com/edge/operator/ws" {
		t.Fatalf("devicePairWSURL() = %q, want wss://gateway.example.com/edge/operator/ws", got)
	}
}

func TestHandleDevicesPairRejectByCode(t *testing.T) {
	t.Parallel()

	gw, _, pairing := newTestGatewayWithDeviceAuth(t)

	create := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair", `{
		"device_id":"ios-2",
		"channel":"ios"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST /operator/devices/pair status = %d body=%s", create.Code, create.Body.String())
	}

	var created devicePairCreateResponse
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}

	reject := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair/reject", fmt.Sprintf(`{"code":%q}`, created.Code))
	if reject.Code != http.StatusOK {
		t.Fatalf("POST /operator/devices/pair/reject status = %d body=%s", reject.Code, reject.Body.String())
	}

	if rec, ok := pairing.GetByCode(created.Code); ok || rec != nil {
		t.Fatalf("expected code index to be cleared, got rec=%#v ok=%v", rec, ok)
	}
	pairings := pairing.ListPairings()
	if len(pairings) != 1 || pairings[0].Status != deviceauth.PairingRevoked {
		t.Fatalf("pairings = %#v, want single revoked record", pairings)
	}
}

func TestHandleDevicesPairCreateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, _, _ := newTestGatewayWithDeviceAuth(t)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair", `{"device_id":"ios-3","channel":"ios"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/devices/pair trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDevicesPairApproveRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, _, _ := newTestGatewayWithDeviceAuth(t)
	created := createDevicePairForTest(t, gw.Handler(), "ios-approve", "ios")

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/pair/approve", fmt.Sprintf(`{"code":%q,"role":"viewer"} {"extra":true}`, created.Code))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/devices/pair/approve trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDevicePairClaimRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, _, _ := newTestGatewayWithDeviceAuth(t)
	created := createDevicePairForTest(t, gw.Handler(), "node-claim", "desktop")

	req := makeUnauthRequest(t, http.MethodPost, "/device/pair/claim", fmt.Sprintf(`{"code":%q,"device_id":"node-claim"} {"extra":true}`, created.Code))
	req.Header.Set("Content-Type", "application/json")

	rec := captureResponse(t, gw.Handler(), req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /device/pair/claim trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDevicesTokenRotateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, store, _ := newTestGatewayWithDeviceAuth(t)
	if err := store.RegisterDevice(&deviceauth.DeviceIdentity{
		DeviceID: "ios-rotate",
		Name:     "Rotate Device",
		Trusted:  true,
	}); err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/ios-rotate/tokens/rotate", `{"role":"viewer"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/devices/{id}/tokens/rotate trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDevicesTokenRevokeRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, store, _ := newTestGatewayWithDeviceAuth(t)
	if err := store.RegisterDevice(&deviceauth.DeviceIdentity{
		DeviceID: "ios-revoke",
		Name:     "Revoke Device",
		Trusted:  true,
	}); err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	if err := store.SetToken(&deviceauth.DeviceToken{
		Token:    "token-viewer",
		DeviceID: "ios-revoke",
		Role:     deviceauth.RoleViewer,
	}); err != nil {
		t.Fatalf("SetToken() error = %v", err)
	}

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/devices/ios-revoke/tokens/revoke", `{"role":"viewer"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/devices/{id}/tokens/revoke trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
}
