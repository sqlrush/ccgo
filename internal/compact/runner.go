package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type MessageClient interface {
	CreateMessage(context.Context, anthropic.Request) (*anthropic.Response, error)
}

type AutoConfig struct {
	Enabled                bool
	Force                  bool
	Window                 WindowConfig
	KeepLast               int
	MinMessages            int
	ExtraInstructions      string
	TokenUsage             int
	ConsecutiveFailures    int
	MaxConsecutiveFailures int
}

const DefaultMaxConsecutiveFailures = 3

type Runner struct {
	Client            MessageClient
	Model             string
	MaxTokens         int
	KeepLast          int
	ExtraInstructions string
}

type Result struct {
	Plan     Plan
	Response *anthropic.Response
	Request  anthropic.Request
	Usage    contracts.Usage
}

func ShouldRun(history []contracts.Message, config AutoConfig) bool {
	if !config.Enabled {
		return false
	}
	if !config.Force && FailureLimitReached(config) {
		return false
	}
	if config.MinMessages > 0 && len(history) < config.MinMessages {
		return false
	}
	if config.Force {
		return true
	}
	usage := config.TokenUsage
	if usage <= 0 {
		usage = EstimateTokens(history)
	}
	window := WindowConfigFromEnv(config.Window)
	window.AutoCompactEnabled = true
	return ShouldAutoCompact(usage, window)
}

func FailureLimit(config AutoConfig) int {
	if config.MaxConsecutiveFailures > 0 {
		return config.MaxConsecutiveFailures
	}
	return DefaultMaxConsecutiveFailures
}

func FailureLimitReached(config AutoConfig) bool {
	return config.ConsecutiveFailures >= FailureLimit(config)
}

func RecordFailure(config *AutoConfig) {
	if config != nil {
		config.ConsecutiveFailures++
	}
}

func RecordSuccess(config *AutoConfig) {
	if config != nil {
		config.ConsecutiveFailures = 0
	}
}

func (r Runner) Compact(ctx context.Context, history []contracts.Message, trigger Trigger, preTokens int, userContext string) (Result, error) {
	if r.Client == nil {
		return Result{}, fmt.Errorf("compact runner missing client")
	}
	request := r.BuildRequest(history)
	response, err := r.Client.CreateMessage(ctx, request)
	if err != nil {
		return Result{Request: request}, err
	}
	summary := strings.TrimSpace(responseText(response))
	plan := BuildPlan(history, PlanOptions{
		Trigger:        trigger,
		PreTokens:      preTokens,
		UserContext:    userContext,
		KeepLast:       r.KeepLast,
		Summary:        summary,
		PreserveRecent: true,
	})
	return Result{
		Plan:     plan,
		Response: response,
		Request:  request,
		Usage:    response.Usage,
	}, nil
}

func (r Runner) BuildRequest(history []contracts.Message) anthropic.Request {
	model := r.Model
	if model == "" {
		model = "sonnet"
	}
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = MaxOutputTokensForSummary
	}
	mode := PromptFull
	if r.KeepLast > 0 {
		mode = PromptPartial
	}
	messages := append([]contracts.Message(nil), history...)
	messages = append(messages, SummaryRequestMessage(mode, r.ExtraInstructions))
	return anthropic.Request{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs.NormalizeForAPI(messages),
	}
}

func responseText(response *anthropic.Response) string {
	if response == nil {
		return ""
	}
	var parts []string
	for _, block := range response.Content {
		if block.Type == contracts.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if summary, ok := providerWrappedSummaryText(text); ok {
		return summary
	}
	return text
}

func providerWrappedSummaryText(raw string) (string, bool) {
	payload, ok := providerWrappedSummaryPayload(raw)
	if !ok {
		return "", false
	}
	var result MicroResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return "", false
	}
	summary := strings.TrimSpace(result.Summary)
	return summary, summary != ""
}

func providerWrappedSummaryPayload(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if raw[0] == '{' || raw[0] == '[' {
		return raw, true
	}
	start := strings.Index(raw, "```")
	if start < 0 {
		return "", false
	}
	afterFence := raw[start+3:]
	lineEnd := strings.IndexAny(afterFence, "\r\n")
	if lineEnd < 0 {
		return "", false
	}
	content := strings.TrimLeft(afterFence[lineEnd:], "\r\n")
	end := strings.Index(content, "```")
	if end >= 0 {
		content = content[:end]
	}
	content = strings.TrimSpace(content)
	if content == "" || (content[0] != '{' && content[0] != '[') {
		return "", false
	}
	return content, true
}
