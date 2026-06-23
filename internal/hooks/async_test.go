package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ccgo/internal/tool"
)

// TestAsyncHookRegistryRegisterAndWait verifies that registered async hooks
// complete and can be waited on.
func TestAsyncHookRegistryRegisterAndWait(t *testing.T) {
	registry := NewAsyncHookRegistry()
	var counter atomic.Int32

	id := registry.Register("test-phase", "test-hook", func() {
		time.Sleep(5 * time.Millisecond)
		counter.Add(1)
	})
	if id == "" {
		t.Fatal("expected non-empty id from Register")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	registry.Wait(ctx)

	if counter.Load() != 1 {
		t.Fatalf("async hook did not complete: counter=%d", counter.Load())
	}
}

// TestAsyncHookRegistryIsRaceFree verifies concurrent Register+Wait is race-free.
// Primarily meaningful under -race.
func TestAsyncHookRegistryIsRaceFree(t *testing.T) {
	registry := NewAsyncHookRegistry()
	var sum atomic.Int32
	const n = 20
	for i := 0; i < n; i++ {
		registry.Register("phase", "hook", func() {
			sum.Add(1)
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry.Wait(ctx)
	if sum.Load() != n {
		t.Fatalf("expected %d completions, got %d", n, sum.Load())
	}
}

// TestHookResultFromJSONAsyncTrue verifies that {"async":true} sets Async=true.
func TestHookResultFromJSONAsyncTrue(t *testing.T) {
	raw := `{"async":true}`
	result, ok := hookResultFromJSON("PreToolUse", raw)
	if !ok {
		t.Fatal("hookResultFromJSON returned not-ok for async JSON")
	}
	if !result.Async {
		t.Fatalf("expected Async=true, got %+v", result)
	}
}

// TestHookResultFromJSONAsyncTimeout verifies that asyncTimeout is parsed.
func TestHookResultFromJSONAsyncTimeout(t *testing.T) {
	raw := `{"async":true,"asyncTimeout":3000}`
	result, ok := hookResultFromJSON("PreToolUse", raw)
	if !ok {
		t.Fatal("hookResultFromJSON returned not-ok")
	}
	if !result.Async {
		t.Fatalf("expected Async=true, got %+v", result)
	}
	if result.AsyncTimeout != 3000 {
		t.Fatalf("expected AsyncTimeout=3000, got %d", result.AsyncTimeout)
	}
}

// TestCommandHookWithAsyncJSONOutputSetsAsyncFlag verifies that when a command
// hook outputs {"async":true}, RunToolHook returns with Async=true.
func TestCommandHookWithAsyncJSONOutputSetsAsyncFlag(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `printf '%s\n' '{"async":true}'`,
		Timeout: 10 * time.Second,
	}
	result, err := hook.RunToolHook(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: t.TempDir(),
		SessionID:        "sess_async",
	}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Async {
		t.Fatalf("expected result.Async=true, got %+v", result)
	}
}

// TestHookResultAsyncFieldNotSetWhenFalse verifies that regular JSON output
// does NOT set Async=true.
func TestHookResultAsyncFieldNotSetWhenFalse(t *testing.T) {
	raw := `{"continue":true,"systemMessage":"hello"}`
	result, ok := hookResultFromJSON("PreToolUse", raw)
	if !ok {
		t.Fatal("hookResultFromJSON returned not-ok")
	}
	if result.Async {
		t.Fatalf("Async should be false for non-async JSON, got %+v", result)
	}
}

// TestAsyncHookRegistryWaitRespectsContextCancellation verifies that Wait
// returns promptly when the context is cancelled.
func TestAsyncHookRegistryWaitRespectsContextCancellation(t *testing.T) {
	registry := NewAsyncHookRegistry()
	registry.Register("phase", "slow-hook", func() {
		time.Sleep(10 * time.Second)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	registry.Wait(ctx)
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("Wait did not respect context cancellation promptly")
	}
}

// TestAsyncHookRegistryLen verifies the Len helper returns the registered count.
func TestAsyncHookRegistryLen(t *testing.T) {
	registry := NewAsyncHookRegistry()
	if registry.Len() != 0 {
		t.Fatalf("expected 0, got %d", registry.Len())
	}
	registry.Register("phase", "hook", func() {})
	if registry.Len() != 1 {
		t.Fatalf("expected 1, got %d", registry.Len())
	}
}

// TestAsyncHookRegistryCancelReturnsTrueAndRemoves verifies Cancel returns true
// for a known id, removes it from the registry, and unblocks any Wait.
// CC ref: SDK-35 cancel_async_message (G12).
func TestAsyncHookRegistryCancelReturnsTrueAndRemoves(t *testing.T) {
	registry := NewAsyncHookRegistry()
	// Register a slow hook that will never finish on its own within test lifetime.
	id := registry.Register("phase", "slow", func() { time.Sleep(10 * time.Second) })

	if registry.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", registry.Len())
	}

	cancelled := registry.Cancel(id)
	if !cancelled {
		t.Fatal("Cancel should return true for a known id")
	}
	if registry.Len() != 0 {
		t.Fatalf("expected 0 entries after cancel, got %d", registry.Len())
	}

	// Wait should now return immediately since the entry was removed/done.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	registry.Wait(ctx)
	// If we get here without timeout the test passes.
}

// TestAsyncHookRegistryCancelReturnsFalseForUnknown verifies Cancel returns
// false for an unknown or already-completed id.
func TestAsyncHookRegistryCancelReturnsFalseForUnknown(t *testing.T) {
	registry := NewAsyncHookRegistry()
	if registry.Cancel("nonexistent") {
		t.Fatal("Cancel should return false for unknown id")
	}
}

// TestAsyncHookRegistryCancelRace verifies that concurrent Cancel + goroutine
// completion does not panic (double-close) under -race.
func TestAsyncHookRegistryCancelRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		registry := NewAsyncHookRegistry()
		var done atomic.Bool
		id := registry.Register("phase", "race", func() {
			// tiny sleep so Cancel sometimes races with goroutine close
			time.Sleep(time.Microsecond)
			done.Store(true)
		})
		// Cancel concurrently — must not panic.
		registry.Cancel(id)
	}
}

// TestAsyncResultNotParsedFromNonJSONStdout verifies that plain-text stdout
// does not set Async=true.
func TestAsyncResultNotParsedFromNonJSONStdout(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `echo "hello world"`,
		Timeout: 10 * time.Second,
	}
	result, err := hook.RunToolHook(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: t.TempDir(),
		SessionID:        "sess_sync",
	}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Async {
		t.Fatalf("Async should be false for plain-text stdout, got %+v", result)
	}
	if !strings.Contains(result.Message, "hello world") {
		t.Fatalf("expected message to contain 'hello world', got %q", result.Message)
	}
}
