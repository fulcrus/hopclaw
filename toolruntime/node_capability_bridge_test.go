package toolruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	registrycap "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
)

type stubDesktopBridgeCapability struct {
	manifest     captypes.Manifest
	health       captypes.Health
	openCount    int
	closeCount   int
	lastInvoke   captypes.InvokeRequest
	invokeResult map[string]*captypes.InvokeResult
}

func (s *stubDesktopBridgeCapability) Manifest() captypes.Manifest { return s.manifest }

func (s *stubDesktopBridgeCapability) Health(context.Context) captypes.Health {
	if s.health.Status == "" {
		return captypes.Health{Status: captypes.StatusReady}
	}
	return s.health
}

func (s *stubDesktopBridgeCapability) Invoke(_ context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	s.lastInvoke = req
	if result, ok := s.invokeResult[req.Operation]; ok {
		return result, nil
	}
	return &captypes.InvokeResult{OK: true}, nil
}

func (s *stubDesktopBridgeCapability) OpenSession(_ context.Context, _ map[string]any) (*captypes.SessionHandle, error) {
	s.openCount++
	return &captypes.SessionHandle{
		ID:         "desktop-session-1",
		Capability: "desktop",
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (s *stubDesktopBridgeCapability) CloseSession(_ context.Context, _ string) error {
	s.closeCount++
	return nil
}

func TestNodeCapabilityBridgeExposesLegacyNodesTools(t *testing.T) {
	t.Parallel()

	reg := registrycap.New()
	if err := reg.Register(&stubDesktopBridgeCapability{
		manifest: desktopBridgeManifest(),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewNodeCapabilityBridge(reg)
	if exec == nil {
		t.Fatal("expected node capability bridge")
	}
	defs := exec.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	if len(defs) != 4 {
		t.Fatalf("len(ToolDefinitions) = %d, want 4", len(defs))
	}
	def, ok := findNodeBridgeTool(defs, "nodes.screen_capture")
	if !ok {
		t.Fatalf("nodes.screen_capture missing from tools: %#v", defs)
	}
	if def.Source != "capability_bridge" {
		t.Fatalf("nodes.screen_capture source = %q, want capability_bridge", def.Source)
	}
}

func TestNodeCapabilityBridgeOnlyExposesAdvertisedDesktopOperations(t *testing.T) {
	t.Parallel()

	reg := registrycap.New()
	if err := reg.Register(&stubDesktopBridgeCapability{
		manifest: captypes.Manifest{
			Name:          "desktop",
			Kind:          captypes.KindSession,
			SessionScoped: true,
			Operations: []captypes.OperationSpec{
				{Name: "clipboard_read", SideEffectClass: "read"},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewNodeCapabilityBridge(reg)
	if exec == nil {
		t.Fatal("expected node capability bridge")
	}
	defs := exec.(agent.ToolDefinitionProvider).ToolDefinitions(nil)
	if len(defs) != 1 {
		t.Fatalf("len(ToolDefinitions) = %d, want 1", len(defs))
	}
	if _, ok := findNodeBridgeTool(defs, "nodes.clipboard_read"); !ok {
		t.Fatalf("nodes.clipboard_read missing from tools: %#v", defs)
	}
	if _, ok := findNodeBridgeTool(defs, "nodes.screen_capture"); ok {
		t.Fatalf("nodes.screen_capture should not be exposed: %#v", defs)
	}
}

func TestNodeCapabilityBridgeExecutesScreenCaptureAndClipboard(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reg := registrycap.New()
	capability := &stubDesktopBridgeCapability{
		manifest: desktopBridgeManifest(),
		invokeResult: map[string]*captypes.InvokeResult{
			"screenshot": {
				OK: true,
				Data: map[string]any{
					"content_base64": base64.StdEncoding.EncodeToString([]byte("png-bytes")),
				},
			},
			"clipboard_read": {
				OK: true,
				Data: map[string]any{
					"text": "clipboard text",
				},
			},
			"clipboard_write": {
				OK: true,
			},
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewNodeCapabilityBridge(reg)
	results, err := exec.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{
		{ID: "capture-1", Name: "nodes.screen_capture", Input: map[string]any{"output_path": filepath.Join(root, "capture.png")}},
		{ID: "read-1", Name: "nodes.clipboard_read", Input: map[string]any{}},
		{ID: "write-1", Name: "nodes.clipboard_write", Input: map[string]any{"content": "hello"}},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	var capturePayload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &capturePayload); err != nil {
		t.Fatalf("capture json.Unmarshal() error = %v", err)
	}
	written, err := os.ReadFile(filepath.Join(root, "capture.png"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(written) != "png-bytes" {
		t.Fatalf("capture file = %q", string(written))
	}
	if capturePayload["ok"] != true {
		t.Fatalf("capture payload = %#v", capturePayload)
	}

	var readPayload map[string]any
	if err := json.Unmarshal([]byte(results[1].Content), &readPayload); err != nil {
		t.Fatalf("read json.Unmarshal() error = %v", err)
	}
	if readPayload["content"] != "clipboard text" {
		t.Fatalf("clipboard read payload = %#v", readPayload)
	}

	var writePayload map[string]any
	if err := json.Unmarshal([]byte(results[2].Content), &writePayload); err != nil {
		t.Fatalf("write json.Unmarshal() error = %v", err)
	}
	if writePayload["ok"] != true {
		t.Fatalf("clipboard write payload = %#v", writePayload)
	}
	if capability.openCount != 3 || capability.closeCount != 3 {
		t.Fatalf("openCount=%d closeCount=%d", capability.openCount, capability.closeCount)
	}
}

func TestNodeCapabilityBridgeExecutesScreenRecord(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outputPath := filepath.Join(root, "capture.mp4")
	reg := registrycap.New()
	capability := &stubDesktopBridgeCapability{
		manifest: desktopBridgeManifest(),
		invokeResult: map[string]*captypes.InvokeResult{
			"screen_record": {
				OK: true,
				Data: map[string]any{
					"ok":           true,
					"path":         outputPath,
					"size_bytes":   42,
					"duration_sec": 3,
				},
			},
		},
	}
	if err := reg.Register(capability); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	exec := NewNodeCapabilityBridge(reg)
	results, err := exec.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:   "record-1",
		Name: "nodes.screen_record",
		Input: map[string]any{
			"output_path":  outputPath,
			"duration_sec": 3,
			"audio":        true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if capability.lastInvoke.Operation != "screen_record" {
		t.Fatalf("lastInvoke.Operation = %q", capability.lastInvoke.Operation)
	}
	if capability.lastInvoke.Params["output_path"] != outputPath {
		t.Fatalf("lastInvoke.Params = %#v", capability.lastInvoke.Params)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["duration_sec"] != float64(3) {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["fps"] != float64(nodesScreenRecordDefaultFPS) {
		t.Fatalf("payload fps = %#v, want %d", payload["fps"], nodesScreenRecordDefaultFPS)
	}
	if payload["quality"] != "medium" {
		t.Fatalf("payload quality = %#v, want medium", payload["quality"])
	}
}

func desktopBridgeManifest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "desktop",
		Kind:          captypes.KindSession,
		SessionScoped: true,
		Operations: []captypes.OperationSpec{
			{Name: "screenshot", SideEffectClass: "read"},
			{Name: "screen_record", SideEffectClass: "local_write"},
			{Name: "clipboard_read", SideEffectClass: "read"},
			{Name: "clipboard_write", SideEffectClass: "external_write"},
		},
	}
}

func findNodeBridgeTool(defs []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	for _, def := range defs {
		if def.Name == name {
			return def, true
		}
	}
	return agent.ToolDefinition{}, false
}
