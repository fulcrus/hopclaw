package registry

import (
	"context"
	"testing"

	captypes "github.com/fulcrus/hopclaw/capability/types"
)

// stubCapability is a minimal Capability implementation for tests.
type stubCapability struct {
	name   string
	health captypes.CapabilityStatus
}

func (s *stubCapability) Manifest() captypes.Manifest {
	return captypes.Manifest{
		Name: s.name,
		Kind: captypes.KindService,
	}
}

func (s *stubCapability) Health(_ context.Context) captypes.Health {
	return captypes.Health{Status: s.health}
}

func (s *stubCapability) Invoke(_ context.Context, _ captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	t.Parallel()

	r := New()
	cap := &stubCapability{name: "browser", health: captypes.StatusReady}

	if err := r.Register(cap); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	got, ok := r.Get("browser")
	if !ok {
		t.Fatal("expected Get to find registered capability")
	}
	if got.Manifest().Name != "browser" {
		t.Fatalf("Manifest().Name = %q, want %q", got.Manifest().Name, "browser")
	}
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	t.Parallel()

	r := New()
	cap := &stubCapability{name: "search", health: captypes.StatusReady}

	if err := r.Register(cap); err != nil {
		t.Fatalf("first Register error: %v", err)
	}

	err := r.Register(cap)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	t.Parallel()

	r := New()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected Get to return false for nonexistent capability")
	}
}

func TestRegistryList(t *testing.T) {
	t.Parallel()

	r := New()
	_ = r.Register(&stubCapability{name: "zebra", health: captypes.StatusReady})
	_ = r.Register(&stubCapability{name: "alpha", health: captypes.StatusReady})

	manifests := r.List()
	if len(manifests) != 2 {
		t.Fatalf("List() returned %d, want 2", len(manifests))
	}
	// Should be sorted by name.
	if manifests[0].Name != "alpha" {
		t.Fatalf("List()[0].Name = %q, want %q", manifests[0].Name, "alpha")
	}
	if manifests[1].Name != "zebra" {
		t.Fatalf("List()[1].Name = %q, want %q", manifests[1].Name, "zebra")
	}
}

func TestRegistryNames(t *testing.T) {
	t.Parallel()

	r := New()
	_ = r.Register(&stubCapability{name: "c-cap", health: captypes.StatusReady})
	_ = r.Register(&stubCapability{name: "a-cap", health: captypes.StatusReady})

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("Names() returned %d, want 2", len(names))
	}
	if names[0] != "a-cap" || names[1] != "c-cap" {
		t.Fatalf("Names() = %v, expected sorted", names)
	}
}

func TestRegistryHealthAll(t *testing.T) {
	t.Parallel()

	r := New()
	_ = r.Register(&stubCapability{name: "cap-ok", health: captypes.StatusReady})
	_ = r.Register(&stubCapability{name: "cap-bad", health: captypes.StatusUnavailable})

	ctx := context.Background()
	results := r.HealthAll(ctx)
	if len(results) != 2 {
		t.Fatalf("HealthAll returned %d results, want 2", len(results))
	}
	if results["cap-ok"].Status != captypes.StatusReady {
		t.Fatalf("cap-ok health = %q, want ready", results["cap-ok"].Status)
	}
	if results["cap-bad"].Status != captypes.StatusUnavailable {
		t.Fatalf("cap-bad health = %q, want unavailable", results["cap-bad"].Status)
	}
}

func TestRegistryReports(t *testing.T) {
	t.Parallel()

	r := New()
	_ = r.Register(&stubCapability{name: "svc-1", health: captypes.StatusReady})

	ctx := context.Background()
	reports := r.Reports(ctx)
	if len(reports) != 1 {
		t.Fatalf("Reports returned %d, want 1", len(reports))
	}
	if reports[0].Manifest.Name != "svc-1" {
		t.Fatalf("Report manifest name = %q", reports[0].Manifest.Name)
	}
	if reports[0].Health.Status != captypes.StatusReady {
		t.Fatalf("Report health = %q", reports[0].Health.Status)
	}
}

func TestRegistryListCapabilitySessionsNotImplemented(t *testing.T) {
	t.Parallel()

	r := New()
	_ = r.Register(&stubCapability{name: "basic", health: captypes.StatusReady})

	sessions := r.ListCapabilitySessions("basic")
	if sessions != nil {
		t.Fatal("expected nil sessions for capability that does not implement SessionLister")
	}
}

func TestRegistryListCapabilitySessionsNotFound(t *testing.T) {
	t.Parallel()

	r := New()
	sessions := r.ListCapabilitySessions("nonexistent")
	if sessions != nil {
		t.Fatal("expected nil sessions for nonexistent capability")
	}
}
