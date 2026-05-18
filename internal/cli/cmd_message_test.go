package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRunMessageSendUsesRunCompletionOutput(t *testing.T) {
	client := &GatewayClient{
		BaseURL: "http://gateway.test",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/runtime/runs":
				return jsonHTTPResponse(http.StatusAccepted, `{"id":"run-1","session_id":"sess-1","status":"completed"}`), nil
			case "/runtime/runs/run-1/completion":
				return jsonHTTPResponse(http.StatusOK, `{"bundle":{"final_text":"hello from completion"}}`), nil
			default:
				t.Fatalf("unexpected request path %q", req.URL.String())
				return nil, nil
			}
		})},
	}

	restore := captureStdout(t)
	if err := runMessageSendWithClient(context.Background(), client, "cli", "cli", "hello", nil); err != nil {
		t.Fatalf("runMessageSendWithClient() error = %v", err)
	}
	if got := strings.TrimSpace(restore()); got != "hello from completion" {
		t.Fatalf("stdout = %q, want %q", got, "hello from completion")
	}
}

func TestRunMessageReadRequestsMessagesPayload(t *testing.T) {
	client := &GatewayClient{
		BaseURL: "http://gateway.test",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/runtime/sessions/sess-1" {
				t.Fatalf("unexpected path %q", req.URL.Path)
			}
			if req.URL.RawQuery != "include=messages" {
				t.Fatalf("RawQuery = %q, want include=messages", req.URL.RawQuery)
			}
			return jsonHTTPResponse(http.StatusOK, `{"id":"sess-1","key":"cli:default","messages":[{"role":"assistant","content":"read ok"}]}`), nil
		})},
	}

	old := newGatewayClient
	newGatewayClient = func() (*GatewayClient, error) { return client, nil }
	t.Cleanup(func() { newGatewayClient = old })

	restore := captureStdout(t)
	if err := runMessageRead(context.Background(), "sess-1"); err != nil {
		t.Fatalf("runMessageRead() error = %v", err)
	}
	if got := restore(); !strings.Contains(got, "read ok") {
		t.Fatalf("stdout = %q, want message content", got)
	}
}
