package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

var ErrUnknownTool = errors.New("unknown tool")
var ErrPermissionDenied = errors.New("permission denied")

type PermissionError struct {
	Decision contracts.PermissionDecision
}

func (e PermissionError) Error() string {
	if e.Decision.Message != "" {
		return e.Decision.Message
	}
	return string(ErrPermissionDenied.Error())
}

type Executor struct {
	Registry       *Registry
	ResultStoreDir string
	Hooks          []Hook
}

func NewExecutor(registry *Registry) Executor {
	return Executor{Registry: registry}
}

func (e Executor) Execute(ctx Context, use contracts.ToolUse, sink ProgressSink) (contracts.ToolResult, error) {
	if sink == nil {
		sink = NopProgressSink()
	}
	if err := contextError(ctx); err != nil {
		return ErrorResult(use, err), err
	}
	if e.Registry == nil {
		err := fmt.Errorf("%w: registry is nil", ErrUnknownTool)
		return ErrorResult(use, err), err
	}
	t, ok := e.Registry.Lookup(use.Name)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrUnknownTool, use.Name)
		return ErrorResult(use, err), err
	}
	raw := normalizeRawInput(use.Input)
	if err := t.Validate(ctx, raw); err != nil {
		return ErrorResult(use, err), err
	}
	raw, err := e.runPreHooks(ctx, use, t, raw)
	if err != nil {
		return ErrorResult(use, err), err
	}
	if err := t.Validate(ctx, raw); err != nil {
		return ErrorResult(use, err), err
	}
	if err := contextError(ctx); err != nil {
		return ErrorResult(use, err), err
	}
	_ = SendProgress(sink, use.ID, "started", map[string]any{"tool": t.Name()})
	decision, err := t.CheckPermissions(ctx, raw)
	if err != nil {
		_ = SendProgress(sink, use.ID, "failed", map[string]any{"tool": t.Name(), "error": err.Error()})
		return ErrorResult(use, err), err
	}
	if decision.Behavior == contracts.PermissionDeny {
		permissionErr := PermissionError{Decision: decision}
		result := contracts.ToolResult{
			ToolUseID: use.ID,
			IsError:   true,
			Content:   decision.Message,
			Meta: map[string]any{
				"permission": decision,
			},
		}
		result = e.runPermissionDeniedHooks(ctx, use, t, raw, decision, result, permissionErr)
		_ = SendProgress(sink, use.ID, "permission_denied", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
		return result, permissionErr
	}
	if decision.Behavior == contracts.PermissionAsk {
		permissionErr := PermissionError{Decision: decision}
		result := contracts.ToolResult{
			ToolUseID: use.ID,
			IsError:   true,
			Content:   decision.Message,
			Meta: map[string]any{
				"permission": decision,
			},
		}
		result = e.runPermissionDeniedHooks(ctx, use, t, raw, decision, result, permissionErr)
		_ = SendProgress(sink, use.ID, "permission_denied", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
		return result, permissionErr
	}
	if err := contextError(ctx); err != nil {
		_ = SendProgress(sink, use.ID, "cancelled", map[string]any{"tool": t.Name(), "error": err.Error()})
		return ErrorResult(use, err), err
	}
	result, err := t.Call(ctx, raw, sink)
	if result.ToolUseID == "" {
		result.ToolUseID = use.ID
	}
	if err == nil {
		result = e.limitResult(t, use, result)
	}
	result, hookErr := e.runPostHooks(ctx, use, t, raw, result, err)
	if hookErr != nil && err == nil {
		err = hookErr
	}
	if err != nil {
		if result.Content == nil {
			result.Content = err.Error()
		}
		result.IsError = true
		_ = SendProgress(sink, use.ID, "failed", map[string]any{"tool": t.Name(), "error": err.Error()})
		return result, err
	}
	_ = SendProgress(sink, use.ID, "completed", map[string]any{"tool": t.Name()})
	return result, nil
}

func (e Executor) runPreHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage) (json.RawMessage, error) {
	current := normalizeRawInput(raw)
	for _, hook := range e.Hooks {
		result, err := hook.RunToolHook(ctx, HookEvent{Phase: HookPreToolUse, ToolUse: use, ToolName: t.Name(), Input: current})
		if err != nil {
			return current, err
		}
		if len(result.UpdatedInput) > 0 {
			current = normalizeRawInput(result.UpdatedInput)
		}
		if result.Block {
			if result.Message == "" {
				result.Message = "blocked by PreToolUse hook"
			}
			return current, HookBlockedError{Phase: HookPreToolUse, Message: result.Message, Metadata: result.Metadata}
		}
	}
	return current, nil
}

func (e Executor) runPermissionDeniedHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, decision contracts.PermissionDecision, result contracts.ToolResult, originalErr error) contracts.ToolResult {
	for _, hook := range e.Hooks {
		hookResult, err := hook.RunToolHook(ctx, HookEvent{Phase: HookPermissionDenied, ToolUse: use, ToolName: t.Name(), Input: raw, Decision: &decision, Result: &result, Error: originalErr.Error()})
		if result.Meta == nil {
			result.Meta = map[string]any{}
		}
		if err != nil {
			result.Meta["permission_denied_hook_error"] = err.Error()
			continue
		}
		if hookResult.Message != "" {
			result.Meta["permission_denied_hook_message"] = hookResult.Message
		}
		if len(hookResult.Metadata) > 0 {
			result.Meta["permission_denied_hook"] = hookResult.Metadata
		}
	}
	return result
}

func (e Executor) runPostHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, result contracts.ToolResult, callErr error) (contracts.ToolResult, error) {
	var errText string
	if callErr != nil {
		errText = callErr.Error()
	}
	for _, hook := range e.Hooks {
		hookResult, err := hook.RunToolHook(ctx, HookEvent{Phase: HookPostToolUse, ToolUse: use, ToolName: t.Name(), Input: raw, Result: &result, Error: errText})
		if err != nil {
			return result, err
		}
		if hookResult.Block {
			if hookResult.Message == "" {
				hookResult.Message = "blocked by PostToolUse hook"
			}
			return result, HookBlockedError{Phase: HookPostToolUse, Message: hookResult.Message, Metadata: hookResult.Metadata}
		}
		if len(hookResult.Metadata) > 0 {
			if result.Meta == nil {
				result.Meta = map[string]any{}
			}
			result.Meta["post_tool_use_hook"] = hookResult.Metadata
		}
	}
	return result, nil
}

type HookBlockedError struct {
	Phase    string
	Message  string
	Metadata map[string]any
}

func (e HookBlockedError) Error() string {
	return e.Message
}

func contextError(ctx Context) error {
	if ctx.Context == nil {
		return nil
	}
	if err := ctx.Context.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return err
	}
	return nil
}

func (e Executor) limitResult(t Tool, use contracts.ToolUse, result contracts.ToolResult) contracts.ToolResult {
	limit := t.MaxResultSizeChars()
	if limit <= 0 {
		return result
	}
	content, ok := result.Content.(string)
	if !ok || len(content) <= limit {
		return result
	}
	dir := e.ResultStoreDir
	if dir == "" {
		dir = filepath.Join(platform.ClaudeHomeDir(), "tool-results")
	}
	name := string(use.ID)
	if name == "" {
		name = string(contracts.NewID())
	}
	path := filepath.Join(dir, sanitizeResultFileName(name)+".txt")
	_ = platform.AtomicWriteFile(path, []byte(content), 0o600)
	preview := content[:limit]
	result.Content = fmt.Sprintf("%s\n\n[Tool output truncated; full output saved to %s]", strings.TrimRight(preview, "\n"), path)
	if result.Meta == nil {
		result.Meta = map[string]any{}
	}
	result.Meta["truncated"] = true
	result.Meta["full_output_path"] = path
	result.Meta["full_output_bytes"] = len(content)
	return result
}

func sanitizeResultFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		default:
			return r
		}
	}, name)
	if name == "." || name == string(os.PathSeparator) || name == "" {
		return "tool-result"
	}
	return name
}
