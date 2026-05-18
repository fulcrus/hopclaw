package approvalflow

import (
	"context"
	"reflect"
	"testing"
)

type stubProvider struct {
	name string
}

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) SubmitApproval(context.Context, SubmitRequest) (*Submission, error) {
	return &Submission{}, nil
}
func (s stubProvider) UpdateApproval(context.Context, UpdateRequest) error { return nil }

func TestProviderRegistryDescribeAndNamesAreSorted(t *testing.T) {
	t.Parallel()

	registry := NewProviderRegistry([]ProviderDescriptor{
		{
			Name:          "zeta",
			Enabled:       true,
			SubmitEnabled: true,
		},
		{
			Name:          "alpha",
			Enabled:       true,
			SubmitEnabled: true,
			CallbackAuth: CallbackAuthPolicy{
				Token: "token",
			},
		},
	}, stubProvider{name: "zeta"}, stubProvider{name: "alpha"})

	if got, want := registry.EnabledProviderNames(), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EnabledProviderNames() = %v, want %v", got, want)
	}
	if got, want := registry.CallbackProtectedProviderNames(), []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CallbackProtectedProviderNames() = %v, want %v", got, want)
	}
	described := registry.Describe()
	if len(described) != 2 {
		t.Fatalf("Describe() = %#v", described)
	}
	if described[0].Name != "alpha" || described[1].Name != "zeta" {
		t.Fatalf("Describe() order = %#v", described)
	}
}
