package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	captypes "github.com/fulcrus/hopclaw/capability/types"
)

func TestGatewayCapabilitySessionRoutesContract(t *testing.T) {
	reg := testRegistry(t)
	if err := reg.Register(&stubSessionCapability{
		manifest: captypes.Manifest{Name: "browser", Kind: captypes.KindSession},
		sessions: []*captypes.SessionHandle{{
			ID:         "test-session",
			Capability: "browser",
		}},
	}); err != nil {
		t.Fatalf("Register(browser) error = %v", err)
	}
	gw := newTestGateway(t, Config{
		AuthToken:    "test-token",
		Capabilities: reg,
	})
	handler := gw.Handler()

	listReq := httptest.NewRequest(http.MethodGet, "/operator/capabilities/browser/sessions", nil)
	listReq.Header.Set("Authorization", "Bearer test-token")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /operator/capabilities/browser/sessions status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	var payload countedItemsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode sessions payload: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("payload.Count = %d, want 1", payload.Count)
	}

	closeReq := httptest.NewRequest(http.MethodDelete, "/operator/capabilities/browser/sessions/test-session", nil)
	closeReq.Header.Set("Authorization", "Bearer test-token")
	closeRec := httptest.NewRecorder()
	handler.ServeHTTP(closeRec, closeReq)
	if closeRec.Code != http.StatusOK {
		t.Fatalf("DELETE /operator/capabilities/browser/sessions/{id} status = %d body=%s", closeRec.Code, closeRec.Body.String())
	}

	var ok capabilitySessionOKResponse
	if err := json.Unmarshal(closeRec.Body.Bytes(), &ok); err != nil {
		t.Fatalf("decode close payload: %v", err)
	}
	if !ok.OK || ok.Capability != "browser" || ok.SessionID != "test-session" {
		t.Fatalf("unexpected close payload: %#v", ok)
	}
}

func TestGatewaySurfaceHandlersFallbackContract(t *testing.T) {
	t.Parallel()

	gw := &Gateway{}
	for _, handler := range []http.Handler{gw.publicSurfaceHandler(), gw.runtimeSurfaceHandler()} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/missing", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("surface fallback status = %d, want 404", rec.Code)
		}
	}
}
