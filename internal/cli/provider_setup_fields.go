package cli

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/charmbracelet/huh"
)

func splitProviderSetupFields(fields []config.SetupProviderField) ([]config.SetupProviderField, []config.SetupProviderField) {
	basic := make([]config.SetupProviderField, 0, len(fields))
	advanced := make([]config.SetupProviderField, 0, len(fields))
	for _, field := range fields {
		if field.Advanced {
			advanced = append(advanced, field)
			continue
		}
		basic = append(basic, field)
	}
	return basic, advanced
}

func promptOptionalProviderFields(catalog cliSetupCatalog, provider string, fields []config.SetupProviderField, values map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	configureAdvanced := false
	for _, field := range fields {
		if field.Required {
			configureAdvanced = true
			break
		}
	}
	if !configureAdvanced {
		enabled, err := promptConfirm(
			itext("Configure advanced connection options?", "现在配置高级连接选项吗？"),
			fmt.Sprintf(
				itext("%s supports optional timeout, header, and credential-pool overrides.", "%s 支持可选的超时、请求头和凭据池覆盖配置。"),
				catalog.ProviderDisplayName(provider),
			),
			false,
		)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
	}
	for _, field := range fields {
		value, err := promptProviderSetupFieldValue(catalog, provider, field)
		if err != nil {
			return fmt.Errorf("%s input: %w", strings.ToLower(field.Label), err)
		}
		if config.SetupProviderFieldHasValue(field, value) {
			values[field.ID] = value
		}
	}
	return nil
}

func promptProviderSetupFieldValue(catalog cliSetupCatalog, provider string, field config.SetupProviderField) (string, error) {
	switch config.SetupProviderFieldType(field) {
	case "string_list":
		return promptProviderStringListValue(catalog, provider, field)
	case "string_map":
		return promptProviderStringMapValue(catalog, provider, field)
	default:
		if field.ID == "default_model" {
			return promptProviderModelValue(catalog, provider, field)
		}
		required := field.Required
		if provider == "custom" && field.ID == "base_url" {
			required = true
		}
		return promptInputField(
			field.Label,
			providerFieldPromptDescription(catalog, provider, field),
			providerFieldInitialValue(catalog, provider, field),
			required,
			field.Secret,
		)
	}
}

func promptProviderStringListValue(catalog cliSetupCatalog, provider string, field config.SetupProviderField) (string, error) {
	if !field.Required {
		enabled, err := promptConfirm(
			fmt.Sprintf(itext("Configure %s?", "现在配置 %s 吗？"), field.Label),
			providerFieldPromptDescription(catalog, provider, field),
			false,
		)
		if err != nil {
			return "", err
		}
		if !enabled {
			return "", nil
		}
	}

	values := make([]string, 0, 2)
	for {
		index := len(values) + 1
		description := providerFieldPromptDescription(catalog, provider, field)
		if len(values) > 0 {
			description = itext("Leave empty to finish. ", "留空即可结束。") + description
		}
		value, err := promptInputField(
			fmt.Sprintf("%s #%d", field.Label, index),
			description,
			"",
			field.Required && len(values) == 0,
			field.Secret,
		)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) == "" {
			break
		}
		values = append(values, strings.TrimSpace(value))
		addAnother, err := promptConfirm(fmt.Sprintf(itext("Add another %s entry?", "继续添加 %s 吗？"), strings.ToLower(field.Label)), "", false)
		if err != nil {
			return "", err
		}
		if !addAnother {
			break
		}
	}
	return strings.Join(values, "\n"), nil
}

func promptProviderStringMapValue(catalog cliSetupCatalog, provider string, field config.SetupProviderField) (string, error) {
	if !field.Required {
		enabled, err := promptConfirm(
			fmt.Sprintf(itext("Configure %s?", "现在配置 %s 吗？"), field.Label),
			providerFieldPromptDescription(catalog, provider, field),
			false,
		)
		if err != nil {
			return "", err
		}
		if !enabled {
			return "", nil
		}
	}

	lines := make([]string, 0, 2)
	for {
		index := len(lines) + 1
		key, err := promptInputField(
			fmt.Sprintf("%s key #%d", field.Label, index),
			itext("Leave empty to finish this section.", "留空即可结束这一部分。"),
			"",
			field.Required && len(lines) == 0,
			false,
		)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(key) == "" {
			break
		}
		value, err := promptInputField(
			fmt.Sprintf("%s value #%d", field.Label, index),
			providerFieldPromptDescription(catalog, provider, field),
			"",
			false,
			false,
		)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(key) + ":"
		if trimmedValue := strings.TrimSpace(value); trimmedValue != "" {
			line += " " + trimmedValue
		}
		lines = append(lines, line)
		addAnother, err := promptConfirm(fmt.Sprintf(itext("Add another %s entry?", "继续添加 %s 吗？"), strings.ToLower(field.Label)), "", false)
		if err != nil {
			return "", err
		}
		if !addAnother {
			break
		}
	}
	return strings.Join(lines, "\n"), nil
}

func promptConfirm(title, description string, initial bool) (bool, error) {
	value := initial
	confirm := huh.NewConfirm().Title(title).Value(&value)
	if strings.TrimSpace(description) != "" {
		confirm.Description(description)
	}
	form := huh.NewForm(huh.NewGroup(confirm))
	if err := form.Run(); err != nil {
		return false, err
	}
	return value, nil
}
