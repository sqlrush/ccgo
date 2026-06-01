package anthropic

import (
	"fmt"
	"math"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/model"
)

type ModelCosts struct {
	InputTokens            float64
	OutputTokens           float64
	PromptCacheWriteTokens float64
	PromptCacheReadTokens  float64
	WebSearchRequests      float64
}

var (
	CostTier3_15   = ModelCosts{InputTokens: 3, OutputTokens: 15, PromptCacheWriteTokens: 3.75, PromptCacheReadTokens: 0.3, WebSearchRequests: 0.01}
	CostTier15_75  = ModelCosts{InputTokens: 15, OutputTokens: 75, PromptCacheWriteTokens: 18.75, PromptCacheReadTokens: 1.5, WebSearchRequests: 0.01}
	CostTier5_25   = ModelCosts{InputTokens: 5, OutputTokens: 25, PromptCacheWriteTokens: 6.25, PromptCacheReadTokens: 0.5, WebSearchRequests: 0.01}
	CostTier30_150 = ModelCosts{InputTokens: 30, OutputTokens: 150, PromptCacheWriteTokens: 37.5, PromptCacheReadTokens: 3, WebSearchRequests: 0.01}
	CostHaiku35    = ModelCosts{InputTokens: 0.8, OutputTokens: 4, PromptCacheWriteTokens: 1, PromptCacheReadTokens: 0.08, WebSearchRequests: 0.01}
	CostHaiku45    = ModelCosts{InputTokens: 1, OutputTokens: 5, PromptCacheWriteTokens: 1.25, PromptCacheReadTokens: 0.1, WebSearchRequests: 0.01}
)

var modelCosts = map[string]ModelCosts{
	"claude-3-5-haiku":  CostHaiku35,
	"claude-haiku-4-5":  CostHaiku45,
	"claude-3-5-sonnet": CostTier3_15,
	"claude-3-7-sonnet": CostTier3_15,
	"claude-sonnet-4":   CostTier3_15,
	"claude-sonnet-4-5": CostTier3_15,
	"claude-sonnet-4-6": CostTier3_15,
	"claude-opus-4":     CostTier15_75,
	"claude-opus-4-1":   CostTier15_75,
	"claude-opus-4-5":   CostTier5_25,
	"claude-opus-4-6":   CostTier5_25,
}

func CostsForModel(name string, usage contracts.Usage) (ModelCosts, bool) {
	canonical := model.CanonicalName(strings.TrimSuffix(strings.TrimSpace(name), "[1m]"))
	if canonical == "claude-opus-4-6" && usage.Speed == "fast" {
		return CostTier30_150, true
	}
	costs, ok := modelCosts[canonical]
	if ok {
		return costs, true
	}
	return CostTier5_25, false
}

func CalculateUSDCost(resolvedModel string, usage contracts.Usage) float64 {
	costs, _ := CostsForModel(resolvedModel, usage)
	return TokensToUSDCost(costs, usage)
}

func TokensToUSDCost(costs ModelCosts, usage contracts.Usage) float64 {
	return (float64(usage.InputTokens)/1_000_000)*costs.InputTokens +
		(float64(usage.OutputTokens)/1_000_000)*costs.OutputTokens +
		(float64(usage.CacheReadInputTokens)/1_000_000)*costs.PromptCacheReadTokens +
		(float64(usage.CacheCreationInputTokens)/1_000_000)*costs.PromptCacheWriteTokens +
		float64(usage.ServerToolUse.WebSearchRequests)*costs.WebSearchRequests
}

func CalculateCostFromTokens(resolvedModel string, inputTokens, outputTokens, cacheReadInputTokens, cacheCreationInputTokens int) float64 {
	return CalculateUSDCost(resolvedModel, contracts.Usage{
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     cacheReadInputTokens,
		CacheCreationInputTokens: cacheCreationInputTokens,
	})
}

func UsageWithCost(resolvedModel string, usage contracts.Usage) contracts.Usage {
	usage.CostUSD = CalculateUSDCost(resolvedModel, usage)
	return usage
}

func FormatModelPricing(costs ModelCosts) string {
	return fmt.Sprintf("%s/%s per Mtok", formatPrice(costs.InputTokens), formatPrice(costs.OutputTokens))
}

func formatPrice(price float64) string {
	if math.Mod(price, 1) == 0 {
		return fmt.Sprintf("$%.0f", price)
	}
	return fmt.Sprintf("$%.2f", price)
}
