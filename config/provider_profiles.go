package config

import (
	"strings"

	"github.com/fulcrus/hopclaw/model"
)

type SetupProviderProfile struct {
	ID               string                 `json:"id"`
	DisplayName      string                 `json:"display_name"`
	Description      string                 `json:"description,omitempty"`
	API              string                 `json:"api,omitempty"`
	BaseURL          string                 `json:"base_url,omitempty"`
	DefaultModels    []string               `json:"default_models,omitempty"`
	EnvVars          []string               `json:"env_vars,omitempty"`
	APIKeyHint       string                 `json:"api_key_hint,omitempty"`
	CapabilityMatrix model.CapabilityMatrix `json:"capability_matrix,omitempty"`
}

var setupProviderProfiles = []SetupProviderProfile{
	{
		ID:            "openai",
		DisplayName:   "OpenAI",
		Description:   "GPT-4o, GPT-4.1, o3 and related models",
		API:           "openai-completions",
		BaseURL:       "https://api.openai.com/v1",
		DefaultModels: []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "o3-mini"},
		EnvVars:       []string{"OPENAI_API_KEY"},
		APIKeyHint:    "Enter your OpenAI API key (sk-...)",
	},
	{
		ID:            "anthropic",
		DisplayName:   "Anthropic",
		Description:   "Claude Sonnet, Opus, and Haiku",
		API:           "anthropic-messages",
		BaseURL:       "https://api.anthropic.com",
		DefaultModels: []string{"claude-sonnet-4-20250514", "claude-haiku-4-20250414", "claude-opus-4-20250514"},
		EnvVars:       []string{"ANTHROPIC_API_KEY"},
		APIKeyHint:    "Enter your Anthropic API key (sk-ant-...)",
	},
	{
		ID:            "google",
		DisplayName:   "Google",
		Description:   "Gemini 2.x models",
		API:           "google-generative-ai",
		BaseURL:       "https://generativelanguage.googleapis.com",
		DefaultModels: []string{"gemini-2.0-flash", "gemini-2.5-pro", "gemini-2.5-flash"},
		EnvVars:       []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"},
		APIKeyHint:    "Enter your Google AI API key",
	},
	{
		ID:            "deepseek",
		DisplayName:   "DeepSeek",
		Description:   "DeepSeek Chat and Reasoner",
		API:           "openai-completions",
		BaseURL:       "https://api.deepseek.com/v1",
		DefaultModels: []string{"deepseek-chat", "deepseek-reasoner"},
		EnvVars:       []string{"DEEPSEEK_API_KEY"},
		APIKeyHint:    "Enter your DeepSeek API key",
	},
	{
		ID:            "moonshot",
		DisplayName:   "Moonshot / Kimi",
		Description:   "Kimi K2.5 and Thinking models",
		API:           "openai-completions",
		BaseURL:       "https://api.moonshot.ai/v1",
		DefaultModels: []string{"kimi-k2.5", "kimi-k2-thinking", "kimi-k2-turbo-preview"},
		EnvVars:       []string{"MOONSHOT_API_KEY", "KIMI_API_KEY"},
		APIKeyHint:    "Enter your Moonshot / Kimi API key",
	},
	{
		ID:            "kimi-coding",
		DisplayName:   "Kimi Coding",
		Description:   "Anthropic-compatible coding endpoint",
		API:           "anthropic-messages",
		BaseURL:       "https://api.kimi.com/coding/",
		DefaultModels: []string{"k2p5"},
		EnvVars:       []string{"KIMI_CODING_API_KEY", "KIMI_API_KEY"},
		APIKeyHint:    "Enter your Kimi Coding API key",
	},
	{
		ID:            "minimax",
		DisplayName:   "MiniMax",
		Description:   "MiniMax M2.5 via Anthropic-compatible API",
		API:           "anthropic-messages",
		BaseURL:       "https://api.minimax.io/anthropic",
		DefaultModels: []string{"MiniMax-M2.5", "MiniMax-M2.5-highspeed"},
		EnvVars:       []string{"MINIMAX_API_KEY"},
		APIKeyHint:    "Enter your MiniMax API key",
	},
	{
		ID:            "xiaomi",
		DisplayName:   "Xiaomi MiMo",
		Description:   "MiMo v2 Flash via Anthropic-compatible API",
		API:           "anthropic-messages",
		BaseURL:       "https://api.xiaomimimo.com/anthropic",
		DefaultModels: []string{"mimo-v2-flash"},
		EnvVars:       []string{"XIAOMI_API_KEY"},
		APIKeyHint:    "Enter your Xiaomi MiMo API key",
	},
	{
		ID:            "dashscope",
		DisplayName:   "DashScope / Qwen",
		Description:   "Qwen models through Alibaba Cloud Model Studio",
		API:           "openai-completions",
		BaseURL:       "https://dashscope.aliyuncs.com/compatible-mode/v1",
		DefaultModels: []string{"qwen-plus-latest", "qwen-max-latest", "qwen3-coder-plus"},
		EnvVars:       []string{"DASHSCOPE_API_KEY"},
		APIKeyHint:    "Enter your DashScope API key",
	},
	{
		ID:            "qianfan",
		DisplayName:   "Qianfan / ERNIE",
		Description:   "Baidu Qianfan unified API",
		API:           "openai-completions",
		BaseURL:       "https://qianfan.baidubce.com/v2",
		DefaultModels: []string{"ERNIE-5.0-Thinking-Preview", "deepseek-v3.2"},
		EnvVars:       []string{"QIANFAN_API_KEY"},
		APIKeyHint:    "Enter your Qianfan API key",
	},
	{
		ID:            "zai",
		DisplayName:   "Z.AI / GLM",
		Description:   "GLM models on the Z.AI platform",
		API:           "openai-completions",
		BaseURL:       "https://api.z.ai/v1",
		DefaultModels: []string{"glm-5", "glm-4.7", "glm-4.6"},
		EnvVars:       []string{"ZAI_API_KEY", "BIGMODEL_API_KEY"},
		APIKeyHint:    "Enter your Z.AI / GLM API key",
	},
	{
		ID:            "volcengine",
		DisplayName:   "Doubao / Volcengine",
		Description:   "Doubao and Ark models on Volcengine",
		API:           "openai-completions",
		BaseURL:       "https://ark.cn-beijing.volces.com/api/v3",
		DefaultModels: []string{"doubao-seed-1-8-251228", "ark-code-latest"},
		EnvVars:       []string{"VOLCENGINE_API_KEY", "ARK_API_KEY"},
		APIKeyHint:    "Enter your Volcengine / Doubao API key",
	},
	{
		ID:            "hunyuan",
		DisplayName:   "Tencent Hunyuan",
		Description:   "Tencent Hunyuan via Anthropic-compatible API",
		API:           "anthropic-messages",
		BaseURL:       "https://api.hunyuan.cloud.tencent.com/anthropic",
		DefaultModels: []string{"hunyuan-2.0-thinking-20251109", "hunyuan-2.0-instruct-20251111"},
		EnvVars:       []string{"HUNYUAN_API_KEY", "TENCENT_HUNYUAN_API_KEY"},
		APIKeyHint:    "Enter your Tencent Hunyuan API key",
	},
	{
		ID:            "siliconflow",
		DisplayName:   "SiliconFlow",
		Description:   "OpenAI-compatible gateway for Qwen, DeepSeek, GLM and more",
		API:           "openai-completions",
		BaseURL:       "https://api.siliconflow.cn/v1",
		DefaultModels: []string{"deepseek-ai/DeepSeek-V3", "Qwen/Qwen2.5-Coder-7B-Instruct"},
		EnvVars:       []string{"SILICONFLOW_API_KEY"},
		APIKeyHint:    "Enter your SiliconFlow API key",
	},
	{
		ID:            "amazon-bedrock",
		DisplayName:   "AWS Bedrock",
		Description:   "Claude, Titan, Llama, and other Bedrock-hosted models over the Converse API",
		API:           "bedrock-converse",
		DefaultModels: []string{"anthropic.claude-3-5-sonnet-20241022-v2:0"},
		EnvVars:       []string{"AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"},
	},
	{
		ID:            "ollama",
		DisplayName:   "Ollama",
		Description:   "Local models over the OpenAI-compatible Ollama endpoint",
		API:           "ollama",
		BaseURL:       "http://127.0.0.1:11434/v1",
		DefaultModels: []string{"llama3.3", "qwen2.5", "deepseek-r1", "mistral"},
	},
	{
		ID:          "custom",
		DisplayName: "Custom",
		Description: "Any OpenAI-compatible endpoint",
		API:         "openai-completions",
		APIKeyHint:  "Enter the API key for your endpoint",
	},
}

func SetupProviderProfiles() []SetupProviderProfile {
	out := make([]SetupProviderProfile, len(setupProviderProfiles))
	for i, profile := range setupProviderProfiles {
		out[i] = hydrateSetupProviderProfile(profile)
	}
	return out
}

func LookupSetupProviderProfile(provider string) (SetupProviderProfile, bool) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	for _, profile := range setupProviderProfiles {
		if profile.ID == provider {
			return hydrateSetupProviderProfile(profile), true
		}
	}
	return SetupProviderProfile{}, false
}

func DefaultProviderAPI(provider string) string {
	if profile, ok := LookupSetupProviderProfile(provider); ok {
		return profile.API
	}
	return ""
}

func DefaultModelForProvider(provider string) string {
	if profile, ok := LookupSetupProviderProfile(provider); ok && len(profile.DefaultModels) > 0 {
		return profile.DefaultModels[0]
	}
	return ""
}

func ProviderDisplayName(provider string) string {
	if profile, ok := LookupSetupProviderProfile(provider); ok && strings.TrimSpace(profile.DisplayName) != "" {
		return profile.DisplayName
	}
	return strings.TrimSpace(provider)
}

func ProviderAPIKeyHint(provider string) string {
	if profile, ok := LookupSetupProviderProfile(provider); ok && strings.TrimSpace(profile.APIKeyHint) != "" {
		return profile.APIKeyHint
	}
	return "Enter your API key"
}
