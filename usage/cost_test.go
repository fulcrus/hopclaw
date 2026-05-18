package usage

import (
	"math"
	"testing"
)

func TestEstimateCostKnownModel(t *testing.T) {
	t.Parallel()

	cost := EstimateCost("gpt-4o", 1_000_000, 0)
	if !floatClose(cost, gpt4oPromptCost, 0.01) {
		t.Fatalf("EstimateCost prompt only = %f, want ~%f", cost, gpt4oPromptCost)
	}

	cost = EstimateCost("gpt-4o", 0, 1_000_000)
	if !floatClose(cost, gpt4oCompletionCost, 0.01) {
		t.Fatalf("EstimateCost completion only = %f, want ~%f", cost, gpt4oCompletionCost)
	}
}

func TestEstimateCostCombined(t *testing.T) {
	t.Parallel()

	promptTokens := 500_000
	completionTokens := 200_000

	cost := EstimateCost("gpt-4o", promptTokens, completionTokens)
	expectedPrompt := float64(promptTokens) * gpt4oPromptCost / float64(tokensPerUnit)
	expectedCompletion := float64(completionTokens) * gpt4oCompletionCost / float64(tokensPerUnit)
	expected := expectedPrompt + expectedCompletion

	if !floatClose(cost, expected, 0.001) {
		t.Fatalf("EstimateCost = %f, want ~%f", cost, expected)
	}
}

func TestEstimateCostUnknownModel(t *testing.T) {
	t.Parallel()

	cost := EstimateCost("unknown-model-xyz", 1000, 1000)
	if cost != 0 {
		t.Fatalf("EstimateCost(unknown) = %f, want 0", cost)
	}
}

func TestEstimateCostZeroTokens(t *testing.T) {
	t.Parallel()

	cost := EstimateCost("gpt-4o", 0, 0)
	if cost != 0 {
		t.Fatalf("EstimateCost(0, 0) = %f, want 0", cost)
	}
}

func TestEstimateCostAnthropic(t *testing.T) {
	t.Parallel()

	cost := EstimateCost("claude-sonnet-4-20250514", 1_000_000, 0)
	if !floatClose(cost, claudeSonnet4PromptCost, 0.01) {
		t.Fatalf("EstimateCost claude sonnet prompt = %f, want ~%f", cost, claudeSonnet4PromptCost)
	}

	cost = EstimateCost("claude-opus-4-20250515", 0, 1_000_000)
	if !floatClose(cost, claudeOpus4CompletionCost, 0.01) {
		t.Fatalf("EstimateCost claude opus completion = %f, want ~%f", cost, claudeOpus4CompletionCost)
	}
}

func TestEstimateCostWithCacheAnthropic(t *testing.T) {
	t.Parallel()

	const (
		totalPromptTokens    = 10_000
		completionTokens     = 2_000
		cacheCreationTokens  = 3_000
		cacheReadTokens      = 4_000
		uncachedPromptTokens = totalPromptTokens - cacheCreationTokens - cacheReadTokens
	)

	cost := EstimateCostWithCache(
		"claude-sonnet-4-20250514",
		totalPromptTokens,
		completionTokens,
		cacheCreationTokens,
		cacheReadTokens,
	)
	expected := (float64(uncachedPromptTokens) * claudeSonnet4PromptCost / float64(tokensPerUnit)) +
		(float64(cacheCreationTokens) * claudeSonnet4PromptCost * anthropicPromptCacheWriteMultiplier / float64(tokensPerUnit)) +
		(float64(cacheReadTokens) * claudeSonnet4PromptCost * anthropicPromptCacheReadMultiplier / float64(tokensPerUnit)) +
		(float64(completionTokens) * claudeSonnet4CompletionCost / float64(tokensPerUnit))

	if !floatClose(cost, expected, 0.000001) {
		t.Fatalf("EstimateCostWithCache = %f, want %f", cost, expected)
	}
}

func TestEstimateCostWithCacheUnknownModel(t *testing.T) {
	t.Parallel()

	if cost := EstimateCostWithCache("unknown-model-xyz", 1000, 100, 400, 500); cost != 0 {
		t.Fatalf("EstimateCostWithCache(unknown) = %f, want 0", cost)
	}
}

func TestEstimateCostGPTMiniModel(t *testing.T) {
	t.Parallel()

	cost := EstimateCost("gpt-4o-mini", 1_000_000, 1_000_000)
	expected := gpt4oMiniPromptCost + gpt4oMiniCompletionCost
	if !floatClose(cost, expected, 0.01) {
		t.Fatalf("EstimateCost gpt-4o-mini = %f, want ~%f", cost, expected)
	}
}

func floatClose(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}
