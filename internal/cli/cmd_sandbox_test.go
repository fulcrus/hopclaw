package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunSandboxImagesSurfacesHTMLGatewayErrors(t *testing.T) {
	old := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) {
		return &GatewayClient{
			BaseURL: "http://gateway.test",
			HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader("<html><head><title>Docker Error</title></head><body>daemon unavailable</body></html>")),
				}, nil
			})},
		}, nil
	}
	t.Cleanup(func() { newGatewayClient = old })

	err := runSandboxImages(context.Background())
	if err == nil {
		t.Fatal("expected sandbox images error")
	}
	if !strings.Contains(err.Error(), "gateway returned HTML instead of JSON") {
		t.Fatalf("error = %v", err)
	}
}
