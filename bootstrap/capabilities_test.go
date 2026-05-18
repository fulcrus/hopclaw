package bootstrap

import "testing"

func TestInitCapabilitiesRegistersRuntimeCapability(t *testing.T) {
	t.Parallel()

	reg := initCapabilities(nil, nil)
	if reg == nil {
		t.Fatal("expected capability registry")
	}
	if _, ok := reg.Get("runtime.local"); !ok {
		t.Fatal("runtime.local should be registered once invoke support exists")
	}
}
