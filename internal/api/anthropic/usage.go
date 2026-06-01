package anthropic

import "ccgo/internal/contracts"

const MaxNonStreamingTokens = 64000

type ThinkingConfig map[string]any

func UpdateUsage(current contracts.Usage, part contracts.Usage) contracts.Usage {
	if part.InputTokens > 0 {
		current.InputTokens = part.InputTokens
	}
	if part.CacheCreationInputTokens > 0 {
		current.CacheCreationInputTokens = part.CacheCreationInputTokens
	}
	if part.CacheReadInputTokens > 0 {
		current.CacheReadInputTokens = part.CacheReadInputTokens
	}
	if part.OutputTokens != 0 {
		current.OutputTokens = part.OutputTokens
	}
	if part.CacheDeletedInputTokens > 0 {
		current.CacheDeletedInputTokens = part.CacheDeletedInputTokens
	}
	if part.ServerToolUse.WebSearchRequests != 0 {
		current.ServerToolUse.WebSearchRequests = part.ServerToolUse.WebSearchRequests
	}
	if part.ServerToolUse.WebFetchRequests != 0 {
		current.ServerToolUse.WebFetchRequests = part.ServerToolUse.WebFetchRequests
	}
	if part.ServiceTier != "" {
		current.ServiceTier = part.ServiceTier
	}
	if part.CacheCreation.Ephemeral1hInputTokens != 0 {
		current.CacheCreation.Ephemeral1hInputTokens = part.CacheCreation.Ephemeral1hInputTokens
	}
	if part.CacheCreation.Ephemeral5mInputTokens != 0 {
		current.CacheCreation.Ephemeral5mInputTokens = part.CacheCreation.Ephemeral5mInputTokens
	}
	if part.InferenceGeo != "" {
		current.InferenceGeo = part.InferenceGeo
	}
	if part.Iterations != 0 {
		current.Iterations = part.Iterations
	}
	if part.Speed != "" {
		current.Speed = part.Speed
	}
	return current
}

func AccumulateUsage(total contracts.Usage, message contracts.Usage) contracts.Usage {
	return contracts.Usage{
		InputTokens:              total.InputTokens + message.InputTokens,
		OutputTokens:             total.OutputTokens + message.OutputTokens,
		CacheCreationInputTokens: total.CacheCreationInputTokens + message.CacheCreationInputTokens,
		CacheReadInputTokens:     total.CacheReadInputTokens + message.CacheReadInputTokens,
		CacheDeletedInputTokens:  total.CacheDeletedInputTokens + message.CacheDeletedInputTokens,
		ServerToolUse: contracts.ToolUseUsage{
			WebSearchRequests: total.ServerToolUse.WebSearchRequests + message.ServerToolUse.WebSearchRequests,
			WebFetchRequests:  total.ServerToolUse.WebFetchRequests + message.ServerToolUse.WebFetchRequests,
		},
		ServiceTier: message.ServiceTier,
		CacheCreation: contracts.CacheCreationUsage{
			Ephemeral1hInputTokens: total.CacheCreation.Ephemeral1hInputTokens + message.CacheCreation.Ephemeral1hInputTokens,
			Ephemeral5mInputTokens: total.CacheCreation.Ephemeral5mInputTokens + message.CacheCreation.Ephemeral5mInputTokens,
		},
		InferenceGeo: message.InferenceGeo,
		Iterations:   message.Iterations,
		Speed:        message.Speed,
		CostUSD:      total.CostUSD + message.CostUSD,
	}
}

func AdjustRequestForNonStreaming(request Request, maxTokensCap int) Request {
	if maxTokensCap <= 0 {
		maxTokensCap = MaxNonStreamingTokens
	}
	if request.MaxTokens > maxTokensCap {
		request.MaxTokens = maxTokensCap
	}
	if request.Thinking != nil && request.Thinking["type"] == "enabled" {
		if budget, ok := intFromAny(request.Thinking["budget_tokens"]); ok && budget >= request.MaxTokens {
			next := map[string]any{}
			for k, v := range request.Thinking {
				next[k] = v
			}
			next["budget_tokens"] = request.MaxTokens - 1
			request.Thinking = next
		}
	}
	return request
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
