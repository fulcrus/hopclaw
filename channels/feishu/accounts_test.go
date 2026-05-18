package feishu

import "testing"

func TestResolveAccountsMergesTopLevelAndNamedAccounts(t *testing.T) {
	t.Parallel()

	defaultID, accounts := resolveAccounts(Config{
		DefaultAccount:    "global",
		AppID:             "top-app",
		AppSecret:         "top-secret",
		ConnectionMode:    "websocket",
		GroupSessionScope: "group_thread",
		Accounts: map[string]AccountConfig{
			"china": {
				AppID:          "cn-app",
				AppSecret:      "cn-secret",
				Domain:         "feishu",
				ConnectionMode: "webhook",
			},
			"global": {
				Domain: "lark",
			},
		},
	})

	if defaultID != "global" {
		t.Fatalf("defaultID = %q", defaultID)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d", len(accounts))
	}
	if accounts[0].ID != "china" || accounts[0].ConnectionMode != "webhook" {
		t.Fatalf("china account = %#v", accounts[0])
	}
	if accounts[1].ID != "global" || accounts[1].AppID != "top-app" || accounts[1].Domain != "lark" {
		t.Fatalf("global account = %#v", accounts[1])
	}
	if accounts[1].GroupSessionScope != "group_thread" {
		t.Fatalf("global session scope = %q", accounts[1].GroupSessionScope)
	}
}
