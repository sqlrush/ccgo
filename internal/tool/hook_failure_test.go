package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"ccgo/internal/contracts"
)

// capturePhaseHook records all phases it sees during RunToolHook.
type capturePhaseHook struct {
	phases []string
}

func (h *capturePhaseHook) RunToolHook(_ Context, event HookEvent) (HookResult, error) {
	h.phases = append(h.phases, event.Phase)
	return HookResult{}, nil
}

// alwaysFailTool is a FuncTool whose Call always returns an error.
func alwaysFailTool() FuncTool {
	return FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:     "AlwaysFail",
			ReadOnly: true,
		},
		CallFunc: CallFunc(func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{}, fmt.Errorf("tool failure")
		}),
	}
}

// TestPostToolUseFailureHookFiredOnError verifies that when a tool execution
// fails, the executor fires HookPostToolUseFailure (HOOK-21).
func TestPostToolUseFailureHookFiredOnError(t *testing.T) {
	hook := &capturePhaseHook{}
	registry, err := NewRegistry(alwaysFailTool())
	if err != nil {
		t.Fatal(err)
	}
	executor := Executor{
		Registry: registry,
		Hooks:    []Hook{hook},
	}
	ctx := Context{Context: context.Background()}
	use := contracts.ToolUse{ID: "toolu_fail", Name: "AlwaysFail", Input: []byte(`{}`)}

	result, callErr := executor.Execute(ctx, use, nil)
	if callErr == nil {
		t.Fatal("expected error from AlwaysFail tool, got nil")
	}
	if !result.IsError {
		t.Fatal("expected result.IsError to be true")
	}

	// HookPostToolUseFailure must be present in the captured phases.
	found := false
	for _, p := range hook.phases {
		if p == HookPostToolUseFailure {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("HookPostToolUseFailure not fired; phases seen: %v", hook.phases)
	}
}

// TestPostToolUseFailureNotFiredOnSuccess verifies that HookPostToolUseFailure
// is NOT fired when a tool succeeds.
func TestPostToolUseFailureNotFiredOnSuccess(t *testing.T) {
	hook := &capturePhaseHook{}
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "OK", ReadOnly: true},
		CallFunc: CallFunc(func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "ok"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := Executor{
		Registry: registry,
		Hooks:    []Hook{hook},
	}
	ctx := Context{Context: context.Background()}
	use := contracts.ToolUse{ID: "toolu_ok", Name: "OK", Input: []byte(`{}`)}

	if _, err := executor.Execute(ctx, use, nil); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	for _, p := range hook.phases {
		if p == HookPostToolUseFailure {
			t.Fatalf("HookPostToolUseFailure should not fire on success; got phases: %v", hook.phases)
		}
	}
}

// TestPostToolUseFailurePayloadHasError verifies the error field in the hook
// event payload matches the tool error (HOOK-21 payload spec).
func TestPostToolUseFailurePayloadHasError(t *testing.T) {
	var capturedError string
	hook := HookFunc(func(_ Context, event HookEvent) (HookResult, error) {
		if event.Phase == HookPostToolUseFailure {
			capturedError = event.Error
		}
		return HookResult{}, nil
	})
	registry, err := NewRegistry(alwaysFailTool())
	if err != nil {
		t.Fatal(err)
	}
	executor := Executor{
		Registry: registry,
		Hooks:    []Hook{hook},
	}
	ctx := Context{Context: context.Background()}
	use := contracts.ToolUse{ID: "toolu_payload", Name: "AlwaysFail", Input: []byte(`{}`)}

	if _, callErr := executor.Execute(ctx, use, nil); callErr == nil {
		t.Fatal("expected error")
	}

	if capturedError == "" {
		t.Fatal("HookPostToolUseFailure event.Error must be non-empty")
	}
	if !errors.Is(errors.New(capturedError), errors.New("tool failure")) && capturedError != "tool failure" {
		t.Fatalf("unexpected error text: %q", capturedError)
	}
}
