package model

// ModelCapability represents a feature supported by a model.
type ModelCapability string

const (
	CapVision     ModelCapability = "vision"
	CapReasoning  ModelCapability = "reasoning"
	CapToolUse    ModelCapability = "tool_use"
	CapStreaming  ModelCapability = "streaming"
	CapJSONMode   ModelCapability = "json_mode"
	CapEmbeddings ModelCapability = "embeddings"
)

// ModelMeta holds static metadata for a known model.
type ModelMeta struct {
	Provider      string            // e.g., "openai", "anthropic"
	Model         string            // canonical model identifier
	DisplayName   string            // human-readable name
	ContextWindow int               // max input tokens
	MaxOutput     int               // max output tokens
	Capabilities  []ModelCapability // supported features
}

// ---------------------------------------------------------------------------
// Capability constants — context windows and output limits
// ---------------------------------------------------------------------------

const (
	ctx128k = 128_000
	ctx192k = 192_000
	ctx200k = 200_000
	ctx256k = 256_000
	ctx262k = 262_144
	ctx1M   = 1_000_000
	out8k   = 8_192
	out16k  = 16_384
	out32k  = 32_768
	out65k  = 65_536
	out100k = 100_000
	out128k = 128_000
)

// Common capability sets to avoid repetition.
var (
	capsVisionToolStreaming          = []ModelCapability{CapVision, CapToolUse, CapStreaming}
	capsVisionReasoningToolStreaming = []ModelCapability{CapVision, CapReasoning, CapToolUse, CapStreaming}
	capsReasoningToolStreaming       = []ModelCapability{CapReasoning, CapToolUse, CapStreaming}
	capsToolStreaming                = []ModelCapability{CapToolUse, CapStreaming}
	capsReasoningStreaming           = []ModelCapability{CapReasoning, CapStreaming}
	capsStreamingOnly                = []ModelCapability{CapStreaming}
)

// knownModels is the static registry of all well-known models.
// Initialized once and returned by KnownModels via a defensive copy.
var knownModels = []ModelMeta{
	// ---------------------------------------------------------------------------
	// Anthropic
	// ---------------------------------------------------------------------------
	{Provider: "anthropic", Model: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", ContextWindow: ctx200k, MaxOutput: out32k, Capabilities: capsVisionReasoningToolStreaming},
	{Provider: "anthropic", Model: "claude-sonnet-4-5-20241022", DisplayName: "Claude Sonnet 4.5", ContextWindow: ctx200k, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},
	{Provider: "anthropic", Model: "claude-haiku-3-5-20241022", DisplayName: "Claude Haiku 3.5", ContextWindow: ctx200k, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},

	// ---------------------------------------------------------------------------
	// OpenAI
	// ---------------------------------------------------------------------------
	{Provider: "openai", Model: "gpt-4o", DisplayName: "GPT-4o", ContextWindow: ctx128k, MaxOutput: out16k, Capabilities: capsVisionToolStreaming},
	{Provider: "openai", Model: "gpt-4o-mini", DisplayName: "GPT-4o Mini", ContextWindow: ctx128k, MaxOutput: out16k, Capabilities: capsVisionToolStreaming},
	{Provider: "openai", Model: "o1-preview", DisplayName: "o1 Preview", ContextWindow: ctx200k, MaxOutput: out100k, Capabilities: capsReasoningToolStreaming},
	{Provider: "openai", Model: "o1-mini", DisplayName: "o1 Mini", ContextWindow: ctx200k, MaxOutput: out65k, Capabilities: capsReasoningToolStreaming},
	{Provider: "openai", Model: "o3-mini", DisplayName: "o3 Mini", ContextWindow: ctx200k, MaxOutput: out100k, Capabilities: capsReasoningToolStreaming},

	// ---------------------------------------------------------------------------
	// Google Gemini
	// ---------------------------------------------------------------------------
	{Provider: "google", Model: "gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash", ContextWindow: ctx1M, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},
	{Provider: "google", Model: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: ctx1M, MaxOutput: out65k, Capabilities: capsVisionReasoningToolStreaming},
	{Provider: "google", Model: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: ctx1M, MaxOutput: out65k, Capabilities: capsVisionToolStreaming},

	// ---------------------------------------------------------------------------
	// DeepSeek
	// ---------------------------------------------------------------------------
	{Provider: "deepseek", Model: "deepseek-chat", DisplayName: "DeepSeek Chat", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},
	{Provider: "deepseek", Model: "deepseek-reasoner", DisplayName: "DeepSeek Reasoner", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsReasoningStreaming},

	// ---------------------------------------------------------------------------
	// Moonshot / Kimi
	// ---------------------------------------------------------------------------
	{Provider: "moonshot", Model: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: ctx256k, MaxOutput: out8k, Capabilities: capsToolStreaming},
	{Provider: "kimi-coding", Model: "k2p5", DisplayName: "Kimi K2P5", ContextWindow: ctx262k, MaxOutput: out32k, Capabilities: capsReasoningToolStreaming},

	// ---------------------------------------------------------------------------
	// MiniMax
	// ---------------------------------------------------------------------------
	{Provider: "minimax", Model: "MiniMax-M2.5", DisplayName: "MiniMax M2.5", ContextWindow: ctx200k, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},
	{Provider: "minimax", Model: "MiniMax-M2.5-highspeed", DisplayName: "MiniMax M2.5 Highspeed", ContextWindow: ctx200k, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},

	// ---------------------------------------------------------------------------
	// Xiaomi
	// ---------------------------------------------------------------------------
	{Provider: "xiaomi", Model: "mimo-v2-flash", DisplayName: "MiMo v2 Flash", ContextWindow: ctx262k, MaxOutput: out8k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Groq
	// ---------------------------------------------------------------------------
	{Provider: "groq", Model: "llama-3.3-70b-versatile", DisplayName: "Llama 3.3 70B Versatile", ContextWindow: ctx128k, MaxOutput: out32k, Capabilities: capsToolStreaming},
	{Provider: "groq", Model: "llama-3.1-8b-instant", DisplayName: "Llama 3.1 8B Instant", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsStreamingOnly},

	// ---------------------------------------------------------------------------
	// xAI
	// ---------------------------------------------------------------------------
	{Provider: "xai", Model: "grok-2", DisplayName: "Grok 2", ContextWindow: ctx128k, MaxOutput: out32k, Capabilities: capsVisionToolStreaming},
	{Provider: "xai", Model: "grok-3", DisplayName: "Grok 3", ContextWindow: ctx128k, MaxOutput: out32k, Capabilities: capsVisionReasoningToolStreaming},

	// ---------------------------------------------------------------------------
	// Together
	// ---------------------------------------------------------------------------
	{Provider: "together", Model: "meta-llama/Llama-3.3-70B-Instruct-Turbo", DisplayName: "Llama 3.3 70B Instruct Turbo", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Qianfan (Baidu)
	// ---------------------------------------------------------------------------
	{Provider: "qianfan", Model: "deepseek-v3.2", DisplayName: "DeepSeek V3.2 (Qianfan)", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},
	{Provider: "qianfan", Model: "ERNIE-5.0-Thinking-Preview", DisplayName: "ERNIE 5.0 Thinking Preview", ContextWindow: ctx128k, MaxOutput: out16k, Capabilities: capsReasoningStreaming},

	// ---------------------------------------------------------------------------
	// DashScope / ModelStudio (Alibaba)
	// ---------------------------------------------------------------------------
	{Provider: "dashscope", Model: "qwen-plus-latest", DisplayName: "Qwen Plus Latest", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "dashscope", Model: "qwen-max-latest", DisplayName: "Qwen Max Latest", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsReasoningToolStreaming},
	{Provider: "dashscope", Model: "qwen3-coder-plus", DisplayName: "Qwen 3 Coder Plus", ContextWindow: ctx256k, MaxOutput: out32k, Capabilities: capsToolStreaming},
	{Provider: "modelstudio", Model: "qwen-plus-latest", DisplayName: "Qwen Plus Latest", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "modelstudio", Model: "qwen-max-latest", DisplayName: "Qwen Max Latest", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsReasoningToolStreaming},
	{Provider: "modelstudio", Model: "qwen3-coder-plus", DisplayName: "Qwen 3 Coder Plus", ContextWindow: ctx256k, MaxOutput: out32k, Capabilities: capsToolStreaming},
	{Provider: "qwen-portal", Model: "qwen3.5-plus", DisplayName: "Qwen 3.5 Plus", ContextWindow: ctx128k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "qwen-portal", Model: "qwen3-coder", DisplayName: "Qwen 3 Coder", ContextWindow: ctx128k, MaxOutput: out32k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Volcengine / BytePlus
	// ---------------------------------------------------------------------------
	{Provider: "volcengine", Model: "doubao-seed-1-8-251228", DisplayName: "Doubao Seed 1.8", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "volcengine", Model: "ark-code-latest", DisplayName: "Ark Code", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "byteplus", Model: "seed-1-8-251228", DisplayName: "Seed 1.8", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},
	{Provider: "byteplus", Model: "ark-code-latest", DisplayName: "Ark Code", ContextWindow: ctx256k, MaxOutput: out16k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Z.AI / GLM
	// ---------------------------------------------------------------------------
	{Provider: "zai", Model: "glm-5", DisplayName: "GLM 5", ContextWindow: ctx200k, MaxOutput: out128k, Capabilities: capsReasoningToolStreaming},
	{Provider: "zai", Model: "glm-4.7", DisplayName: "GLM 4.7", ContextWindow: ctx128k, MaxOutput: out128k, Capabilities: capsReasoningToolStreaming},
	{Provider: "zai", Model: "glm-4.6", DisplayName: "GLM 4.6", ContextWindow: ctx128k, MaxOutput: out100k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Tencent Hunyuan
	// ---------------------------------------------------------------------------
	{Provider: "hunyuan", Model: "hunyuan-2.0-thinking-20251109", DisplayName: "Hunyuan 2.0 Thinking", ContextWindow: ctx256k, MaxOutput: out32k, Capabilities: capsReasoningToolStreaming},
	{Provider: "hunyuan", Model: "hunyuan-2.0-instruct-20251111", DisplayName: "Hunyuan 2.0 Instruct", ContextWindow: ctx256k, MaxOutput: out32k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// SiliconFlow
	// ---------------------------------------------------------------------------
	{Provider: "siliconflow", Model: "deepseek-ai/DeepSeek-V3", DisplayName: "DeepSeek V3 (SiliconFlow)", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},
	{Provider: "siliconflow", Model: "Qwen/Qwen2.5-Coder-7B-Instruct", DisplayName: "Qwen 2.5 Coder 7B (SiliconFlow)", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Synthetic
	// ---------------------------------------------------------------------------
	{Provider: "synthetic", Model: "hf:MiniMaxAI/MiniMax-M2.5", DisplayName: "MiniMax M2.5 (Synthetic)", ContextWindow: ctx192k, MaxOutput: out8k, Capabilities: capsVisionToolStreaming},

	// ---------------------------------------------------------------------------
	// Venice
	// ---------------------------------------------------------------------------
	{Provider: "venice", Model: "kimi-k2-5", DisplayName: "Kimi K2.5 (Venice)", ContextWindow: ctx256k, MaxOutput: out8k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// OpenRouter
	// ---------------------------------------------------------------------------
	{Provider: "openrouter", Model: "auto", DisplayName: "OpenRouter Auto", ContextWindow: ctx128k, MaxOutput: out8k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Kilocode
	// ---------------------------------------------------------------------------
	{Provider: "kilocode", Model: "kilo/auto", DisplayName: "Kilocode Auto", ContextWindow: ctx1M, MaxOutput: out128k, Capabilities: capsToolStreaming},

	// ---------------------------------------------------------------------------
	// Vercel AI Gateway
	// ---------------------------------------------------------------------------
	{Provider: "vercel-ai-gateway", Model: "anthropic/claude-opus-4.6", DisplayName: "Claude Opus 4.6 (Vercel)", ContextWindow: ctx200k, MaxOutput: out32k, Capabilities: capsVisionReasoningToolStreaming},
}

// KnownModels returns metadata for all well-known models across all providers.
// The returned slice is a defensive copy; callers may modify it freely.
func KnownModels() []ModelMeta {
	out := make([]ModelMeta, len(knownModels))
	copy(out, knownModels)
	return out
}

// LookupModel returns metadata for a specific provider/model pair.
// Returns false if the combination is not found in the known models list.
func LookupModel(provider, model string) (ModelMeta, bool) {
	for _, m := range knownModels {
		if m.Provider == provider && m.Model == model {
			return m, true
		}
	}
	return ModelMeta{}, false
}

// ModelsForProvider returns all known models for the given provider.
// Returns nil if no models are registered for the provider.
func ModelsForProvider(provider string) []ModelMeta {
	var out []ModelMeta
	for _, m := range knownModels {
		if m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// HasCapability reports whether the given model metadata includes the
// specified capability.
func HasCapability(meta ModelMeta, cap ModelCapability) bool {
	for _, c := range meta.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
