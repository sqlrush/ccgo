package tool

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

// staticPermHook returns a fixed PermissionDecision for the PermissionRequest phase.
type staticPermHook struct {
	behavior contracts.PermissionBehavior
}

func (h staticPermHook) HookPhases() []string { return []string{HookPermissionRequest} }
func (h staticPermHook) RunToolHook(_ Context, _ HookEvent) (HookResult, error) {
	return HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: h.behavior}}, nil
}

// newPermExecutor builds an executor with the given hook behaviors applied in order.
// It uses a FuncTool named "perm_echo" registered under that name.
// askDecider (from executor_asker_test.go) forces the Ask path so hooks participate.
func newPermExecutor(t *testing.T, behaviors ...contracts.PermissionBehavior) (Executor, contracts.ToolUse, Context) {
	t.Helper()
	echoTool := FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:     "perm_echo",
			ReadOnly: true,
		},
		CallFunc: func(_ Context, _ json.RawMessage, _ ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "ok"}, nil
		},
	}
	reg, err := NewRegistry(echoTool)
	if err != nil {
		t.Fatal(err)
	}
	exec := NewExecutor(reg)
	for _, b := range behaviors {
		exec.Hooks = append(exec.Hooks, staticPermHook{behavior: b})
	}
	use := contracts.ToolUse{ID: "u1", Name: "perm_echo", Input: json.RawMessage(`{}`)}
	// askDecider forces PermissionAsk so runPermissionRequestHooks is invoked.
	ctx := Context{Context: context.Background(), Permissions: askDecider{}}
	return exec, use, ctx
}

// TestExecutorPermissionDenyBeatsAllow: allow + deny → deny blocks (deny wins regardless of order).
func TestExecutorPermissionDenyBeatsAllow(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionAllow, contracts.PermissionDeny)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError (deny wins), got %v", err)
	}
}

// TestExecutorPermissionDenyBeatsAllowReversedOrder: deny + allow → deny still blocks (order-independent).
// With the old last-wins code, deny then allow resolves to allow (tool runs) — proving the bug.
func TestExecutorPermissionDenyBeatsAllowReversedOrder(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionDeny, contracts.PermissionAllow)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError (deny wins regardless of order), got %v", err)
	}
}

// TestExecutorPermissionAllowWhenAllAllow: allow + allow → tool runs.
func TestExecutorPermissionAllowWhenAllAllow(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionAllow, contracts.PermissionAllow)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if err != nil {
		t.Fatalf("expected allow to run tool, got %v", err)
	}
	if res.IsError {
		t.Fatalf("expected non-error result, got %q", res.Content)
	}
}

// TestExecutorPermissionAskBeatsAllow: ask + allow → still asks (ask wins over allow).
// When hooks fold to Ask and there is no Asker, the executor falls through to PermissionError.
func TestExecutorPermissionAskBeatsAllow(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionAsk, contracts.PermissionAllow)
	// No Asker set, so Ask resolves to PermissionError.
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError (ask without asker), got %v", err)
	}
}
