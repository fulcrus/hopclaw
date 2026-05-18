package agent

import (
	"strings"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/meta"
)

const (
	MetadataKeyAutomationID = "automation_id"
)

func injectScopeMetadata(metadata map[string]any, msg IncomingMessage) map[string]any {
	scope := ScopeRefFromIncomingMessage(msg)
	type entry struct {
		key   string
		value string
	}
	entries := []entry{
		{key: MetadataKeyAutomationID, value: strings.TrimSpace(scope.AutomationID)},
	}
	var out map[string]any
	for _, item := range entries {
		if item.value == "" {
			continue
		}
		if out == nil {
			out = cloneMap(metadata)
			if out == nil {
				out = make(map[string]any, len(entries))
			}
		}
		out[item.key] = item.value
	}
	if out == nil {
		return metadata
	}
	return out
}

func MergeScopeMetadata(dst map[string]any, msg IncomingMessage) map[string]any {
	return injectScopeMetadata(dst, msg)
}

func injectSessionMetadata(metadata map[string]any, msg IncomingMessage) map[string]any {
	out := injectScopeMetadata(metadata, msg)
	if len(msg.Metadata) == 0 {
		return out
	}
	for _, key := range []string{
		meta.KeyChannelName,
		meta.KeyChannel,
		meta.KeyChannelCapabilities,
		meta.KeyChannelInteractive,
		meta.KeyChannelThreading,
		meta.KeyChannelMobile,
		meta.KeyChannelInlineDelivery,
	} {
		value, ok := msg.Metadata[key]
		if !ok {
			continue
		}
		if out == nil {
			out = cloneMap(metadata)
			if out == nil {
				out = make(map[string]any, 8)
			}
		}
		if key == meta.KeyChannelName {
			text, _ := value.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				out[meta.KeyChannel] = text
			}
		}
		if current, ok := value.(map[string]any); ok {
			out[key] = cloneMap(current)
			continue
		}
		out[key] = value
	}
	descriptor, ok := normalizedChannelCapabilityDescriptor(out)
	if !ok {
		return out
	}
	if out == nil {
		out = cloneMap(metadata)
		if out == nil {
			out = make(map[string]any, 8)
		}
	}
	out[meta.KeyChannelCapabilities] = descriptor
	out[meta.KeyChannelInteractive] = descriptorBool(descriptor, "interactive")
	out[meta.KeyChannelThreading] = descriptorBool(descriptor, "threading")
	out[meta.KeyChannelMobile] = descriptorBool(descriptor, "mobile")
	out[meta.KeyChannelInlineDelivery] = descriptorBool(descriptor, "inline_delivery")
	return out
}

func MergeSessionMetadata(dst map[string]any, msg IncomingMessage) map[string]any {
	return injectSessionMetadata(dst, msg)
}

func ScopeRefFromIncomingMessage(msg IncomingMessage) domainscope.Ref {
	return domainscope.Ref{
		AutomationID: strings.TrimSpace(msg.AutomationID),
	}.Normalize()
}

func ScopeRefFromMetadata(metadata map[string]any) domainscope.Ref {
	if len(metadata) == 0 {
		return domainscope.Ref{}
	}
	var ref domainscope.Ref
	if value, _ := metadata[MetadataKeyAutomationID].(string); strings.TrimSpace(value) != "" {
		ref.AutomationID = strings.TrimSpace(value)
	}
	return ref.Normalize()
}

func MergeScopeRef(dst domainscope.Ref, msg IncomingMessage) domainscope.Ref {
	incoming := ScopeRefFromIncomingMessage(msg)
	if strings.TrimSpace(incoming.AutomationID) != "" {
		dst.AutomationID = incoming.AutomationID
	}
	return dst.Normalize()
}
