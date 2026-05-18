package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestActivateOperatorChannelsSupervisesManagedInstallations(t *testing.T) {
	t.Parallel()

	manager := channelmgr.New()
	adapter := &channelRuntimeDiffAdapter{name: "plugin:demo/ops"}
	if err := manager.Register("plugin:demo/ops", adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	bridge := &channelRuntimeDiffBridge{name: "plugin:demo/ops"}
	processManager := plugin.NewProcessManager()
	defer processManager.Stop(context.Background())

	spawned := make(chan struct{}, 1)
	active := activateOperatorChannels(context.Background(), manager, processManager, []channelregistry.Installation{{
		Name:    "plugin:demo/ops",
		Adapter: adapter,
		Bridge:  bridge,
		ManagedProcess: &channelregistry.ManagedProcessPlan{
			Config: plugin.ProcessConfig{Name: "plugin:demo/ops"},
			Spawn: func(ctx context.Context) error {
				select {
				case spawned <- struct{}{}:
				default:
				}
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}}, nil, nil)

	select {
	case <-spawned:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for managed process spawn")
	}

	if len(active) != 1 || active[0].name != "plugin:demo/ops" {
		t.Fatalf("active bridges = %#v, want plugin:demo/ops", active)
	}
	if bridge.starts.Load() != 1 {
		t.Fatalf("bridge starts = %d, want 1", bridge.starts.Load())
	}
	if adapter.connects.Load() != 1 {
		t.Fatalf("adapter connects = %d, want 1", adapter.connects.Load())
	}
	if handles := processManager.Handles(); len(handles) != 1 || handles[0].Name != "plugin:demo/ops" {
		t.Fatalf("process manager handles = %#v, want plugin:demo/ops", handles)
	}
}

func TestActivateOperatorChannelsRecordsConnectWarnings(t *testing.T) {
	t.Parallel()

	manager := channelmgr.New()
	adapter := &channelRuntimeDiffAdapter{
		name:       "plugin:demo/failing",
		connectErr: errors.New("token rejected"),
	}
	if err := manager.Register("plugin:demo/failing", adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	warnings := newStartupWarningCollector()

	active := activateOperatorChannels(context.Background(), manager, nil, []channelregistry.Installation{{
		Name:    "plugin:demo/failing",
		Adapter: adapter,
		Bridge:  &channelRuntimeDiffBridge{name: "plugin:demo/failing"},
	}}, nil, warnings)

	if len(active) != 0 {
		t.Fatalf("active bridges = %#v, want none", active)
	}
	items := warnings.OperationalWarnings()
	if len(items) != 1 {
		t.Fatalf("warning count = %d, want 1 (%#v)", len(items), items)
	}
	if items[0].Component != "channel/plugin:demo/failing" {
		t.Fatalf("warning component = %q", items[0].Component)
	}
	if items[0].Summary != `Channel "plugin:demo/failing" failed to connect` {
		t.Fatalf("warning summary = %q", items[0].Summary)
	}
	if !strings.Contains(items[0].Detail, "token rejected") {
		t.Fatalf("warning detail = %q", items[0].Detail)
	}
}
