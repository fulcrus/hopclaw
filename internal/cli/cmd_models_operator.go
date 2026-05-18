package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/model"

	"github.com/spf13/cobra"
)

const (
	modelsValidatePath = "/operator/models/validate"
	modelsTestChatPath = "/operator/models/test-chat"
)

type modelsNamedOKResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type modelsOperatorModelMeta struct {
	Provider      string                  `json:"provider"`
	Model         string                  `json:"model"`
	DisplayName   string                  `json:"display_name,omitempty"`
	ContextWindow int                     `json:"context_window,omitempty"`
	MaxOutput     int                     `json:"max_output,omitempty"`
	Capabilities  []model.ModelCapability `json:"capabilities,omitempty"`
}

type modelsValidateResponse struct {
	Valid   bool                      `json:"valid"`
	Message string                    `json:"message"`
	Models  []modelsOperatorModelMeta `json:"models"`
}

type modelsTestChatResponse struct {
	OK        bool   `json:"ok"`
	Reply     string `json:"reply"`
	LatencyMS int64  `json:"latency_ms"`
	Tokens    int    `json:"tokens"`
}

type modelsProviderMutationRequest struct {
	Name         *string            `json:"name,omitempty"`
	API          *string            `json:"api,omitempty"`
	BaseURL      *string            `json:"base_url,omitempty"`
	Region       *string            `json:"region,omitempty"`
	APIKey       *string            `json:"api_key,omitempty"`
	APIKeys      *[]string          `json:"api_keys,omitempty"`
	AccessKeyID  *string            `json:"access_key_id,omitempty"`
	SecretKey    *string            `json:"secret_key,omitempty"`
	SessionToken *string            `json:"session_token,omitempty"`
	DefaultModel *string            `json:"default_model,omitempty"`
	Timeout      *string            `json:"timeout,omitempty"`
	Headers      *map[string]string `json:"headers,omitempty"`
}

type modelsProviderConnectionInput struct {
	Provider        string            `json:"provider"`
	CatalogProvider string            `json:"catalog_provider,omitempty"`
	API             string            `json:"api,omitempty"`
	BaseURL         string            `json:"base_url,omitempty"`
	Region          string            `json:"region,omitempty"`
	APIKey          string            `json:"api_key,omitempty"`
	APIKeys         []string          `json:"api_keys,omitempty"`
	AccessKeyID     string            `json:"access_key_id,omitempty"`
	SecretKey       string            `json:"secret_key,omitempty"`
	SessionToken    string            `json:"session_token,omitempty"`
	DefaultModel    string            `json:"default_model,omitempty"`
	Timeout         string            `json:"timeout,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

type modelsTestChatRequest struct {
	modelsProviderConnectionInput
	Message string `json:"message"`
}

type modelsProviderCommandOptions struct {
	CatalogProvider string
	API             string
	Set             []string
	Clear           []string
	Interactive     bool
	Message         string
}

type modelsProviderCommandContext struct {
	Name                   string
	Existing               bool
	Mutable                bool
	EffectiveAPI           string
	RequestAPI             string
	RequestCatalogProvider string
	PromptProvider         string
	APIProfile             config.ProviderAPIProfile
}

type modelsProviderFieldValues struct {
	Scalars map[string]string
	Lists   map[string][]string
	Maps    map[string]map[string]string
}

func newModelsAddCmd() *cobra.Command {
	var opts modelsProviderCommandOptions

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a model provider",
		Long:  "Add a model provider through the operator surface using schema-driven provider fields.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsAdd(cmd.Context(), args[0], opts)
		},
	}

	bindModelsProviderCommandFlags(cmd, &opts, false, false)
	return cmd
}

func newModelsUpdateCmd() *cobra.Command {
	var opts modelsProviderCommandOptions

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a model provider",
		Long:  "Update a model provider through the operator surface using schema-driven provider fields.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsUpdate(cmd.Context(), args[0], opts)
		},
	}

	bindModelsProviderCommandFlags(cmd, &opts, true, false)
	return cmd
}

func newModelsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a model provider",
		Long:  "Delete a mutable model provider from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsDelete(cmd.Context(), args[0])
		},
	}
}

func newModelsValidateCmd() *cobra.Command {
	var opts modelsProviderCommandOptions

	cmd := &cobra.Command{
		Use:   "validate <name>",
		Short: "Validate provider connectivity",
		Long:  "Validate an existing or temporary provider configuration through the operator model validation surface.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsValidate(cmd.Context(), args[0], opts)
		},
	}

	bindModelsProviderCommandFlags(cmd, &opts, false, false)
	return cmd
}

func newModelsTestChatCmd() *cobra.Command {
	var opts modelsProviderCommandOptions

	cmd := &cobra.Command{
		Use:   "test-chat <name>",
		Short: "Send a temporary chat through a provider",
		Long:  "Send a test chat through an existing or temporary provider configuration using the operator test-chat surface.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsTestChat(cmd.Context(), args[0], opts)
		},
	}

	bindModelsProviderCommandFlags(cmd, &opts, false, true)
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func bindModelsProviderCommandFlags(cmd *cobra.Command, opts *modelsProviderCommandOptions, allowClear, includeMessage bool) {
	cmd.Flags().StringVar(&opts.CatalogProvider, "catalog-provider", "", "built-in provider preset to use when the configured provider name differs")
	cmd.Flags().StringVar(&opts.API, "api", "", "provider API type such as openai-completions, anthropic-messages, google-generative-ai, or bedrock-converse")
	cmd.Flags().StringArrayVar(&opts.Set, "set", nil, "set provider fields as field=value; repeat as needed")
	cmd.Flags().BoolVar(&opts.Interactive, "interactive", false, "prompt for provider fields interactively using the provider API schema")
	if allowClear {
		cmd.Flags().StringArrayVar(&opts.Clear, "clear", nil, "clear a provider field by field id; repeat as needed")
	}
	if includeMessage {
		cmd.Flags().StringVar(&opts.Message, "message", "", "chat message to send through the temporary provider")
	}
}

func runModelsAdd(ctx context.Context, name string, opts modelsProviderCommandOptions) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	state, hasState := loadModelsOperatorStateBestEffort(ctx, client)
	name = strings.TrimSpace(name)
	if hasState {
		if _, ok := state.Providers[name]; ok {
			return fmt.Errorf("provider %q already exists", name)
		}
	}

	req, commandCtx, err := buildModelsProviderMutationRequest(catalog, name, opts, state, hasState, true)
	if err != nil {
		return err
	}

	var resp modelsNamedOKResponse
	if err := client.Post(ctx, modelsBasePath, req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Added provider %q\n", resp.Name)
	if commandCtx.RequestAPI != "" {
		fmt.Printf("API: %s\n", commandCtx.RequestAPI)
	}
	return nil
}

func runModelsUpdate(ctx context.Context, name string, opts modelsProviderCommandOptions) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	state, hasState := loadModelsOperatorStateBestEffort(ctx, client)
	name = strings.TrimSpace(name)
	if err := ensureModelsProviderMutable(state, hasState, name); err != nil {
		return err
	}

	req, _, err := buildModelsProviderMutationRequest(catalog, name, opts, state, hasState, false)
	if err != nil {
		return err
	}

	var resp modelsNamedOKResponse
	if err := client.Put(ctx, modelsBasePath+"/"+name, req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Updated provider %q\n", resp.Name)
	return nil
}

func runModelsDelete(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	state, hasState := loadModelsOperatorStateBestEffort(ctx, client)
	name = strings.TrimSpace(name)
	if err := ensureModelsProviderMutable(state, hasState, name); err != nil {
		return err
	}

	var resp modelsNamedOKResponse
	if err := client.Delete(ctx, modelsBasePath+"/"+name, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Deleted provider %q\n", resp.Name)
	return nil
}

func runModelsValidate(ctx context.Context, name string, opts modelsProviderCommandOptions) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	state, hasState := loadModelsOperatorStateBestEffort(ctx, client)
	req, commandCtx, err := buildModelsProviderConnectionInput(catalog, name, opts, state, hasState)
	if err != nil {
		return err
	}

	var resp modelsValidateResponse
	if err := client.Post(ctx, modelsValidatePath, req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Provider: %s\n", commandCtx.Name)
	if commandCtx.RequestAPI != "" {
		fmt.Printf("API:      %s\n", commandCtx.RequestAPI)
	}
	fmt.Printf("Valid:    %v\n", resp.Valid)
	if resp.Message != "" {
		fmt.Printf("Message:  %s\n", resp.Message)
	}
	if len(resp.Models) > 0 {
		sort.SliceStable(resp.Models, func(i, j int) bool {
			return resp.Models[i].Model < resp.Models[j].Model
		})
		fmt.Println("Models:")
		for _, item := range resp.Models {
			label := item.Model
			if strings.TrimSpace(item.DisplayName) != "" && item.DisplayName != item.Model {
				label = item.DisplayName + " (" + item.Model + ")"
			}
			fmt.Printf("  %s\n", label)
		}
	}
	return nil
}

func runModelsTestChat(ctx context.Context, name string, opts modelsProviderCommandOptions) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	state, hasState := loadModelsOperatorStateBestEffort(ctx, client)
	input, commandCtx, err := buildModelsProviderConnectionInput(catalog, name, opts, state, hasState)
	if err != nil {
		return err
	}
	message := strings.TrimSpace(opts.Message)
	if message == "" {
		return fmt.Errorf("message is required")
	}

	var resp modelsTestChatResponse
	if err := client.Post(ctx, modelsTestChatPath, modelsTestChatRequest{
		modelsProviderConnectionInput: input,
		Message:                       message,
	}, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Provider: %s\n", commandCtx.Name)
	if commandCtx.RequestAPI != "" {
		fmt.Printf("API:      %s\n", commandCtx.RequestAPI)
	}
	fmt.Printf("OK:       %v\n", resp.OK)
	if resp.LatencyMS > 0 {
		fmt.Printf("Latency:  %d ms\n", resp.LatencyMS)
	}
	if resp.Tokens > 0 {
		fmt.Printf("Tokens:   %d\n", resp.Tokens)
	}
	if resp.Reply != "" {
		fmt.Printf("Reply:    %s\n", resp.Reply)
	}
	return nil
}

func buildModelsProviderMutationRequest(catalog cliSetupCatalog, name string, opts modelsProviderCommandOptions, state modelProviderState, hasState, create bool) (modelsProviderMutationRequest, modelsProviderCommandContext, error) {
	commandCtx, err := resolveModelsProviderCommandContext(catalog, strings.TrimSpace(name), opts.CatalogProvider, opts.API, state, hasState)
	if err != nil {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
	}

	promptValues, err := collectModelsProviderPromptValues(catalog, commandCtx, opts.Interactive, !create)
	if err != nil {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
	}
	values, err := buildModelsProviderFieldValues(catalog, promptValues, opts.Set)
	if err != nil {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
	}
	if create {
		applyModelsProviderDefaults(catalog, &values, commandCtx)
		if err := validateModelsProviderCreateValues(catalog, commandCtx, values); err != nil {
			return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
		}
	}

	clearSet, err := normalizeModelsProviderClearFields(catalog, opts.Clear)
	if err != nil {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
	}
	if create && len(clearSet) > 0 {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, fmt.Errorf("clear flags are only supported on update")
	}
	if err := validateModelsProviderFieldConflicts(values, clearSet); err != nil {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, err
	}

	req := modelsProviderMutationRequest{}
	name = strings.TrimSpace(name)
	req.Name = &name
	if strings.TrimSpace(commandCtx.RequestAPI) != "" {
		req.API = stringPtr(commandCtx.RequestAPI)
	}
	applyModelsProviderMutationValues(&req, values)
	applyModelsProviderMutationClears(&req, clearSet)
	if !create && !modelsProviderMutationRequestHasChanges(req) {
		return modelsProviderMutationRequest{}, modelsProviderCommandContext{}, fmt.Errorf("no provider changes requested")
	}
	return req, commandCtx, nil
}

func buildModelsProviderConnectionInput(catalog cliSetupCatalog, name string, opts modelsProviderCommandOptions, state modelProviderState, hasState bool) (modelsProviderConnectionInput, modelsProviderCommandContext, error) {
	commandCtx, err := resolveModelsProviderCommandContext(catalog, strings.TrimSpace(name), opts.CatalogProvider, opts.API, state, hasState)
	if err != nil {
		return modelsProviderConnectionInput{}, modelsProviderCommandContext{}, err
	}

	promptValues, err := collectModelsProviderPromptValues(catalog, commandCtx, opts.Interactive, commandCtx.Existing)
	if err != nil {
		return modelsProviderConnectionInput{}, modelsProviderCommandContext{}, err
	}
	values, err := buildModelsProviderFieldValues(catalog, promptValues, opts.Set)
	if err != nil {
		return modelsProviderConnectionInput{}, modelsProviderCommandContext{}, err
	}
	if !commandCtx.Existing {
		applyModelsProviderDefaults(catalog, &values, commandCtx)
		if err := validateModelsProviderCreateValues(catalog, commandCtx, values); err != nil {
			return modelsProviderConnectionInput{}, modelsProviderCommandContext{}, err
		}
	}
	if len(opts.Clear) > 0 {
		return modelsProviderConnectionInput{}, modelsProviderCommandContext{}, fmt.Errorf("clear flags are not supported for validate or test-chat")
	}

	input := modelsProviderConnectionInput{
		Provider: strings.TrimSpace(name),
	}
	if strings.TrimSpace(commandCtx.RequestCatalogProvider) != "" {
		input.CatalogProvider = commandCtx.RequestCatalogProvider
	}
	if strings.TrimSpace(commandCtx.RequestAPI) != "" {
		input.API = commandCtx.RequestAPI
	}
	applyModelsProviderConnectionValues(&input, values)
	return input, commandCtx, nil
}

func resolveModelsProviderCommandContext(catalog cliSetupCatalog, name, catalogProvider, api string, state modelProviderState, hasState bool) (modelsProviderCommandContext, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return modelsProviderCommandContext{}, fmt.Errorf("provider name is required")
	}

	existing := false
	mutable := true
	var entry model.ProviderEntry
	if hasState {
		var ok bool
		entry, ok = state.Providers[name]
		existing = ok
		if existing {
			mutable = modelProviderDetailForState(state, name, entry).Mutable
		}
	}

	catalogProvider = strings.TrimSpace(strings.ToLower(catalogProvider))
	requestCatalogProvider := ""
	promptProvider := ""
	requestAPI := ""
	effectiveAPI := ""

	if catalogProvider != "" {
		profile, ok := catalog.LookupProviderProfile(catalogProvider)
		if !ok {
			return modelsProviderCommandContext{}, fmt.Errorf("unknown catalog provider %q", catalogProvider)
		}
		requestCatalogProvider = profile.ID
		promptProvider = profile.ID
		effectiveAPI = strings.TrimSpace(profile.API)
		requestAPI = effectiveAPI
	}

	api = normalizeModelsProviderAPI(api)
	if api != "" {
		if _, ok := catalog.LookupProviderAPIProfile(api); !ok {
			return modelsProviderCommandContext{}, fmt.Errorf("unsupported provider api %q", api)
		}
		if effectiveAPI != "" && effectiveAPI != api {
			return modelsProviderCommandContext{}, fmt.Errorf("catalog provider %q expects api %q, got %q", catalogProvider, effectiveAPI, api)
		}
		effectiveAPI = api
		requestAPI = api
		if promptProvider == "" {
			promptProvider = promptProviderForAPI(catalog, api)
		}
	}

	if !existing {
		if promptProvider == "" {
			if profile, ok := catalog.LookupProviderProfile(strings.ToLower(name)); ok {
				promptProvider = profile.ID
				if requestCatalogProvider == "" {
					requestCatalogProvider = profile.ID
				}
				if effectiveAPI == "" {
					effectiveAPI = strings.TrimSpace(profile.API)
				}
				if requestAPI == "" {
					requestAPI = effectiveAPI
				}
			}
		}
	} else if effectiveAPI == "" {
		effectiveAPI = string(effectiveModelProviderAPI(entry))
	}

	if promptProvider == "" {
		if profile, ok := catalog.LookupProviderProfile(strings.ToLower(name)); ok {
			promptProvider = profile.ID
		} else {
			promptProvider = promptProviderForAPI(catalog, effectiveAPI)
		}
	}
	if promptProvider == "" {
		promptProvider = "custom"
	}

	if existing && requestCatalogProvider != "" && catalogProvider == "" {
		requestCatalogProvider = ""
	}
	if existing && requestAPI != "" && catalogProvider == "" && api == "" {
		requestAPI = ""
	}

	if effectiveAPI == "" {
		return modelsProviderCommandContext{}, fmt.Errorf("provider %q is not in the built-in catalog; use --catalog-provider or --api", name)
	}
	apiProfile, ok := catalog.LookupProviderAPIProfile(effectiveAPI)
	if !ok {
		return modelsProviderCommandContext{}, fmt.Errorf("unsupported provider api %q", effectiveAPI)
	}

	return modelsProviderCommandContext{
		Name:                   name,
		Existing:               existing,
		Mutable:                mutable,
		EffectiveAPI:           effectiveAPI,
		RequestAPI:             requestAPI,
		RequestCatalogProvider: requestCatalogProvider,
		PromptProvider:         promptProvider,
		APIProfile:             apiProfile,
	}, nil
}

func collectModelsProviderPromptValues(catalog cliSetupCatalog, commandCtx modelsProviderCommandContext, interactive, patch bool) (map[string]string, error) {
	if !interactive {
		return nil, nil
	}
	return promptModelsProviderValues(catalog, commandCtx.PromptProvider, commandCtx.EffectiveAPI, patch)
}

func promptModelsProviderValues(catalog cliSetupCatalog, provider, api string, patch bool) (map[string]string, error) {
	apiProfile, ok := catalog.LookupProviderAPIProfile(api)
	if !ok {
		return nil, fmt.Errorf("unsupported provider api %q", api)
	}

	fields := make([]config.SetupProviderField, len(apiProfile.Fields))
	copy(fields, apiProfile.Fields)
	if patch {
		for i := range fields {
			fields[i].Required = false
		}
	}

	values := make(map[string]string, len(fields))
	basicFields, advancedFields := splitProviderSetupFields(fields)
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

func buildModelsProviderFieldValues(catalog cliSetupCatalog, promptValues map[string]string, rawAssignments []string) (modelsProviderFieldValues, error) {
	values := newModelsProviderFieldValues()
	fieldCatalog := modelsProviderCommandFieldCatalog(catalog)
	if err := applyModelsProviderPromptValues(&values, promptValues, fieldCatalog); err != nil {
		return modelsProviderFieldValues{}, err
	}
	if err := applyModelsProviderAssignments(&values, rawAssignments, fieldCatalog); err != nil {
		return modelsProviderFieldValues{}, err
	}
	return values, nil
}

func newModelsProviderFieldValues() modelsProviderFieldValues {
	return modelsProviderFieldValues{
		Scalars: make(map[string]string),
		Lists:   make(map[string][]string),
		Maps:    make(map[string]map[string]string),
	}
}

func applyModelsProviderPromptValues(values *modelsProviderFieldValues, promptValues map[string]string, fieldCatalog map[string]config.SetupProviderField) error {
	for fieldID, raw := range promptValues {
		field, ok := fieldCatalog[fieldID]
		if !ok {
			return fmt.Errorf("unsupported provider field %q", fieldID)
		}
		if err := applyModelsProviderAssignmentValue(values, field, raw, true); err != nil {
			return err
		}
	}
	return nil
}

func applyModelsProviderAssignments(values *modelsProviderFieldValues, rawAssignments []string, fieldCatalog map[string]config.SetupProviderField) error {
	overrides := make(map[string]bool)
	for _, item := range rawAssignments {
		fieldID, raw, err := parseModelsProviderAssignment(item)
		if err != nil {
			return err
		}
		field, ok := fieldCatalog[fieldID]
		if !ok {
			return fmt.Errorf("unsupported provider field %q", fieldID)
		}
		if !overrides[fieldID] {
			clearModelsProviderField(values, fieldID)
			overrides[fieldID] = true
		}
		if err := applyModelsProviderAssignmentValue(values, field, raw, false); err != nil {
			return err
		}
	}
	return nil
}

func applyModelsProviderAssignmentValue(values *modelsProviderFieldValues, field config.SetupProviderField, raw string, fromPrompt bool) error {
	switch config.SetupProviderFieldType(field) {
	case "string_list":
		items := config.SplitSetupProviderFieldList(raw)
		if len(items) == 0 {
			if fromPrompt {
				return nil
			}
			return fmt.Errorf("field %q requires at least one value; use --clear %s to clear it", field.ID, field.ID)
		}
		values.Lists[field.ID] = append(values.Lists[field.ID], items...)
	case "string_map":
		items, err := parseModelsProviderMapAssignments(raw)
		if err != nil {
			return fmt.Errorf("field %q: %w", field.ID, err)
		}
		if len(items) == 0 {
			if fromPrompt {
				return nil
			}
			return fmt.Errorf("field %q requires at least one key/value pair; use --clear %s to clear it", field.ID, field.ID)
		}
		if values.Maps[field.ID] == nil {
			values.Maps[field.ID] = make(map[string]string, len(items))
		}
		for key, value := range items {
			values.Maps[field.ID][key] = value
		}
	default:
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if fromPrompt {
				return nil
			}
			return fmt.Errorf("field %q requires a non-empty value; use --clear %s to clear it", field.ID, field.ID)
		}
		values.Scalars[field.ID] = trimmed
	}
	return nil
}

func clearModelsProviderField(values *modelsProviderFieldValues, fieldID string) {
	delete(values.Scalars, fieldID)
	delete(values.Lists, fieldID)
	delete(values.Maps, fieldID)
}

func applyModelsProviderDefaults(catalog cliSetupCatalog, values *modelsProviderFieldValues, commandCtx modelsProviderCommandContext) {
	for _, field := range commandCtx.APIProfile.Fields {
		if values.hasField(field) {
			continue
		}
		if value := defaultModelsProviderFieldValue(catalog, commandCtx.PromptProvider, commandCtx.EffectiveAPI, field); value != "" {
			switch config.SetupProviderFieldType(field) {
			case "string_list":
				values.Lists[field.ID] = config.SplitSetupProviderFieldList(value)
			case "string_map":
				items, err := parseModelsProviderMapAssignments(value)
				if err == nil && len(items) > 0 {
					values.Maps[field.ID] = items
				}
			default:
				values.Scalars[field.ID] = strings.TrimSpace(value)
			}
		}
	}
}

func defaultModelsProviderFieldValue(catalog cliSetupCatalog, provider, api string, field config.SetupProviderField) string {
	if value := strings.TrimSpace(field.DefaultValue); value != "" {
		return value
	}
	switch field.ID {
	case "base_url":
		if value := strings.TrimSpace(catalog.DefaultBaseURL(provider)); value != "" {
			return value
		}
	case "default_model":
		if value := strings.TrimSpace(catalog.DefaultModelForProvider(provider)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(catalog.ProviderAPIFieldDefault(api, field.ID))
}

func validateModelsProviderCreateValues(catalog cliSetupCatalog, commandCtx modelsProviderCommandContext, values modelsProviderFieldValues) error {
	missing := make([]string, 0, 4)
	for _, field := range commandCtx.APIProfile.Fields {
		if field.Required && !values.hasField(field) {
			missing = append(missing, field.Label)
		}
	}
	if strings.TrimSpace(values.Scalars["default_model"]) == "" {
		missing = append(missing, "Default Model")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s requires %s", providerCommandDisplayName(catalog, commandCtx), strings.Join(missing, ", "))
	}
	return nil
}

func normalizeModelsProviderClearFields(catalog cliSetupCatalog, raw []string) (map[string]struct{}, error) {
	fieldCatalog := modelsProviderCommandFieldCatalog(catalog)
	out := make(map[string]struct{}, len(raw))
	for _, fieldID := range raw {
		fieldID = strings.TrimSpace(fieldID)
		if fieldID == "" {
			return nil, fmt.Errorf("clear field id cannot be empty")
		}
		if _, ok := fieldCatalog[fieldID]; !ok {
			return nil, fmt.Errorf("unsupported provider field %q", fieldID)
		}
		out[fieldID] = struct{}{}
	}
	return out, nil
}

func validateModelsProviderFieldConflicts(values modelsProviderFieldValues, clearSet map[string]struct{}) error {
	for fieldID := range clearSet {
		if values.hasFieldID(fieldID) {
			return fmt.Errorf("field %q cannot be set and cleared in the same command", fieldID)
		}
	}
	return nil
}

func applyModelsProviderMutationValues(req *modelsProviderMutationRequest, values modelsProviderFieldValues) {
	for fieldID, value := range values.Scalars {
		switch fieldID {
		case "base_url":
			req.BaseURL = stringPtr(value)
		case "region":
			req.Region = stringPtr(value)
		case "api_key":
			req.APIKey = stringPtr(value)
		case "access_key_id":
			req.AccessKeyID = stringPtr(value)
		case "secret_key":
			req.SecretKey = stringPtr(value)
		case "session_token":
			req.SessionToken = stringPtr(value)
		case "default_model":
			req.DefaultModel = stringPtr(value)
		case "timeout":
			req.Timeout = stringPtr(value)
		}
	}
	for fieldID, items := range values.Lists {
		if fieldID == "api_keys" {
			req.APIKeys = stringSlicePtr(items)
		}
	}
	for fieldID, items := range values.Maps {
		if fieldID == "headers" {
			req.Headers = stringMapPtr(items)
		}
	}
}

func applyModelsProviderMutationClears(req *modelsProviderMutationRequest, clearSet map[string]struct{}) {
	for fieldID := range clearSet {
		switch fieldID {
		case "base_url":
			req.BaseURL = stringPtr("")
		case "region":
			req.Region = stringPtr("")
		case "api_key":
			req.APIKey = stringPtr("")
		case "api_keys":
			req.APIKeys = stringSlicePtr(nil)
		case "access_key_id":
			req.AccessKeyID = stringPtr("")
		case "secret_key":
			req.SecretKey = stringPtr("")
		case "session_token":
			req.SessionToken = stringPtr("")
		case "default_model":
			req.DefaultModel = stringPtr("")
		case "timeout":
			req.Timeout = stringPtr("")
		case "headers":
			req.Headers = stringMapPtr(nil)
		}
	}
}

func modelsProviderMutationRequestHasChanges(req modelsProviderMutationRequest) bool {
	return req.API != nil ||
		req.BaseURL != nil ||
		req.Region != nil ||
		req.APIKey != nil ||
		req.APIKeys != nil ||
		req.AccessKeyID != nil ||
		req.SecretKey != nil ||
		req.SessionToken != nil ||
		req.DefaultModel != nil ||
		req.Timeout != nil ||
		req.Headers != nil
}

func applyModelsProviderConnectionValues(input *modelsProviderConnectionInput, values modelsProviderFieldValues) {
	for fieldID, value := range values.Scalars {
		switch fieldID {
		case "base_url":
			input.BaseURL = value
		case "region":
			input.Region = value
		case "api_key":
			input.APIKey = value
		case "access_key_id":
			input.AccessKeyID = value
		case "secret_key":
			input.SecretKey = value
		case "session_token":
			input.SessionToken = value
		case "default_model":
			input.DefaultModel = value
		case "timeout":
			input.Timeout = value
		}
	}
	if items := values.Lists["api_keys"]; len(items) > 0 {
		input.APIKeys = append([]string(nil), items...)
	}
	if items := values.Maps["headers"]; len(items) > 0 {
		input.Headers = cloneModelHeaders(items)
	}
}

func ensureModelsProviderMutable(state modelProviderState, hasState bool, name string) error {
	if !hasState {
		return nil
	}
	entry, ok := state.Providers[name]
	if !ok {
		return fmt.Errorf("provider %q not found", name)
	}
	detail := modelProviderDetailForState(state, name, entry)
	if !detail.Mutable {
		return fmt.Errorf("provider %q is read-only in the effective config surface", name)
	}
	return nil
}

func loadModelsOperatorStateBestEffort(ctx context.Context, client *GatewayClient) (modelProviderState, bool) {
	state, err := loadOperatorModelState(ctx, client)
	if err != nil {
		return modelProviderState{}, false
	}
	return state, true
}

func providerCommandDisplayName(catalog cliSetupCatalog, commandCtx modelsProviderCommandContext) string {
	if profile, ok := catalog.LookupProviderProfile(commandCtx.PromptProvider); ok {
		return profile.DisplayName
	}
	return commandCtx.Name
}

func promptProviderForAPI(catalog cliSetupCatalog, api string) string {
	api = normalizeModelsProviderAPI(api)
	if api == "" {
		return "custom"
	}
	var matches []string
	for _, profile := range catalog.ProviderProfiles() {
		if normalizeModelsProviderAPI(profile.API) == api {
			matches = append(matches, profile.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return "custom"
}

func modelsProviderCommandFieldCatalog(catalog cliSetupCatalog) map[string]config.SetupProviderField {
	fields := make(map[string]config.SetupProviderField)
	for _, profile := range catalog.ProviderAPIProfiles() {
		for _, field := range profile.Fields {
			if strings.TrimSpace(field.ID) == "" {
				continue
			}
			if _, ok := fields[field.ID]; ok {
				continue
			}
			fields[field.ID] = field
		}
	}
	return fields
}

func parseModelsProviderAssignment(raw string) (string, string, error) {
	index := strings.Index(raw, "=")
	if index <= 0 {
		return "", "", fmt.Errorf("invalid --set value %q: expected field=value", raw)
	}
	fieldID := strings.TrimSpace(raw[:index])
	if fieldID == "" {
		return "", "", fmt.Errorf("invalid --set value %q: field id is required", raw)
	}
	return fieldID, raw[index+1:], nil
}

func parseModelsProviderMapAssignments(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	out := make(map[string]string, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		separator := strings.Index(line, ":")
		equals := strings.Index(line, "=")
		switch {
		case separator > 0 && (equals < 0 || separator < equals):
			out[strings.TrimSpace(line[:separator])] = strings.TrimSpace(line[separator+1:])
		case equals > 0:
			out[strings.TrimSpace(line[:equals])] = strings.TrimSpace(line[equals+1:])
		default:
			return nil, fmt.Errorf("invalid map entry %q: expected key=value or key: value", line)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeModelsProviderAPI(api string) string {
	return string(model.NormalizeProviderAPI(model.ProviderAPI(strings.TrimSpace(api))))
}

func (v modelsProviderFieldValues) hasField(field config.SetupProviderField) bool {
	return v.hasFieldID(field.ID)
}

func (v modelsProviderFieldValues) hasFieldID(fieldID string) bool {
	if strings.TrimSpace(v.Scalars[fieldID]) != "" {
		return true
	}
	if len(v.Lists[fieldID]) > 0 {
		return true
	}
	if len(v.Maps[fieldID]) > 0 {
		return true
	}
	return false
}

func stringPtr(value string) *string {
	return &value
}

func stringSlicePtr(values []string) *[]string {
	copyValues := append([]string(nil), values...)
	return &copyValues
}

func stringMapPtr(values map[string]string) *map[string]string {
	copyValues := cloneModelHeaders(values)
	return &copyValues
}
