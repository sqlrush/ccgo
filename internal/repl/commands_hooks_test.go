package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestHooksHandlerNoHooksConfigured(t *testing.T) {
	settings := func() contracts.Settings { return contracts.Settings{} }
	h := hooksHandler(settings)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "No hooks configured") {
		t.Fatalf("expected 'No hooks configured' in status, got: %q", out.Status)
	}
}

func TestHooksHandlerDisableAllHooks(t *testing.T) {
	disabled := true
	settings := func() contracts.Settings {
		return contracts.Settings{
			DisableAllHooks: &disabled,
			Hooks: map[string]any{
				"PreToolUse": []any{"echo test"},
			},
		}
	}
	h := hooksHandler(settings)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "disabled") {
		t.Fatalf("expected 'disabled' in status, got: %q", out.Status)
	}
}

func TestHooksHandlerSummarisesCommandHooks(t *testing.T) {
	settings := func() contracts.Settings {
		return contracts.Settings{
			Hooks: map[string]any{
				"PreToolUse": []any{
					map[string]any{
						"matcher": "Bash",
						"hooks":   []any{"echo before-bash"},
					},
				},
				"PostToolUse": []any{"echo after"},
			},
		}
	}
	h := hooksHandler(settings)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "Configured hooks") {
		t.Fatalf("expected 'Configured hooks' in status, got: %q", out.Status)
	}
	if !strings.Contains(out.Status, "PreToolUse") {
		t.Fatalf("expected 'PreToolUse' in status, got: %q", out.Status)
	}
	if !strings.Contains(out.Status, "PostToolUse") {
		t.Fatalf("expected 'PostToolUse' in status, got: %q", out.Status)
	}
	if !strings.Contains(out.Status, "echo before-bash") {
		t.Fatalf("expected command text in status, got: %q", out.Status)
	}
}

func TestHooksHandlerHTTPHook(t *testing.T) {
	settings := func() contracts.Settings {
		return contracts.Settings{
			Hooks: map[string]any{
				"PreToolUse": []any{
					map[string]any{
						"type": "http",
						"url":  "https://example.com/hook",
					},
				},
			},
		}
	}
	h := hooksHandler(settings)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "https://example.com/hook") {
		t.Fatalf("expected URL in status, got: %q", out.Status)
	}
}
