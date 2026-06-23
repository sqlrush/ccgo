package repl

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

// TestColorHandlerSetsSessionColor verifies that /color <name> sets the
// screen's SessionColor field, producing a visible render change.
// CC ref: src/commands/color/color.ts — /color sets the agent's session colour.
func TestColorHandlerSetsSessionColor(t *testing.T) {
	screen := tui.NewREPLScreen(80, 24, nil)
	h := colorHandlerWith(nil) // no persistence needed for session-only color
	cc := CommandContext{Args: "blue", Screen: &screen}
	out, err := h(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if screen.SessionColor != "blue" {
		t.Errorf("expected screen.SessionColor='blue'; got %q", screen.SessionColor)
	}
}

// TestColorHandlerResetClearsSessionColor verifies that /color default resets
// the session colour (matches CC's RESET_ALIASES behaviour).
func TestColorHandlerResetClearsSessionColor(t *testing.T) {
	screen := tui.NewREPLScreen(80, 24, nil)
	screen.SessionColor = "green"
	h := colorHandlerWith(nil)
	cc := CommandContext{Args: "default", Screen: &screen}
	out, err := h(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if screen.SessionColor != "" {
		t.Errorf("expected screen.SessionColor to be cleared; got %q", screen.SessionColor)
	}
}
