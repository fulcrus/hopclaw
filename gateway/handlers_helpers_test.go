package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

type managedHelpersStub struct {
	browser      HelperState
	desktop      HelperState
	statusErr    error
	reclaimErr   error
	reclaimCalls []string
}

func (s *managedHelpersStub) Status(context.Context) (HelperState, HelperState, error) {
	if s.statusErr != nil {
		return HelperState{}, HelperState{}, s.statusErr
	}
	return s.browser, s.desktop, nil
}

func (s *managedHelpersStub) Reclaim(_ context.Context, name string) error {
	s.reclaimCalls = append(s.reclaimCalls, name)
	if s.reclaimErr != nil {
		return s.reclaimErr
	}
	return nil
}

func TestHandleHelpersStatusReturnsUnavailableItemsWhenHelpersMissing(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/helpers/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/helpers/status status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload helperStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(status) error = %v", err)
	}
	if payload.Browser.Status != "unavailable" {
		t.Fatalf("browser status = %q, want unavailable", payload.Browser.Status)
	}
	if payload.Desktop.Status != "unavailable" {
		t.Fatalf("desktop status = %q, want unavailable", payload.Desktop.Status)
	}
	if len(payload.Helpers) != 2 {
		t.Fatalf("helpers len = %d, want 2", len(payload.Helpers))
	}
	if payload.Helpers[0].Name != "browser" || payload.Helpers[0].Status != "unavailable" {
		t.Fatalf("helpers[0] = %#v, want browser unavailable", payload.Helpers[0])
	}
	if payload.Helpers[1].Name != "desktop" || payload.Helpers[1].Status != "unavailable" {
		t.Fatalf("helpers[1] = %#v, want desktop unavailable", payload.Helpers[1])
	}
}

func TestHandleHelpersStatusIncludesNamedHelperList(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetManagedHelpers(&managedHelpersStub{
		browser: HelperState{
			Status:         "running",
			SessionCount:   2,
			LastUseAt:      "2026-03-26T10:30:00Z",
			IdleTimeoutSec: 90,
		},
		desktop: HelperState{
			Status:         "stopped",
			LastUseAt:      "2026-03-26T09:15:00Z",
			IdleTimeoutSec: 300,
		},
	})

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/helpers/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/helpers/status status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload helperStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(status) error = %v", err)
	}
	if len(payload.Helpers) != 2 {
		t.Fatalf("helpers len = %d, want 2", len(payload.Helpers))
	}
	if payload.Helpers[0].Name != "browser" || payload.Helpers[0].SessionCount != 2 {
		t.Fatalf("helpers[0] = %#v, want named browser state", payload.Helpers[0])
	}
	if payload.Helpers[1].Name != "desktop" || payload.Helpers[1].IdleTimeoutSec != 300 {
		t.Fatalf("helpers[1] = %#v, want named desktop state", payload.Helpers[1])
	}
}

func TestHandleHelpersReclaimRejectsMissingName(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetManagedHelpers(&managedHelpersStub{})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/helpers/reclaim missing name status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v", err)
	}
	if payload.Code != string(apiresponse.ErrorCodeInvalidArgument) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeInvalidArgument)
	}
	if payload.Error == "" {
		t.Fatal("expected non-empty structured error message")
	}
}

func TestHandleHelpersReclaimRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	stub := &managedHelpersStub{}
	gw.SetManagedHelpers(stub)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{"name":"browser"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/helpers/reclaim trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.reclaimCalls) != 0 {
		t.Fatalf("reclaim calls = %#v, want none", stub.reclaimCalls)
	}
}

func TestHandleHelpersReclaimTrimsAndNormalizesName(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	stub := &managedHelpersStub{}
	gw.SetManagedHelpers(stub)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{"name":" Desktop "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/helpers/reclaim status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.reclaimCalls) != 1 || stub.reclaimCalls[0] != "desktop" {
		t.Fatalf("reclaim calls = %#v, want [desktop]", stub.reclaimCalls)
	}

	var payload namedOKResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(reclaim) error = %v", err)
	}
	if payload.Name != "desktop" {
		t.Fatalf("response name = %q, want desktop", payload.Name)
	}
}

func TestHandleHelpersReclaimRejectsUnsupportedName(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	stub := &managedHelpersStub{}
	gw.SetManagedHelpers(stub)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{"name":"camera"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/helpers/reclaim invalid name status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.reclaimCalls) != 0 {
		t.Fatalf("reclaim calls = %#v, want none", stub.reclaimCalls)
	}
}

func TestHandleHelpersReclaimReturnsServiceUnavailableWhenHelpersMissing(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{"name":"browser"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /operator/helpers/reclaim unavailable status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelpersReclaimPropagatesControllerError(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	stub := &managedHelpersStub{reclaimErr: errors.New("stop failed")}
	gw.SetManagedHelpers(stub)

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/helpers/reclaim", `{"name":"browser"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /operator/helpers/reclaim controller error status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.reclaimCalls) != 1 || stub.reclaimCalls[0] != "browser" {
		t.Fatalf("reclaim calls = %#v, want [browser]", stub.reclaimCalls)
	}
}
