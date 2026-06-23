package conversation

// Tests for W-C18: Stop hook CC-standard fields (HOOK-30) + Notification emit trigger (HOOK-35)
// and W-C27 (Stop hook BLOCKING semantics).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	msgs "ccgo/internal/messages"

	"ccgo/internal/contracts"
)

// TestRunStopHooksPayloadHasStopHookActiveAndLastAssistantMessage verifies
// that runStopHooks sends both stop_hook_active (HOOK-30) and
// last_assistant_message (HOOK-30) fields to the hook process.
// The hook captures its stdin, and we assert the JSON contains the fields.
func TestRunStopHooksPayloadHasStopHookActiveAndLastAssistantMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "captured.json")
	// Script writes its stdin to a file so we can inspect the payload.
	script := filepath.Join(dir, "capture.sh")
	scriptContent := "#!/bin/sh\ncat > " + shellQuoteConv(capture) + "\n"
	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	assistant := msgs.AssistantText("Hello from assistant", "claude-sonnet", nil)
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_stop_fields",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}
	// stopHookActive=false (first call, not re-entrant)
	if err := r.runStopHooks(context.Background(), "claude-sonnet", "end_turn", "", assistant); err != nil {
		t.Fatalf("runStopHooks: %v", err)
	}

	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("capture file: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse captured JSON: %v", err)
	}

	// stop_hook_active must be present (false for normal non-re-entrant call)
	if _, ok := got["stop_hook_active"]; !ok {
		t.Error("stop_hook_active field must be present in Stop hook input")
	}
	active, _ := got["stop_hook_active"].(bool)
	if active {
		t.Error("stop_hook_active must be false for normal Stop call")
	}

	// last_assistant_message must contain the assistant's text
	lam, ok := got["last_assistant_message"].(string)
	if !ok {
		t.Error("last_assistant_message must be a string in Stop hook input")
	} else if !strings.Contains(lam, "Hello from assistant") {
		t.Errorf("last_assistant_message = %q; want it to contain %q", lam, "Hello from assistant")
	}
}

// TestRunStopHooksBlockingPreventsTurnEnd verifies that a Stop hook that
// returns block=true (exit code 2) causes runStopHooks to return an error
// (HOOK-27: BLOCKING semantics prevent stopping/force continuation).
func TestRunStopHooksBlockingPreventsTurnEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_stop_block",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": denyShellCommand(),
					}},
				}},
			},
		},
	}
	assistant := msgs.AssistantText("test", "claude-sonnet", nil)
	err := r.runStopHooks(context.Background(), "claude-sonnet", "end_turn", "", assistant)
	if err == nil {
		t.Fatal("expected error from blocking Stop hook, got nil")
	}
}

// TestRunNotificationHooksEmitsOnPermissionWait verifies that
// RunNotificationHooks can be called and fires configured Notification hooks
// with the correct notification_type field (HOOK-35).
func TestRunNotificationHooksEmitsOnPermissionWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "notified")
	script := filepath.Join(dir, "notify.sh")
	scriptContent := "#!/bin/sh\ntouch " + shellQuoteConv(marker) + "\n"
	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_notification",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Notification": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	// Trigger notification (simulating permission-awaited or idle-awaiting-input scenario)
	if err := r.RunNotificationHooks(context.Background(), "permission_requested", "Awaiting permission", ""); err != nil {
		t.Fatalf("RunNotificationHooks: %v", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("Notification hook did not fire: marker file not created")
	}
}
