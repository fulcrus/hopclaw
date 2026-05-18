package plugin

import (
	"context"
	"errors"
	"testing"
)

type testChannelPlugin struct {
	channel Channel
}

func (p testChannelPlugin) Channel() Channel {
	return p.channel
}

func TestChannelConnectAndSend(t *testing.T) {
	t.Parallel()

	plugin := testChannelPlugin{
		channel: Channel{
			ConnectFunc: func(_ context.Context, runtime PluginRuntime) error {
				value, err := ConfigValue(runtime, "mode")
				if err != nil {
					return err
				}
				if value.(string) != "demo" {
					t.Fatalf("ConfigValue() = %v, want %q", value, "demo")
				}
				return nil
			},
			SendFunc: func(_ context.Context, _ PluginRuntime, message OutboundMessage) (SendResult, error) {
				if message.Metadata["scope"] != "test" {
					t.Fatalf("message.Metadata = %#v", message.Metadata)
				}
				return SendResult{
					MessageID: "msg-1",
					Metadata: map[string]any{
						"target": message.TargetID,
					},
				}, nil
			},
		},
	}

	runtime := stubRuntime{
		config: map[string]any{
			"mode": "demo",
		},
	}

	if err := plugin.Channel().Connect(context.Background(), runtime); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	result, err := plugin.Channel().Send(context.Background(), runtime, OutboundMessage{
		TargetID: "user-1",
		Content:  "hello",
		Metadata: map[string]any{
			"scope": "test",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.MessageID != "msg-1" {
		t.Fatalf("Send() MessageID = %q, want %q", result.MessageID, "msg-1")
	}
	if result.Metadata["target"] != "user-1" {
		t.Fatalf("Send() Metadata = %#v", result.Metadata)
	}
}

func TestChannelCapabilities(t *testing.T) {
	t.Parallel()

	plugin := testChannelPlugin{
		channel: Channel{
			CapabilitiesList: []ChannelCapability{ChannelCapabilitySend},
			ConnectFunc:      func(context.Context, PluginRuntime) error { return nil },
			SendFunc:         func(context.Context, PluginRuntime, OutboundMessage) (SendResult, error) { return SendResult{}, nil },
		},
	}

	got := plugin.Channel().Capabilities()
	want := []ChannelCapability{ChannelCapabilitySend, ChannelCapabilityConnect}
	if len(got) != len(want) {
		t.Fatalf("Capabilities() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("Capabilities()[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}

func TestChannelNotImplementedAndNilRuntime(t *testing.T) {
	t.Parallel()

	plugin := testChannelPlugin{}

	if err := plugin.Channel().Connect(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("Connect(nil) error = %v, want ErrNilRuntime", err)
	}
	if _, err := plugin.Channel().Send(context.Background(), nil, OutboundMessage{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("Send(nil) error = %v, want ErrNilRuntime", err)
	}

	runtime := stubRuntime{}
	if err := plugin.Channel().Connect(context.Background(), runtime); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Connect() error = %v, want ErrNotImplemented", err)
	}
	if _, err := plugin.Channel().Send(context.Background(), runtime, OutboundMessage{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Send() error = %v, want ErrNotImplemented", err)
	}
}
