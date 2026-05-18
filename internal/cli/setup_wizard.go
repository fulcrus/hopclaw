package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/charmbracelet/huh"
)

const (
	onboardSkipOptionValue         = "__skip__"
	onboardConfigureNowOptionValue = "__configure_now__"
)

func onboardSkipOptionLabel() string {
	return itextKey("cli.setup_cli.skip_dashboard", "Skip for now (configure later in dashboard)", "稍后在控制台配置")
}

func collectProviderSetupOptionsWithCatalog(catalog cliSetupCatalog) (config.SetupOptions, error) {
	return collectProviderSetupOptionsWithPrompt(catalog, false)
}

func collectOnboardProviderSetupOptionsWithCatalog(catalog cliSetupCatalog) (config.SetupOptions, error) {
	return collectProviderSetupOptionsWithPrompt(catalog, true)
}

func collectProviderSetupOptionsWithPrompt(catalog cliSetupCatalog, allowSkip bool) (config.SetupOptions, error) {
	var provider string
	if allowSkip {
		provider = defaultOnboardProviderSelection(catalog)
	}

	selectPrompt := huh.NewSelect[string]().
		Title(itextKey("cli.setup_cli.select_ai_provider", "Select AI provider", "选择模型提供商")).
		Options(providerPromptOptions(catalog, allowSkip)...).
		Value(&provider)
	if allowSkip {
		selectPrompt.Description(itextKey("cli.setup_cli.provider_or_skip", "Choose one now, or skip and finish model setup later in the dashboard.", "你可以现在选择一个，也可以先跳过，稍后在控制台完成模型配置。"))
	}

	providerForm := huh.NewForm(
		huh.NewGroup(
			selectPrompt,
		),
	)
	if err := providerForm.Run(); err != nil {
		return config.SetupOptions{}, fmt.Errorf("provider selection: %w", err)
	}
	return resolveProviderSetupOptionsWithCatalog(catalog, provider, allowSkip)
}

func resolveProviderSetupOptionsWithCatalog(catalog cliSetupCatalog, provider string, allowSkip bool) (config.SetupOptions, error) {
	var opts config.SetupOptions
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return opts, nil
	}
	if allowSkip {
		configureNow, err := promptOnboardConfigureNow(
			fmt.Sprintf(itextKey("cli.setup_cli.configure_provider_now", "Configure %s now?", "现在配置 %s 吗？"), catalog.ProviderDisplayName(provider)),
			itextKey("cli.setup_cli.finish_models_later", "You can continue installation now and finish model setup later in the dashboard.", "你也可以先继续安装，稍后在控制台完成模型配置。"),
		)
		if err != nil {
			return config.SetupOptions{}, err
		}
		if !configureNow {
			return config.SetupOptions{}, nil
		}
	}
	opts.Provider = provider
	opts.ProviderAPI = catalog.DefaultProviderAPI(provider)
	values, err := promptProviderSetupValuesWithCatalog(catalog, provider)
	if err != nil {
		return config.SetupOptions{}, err
	}
	opts.ProviderValues = values
	opts.APIKey = strings.TrimSpace(values["api_key"])
	opts.BaseURL = strings.TrimSpace(values["base_url"])
	opts.Model = strings.TrimSpace(values["default_model"])

	return opts, nil
}

func defaultOnboardProviderSelection(catalog cliSetupCatalog) string {
	return ""
}

func defaultOnboardAuthMode(catalog cliSetupCatalog) string {
	return onboardSkipOptionValue
}

func promptOnboardAuthConfigWithCatalog(catalog cliSetupCatalog) (mode, bearerToken, apiKey, jwtSecret string, err error) {
	mode = defaultOnboardAuthMode(catalog)
	authForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(itextKey("cli.setup_cli.select_authentication_method", "Select authentication method", "选择访问认证方式")).
				Description(itextKey("cli.setup_cli.gateway_authenticate_operator", "How should the gateway authenticate operator requests?", "HopClaw 应该如何保护本机操作入口？")).
				Options(authModeOptions(catalog, true)...).
				Value(&mode),
		),
	)
	if err := authForm.Run(); err != nil {
		return "", "", "", "", err
	}

	switch mode {
	case onboardSkipOptionValue:
		return "", "", "", "", nil
	case "bearer":
		initial, _ := generateSecureToken(24)
		bearerToken, err = promptInput(itextKey("cli.setup_cli.bearer_token", "Bearer token", "Bearer Token"), "", initial, true)
	case "apikey":
		initial, _ := generateSecureToken(24)
		apiKey, err = promptInput(itextKey("cli.setup_cli.operator_api_key", "Operator API key", "操作 API Key"), itextKey("cli.setup_cli.operator_api_key_help", "This key will be used by local CLI clients.", "这个 key 会被本地 CLI 客户端使用。"), initial, true)
	case "jwt":
		initial, _ := generateSecureToken(32)
		jwtSecret, err = promptInput("JWT secret", itextKey("cli.setup_cli.jwt_secret_help", "HopClaw will validate HS256 tokens with this secret.", "HopClaw 会用这个 secret 校验 HS256 token。"), initial, true)
	case "none":
		return mode, "", "", "", nil
	default:
		return "", "", "", "", fmt.Errorf("unsupported auth mode %q", mode)
	}
	if err != nil {
		return "", "", "", "", err
	}
	return mode, bearerToken, apiKey, jwtSecret, nil
}

func promptInput(title, description, initial string, required bool) (string, error) {
	return promptInputField(title, description, initial, required, false)
}

func promptInputField(title, description, initial string, required, secret bool) (string, error) {
	value := initial
	input := huh.NewInput().
		Title(title).
		Value(&value)
	if secret {
		input.EchoMode(huh.EchoModePassword)
	}
	if description != "" {
		input.Description(description)
	}
	if required {
		input.Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf(itextKey("cli.setup_cli.field_required", "%s is required", "请填写%s"), title)
			}
			return nil
		})
	}
	form := huh.NewForm(huh.NewGroup(input))
	if err := form.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func promptSetupChannelField(field config.SetupChannelField) (string, error) {
	return promptInputField(
		field.Label,
		channelFieldPromptDescription(field),
		strings.TrimSpace(field.DefaultValue),
		field.Required,
		field.Secret,
	)
}

func channelFieldPromptDescription(field config.SetupChannelField) string {
	parts := make([]string, 0, 3)
	if description := strings.TrimSpace(field.Description); description != "" {
		parts = append(parts, description)
	}
	if placeholder := strings.TrimSpace(field.Placeholder); placeholder != "" {
		parts = append(parts, fmt.Sprintf(itext("Example: %s", "示例：%s"), placeholder))
	}
	if field.Type == config.SetupChannelFieldBool {
		parts = append(parts, itext("Enter true or false.", "请输入 true 或 false。"))
	}
	return strings.Join(parts, " ")
}

func promptProviderSetupValuesWithCatalog(catalog cliSetupCatalog, provider string) (map[string]string, error) {
	api := strings.TrimSpace(catalog.DefaultProviderAPI(provider))
	if api == "" {
		api = "openai-completions"
	}
	apiProfile, ok := catalog.LookupProviderAPIProfile(api)
	if !ok {
		return nil, fmt.Errorf("provider %q uses unsupported setup API %q", provider, api)
	}

	values := make(map[string]string, len(apiProfile.Fields))
	basicFields, advancedFields := splitProviderSetupFields(apiProfile.Fields)
	for _, field := range basicFields {
		value, err := promptProviderSetupFieldValue(catalog, provider, field)
		if err != nil {
			return nil, fmt.Errorf("%s input: %w", strings.ToLower(field.Label), err)
		}
		if config.SetupProviderFieldHasValue(field, value) {
			values[field.ID] = value
		}
	}
	if err := promptOptionalProviderFields(catalog, provider, advancedFields, values); err != nil {
		return nil, err
	}
	return values, nil
}

func promptProviderModelValue(catalog cliSetupCatalog, provider string, field config.SetupProviderField) (string, error) {
	initial := providerFieldInitialValue(catalog, provider, field)
	models := catalog.DefaultModelsForProvider(provider)
	if len(models) == 0 {
		return promptInputField(field.Label, providerFieldPromptDescription(catalog, provider, field), initial, true, false)
	}

	var model string
	modelOptions := make([]huh.Option[string], 0, len(models)+1)
	for _, candidate := range models {
		modelOptions = append(modelOptions, huh.NewOption(candidate, candidate))
	}
	modelOptions = append(modelOptions, huh.NewOption(itext("Enter custom model name...", "输入自定义模型名..."), "_custom"))

	if containsString(models, initial) {
		model = initial
	} else if initial != "" {
		model = "_custom"
	} else {
		model = models[0]
	}

	modelForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(field.Label).
				Description(providerFieldPromptDescription(catalog, provider, field)).
				Options(modelOptions...).
				Value(&model),
		),
	)
	if err := modelForm.Run(); err != nil {
		return "", err
	}
	if model == "_custom" || strings.TrimSpace(model) == "" {
		return promptInputField(itext("Custom model name", "自定义模型名"), providerFieldPromptDescription(catalog, provider, field), initial, true, false)
	}
	return strings.TrimSpace(model), nil
}

func providerFieldPromptDescription(catalog cliSetupCatalog, provider string, field config.SetupProviderField) string {
	if desc := strings.TrimSpace(field.Description); desc != "" {
		return desc
	}
	if field.ID == "api_key" {
		if hint := strings.TrimSpace(catalog.ProviderAPIKeyHint(provider)); hint != "" {
			return hint
		}
	}
	switch config.SetupProviderFieldType(field) {
	case "string_list":
		return itext("Add one value per prompt. You can stop after the first item.", "每次输入一个值，输入完第一个后就可以停止。")
	case "string_map":
		return itext("Add one key/value pair per prompt.", "每次输入一组键值对。")
	}
	return strings.TrimSpace(field.Placeholder)
}

func providerFieldInitialValue(catalog cliSetupCatalog, provider string, field config.SetupProviderField) string {
	if value := strings.TrimSpace(field.DefaultValue); value != "" {
		return value
	}
	switch field.ID {
	case "api_key":
		if value := providerEnvValueFromCatalog(catalog, provider); value != "" {
			return value
		}
		detectedProvider, detectedKey := config.DetectAPIKey()
		if detectedProvider == provider {
			return detectedKey
		}
	case "base_url":
		return strings.TrimSpace(catalog.DefaultBaseURL(provider))
	case "default_model":
		return strings.TrimSpace(catalog.DefaultModelForProvider(provider))
	case "region":
		for _, envVar := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
			if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
				return value
			}
		}
	case "access_key_id":
		return strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	case "secret_key":
		return strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	case "session_token":
		return strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN"))
	}
	return ""
}

func providerEnvValueFromCatalog(catalog cliSetupCatalog, provider string) string {
	profile, ok := catalog.LookupProviderProfile(provider)
	if !ok {
		return ""
	}
	for _, envVar := range profile.EnvVars {
		envVar = strings.TrimSpace(envVar)
		if envVar == "" {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return value
		}
	}
	return ""
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func promptSingleSetupChannelWithCatalog(catalog cliSetupCatalog) ([]config.SetupChannelSelection, error) {
	return promptSetupChannelSelectionsWithCatalog(catalog, false)
}

func promptMultiSetupChannelsWithCatalog(catalog cliSetupCatalog) ([]config.SetupChannelSelection, error) {
	return promptSetupChannelSelectionsWithCatalog(catalog, true)
}

func promptSetupChannelSelectionsWithCatalog(catalog cliSetupCatalog, multi bool) ([]config.SetupChannelSelection, error) {
	profiles := catalog.SetupChannelProfiles()
	if multi {
		profiles = catalog.OnboardingChannelProfiles()
	}
	if len(profiles) == 0 {
		return nil, nil
	}

	if !multi {
		var selected string
		options := []huh.Option[string]{huh.NewOption(onboardSkipOptionLabel(), "")}
		for _, profile := range profiles {
			options = append(options, huh.NewOption(channelOptionLabel(profile), profile.ID))
		}
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(itext("Set up a chat channel? (optional)", "现在配置一个聊天渠道吗？（可选）")).
					Description(itext("Choose one now, or skip and finish channel setup later in the dashboard.", "你可以现在配置一个，也可以先跳过，稍后在控制台完成渠道配置。")).
					Options(options...).
					Value(&selected),
			),
		)
		if err := form.Run(); err != nil {
			return nil, err
		}
		if selected == "" {
			return nil, nil
		}
		selection, err := promptSetupChannelSelectionWithCatalog(catalog, selected)
		if err != nil {
			return nil, err
		}
		return []config.SetupChannelSelection{selection}, nil
	}

	var selected []string
	configureChannelsNow, err := promptOnboardConfigureNow(
		itext("Configure chat channels now?", "现在配置聊天渠道吗？"),
		itext("You can continue installation now and finish channel setup later in the dashboard.", "你也可以先继续安装，稍后在控制台完成渠道配置。"),
	)
	if err != nil {
		return nil, err
	}
	if !configureChannelsNow {
		return nil, nil
	}

	options := make([]huh.Option[string], 0, len(profiles))
	for _, profile := range profiles {
		options = append(options, huh.NewOption(channelOptionLabel(profile), profile.ID))
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(itext("Select channels to configure (space to toggle)", "选择现在要配置的渠道（空格勾选）")).
				Description(itext("Select any channels you want to configure now. Press enter to skip and finish later in the dashboard.", "选择你现在想配置的渠道。直接回车可先跳过，稍后在控制台继续。")).
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return nil, err
	}
	selections := make([]config.SetupChannelSelection, 0, len(selected))
	for _, id := range selected {
		selection, err := promptSetupChannelSelectionWithCatalog(catalog, id)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(selection.ID) == "" {
			continue
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

func promptSetupChannelSelectionWithCatalog(catalog cliSetupCatalog, id string) (config.SetupChannelSelection, error) {
	profile, ok := catalog.LookupChannelProfile(id)
	if !ok {
		return config.SetupChannelSelection{}, fmt.Errorf("unknown channel %q", id)
	}
	fields := config.EffectiveOperatorChannelFields(profile)
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		value, err := promptSetupChannelField(field)
		if err != nil {
			return config.SetupChannelSelection{}, err
		}
		if value != "" {
			values[field.ID] = value
		}
	}
	return config.SetupChannelSelection{
		ID:     profile.ID,
		Values: values,
	}, nil
}

func setupProviderOptions(catalog cliSetupCatalog) []huh.Option[string] {
	profiles := catalog.ProviderProfiles()
	options := make([]huh.Option[string], 0, len(profiles))
	for _, profile := range profiles {
		options = append(options, huh.NewOption(providerOptionLabel(profile), profile.ID))
	}
	return options
}

func providerPromptOptions(catalog cliSetupCatalog, allowSkip bool) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(catalog.ProviderProfiles())+1)
	if allowSkip {
		options = append(options, huh.NewOption(onboardSkipOptionLabel(), ""))
	}
	options = append(options, setupProviderOptions(catalog)...)
	return options
}

func authModeOptions(catalog cliSetupCatalog, allowSkip bool) []huh.Option[string] {
	profiles := catalog.AuthModeProfiles()
	options := make([]huh.Option[string], 0, len(profiles)+1)
	if allowSkip {
		options = append(options, huh.NewOption(onboardSkipOptionLabel(), onboardSkipOptionValue))
	}
	for _, profile := range profiles {
		options = append(options, huh.NewOption(authModeOptionLabel(profile), profile.ID))
	}
	return options
}

func providerOptionLabel(profile config.SetupProviderProfile) string {
	if installLangIsChinese() {
		return profile.DisplayName
	}
	if strings.TrimSpace(profile.Description) == "" {
		return profile.DisplayName
	}
	return fmt.Sprintf("%s (%s)", profile.DisplayName, profile.Description)
}

func authModeOptionLabel(profile config.AuthModeProfile) string {
	label := profile.DisplayName
	if installLangIsChinese() {
		switch strings.TrimSpace(profile.ID) {
		case "bearer":
			label = "Bearer Token"
		case "apikey":
			label = "API Key"
		case "jwt":
			label = "JWT"
		case "none":
			label = "无认证"
		}
		if profile.Recommended {
			label += "（推荐）"
		}
		return label
	}
	if profile.Recommended {
		label += " (recommended)"
	}
	if strings.TrimSpace(profile.Description) == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, profile.Description)
}

func channelOptionLabel(profile config.ChannelProfile) string {
	if installLangIsChinese() {
		return profile.DisplayName
	}
	if strings.TrimSpace(profile.Description) == "" {
		return profile.DisplayName
	}
	return fmt.Sprintf("%s (%s)", profile.DisplayName, profile.Description)
}

func promptOnboardConfigureNow(title, description string) (bool, error) {
	choice := onboardSkipOptionValue
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(description).
				Options(
					huh.NewOption(itext("Configure now", "现在配置"), onboardConfigureNowOptionValue),
					huh.NewOption(onboardSkipOptionLabel(), onboardSkipOptionValue),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return choice == onboardConfigureNowOptionValue, nil
}
