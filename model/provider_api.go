package model

import "strings"

// NormalizeProviderAPI folds legacy aliases into the canonical provider API IDs
// used by the registry and operator surfaces.
func NormalizeProviderAPI(api ProviderAPI) ProviderAPI {
	normalized := strings.TrimSpace(strings.ToLower(string(api)))
	switch normalized {
	case "":
		return ""
	case "openai", "custom", string(APIOpenAICompletions):
		return APIOpenAICompletions
	case "responses", string(APIOpenAIResponses):
		return APIOpenAIResponses
	case "anthropic", string(APIAnthropicMessages):
		return APIAnthropicMessages
	case "google", "gemini", string(APIGoogleGenerativeAI):
		return APIGoogleGenerativeAI
	case "bedrock", "amazon-bedrock", string(APIBedrockConverse):
		return APIBedrockConverse
	case string(APIOllama):
		return APIOllama
	case "copilot", string(APIGitHubCopilot):
		return APIGitHubCopilot
	default:
		return ProviderAPI(normalized)
	}
}
