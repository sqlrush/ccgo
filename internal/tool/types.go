package tool

import (
	"context"
	"encoding/json"

	"ccgo/internal/contracts"
)

type PromptContext struct {
	Model            string
	WorkingDirectory string
	Metadata         map[string]any
}

type AgentInfo struct {
	Name           string
	Description    string
	Path           string
	Prompt         string
	Model          string
	PermissionMode contracts.PermissionMode
	AllowedTools   []string
}

type Context struct {
	Context          context.Context
	WorkingDirectory string
	SessionID        contracts.ID
	Permissions      PermissionDecider
	Metadata         map[string]any
}

type PermissionDecider interface {
	DecideTool(tool Tool, input json.RawMessage, ctx Context) (contracts.PermissionDecision, error)
}

type Tool interface {
	Name() string
	Aliases() []string
	InputSchema(PromptContext) contracts.JSONSchema
	Prompt(PromptContext) (string, error)
	Validate(Context, json.RawMessage) error
	CheckPermissions(Context, json.RawMessage) (contracts.PermissionDecision, error)
	Call(Context, json.RawMessage, ProgressSink) (contracts.ToolResult, error)
	IsReadOnly(json.RawMessage) bool
	IsConcurrencySafe(json.RawMessage) bool
	IsDestructive(json.RawMessage) bool
	InterruptBehavior() string
	MaxResultSizeChars() int
}

type DefinitionProvider interface {
	ContractDefinition(PromptContext) (contracts.ToolDefinition, error)
}

type ProgressSink interface {
	Send(contracts.ToolProgress) error
}

type ProgressFunc func(contracts.ToolProgress) error

func (f ProgressFunc) Send(progress contracts.ToolProgress) error {
	return f(progress)
}

type nopProgressSink struct{}

func (nopProgressSink) Send(contracts.ToolProgress) error {
	return nil
}

func NopProgressSink() ProgressSink {
	return nopProgressSink{}
}

const (
	MetadataInternalPathContextKey = "ccgo.permissions.internal_paths"
	MetadataSettingsKey            = "ccgo.settings"
	MetadataSessionPathKey         = "ccgo.session.path"
	MetadataAvailableAgentsKey     = "ccgo.available_agents"

	HookPreToolUse        = "PreToolUse"
	HookPostToolUse       = "PostToolUse"
	HookPermissionRequest = "PermissionRequest"
	HookPermissionDenied  = "PermissionDenied"
	HookUserPromptSubmit  = "UserPromptSubmit"
	HookStop              = "Stop"
	HookSubagentStop      = "SubagentStop"
)

type HookEvent struct {
	Phase    string
	ToolUse  contracts.ToolUse
	ToolName string
	Input    json.RawMessage
	Decision *contracts.PermissionDecision
	Result   *contracts.ToolResult
	Error    string
	Payload  map[string]any
}

type HookResult struct {
	Block              bool
	Message            string
	UpdatedInput       json.RawMessage
	PermissionDecision *contracts.PermissionDecision
	Metadata           map[string]any
}

type Hook interface {
	RunToolHook(Context, HookEvent) (HookResult, error)
}

type PhaseHook interface {
	HookPhases() []string
}

type HookFunc func(Context, HookEvent) (HookResult, error)

func (f HookFunc) RunToolHook(ctx Context, event HookEvent) (HookResult, error) {
	return f(ctx, event)
}

func SendProgress(sink ProgressSink, toolUseID contracts.ID, progressType string, data map[string]any) error {
	if sink == nil {
		return nil
	}
	return sink.Send(contracts.ToolProgress{ToolUseID: toolUseID, Type: progressType, Data: data})
}

func Definition(ctx PromptContext, t Tool) (contracts.ToolDefinition, error) {
	if provider, ok := t.(DefinitionProvider); ok {
		return provider.ContractDefinition(ctx)
	}
	prompt, err := t.Prompt(ctx)
	if err != nil {
		return contracts.ToolDefinition{}, err
	}
	return contracts.ToolDefinition{
		Name:               t.Name(),
		Aliases:            t.Aliases(),
		Prompt:             prompt,
		InputSchema:        t.InputSchema(ctx),
		ReadOnly:           t.IsReadOnly(nil),
		Destructive:        t.IsDestructive(nil),
		ConcurrencySafe:    t.IsConcurrencySafe(nil),
		MaxResultSizeChars: t.MaxResultSizeChars(),
		InterruptBehavior:  t.InterruptBehavior(),
	}, nil
}
