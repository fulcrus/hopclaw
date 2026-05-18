package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	captypes "github.com/fulcrus/hopclaw/capability/types"
)

func TestCloseSessionKeepsTrackedHandleOnRemoteFailure(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1":
			var req browsertypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			switch req.Action {
			case browsertypes.ActionCreateSession:
				_ = json.NewEncoder(w).Encode(browsertypes.Response{
					OK:        true,
					SessionID: "browser-session-1",
					Data: map[string]any{
						"session_id": "browser-session-1",
					},
				})
			case browsertypes.ActionCloseSession:
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(browsertypes.Response{
					OK:    false,
					Error: "remote close failed",
				})
			default:
				t.Fatalf("unexpected action %q", req.Action)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	if handle.ID != "browser-session-1" {
		t.Fatalf("handle.ID = %q", handle.ID)
	}

	err = capability.CloseSession(context.Background(), handle.ID)
	if err == nil || err.Error() != "browser host: remote close failed" {
		t.Fatalf("CloseSession() error = %v", err)
	}

	sessions := capability.ListSessions()
	if len(sessions) != 1 || sessions[0].ID != handle.ID {
		t.Fatalf("ListSessions() = %#v", sessions)
	}
}

func TestCloseSessionRemovesTrackedHandleOnSuccess(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1":
			var req browsertypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(browsertypes.Response{
				OK:        true,
				SessionID: "browser-session-1",
				Data: map[string]any{
					"session_id": "browser-session-1",
					"closed":     req.Action == browsertypes.ActionCloseSession,
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), nil)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	if err := capability.CloseSession(context.Background(), handle.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if sessions := capability.ListSessions(); len(sessions) != 0 {
		t.Fatalf("ListSessions() = %#v", sessions)
	}
}

func TestInvokeHonorsConfiguredTimeout(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1":
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(browsertypes.Response{
				OK:   true,
				Data: map[string]any{"ok": true},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL, Timeout: 20 * time.Millisecond})
	_, err := capability.Invoke(context.Background(), captypes.InvokeRequest{
		Operation: "click",
		SessionID: "browser-session-1",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("Invoke() error = %v, want timeout", err)
	}
}

func TestInvokeUsesLongerTimeoutForBrowserCaptures(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1":
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(browsertypes.Response{
				OK:   true,
				Data: map[string]any{"ok": true},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL, Timeout: 20 * time.Millisecond})
	result, err := capability.Invoke(context.Background(), captypes.InvokeRequest{
		Operation: browsertypes.ActionScreenshotLabeled,
		SessionID: "browser-session-1",
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("Invoke() result = %#v, want ok result", result)
	}
}

func TestInvokeAttachesBrowserProfileTelemetryAndTracksSessionState(t *testing.T) {
	t.Parallel()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/browser/v1":
			var req browsertypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			switch req.Action {
			case browsertypes.ActionCreateSession:
				if req.Params["route_profile_id"] != "douyin.site" {
					t.Fatalf("create_session route_profile_id = %#v", req.Params["route_profile_id"])
				}
				if req.Params["preferred_transport"] != capprofile.TransportBrowserNavigation {
					t.Fatalf("create_session preferred_transport = %#v", req.Params["preferred_transport"])
				}
				_ = json.NewEncoder(w).Encode(browsertypes.Response{
					OK:        true,
					SessionID: "browser-session-1",
					Data: map[string]any{
						"session_id": "browser-session-1",
						"url":        "https://www.douyin.com/",
						"title":      "抖音",
					},
				})
			case browsertypes.ActionNavigate:
				if req.Params["route_profile_id"] != "douyin.site" {
					t.Fatalf("navigate route_profile_id = %#v", req.Params["route_profile_id"])
				}
				if req.Params["preferred_transport"] != capprofile.TransportBrowserNavigation {
					t.Fatalf("navigate preferred_transport = %#v", req.Params["preferred_transport"])
				}
				_ = json.NewEncoder(w).Encode(browsertypes.Response{
					OK: true,
					Data: map[string]any{
						"url":   "https://www.douyin.com/search/%E5%88%98%E5%BE%B7%E5%8D%8E",
						"title": "刘德华 - 抖音",
					},
				})
			default:
				t.Fatalf("unexpected action %q", req.Action)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer host.Close()

	capability := New(Config{BaseURL: host.URL})
	handle, err := capability.OpenSession(context.Background(), map[string]any{
		"url": "https://www.douyin.com/",
	})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	if handle.ID != "browser-session-1" {
		t.Fatalf("handle.ID = %q", handle.ID)
	}

	result, err := capability.Invoke(context.Background(), captypes.InvokeRequest{
		Operation: browsertypes.ActionNavigate,
		SessionID: handle.ID,
		Params: map[string]any{
			"url": "https://www.douyin.com/search/%E5%88%98%E5%BE%B7%E5%8D%8E",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	trace, ok := capprofile.DecodeExecutionTrace(result.Metadata)
	if !ok {
		t.Fatalf("result.Metadata = %#v", result.Metadata)
	}
	if trace.ProfileID != "douyin.site" {
		t.Fatalf("trace.ProfileID = %q", trace.ProfileID)
	}
	if trace.ChosenTransport != capprofile.TransportBrowserNavigation {
		t.Fatalf("trace.ChosenTransport = %q", trace.ChosenTransport)
	}
	sessions := capability.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() count = %d", len(sessions))
	}
	if sessions[0].Metadata["profile_id"] != "douyin.site" {
		t.Fatalf("session metadata = %#v", sessions[0].Metadata)
	}
	if sessions[0].Metadata["last_transport"] != capprofile.TransportBrowserNavigation {
		t.Fatalf("session metadata last_transport = %#v", sessions[0].Metadata["last_transport"])
	}
}
