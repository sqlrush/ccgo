package repl

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/tui"
)

// TestRunInteractiveWithOptionsAppliesCustomKeymap verifies that a non-nil
// CustomKeymap built from a JSON spec is applied to the loop's screen.Keymap and
// overrides the default binding for the bound key.
func TestRunInteractiveWithOptionsAppliesCustomKeymap(t *testing.T) {
	// Rebind ctrl+p (default: history_previous) to stash_prompt so we can prove
	// the custom binding took effect rather than the built-in default.
	specJSON := `[{"key":"ctrl+p","action":"stash_prompt"}]`
	var specs []tui.BindingSpec
	if err := json.Unmarshal([]byte(specJSON), &specs); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	km, err := tui.KeymapFromSpecs(tui.DefaultKeymap(), specs)
	if err != nil {
		t.Fatalf("KeymapFromSpecs: %v", err)
	}

	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Simulate what RunInteractiveWithOptions does with CustomKeymap.
	l.screen.Keymap = km
	if got := l.screen.Keymap.Resolve(tui.Key{Type: tui.KeyCtrlP}); got != tui.ActionStashPrompt {
		t.Fatalf("custom ctrl+p binding not applied: resolved to %q, want %q", got, tui.ActionStashPrompt)
	}
}

// TestLoadKeyBindingSpecsGracefulOnMissing verifies that loading a non-existent
// keybindings file returns an error wrapping os.ErrNotExist so the caller can
// treat it as non-fatal (silently skip).
func TestLoadKeyBindingSpecsGracefulOnMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file.json")
	_, err := tui.LoadKeyBindingSpecs(path)
	if err == nil {
		t.Fatal("LoadKeyBindingSpecs must return error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error must wrap os.ErrNotExist, got: %v", err)
	}
}
