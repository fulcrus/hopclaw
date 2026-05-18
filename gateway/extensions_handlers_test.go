package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/channels"
	channelhealth "github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	modtypes "github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

func TestGatewayExtensionsEndpointReturnsUnifiedSnapshot(t *testing.T) {
	t.Parallel()

	caps := capregistry.New()
	if err := caps.Register(&gatewayStubCapability{
		manifest: captypes.Manifest{Name: "runtime", Kind: captypes.KindService},
		health:   captypes.Health{Status: captypes.StatusReady},
	}); err != nil {
		t.Fatalf("Register(capability) error = %v", err)
	}

	mgr := channelmgr.New()
	if err := mgr.Register("slack", &matrixTestAdapter{}); err != nil {
		t.Fatalf("Register(channel) error = %v", err)
	}

	reg := extregistry.New(extregistry.Options{
		Capabilities: caps,
		Channels:     mgr,
		ChannelHealth: gatewayStaticHealth{
			items: []channelhealth.ChannelHealth{{Name: "slack", State: channelhealth.StateConnected}},
		},
		Tools: gatewayStaticTools{defs: []agent.ToolDefinition{
			{Name: "fs.read", Source: "builtin"},
			{Name: "channel.send", Source: "builtin"},
		}},
		Modules: gatewayStaticModules{modules: []modtypes.StaticModule{{
			ManifestValue: modtypes.Manifest{
				ID:       "builtin:core",
				Name:     "core",
				Source:   modtypes.SourceBuiltin,
				Delivery: modtypes.DeliveryEmbedded,
			},
			ContributionsValue: modtypes.Contributions{
				Tools:    []modtypes.Component{{Name: "fs.read"}},
				Channels: []modtypes.Component{{Name: "slack"}},
			},
			HealthValue: modtypes.HealthReport{
				Status:  modtypes.HealthReady,
				Summary: "runtime healthy",
			},
		}}},
	})

	gw := newTestGatewayFull(t)
	gw.SetExtensionRegistry(reg)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/extensions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Counts struct {
			CapabilityCount int `json:"capability_count"`
			ChannelCount    int `json:"channel_count"`
			ToolCount       int `json:"tool_count"`
			ModuleCount     int `json:"module_count"`
		} `json:"counts"`
		Capabilities []map[string]any `json:"capabilities"`
		Channels     []map[string]any `json:"channels"`
		Tools        []map[string]any `json:"tools"`
		Modules      []map[string]any `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Counts.CapabilityCount != 1 || payload.Counts.ChannelCount != 1 || payload.Counts.ToolCount != 2 || payload.Counts.ModuleCount != 1 {
		t.Fatalf("counts = %#v", payload.Counts)
	}
	if len(payload.Capabilities) != 1 || len(payload.Channels) != 1 || len(payload.Tools) != 2 || len(payload.Modules) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if health, ok := payload.Modules[0]["health"].(map[string]any); !ok || health["status"] != string(modtypes.HealthReady) {
		t.Fatalf("module health payload = %#v", payload.Modules[0]["health"])
	}
	if contributions, ok := payload.Modules[0]["contributions"].(map[string]any); !ok || int(contributions["tool_count"].(float64)) != 1 {
		t.Fatalf("module contributions payload = %#v", payload.Modules[0]["contributions"])
	}
}

func TestGatewayCapabilitiesEndpointUsesExtensionRegistry(t *testing.T) {
	t.Parallel()

	caps := capregistry.New()
	if err := caps.Register(&gatewayStubCapability{
		manifest: captypes.Manifest{Name: "browser", Kind: captypes.KindSession},
		health:   captypes.Health{Status: captypes.StatusReady},
	}); err != nil {
		t.Fatalf("Register(capability) error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.capabilities = nil
	gw.SetExtensionRegistry(extregistry.New(extregistry.Options{Capabilities: caps}))

	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/capabilities", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d", payload.Count)
	}
}

type gatewayStaticTools struct {
	defs []agent.ToolDefinition
}

func (g gatewayStaticTools) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	return append([]agent.ToolDefinition(nil), g.defs...)
}

type gatewayStaticHealth struct {
	items []channelhealth.ChannelHealth
}

func (g gatewayStaticHealth) Status() []channelhealth.ChannelHealth {
	return append([]channelhealth.ChannelHealth(nil), g.items...)
}

type gatewayStaticModules struct {
	modules []modtypes.StaticModule
}

func (g gatewayStaticModules) Modules() []modtypes.StaticModule {
	return append([]modtypes.StaticModule(nil), g.modules...)
}

func (g gatewayStaticModules) Manifests() []modtypes.Manifest {
	out := make([]modtypes.Manifest, 0, len(g.modules))
	for _, item := range g.modules {
		out = append(out, item.Manifest())
	}
	return out
}

type gatewayStubCapability struct {
	manifest captypes.Manifest
	health   captypes.Health
}

func (g *gatewayStubCapability) Manifest() captypes.Manifest { return g.manifest }
func (g *gatewayStubCapability) Health(context.Context) captypes.Health {
	return g.health
}
func (g *gatewayStubCapability) Invoke(context.Context, captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}
func (g *gatewayStubCapability) OpenSession(context.Context, map[string]any) (*captypes.SessionHandle, error) {
	return &captypes.SessionHandle{ID: "session-1", Capability: g.manifest.Name, CreatedAt: time.Now().UTC()}, nil
}
func (g *gatewayStubCapability) CloseSession(context.Context, string) error { return nil }
func (g *gatewayStubCapability) ListSessions() []*captypes.SessionHandle {
	return []*captypes.SessionHandle{{ID: "session-1", Capability: g.manifest.Name, CreatedAt: time.Now().UTC()}}
}

var _ channels.MessageEditor = (*matrixTestAdapter)(nil)

func (a *matrixTestAdapter) EditMessage(context.Context, string, string, string) error { return nil }
