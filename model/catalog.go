package model

import "strings"

// CatalogEntry describes a built-in provider preset.
type CatalogEntry struct {
	Provider       ProviderEntry
	RequireBaseURL bool
}

// KnownProviders returns the built-in catalog of well-known model providers.
// Users only need to supply credentials for providers with stable base URLs.
// Gateway-style providers that require a tenant-specific endpoint are marked
// with RequireBaseURL so config validation can reject ambiguous setups.
func KnownProviders() map[string]CatalogEntry {
	return map[string]CatalogEntry{
		// ---------------------------------------------------------------------------
		// Anthropic-compatible APIs
		// ---------------------------------------------------------------------------
		"anthropic":             {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.anthropic.com", DefaultModel: "claude-sonnet-4-5-20241022"}},
		"minimax":               {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.minimax.io/anthropic", DefaultModel: "MiniMax-M2.5"}},
		"minimax-portal":        {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.minimax.io/anthropic", DefaultModel: "MiniMax-M2.5"}},
		"xiaomi":                {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.xiaomimimo.com/anthropic", DefaultModel: "mimo-v2-flash"}},
		"kimi-coding":           {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.kimi.com/coding/", DefaultModel: "k2p5"}},
		"hunyuan":               {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.hunyuan.cloud.tencent.com/anthropic", DefaultModel: "hunyuan-2.0-thinking-20251109"}},
		"synthetic":             {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://api.synthetic.new/anthropic", DefaultModel: "hf:MiniMaxAI/MiniMax-M2.5"}},
		"chutes":                {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://chutes.ai/anthropic"}},
		"cloudflare-ai-gateway": {Provider: ProviderEntry{API: APIAnthropicMessages, DefaultModel: "claude-sonnet-4-5"}, RequireBaseURL: true},
		"vercel-ai-gateway":     {Provider: ProviderEntry{API: APIAnthropicMessages, BaseURL: "https://ai-gateway.vercel.sh", DefaultModel: "anthropic/claude-opus-4.6"}},

		// ---------------------------------------------------------------------------
		// Google Generative AI
		// ---------------------------------------------------------------------------
		"google": {Provider: ProviderEntry{API: APIGoogleGenerativeAI, BaseURL: "https://generativelanguage.googleapis.com", DefaultModel: "gemini-2.0-flash"}},

		// ---------------------------------------------------------------------------
		// OpenAI-compatible APIs
		// ---------------------------------------------------------------------------
		"openai":      {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o"}},
		"deepseek":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.deepseek.com/v1", DefaultModel: "deepseek-chat"}},
		"moonshot":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.moonshot.ai/v1", DefaultModel: "kimi-k2.5"}},
		"mistral":     {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.mistral.ai/v1"}},
		"groq":        {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.groq.com/openai/v1", DefaultModel: "llama-3.3-70b-versatile"}},
		"cerebras":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.cerebras.ai/v1"}},
		"xai":         {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.x.ai/v1", DefaultModel: "grok-2"}},
		"together":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.together.xyz/v1", DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo"}},
		"openrouter":  {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "auto"}},
		"nvidia":      {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://integrate.api.nvidia.com/v1", DefaultModel: "nvidia/llama-3.1-nemotron-70b-instruct"}},
		"venice":      {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.venice.ai/api/v1", DefaultModel: "kimi-k2-5"}},
		"zai":         {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.z.ai/v1"}},
		"siliconflow": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.siliconflow.cn/v1", DefaultModel: "deepseek-ai/DeepSeek-V3"}},
		"deepgram":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.deepgram.com/v1"}},

		// Dynamic-discovery providers (list models from API).
		"huggingface": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://router.huggingface.co/v1"}},
		"kilocode":    {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://api.kilo.ai/api/gateway/", DefaultModel: "kilo/auto"}},

		// Gateway/proxy providers (user supplies their own base URL).
		"litellm":  {Provider: ProviderEntry{API: APIOpenAICompletions}, RequireBaseURL: true},
		"opencode": {Provider: ProviderEntry{API: APIOpenAICompletions}, RequireBaseURL: true},

		// ---------------------------------------------------------------------------
		// Chinese cloud providers
		// ---------------------------------------------------------------------------

		// Volcengine (Doubao / 火山引擎).
		"volcengine":      {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://ark.cn-beijing.volces.com/api/v3", DefaultModel: "doubao-seed-1-8-251228"}},
		"volcengine-plan": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://ark.cn-beijing.volces.com/api/coding/v3", DefaultModel: "ark-code-latest"}},

		// BytePlus (Southeast Asia variant of Volcengine).
		"byteplus":      {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://ark.ap-southeast.bytepluses.com/api/v3", DefaultModel: "seed-1-8-251228"}},
		"byteplus-plan": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://ark.ap-southeast.bytepluses.com/api/coding/v3", DefaultModel: "ark-code-latest"}},

		// Baidu Qianfan (百度千帆).
		"qianfan": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://qianfan.baidubce.com/v2", DefaultModel: "deepseek-v3.2"}},

		// Alibaba Qwen Portal (通义千问).
		"qwen-portal": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://portal.qwen.ai/v1", DefaultModel: "qwen3.5-plus"}},

		// Alibaba ModelStudio / DashScope (阿里云百炼).
		"dashscope":   {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-plus-latest"}},
		"modelstudio": {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-plus-latest"}},

		// ---------------------------------------------------------------------------
		// AWS Bedrock
		// ---------------------------------------------------------------------------
		"amazon-bedrock": {Provider: ProviderEntry{API: APIBedrockConverse}},

		// ---------------------------------------------------------------------------
		// GitHub Copilot
		// ---------------------------------------------------------------------------
		"github-copilot": {Provider: ProviderEntry{API: APIGitHubCopilot}},

		// ---------------------------------------------------------------------------
		// Local runtimes
		// ---------------------------------------------------------------------------
		"ollama": {Provider: ProviderEntry{API: APIOllama, BaseURL: "http://127.0.0.1:11434/v1"}},
		"vllm":   {Provider: ProviderEntry{API: APIOpenAICompletions, BaseURL: "http://127.0.0.1:8000/v1"}},
	}
}

// CatalogLookup returns the built-in provider entry and whether it exists.
func CatalogLookup(name string) (CatalogEntry, bool) {
	entry, ok := KnownProviders()[strings.TrimSpace(name)]
	return entry, ok
}

// MergeWithCatalog fills in missing fields from the known provider catalog.
// Explicit config values always take precedence over catalog defaults.
func MergeWithCatalog(providers map[string]ProviderEntry) map[string]ProviderEntry {
	merged := make(map[string]ProviderEntry, len(providers))
	for name, entry := range providers {
		if defaults, ok := CatalogLookup(name); ok {
			catalog := defaults.Provider
			if entry.API == "" {
				entry.API = catalog.API
			}
			if entry.BaseURL == "" {
				entry.BaseURL = catalog.BaseURL
			}
			if entry.Region == "" {
				entry.Region = catalog.Region
			}
			if entry.DefaultModel == "" {
				entry.DefaultModel = catalog.DefaultModel
			}
		}
		merged[name] = entry
	}
	return merged
}
