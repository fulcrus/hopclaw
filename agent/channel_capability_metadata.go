package agent

import "github.com/fulcrus/hopclaw/internal/meta"

var channelCapabilityMetadataKeys = []string{
	"send_text",
	"send_rich_text",
	"send_file",
	"receive_message",
	"receive_event",
	"edit_message",
	"delete_message",
	"reactions",
	"history",
	"threading",
	"typing_indicator",
	"rich_cards",
	"streaming_updates",
	"multi_account",
	"webhook_inbound",
	"policy_controls",
	"dedupe",
	"pairing",
	"thread_binding",
	"interactive",
	"mobile",
	"inline_delivery",
}

func normalizedChannelCapabilityDescriptor(metadataMap map[string]any) (map[string]any, bool) {
	if len(metadataMap) == 0 {
		return nil, false
	}

	rawDescriptor, hasDescriptor := metadataMap[meta.KeyChannelCapabilities]
	descriptor := make(map[string]any, len(channelCapabilityMetadataKeys))
	for _, key := range channelCapabilityMetadataKeys {
		descriptor[key] = descriptorValue(rawDescriptor, key)
	}
	if !hasDescriptor {
		descriptor["interactive"] = metadataBool(metadataMap, meta.KeyChannelInteractive)
		descriptor["threading"] = metadataBool(metadataMap, meta.KeyChannelThreading)
		descriptor["mobile"] = metadataBool(metadataMap, meta.KeyChannelMobile)
		descriptor["inline_delivery"] = metadataBool(metadataMap, meta.KeyChannelInlineDelivery)
	}
	if descriptorBool(descriptor, "thread_binding") {
		descriptor["threading"] = true
	}
	if descriptorBool(descriptor, "mobile") || descriptorBool(descriptor, "inline_delivery") {
		descriptor["interactive"] = true
	}
	return descriptor, hasAnyChannelCapability(descriptor)
}

func hasAnyChannelCapability(descriptor map[string]any) bool {
	if len(descriptor) == 0 {
		return false
	}
	for _, key := range channelCapabilityMetadataKeys {
		if descriptorBool(descriptor, key) {
			return true
		}
	}
	return false
}

func descriptorValue(raw any, key string) bool {
	switch current := raw.(type) {
	case map[string]any:
		return metadataBool(current, key)
	case map[string]bool:
		return current[key]
	default:
		return false
	}
}
