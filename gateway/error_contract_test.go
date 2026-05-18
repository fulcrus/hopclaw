package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func TestGatewayErrorIncludesCanonicalCode(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	gwError(rec, http.StatusServiceUnavailable, "runtime not available")

	payload := decodeGatewayErrorPayload(t, rec)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if payload.Code != string(apiresponse.ErrorCodeServiceUnavailable) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeServiceUnavailable)
	}
	if payload.Error == "" {
		t.Fatal("expected non-empty structured error message")
	}
}

func TestWriteAuthErrorIncludesCanonicalCode(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeAuthError(context.Background(), rec, "missing or invalid auth credentials")

	payload := decodeGatewayErrorPayload(t, rec)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if payload.Code != string(apiresponse.ErrorCodeUnauthenticated) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeUnauthenticated)
	}
	if payload.Error == "" {
		t.Fatal("expected non-empty structured error message")
	}
}

func TestWriteAuthorizationErrorIncludesCanonicalCode(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeAuthorizationError(rec, "insufficient permissions")

	payload := decodeGatewayErrorPayload(t, rec)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if payload.Code != string(apiresponse.ErrorCodeAuthorizationDenied) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeAuthorizationDenied)
	}
	if payload.Error == "" {
		t.Fatal("expected non-empty structured error message")
	}
}

func TestHandleGatewayJSONDecodeErrorUsesInvalidJSONCode(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	ok := handleGatewayJSONDecodeError(rec, json.Unmarshal([]byte(`{"x":`), &map[string]any{}))
	if ok {
		t.Fatal("handleGatewayJSONDecodeError() = true, want false")
	}

	payload := decodeGatewayErrorPayload(t, rec)
	if payload.Code != string(apiresponse.ErrorCodeInvalidJSON) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeInvalidJSON)
	}
}

func TestHandleGatewayJSONDecodeErrorUsesPayloadTooLargeCode(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	ok := handleGatewayJSONDecodeError(rec, errRequestBodyTooLarge)
	if ok {
		t.Fatal("handleGatewayJSONDecodeError() = true, want false")
	}

	payload := decodeGatewayErrorPayload(t, rec)
	if payload.Code != string(apiresponse.ErrorCodeRequestBodyTooLarge) {
		t.Fatalf("code = %q, want %q", payload.Code, apiresponse.ErrorCodeRequestBodyTooLarge)
	}
}

func decodeGatewayErrorPayload(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()

	var payload errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode gateway error payload: %v body=%s", err, rec.Body.String())
	}
	return payload
}
