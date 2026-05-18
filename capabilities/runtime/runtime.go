package runtime

import (
	"context"
	"fmt"
	"strings"

	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
)

// Capability describes the local HopClaw runtime and operator surface.
type Capability struct {
	registry capabilityLookup
}

type capabilityLookup interface {
	Get(name string) (capregistry.Capability, bool)
}

func New(registry ...capabilityLookup) *Capability {
	capability := &Capability{}
	if len(registry) > 0 {
		capability.registry = registry[0]
	}
	return capability
}

func (c *Capability) Manifest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "runtime.local",
		Kind:          captypes.KindService,
		Operations:    runtimeOperations(),
		ArtifactKinds: []string{"artifact.blob", "artifact.preview"},
		Events: []string{
			"run.submitted",
			"run.started",
			"run.waiting_approval",
			"run.resumed",
			"run.completed",
			"run.failed",
			"run.cancelled",
			"approval.requested",
			"approval.resolved",
			"tool.executed",
		},
		ApprovalPolicy: "policy",
	}
}

func (c *Capability) Health(context.Context) captypes.Health {
	return captypes.Health{
		Status:  captypes.StatusReady,
		Message: "local runtime is active",
	}
}

func (c *Capability) Invoke(ctx context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	if c == nil || c.registry == nil {
		return nil, fmt.Errorf("runtime.local capability registry is not configured")
	}

	targetName, targetReq, err := resolveTargetInvokeRequest(req)
	if err != nil {
		return nil, err
	}
	if targetName == c.Manifest().Name {
		return nil, fmt.Errorf("runtime.local cannot invoke itself")
	}

	target, ok := c.registry.Get(targetName)
	if !ok {
		return nil, fmt.Errorf("capability %q not found", targetName)
	}
	return target.Invoke(ctx, targetReq)
}

func runtimeOperations() []captypes.OperationSpec {
	return []captypes.OperationSpec{
		{Name: "runs.list", Description: "List runtime runs", SideEffectClass: "read", Idempotent: true},
		{Name: "runs.cancel", Description: "Cancel a runtime run", SideEffectClass: "external_write"},
		{Name: "sessions.list", Description: "List runtime sessions", SideEffectClass: "read", Idempotent: true},
		{Name: "approvals.list", Description: "List pending approvals", SideEffectClass: "read", Idempotent: true},
		{Name: "approvals.resolve", Description: "Resolve an approval ticket", SideEffectClass: "external_write"},
		{Name: "artifacts.list", Description: "List runtime artifacts", SideEffectClass: "read", Idempotent: true},
		{Name: "events.tail", Description: "Read recent runtime events", SideEffectClass: "read", Idempotent: true},
	}
}

func resolveTargetInvokeRequest(req captypes.InvokeRequest) (string, captypes.InvokeRequest, error) {
	params := cloneInvokeParams(req.Params)
	targetName := strings.TrimSpace(stringParam(params, "capability"))
	if targetName != "" {
		delete(params, "capability")
	}

	targetOperation := strings.TrimSpace(req.Operation)
	if targetName == "" {
		targetName = targetOperation
		if targetName == "" {
			return "", captypes.InvokeRequest{}, fmt.Errorf("capability is required")
		}
		targetOperation = strings.TrimSpace(stringParam(params, "operation"))
	} else {
		if explicitOperation := strings.TrimSpace(stringParam(params, "operation")); explicitOperation != "" {
			targetOperation = explicitOperation
		}
	}
	delete(params, "operation")

	if targetOperation == "" {
		return "", captypes.InvokeRequest{}, fmt.Errorf("target operation is required")
	}

	return targetName, captypes.InvokeRequest{
		Operation: targetOperation,
		SessionID: req.SessionID,
		JobID:     req.JobID,
		Params:    params,
	}, nil
}

func cloneInvokeParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(params))
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

func stringParam(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	value, ok := params[key].(string)
	if !ok {
		return ""
	}
	return value
}
