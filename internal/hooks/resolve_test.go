package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// barrierHook blocks until all N hooks have started (proving parallelism with
// no sleeps), then returns a fixed result.
type barrierHook struct {
	start  *sync.WaitGroup // Done() once on entry
	gate   chan struct{}    // closed after all started
	result tool.HookResult
}

func (h barrierHook) RunToolHook(_ tool.Context, _ tool.HookEvent) (tool.HookResult, error) {
	h.start.Done()
	<-h.gate
	return h.result, nil
}

func resolveWithBarrier(t *testing.T, results []tool.HookResult) Resolution {
	t.Helper()
	var started sync.WaitGroup
	started.Add(len(results))
	gate := make(chan struct{})
	hooks := make([]tool.Hook, len(results))
	for i, r := range results {
		hooks[i] = barrierHook{start: &started, gate: gate, result: r}
	}
	go func() { started.Wait(); close(gate) }() // open gate only once all started
	res, err := Resolve(tool.Context{Context: context.Background()}, hooks,
		tool.HookEvent{Phase: tool.HookPreToolUse})
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	return res
}

func deny() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: "no"}}
}
func ask() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionAsk}}
}
func allow() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionAllow}}
}

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name string
		in   []tool.HookResult
		want contracts.PermissionBehavior
	}{
		{"allow-only", []tool.HookResult{allow(), allow()}, contracts.PermissionAllow},
		{"ask-beats-allow", []tool.HookResult{allow(), ask()}, contracts.PermissionAsk},
		{"deny-beats-ask", []tool.HookResult{ask(), deny()}, contracts.PermissionDeny},
		{"deny-beats-allow", []tool.HookResult{deny(), allow()}, contracts.PermissionDeny},
		{"deny-beats-all", []tool.HookResult{allow(), ask(), deny()}, contracts.PermissionDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolveWithBarrier(t, tc.in)
			if res.PermissionDecision == nil {
				t.Fatalf("nil decision")
			}
			if res.PermissionDecision.Behavior != tc.want {
				t.Fatalf("behavior = %v want %v", res.PermissionDecision.Behavior, tc.want)
			}
		})
	}
}

func TestResolveConcatenatesContext(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{Message: "first"},
		{Message: "second"},
	})
	if len(res.AdditionalContext) != 2 || res.AdditionalContext[0] != "first" || res.AdditionalContext[1] != "second" {
		t.Fatalf("context = %#v", res.AdditionalContext)
	}
	if res.Message != "first\nsecond" {
		t.Fatalf("message = %q", res.Message)
	}
}

func TestResolveBlockIsSticky(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{},
		{Block: true, Message: "blocked here"},
		{},
	})
	if !res.Block || res.Message != "blocked here" {
		t.Fatalf("res = %#v", res)
	}
}

func TestResolveFirstUpdatedInputWins(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{UpdatedInput: json.RawMessage(`{"a":1}`)},
		{UpdatedInput: json.RawMessage(`{"a":2}`)},
	})
	if string(res.UpdatedInput) != `{"a":1}` {
		t.Fatalf("updatedInput = %s", res.UpdatedInput)
	}
}

func TestResolveEmpty(t *testing.T) {
	res, err := Resolve(tool.Context{Context: context.Background()}, nil, tool.HookEvent{})
	if err != nil || res.Block || res.PermissionDecision != nil {
		t.Fatalf("empty resolve = %#v, %v", res, err)
	}
}

// panicHook panics in RunToolHook.
type panicHook struct{}

func (h panicHook) RunToolHook(_ tool.Context, _ tool.HookEvent) (tool.HookResult, error) {
	panic("intentional test panic")
}

// testHook wraps a HookResult for testing.
type testHook struct {
	result tool.HookResult
}

func (h *testHook) RunToolHook(_ tool.Context, _ tool.HookEvent) (tool.HookResult, error) {
	return h.result, nil
}

func TestResolveRecoversHookPanic(t *testing.T) {
	hooks := []tool.Hook{
		&testHook{result: allow()},
		panicHook{},
		&testHook{result: allow()},
	}

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Resolve panicked instead of recovering: %v", r)
			}
		}()
		_, err = Resolve(tool.Context{Context: context.Background()}, hooks, tool.HookEvent{Phase: tool.HookPreToolUse})
		return err
	}()

	// We expect an error from the panicked hook.
	if err == nil {
		t.Fatalf("expected error from panic, got nil")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("expected panic error, got: %v", err)
	}
}

func TestResolveFirstDenyMessageWins(t *testing.T) {
	hooks := []tool.Hook{
		&testHook{result: tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: "first deny"}}},
		&testHook{result: tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: "second deny"}}},
	}

	res, err := Resolve(tool.Context{Context: context.Background()}, hooks, tool.HookEvent{Phase: tool.HookPreToolUse})
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if res.PermissionDecision == nil {
		t.Fatalf("nil decision")
	}
	if res.PermissionDecision.Behavior != contracts.PermissionDeny {
		t.Fatalf("behavior = %v want %v", res.PermissionDecision.Behavior, contracts.PermissionDeny)
	}
	if res.PermissionDecision.Message != "first deny" {
		t.Fatalf("message = %q, want %q", res.PermissionDecision.Message, "first deny")
	}
}
