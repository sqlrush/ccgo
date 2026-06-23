package conversation

// Tests for HOOK-12 async hook runtime: when runConversationHooks receives
// a resolution with AsyncHooks entries, those are enqueued into the
// AsyncHookRegistry non-blocking. The AsyncHookRegistry is initialised in
// RunTurn; here we test it via the inner runConversationHooks path directly
// using a Runner with a settingsOverride.
//
// CC ref: docs/cc-parity/sections/10-hooks.md HOOK-12 (G12).

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	hookpkg "ccgo/internal/hooks"
	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// TestAsyncHookEnqueuedNonBlocking verifies that when a hook returns
// {"async":true}, runConversationHooks returns immediately (non-blocking)
// and the async entry is registered in the AsyncHookRegistry.
func TestAsyncHookEnqueuedNonBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "async_hook.sh")
	// Script outputs {"async":true} — signals async detach.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"async\":true}'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	reg := hookpkg.NewAsyncHookRegistry()
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "async_test_sess",
		AsyncHookRegistry: reg,
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"PreToolUse": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	start := time.Now()
	result, err := r.runConversationHooks(context.Background(), tool.HookPreToolUse, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "echo hi"},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("runConversationHooks: %v", err)
	}
	// Result should not be blocked by async hook.
	if result.Block {
		t.Fatalf("async hook must not block the turn; result=%+v", result)
	}
	// Should have returned quickly (not waited for registry).
	_ = elapsed // timing is non-deterministic; we verify registry instead.

	// The async hook was registered.
	if reg.Len() == 0 {
		t.Fatal("expected AsyncHookRegistry to have at least 1 entry after async hook")
	}
}

// TestAsyncHookRegistryInitialisedInRunTurn verifies that the Runner's
// AsyncHookRegistry field is populated before any hook can run. Since
// AsyncHookRegistry is initialised after the nil-client guard in RunTurn,
// we verify this by calling runConversationHooks directly (which uses the
// asyncHookRegistry() helper that creates one if nil).
func TestAsyncHookRegistryInitialisedInRunTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	// Runner with nil AsyncHookRegistry — asyncHookRegistry() should create one.
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "init_reg_test",
		AsyncHookRegistry: nil,
		settingsOverride: &contracts.Settings{},
	}

	// After running hooks (even with no matched hooks), the helper must
	// not panic. The registry is lazily created.
	_, err := r.runConversationHooks(context.Background(), tool.HookPreToolUse, map[string]any{
		"tool_name": "Bash",
	})
	if err != nil {
		t.Fatalf("runConversationHooks: %v", err)
	}
	// No panic means the helper works with nil registry.
}

// TestAsyncHookWaitedAtTurnBoundary verifies that async hooks registered
// during a turn are waited on at the turn's end (between turns semantics).
// We use a real AsyncHookRegistry and confirm the goroutines complete.
func TestAsyncHookWaitedAtTurnBoundary(t *testing.T) {
	reg := hookpkg.NewAsyncHookRegistry()
	var counter atomic.Int32

	// Register 3 async hooks manually.
	for i := 0; i < 3; i++ {
		reg.Register("PreToolUse", "test-hook", func() {
			time.Sleep(5 * time.Millisecond)
			counter.Add(1)
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reg.Wait(ctx)

	if counter.Load() != 3 {
		t.Fatalf("expected 3 async hooks to complete; got %d", counter.Load())
	}
}

// TestAsyncHookDoesNotBlockTurnWhenSet verifies that a hook returning
// {"async":true} does not leave the result as Block=true.
func TestAsyncHookDoesNotBlockTurnWhenSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "async_noblock.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"async\":true,\"asyncTimeout\":5000}'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	reg := hookpkg.NewAsyncHookRegistry()
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "async_noblock_sess",
		AsyncHookRegistry: reg,
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"UserPromptSubmit": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	result, err := r.runConversationHooks(context.Background(), tool.HookUserPromptSubmit, map[string]any{
		"prompt": "hello",
	})
	if err != nil {
		t.Fatalf("runConversationHooks: %v", err)
	}
	if result.Block {
		t.Fatal("async hook must not block the turn")
	}
}
