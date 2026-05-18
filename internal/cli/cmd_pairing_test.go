package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels/pairing"
)

func TestPairingCmdHelp(t *testing.T) {
	root := newRootCmd()
	buf := new(strings.Builder)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"pairing", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(pairing --help) error: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"list", "initiate", "verify", "revoke"} {
		if !strings.Contains(output, sub) {
			t.Fatalf("pairing help missing %q in %q", sub, output)
		}
	}
}

func TestRunPairingInitiateJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != operatorPairingBasePath+"/initiate" {
			t.Fatalf("path = %s, want %s", r.URL.Path, operatorPairingBasePath+"/initiate")
		}
		var req pairingInitiateCLIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req.Channel != "feishu" || req.UserID != "ou_123" {
			t.Fatalf("unexpected request: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(pairingRecordCLIResponse{
			Record: pairing.PairingRecord{
				Channel:       req.Channel,
				UserID:        req.UserID,
				DisplayName:   req.DisplayName,
				Status:        pairing.StatusPending,
				Code:          "654321",
				CodeExpiresAt: time.Now().UTC().Add(10 * time.Minute),
				CreatedAt:     time.Now().UTC(),
			},
		})
	}))
	defer srv.Close()

	configPath := writeTestCLIConfig(t, srv.URL)
	oldConfig := flagConfig
	oldJSON := flagJSON
	flagConfig = configPath
	flagJSON = true
	t.Cleanup(func() {
		flagConfig = oldConfig
		flagJSON = oldJSON
	})

	restore := captureStdout(t)
	if err := runPairingInitiate(context.Background(), "feishu", "ou_123", "Alice"); err != nil {
		t.Fatalf("runPairingInitiate() error = %v", err)
	}
	output := restore()
	var payload pairingRecordCLIResponse
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("json.Unmarshal(output) error = %v; output=%q", err, output)
	}
	if payload.Record.Channel != "feishu" {
		t.Fatalf("record.channel = %q, want feishu", payload.Record.Channel)
	}
	if payload.Record.UserID != "ou_123" {
		t.Fatalf("record.user_id = %q, want ou_123", payload.Record.UserID)
	}
}

func TestRunPairingVerifyAndRevoke(t *testing.T) {
	var verifyCalled bool
	var revokeCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == operatorPairingBasePath+"/verify":
			verifyCalled = true
			_ = json.NewEncoder(w).Encode(pairingRecordCLIResponse{
				Record: pairing.PairingRecord{
					Channel:   "feishu",
					UserID:    "ou_123",
					Status:    pairing.StatusVerified,
					CreatedAt: time.Now().UTC(),
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == operatorPairingBasePath+"/feishu/ou_123":
			revokeCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	configPath := writeTestCLIConfig(t, srv.URL)
	oldConfig := flagConfig
	oldJSON := flagJSON
	flagConfig = configPath
	flagJSON = false
	t.Cleanup(func() {
		flagConfig = oldConfig
		flagJSON = oldJSON
	})

	restoreVerify := captureStdout(t)
	if err := runPairingVerify(context.Background(), "123456"); err != nil {
		t.Fatalf("runPairingVerify() error = %v", err)
	}
	verifyOutput := restoreVerify()
	if !verifyCalled {
		t.Fatal("expected verify endpoint to be called")
	}
	if !strings.Contains(verifyOutput, "Status:  verified") {
		t.Fatalf("verify output = %q", verifyOutput)
	}

	restoreRevoke := captureStdout(t)
	if err := runPairingRevoke(context.Background(), "feishu", "ou_123"); err != nil {
		t.Fatalf("runPairingRevoke() error = %v", err)
	}
	revokeOutput := restoreRevoke()
	if !revokeCalled {
		t.Fatal("expected revoke endpoint to be called")
	}
	if !strings.Contains(revokeOutput, "pairing revoked for feishu/ou_123") {
		t.Fatalf("revoke output = %q", revokeOutput)
	}
}

func writeTestCLIConfig(t *testing.T, gatewayURL string) string {
	t.Helper()
	addr := strings.TrimPrefix(gatewayURL, "http://")
	path := filepath.Join(t.TempDir(), "hopclaw.yaml")
	content := "server:\n  address: " + addr + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	return path
}
