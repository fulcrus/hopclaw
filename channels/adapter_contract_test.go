package channels_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/bluebubbles"
	"github.com/fulcrus/hopclaw/channels/discord"
	"github.com/fulcrus/hopclaw/channels/feishu"
	"github.com/fulcrus/hopclaw/channels/googlechat"
	"github.com/fulcrus/hopclaw/channels/imessage"
	"github.com/fulcrus/hopclaw/channels/irc"
	"github.com/fulcrus/hopclaw/channels/line"
	"github.com/fulcrus/hopclaw/channels/matrix"
	"github.com/fulcrus/hopclaw/channels/mattermost"
	"github.com/fulcrus/hopclaw/channels/msteams"
	"github.com/fulcrus/hopclaw/channels/nextcloudtalk"
	"github.com/fulcrus/hopclaw/channels/nostr"
	"github.com/fulcrus/hopclaw/channels/signal"
	"github.com/fulcrus/hopclaw/channels/slack"
	"github.com/fulcrus/hopclaw/channels/synologychat"
	"github.com/fulcrus/hopclaw/channels/telegram"
	"github.com/fulcrus/hopclaw/channels/tlon"
	"github.com/fulcrus/hopclaw/channels/twitch"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/channels/whatsapp"
	"github.com/fulcrus/hopclaw/channels/zalo"
	"github.com/fulcrus/hopclaw/channels/zalouser"
)

var (
	_ channels.Adapter = (*bluebubbles.Adapter)(nil)
	_ channels.Adapter = (*googlechat.Adapter)(nil)
	_ channels.Adapter = (*imessage.Adapter)(nil)
	_ channels.Adapter = (*nextcloudtalk.Adapter)(nil)
	_ channels.Adapter = (*nostr.Adapter)(nil)
	_ channels.Adapter = (*synologychat.Adapter)(nil)
	_ channels.Adapter = (*tlon.Adapter)(nil)
	_ channels.Adapter = (*twitch.Adapter)(nil)
	_ channels.Adapter = (*webhook.Adapter)(nil)
	_ channels.Adapter = (*zalo.Adapter)(nil)
	_ channels.Adapter = (*zalouser.Adapter)(nil)
)

type capabilityMatrixClaimingAdapter struct{}

func (capabilityMatrixClaimingAdapter) Connect(context.Context) error { return nil }

func (capabilityMatrixClaimingAdapter) Disconnect(context.Context) error { return nil }

func (capabilityMatrixClaimingAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}

func (capabilityMatrixClaimingAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}

func (capabilityMatrixClaimingAdapter) Status() channels.Status { return channels.StatusConnected }

func (capabilityMatrixClaimingAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return make(chan channels.InboundMessage)
}

func (capabilityMatrixClaimingAdapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		EditMessage: true,
		Reactions:   true,
		Threading:   true,
	}
}

func TestLongTailAdapterContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		adapter channels.Adapter
		wantErr string
	}{
		{name: "bluebubbles", adapter: bluebubbles.New(bluebubbles.Config{}), wantErr: "base_url is required"},
		{name: "imessage", adapter: imessage.New(imessage.Config{}), wantErr: "base_url is required"},
		{name: "nextcloudtalk", adapter: nextcloudtalk.New(nextcloudtalk.Config{}), wantErr: "base_url is required"},
		{name: "nostr", adapter: nostr.New(nostr.Config{}), wantErr: "private_key and at least one relay are required"},
		{name: "synologychat", adapter: synologychat.New(synologychat.Config{}), wantErr: "webhook_url is required"},
		{name: "tlon", adapter: tlon.New(tlon.Config{}), wantErr: "ship_url is required"},
		{name: "twitch", adapter: twitch.New(twitch.Config{}), wantErr: "oauth_token is required"},
		{name: "zalo", adapter: zalo.New(zalo.Config{}), wantErr: "app_id is required"},
		{name: "zalouser", adapter: zalouser.New(zalouser.Config{}), wantErr: "cookie is required"},
		{name: "googlechat", adapter: googlechat.New(googlechat.Config{}), wantErr: "either webhook_url or service_account is required"},
		{name: "webhook", adapter: webhook.New(webhook.Config{}), wantErr: "id is required"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.adapter.Status(); got != channels.StatusDisconnected {
				t.Fatalf("initial Status() = %q, want %q", got, channels.StatusDisconnected)
			}
			if sub := tt.adapter.SubscribeEvents(); sub == nil {
				t.Fatal("SubscribeEvents() returned nil")
			}
			if err := tt.adapter.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Connect() error = %v, want substring %q", err, tt.wantErr)
			}
			matrix := channels.MatrixForAdapter(tt.adapter)
			caps := tt.adapter.Capabilities()
			if matrix.SendText != caps.SendText || matrix.SendRichText != caps.SendRichText || matrix.SendFile != caps.SendFile {
				t.Fatalf("matrix send capabilities mismatch: matrix=%+v caps=%+v", matrix, caps)
			}
			if matrix.ReceiveMessage != caps.ReceiveMessage || matrix.ReceiveEvent != caps.ReceiveEvent {
				t.Fatalf("matrix receive capabilities mismatch: matrix=%+v caps=%+v", matrix, caps)
			}
			if err := tt.adapter.Disconnect(context.Background()); err != nil {
				t.Fatalf("Disconnect() error = %v", err)
			}
			if err := tt.adapter.Disconnect(context.Background()); err != nil {
				t.Fatalf("Disconnect() second call error = %v", err)
			}
		})
	}
}

func TestCapabilityMatrixCapturesInboundAndThreadingSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		adapter       channels.Adapter
		wantWebhook   bool
		wantThreading bool
		wantRichCards bool
	}{
		{
			name:        "googlechat",
			adapter:     googlechat.New(googlechat.Config{WebhookURL: "https://example.com/webhook"}),
			wantWebhook: true, wantThreading: true, wantRichCards: true,
		},
		{
			name:          "nextcloudtalk",
			adapter:       nextcloudtalk.New(nextcloudtalk.Config{}),
			wantThreading: true,
		},
		{
			name:        "synologychat",
			adapter:     synologychat.New(synologychat.Config{}),
			wantWebhook: true,
		},
		{
			name:        "zalo",
			adapter:     zalo.New(zalo.Config{}),
			wantWebhook: true,
		},
		{
			name:        "zalouser",
			adapter:     zalouser.New(zalouser.Config{}),
			wantWebhook: true,
		},
		{
			name:        "webhook",
			adapter:     webhook.New(webhook.Config{ID: "wh-test"}),
			wantWebhook: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matrix := channels.MatrixForAdapter(tt.adapter)
			if matrix.WebhookInbound != tt.wantWebhook {
				t.Fatalf("WebhookInbound = %v, want %v", matrix.WebhookInbound, tt.wantWebhook)
			}
			if matrix.Threading != tt.wantThreading {
				t.Fatalf("Threading = %v, want %v", matrix.Threading, tt.wantThreading)
			}
			if matrix.RichCards != tt.wantRichCards {
				t.Fatalf("RichCards = %v, want %v", matrix.RichCards, tt.wantRichCards)
			}
		})
	}
}

func TestCapabilityMatrixDoesNotReportInterfaceFeaturesWithoutImplementation(t *testing.T) {
	t.Parallel()

	matrix := channels.MatrixForAdapter(capabilityMatrixClaimingAdapter{})
	if matrix.EditMessage {
		t.Fatal("EditMessage should require MessageEditor implementation")
	}
	if matrix.Reactions {
		t.Fatal("Reactions should require MessageReactor implementation")
	}
	if !matrix.Threading {
		t.Fatal("Threading reporter flag should still be preserved")
	}
}

func TestDescriptorForAdapterCapturesBehaviorSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		adapter            channels.Adapter
		wantInteractive    bool
		wantThreading      bool
		wantMobile         bool
		wantInlineDelivery bool
	}{
		{
			name:               "bluebubbles",
			adapter:            bluebubbles.New(bluebubbles.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "discord",
			adapter:            discord.New(discord.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "feishu",
			adapter:            feishu.New(feishu.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "googlechat",
			adapter:            googlechat.New(googlechat.Config{WebhookURL: "https://example.com/webhook"}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "imessage",
			adapter:            imessage.New(imessage.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "irc",
			adapter:            irc.New(irc.Config{}),
			wantInteractive:    true,
			wantInlineDelivery: true,
		},
		{
			name:               "line",
			adapter:            line.New(line.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "matrix",
			adapter:            matrix.New(matrix.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "mattermost",
			adapter:            mattermost.New(mattermost.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "msteams",
			adapter:            msteams.New(msteams.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "nextcloudtalk",
			adapter:            nextcloudtalk.New(nextcloudtalk.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "nostr",
			adapter:            nostr.New(nostr.Config{}),
			wantInteractive:    true,
			wantInlineDelivery: true,
		},
		{
			name:               "signal",
			adapter:            signal.New(signal.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "slack",
			adapter:            slack.New(slack.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantInlineDelivery: true,
		},
		{
			name:               "synologychat",
			adapter:            synologychat.New(synologychat.Config{}),
			wantInteractive:    true,
			wantInlineDelivery: true,
		},
		{
			name:               "telegram",
			adapter:            telegram.New(telegram.Config{}),
			wantInteractive:    true,
			wantThreading:      true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "tlon",
			adapter:            tlon.New(tlon.Config{}),
			wantInteractive:    true,
			wantInlineDelivery: true,
		},
		{
			name:               "twitch",
			adapter:            twitch.New(twitch.Config{}),
			wantInteractive:    true,
			wantInlineDelivery: true,
		},
		{
			name:               "whatsapp",
			adapter:            whatsapp.New(whatsapp.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "zalo",
			adapter:            zalo.New(zalo.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "zalouser",
			adapter:            zalouser.New(zalouser.Config{}),
			wantInteractive:    true,
			wantMobile:         true,
			wantInlineDelivery: true,
		},
		{
			name:               "webhook",
			adapter:            webhook.New(webhook.Config{ID: "wh-test"}),
			wantInteractive:    false,
			wantThreading:      false,
			wantMobile:         false,
			wantInlineDelivery: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			descriptor := channels.DescriptorForAdapter(tt.adapter)
			if descriptor.Interactive != tt.wantInteractive {
				t.Fatalf("Interactive = %v, want %v", descriptor.Interactive, tt.wantInteractive)
			}
			if descriptor.Threading != tt.wantThreading {
				t.Fatalf("Threading = %v, want %v", descriptor.Threading, tt.wantThreading)
			}
			if descriptor.Mobile != tt.wantMobile {
				t.Fatalf("Mobile = %v, want %v", descriptor.Mobile, tt.wantMobile)
			}
			if descriptor.InlineDelivery != tt.wantInlineDelivery {
				t.Fatalf("InlineDelivery = %v, want %v", descriptor.InlineDelivery, tt.wantInlineDelivery)
			}
		})
	}
}
