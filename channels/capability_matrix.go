package channels

type CapabilityReporter interface {
	CapabilityMatrix() CapabilityMatrix
}

func DescriptorForAdapter(adapter Adapter) ChannelCapabilityDescriptor {
	if adapter == nil {
		return ChannelCapabilityDescriptor{}
	}
	caps := adapter.Capabilities().Normalize()
	matrix := caps
	if reporter, ok := adapter.(CapabilityReporter); ok {
		matrix = mergeCapabilityMatrix(matrix, reporter.CapabilityMatrix())
		matrix.SendText = caps.SendText
		matrix.SendRichText = caps.SendRichText
		matrix.SendFile = caps.SendFile
		matrix.ReceiveMessage = caps.ReceiveMessage
		matrix.ReceiveEvent = caps.ReceiveEvent
	}
	matrix.EditMessage = implementsMessageEditor(adapter)
	matrix.DeleteMessage = implementsMessageDeleter(adapter)
	matrix.Reactions = implementsMessageReactor(adapter)
	matrix.History = implementsHistoryReader(adapter)
	matrix.WebhookInbound = matrix.WebhookInbound || implementsHTTPInbound(adapter)
	return matrix.Normalize()
}

func MatrixForAdapter(adapter Adapter) CapabilityMatrix {
	return DescriptorForAdapter(adapter)
}

func mergeCapabilityMatrix(base, extra CapabilityMatrix) CapabilityMatrix {
	return CapabilityMatrix{
		SendText:         base.SendText || extra.SendText,
		SendRichText:     base.SendRichText || extra.SendRichText,
		SendFile:         base.SendFile || extra.SendFile,
		ReceiveMessage:   base.ReceiveMessage || extra.ReceiveMessage,
		ReceiveEvent:     base.ReceiveEvent || extra.ReceiveEvent,
		EditMessage:      base.EditMessage || extra.EditMessage,
		DeleteMessage:    base.DeleteMessage || extra.DeleteMessage,
		Reactions:        base.Reactions || extra.Reactions,
		History:          base.History || extra.History,
		Threading:        base.Threading || extra.Threading,
		TypingIndicator:  base.TypingIndicator || extra.TypingIndicator,
		RichCards:        base.RichCards || extra.RichCards,
		StreamingUpdates: base.StreamingUpdates || extra.StreamingUpdates,
		MultiAccount:     base.MultiAccount || extra.MultiAccount,
		WebhookInbound:   base.WebhookInbound || extra.WebhookInbound,
		PolicyControls:   base.PolicyControls || extra.PolicyControls,
		Dedupe:           base.Dedupe || extra.Dedupe,
		Pairing:          base.Pairing || extra.Pairing,
		ThreadBinding:    base.ThreadBinding || extra.ThreadBinding,
		Interactive:      base.Interactive || extra.Interactive,
		Mobile:           base.Mobile || extra.Mobile,
		InlineDelivery:   base.InlineDelivery || extra.InlineDelivery,
	}.Normalize()
}

func implementsMessageEditor(adapter Adapter) bool {
	_, ok := adapter.(MessageEditor)
	return ok
}

func implementsMessageDeleter(adapter Adapter) bool {
	_, ok := adapter.(MessageDeleter)
	return ok
}

func implementsMessageReactor(adapter Adapter) bool {
	_, ok := adapter.(MessageReactor)
	return ok
}

func implementsHistoryReader(adapter Adapter) bool {
	_, ok := adapter.(HistoryReader)
	return ok
}

func implementsHTTPInbound(adapter Adapter) bool {
	_, ok := adapter.(HTTPInboundAdapter)
	return ok
}
