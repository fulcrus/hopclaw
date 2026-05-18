package model

import (
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/modelrouter"
)

const (
	defaultContextWindow = 128_000
	defaultMaxOutput     = 8_192
)

// CapabilityMatrix reports the vendor-level and runtime-level behavior for a
// configured chat model provider entry.
type CapabilityMatrix struct {
	ProviderName         string      `json:"provider_name"`
	ProviderAPI          ProviderAPI `json:"provider_api"`
	Model                string      `json:"model"`
	DisplayName          string      `json:"display_name"`
	ContextWindow        int         `json:"context_window"`
	MaxOutputTokens      int         `json:"max_output_tokens"`
	SupportsSystemPrompt bool        `json:"supports_system_prompt"`
	SupportsTemperature  bool        `json:"supports_temperature"`
	SupportsMaxTokens    bool        `json:"supports_max_tokens"`
	SupportsTools        bool        `json:"supports_tools"`
	SupportsToolReplay   bool        `json:"supports_tool_replay"`
	SupportsVision       bool        `json:"supports_vision"`
	SupportsReasoning    bool        `json:"supports_reasoning"`
	SupportsStreaming    bool        `json:"supports_streaming"`
	SupportsJSONMode     bool        `json:"supports_json_mode"`
	SupportsEmbeddings   bool        `json:"supports_embeddings"`
	Source               string      `json:"source"`
	Notes                []string    `json:"notes,omitempty"`
}

// HasCapabilityMatrixContract reports whether a capability matrix carries
// non-zero contract data and therefore should override local inference.
func HasCapabilityMatrixContract(matrix CapabilityMatrix) bool {
	return strings.TrimSpace(matrix.ProviderName) != "" ||
		matrix.ProviderAPI != "" ||
		strings.TrimSpace(matrix.Model) != "" ||
		strings.TrimSpace(matrix.DisplayName) != "" ||
		matrix.ContextWindow > 0 ||
		matrix.MaxOutputTokens > 0 ||
		strings.TrimSpace(matrix.Source) != "" ||
		len(matrix.Notes) > 0
}

// CapabilityMatricesForProviders builds a deterministic list of capability
// reports for configured model providers.
func CapabilityMatricesForProviders(providers map[string]ProviderEntry) []CapabilityMatrix {
	if len(providers) == 0 {
		return nil
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]CapabilityMatrix, 0, len(names))
	for _, name := range names {
		out = append(out, CapabilityMatrixForProvider(name, providers[name]))
	}
	return out
}

// CapabilityMatrixForProvider returns the runtime capability report for one
// configured provider entry.
func CapabilityMatrixForProvider(providerName string, entry ProviderEntry) CapabilityMatrix {
	modelID := strings.TrimSpace(entry.DefaultModel)
	meta, ok, source := resolveModelMeta(providerName, modelID)
	matrix := CapabilityMatrix{
		ProviderName:         providerName,
		ProviderAPI:          effectiveProviderAPI(entry),
		Model:                modelID,
		SupportsSystemPrompt: true,
		SupportsTemperature:  true,
		SupportsMaxTokens:    true,
		SupportsToolReplay:   true,
		Source:               source,
	}
	if ok {
		matrix.DisplayName = meta.DisplayName
		matrix.ContextWindow = meta.ContextWindow
		matrix.MaxOutputTokens = meta.MaxOutput
		matrix.SupportsVision = HasCapability(meta, CapVision)
		matrix.SupportsReasoning = HasCapability(meta, CapReasoning)
		matrix.SupportsJSONMode = HasCapability(meta, CapJSONMode)
		matrix.SupportsEmbeddings = HasCapability(meta, CapEmbeddings)
	}
	if matrix.ContextWindow <= 0 {
		matrix.ContextWindow = defaultContextWindowForAPI(matrix.ProviderAPI)
	}
	if matrix.MaxOutputTokens <= 0 {
		matrix.MaxOutputTokens = defaultMaxOutputForAPI(matrix.ProviderAPI)
	}
	matrix.SupportsTools = supportsToolsForAPI(matrix.ProviderAPI)
	matrix.SupportsStreaming = supportsStreamingForAPI(matrix.ProviderAPI)
	if !ok {
		matrix.Source = "api_defaults"
	}
	if matrix.DisplayName == "" && matrix.Model != "" {
		matrix.DisplayName = matrix.Model
	}
	if matrix.Model == "" {
		matrix.Model = defaultModelLabel(providerName)
		matrix.Notes = append(matrix.Notes, "default model is not configured; routing falls back to provider defaults")
	}
	switch matrix.ProviderAPI {
	case APIBedrockConverse:
		matrix.Notes = append(matrix.Notes, "runtime streaming is synthesized from full Bedrock Converse responses")
	}
	if !matrix.SupportsJSONMode {
		matrix.Notes = append(matrix.Notes, "structured json_mode is not implemented as a first-class runtime contract")
	}
	return matrix
}

// CapabilityMatrixForCatalogEntry projects setup-catalog provider metadata onto
// the same capability contract used by configured runtime providers.
func CapabilityMatrixForCatalogEntry(name string, api ProviderAPI, defaultModel string) CapabilityMatrix {
	return CapabilityMatrixForProvider(strings.TrimSpace(name), ProviderEntry{
		API:          NormalizeProviderAPI(api),
		DefaultModel: strings.TrimSpace(defaultModel),
	})
}

// BuildRouterProfiles converts configured providers into runtime router
// profiles. Profiles use runtime-supported capabilities rather than vendor
// marketing claims, so routing does not overpromise unsupported behaviors.
func BuildRouterProfiles(providers map[string]ProviderEntry, defaultProvider string) []modelrouter.ModelProfile {
	return BuildRouterProfilesWithProviderCapabilities(providers, nil, defaultProvider)
}

// BuildRouterProfilesWithProviderCapabilities builds router profiles while
// honoring provider-level runtime capability contracts from operator or other
// higher-level surfaces.
func BuildRouterProfilesWithProviderCapabilities(
	providers map[string]ProviderEntry,
	providerCapabilities map[string]CapabilityMatrix,
	defaultProvider string,
) []modelrouter.ModelProfile {
	if len(providers) == 0 {
		return nil
	}
	defaultProvider = ResolveDefaultProvider(providers, defaultProvider)

	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	var profiles []modelrouter.ModelProfile
	for _, name := range names {
		entry := providers[name]
		contract, hasContract := providerCapabilities[name]
		hasContract = hasContract && HasCapabilityMatrixContract(contract)
		models := knownModelsForProvider(name)
		if len(models) == 0 && strings.TrimSpace(entry.DefaultModel) != "" {
			if meta, ok, _ := resolveModelMeta(name, entry.DefaultModel); ok {
				models = append(models, meta)
			}
		}
		if len(models) == 0 {
			matrix := routerCapabilityMatrixForProvider(name, entry, contract, hasContract)
			profiles = append(profiles, capabilityMatrixToProfile(matrix, defaultProvider, true))
			continue
		}
		seen := make(map[string]struct{}, len(models)+1)
		defaultModel := strings.TrimSpace(entry.DefaultModel)
		for _, meta := range models {
			matrix := routerCapabilityMatrixForProvider(name, ProviderEntry{
				API:          entry.API,
				DefaultModel: meta.Model,
			}, contract, hasContract)
			matrix.ProviderName = name
			profiles = append(profiles, capabilityMatrixToProfile(matrix, defaultProvider, meta.Model == defaultModel))
			seen[meta.Model] = struct{}{}
		}
		if defaultModel != "" {
			if _, ok := seen[defaultModel]; !ok {
				matrix := routerCapabilityMatrixForProvider(name, entry, contract, hasContract)
				profiles = append(profiles, capabilityMatrixToProfile(matrix, defaultProvider, true))
			}
		}
	}
	return profiles
}

// ResolveDefaultProvider normalizes the configured default provider against the
// effective provider map used by runtime and operator surfaces.
func ResolveDefaultProvider(providers map[string]ProviderEntry, defaultProvider string) string {
	defaultProvider = strings.TrimSpace(defaultProvider)
	if defaultProvider != "" {
		if _, ok := providers[defaultProvider]; ok {
			return defaultProvider
		}
	}
	if _, ok := providers["default"]; ok {
		return "default"
	}
	if len(providers) == 1 {
		for name := range providers {
			return name
		}
	}
	return ""
}

func routerCapabilityMatrixForProvider(providerName string, entry ProviderEntry, contract CapabilityMatrix, hasContract bool) CapabilityMatrix {
	matrix := CapabilityMatrixForProvider(providerName, entry)
	if !hasContract {
		return matrix
	}
	return applyRouterProviderCapabilityContract(matrix, contract)
}

func applyRouterProviderCapabilityContract(base, contract CapabilityMatrix) CapabilityMatrix {
	if api := NormalizeProviderAPI(contract.ProviderAPI); api != "" {
		base.ProviderAPI = api
	}
	if provider := strings.TrimSpace(contract.ProviderName); provider != "" {
		base.ProviderName = provider
	}
	base.SupportsSystemPrompt = contract.SupportsSystemPrompt
	base.SupportsTemperature = contract.SupportsTemperature
	base.SupportsMaxTokens = contract.SupportsMaxTokens
	base.SupportsTools = contract.SupportsTools
	base.SupportsToolReplay = contract.SupportsToolReplay
	base.SupportsStreaming = contract.SupportsStreaming
	return base
}

func capabilityMatrixToProfile(matrix CapabilityMatrix, defaultProvider string, preferred bool) modelrouter.ModelProfile {
	priority := 50
	if preferred {
		priority = 100
	}
	profileID := matrix.Model
	if profileID == "" {
		profileID = defaultModelLabel(matrix.ProviderName)
	}
	if strings.TrimSpace(matrix.ProviderName) != "" && strings.TrimSpace(matrix.ProviderName) != strings.TrimSpace(defaultProvider) {
		profileID = strings.TrimSpace(matrix.ProviderName) + "/" + strings.TrimSpace(matrix.Model)
		if strings.TrimSpace(matrix.Model) == "" {
			profileID = strings.TrimSpace(matrix.ProviderName)
		}
	}
	supports := map[modelrouter.Capability]bool{
		modelrouter.CapabilityChat:      true,
		modelrouter.CapabilityTools:     matrix.SupportsTools,
		modelrouter.CapabilityVision:    matrix.SupportsVision,
		modelrouter.CapabilityThinking:  matrix.SupportsReasoning,
		modelrouter.CapabilityJSONMode:  matrix.SupportsJSONMode,
		modelrouter.CapabilityStreaming: matrix.SupportsStreaming,
	}
	return modelrouter.ModelProfile{
		ID:              profileID,
		Provider:        matrix.ProviderName,
		Priority:        priority,
		ContextWindow:   matrix.ContextWindow,
		MaxOutputTokens: matrix.MaxOutputTokens,
		Enabled:         true,
		Supports:        supports,
	}
}

func knownModelsForProvider(providerName string) []ModelMeta {
	models := ModelsForProvider(providerName)
	if len(models) == 0 {
		return nil
	}
	out := make([]ModelMeta, len(models))
	copy(out, models)
	return out
}

func resolveModelMeta(providerName, modelID string) (ModelMeta, bool, string) {
	if modelID == "" {
		return ModelMeta{}, false, "api_defaults"
	}
	if meta, ok := LookupModel(providerName, modelID); ok {
		return meta, true, "known_model_exact"
	}
	var match *ModelMeta
	for _, candidate := range KnownModels() {
		if candidate.Model != modelID {
			continue
		}
		if match != nil {
			return ModelMeta{}, false, "api_defaults"
		}
		copied := candidate
		match = &copied
	}
	if match != nil {
		return *match, true, "known_model_alias"
	}
	return ModelMeta{}, false, "api_defaults"
}

func effectiveProviderAPI(entry ProviderEntry) ProviderAPI {
	if api := NormalizeProviderAPI(entry.API); api != "" {
		return api
	}
	return APIOpenAICompletions
}

func defaultContextWindowForAPI(api ProviderAPI) int {
	switch api {
	case APIAnthropicMessages:
		return 200_000
	case APIGoogleGenerativeAI:
		return 1_000_000
	case APIBedrockConverse:
		return 200_000
	default:
		return defaultContextWindow
	}
}

func defaultMaxOutputForAPI(api ProviderAPI) int {
	switch api {
	case APIAnthropicMessages:
		return 8_192
	case APIGoogleGenerativeAI:
		return 8_192
	case APIBedrockConverse:
		return 4_096
	default:
		return defaultMaxOutput
	}
}

func supportsToolsForAPI(api ProviderAPI) bool {
	switch api {
	case APIOpenAICompletions, APIOpenAIResponses, APIOllama, APIAnthropicMessages, APIGoogleGenerativeAI, APIBedrockConverse, APIGitHubCopilot:
		return true
	default:
		return false
	}
}

func supportsStreamingForAPI(api ProviderAPI) bool {
	switch api {
	case APIOpenAICompletions, APIOpenAIResponses, APIOllama, APIAnthropicMessages, APIGitHubCopilot, APIGoogleGenerativeAI, APIBedrockConverse:
		return true
	default:
		return false
	}
}

func defaultModelLabel(providerName string) string {
	if strings.TrimSpace(providerName) == "" {
		return "default"
	}
	return strings.TrimSpace(providerName) + "/default"
}
