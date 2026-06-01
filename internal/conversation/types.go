package conversation

import (
	"context"

	"ccgo/internal/api/anthropic"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

type MessageClient interface {
	CreateMessage(context.Context, anthropic.Request) (*anthropic.Response, error)
}

type StreamingMessageClient interface {
	StreamMessages(context.Context, anthropic.Request, func(anthropic.StreamEvent) error) error
}

type EventType string

const (
	EventUserMessage      EventType = "user_message"
	EventAssistantMessage EventType = "assistant_message"
	EventToolUse          EventType = "tool_use"
	EventToolResult       EventType = "tool_result"
	EventRetry            EventType = "retry"
	EventCompact          EventType = "compact"
)

type Event struct {
	Type       EventType
	Message    *contracts.Message
	ToolUse    *contracts.ToolUse
	ToolResult *contracts.ToolResult
	Compact    *compactpkg.Result
	Model      string
	Error      error
}

type Runner struct {
	Client                    MessageClient
	Tools                     tool.Executor
	Model                     string
	FallbackModels            []string
	MaxTokens                 int
	MaxToolRounds             int
	UseStreaming              bool
	SessionID                 contracts.ID
	SessionPath               string
	WorkingDirectory          string
	ContentBudget             *session.ContentReplacementState
	ContentBudgetDir          string
	ContentBudgetMax          int
	SkipBudgetTools           map[string]struct{}
	AutoCompact               *compactpkg.AutoConfig
	CompactClient             compactpkg.MessageClient
	CompactMaxTokens          int
	SessionMemoryRoot         string
	EnableSessionMemoryRecall bool
	SessionMemoryRecallRoot   string
	SessionMemoryRecallLimit  int
	OnEvent                   func(Event)
}

type Result struct {
	Messages      []contracts.Message
	Assistant     contracts.Message
	ToolResults   []contracts.ToolResult
	StopReason    string
	Usage         contracts.Usage
	FinalRequest  anthropic.Request
	ModelsAttempt []string
	Compacted     bool
	Compact       *compactpkg.Result
}

func (r Runner) maxTokens() int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return 4096
}

func (r Runner) model() string {
	if r.Model != "" {
		return r.Model
	}
	return "sonnet"
}

func (r Runner) maxToolRounds() int {
	if r.MaxToolRounds > 0 {
		return r.MaxToolRounds
	}
	return 8
}

func (r Runner) emit(event Event) {
	if r.OnEvent != nil {
		r.OnEvent(event)
	}
}
