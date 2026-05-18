package helloprovider

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
	if manifest.Providers[ProviderName].DefaultModel != DefaultModel {
		t.Fatalf("DefaultModel = %q, want %q", manifest.Providers[ProviderName].DefaultModel, DefaultModel)
	}

	data, err := os.ReadFile(filepath.Join(".", "hopclaw.plugin.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "name: hello-provider") || !strings.Contains(text, "default_model: hello-provider-chat") {
		t.Fatalf("manifest file content = %q", text)
	}
}

func TestProviderModelsAndChat(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	models, err := harness.ListModels(context.Background(), Plugin{})
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != DefaultModel {
		t.Fatalf("ListModels() = %#v", models)
	}

	response, err := harness.Chat(context.Background(), Plugin{}, sdkplugin.ChatRequest{
		Messages: []sdkplugin.ChatMessage{{
			Role:    sdkplugin.ChatRoleUser,
			Content: "HopClaw",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	sdkplugin.AssertChatContent(t, response, "Hello, HopClaw!")
	if event, ok := harness.Runtime.LastEvent(); !ok || event.Name != "hello-provider.chat" {
		t.Fatalf("LastEvent() = %#v, %v", event, ok)
	}
	if len(harness.Runtime.Logs()) != 1 || !strings.Contains(harness.Runtime.Logs()[0], DefaultModel) {
		t.Fatalf("Logs() = %#v", harness.Runtime.Logs())
	}
}
