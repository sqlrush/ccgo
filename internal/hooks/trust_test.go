package hooks

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ccgo/internal/tool"
)

// trustCtx returns a tool.Context with the given interactive and trusted flags.
func trustCtx(interactive, trusted bool) tool.Context {
	return tool.Context{
		Context:          context.Background(),
		WorkingDirectory: "/tmp",
		SessionID:        "sess_trust",
		Metadata: map[string]any{
			tool.MetadataIsInteractiveKey:    interactive,
			tool.MetadataWorkspaceTrustedKey: trusted,
		},
	}
}

// TestCommandHookSkippedWhenUntrustedInteractive verifies that a hook is skipped
// (returns empty HookResult, no block, no action) when the session is interactive
// and the workspace is not trusted.
// CC ref: src/utils/hooks.ts:286-296 (HOOK-62).
func TestCommandHookSkippedWhenUntrustedInteractive(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `exit 2`, // would block if it ran
		Timeout: 5 * time.Second,
	}
	result, err := hook.RunToolHook(trustCtx(true, false), tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Block {
		t.Fatal("hook should not block when untrusted+interactive: it should be skipped entirely")
	}
	if result.Message != "" {
		t.Fatalf("expected empty message from skipped hook, got %q", result.Message)
	}
	if result.Async {
		t.Fatal("unexpected Async flag on skipped result")
	}
}

// TestCommandHookRunsWhenTrusted verifies that a hook runs normally when the
// session is interactive and the workspace IS trusted.
func TestCommandHookRunsWhenTrusted(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `printf 'ran'`,
		Timeout: 5 * time.Second,
	}
	result, err := hook.RunToolHook(trustCtx(true, true), tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Hook ran: message should be "ran"
	if result.Message != "ran" {
		t.Fatalf("expected message 'ran', got %q", result.Message)
	}
}

// TestCommandHookRunsWhenHeadless verifies that a hook runs when the session is
// NOT interactive (headless/SDK), even when workspace trusted key is absent.
// In headless mode workspace trust is implicit.
func TestCommandHookRunsWhenHeadless(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `printf 'headless'`,
		Timeout: 5 * time.Second,
	}
	// Use a context without interactive or trusted keys (simulates headless).
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: "/tmp",
		SessionID:        "sess_headless",
		Metadata:         map[string]any{},
	}
	result, err := hook.RunToolHook(ctx, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message != "headless" {
		t.Fatalf("expected message 'headless', got %q", result.Message)
	}
}

// TestCommandHookRunsWhenInteractiveFalseUntrusted verifies that a hook runs
// when interactive=false even if workspace trust key is explicitly false.
func TestCommandHookRunsWhenInteractiveFalseUntrusted(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Bash",
		Command: `printf 'ok'`,
		Timeout: 5 * time.Second,
	}
	result, err := hook.RunToolHook(trustCtx(false, false), tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"echo hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message != "ok" {
		t.Fatalf("expected message 'ok', got %q", result.Message)
	}
}
