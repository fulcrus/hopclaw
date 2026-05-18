package discovery

import (
	"context"
	"sync"
	"testing"
)

func TestMDNSResolver_StopThenDiscover(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, err = r.Discover(context.Background())
	if err != ErrResolverStopped {
		t.Errorf("expected ErrResolverStopped, got %v", err)
	}
}

func TestMDNSResolver_StopThenAnnounce(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	err = r.Announce(context.Background(), Peer{ID: "test"})
	if err != ErrResolverStopped {
		t.Errorf("expected ErrResolverStopped, got %v", err)
	}
}

func TestMDNSResolver_ConcurrentSafety(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}

	// Stop first so concurrent calls hit the fast ErrResolverStopped path
	// instead of performing real multicast I/O.
	_ = r.Stop()

	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = r.Discover(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = r.Announce(ctx, Peer{ID: "test"})
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = r.Stop()
	}()

	wg.Wait()
}

func TestMDNSResolver_InvalidInterface(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Method:    MethodMDNS,
		Interface: "nonexistent-iface-12345",
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}

	err = r.Announce(context.Background(), Peer{ID: "test"})
	if err == nil {
		t.Error("expected error for invalid interface, got nil")
		_ = r.Stop()
	}
}

func TestMDNSResolver_DefaultPort(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}
	defer r.Stop()

	if r.port != mdnsDefaultPort {
		t.Errorf("expected default port %d, got %d", mdnsDefaultPort, r.port)
	}
}

func TestMDNSResolver_CustomPort(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
		Port:    9999,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}
	defer r.Stop()

	if r.port != 9999 {
		t.Errorf("expected port 9999, got %d", r.port)
	}
}

func TestMDNSResolver_DoubleStop(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Method:  MethodMDNS,
	}
	r, err := newMDNSResolver(cfg)
	if err != nil {
		t.Fatalf("newMDNSResolver: %v", err)
	}

	if err := r.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
