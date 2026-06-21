package conversation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/compact"
	"ccgo/internal/contracts"
)

func TestRunSubagentStartHooks(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_sub",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SubagentStart": []any{map[string]any{
					"matcher": "code-reviewer",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf started > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	err := r.runSubagentStartHooks(context.Background(), map[string]any{
		"agent_id":   "a1",
		"agent_type": "code-reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("SubagentStart hook did not fire: %v", statErr)
	}
}

func TestRunSubagentStartHooksMatcherFilters(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_sub_filter",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SubagentStart": []any{map[string]any{
					"matcher": "code-reviewer",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf started > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	// agent_type does not match "code-reviewer" → hook must not fire
	err := r.runSubagentStartHooks(context.Background(), map[string]any{
		"agent_id":   "a2",
		"agent_type": "general-purpose",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("SubagentStart hook fired despite agent_type mismatch")
	}
}

func TestRunPostCompactHooks(t *testing.T) {
	r := Runner{WorkingDirectory: t.TempDir(), SessionID: "sess_pc"}
	// No hooks → no-op, no error.
	if err := r.runPostCompactHooks(context.Background(), compact.TriggerAuto, "summary text"); err != nil {
		t.Fatalf("PostCompact no-op err: %v", err)
	}
}

func TestRunPostCompactHooksFires(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "post_compact")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_pc_fire",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"PostCompact": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf done > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	if err := r.runPostCompactHooks(context.Background(), compact.TriggerManual, "some summary"); err != nil {
		t.Fatalf("PostCompact fire err: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("PostCompact hook did not fire: %v", statErr)
	}
}
