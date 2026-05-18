package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONBodyRejectsOversizedRequest(t *testing.T) {
	t.Parallel()

	body := `{"allow_users":["` + strings.Repeat("a", configMaxBodySize) + `"]}`
	req := httptest.NewRequest("POST", "/operator/allowlist/test", strings.NewReader(body))
	rec := httptest.NewRecorder()

	var payload allowlistSetRequest
	err := decodeJSONBody(rec, req, &payload)
	if err == nil {
		t.Fatal("expected oversized request to fail")
	}
	if err != errRequestBodyTooLarge {
		t.Fatalf("err = %v, want errRequestBodyTooLarge", err)
	}
}

func TestDecodeOptionalJSONBodyAllowsEmptyBody(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/operator/governance/deliveries/redrive", nil)
	rec := httptest.NewRecorder()

	var payload struct {
		IDs []string `json:"ids"`
	}
	hasBody, err := decodeOptionalJSONBody(rec, req, &payload)
	if err != nil {
		t.Fatalf("decodeOptionalJSONBody() error = %v", err)
	}
	if hasBody {
		t.Fatal("expected empty body to report hasBody=false")
	}
	if len(payload.IDs) != 0 {
		t.Fatalf("payload = %#v, want empty", payload)
	}
}

func TestDecodeOptionalJSONBodyRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/operator/governance/deliveries/redrive", strings.NewReader(`{"ids":["one"]} {"extra":true}`))
	rec := httptest.NewRecorder()

	var payload struct {
		IDs []string `json:"ids"`
	}
	_, err := decodeOptionalJSONBody(rec, req, &payload)
	if err == nil {
		t.Fatal("expected trailing json to fail")
	}
}
