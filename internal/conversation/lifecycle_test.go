package conversation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
