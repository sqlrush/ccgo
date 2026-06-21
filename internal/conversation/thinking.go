package conversation

import "ccgo/internal/model"

// thinkingRequestConfig returns the Anthropic `thinking` request parameter for a
// model that supports extended thinking, or nil when thinking should not be set.
// Shape matches the API: {"type":"enabled","budget_tokens":N}.
//
// Default-off behavior: returns nil when budgetTokens <= 0 (unless the model
// forces AlwaysOnThinking), or when the model does not support thinking at all.
//
// The Anthropic API requires budget_tokens < max_tokens; budgetTokens is clamped
// to maxTokens-1 when it would otherwise meet or exceed maxTokens (mirroring
// AdjustRequestForNonStreaming in usage.go).
func thinkingRequestConfig(capability model.Capability, budgetTokens int, maxTokens int) map[string]any {
	if budgetTokens <= 0 && !capability.AlwaysOnThinking {
		return nil
	}
	if !capability.SupportsThinking && !capability.AlwaysOnThinking {
		return nil
	}
	if budgetTokens <= 0 {
		budgetTokens = defaultThinkingBudgetTokens
	}
	// Clamp budget_tokens to maxTokens-1 to satisfy the API constraint
	// budget_tokens < max_tokens (mirrors usage.go:AdjustRequestForNonStreaming).
	if maxTokens > 0 && budgetTokens >= maxTokens {
		budgetTokens = maxTokens - 1
	}
	return map[string]any{
		"type":          "enabled",
		"budget_tokens": budgetTokens,
	}
}

const defaultThinkingBudgetTokens = 4_096
