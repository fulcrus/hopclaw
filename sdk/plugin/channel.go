package plugin

import (
	"context"
	"slices"
)

type ChannelCapability string

const (
	ChannelCapabilityConnect ChannelCapability = "connect"
	ChannelCapabilitySend    ChannelCapability = "send"
)

type OutboundMessage struct {
	TargetID string
	Content  string
	Metadata map[string]any
}

type SendResult struct {
	MessageID string
	Metadata  map[string]any
}

type ChannelPlugin interface {
	Channel() Channel
}

type Channel struct {
	CapabilitiesList []ChannelCapability
	ConnectFunc      func(ctx context.Context, runtime PluginRuntime) error
	SendFunc         func(ctx context.Context, runtime PluginRuntime, message OutboundMessage) (SendResult, error)
}

func (c Channel) Connect(ctx context.Context, runtime PluginRuntime) error {
	if runtime == nil {
		return ErrNilRuntime
	}
	if c.ConnectFunc == nil {
		return ErrNotImplemented
	}
	return c.ConnectFunc(ctx, runtime)
}

func (c Channel) Send(ctx context.Context, runtime PluginRuntime, message OutboundMessage) (SendResult, error) {
	if runtime == nil {
		return SendResult{}, ErrNilRuntime
	}
	if c.SendFunc == nil {
		return SendResult{}, ErrNotImplemented
	}
	message.Metadata = cloneMapAny(message.Metadata)
	result, err := c.SendFunc(ctx, runtime, message)
	if err != nil {
		return SendResult{}, err
	}
	result.Metadata = cloneMapAny(result.Metadata)
	return result, nil
}

func (c Channel) Capabilities() []ChannelCapability {
	out := slices.Clone(c.CapabilitiesList)
	if c.ConnectFunc != nil {
		out = appendCapability(out, ChannelCapabilityConnect)
	}
	if c.SendFunc != nil {
		out = appendCapability(out, ChannelCapabilitySend)
	}
	return out
}

func appendCapability(values []ChannelCapability, capability ChannelCapability) []ChannelCapability {
	for _, existing := range values {
		if existing == capability {
			return values
		}
	}
	return append(values, capability)
}
