package config

import (
	"fmt"
	"strconv"
	"strings"
)

func renderSetupChannelsSection(opts SetupOptions) (string, error) {
	selections := normalizeSetupChannelSelections(opts)
	if len(selections) == 0 {
		return "", nil
	}
	var buf strings.Builder
	seen := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		id := normalizeChannelCatalogID(selection.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			return "", fmt.Errorf("duplicate channel selection %q", id)
		}
		rendered, err := renderSetupChannel(selection)
		if err != nil {
			return "", err
		}
		buf.WriteString(rendered)
		seen[id] = struct{}{}
	}
	return buf.String(), nil
}

func normalizeSetupChannelSelections(opts SetupOptions) []SetupChannelSelection {
	if len(opts.Channels) > 0 {
		out := make([]SetupChannelSelection, 0, len(opts.Channels))
		for _, selection := range opts.Channels {
			if id := normalizeChannelCatalogID(selection.ID); id != "" {
				out = append(out, SetupChannelSelection{
					ID:     id,
					Values: copyChannelValues(selection.Values),
				})
			}
		}
		return out
	}
	if strings.TrimSpace(opts.Channel) == "" {
		return nil
	}
	values := copyChannelValues(opts.ChannelValues)
	switch normalizeChannelCatalogID(opts.Channel) {
	case "telegram", "discord", "slack":
		if token := strings.TrimSpace(opts.ChannelToken); token != "" {
			values["bot_token"] = token
		}
		if appToken := strings.TrimSpace(opts.ChannelAppToken); appToken != "" {
			values["app_token"] = appToken
		}
	}
	return []SetupChannelSelection{{
		ID:     normalizeChannelCatalogID(opts.Channel),
		Values: values,
	}}
}

func renderSetupChannel(selection SetupChannelSelection) (string, error) {
	profile, ok := LookupChannelProfile(selection.ID)
	if !ok {
		return "", fmt.Errorf("unknown channel %q", selection.ID)
	}
	if !profile.SetupSupported {
		return "", fmt.Errorf("channel %q is implemented but not scaffoldable via setup/onboard yet", profile.ID)
	}
	values := copyChannelValues(selection.Values)
	var buf strings.Builder
	buf.WriteString("  " + profile.ID + ":\n")
	buf.WriteString("    enabled: true\n")
	for _, field := range EffectiveOperatorChannelFields(profile) {
		value := lookupChannelFieldValue(values, field)
		if value == "" {
			if field.Required {
				return "", fmt.Errorf("%s requires %s", profile.DisplayName, field.Label)
			}
			continue
		}
		switch field.Type {
		case SetupChannelFieldStringList:
			items := splitChannelFieldList(value)
			if len(items) == 0 {
				if field.Required {
					return "", fmt.Errorf("%s requires %s", profile.DisplayName, field.Label)
				}
				continue
			}
			buf.WriteString("    " + field.ConfigKey + ":\n")
			for _, item := range items {
				buf.WriteString("      - " + yamlQuoteString(item) + "\n")
			}
		case SetupChannelFieldBool:
			boolValue, err := parseSetupChannelBool(value)
			if err != nil {
				return "", fmt.Errorf("%s %s: %w", profile.DisplayName, field.Label, err)
			}
			buf.WriteString("    " + field.ConfigKey + ": " + strconv.FormatBool(boolValue) + "\n")
		default:
			buf.WriteString("    " + field.ConfigKey + ": " + yamlQuoteString(value) + "\n")
		}
	}
	return buf.String(), nil
}

func lookupChannelFieldValue(values map[string]string, field SetupChannelField) string {
	if values != nil {
		if value := strings.TrimSpace(values[field.ID]); value != "" {
			return value
		}
		if value := strings.TrimSpace(values[field.ConfigKey]); value != "" {
			return value
		}
	}
	return strings.TrimSpace(field.DefaultValue)
}
