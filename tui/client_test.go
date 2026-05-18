package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDeleteSessionUsesRuntimeEndpoint(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotToken  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotToken = r.Header.Get(authHeaderName)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "secret-token")
	if err := client.DeleteSession(context.Background(), "sess-1"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodDelete)
	}
	if gotPath != "/runtime/sessions/sess-1" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "secret-token" {
		t.Fatalf("token = %q", gotToken)
	}
}
