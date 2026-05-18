package channels

// ChannelCapabilityDescriptor is the unified capability contract for channel
// adapters. It carries both transport features and behavior-shaping flags so
// downstream code can reason about channel behavior without guessing from
// names, prefixes, or adapter-specific branches.
type ChannelCapabilityDescriptor struct {
	SendText       bool `json:"send_text"`
	SendRichText   bool `json:"send_rich_text"`
	SendFile       bool `json:"send_file"`
	ReceiveMessage bool `json:"receive_message"`
	ReceiveEvent   bool `json:"receive_event"`

	EditMessage   bool `json:"edit_message"`
	DeleteMessage bool `json:"delete_message"`
	Reactions     bool `json:"reactions"`
	History       bool `json:"history"`

	Threading        bool `json:"threading"`
	TypingIndicator  bool `json:"typing_indicator"`
	RichCards        bool `json:"rich_cards"`
	StreamingUpdates bool `json:"streaming_updates"`
	MultiAccount     bool `json:"multi_account"`
	WebhookInbound   bool `json:"webhook_inbound"`
	PolicyControls   bool `json:"policy_controls"`
	Dedupe           bool `json:"dedupe"`
	Pairing          bool `json:"pairing"`
	ThreadBinding    bool `json:"thread_binding"`

	Interactive    bool `json:"interactive"`
	Mobile         bool `json:"mobile"`
	InlineDelivery bool `json:"inline_delivery"`
}

// Normalize applies behavior invariants so callers can depend on a stable
// descriptor shape even when adapters only declare the narrow differentiators.
func (d ChannelCapabilityDescriptor) Normalize() ChannelCapabilityDescriptor {
	if d.ThreadBinding {
		d.Threading = true
	}
	if d.Mobile || d.InlineDelivery {
		d.Interactive = true
	}
	return d
}

// Capabilities remains as a compatibility alias while the codebase migrates to
// ChannelCapabilityDescriptor as the canonical name.
type Capabilities = ChannelCapabilityDescriptor

// CapabilityMatrix remains as the operator-facing alias for the same unified
// descriptor shape.
type CapabilityMatrix = ChannelCapabilityDescriptor
