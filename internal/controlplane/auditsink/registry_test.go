package auditsink

import "testing"

func TestRegistryDescribeIsSorted(t *testing.T) {
	t.Parallel()

	registry := NewRegistry([]SinkDescriptor{
		{Name: "zeta", Enabled: true},
		{Name: "alpha", Enabled: true},
	}, "zeta", "alpha")

	described := registry.Describe()
	if len(described) != 2 {
		t.Fatalf("Describe() = %#v", described)
	}
	if described[0].Name != "alpha" || described[1].Name != "zeta" {
		t.Fatalf("Describe() order = %#v", described)
	}
}
