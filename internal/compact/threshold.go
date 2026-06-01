package compact

import (
	"math"
	"os"
	"strconv"
)

const (
	MaxOutputTokensForSummary    = 20_000
	AutoCompactBufferTokens      = 13_000
	WarningThresholdBufferTokens = 20_000
	ErrorThresholdBufferTokens   = 20_000
	ManualCompactBufferTokens    = 3_000
)

type WindowConfig struct {
	ContextWindow       int
	MaxOutputTokens     int
	AutoCompactEnabled  bool
	AutoCompactOverride *float64
	BlockingLimit       int
}

type WarningState struct {
	PercentLeft                 int
	IsAboveWarningThreshold     bool
	IsAboveErrorThreshold       bool
	IsAboveAutoCompactThreshold bool
	IsAtBlockingLimit           bool
}

func EffectiveContextWindow(config WindowConfig) int {
	contextWindow := config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 200_000
	}
	outputTokens := config.MaxOutputTokens
	if outputTokens <= 0 || outputTokens > MaxOutputTokensForSummary {
		outputTokens = MaxOutputTokensForSummary
	}
	return max(0, contextWindow-outputTokens)
}

func AutoCompactThreshold(config WindowConfig) int {
	effective := EffectiveContextWindow(config)
	threshold := effective - AutoCompactBufferTokens
	if override := config.AutoCompactOverride; override != nil && *override > 0 && *override <= 100 {
		byPercent := int(math.Floor(float64(effective) * (*override / 100)))
		if byPercent < threshold {
			threshold = byPercent
		}
	}
	return max(0, threshold)
}

func CalculateWarningState(tokenUsage int, config WindowConfig) WarningState {
	autoThreshold := AutoCompactThreshold(config)
	threshold := EffectiveContextWindow(config)
	if config.AutoCompactEnabled {
		threshold = autoThreshold
	}
	if threshold <= 0 {
		return WarningState{PercentLeft: 0, IsAboveWarningThreshold: true, IsAboveErrorThreshold: true, IsAboveAutoCompactThreshold: config.AutoCompactEnabled, IsAtBlockingLimit: true}
	}
	percentLeft := int(math.Round(float64(threshold-tokenUsage) / float64(threshold) * 100))
	if percentLeft < 0 {
		percentLeft = 0
	}
	warningThreshold := threshold - WarningThresholdBufferTokens
	errorThreshold := threshold - ErrorThresholdBufferTokens
	blockingLimit := config.BlockingLimit
	if blockingLimit <= 0 {
		blockingLimit = EffectiveContextWindow(config) - ManualCompactBufferTokens
	}
	return WarningState{
		PercentLeft:                 percentLeft,
		IsAboveWarningThreshold:     tokenUsage >= warningThreshold,
		IsAboveErrorThreshold:       tokenUsage >= errorThreshold,
		IsAboveAutoCompactThreshold: config.AutoCompactEnabled && tokenUsage >= autoThreshold,
		IsAtBlockingLimit:           tokenUsage >= blockingLimit,
	}
}

func ShouldAutoCompact(tokenUsage int, config WindowConfig) bool {
	if os.Getenv("DISABLE_COMPACT") != "" || os.Getenv("DISABLE_AUTO_COMPACT") != "" {
		return false
	}
	if !config.AutoCompactEnabled {
		return false
	}
	return tokenUsage >= AutoCompactThreshold(config)
}

func WindowConfigFromEnv(base WindowConfig) WindowConfig {
	if raw := os.Getenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 && parsed <= 100 {
			base.AutoCompactOverride = &parsed
		}
	}
	if raw := os.Getenv("CLAUDE_CODE_BLOCKING_LIMIT_OVERRIDE"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			base.BlockingLimit = parsed
		}
	}
	return base
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
