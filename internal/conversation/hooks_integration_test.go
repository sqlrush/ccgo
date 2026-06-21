package conversation

// Integration test for hook lifecycle fire order and event precedence.
// Exercises real echo-script hooks (shell) in t.TempDir() — proves end-to-end:
// (a) SessionStart hook injects additionalContext.
// (b) UserPromptSubmit deny hook blocks the turn.
// (c) Two PreToolUse hooks (allow + deny) resolve to DENY via the parallel fold.
// (d) Lifecycle firing ORDER is correct: SessionStart → UserPromptSubmit → SessionEnd.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	msgs "ccgo/internal/messages"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// skipOnWindows skips the test on Windows where /bin/sh is unavailable.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script hooks require /bin/sh; skipping on Windows")
	}
}

// writeEchoScript writes a shell script to path that:
//   - appends label to orderFile (for ordering proof), and
//   - emits stdoutJSON to stdout (for JSON-output proof, empty string → no output).
//
// The script is made executable (0755) so CommandHook can run it directly.
func writeEchoScript(t *testing.T, path, orderFile, label, stdoutJSON string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if orderFile != "" {
		sb.WriteString("printf '")
		sb.WriteString(label)
		sb.WriteString("\\n' >> ")
		sb.WriteString(shellQuoteConv(orderFile))
		sb.WriteString("\n")
	}
	if stdoutJSON != "" {
		sb.WriteString("printf '%s' ")
		sb.WriteString(shellQuoteConv(stdoutJSON))
		sb.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0755); err != nil {
		t.Fatalf("writeEchoScript %s: %v", path, err)
	}
}

// TestHookIntegrationSessionStartInjectsContext proves (a): a SessionStart hook
// script that outputs {"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"ctx-injected"}}
// causes RunSessionStartHooks to return "ctx-injected".
func TestHookIntegrationSessionStartInjectsContext(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "ss_ctx.sh")
	writeEchoScript(t, script, "", "",
		`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"ctx-injected"}}`)

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "integ_sess_ctx",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	got, err := r.RunSessionStartHooks(context.Background(), SessionStartStartup)
	if err != nil {
		t.Fatalf("RunSessionStartHooks: %v", err)
	}
	if got != "ctx-injected" {
		t.Fatalf("additionalContext = %q; want %q", got, "ctx-injected")
	}
}

// TestHookIntegrationUserPromptSubmitDeny proves (b): a UserPromptSubmit hook
// that exits 2 causes applyUserPromptSubmitHooks to return blocked=true.
func TestHookIntegrationUserPromptSubmitDeny(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "ups_deny.sh")
	// Exit 2 is the "block" signal for CommandHook.
	const scriptContent = "#!/bin/sh\nprintf 'blocked by test hook' >&2\nexit 2\n"
	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("write deny script: %v", err)
	}

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "integ_ups_deny",
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

	userMsg := msgs.UserText("hello world")
	_, blocked, blockMsg, err := r.applyUserPromptSubmitHooks(context.Background(), []contracts.Message{userMsg})
	if err != nil {
		t.Fatalf("applyUserPromptSubmitHooks: %v", err)
	}
	if !blocked {
		t.Fatal("expected blocked=true from exit-2 hook; got false")
	}
	if strings.TrimSpace(blockMsg) == "" {
		t.Fatal("expected non-empty block message")
	}
}

// TestHookIntegrationPreToolUseDenyPrecedence proves (c): two PreToolUse hooks —
// one that approves (permissionDecision=allow) and one that denies
// (permissionDecision=deny) — resolve to DENY via the parallel fold.
func TestHookIntegrationPreToolUseDenyPrecedence(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()

	allowScript := filepath.Join(dir, "pre_allow.sh")
	writeEchoScript(t, allowScript, "", "",
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"allow-hook"}}`)

	denyScript := filepath.Join(dir, "pre_deny.sh")
	writeEchoScript(t, denyScript, "", "",
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"deny-hook"}}`)

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "integ_pre_deny",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"PreToolUse": []any{map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": allowScript},
						map[string]any{"type": "command", "command": denyScript},
					},
				}},
			},
		},
	}

	result, err := r.runConversationHooks(context.Background(), tool.HookPreToolUse, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{"command": "echo hi"},
	})
	if err != nil {
		t.Fatalf("runConversationHooks: %v", err)
	}
	if !result.Block {
		t.Fatalf("expected Block=true from deny fold; got %#v", result)
	}
	if result.PermissionDecision == nil {
		t.Fatal("expected non-nil PermissionDecision")
	}
	if result.PermissionDecision.Behavior != contracts.PermissionDeny {
		t.Fatalf("PermissionDecision.Behavior = %q; want %q", result.PermissionDecision.Behavior, contracts.PermissionDeny)
	}
}

// TestHookIntegrationLifecycleFireOrder proves (d): SessionStart fires before
// UserPromptSubmit which fires before SessionEnd — proven via monotonic appends
// to a shared order file by real shell scripts in t.TempDir().
func TestHookIntegrationLifecycleFireOrder(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	orderFile := filepath.Join(dir, "order.log")

	startScript := filepath.Join(dir, "ss.sh")
	writeEchoScript(t, startScript, orderFile, "start", "")

	promptScript := filepath.Join(dir, "ups.sh")
	writeEchoScript(t, promptScript, orderFile, "prompt", "")

	endScript := filepath.Join(dir, "se.sh")
	writeEchoScript(t, endScript, orderFile, "end", "")

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "integ_order",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": startScript,
					}},
				}},
				"UserPromptSubmit": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": promptScript,
					}},
				}},
				"SessionEnd": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": endScript,
					}},
				}},
			},
		},
	}

	ctx := context.Background()

	if _, err := r.RunSessionStartHooks(ctx, SessionStartStartup); err != nil {
		t.Fatalf("RunSessionStartHooks: %v", err)
	}

	userMsg := msgs.UserText("hello")
	if _, blocked, _, err := r.applyUserPromptSubmitHooks(ctx, []contracts.Message{userMsg}); err != nil {
		t.Fatalf("applyUserPromptSubmitHooks: %v", err)
	} else if blocked {
		t.Fatal("order test: prompt hook must not block")
	}

	if err := r.RunSessionEndHooks(ctx, SessionEndPromptInputExit); err != nil {
		t.Fatalf("RunSessionEndHooks: %v", err)
	}

	data, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("read order.log: %v", err)
	}
	got := strings.Fields(string(data))
	want := []string{"start", "prompt", "end"}
	if len(got) != len(want) {
		t.Fatalf("fire order len=%d want %d; got=%v", len(got), len(want), got)
	}
	for i, label := range want {
		if got[i] != label {
			t.Fatalf("fire order[%d] = %q; want %q; full order = %v", i, got[i], label, got)
		}
	}
}
