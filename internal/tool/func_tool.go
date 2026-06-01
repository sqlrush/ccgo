package tool

import (
	"encoding/json"
	"errors"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
)

type ValidateFunc func(Context, json.RawMessage) error
type PermissionFunc func(Context, json.RawMessage) (contracts.PermissionDecision, error)
type CallFunc func(Context, json.RawMessage, ProgressSink) (contracts.ToolResult, error)
type InputPredicate func(json.RawMessage) bool

type FuncTool struct {
	DefinitionValue contracts.ToolDefinition
	ValidateFunc    ValidateFunc
	PermissionFunc  PermissionFunc
	CallFunc        CallFunc
	ReadOnlyFunc    InputPredicate
	ConcurrencyFunc InputPredicate
	DestructiveFunc InputPredicate
	InterruptValue  string
	MaxResultSize   int
	PromptFunc      func(PromptContext) (string, error)
	InputSchemaFunc func(PromptContext) contracts.JSONSchema
}

func (t FuncTool) Name() string {
	return t.DefinitionValue.Name
}

func (t FuncTool) Aliases() []string {
	return append([]string(nil), t.DefinitionValue.Aliases...)
}

func (t FuncTool) InputSchema(ctx PromptContext) contracts.JSONSchema {
	if t.InputSchemaFunc != nil {
		return t.InputSchemaFunc(ctx)
	}
	return t.DefinitionValue.InputSchema
}

func (t FuncTool) ContractDefinition(ctx PromptContext) (contracts.ToolDefinition, error) {
	definition := t.DefinitionValue
	prompt, err := t.Prompt(ctx)
	if err != nil {
		return contracts.ToolDefinition{}, err
	}
	definition.Prompt = prompt
	definition.InputSchema = t.InputSchema(ctx)
	definition.ReadOnly = t.IsReadOnly(nil)
	definition.ConcurrencySafe = t.IsConcurrencySafe(nil)
	definition.Destructive = t.IsDestructive(nil)
	definition.InterruptBehavior = t.InterruptBehavior()
	definition.MaxResultSizeChars = t.MaxResultSizeChars()
	return definition, nil
}

func (t FuncTool) Prompt(ctx PromptContext) (string, error) {
	if t.PromptFunc != nil {
		return t.PromptFunc(ctx)
	}
	return t.DefinitionValue.Prompt, nil
}

func (t FuncTool) Validate(ctx Context, raw json.RawMessage) error {
	if err := ValidateSchema(t.DefinitionValue.InputSchema, raw); err != nil {
		return err
	}
	if t.ValidateFunc != nil {
		return t.ValidateFunc(ctx, normalizeRawInput(raw))
	}
	return nil
}

func (t FuncTool) CheckPermissions(ctx Context, raw json.RawMessage) (contracts.PermissionDecision, error) {
	if t.PermissionFunc != nil {
		return t.PermissionFunc(ctx, normalizeRawInput(raw))
	}
	if ctx.Permissions == nil {
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "no permission engine configured"}, nil
	}
	return ctx.Permissions.DecideTool(t, normalizeRawInput(raw), ctx)
}

func (t FuncTool) Call(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
	if t.CallFunc == nil {
		return contracts.ToolResult{}, errors.New("tool has no call function")
	}
	return t.CallFunc(ctx, normalizeRawInput(raw), sink)
}

func (t FuncTool) IsReadOnly(raw json.RawMessage) bool {
	if t.ReadOnlyFunc != nil {
		return t.ReadOnlyFunc(normalizeRawInput(raw))
	}
	return t.DefinitionValue.ReadOnly
}

func (t FuncTool) IsConcurrencySafe(raw json.RawMessage) bool {
	if t.ConcurrencyFunc != nil {
		return t.ConcurrencyFunc(normalizeRawInput(raw))
	}
	return t.DefinitionValue.ConcurrencySafe
}

func (t FuncTool) IsDestructive(raw json.RawMessage) bool {
	if t.DestructiveFunc != nil {
		return t.DestructiveFunc(normalizeRawInput(raw))
	}
	return t.DefinitionValue.Destructive
}

func (t FuncTool) InterruptBehavior() string {
	if t.InterruptValue != "" {
		return t.InterruptValue
	}
	if t.DefinitionValue.InterruptBehavior != "" {
		return t.DefinitionValue.InterruptBehavior
	}
	return "block"
}

func (t FuncTool) MaxResultSizeChars() int {
	if t.MaxResultSize != 0 {
		return t.MaxResultSize
	}
	return t.DefinitionValue.MaxResultSizeChars
}

type EnginePermissionDecider struct {
	Engine permissions.Engine
}

func (d EnginePermissionDecider) DecideTool(t Tool, raw json.RawMessage, ctx Context) (contracts.PermissionDecision, error) {
	raw = normalizeRawInput(raw)
	req := permissions.Request{
		ToolName:         t.Name(),
		Input:            raw,
		Command:          firstInputString(raw, "command", "cmd"),
		Path:             firstInputString(raw, "file_path", "path"),
		WorkingDirectory: ctx.WorkingDirectory,
		ReadOnly:         t.IsReadOnly(raw),
		WritesFiles:      !t.IsReadOnly(raw) && !t.IsDestructive(raw),
		Destructive:      t.IsDestructive(raw),
	}
	return d.Engine.Decide(req), nil
}

func NewEnginePermissionDecider(engine permissions.Engine) EnginePermissionDecider {
	return EnginePermissionDecider{Engine: engine}
}

func firstInputString(raw json.RawMessage, keys ...string) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value
		}
	}
	return ""
}

func ErrorResult(use contracts.ToolUse, err error) contracts.ToolResult {
	return contracts.ToolResult{
		ToolUseID: use.ID,
		IsError:   true,
		Content:   fmt.Sprintf("%v", err),
	}
}
