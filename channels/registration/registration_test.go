package registration

import "testing"

func TestBuiltinDescriptorsFlattenProviders(t *testing.T) {
	builtinProvidersMu.Lock()
	original := append([]BuiltinProvider(nil), builtinProviders...)
	builtinProviders = nil
	builtinProvidersMu.Unlock()
	t.Cleanup(func() {
		builtinProvidersMu.Lock()
		builtinProviders = original
		builtinProvidersMu.Unlock()
	})

	RegisterBuiltinProvider(func(RuntimeDeps, DescriptorState) []Descriptor {
		return []Descriptor{{
			Name:          "alpha",
			Order:         100,
			RuntimeConfig: "alpha-config",
		}}
	})
	RegisterBuiltinProvider(func(RuntimeDeps, DescriptorState) []Descriptor {
		return []Descriptor{{
			Name:          "beta",
			Order:         200,
			RuntimeConfig: 42,
		}}
	})

	descriptors := BuiltinDescriptors(RuntimeDeps{}, nil)
	if len(descriptors) != 2 {
		t.Fatalf("len(BuiltinDescriptors) = %d, want 2", len(descriptors))
	}
	if descriptors[0].Name != "alpha" || descriptors[0].RuntimeConfig != "alpha-config" {
		t.Fatalf("first descriptor = %#v, want alpha with runtime config", descriptors[0])
	}
	if descriptors[1].Name != "beta" || descriptors[1].RuntimeConfig != 42 {
		t.Fatalf("second descriptor = %#v, want beta with runtime config", descriptors[1])
	}
}
