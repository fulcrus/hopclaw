package governanceadapter

import (
	"context"
	"reflect"
	"testing"
)

type namedAdapterStub struct {
	name string
}

func (s namedAdapterStub) Name() string { return s.name }
func (s namedAdapterStub) HandleGovernanceRecord(context.Context, Record) error {
	return nil
}

func TestAdapterRegistryDescribeAndNamesAreSorted(t *testing.T) {
	t.Parallel()

	registry := NewAdapterRegistry([]AdapterDescriptor{
		{Name: "zeta", Enabled: true},
		{Name: "alpha", Enabled: true},
	}, namedAdapterStub{name: "zeta"}, namedAdapterStub{name: "alpha"})

	if got, want := registry.EnabledAdapterNames(), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EnabledAdapterNames() = %v, want %v", got, want)
	}
	described := registry.Describe()
	if len(described) != 2 {
		t.Fatalf("Describe() = %#v", described)
	}
	if described[0].Name != "alpha" || described[1].Name != "zeta" {
		t.Fatalf("Describe() order = %#v", described)
	}
}
