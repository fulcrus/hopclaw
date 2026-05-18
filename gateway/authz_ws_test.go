package gateway

import (
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/authz"
)

func TestAccessRequirementForRequestUsesOperatorWebSocketContract(t *testing.T) {
	t.Parallel()

	canonical := httptest.NewRequest("GET", "https://gateway.example.com"+operatorWebSocketPath, nil)

	canonicalReq := accessRequirementForRequest(canonical)

	if canonicalReq.resource != authz.ResourceRuns || canonicalReq.action != authz.ActionExecute {
		t.Fatalf("canonical operator websocket requirement = %#v, want runs execute", canonicalReq)
	}
}
