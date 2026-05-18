package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestOperatorMetricsRouteAvailable(t *testing.T) {
	gw := newTestGatewayFull(t)

	req := httptest.NewRequest(http.MethodGet, "/operator/metrics", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	gw.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestOperatorMetricsRouteHonorsGatewayAuthorization(t *testing.T) {
	handler := newTestGateway(t, Config{
		AuthConfig: config.AuthConfig{
			APIKeys: []config.AuthKeyEntry{
				{Key: "viewer-key", Name: "viewer", Enabled: true, Scopes: []string{"rbac:viewer"}},
			},
			RBAC: config.AuthRBACConfig{
				ScopePrefixes: []string{"rbac:"},
				Roles: []config.AuthRBACRoleConfig{
					{
						Name: "viewer",
						Grants: []config.AuthRBACGrantConfig{
							{Resource: "runs", Permissions: []string{"read"}},
						},
					},
				},
			},
		},
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/operator/metrics", nil)
	req.Header.Set("X-API-Key", "viewer-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
