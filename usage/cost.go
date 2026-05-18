package usage

// ---------------------------------------------------------------------------
// Model pricing (USD per 1M tokens)
// ---------------------------------------------------------------------------

const (
	tokensPerUnit                       = 1_000_000 // pricing is per 1M tokens
	anthropicPromptCacheWriteMultiplier = 1.25
	anthropicPromptCacheReadMultiplier  = 0.10

	// OpenAI models
	gpt4oPromptCost          = 2.50  // USD per 1M prompt tokens
	gpt4oCompletionCost      = 10.00 // USD per 1M completion tokens
	gpt4oMiniPromptCost      = 0.15  // USD per 1M prompt tokens
	gpt4oMiniCompletionCost  = 0.60  // USD per 1M completion tokens
	gpt4TurboPromptCost      = 10.00
	gpt4TurboCompletionCost  = 30.00
	gpt35TurboPromptCost     = 0.50
	gpt35TurboCompletionCost = 1.50
	o1PromptCost             = 15.00
	o1CompletionCost         = 60.00
	o1MiniPromptCost         = 3.00
	o1MiniCompletionCost     = 12.00
	o3MiniPromptCost         = 1.10
	o3MiniCompletionCost     = 4.40

	// Anthropic models
	claudeSonnet4PromptCost      = 3.00
	claudeSonnet4CompletionCost  = 15.00
	claudeHaiku4_5PromptCost     = 0.80
	claudeHaiku4_5CompletionCost = 4.00
	claudeOpus4PromptCost        = 15.00
	claudeOpus4CompletionCost    = 75.00
)

// modelPricing maps model identifiers to their per-1M-token costs.
type modelPrice struct {
	promptCost     float64
	completionCost float64
}

var pricingTable = map[string]modelPrice{
	// OpenAI
	"gpt-4o":                 {gpt4oPromptCost, gpt4oCompletionCost},
	"gpt-4o-2024-05-13":      {gpt4oPromptCost, gpt4oCompletionCost},
	"gpt-4o-2024-08-06":      {gpt4oPromptCost, gpt4oCompletionCost},
	"gpt-4o-2024-11-20":      {gpt4oPromptCost, gpt4oCompletionCost},
	"gpt-4o-mini":            {gpt4oMiniPromptCost, gpt4oMiniCompletionCost},
	"gpt-4o-mini-2024-07-18": {gpt4oMiniPromptCost, gpt4oMiniCompletionCost},
	"gpt-4-turbo":            {gpt4TurboPromptCost, gpt4TurboCompletionCost},
	"gpt-4-turbo-preview":    {gpt4TurboPromptCost, gpt4TurboCompletionCost},
	"gpt-3.5-turbo":          {gpt35TurboPromptCost, gpt35TurboCompletionCost},
	"o1":                     {o1PromptCost, o1CompletionCost},
	"o1-preview":             {o1PromptCost, o1CompletionCost},
	"o1-mini":                {o1MiniPromptCost, o1MiniCompletionCost},
	"o3-mini":                {o3MiniPromptCost, o3MiniCompletionCost},

	// Anthropic
	"claude-sonnet-4-20250514":  {claudeSonnet4PromptCost, claudeSonnet4CompletionCost},
	"claude-haiku-4-5-20251001": {claudeHaiku4_5PromptCost, claudeHaiku4_5CompletionCost},
	"claude-opus-4-20250515":    {claudeOpus4PromptCost, claudeOpus4CompletionCost},
}

// EstimateCost returns the estimated USD cost for a model call.
// Returns 0 for unknown models.
func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	return EstimateCostWithCache(model, promptTokens, completionTokens, 0, 0)
}

// EstimateCostWithCache returns the estimated USD cost for a model call,
// including Anthropic prompt caching read/write adjustments when present.
// promptTokens should represent the total effective input tokens, including any
// cache creation or cache read usage.
func EstimateCostWithCache(model string, promptTokens, completionTokens, cacheCreationInputTokens, cacheReadInputTokens int) float64 {
	price, ok := pricingTable[model]
	if !ok {
		return 0
	}

	if cacheCreationInputTokens < 0 {
		cacheCreationInputTokens = 0
	}
	if cacheReadInputTokens < 0 {
		cacheReadInputTokens = 0
	}
	uncachedPromptTokens := promptTokens - cacheCreationInputTokens - cacheReadInputTokens
	if uncachedPromptTokens < 0 {
		uncachedPromptTokens = 0
	}

	promptCost := float64(uncachedPromptTokens) * price.promptCost / float64(tokensPerUnit)
	cacheCreationCost := float64(cacheCreationInputTokens) * price.promptCost * anthropicPromptCacheWriteMultiplier / float64(tokensPerUnit)
	cacheReadCost := float64(cacheReadInputTokens) * price.promptCost * anthropicPromptCacheReadMultiplier / float64(tokensPerUnit)
	completionCost := float64(completionTokens) * price.completionCost / float64(tokensPerUnit)
	return promptCost + cacheCreationCost + cacheReadCost + completionCost
}
