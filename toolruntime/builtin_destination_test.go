package toolruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
)

func TestDestinationInventoryAndProbe(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Email: EmailServiceConfig{
				SMTPHost: "smtp.example.com",
				SMTPPort: 587,
				Username: "robot@example.com",
				From:     "robot@example.com",
			},
		},
	})
	manager := channelmgr.New()
	if err := manager.Register("feishu", &destinationTestAdapter{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{
		ChannelManager: manager,
		DestinationCatalog: DestinationCatalog{
			ChannelAccounts: map[string][]DestinationAccount{
				"feishu": {{
					ID:      "ops",
					Label:   "Ops Bot",
					Enabled: true,
					Default: true,
				}},
			},
		},
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "destination.inventory",
		Input: map[string]any{
			"kind":            "channel",
			"current_channel": "feishu",
			"current_target":  "chat-123",
			"current_account": "ops",
		},
	}, {
		ID:   "call-2",
		Name: "destination.probe",
		Input: map[string]any{
			"kind":       "channel",
			"provider":   "feishu",
			"channel":    "feishu",
			"account_id": "ops",
			"target":     "chat-123",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	var inventory struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Spec   struct {
				AccountID string `json:"account_id"`
				Target    string `json:"target"`
			} `json:"spec"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &inventory); err != nil {
		t.Fatalf("inventory unmarshal: %v", err)
	}
	if inventory.Count == 0 || len(inventory.Items) == 0 {
		t.Fatalf("inventory = %#v", inventory)
	}
	if inventory.Items[0].Status != "ready" || inventory.Items[0].Spec.Target != "chat-123" || inventory.Items[0].Spec.AccountID != "ops" {
		t.Fatalf("inventory first item = %#v", inventory.Items[0])
	}

	var probe struct {
		Probe struct {
			Status    string `json:"status"`
			Reachable bool   `json:"reachable"`
		} `json:"probe"`
	}
	if err := json.Unmarshal([]byte(results[1].Content), &probe); err != nil {
		t.Fatalf("probe unmarshal: %v", err)
	}
	if probe.Probe.Status != "ready" || !probe.Probe.Reachable {
		t.Fatalf("probe = %#v", probe)
	}
}

func TestDestinationOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Email: EmailServiceConfig{
				SMTPHost: "smtp.example.com",
				Username: "robot@example.com",
			},
		},
	})
	manager := channelmgr.New()
	if err := manager.Register("slack", &destinationTestAdapter{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{ChannelManager: manager})

	run := &agent.Run{ID: "run-destination-schema"}
	sess := &agent.Session{ID: "sess-destination-schema"}

	inventoryPayload := execBuiltinPayload(t, builtins, run, sess, "destination.inventory", map[string]any{
		"kind":            "email",
		"current_channel": "slack",
	})
	assertPayloadMatchesSchema(t, "destination.inventory", inventoryPayload, destinationInventoryOutputSchema())

	probePayload := execBuiltinPayload(t, builtins, run, sess, "destination.probe", map[string]any{
		"kind":     "email",
		"provider": "smtp",
		"target":   "user@example.com",
	})
	assertPayloadMatchesSchema(t, "destination.probe", probePayload, destinationProbeOutputSchema())
}

type destinationTestAdapter struct{}

func (d *destinationTestAdapter) Connect(context.Context) error    { return nil }
func (d *destinationTestAdapter) Disconnect(context.Context) error { return nil }
func (d *destinationTestAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}
func (d *destinationTestAdapter) Capabilities() channels.Capabilities {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}
func (d *destinationTestAdapter) Status() channels.Status { return channels.StatusConnected }
func (d *destinationTestAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return nil
}
