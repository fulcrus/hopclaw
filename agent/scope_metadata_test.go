package agent

import (
	"testing"

	"github.com/fulcrus/hopclaw/internal/meta"
)

func TestMergeSessionMetadataCanonicalizesChannelCapabilityDescriptor(t *testing.T) {
	t.Parallel()

	merged := MergeSessionMetadata(nil, IncomingMessage{
		Metadata: map[string]any{
			meta.KeyChannelInteractive:    false,
			meta.KeyChannelThreading:      true,
			meta.KeyChannelMobile:         true,
			meta.KeyChannelInlineDelivery: true,
		},
	})
	descriptor, ok := merged[meta.KeyChannelCapabilities].(map[string]any)
	if !ok {
		t.Fatalf("channel_capabilities = %#v, want map", merged[meta.KeyChannelCapabilities])
	}
	if got := descriptor["interactive"]; got != true {
		t.Fatalf("descriptor interactive = %#v, want true", got)
	}
	if got := descriptor["threading"]; got != true {
		t.Fatalf("descriptor threading = %#v, want true", got)
	}
	if got := descriptor["mobile"]; got != true {
		t.Fatalf("descriptor mobile = %#v, want true", got)
	}
	if got := descriptor["inline_delivery"]; got != true {
		t.Fatalf("descriptor inline_delivery = %#v, want true", got)
	}
	if got := merged[meta.KeyChannelInteractive]; got != true {
		t.Fatalf("channel_interactive = %#v, want true", got)
	}
}
