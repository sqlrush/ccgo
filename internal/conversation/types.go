package conversation

import (
	"context"
	"time"

	"ccgo/internal/api/anthropic"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
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
	EventToolProgress     EventType = "tool_progress"
	EventRetry            EventType = "retry"
	EventTokenWarning     EventType = "token_warning"
	EventCompact          EventType = "compact"
	EventStreamEvent      EventType = "stream_event"
)

type TokenWarning struct {
	TokenUsage int
	Window     compactpkg.WindowConfig
	State      compactpkg.WarningState
}

type Event struct {
	Type         EventType
	Message      *contracts.Message
	ToolUse      *contracts.ToolUse
	ToolResult   *contracts.ToolResult
	ToolProgress *contracts.ToolProgress
	TokenWarning *TokenWarning
	Compact      *compactpkg.Result
	StreamEvent  *anthropic.StreamEvent
	Model        string
	Error        error
}

type Runner struct {
	Client                    MessageClient
	Tools                     tool.Executor
	MCP                       *MCPConfig
	Permissions               tool.PermissionDecider
	PermissionMode            contracts.PermissionMode
	APIKeySource              string
	BetaHeaders               []string
	FastMode                  bool
	Model                     string
	FallbackModels            []string
	SystemPrompt              string
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
	RelevantMemoryDir         string
	RelevantMemoryLimit       int
	SkillDirs                 []string
	EnableMemoryExtraction    bool
	MemoryAgentClient         memory.MessageClient
	MemoryExtractLimit        int
	OnEvent                   func(Event)
}

type MCPConfig struct {
	UserSettings    contracts.Settings
	ProjectSettings contracts.Settings
	LocalSettings   contracts.Settings
	PluginServers   map[string]contracts.MCPServer
	CWD             string
	ParseOptions    mcp.ParseOptions
	ToolOptions     mcp.ServerToolOptions
}

type Result struct {
	Messages      []contracts.Message
	Assistant     contracts.Message
	ToolResults   []contracts.ToolResult
	StopReason    string
	Usage         contracts.Usage
	APIDuration   time.Duration
	FinalRequest  anthropic.Request
	ModelsAttempt []string
	Compacted     bool
	Compact       *compactpkg.Result
	Cleared       bool
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
	r.recordTelemetry(event)
	if r.OnEvent != nil {
		r.OnEvent(event)
	}
}
