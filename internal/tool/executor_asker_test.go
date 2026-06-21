package tool

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

type fakeAsker struct {
	behavior contracts.PermissionBehavior
	called   bool
}

func (f *fakeAsker) Ask(_ context.Context, _ PermissionAskRequest) (contracts.PermissionDecision, error) {
	f.called = true
	return contracts.PermissionDecision{Behavior: f.behavior}, nil
}

// askDecider always returns Ask, forcing the asker path.
type askDecider struct{}

func (askDecider) DecideTool(_ Tool, _ json.RawMessage, _ Context) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{Behavior: contracts.PermissionAsk}, nil
}

func newAskExecutor(t *testing.T, asker PermissionAsker) (Executor, contracts.ToolUse, Context) {
	t.Helper()
	echoTool := FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:     "asker_echo",
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
	exec.Asker = asker
	use := contracts.ToolUse{ID: "u1", Name: "asker_echo", Input: json.RawMessage(`{}`)}
	ctx := Context{Context: context.Background(), Permissions: askDecider{}}
	return exec, use, ctx
}

func TestExecutorAskerAllowRunsTool(t *testing.T) {
	asker := &fakeAsker{behavior: contracts.PermissionAllow}
	exec, use, ctx := newAskExecutor(t, asker)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !asker.called {
		t.Fatal("asker not consulted")
	}
	if res.IsError {
		t.Fatalf("expected tool to run, got error result: %q", res.Content)
	}
}

func TestExecutorAskerDenyBlocksTool(t *testing.T) {
	asker := &fakeAsker{behavior: contracts.PermissionDeny}
	exec, use, ctx := newAskExecutor(t, asker)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError, got %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result on deny")
	}
}

func TestExecutorNilAskerPreservesOldBehavior(t *testing.T) {
	exec, use, ctx := newAskExecutor(t, nil)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("nil asker should still return PermissionError, got %v", err)
	}
}
