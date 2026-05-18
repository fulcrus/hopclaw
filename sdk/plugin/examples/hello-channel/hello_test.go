package hellochannel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestManifestIsValid(t *testing.T) {
	t.Parallel()

	manifest := Manifest()
	if errs := sdkplugin.ValidateManifest(manifest); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %#v", errs)
	}
	if len(manifest.Channels) != 1 {
		t.Fatalf("Manifest().Channels = %#v", manifest.Channels)
	}

	data, err := os.ReadFile(filepath.Join(".", "hopclaw.plugin.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "name: hello-channel") || !strings.Contains(text, "type: stdio") {
		t.Fatalf("manifest file content = %q", text)
	}
}

func TestChannelConnectAndSend(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	if err := harness.Connect(context.Background(), Plugin{}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	result, err := harness.Send(context.Background(), Plugin{}, sdkplugin.OutboundMessage{
		TargetID: "ops-room",
		Content:  "Hello HopClaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.MessageID != "hello-channel:ops-room" {
		t.Fatalf("MessageID = %q, want hello-channel:ops-room", result.MessageID)
	}
	if result.Metadata["echo"] != "Hello HopClaw" {
		t.Fatalf("Metadata = %#v", result.Metadata)
	}
	if len(harness.Runtime.Events()) != 2 {
		t.Fatalf("Events() = %#v", harness.Runtime.Events())
	}
	if len(harness.Runtime.Logs()) != 2 {
		t.Fatalf("Logs() = %#v", harness.Runtime.Logs())
	}
}
