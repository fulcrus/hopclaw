package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildConfig_AllSetupProvidersRoundTrip(t *testing.T) {
	for _, profile := range SetupProviderProfiles() {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			api := strings.TrimSpace(DefaultProviderAPI(profile.ID))
			if api == "" {
				t.Fatalf("provider %q has no default api", profile.ID)
			}
			apiProfile, ok := LookupSetupProviderAPIProfile(api)
			if !ok {
				t.Fatalf("provider %q api profile %q not found", profile.ID, api)
			}

			values := sampleProviderValues(profile, apiProfile, true)
			opts := SetupOptions{
				Provider:       profile.ID,
				ProviderAPI:    api,
				ProviderValues: values,
			}

			cfgText, err := BuildConfig(opts)
			if err != nil {
				t.Fatalf("BuildConfig() error = %v", err)
			}
			cfg, err := Parse([]byte(cfgText))
			if err != nil {
				t.Fatalf("Parse() error = %v\n%s", err, cfgText)
			}

			expectedModel := strings.TrimSpace(values["default_model"])
			if expectedModel == "" {
				expectedModel = strings.TrimSpace(DefaultModelForProvider(profile.ID))
			}
			if expectedModel == "" {
				t.Fatalf("provider %q did not resolve a default model", profile.ID)
			}

			expectedAgentDefault := expectedModel
			if !providerUsesCompatDefaultModel(profile.ID) {
				expectedAgentDefault = profile.ID + "/" + expectedModel
			}
			if cfg.Agent.DefaultModel != expectedAgentDefault {
				t.Fatalf("agent.default_model = %q, want %q", cfg.Agent.DefaultModel, expectedAgentDefault)
			}

			expectedCompat := providerUsesCompatRoot(profile.ID) &&
				canRenderSetupOpenAICompat(apiProfile.Fields, normalizeSetupProviderValues(opts))
			if expectedCompat {
				if strings.TrimSpace(cfg.Models.OpenAICompat.Model) != expectedModel {
					t.Fatalf("models.openai_compat.model = %q, want %q", cfg.Models.OpenAICompat.Model, expectedModel)
				}
				if expectedBaseURL := strings.TrimSpace(values["base_url"]); expectedBaseURL != "" &&
					strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != expectedBaseURL {
					t.Fatalf("models.openai_compat.base_url = %q, want %q", cfg.Models.OpenAICompat.BaseURL, expectedBaseURL)
				}
				if expectedAPIKey := strings.TrimSpace(values["api_key"]); expectedAPIKey != "" &&
					strings.TrimSpace(cfg.Models.OpenAICompat.APIKey) != expectedAPIKey {
					t.Fatalf("models.openai_compat.api_key = %q, want %q", cfg.Models.OpenAICompat.APIKey, expectedAPIKey)
				}
				return
			}

			if strings.TrimSpace(cfg.Models.DefaultProvider) != profile.ID {
				t.Fatalf("models.default_provider = %q, want %q", cfg.Models.DefaultProvider, profile.ID)
			}
			providerCfg, ok := cfg.Models.Providers[profile.ID]
			if !ok {
				t.Fatalf("models.providers[%q] missing", profile.ID)
			}
			if strings.TrimSpace(providerCfg.API) != api {
				t.Fatalf("models.providers[%s].api = %q, want %q", profile.ID, providerCfg.API, api)
			}
			if strings.TrimSpace(providerCfg.DefaultModel) != expectedModel {
				t.Fatalf("models.providers[%s].default_model = %q, want %q", profile.ID, providerCfg.DefaultModel, expectedModel)
			}
		})
	}
}

func TestBuildConfig_AllSetupChannelsRoundTrip(t *testing.T) {
	baseProvider := mustSetupProviderProfile(t, "anthropic")
	api := strings.TrimSpace(DefaultProviderAPI(baseProvider.ID))
	apiProfile, ok := LookupSetupProviderAPIProfile(api)
	if !ok {
		t.Fatalf("provider api profile %q not found", api)
	}

	for _, profile := range SetupChannelProfiles() {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			values := sampleChannelValues(profile)
			opts := SetupOptions{
				Provider:       baseProvider.ID,
				ProviderAPI:    api,
				ProviderValues: sampleProviderValues(baseProvider, apiProfile, false),
				Channels: []SetupChannelSelection{{
					ID:     profile.ID,
					Values: values,
				}},
			}

			cfgText, err := BuildConfig(opts)
			if err != nil {
				t.Fatalf("BuildConfig() error = %v", err)
			}
			cfg, err := Parse([]byte(cfgText))
			if err != nil {
				t.Fatalf("Parse() error = %v\n%s", err, cfgText)
			}
			assertChannelConfigValues(t, cfg.Channels, profile, values)
		})
	}
}

func TestBuildConfig_AllSetupProviderChannelPairsParse(t *testing.T) {
	channels := SetupChannelProfiles()
	for _, provider := range SetupProviderProfiles() {
		provider := provider
		api := strings.TrimSpace(DefaultProviderAPI(provider.ID))
		apiProfile, ok := LookupSetupProviderAPIProfile(api)
		if !ok {
			t.Fatalf("provider api profile %q not found", api)
		}

		t.Run(provider.ID, func(t *testing.T) {
			for _, channel := range channels {
				channel := channel
				t.Run(channel.ID, func(t *testing.T) {
					opts := SetupOptions{
						Provider:       provider.ID,
						ProviderAPI:    api,
						ProviderValues: sampleProviderValues(provider, apiProfile, false),
						Channels: []SetupChannelSelection{{
							ID:     channel.ID,
							Values: sampleChannelValues(channel),
						}},
					}

					cfgText, err := BuildConfig(opts)
					if err != nil {
						t.Fatalf("BuildConfig() error = %v", err)
					}
					if _, err := Parse([]byte(cfgText)); err != nil {
						t.Fatalf("Parse() error = %v\n%s", err, cfgText)
					}
				})
			}
		})
	}
}

func mustSetupProviderProfile(t *testing.T, provider string) SetupProviderProfile {
	t.Helper()
	profile, ok := LookupSetupProviderProfile(provider)
	if !ok {
		t.Fatalf("provider profile %q not found", provider)
	}
	return profile
}

func sampleProviderValues(profile SetupProviderProfile, apiProfile ProviderAPIProfile, includeAdvanced bool) map[string]string {
	values := make(map[string]string, len(apiProfile.Fields))
	defaultModel := strings.TrimSpace(firstNonEmptyString(firstNonEmptyString(profile.DefaultModels...), ProviderAPIFieldDefault(apiProfile.ID, "default_model"), "demo-model"))
	defaultBaseURL := strings.TrimSpace(firstNonEmptyString(profile.BaseURL, ProviderAPIFieldDefault(apiProfile.ID, "base_url"), "https://example.invalid/v1"))

	for _, field := range apiProfile.Fields {
		if field.Advanced && !includeAdvanced {
			continue
		}
		values[field.ID] = sampleProviderFieldValue(field, defaultBaseURL, defaultModel)
	}
	return values
}

func sampleProviderFieldValue(field SetupProviderField, defaultBaseURL, defaultModel string) string {
	switch strings.TrimSpace(field.ID) {
	case "base_url":
		return defaultBaseURL
	case "api_key":
		return "test-api-key"
	case "api_keys":
		return "test-api-key-2\ntest-api-key-3"
	case "headers":
		return "X-Test: matrix\nAuthorization: Bearer sample"
	case "timeout":
		return "45s"
	case "default_model":
		return defaultModel
	case "region":
		return "us-east-1"
	case "access_key_id":
		return "AKIA_TEST_ACCESS_KEY"
	case "secret_key":
		return "test-secret-key"
	case "session_token":
		return "test-session-token"
	}

	switch SetupProviderFieldType(field) {
	case "url":
		return defaultBaseURL
	case "duration":
		return "45s"
	case "string_list":
		return "item-one\nitem-two"
	case "string_map":
		return "X-Test: matrix"
	default:
		if value := strings.TrimSpace(field.DefaultValue); value != "" {
			return value
		}
		if value := strings.TrimSpace(field.Placeholder); value != "" && !strings.Contains(value, "Enter ") {
			return value
		}
		return "sample-" + strings.ReplaceAll(strings.TrimSpace(field.ID), "_", "-")
	}
}

func sampleChannelValues(profile ChannelProfile) map[string]string {
	fields := EffectiveOperatorChannelFields(profile)
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		if field.Required || field.Type == SetupChannelFieldBool || field.Type == SetupChannelFieldStringList || knownChannelOptionalValue(field.ConfigKey) != "" {
			values[field.ID] = sampleChannelFieldValue(field)
		}
	}
	return values
}

func sampleChannelFieldValue(field SetupChannelField) string {
	if value := knownChannelOptionalValue(field.ConfigKey); value != "" {
		return value
	}
	switch field.Type {
	case SetupChannelFieldBool:
		return "true"
	case SetupChannelFieldStringList:
		if strings.TrimSpace(field.ConfigKey) == "relays" {
			return "wss://relay1.example\nwss://relay2.example"
		}
		return "item-one\nitem-two"
	}
	switch strings.TrimSpace(field.ConfigKey) {
	case "base_url":
		return "https://example.invalid"
	case "homeserver":
		return "https://matrix.example"
	case "websocket_url":
		return "wss://example.invalid/ws"
	case "webhook_url":
		return "https://example.invalid/webhook"
	case "ship_url":
		return "https://example.invalid/ship"
	case "domain":
		return "feishu"
	case "app_id":
		return "cli-app-id"
	case "app_secret":
		return "cli-app-secret"
	case "bot_token":
		return "bot-token-value"
	case "app_token":
		return "app-token-value"
	case "api_token":
		return "api-token-value"
	case "channel_secret":
		return "channel-secret-value"
	case "channel_token":
		return "channel-token-value"
	case "oauth_token":
		return "oauth:sample"
	case "private_key":
		return "nsec1sampleprivatekey"
	case "user_id":
		return "@hopclaw:example.org"
	case "number":
		return "+1234567890"
	case "phone_id":
		return "1234567890"
	case "nick":
		return "hopclawbot"
	case "server":
		return "irc.example.org:6697"
	case "channels":
		return "#ops,#alerts"
	}
	if value := strings.TrimSpace(field.DefaultValue); value != "" {
		return value
	}
	return "sample-" + strings.ReplaceAll(strings.TrimSpace(field.ConfigKey), "_", "-")
}

func knownChannelOptionalValue(configKey string) string {
	switch strings.TrimSpace(configKey) {
	case "dm_policy":
		return "open"
	case "group_policy":
		return "open"
	case "group_session_scope":
		return "group"
	case "reply_in_thread":
		return "enabled"
	case "connection_mode":
		return "websocket"
	case "domain":
		return "feishu"
	}
	return ""
}

func assertChannelConfigValues(t *testing.T, channelsCfg ChannelsConfig, profile ChannelProfile, values map[string]string) {
	t.Helper()

	channelValue, ok := lookupChannelConfigValue(channelsCfg, profile.ID)
	if !ok {
		t.Fatalf("channel config %q not found in schema", profile.ID)
	}
	enabledField, ok := findStructFieldByYAMLTag(channelValue, "enabled")
	if !ok {
		t.Fatalf("channel %q missing enabled field in schema", profile.ID)
	}
	if !fieldTruthy(enabledField) {
		t.Fatalf("channel %q enabled field was not set true", profile.ID)
	}

	for _, field := range EffectiveOperatorChannelFields(profile) {
		want, ok := values[field.ID]
		if !ok || strings.TrimSpace(want) == "" {
			continue
		}
		actualField, ok := findStructFieldByYAMLTag(channelValue, field.ConfigKey)
		if !ok {
			t.Fatalf("channel %q missing config field %q", profile.ID, field.ConfigKey)
		}
		assertChannelFieldValue(t, profile.ID, field, actualField, want)
	}
}

func assertChannelFieldValue(t *testing.T, channelID string, field SetupChannelField, actual reflect.Value, want string) {
	t.Helper()

	if actual.Kind() == reflect.Pointer {
		if actual.IsNil() {
			t.Fatalf("channel %q field %q is nil", channelID, field.ConfigKey)
		}
		actual = actual.Elem()
	}

	switch actual.Kind() {
	case reflect.String:
		if strings.TrimSpace(actual.String()) == "" {
			t.Fatalf("channel %q field %q is empty", channelID, field.ConfigKey)
		}
	case reflect.Bool:
		if !actual.Bool() {
			t.Fatalf("channel %q field %q is false, want true", channelID, field.ConfigKey)
		}
	case reflect.Slice:
		if actual.Len() == 0 {
			t.Fatalf("channel %q field %q is empty", channelID, field.ConfigKey)
		}
	default:
		t.Fatalf("channel %q field %q has unsupported kind %s", channelID, field.ConfigKey, actual.Kind())
	}
}

func lookupChannelConfigValue(channelsCfg ChannelsConfig, channelID string) (reflect.Value, bool) {
	root := reflect.ValueOf(channelsCfg)
	rootType := root.Type()
	for i := 0; i < root.NumField(); i++ {
		field := rootType.Field(i)
		if strings.TrimSpace(field.Tag.Get("yaml")) == channelID {
			return root.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func findStructFieldByYAMLTag(value reflect.Value, yamlTag string) (reflect.Value, bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		structField := valueType.Field(i)
		tag := strings.Split(structField.Tag.Get("yaml"), ",")[0]
		if tag == yamlTag {
			return value.Field(i), true
		}
		if strings.Contains(structField.Tag.Get("yaml"), ",inline") || structField.Anonymous {
			if nested, ok := findStructFieldByYAMLTag(value.Field(i), yamlTag); ok {
				return nested, true
			}
		}
	}
	return reflect.Value{}, false
}

func fieldTruthy(value reflect.Value) bool {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return value.Bool()
	case reflect.String:
		return strings.TrimSpace(value.String()) != ""
	}
	return !value.IsZero()
}

func TestSetupSupportedChannelsMapToSchema(t *testing.T) {
	channelsType := reflect.TypeOf(ChannelsConfig{})
	channelFields := make(map[string]reflect.Type, channelsType.NumField())
	for i := 0; i < channelsType.NumField(); i++ {
		field := channelsType.Field(i)
		channelFields[strings.TrimSpace(field.Tag.Get("yaml"))] = field.Type
	}

	for _, profile := range SetupChannelProfiles() {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			channelType, ok := channelFields[profile.ID]
			if !ok {
				t.Fatalf("channel %q not found in ChannelsConfig", profile.ID)
			}
			for _, field := range EffectiveOperatorChannelFields(profile) {
				if _, ok := findTypeFieldByYAMLTag(channelType, field.ConfigKey); !ok {
					t.Fatalf("channel %q field %q is not present in schema", profile.ID, field.ConfigKey)
				}
			}
		})
	}
}

func findTypeFieldByYAMLTag(valueType reflect.Type, yamlTag string) (reflect.StructField, bool) {
	if valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}

	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		tag := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if tag == yamlTag {
			return field, true
		}
		if strings.Contains(field.Tag.Get("yaml"), ",inline") || field.Anonymous {
			if nested, ok := findTypeFieldByYAMLTag(field.Type, yamlTag); ok {
				return nested, true
			}
		}
	}
	return reflect.StructField{}, false
}

func TestSampleChannelValuesCoverRequiredFields(t *testing.T) {
	for _, profile := range SetupChannelProfiles() {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			values := sampleChannelValues(profile)
			for _, field := range EffectiveOperatorChannelFields(profile) {
				if !field.Required {
					continue
				}
				if strings.TrimSpace(values[field.ID]) == "" {
					t.Fatalf("required field %q for channel %q did not receive a sample value", field.ID, profile.ID)
				}
			}
		})
	}
}

func TestSampleProviderValuesCoverRequiredFields(t *testing.T) {
	for _, profile := range SetupProviderProfiles() {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			api := strings.TrimSpace(DefaultProviderAPI(profile.ID))
			apiProfile, ok := LookupSetupProviderAPIProfile(api)
			if !ok {
				t.Fatalf("provider api profile %q not found", api)
			}
			values := sampleProviderValues(profile, apiProfile, true)
			for _, field := range apiProfile.Fields {
				if !field.Required {
					continue
				}
				if strings.TrimSpace(values[field.ID]) == "" {
					t.Fatalf("required field %q for provider %q did not receive a sample value", field.ID, profile.ID)
				}
			}
			if strings.TrimSpace(values["default_model"]) == "" {
				t.Fatalf("provider %q did not receive a default model sample", profile.ID)
			}
		})
	}
}

func TestSetupSupportedProviderMatrixIsNonEmpty(t *testing.T) {
	if len(SetupProviderProfiles()) == 0 {
		t.Fatal("expected setup provider profiles")
	}
	if len(SetupChannelProfiles()) == 0 {
		t.Fatal("expected setup channel profiles")
	}
}
