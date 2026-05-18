package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestBuildChannelRowsUsesCatalogAndGenericConfigDetection(t *testing.T) {
	rows := buildChannelRows(config.ChannelsConfig{
		Slack: config.SlackChannelConfig{
			CommonChannelConfig: config.CommonChannelConfig{
				DMPolicy: "allow_all",
			},
		},
		GoogleChat: config.GoogleChatChannelConfig{
			WebhookURL: "https://chat.google.test/webhook",
		},
		Webhook: config.WebhookChannelConfig{
			Instances: map[string]config.WebhookInstanceConfig{
				"ops": {CallbackURL: "https://example.test/hook"},
			},
		},
	})

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Name != "webhook" && rows[0].Name != "googlechat" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}

	items := make(map[string]channelListRow, len(rows))
	for _, row := range rows {
		items[row.Name] = row
	}
	if _, ok := items["slack"]; ok {
		t.Fatal("slack should not be considered configured when only common policy is set")
	}
	if row, ok := items["googlechat"]; !ok {
		t.Fatal("expected googlechat row from generic config detection")
	} else {
		if !row.Enabled || !row.Configured {
			t.Fatalf("googlechat row = %+v", row)
		}
		if row.Source != "yaml" {
			t.Fatalf("googlechat source = %q, want yaml", row.Source)
		}
	}
	if row, ok := items["webhook"]; !ok {
		t.Fatal("expected webhook row from map-backed config")
	} else if !row.Configured {
		t.Fatalf("webhook row = %+v", row)
	}
}

func TestLoadOperatorChannelRowsUsesOperatorContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case channelsBasePath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(channelConfigListResponse{
				Items: []channelConfigInfo{
					{Name: "discord", Source: "api", Enabled: boolPtr(false), Config: json.RawMessage(`{"bot_token":"discord-token"}`)},
					{Name: "slack", Source: "yaml", Enabled: boolPtr(true), Config: json.RawMessage(`{"bot_token":"slack-token"}`)},
				},
				Count: 2,
			})
		case setupCatalogPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(config.OperatorSetupCatalog{
				Channels: []config.ChannelProfile{
					{ID: "slack", DisplayName: "Slack"},
					{ID: "discord", DisplayName: "Discord"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	rows, err := loadOperatorChannelRows(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("loadOperatorChannelRows() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Name != "slack" || rows[1].Name != "discord" {
		t.Fatalf("rows order = %#v", rows)
	}
	if !rows[0].Enabled || rows[0].Source != "yaml" || !rows[0].Configured {
		t.Fatalf("slack row = %+v", rows[0])
	}
	if rows[1].Enabled || rows[1].Source != "api" || !rows[1].Configured {
		t.Fatalf("discord row = %+v", rows[1])
	}
}

func TestBuildChannelCreateRequestUsesCatalogTypedFields(t *testing.T) {

	req, profile, err := buildChannelCreateRequest(localCLISetupCatalog(), "matrix", channelCommandOptions{
		Set: []string{
			"homeserver=https://matrix.example.com",
			"user_id=@bot:example.com",
			"access_token=matrix-token",
			"require_mention=true",
		},
	})
	if err != nil {
		t.Fatalf("buildChannelCreateRequest() error = %v", err)
	}
	if profile.ID != "matrix" {
		t.Fatalf("profile.ID = %q, want matrix", profile.ID)
	}
	if req.Name != "matrix" {
		t.Fatalf("req.Name = %q, want matrix", req.Name)
	}
	if req.Enabled == nil || !*req.Enabled {
		t.Fatalf("req.Enabled = %#v, want true", req.Enabled)
	}
	if req.Config["type"] != "matrix" {
		t.Fatalf("config.type = %#v, want matrix", req.Config["type"])
	}
	if req.Config["homeserver"] != "https://matrix.example.com" {
		t.Fatalf("homeserver = %#v", req.Config["homeserver"])
	}
	if req.Config["require_mention"] != true {
		t.Fatalf("require_mention = %#v, want true", req.Config["require_mention"])
	}
}

func TestBuildChannelCreateRequestSupportsLegacyTokenShorthand(t *testing.T) {

	req, profile, err := buildChannelCreateRequest(localCLISetupCatalog(), "telegram", channelCommandOptions{
		Token: "123456:ABC-DEF",
	})
	if err != nil {
		t.Fatalf("buildChannelCreateRequest() error = %v", err)
	}
	if profile.ID != "telegram" {
		t.Fatalf("profile.ID = %q, want telegram", profile.ID)
	}
	if req.Config["bot_token"] != "123456:ABC-DEF" {
		t.Fatalf("bot_token = %#v, want token shorthand to map to bot_token", req.Config["bot_token"])
	}
}

func TestRunChannelsAddWithClientUsesOperatorPayload(t *testing.T) {
	var got channelCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != channelsBasePath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelAddResponse{OK: true, Name: got.Name})
	}))
	defer srv.Close()

	restore := captureStdout(t)
	if err := runChannelsAddWithClient(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, "slack", channelCreateRequest{
		Name: "slack",
		Config: map[string]any{
			"type":      "slack",
			"bot_token": "xoxb-test",
			"app_token": "xapp-test",
		},
		Enabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("runChannelsAddWithClient() error = %v", err)
	}

	if got.Name != "slack" {
		t.Fatalf("got.Name = %q, want slack", got.Name)
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Fatalf("got.Enabled = %#v, want true", got.Enabled)
	}
	if got.Config["type"] != "slack" || got.Config["bot_token"] != "xoxb-test" || got.Config["app_token"] != "xapp-test" {
		t.Fatalf("got.Config = %#v", got.Config)
	}
	output := restore()
	if !strings.Contains(output, `Added channel "slack" (type: slack)`) {
		t.Fatalf("unexpected output %q", output)
	}
}

func TestRunChannelsValidateWithClientUsesOperatorSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != channelsBasePath+"/validate" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["channel"] != "slack" {
			t.Fatalf("channel = %q, want slack", req["channel"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelValidateResponse{
			Valid:   true,
			Status:  "connected",
			Message: "channel adapter reachable (connected)",
		})
	}))
	defer srv.Close()

	restore := captureStdout(t)
	if err := runChannelsValidateWithClient(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, "slack"); err != nil {
		t.Fatalf("runChannelsValidateWithClient() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, "Valid:   true") || !strings.Contains(output, "Status:  connected") {
		t.Fatalf("unexpected output %q", output)
	}
}

func TestRunChannelsTestWithClientIncludesTargetID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != channelsBasePath+"/test-message" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var req channelTestMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Channel != "slack" || req.TargetID != "C123" || req.Message != "ping" {
			t.Fatalf("request = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelTestMessageResponse{
			OK:      true,
			Message: "sent",
		})
	}))
	defer srv.Close()

	restore := captureStdout(t)
	if err := runChannelsTestWithClient(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, "slack", "C123", "ping"); err != nil {
		t.Fatalf("runChannelsTestWithClient() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, "Target:  C123") || !strings.Contains(output, "Result:  sent") {
		t.Fatalf("unexpected output %q", output)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
