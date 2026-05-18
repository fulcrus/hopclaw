package setup

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// DestinationSpec.Normalized
// ---------------------------------------------------------------------------

func TestDestinationSpecNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec DestinationSpec
		want DestinationSpec
	}{
		{
			name: "trims and lowercases",
			spec: DestinationSpec{
				Kind:       "  CHANNEL  ",
				Provider:   "  Telegram  ",
				Channel:    "  Telegram  ",
				AccountID:  "  acc-1  ",
				TargetType: "  GROUP  ",
				Target:     "  -12345  ",
				Label:      "  My Group  ",
			},
			want: DestinationSpec{
				Kind:       "channel",
				Provider:   "telegram",
				Channel:    "telegram",
				AccountID:  "acc-1",
				TargetType: "group",
				Target:     "-12345",
				Label:      "My Group",
			},
		},
		{
			name: "infers kind=channel from channel",
			spec: DestinationSpec{
				Channel: "slack",
				Target:  "C123",
			},
			want: DestinationSpec{
				Kind:     "channel",
				Provider: "slack",
				Channel:  "slack",
				Target:   "C123",
			},
		},
		{
			name: "infers kind=email from provider=email",
			spec: DestinationSpec{
				Provider: "email",
				Target:   "user@example.com",
			},
			want: DestinationSpec{
				Kind:     "email",
				Provider: "email",
				Target:   "user@example.com",
			},
		},
		{
			name: "infers kind=email from provider=smtp",
			spec: DestinationSpec{
				Provider: "smtp",
				Target:   "user@example.com",
			},
			want: DestinationSpec{
				Kind:     "email",
				Provider: "smtp",
				Target:   "user@example.com",
			},
		},
		{
			name: "fills channel from provider for kind=channel",
			spec: DestinationSpec{
				Kind:     "channel",
				Provider: "feishu",
				Target:   "oc_123",
			},
			want: DestinationSpec{
				Kind:     "channel",
				Provider: "feishu",
				Channel:  "feishu",
				Target:   "oc_123",
			},
		},
		{
			name: "fills provider from channel for kind=channel",
			spec: DestinationSpec{
				Kind:    "channel",
				Channel: "feishu",
				Target:  "oc_123",
			},
			want: DestinationSpec{
				Kind:     "channel",
				Provider: "feishu",
				Channel:  "feishu",
				Target:   "oc_123",
			},
		},
		{
			name: "nils empty metadata",
			spec: DestinationSpec{
				Metadata: map[string]any{},
			},
			want: DestinationSpec{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.spec.Normalized()
			if got.Kind != tt.want.Kind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tt.want.Kind)
			}
			if got.Provider != tt.want.Provider {
				t.Fatalf("Provider = %q, want %q", got.Provider, tt.want.Provider)
			}
			if got.Channel != tt.want.Channel {
				t.Fatalf("Channel = %q, want %q", got.Channel, tt.want.Channel)
			}
			if got.Target != tt.want.Target {
				t.Fatalf("Target = %q, want %q", got.Target, tt.want.Target)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DestinationSpec.Ready
// ---------------------------------------------------------------------------

func TestDestinationSpecReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec DestinationSpec
		want bool
	}{
		{"complete", DestinationSpec{Kind: "channel", Provider: "slack", Target: "C1"}, true},
		{"missing kind", DestinationSpec{Provider: "slack", Target: "C1", Channel: "slack"}, true}, // kind inferred
		{"missing target", DestinationSpec{Kind: "channel", Provider: "slack"}, false},
		{"empty", DestinationSpec{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.spec.Ready(); got != tt.want {
				t.Fatalf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DestinationSpec.DisplayName
// ---------------------------------------------------------------------------

func TestDestinationSpecDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec DestinationSpec
		want string
	}{
		{
			name: "uses label",
			spec: DestinationSpec{Label: "My Target", Provider: "slack", Target: "C1"},
			want: "My Target",
		},
		{
			name: "provider + target",
			spec: DestinationSpec{Provider: "slack", Target: "C1"},
			want: "slack -> C1",
		},
		{
			name: "provider + account + target",
			spec: DestinationSpec{Provider: "slack", AccountID: "W1", Target: "C1"},
			want: "slack/W1 -> C1",
		},
		{
			name: "provider + account only",
			spec: DestinationSpec{Provider: "slack", AccountID: "W1"},
			want: "slack/W1",
		},
		{
			name: "provider only",
			spec: DestinationSpec{Provider: "slack"},
			want: "slack",
		},
		{
			name: "falls back to kind via provider inference",
			spec: DestinationSpec{Kind: "email"},
			want: "smtp", // Normalized() infers provider=smtp for kind=email
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.spec.DisplayName()
			if got != tt.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// InventoryItem.Normalized
// ---------------------------------------------------------------------------

func TestInventoryItemNormalized(t *testing.T) {
	t.Parallel()

	item := InventoryItem{
		Kind:         "  CHANNEL  ",
		Provider:     "  Slack  ",
		Label:        "  My Slack  ",
		Summary:      "  A summary  ",
		Status:       "  ACTIVE  ",
		Capabilities: []string{},
		Metadata:     map[string]any{},
		Spec:         DestinationSpec{Channel: "slack", Target: "C1"},
		Probe: &ProbeResult{
			Status: "  READY  ",
		},
	}.Normalized()

	if item.Kind != "channel" {
		t.Fatalf("Kind = %q", item.Kind)
	}
	if item.Provider != "slack" {
		t.Fatalf("Provider = %q", item.Provider)
	}
	if item.Label != "My Slack" {
		t.Fatalf("Label = %q", item.Label)
	}
	if item.Capabilities != nil {
		t.Fatal("Capabilities should be nil for empty slice")
	}
	if item.Metadata != nil {
		t.Fatal("Metadata should be nil for empty map")
	}
	if item.Probe.Status != ProbeStatusReady {
		t.Fatalf("Probe.Status = %q", item.Probe.Status)
	}
}

// ---------------------------------------------------------------------------
// ProbeResult.Normalized
// ---------------------------------------------------------------------------

func TestProbeResultNormalized(t *testing.T) {
	t.Parallel()

	now := time.Now()
	pr := ProbeResult{
		SlotID:    "  slot-1  ",
		Kind:      "  CHANNEL  ",
		Provider:  "  Feishu  ",
		Status:    "  READY  ",
		Code:      "  OK  ",
		Message:   "  all good  ",
		CheckedAt: now,
		Metadata:  map[string]any{},
	}.Normalized()

	if pr.SlotID != "slot-1" {
		t.Fatalf("SlotID = %q", pr.SlotID)
	}
	if pr.Kind != "channel" {
		t.Fatalf("Kind = %q", pr.Kind)
	}
	if pr.Provider != "feishu" {
		t.Fatalf("Provider = %q", pr.Provider)
	}
	if pr.Status != ProbeStatusReady {
		t.Fatalf("Status = %q", pr.Status)
	}
	if pr.Code != "ok" {
		t.Fatalf("Code = %q", pr.Code)
	}
	if pr.Metadata != nil {
		t.Fatal("Metadata should be nil for empty map")
	}
}

// ---------------------------------------------------------------------------
// Contract.Ready
// ---------------------------------------------------------------------------

func TestContractReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ContractStatus
		want   bool
	}{
		{"ready", ContractStatusReady, true},
		{"needs input", ContractStatusNeedsInput, false},
		{"blocked", ContractStatusBlocked, false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Contract{Status: tt.status}
			if got := c.Ready(); got != tt.want {
				t.Fatalf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}
