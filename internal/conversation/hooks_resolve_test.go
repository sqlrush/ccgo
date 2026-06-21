package conversation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// shellQuoteConv returns a single-quoted shell string safe for inclusion in
// a sh -c command. Mirrors the helper in internal/hooks/command_test.go.
func shellQuoteConv(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// denyShellCommand returns a shell snippet that writes to stderr and exits 2,
// which conversation hooks interpret as Block (non-zero exit).
func denyShellCommand() string {
	return `printf '%s\n' 'stop blocked' >&2; exit 2`
}

func TestRunConversationHooksParallelBlock(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_conv",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "printf ctx-a"},
						map[string]any{"type": "command", "command": denyShellCommand()},
						map[string]any{"type": "command", "command": "printf ctx-c > " + shellQuoteConv(marker)},
					},
				}},
			},
		},
	}
	result, err := r.runConversationHooks(context.Background(), tool.HookStop, map[string]any{"stop_reason": "end_turn"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Block {
		t.Fatalf("expected Block from exit-2 hook; result=%#v", result)
	}
	// Parallel: even though hook[1] blocks, hook[2] still ran (no short-circuit).
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("hook[2] did not run (sequential short-circuit not removed): %v", statErr)
	}
}
