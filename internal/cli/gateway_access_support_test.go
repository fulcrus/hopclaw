package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

func TestGatewayAccessFromClientNormalizesFields(t *testing.T) {
	access := gatewayAccessFromClient(&GatewayClient{
		BaseURL:   "https://gateway.example.com:17080/",
		AuthToken: "  secret-token  ",
	})

	if access.Address != "gateway.example.com:17080" {
		t.Fatalf("Address = %q", access.Address)
	}
	if access.BaseURL != "https://gateway.example.com:17080" {
		t.Fatalf("BaseURL = %q", access.BaseURL)
	}
	if access.AuthToken != "secret-token" {
		t.Fatalf("AuthToken = %q", access.AuthToken)
	}
}

func TestBuildGatewayURLEncodesQuery(t *testing.T) {
	url := buildGatewayURL("http://127.0.0.1:16280/", qrDefaultPairingPath, buildQRQuery("room/1 & a", "web chat"))
	expected := "http://127.0.0.1:16280/dashboard?channel=web+chat&session=room%2F1+%26+a"
	if url != expected {
		t.Fatalf("buildGatewayURL() = %q, want %q", url, expected)
	}
}

func TestDashboardURLsIncludeTokenOnlyForOpenURL(t *testing.T) {
	displayURL, openURL := dashboardURLs(gatewayAccess{
		BaseURL:   "http://127.0.0.1:16280",
		AuthToken: "secret token",
	})
	if displayURL != "http://127.0.0.1:16280/dashboard/" {
		t.Fatalf("displayURL = %q", displayURL)
	}
	if openURL != "http://127.0.0.1:16280/dashboard/?token=secret+token" {
		t.Fatalf("openURL = %q", openURL)
	}
}

func TestRunDashboardWithAccessMasksAuthToken(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runDashboardWithAccess(cmd, false, gatewayAccess{
		BaseURL:   "http://127.0.0.1:16280",
		AuthToken: "super-secret-token",
	})
	if err != nil {
		t.Fatalf("runDashboardWithAccess() error = %v", err)
	}

	output := out.String()
	if !bytes.Contains(out.Bytes(), []byte("Dashboard:  http://127.0.0.1:16280/dashboard/")) {
		t.Fatalf("unexpected output %q", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("Auth token: supe**********oken")) {
		t.Fatalf("expected masked token in output, got %q", output)
	}
	if bytes.Contains(out.Bytes(), []byte("super-secret-token")) {
		t.Fatalf("output leaked full token: %q", output)
	}
}

func TestRunQRGenerateWithAccessJSONUsesEncodedURL(t *testing.T) {
	oldJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = oldJSON }()

	restore := captureStdout(t)
	err := runQRGenerateWithAccess(gatewayAccess{
		BaseURL:   "http://127.0.0.1:16280",
		AuthToken: "secret-token",
	}, "room/1 & a", "web chat")
	if err != nil {
		t.Fatalf("runQRGenerateWithAccess() error = %v", err)
	}

	var payload qrConfig
	if err := json.Unmarshal([]byte(restore()), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.URL != "http://127.0.0.1:16280/dashboard?channel=web+chat&session=room%2F1+%26+a" {
		t.Fatalf("payload.URL = %q", payload.URL)
	}
	if payload.Token != "secret-token" {
		t.Fatalf("payload.Token = %q", payload.Token)
	}
	if payload.Session != "room/1 & a" {
		t.Fatalf("payload.Session = %q", payload.Session)
	}
}

func TestRunQRShowWithAccessMasksTokenInDisplay(t *testing.T) {
	restore := captureStdout(t)
	if err := runQRShowWithAccess(gatewayAccess{
		BaseURL:   "http://127.0.0.1:16280",
		AuthToken: "super-secret-token",
	}); err != nil {
		t.Fatalf("runQRShowWithAccess() error = %v", err)
	}

	output := restore()
	if !bytes.Contains([]byte(output), []byte("Auth token: supe**********oken")) {
		t.Fatalf("expected masked token in output, got %q", output)
	}
	if bytes.Contains([]byte(output), []byte("super-secret-token")) {
		t.Fatalf("output leaked full token: %q", output)
	}
}

func TestMaskDisplayToken(t *testing.T) {
	if got := maskDisplayToken("short"); got != "*****" {
		t.Fatalf("maskDisplayToken(short) = %q", got)
	}
	if got := maskDisplayToken("super-secret-token"); got != "supe**********oken" {
		t.Fatalf("maskDisplayToken(long) = %q", got)
	}
}

func TestRunQRGenerateWithAccessHumanOutputDoesNotPanic(t *testing.T) {
	restore := captureStdout(t)
	if err := runQRGenerateWithAccess(gatewayAccess{
		BaseURL:   "http://127.0.0.1:16280",
		AuthToken: "secret-token",
	}, "", ""); err != nil {
		t.Fatalf("runQRGenerateWithAccess() error = %v", err)
	}
	output := restore()
	if output == "" {
		t.Fatal("expected output")
	}
}

func TestResolveGatewayAccessForTargetUsesRuntimeError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := resolveGatewayAccessForTarget(context.Background(), "missing-runtime")
	if err == nil || err.Error() != `connection "missing-runtime" not found` {
		t.Fatalf("resolveGatewayAccessForTarget() error = %v", err)
	}
}
