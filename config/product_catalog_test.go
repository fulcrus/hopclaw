package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/model"
)

func TestChannelProfilesCoverChannelsConfig(t *testing.T) {
	profiles := ChannelProfiles()
	got := make(map[string]ChannelProfile, len(profiles))
	for _, profile := range profiles {
		got[profile.ID] = profile
	}

	channelsType := reflect.TypeOf(ChannelsConfig{})
	for i := 0; i < channelsType.NumField(); i++ {
		field := channelsType.Field(i)
		tag := strings.TrimSpace(strings.Split(field.Tag.Get("yaml"), ",")[0])
		if tag == "" || tag == "-" {
			continue
		}
		if _, ok := got[tag]; !ok {
			t.Fatalf("channel profile missing config tag %q", tag)
		}
	}

	for _, profile := range profiles {
		if !profile.Implemented {
			t.Fatalf("channel profile %q must be marked implemented", profile.ID)
		}
		if profile.SupportLevel == "" {
			t.Fatalf("channel profile %q must declare support_level", profile.ID)
		}
		if profile.SupportLevel == SupportLevelCore {
			t.Fatalf("channel profile %q must not use core support level", profile.ID)
		}
		if want := expectedChannelSupportLevel(profile); profile.SupportLevel != want {
			t.Fatalf("channel profile %q support_level = %q, want %q", profile.ID, profile.SupportLevel, want)
		}
	}
}

func TestSetupChannelProfilesAreSubsetOfCatalog(t *testing.T) {
	catalog := make(map[string]struct{}, len(ChannelProfiles()))
	for _, profile := range ChannelProfiles() {
		catalog[profile.ID] = struct{}{}
	}

	for _, profile := range SetupChannelProfiles() {
		if _, ok := catalog[profile.ID]; !ok {
			t.Fatalf("setup profile %q is not present in channel catalog", profile.ID)
		}
		if !profile.SetupSupported {
			t.Fatalf("setup profile %q must be marked setup_supported", profile.ID)
		}
		if profile.SupportLevel != SupportLevelSupported {
			t.Fatalf("setup profile %q support_level = %q, want supported", profile.ID, profile.SupportLevel)
		}
	}
}

func TestSupportedChannelProfilesHaveDirectRegressionTests(t *testing.T) {
	t.Parallel()

	for _, profile := range ChannelProfiles() {
		if profile.SupportLevel != SupportLevelSupported {
			continue
		}
		pkgDir := filepath.Join("..", "channels", supportedChannelPackageDir(profile.ID))
		entries, err := os.ReadDir(pkgDir)
		if err != nil {
			t.Fatalf("ReadDir(%q) error = %v", pkgDir, err)
		}
		hasTest := false
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), "_test.go") {
				hasTest = true
				break
			}
		}
		if !hasTest {
			t.Fatalf("supported channel %q must have at least one direct regression test file in %s", profile.ID, pkgDir)
		}
	}
}

func TestSupportedChannelProfilesKeepCriticalDirectRegressionCoverage(t *testing.T) {
	t.Parallel()

	requiredTests := map[string][]string{
		"bluebubbles": {
			"TestConnectMarksAdapterConnected",
			"TestSendPostsMessagePayload",
		},
		"twitch": {
			"TestConnectWritesHandshakeAndJoinsChannels",
			"TestSendWritesPrivmsg",
		},
	}

	for channelID, testNames := range requiredTests {
		pkgDir := filepath.Join("..", "channels", supportedChannelPackageDir(channelID))
		assertPackageContainsTestFunctions(t, pkgDir, testNames)
	}
}

func TestCurrentOperatorSetupCatalogUsesIndependentCopies(t *testing.T) {
	catalog := CurrentOperatorSetupCatalog()
	if catalog.DefaultAddress != DefaultGatewayAddress {
		t.Fatalf("DefaultAddress = %q, want %q", catalog.DefaultAddress, DefaultGatewayAddress)
	}
	if len(catalog.AuthModes) == 0 {
		t.Fatal("expected auth modes in setup catalog")
	}
	if len(catalog.Providers) == 0 {
		t.Fatal("expected provider profiles in setup catalog")
	}
	if len(catalog.ProviderAPIs) == 0 {
		t.Fatal("expected provider api profiles in setup catalog")
	}
	if len(catalog.Channels) == 0 {
		t.Fatal("expected channel profiles in setup catalog")
	}

	catalog.AuthModes[0].DisplayName = "mutated auth"
	catalog.Providers[0].DisplayName = "mutated provider"
	catalog.Providers[0].DefaultModels[0] = "mutated-model"
	catalog.ProviderAPIs[0].DisplayName = "mutated api"
	catalog.ProviderAPIs[0].Fields[0].Label = "mutated field"
	catalog.Channels[0].DisplayName = "mutated channel"
	if len(catalog.Channels[0].OperatorFields) > 0 {
		catalog.Channels[0].OperatorFields[0].Label = "mutated operator field"
	}

	fresh := CurrentOperatorSetupCatalog()
	if fresh.AuthModes[0].DisplayName == "mutated auth" {
		t.Fatal("auth modes should be returned as copies")
	}
	if fresh.Providers[0].DisplayName == "mutated provider" {
		t.Fatal("provider profiles should be returned as copies")
	}
	if fresh.Providers[0].DefaultModels[0] == "mutated-model" {
		t.Fatal("provider default models should be returned as copies")
	}
	if fresh.ProviderAPIs[0].DisplayName == "mutated api" {
		t.Fatal("provider api profiles should be returned as copies")
	}
	if fresh.ProviderAPIs[0].Fields[0].Label == "mutated field" {
		t.Fatal("provider api fields should be returned as copies")
	}
	if fresh.Channels[0].DisplayName == "mutated channel" {
		t.Fatal("channel profiles should be returned as copies")
	}
	if len(fresh.Channels[0].OperatorFields) > 0 && fresh.Channels[0].OperatorFields[0].Label == "mutated operator field" {
		t.Fatal("channel operator fields should be returned as copies")
	}
}

func assertPackageContainsTestFunctions(t *testing.T, pkgDir string, testNames []string) {
	t.Helper()

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", pkgDir, err)
	}
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(pkgDir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", filepath.Join(pkgDir, entry.Name()), err)
		}
		contents = append(contents, string(body))
	}
	joined := strings.Join(contents, "\n")
	for _, testName := range testNames {
		if !strings.Contains(joined, "func "+testName+"(") {
			t.Fatalf("package %s must contain regression test %s", pkgDir, testName)
		}
	}
}

func TestChannelProfilesExposeOperatorFieldMetadata(t *testing.T) {
	t.Parallel()

	profiles := ChannelProfiles()
	slack := findChannelProfile(t, profiles, "slack")
	if len(slack.OperatorFields) == 0 {
		t.Fatal("expected slack operator fields")
	}
	appToken := findChannelField(t, slack.OperatorFields, "app_token")
	if !appToken.Secret {
		t.Fatal("expected slack app_token to be secret")
	}
	if appToken.Placeholder != "xapp-... (Socket Mode)" {
		t.Fatalf("slack app_token placeholder = %q", appToken.Placeholder)
	}
	dmPolicy := findChannelField(t, slack.OperatorFields, "dm_policy")
	if dmPolicy.Type != SetupChannelFieldString {
		t.Fatalf("slack dm_policy type = %q", dmPolicy.Type)
	}

	nostr := findChannelProfile(t, profiles, "nostr")
	relays := findChannelField(t, nostr.OperatorFields, "relays")
	if relays.Type != SetupChannelFieldStringList {
		t.Fatalf("nostr relays type = %q, want %q", relays.Type, SetupChannelFieldStringList)
	}

	matrix := findChannelProfile(t, profiles, "matrix")
	requireMention := findChannelField(t, matrix.OperatorFields, "require_mention")
	if requireMention.Type != SetupChannelFieldBool {
		t.Fatalf("matrix require_mention type = %q, want %q", requireMention.Type, SetupChannelFieldBool)
	}

	imessage := findChannelProfile(t, profiles, "imessage")
	apiKey := findChannelField(t, imessage.Fields, "api_key")
	if !apiKey.Required {
		t.Fatal("imessage api_key must be marked required in the shipped catalog")
	}
	setupFields := channelOperatorFields("imessage", imessage.Fields)
	apiKey = findChannelField(t, setupFields, "api_key")
	if !apiKey.Required {
		t.Fatal("imessage api_key must be required in setup field projections")
	}
}

func TestEffectiveOperatorChannelFieldsPrefersOperatorSurface(t *testing.T) {
	t.Parallel()

	profile := ChannelProfile{
		ID: "demo",
		Fields: []SetupChannelField{
			{ID: "legacy_token", ConfigKey: "legacy_token", Label: "Legacy Token"},
		},
		OperatorFields: []SetupChannelField{
			{ID: "bot_token", ConfigKey: "bot_token", Label: "Bot Token", Secret: true},
		},
	}

	fields := EffectiveOperatorChannelFields(profile)
	if len(fields) != 1 || fields[0].ID != "bot_token" {
		t.Fatalf("EffectiveOperatorChannelFields() = %#v", fields)
	}
	fields[0].Label = "mutated"
	if profile.OperatorFields[0].Label == "mutated" {
		t.Fatal("effective operator fields should be returned as copies")
	}

	fallback := EffectiveOperatorChannelFields(ChannelProfile{
		ID: "fallback",
		Fields: []SetupChannelField{
			{ID: "bot_token", ConfigKey: "bot_token", Label: "Bot Token"},
		},
	})
	if len(fallback) != 1 || fallback[0].ID != "bot_token" {
		t.Fatalf("fallback EffectiveOperatorChannelFields() = %#v", fallback)
	}
}

func TestSetupCatalogProfilesExposeCapabilityMatrices(t *testing.T) {
	t.Parallel()

	providers := SetupProviderProfiles()
	openai := findSetupProviderProfile(t, providers, "openai")
	if openai.CapabilityMatrix.ProviderAPI != model.APIOpenAICompletions {
		t.Fatalf("openai capability api = %q, want %q", openai.CapabilityMatrix.ProviderAPI, model.APIOpenAICompletions)
	}
	if !openai.CapabilityMatrix.SupportsTools || !openai.CapabilityMatrix.SupportsStreaming {
		t.Fatalf("unexpected openai capability matrix: %+v", openai.CapabilityMatrix)
	}

	apis := SetupProviderAPIProfiles()
	bedrock := findProviderAPIProfile(t, apis, "bedrock-converse")
	if bedrock.CapabilityMatrix.ProviderAPI != model.APIBedrockConverse {
		t.Fatalf("bedrock capability api = %q, want %q", bedrock.CapabilityMatrix.ProviderAPI, model.APIBedrockConverse)
	}
	if !bedrock.CapabilityMatrix.SupportsStreaming {
		t.Fatalf("expected bedrock api capability matrix to report streaming: %+v", bedrock.CapabilityMatrix)
	}
}

func TestProviderAPIProfilesExposeAdvancedConnectionFields(t *testing.T) {
	t.Parallel()

	apis := SetupProviderAPIProfiles()
	openAI := findProviderAPIProfile(t, apis, "openai-completions")

	baseURL := findProviderAPIField(t, openAI.Fields, "base_url")
	if baseURL.Type != "url" {
		t.Fatalf("base_url type = %q, want url", baseURL.Type)
	}

	timeout := findProviderAPIField(t, openAI.Fields, "timeout")
	if timeout.Type != "duration" {
		t.Fatalf("timeout type = %q, want duration", timeout.Type)
	}
	if !timeout.Advanced {
		t.Fatal("timeout field must be marked advanced")
	}

	headers := findProviderAPIField(t, openAI.Fields, "headers")
	if headers.Type != "string_map" {
		t.Fatalf("headers type = %q, want string_map", headers.Type)
	}
	if !headers.Advanced {
		t.Fatal("headers field must be marked advanced")
	}

	apiKeys := findProviderAPIField(t, openAI.Fields, "api_keys")
	if apiKeys.Type != "string_list" {
		t.Fatalf("api_keys type = %q, want string_list", apiKeys.Type)
	}
	if !apiKeys.Advanced {
		t.Fatal("api_keys field must be marked advanced")
	}
}

func findSetupProviderProfile(t *testing.T, items []SetupProviderProfile, id string) SetupProviderProfile {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("provider profile %q not found", id)
	return SetupProviderProfile{}
}

func findProviderAPIProfile(t *testing.T, items []ProviderAPIProfile, id string) ProviderAPIProfile {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("provider api profile %q not found", id)
	return ProviderAPIProfile{}
}

func findProviderAPIField(t *testing.T, items []SetupProviderField, id string) SetupProviderField {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("provider api field %q not found", id)
	return SetupProviderField{}
}

func expectedChannelSupportLevel(profile ChannelProfile) SupportLevel {
	if profile.ID == "webhook" {
		return SupportLevelSupported
	}
	if profile.SetupSupported || profile.OnboardingSupported {
		return SupportLevelSupported
	}
	return SupportLevelExperimental
}

func supportedChannelPackageDir(id string) string {
	switch id {
	case "nextcloud_talk":
		return "nextcloudtalk"
	default:
		return strings.ReplaceAll(id, "_", "")
	}
}

func findChannelProfile(t *testing.T, items []ChannelProfile, id string) ChannelProfile {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("channel profile %q not found", id)
	return ChannelProfile{}
}

func findChannelField(t *testing.T, items []SetupChannelField, id string) SetupChannelField {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("channel field %q not found", id)
	return SetupChannelField{}
}

func TestLookupAuthModeProfileIsCaseInsensitive(t *testing.T) {
	profile, ok := LookupAuthModeProfile("Bearer")
	if !ok {
		t.Fatal("expected bearer auth mode profile")
	}
	if profile.ID != "bearer" {
		t.Fatalf("profile.ID = %q, want bearer", profile.ID)
	}
	if !profile.Recommended {
		t.Fatal("expected bearer auth mode to be recommended")
	}
}
