package anthropic

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultMaxRetries = 10
const BaseDelay = 500 * time.Millisecond
const FloorOutputTokens = 3000

type RetryConfig struct {
	MaxRetries     int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	JitterFraction float64
	Sleep          func(context.Context, time.Duration) error
}

type ContextOverflow struct {
	InputTokens  int
	MaxTokens    int
	ContextLimit int
}

var contextOverflowPattern = regexp.MustCompile("input length and `max_tokens` exceed context limit: (\\d+) \\+ (\\d+) > (\\d+)")

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     DefaultMaxRetries,
		BaseDelay:      BaseDelay,
		MaxDelay:       32 * time.Second,
		JitterFraction: 0.25,
	}
}

func RetryDelay(attempt int, retryAfterHeader string, config RetryConfig) time.Duration {
	if retryAfterHeader != "" {
		seconds, err := strconv.Atoi(strings.TrimSpace(retryAfterHeader))
		if err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = BaseDelay
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 32 * time.Second
	}
	delay := config.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= config.MaxDelay {
			delay = config.MaxDelay
			break
		}
	}
	if config.JitterFraction > 0 {
		jitter := time.Duration(rand.Float64() * config.JitterFraction * float64(delay))
		delay += jitter
	}
	return delay
}

func ShouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return ShouldRetryAPIError(apiErr)
}

func ShouldRetryAPIError(err APIError) bool {
	if IsOverloadedError(err) {
		return true
	}
	if _, ok := ParseMaxTokensContextOverflowError(err); ok {
		return true
	}
	shouldRetry := strings.ToLower(err.Header.Get("x-should-retry"))
	if shouldRetry == "true" {
		return true
	}
	if shouldRetry == "false" {
		if !(strings.EqualFold(strings.TrimSpace(os.Getenv("USER_TYPE")), "ant") && err.StatusCode >= 500) {
			return false
		}
	}
	switch err.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests, http.StatusUnauthorized:
		return true
	default:
		return err.StatusCode >= 500
	}
}

func IsOverloadedError(err APIError) bool {
	return err.StatusCode == 529 || strings.Contains(err.Message, `"type":"overloaded_error"`) || err.Type == "overloaded_error"
}

func ParseMaxTokensContextOverflowError(err APIError) (ContextOverflow, bool) {
	if err.StatusCode != http.StatusBadRequest || !strings.Contains(err.Message, "input length and `max_tokens` exceed context limit") {
		return ContextOverflow{}, false
	}
	match := contextOverflowPattern.FindStringSubmatch(err.Message)
	if len(match) != 4 {
		return ContextOverflow{}, false
	}
	inputTokens, inputErr := strconv.Atoi(match[1])
	maxTokens, maxErr := strconv.Atoi(match[2])
	contextLimit, contextErr := strconv.Atoi(match[3])
	if inputErr != nil || maxErr != nil || contextErr != nil {
		return ContextOverflow{}, false
	}
	return ContextOverflow{InputTokens: inputTokens, MaxTokens: maxTokens, ContextLimit: contextLimit}, true
}

func AdjustMaxTokensForContextOverflow(info ContextOverflow, thinkingBudget int) (int, bool) {
	const safetyBuffer = 1000
	availableContext := info.ContextLimit - info.InputTokens - safetyBuffer
	if availableContext < 0 {
		availableContext = 0
	}
	if availableContext < FloorOutputTokens {
		return 0, false
	}
	minRequired := thinkingBudget + 1
	adjusted := availableContext
	if adjusted < FloorOutputTokens {
		adjusted = FloorOutputTokens
	}
	if adjusted < minRequired {
		adjusted = minRequired
	}
	return adjusted, true
}

func sleepWithConfig(ctx context.Context, delay time.Duration, config RetryConfig) error {
	if delay <= 0 {
		return nil
	}
	if config.Sleep != nil {
		return config.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
