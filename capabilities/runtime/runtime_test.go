package runtime

import (
	"context"
	"testing"

	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
)

type runtimeInvokeStub struct {
	lastRequest captypes.InvokeRequest
}

func (s *runtimeInvokeStub) Manifest() captypes.Manifest {
	return captypes.Manifest{Name: "demo.cap", Kind: captypes.KindService}
}

func (s *runtimeInvokeStub) Health(context.Context) captypes.Health {
	return captypes.Health{Status: captypes.StatusReady}
}

func (s *runtimeInvokeStub) Invoke(_ context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	s.lastRequest = req
	return &captypes.InvokeResult{
		OK:      true,
		Status:  "ok",
		Summary: "invoked",
		Data: map[string]any{
			"operation": req.Operation,
			"params":    req.Params,
		},
	}, nil
}

func TestRuntimeLocalInvokeDispatchesToRegisteredCapability(t *testing.T) {
	t.Parallel()

	reg := capregistry.New()
	target := &runtimeInvokeStub{}
	if err := reg.Register(target); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	capability := New(reg)
	result, err := capability.Invoke(context.Background(), captypes.InvokeRequest{
		Operation: "demo.run",
		Params: map[string]any{
			"capability": "demo.cap",
			"task":       "health-check",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = %v, want true", result.OK)
	}
	if target.lastRequest.Operation != "demo.run" {
		t.Fatalf("target operation = %q, want %q", target.lastRequest.Operation, "demo.run")
	}
	if got := target.lastRequest.Params["task"]; got != "health-check" {
		t.Fatalf("forwarded task = %#v", got)
	}
	if _, exists := target.lastRequest.Params["capability"]; exists {
		t.Fatal("capability param should be removed before forwarding")
	}
}
