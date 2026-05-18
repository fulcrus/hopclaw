package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
)

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

// newTestGatewayFull creates a Gateway wired with in-memory stores suitable
// for handler-level testing. The caller may further configure the gateway via
// its Set* methods before calling Handler().
func newTestGatewayFull(t *testing.T) *Gateway {
	t.Helper()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	bus := eventbus.NewInMemoryBus()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, bus, nil)
	srv := server.New(svc, server.Config{AuthToken: "test-token"})
	cfg := Config{
		AuthToken: "test-token",
		Runtime:   svc,
	}
	return gatewayFromServer(srv, cfg)
}

func gatewayFromServer(srv *server.Server, cfg Config) *Gateway {
	if srv == nil {
		return New(nil, nil, cfg)
	}
	return New(srv.PublicHandler(), srv.RuntimeHandler(), cfg)
}

type operationalWarningSourceStub struct {
	warnings []controlplane.OperationalWarning
}

func (s operationalWarningSourceStub) OperationalWarnings() []controlplane.OperationalWarning {
	return append([]controlplane.OperationalWarning(nil), s.warnings...)
}

// doRequest is a convenience wrapper around httptest. It builds an HTTP
// request, executes it against handler, and returns the recorded response.
// body may be nil for methods without a request body.
func doRequest(t *testing.T, handler http.Handler, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Authorization", "Bearer test-token")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func scopedAuthContext(subject string) context.Context {
	return contextWithAuthIdentity(context.Background(), &AuthIdentity{
		Subject:  subject,
		Provider: "jwt",
	})
}

func scopedAutomationAuthContext(subject string, automationIDs ...string) context.Context {
	scopes := make([]string, 0, len(automationIDs))
	for _, id := range automationIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			scopes = append(scopes, "automation:"+trimmed)
		}
	}
	return contextWithAuthIdentity(context.Background(), &AuthIdentity{
		Subject:  subject,
		Provider: "jwt",
		Scopes:   scopes,
	})
}

// makeUnauthRequest creates an httptest request without auth headers.
func makeUnauthRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}
	return httptest.NewRequest(method, path, bodyReader)
}

// captureResponse runs the handler and returns the recorded response.
func captureResponse(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
