package channels

import "github.com/fulcrus/hopclaw/internal/meta"

// MetadataMap serializes the descriptor to a JSON-friendly map so bridges can
// persist stable session metadata without leaking package types into storage.
func (d ChannelCapabilityDescriptor) MetadataMap() map[string]any {
	d = d.Normalize()
	return map[string]any{
		"send_text":         d.SendText,
		"send_rich_text":    d.SendRichText,
		"send_file":         d.SendFile,
		"receive_message":   d.ReceiveMessage,
		"receive_event":     d.ReceiveEvent,
		"edit_message":      d.EditMessage,
		"delete_message":    d.DeleteMessage,
		"reactions":         d.Reactions,
		"history":           d.History,
		"threading":         d.Threading,
		"typing_indicator":  d.TypingIndicator,
		"rich_cards":        d.RichCards,
		"streaming_updates": d.StreamingUpdates,
		"multi_account":     d.MultiAccount,
		"webhook_inbound":   d.WebhookInbound,
		"policy_controls":   d.PolicyControls,
		"dedupe":            d.Dedupe,
		"pairing":           d.Pairing,
		"thread_binding":    d.ThreadBinding,
		"interactive":       d.Interactive,
		"mobile":            d.Mobile,
		"inline_delivery":   d.InlineDelivery,
	}
}

// ApplyChannelCapabilityMetadata injects the normalized channel descriptor and
// the behavior flags that Phase 4 consumers use directly.
func ApplyChannelCapabilityMetadata(metadataMap map[string]any, descriptor ChannelCapabilityDescriptor) {
	if metadataMap == nil {
		return
	}
	descriptor = descriptor.Normalize()
	metadataMap[meta.KeyChannelCapabilities] = descriptor.MetadataMap()
	metadataMap[meta.KeyChannelInteractive] = descriptor.Interactive
	metadataMap[meta.KeyChannelThreading] = descriptor.Threading
	metadataMap[meta.KeyChannelMobile] = descriptor.Mobile
	metadataMap[meta.KeyChannelInlineDelivery] = descriptor.InlineDelivery
}

// ApplyAdapterCapabilityMetadata derives and injects the effective descriptor
// for an adapter, including merged matrix/interface features.
func ApplyAdapterCapabilityMetadata(metadataMap map[string]any, adapter Adapter) {
	ApplyChannelCapabilityMetadata(metadataMap, DescriptorForAdapter(adapter))
}
