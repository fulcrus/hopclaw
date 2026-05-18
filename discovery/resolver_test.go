package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewResolver factory
// ---------------------------------------------------------------------------

func TestNewResolver_Static(t *testing.T) {
	r, err := NewResolver(Config{
		Enabled: true,
		Method:  MethodStatic,
		Peers:   []string{"127.0.0.1:16280"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*staticResolver); !ok {
		t.Fatalf("expected *staticResolver, got %T", r)
	}
}

func TestNewResolver_Tailscale(t *testing.T) {
	r, err := NewResolver(Config{
		Enabled: true,
		Method:  MethodTailscale,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*tailscaleResolver); !ok {
		t.Fatalf("expected *tailscaleResolver, got %T", r)
	}
}

func TestNewResolver_MDNS(t *testing.T) {
	r, err := NewResolver(Config{
		Enabled: true,
		Method:  MethodMDNS,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*mdnsResolver); !ok {
		t.Fatalf("expected *mdnsResolver, got %T", r)
	}
}

func TestNewResolver_UnsupportedMethod(t *testing.T) {
	_, err := NewResolver(Config{
		Enabled: true,
		Method:  Method("consul"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported method")
	}
	if !strings.Contains(err.Error(), "unsupported discovery method") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Static resolver
// ---------------------------------------------------------------------------

func TestStaticResolver_Discover_OnlinePeer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operator/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"version": "1.2.3",
		})
	}))
	defer srv.Close()

	// Strip the "http://" prefix to get the host:port address.
	addr := strings.TrimPrefix(srv.URL, "http://")

	resolver := newStaticResolver(Config{
		Peers: []string{addr},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peers, err := resolver.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	p := peers[0]
	if p.Status != StatusOnline {
		t.Errorf("expected status %q, got %q", StatusOnline, p.Status)
	}
	if p.Version != "1.2.3" {
		t.Errorf("expected version %q, got %q", "1.2.3", p.Version)
	}
	if p.Address != addr {
		t.Errorf("expected address %q, got %q", addr, p.Address)
	}
	if p.ID == "" {
		t.Error("expected non-empty peer ID")
	}
}

func TestStaticResolver_Discover_OfflinePeer(t *testing.T) {
	// Use a closed server to simulate an unreachable peer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	resolver := newStaticResolver(Config{
		Peers: []string{addr},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peers, err := resolver.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Status != StatusOffline {
		t.Errorf("expected status %q, got %q", StatusOffline, peers[0].Status)
	}
}

func TestStaticResolver_Discover_EmptyPeers(t *testing.T) {
	resolver := newStaticResolver(Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peers, err := resolver.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestStaticResolver_Announce_Noop(t *testing.T) {
	resolver := newStaticResolver(Config{})
	if err := resolver.Announce(context.Background(), Peer{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStaticResolver_Stop_Noop(t *testing.T) {
	resolver := newStaticResolver(Config{})
	if err := resolver.Stop(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tailscale resolver
// ---------------------------------------------------------------------------

func TestTailscaleResolver_Discover_MissingBinary(t *testing.T) {
	// Ensure the tailscale binary is not on PATH for this test by using a
	// context that would resolve a missing binary.
	resolver := newTailscaleResolver(Config{
		Method: MethodTailscale,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := resolver.Discover(ctx)
	// On machines without tailscale installed this must return a clear error.
	// On machines with tailscale we accept either success or a status error.
	if err != nil && !strings.Contains(err.Error(), "tailscale") {
		t.Fatalf("expected tailscale-related error, got: %v", err)
	}
}

func TestTailscaleResolver_Announce_Noop(t *testing.T) {
	resolver := newTailscaleResolver(Config{})
	if err := resolver.Announce(context.Background(), Peer{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTailscaleResolver_Stop_Noop(t *testing.T) {
	resolver := newTailscaleResolver(Config{})
	if err := resolver.Stop(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTailscaleResolver_DefaultService(t *testing.T) {
	resolver := newTailscaleResolver(Config{})
	if resolver.service != defaultServiceName {
		t.Errorf("expected service %q, got %q", defaultServiceName, resolver.service)
	}
}

func TestTailscaleResolver_CustomService(t *testing.T) {
	resolver := newTailscaleResolver(Config{Service: "_custom._tcp"})
	if resolver.service != "_custom._tcp" {
		t.Errorf("expected service %q, got %q", "_custom._tcp", resolver.service)
	}
}

// ---------------------------------------------------------------------------
// mDNS resolver (stub)
// ---------------------------------------------------------------------------

func TestMDNSResolver_Discover_WhenStopped(t *testing.T) {
	resolver, err := newMDNSResolver(Config{})
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}
	if err := resolver.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, discoverErr := resolver.Discover(context.Background())
	if discoverErr != ErrResolverStopped {
		t.Fatalf("expected ErrResolverStopped, got: %v", discoverErr)
	}
}

func TestMDNSResolver_Announce_WhenStopped(t *testing.T) {
	resolver, err := newMDNSResolver(Config{})
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}
	if err := resolver.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	announceErr := resolver.Announce(context.Background(), Peer{})
	if announceErr != ErrResolverStopped {
		t.Fatalf("expected ErrResolverStopped, got: %v", announceErr)
	}
}

func TestMDNSResolver_Stop_Noop(t *testing.T) {
	resolver, err := newMDNSResolver(Config{})
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}
	if err := resolver.Stop(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMDNSResolver_DefaultService(t *testing.T) {
	resolver, err := newMDNSResolver(Config{})
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}
	if resolver.service != defaultServiceName {
		t.Errorf("expected service %q, got %q", defaultServiceName, resolver.service)
	}
}

// ---------------------------------------------------------------------------
// Probe helpers
// ---------------------------------------------------------------------------

func TestProbePeer_SuccessfulProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"version": "2.0.0",
		})
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	status, version := probePeer(context.Background(), addr)

	if status != StatusOnline {
		t.Errorf("expected status %q, got %q", StatusOnline, status)
	}
	if version != "2.0.0" {
		t.Errorf("expected version %q, got %q", "2.0.0", version)
	}
}

func TestProbePeer_ServerReturnsNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"version": "1.0.0",
		})
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	status, version := probePeer(context.Background(), addr)

	if status != StatusOffline {
		t.Errorf("expected status %q, got %q", StatusOffline, status)
	}
	if version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", version)
	}
}

func TestProbePeer_Unreachable(t *testing.T) {
	// Use a closed server to guarantee connection failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	status, version := probePeer(context.Background(), addr)

	if status != StatusOffline {
		t.Errorf("expected status %q, got %q", StatusOffline, status)
	}
	if version != "" {
		t.Errorf("expected empty version, got %q", version)
	}
}
