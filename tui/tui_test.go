package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Client creation
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	t.Parallel()

	c := NewClient("http://localhost:8080", "my-token")
	if c.baseURL != "http://localhost:8080" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
	if c.authToken != "my-token" {
		t.Fatalf("authToken = %q", c.authToken)
	}
	if c.http == nil {
		t.Fatal("http client is nil")
	}
}

// ---------------------------------------------------------------------------
// Client.GetStatus
// ---------------------------------------------------------------------------

func TestClientGetStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/operator/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get(authHeaderName) != "tok" {
			t.Errorf("auth = %q", r.Header.Get(authHeaderName))
		}
		json.NewEncoder(w).Encode(StatusResponse{
			OK:      true,
			Version: "1.0.0",
			Uptime:  "5m",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	resp, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !resp.OK {
		t.Fatal("OK = false")
	}
	if resp.Version != "1.0.0" {
		t.Fatalf("Version = %q", resp.Version)
	}
}

// ---------------------------------------------------------------------------
// Client.GetCapabilities
// ---------------------------------------------------------------------------

func TestClientGetCapabilities(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operator/capabilities" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(CapabilitiesResponse{
			Items: []CapabilityItem{{Name: "chat", Kind: "channel"}},
			Count: 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.GetCapabilities(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d", resp.Count)
	}
	if resp.Items[0].Name != "chat" {
		t.Fatalf("Items[0].Name = %q", resp.Items[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Client.GetSessions
// ---------------------------------------------------------------------------

func TestClientGetSessions(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/sessions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(SessionsResponse{
			Items: []SessionItem{{ID: "s1", Key: "chat-1"}},
			Count: 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.GetSessions(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d", resp.Count)
	}
}

// ---------------------------------------------------------------------------
// Client.SubmitMessage
// ---------------------------------------------------------------------------

func TestClientSubmitMessage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/runtime/runs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}

		var body SubmitRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.SessionKey != "chat-1" {
			t.Errorf("SessionKey = %q", body.SessionKey)
		}
		if body.Content != "hello" {
			t.Errorf("Content = %q", body.Content)
		}

		json.NewEncoder(w).Encode(RunResponse{
			ID:     "run-1",
			Status: "queued",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.SubmitMessage(context.Background(), "chat-1", "hello")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.ID != "run-1" {
		t.Fatalf("ID = %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Client.GetRun
// ---------------------------------------------------------------------------

func TestClientGetRun(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/runs/run-1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(RunResponse{
			ID:     "run-1",
			Status: "completed",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("Status = %q", resp.Status)
	}
}

// ---------------------------------------------------------------------------
// Client.GetApprovals
// ---------------------------------------------------------------------------

func TestClientGetApprovals(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/operator/approvals") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("status") != "pending" {
			t.Errorf("status = %q", r.URL.Query().Get("status"))
		}
		json.NewEncoder(w).Encode(ApprovalsResponse{
			Items: []ApprovalTicket{{ID: "a1", Status: "pending"}},
			Count: 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.GetApprovals(context.Background(), "pending")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d", resp.Count)
	}
}

func TestClientGetApprovalsNoStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %q", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(ApprovalsResponse{Count: 0})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.GetApprovals(context.Background(), "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client.ResolveApproval
// ---------------------------------------------------------------------------

func TestClientResolveApproval(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/operator/approvals/a1/resolve" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body resolveApprovalRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.Status != "approved" {
			t.Errorf("Status = %q", body.Status)
		}
		if body.By != "tui-operator" {
			t.Errorf("By = %q", body.By)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	err := c.ResolveApproval(context.Background(), "a1", "approved", "run", "looks good")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client.GetRuns
// ---------------------------------------------------------------------------

func TestClientGetRuns(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/runs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(RunsResponse{
			Items: []RunItem{{ID: "r1", Status: "completed"}},
			Count: 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.GetRuns(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d", resp.Count)
	}
}

// ---------------------------------------------------------------------------
// Client error handling
// ---------------------------------------------------------------------------

func TestClientHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal failure"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "internal failure") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientHTTPErrorRawBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("raw error text"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "raw error text") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientCancelledContext(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(StatusResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetStatus(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Client.url helper
// ---------------------------------------------------------------------------

func TestClientURL(t *testing.T) {
	t.Parallel()

	c := NewClient("http://localhost:8080", "")

	if got := c.url("/status"); got != "http://localhost:8080/status" {
		t.Fatalf("url = %q", got)
	}
	if got := c.url("status"); got != "http://localhost:8080/status" {
		t.Fatalf("url = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Client.CreateSession
// ---------------------------------------------------------------------------

func TestClientCreateSession(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		var body CreateSessionRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.SessionKey != "new-session" {
			t.Errorf("SessionKey = %q", body.SessionKey)
		}
		if body.Model != "sonnet-4.6" {
			t.Errorf("Model = %q", body.Model)
		}
		json.NewEncoder(w).Encode(RunResponse{ID: "run-new", Status: "queued"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	resp, err := c.CreateSession(context.Background(), "new-session", "sonnet-4.6")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.ID != "run-new" {
		t.Fatalf("ID = %q", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// Client.CancelApproval
// ---------------------------------------------------------------------------

func TestClientCancelApproval(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/operator/approvals/a1/cancel" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	err := c.CancelApproval(context.Background(), "a1")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}
