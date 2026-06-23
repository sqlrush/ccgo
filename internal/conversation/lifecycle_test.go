package conversation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestRunSessionStartHooksInjectsContext(t *testing.T) {
	dir := t.TempDir()
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_start",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"matcher": "startup",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": `printf '%s' '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"loaded ctx"}}'`,
					}},
				}},
			},
		},
	}
	got, err := r.RunSessionStartHooks(context.Background(), SessionStartStartup)
	if err != nil {
		t.Fatal(err)
	}
	if got != "loaded ctx" {
		t.Fatalf("context = %q want %q", got, "loaded ctx")
	}
}

func TestRunSessionStartHooksMatcherFilters(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_filter",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"matcher": "resume", // only fires on resume, not startup
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf ran > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	if _, err := r.RunSessionStartHooks(context.Background(), SessionStartStartup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("resume-matched hook must not fire on startup")
	}
}

func TestRunSessionEndHooks(t *testing.T) {
	r := Runner{WorkingDirectory: t.TempDir(), SessionID: "sess_end"}
	// No hooks configured → no error, no-op.
	if err := r.RunSessionEndHooks(context.Background(), SessionEndPromptInputExit); err != nil {
		t.Fatalf("SessionEnd no-op err: %v", err)
	}
}

var _ = tool.HookSessionStart // keep import if unused above

// TestRunSessionEndHooksBounded verifies that RunSessionEndHooks returns within
// the bounded timeout even when the hook command is slow.
// CC ref: src/utils/hooks.ts:175-182 (HOOK-29).
func TestRunSessionEndHooksBounded(t *testing.T) {
	r := Runner{
		WorkingDirectory: t.TempDir(),
		SessionID:        "sess_end_timeout",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionEnd": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "sleep 5",
						"timeout": 60, // hook's own timeout is long; the SessionEnd cap should win
					}},
				}},
			},
		},
	}
	t.Setenv("CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS", "300")
	start := time.Now()
	_ = r.RunSessionEndHooks(context.Background(), SessionEndPromptInputExit)
	elapsed := time.Since(start)
	// Should return within ~300ms + buffer, not 5s.
	if elapsed > 2*time.Second {
		t.Fatalf("RunSessionEndHooks took %v, expected <2s (bounded at 300ms)", elapsed)
	}
}

// TestGetSessionEndHookTimeoutMSEnvOverride verifies the env var override for
// the SessionEnd hook timeout.
func TestGetSessionEndHookTimeoutMSEnvOverride(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS", "500")
	got := getSessionEndHookTimeoutMS()
	if got != 500*time.Millisecond {
		t.Fatalf("expected 500ms, got %v", got)
	}
}

// TestGetSessionEndHookTimeoutMSDefault verifies the default is 1500ms when
// the env var is not set.
func TestGetSessionEndHookTimeoutMSDefault(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS", "")
	got := getSessionEndHookTimeoutMS()
	if got != 1500*time.Millisecond {
		t.Fatalf("expected 1500ms, got %v", got)
	}
}
